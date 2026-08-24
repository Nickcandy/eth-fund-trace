package fundgraph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/chains"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

var (
	ErrInvalidQuery     = errors.New("invalid edge query")
	ErrAddressNotSynced = errors.New("address is not synced")
)

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	QueryTransfers(context.Context, store.TransferQuery) ([]store.Transfer, error)
}

type Graph struct {
	repository Repository
}

type EdgeQuery struct {
	Chain     string
	Addresses []string
	Direction string
	Asset     string
	FromBlock int64
	ToBlock   int64
	Limit     int
	Cursor    string
}

type Edge struct {
	Chain       string    `json:"chain"`
	ChainID     int64     `json:"chainId"`
	TxHash      string    `json:"txHash"`
	BlockNumber int64     `json:"blockNumber"`
	BlockTime   time.Time `json:"blockTime,omitempty"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	AssetType   string    `json:"assetType"`
	Asset       string    `json:"asset"`
	Symbol      string    `json:"symbol,omitempty"`
	Decimals    int32     `json:"decimals"`
	Amount      string    `json:"amount,omitempty"`
	TokenValue  string    `json:"tokenValue,omitempty"`
	Source      string    `json:"source"`
	TraceID     string    `json:"traceId,omitempty"`
	LogIndex    int64     `json:"logIndex"`
}

type EdgePage struct {
	Items            []Edge `json:"items"`
	NextCursor       string `json:"nextCursor,omitempty"`
	DataThroughBlock int64  `json:"dataThroughBlock"`
	DataStatus       string `json:"dataStatus"`
}

type cursor struct {
	Version     int    `json:"v"`
	BlockNumber int64  `json:"b"`
	TxHash      string `json:"h"`
	Source      string `json:"s"`
	TraceID     string `json:"t"`
	LogIndex    int64  `json:"l"`
	Asset       string `json:"a"`
}

func New(repository Repository) *Graph {
	return &Graph{repository: repository}
}

func (g *Graph) Edges(ctx context.Context, query EdgeQuery) (EdgePage, error) {
	normalized, dataThrough, dataStatus, err := g.normalize(ctx, query)
	if err != nil {
		return EdgePage{}, err
	}
	transfers, err := g.repository.QueryTransfers(ctx, normalized)
	if err != nil {
		return EdgePage{}, err
	}
	hasMore := len(transfers) > int(normalized.Limit)-1
	if len(transfers) > int(normalized.Limit)-1 {
		transfers = transfers[:normalized.Limit-1]
	}
	items := make([]Edge, 0, len(transfers))
	for _, transfer := range transfers {
		items = append(items, edgeFromTransfer(transfer))
	}
	page := EdgePage{Items: items, DataThroughBlock: dataThrough, DataStatus: dataStatus}
	if hasMore && len(transfers) > 0 {
		page.NextCursor, err = encodeCursor(transfers[len(transfers)-1])
		if err != nil {
			return EdgePage{}, err
		}
	}
	return page, nil
}

func (g *Graph) normalize(ctx context.Context, query EdgeQuery) (store.TransferQuery, int64, string, error) {
	query.Chain = strings.ToLower(strings.TrimSpace(query.Chain))
	if query.Chain == "" {
		query.Chain = "ethereum"
	}
	chain, chainErr := chains.Resolve(query.Chain)
	query.Chain = chain.Name
	if chainErr != nil || len(query.Addresses) == 0 || query.FromBlock < 0 || query.ToBlock < 0 || (query.ToBlock > 0 && query.FromBlock > query.ToBlock) {
		return store.TransferQuery{}, 0, "", ErrInvalidQuery
	}
	seen := make(map[string]struct{}, len(query.Addresses))
	addresses := make([]string, 0, len(query.Addresses))
	dataThrough := int64(-1)
	dataStatus := "synced"
	for _, value := range query.Addresses {
		address, err := ethaddr.Normalize(value)
		if err != nil {
			return store.TransferQuery{}, 0, "", ErrInvalidQuery
		}
		if _, ok := seen[address]; ok {
			continue
		}
		metadata, found, err := g.repository.FindAddress(ctx, query.Chain, address)
		if err != nil {
			return store.TransferQuery{}, 0, "", err
		}
		if !found || (metadata.SyncStatus != "synced" && metadata.LastSyncedAt.IsZero()) {
			return store.TransferQuery{}, 0, "", ErrAddressNotSynced
		}
		if metadata.SyncStatus != "synced" {
			dataStatus = "stale"
		}
		if dataThrough < 0 || metadata.LatestSyncedBlock < dataThrough {
			dataThrough = metadata.LatestSyncedBlock
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	direction := strings.ToLower(query.Direction)
	if direction == "" {
		direction = "both"
	}
	if direction != "in" && direction != "out" && direction != "both" {
		return store.TransferQuery{}, 0, "", ErrInvalidQuery
	}
	assetMode, asset, err := normalizeAsset(query.Asset)
	if err != nil {
		return store.TransferQuery{}, 0, "", err
	}
	limit := query.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 500 {
		return store.TransferQuery{}, 0, "", ErrInvalidQuery
	}
	var after *store.TransferCursor
	if query.Cursor != "" {
		decoded, err := decodeCursor(query.Cursor)
		if err != nil {
			return store.TransferQuery{}, 0, "", ErrInvalidQuery
		}
		after = &store.TransferCursor{BlockNumber: decoded.BlockNumber, TxHash: decoded.TxHash, Source: decoded.Source, TraceID: decoded.TraceID, LogIndex: decoded.LogIndex, Asset: decoded.Asset}
	}
	return store.TransferQuery{Chain: query.Chain, Addresses: addresses, Direction: direction, AssetMode: assetMode, Asset: asset, FromBlock: query.FromBlock, ToBlock: query.ToBlock, Limit: int64(limit + 1), After: after}, dataThrough, dataStatus, nil
}

func normalizeAsset(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "all":
		return "all", "", nil
	case "eth":
		return "eth", "ETH", nil
	case "erc20":
		return "erc20", "", nil
	default:
		address, err := ethaddr.Normalize(value)
		if err != nil {
			return "", "", ErrInvalidQuery
		}
		return "contract", address, nil
	}
}

func encodeCursor(transfer store.Transfer) (string, error) {
	data, err := json.Marshal(cursor{Version: 1, BlockNumber: transfer.BlockNumber, TxHash: transfer.TxHash, Source: transfer.Source, TraceID: transfer.TraceID, LogIndex: transfer.LogIndex, Asset: transfer.Asset})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeCursor(value string) (cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor{}, err
	}
	var result cursor
	if err := json.Unmarshal(data, &result); err != nil || result.Version != 1 || result.TxHash == "" || result.Source == "" || result.Asset == "" {
		return cursor{}, ErrInvalidQuery
	}
	return result, nil
}

func edgeFromTransfer(value store.Transfer) Edge {
	return Edge{
		Chain: value.Chain, ChainID: value.ChainID, TxHash: value.TxHash, BlockNumber: value.BlockNumber, BlockTime: value.BlockTime,
		From: value.From, To: value.To, AssetType: value.AssetType, Asset: value.Asset, Symbol: value.Symbol, Decimals: value.Decimals,
		Amount: value.Amount, TokenValue: value.TokenValue, Source: value.Source, TraceID: value.TraceID, LogIndex: value.LogIndex,
	}
}
