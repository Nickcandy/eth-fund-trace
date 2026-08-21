package etherscan

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

type rawTransfer struct {
	BlockNumber  string `json:"blockNumber"`
	TimeStamp    string `json:"timeStamp"`
	Hash         string `json:"hash"`
	From         string `json:"from"`
	To           string `json:"to"`
	Value        string `json:"value"`
	TraceID      string `json:"traceId"`
	TokenName    string `json:"tokenName"`
	TokenSymbol  string `json:"tokenSymbol"`
	TokenDecimal string `json:"tokenDecimal"`
	TokenAddress string `json:"contractAddress"`
	LogIndex     string `json:"logIndex"`
}

func normalize(items []json.RawMessage, action string) ([]store.Transfer, error) {
	transfers := make([]store.Transfer, 0, len(items))
	for _, item := range items {
		var raw rawTransfer
		if err := json.Unmarshal(item, &raw); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedResponse, err)
		}
		transfer, err := normalizeOne(raw, action)
		if err != nil {
			return nil, err
		}
		transfers = append(transfers, transfer)
	}
	return transfers, nil
}

func normalizeOne(raw rawTransfer, action string) (store.Transfer, error) {
	blockNumber, err := parseInt(raw.BlockNumber, "blockNumber")
	if err != nil {
		return store.Transfer{}, err
	}
	timestamp, err := parseInt(raw.TimeStamp, "timeStamp")
	if err != nil {
		return store.Transfer{}, err
	}
	if raw.Hash == "" || raw.From == "" || raw.To == "" || raw.Value == "" {
		return store.Transfer{}, fmt.Errorf("%w: missing required transaction field", ErrMalformedResponse)
	}
	transfer := store.Transfer{
		Chain:       "ethereum",
		ChainID:     1,
		TxHash:      raw.Hash,
		BlockNumber: blockNumber,
		BlockTime:   time.Unix(timestamp, 0).UTC(),
		From:        raw.From,
		To:          raw.To,
		AssetType:   "eth",
		Asset:       "ETH",
		Amount:      raw.Value,
		Source:      action,
		TraceID:     raw.TraceID,
		ObservedAt:  time.Now().UTC(),
	}
	if action == "tokentx" {
		decimals, err := parseInt(raw.TokenDecimal, "tokenDecimal")
		if err != nil {
			return store.Transfer{}, err
		}
		logIndex, err := parseInt(raw.LogIndex, "logIndex")
		if err != nil {
			return store.Transfer{}, err
		}
		if raw.TokenAddress == "" {
			return store.Transfer{}, fmt.Errorf("%w: missing contractAddress", ErrMalformedResponse)
		}
		transfer.AssetType = "erc20"
		transfer.Asset = raw.TokenAddress
		transfer.Symbol = raw.TokenSymbol
		transfer.Decimals = int32(decimals)
		transfer.Amount = ""
		transfer.TokenValue = raw.Value
		transfer.LogIndex = logIndex
	}
	return transfer, nil
}

func parseInt(value, field string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%w: invalid %s", ErrMalformedResponse, field)
	}
	return parsed, nil
}
