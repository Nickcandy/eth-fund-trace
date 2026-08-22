package tracer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/risk"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

var ErrInvalidQuery = errors.New("invalid trace query")
var ErrAddressNotSynced = errors.New("address is not synced")

type AddressNotSyncedError struct{ Address string }

func (e AddressNotSyncedError) Error() string { return ErrAddressNotSynced.Error() + ": " + e.Address }
func (e AddressNotSyncedError) Unwrap() error { return ErrAddressNotSynced }

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	QueryTransfers(context.Context, store.TransferQuery) ([]store.Transfer, error)
	ListLabels(context.Context, string, string) ([]store.Label, error)
}

type Query struct {
	Chain, Address, Direction, Asset string
	Depth, TopN                      int
}
type Node struct {
	Address  string `json:"address"`
	Depth    int    `json:"depth"`
	Terminal bool   `json:"terminal"`
}
type Edge struct {
	Transfer store.Transfer `json:"transfer"`
	Depth    int            `json:"depth"`
	Path     []string       `json:"path"`
}
type Result struct {
	Nodes            []Node               `json:"nodes"`
	Edges            []Edge               `json:"edges"`
	Paths            [][]string           `json:"paths,omitempty"`
	DataThroughBlock int64                `json:"dataThroughBlock"`
	DataStatus       string               `json:"dataStatus"`
	Labels           []risk.InferredLabel `json:"labels,omitempty"`
	Risk             risk.Result          `json:"risk"`
	RuleVersion      string               `json:"ruleVersion"`
}

type Graph struct{ repository Repository }

func New(repository Repository) *Graph { return &Graph{repository: repository} }

func ValidateQuery(query Query) error {
	q, err := normalize(query)
	if err != nil {
		return err
	}
	if _, err := ethaddr.Normalize(q.Address); err != nil {
		return ErrInvalidQuery
	}
	return nil
}

func (g *Graph) Trace(ctx context.Context, query Query) (Result, error) {
	q, err := normalize(query)
	if err != nil {
		return Result{}, err
	}
	seed, err := ethaddr.Normalize(q.Address)
	if err != nil {
		return Result{}, ErrInvalidQuery
	}
	metadata, found, err := g.repository.FindAddress(ctx, q.Chain, seed)
	if err != nil {
		return Result{}, err
	}
	if !found || metadata.SyncStatus != "synced" {
		return Result{}, AddressNotSyncedError{Address: seed}
	}
	result := Result{DataThroughBlock: metadata.LatestSyncedBlock, DataStatus: "synced", RuleVersion: "trace-v1"}
	seedLabels, err := g.repository.ListLabels(ctx, q.Chain, seed)
	if err != nil {
		return Result{}, err
	}
	frontier := []string{seed}
	visited := map[string]bool{seed: true}
	paths := map[string][]string{seed: {seed}}
	seedTerminal := metadata.IsTerminal || hasTerminalLabel(seedLabels)
	result.Nodes = append(result.Nodes, Node{Address: seed, Depth: 0, Terminal: seedTerminal})
	allTransfers := make([]store.Transfer, 0)
	resultFacts := make(map[string]bool)
	seenFacts := make(map[string]bool)
	for depth := 0; depth < q.Depth && len(frontier) > 0 && len(visited) < 5000; depth++ {
		sort.Strings(frontier)
		next := make([]string, 0)
		for offset := 0; offset < len(frontier); offset += 500 {
			end := offset + 500
			if end > len(frontier) {
				end = len(frontier)
			}
			queries := []string{q.Direction}
			for _, direction := range queries {
				transfers, queryErr := g.repository.QueryTransfers(ctx, store.TransferQuery{Chain: q.Chain, Addresses: frontier[offset:end], Direction: direction, AssetMode: assetMode(q.Asset), Asset: assetValue(q.Asset), Limit: 10000})
				if queryErr != nil {
					return Result{}, queryErr
				}
				byAddress := make(map[string][]store.Transfer)
				for _, transfer := range transfers {
					if !seenFacts[edgeKey(transfer)] {
						allTransfers = append(allTransfers, transfer)
						seenFacts[edgeKey(transfer)] = true
					}
					for _, address := range frontier[offset:end] {
						if direction == "both" {
							if strings.EqualFold(transfer.To, address) {
								byAddress[strings.ToLower(address)+"|in"] = append(byAddress[strings.ToLower(address)+"|in"], transfer)
							}
							if strings.EqualFold(transfer.From, address) {
								byAddress[strings.ToLower(address)+"|out"] = append(byAddress[strings.ToLower(address)+"|out"], transfer)
							}
						} else if (direction == "in" && strings.EqualFold(transfer.To, address)) || (direction == "out" && strings.EqualFold(transfer.From, address)) {
							byAddress[strings.ToLower(address)+"|"+direction] = append(byAddress[strings.ToLower(address)+"|"+direction], transfer)
						}
					}
				}
				for _, address := range frontier[offset:end] {
					if nodeTerminal(result.Nodes, address) {
						continue
					}
					directions := []string{q.Direction}
					if q.Direction == "both" {
						directions = []string{"in", "out"}
					}
					for _, selectedDirection := range directions {
						candidates := byAddress[strings.ToLower(address)+"|"+selectedDirection]
						sort.SliceStable(candidates, func(i, j int) bool { return edgeKey(candidates[i]) > edgeKey(candidates[j]) })
						count := 0
						for _, transfer := range candidates {
							if count >= q.TopN {
								break
							}
							other := strings.ToLower(transfer.To)
							if selectedDirection == "in" {
								other = strings.ToLower(transfer.From)
							} else if other == strings.ToLower(address) {
								other = strings.ToLower(transfer.From)
							}
							if other == "" {
								continue
							}
							path := append(append([]string(nil), paths[address]...), other)
							if !resultFacts[edgeKey(transfer)] {
								result.Edges = append(result.Edges, Edge{Transfer: transfer, Depth: depth + 1, Path: path})
								resultFacts[edgeKey(transfer)] = true
							}
							count++
							if visited[other] || count >= q.TopN || len(visited) >= 5000 {
								continue
							}
							otherMetadata, otherFound, metadataErr := g.repository.FindAddress(ctx, q.Chain, other)
							if metadataErr != nil {
								return Result{}, metadataErr
							}
							if !otherFound || otherMetadata.SyncStatus != "synced" {
								return Result{}, AddressNotSyncedError{Address: other}
							}
							otherLabels, labelsErr := g.repository.ListLabels(ctx, q.Chain, other)
							if labelsErr != nil {
								return Result{}, labelsErr
							}
							terminal := otherFound && otherMetadata.IsTerminal || hasTerminalLabel(otherLabels)
							visited[other] = true
							if !terminal {
								next = append(next, other)
							}
							paths[other] = path
							result.Nodes = append(result.Nodes, Node{Address: other, Depth: depth + 1, Terminal: terminal})
							result.Paths = append(result.Paths, path)
						}
					}
				}
			}
		}
		frontier = next
	}
	allLabels := append([]store.Label(nil), seedLabels...)
	for _, node := range result.Nodes[1:] {
		labels, labelErr := g.repository.ListLabels(ctx, q.Chain, node.Address)
		if labelErr != nil {
			return Result{}, labelErr
		}
		allLabels = append(allLabels, labels...)
	}
	result.Risk = risk.Analyze(seed, allTransfers, allLabels)
	result.Labels = result.Risk.InferredLabels
	return result, nil
}

func hasTerminalLabel(labels []store.Label) bool {
	for _, label := range labels {
		value := strings.ToLower(label.Type)
		if value == "exchange" || value == "exchange_hot_wallet" || value == "hot_wallet" {
			return true
		}
	}
	return false
}

func nodeTerminal(nodes []Node, address string) bool {
	for _, node := range nodes {
		if strings.EqualFold(node.Address, address) {
			return node.Terminal
		}
	}
	return false
}

func normalize(q Query) (Query, error) {
	q.Chain = strings.ToLower(strings.TrimSpace(q.Chain))
	if q.Chain == "" {
		q.Chain = "ethereum"
	}
	if q.Chain != "ethereum" {
		return q, ErrInvalidQuery
	}
	if q.Depth == 0 {
		q.Depth = 3
	}
	if q.TopN == 0 {
		q.TopN = 10
	}
	if q.Depth < 1 || q.Depth > 5 || q.TopN < 1 || q.TopN > 20 {
		return q, ErrInvalidQuery
	}
	d := strings.ToLower(q.Direction)
	if d == "" {
		d = "both"
	}
	if d != "in" && d != "out" && d != "both" {
		return q, ErrInvalidQuery
	}
	q.Direction = d
	if q.Asset != "" && !strings.EqualFold(q.Asset, "all") && !strings.EqualFold(q.Asset, "eth") && !strings.EqualFold(q.Asset, "erc20") {
		if _, err := ethaddr.Normalize(q.Asset); err != nil {
			return q, ErrInvalidQuery
		}
	}
	return q, nil
}
func assetMode(value string) string {
	switch strings.ToLower(value) {
	case "", "all":
		return "all"
	case "eth":
		return "eth"
	case "erc20":
		return "erc20"
	default:
		return "contract"
	}
}
func assetValue(value string) string {
	if strings.EqualFold(value, "eth") {
		return "ETH"
	}
	if assetMode(value) == "contract" {
		a, _ := ethaddr.Normalize(value)
		return a
	}
	return ""
}
func edgeKey(t store.Transfer) string {
	return fmt.Sprintf("%020d|%s|%s|%s|%020d|%s", t.BlockNumber, t.TxHash, t.Source, t.TraceID, t.LogIndex, t.Asset)
}
