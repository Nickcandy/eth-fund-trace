package etherscan

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

var errMissingRequiredTransactionField = errors.New("missing required transaction field")

type rawTransfer struct {
	BlockNumber     string `json:"blockNumber"`
	TimeStamp       string `json:"timeStamp"`
	Hash            string `json:"hash"`
	From            string `json:"from"`
	To              string `json:"to"`
	Value           string `json:"value"`
	TraceID         string `json:"traceId"`
	TokenName       string `json:"tokenName"`
	TokenSymbol     string `json:"tokenSymbol"`
	TokenDecimal    string `json:"tokenDecimal"`
	ContractAddress string `json:"contractAddress"`
	LogIndex        string `json:"logIndex"`
	IsError         string `json:"isError"`
	ReceiptStatus   string `json:"txreceipt_status"`
}

func normalize(items []json.RawMessage, action string) ([]store.Transfer, error) {
	return normalizeWithState(items, action, make(map[string]int64))
}

func normalizeWithState(items []json.RawMessage, action string, tokenOccurrences map[string]int64) ([]store.Transfer, error) {
	transfers := make([]store.Transfer, 0, len(items))
	for _, item := range items {
		var raw rawTransfer
		if err := json.Unmarshal(item, &raw); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedResponse, err)
		}
		if (action == "txlist" || action == "txlistinternal") && raw.IsError == "1" {
			continue
		}
		if action == "txlist" && raw.ReceiptStatus == "0" {
			continue
		}
		var missingLogIndex int64
		if action == "tokentx" && raw.LogIndex == "" {
			key := tokenIdentityKey(raw)
			missingLogIndex = syntheticLogIndex(key, tokenOccurrences[key])
			tokenOccurrences[key]++
		}
		transfer, err := normalizeOne(raw, action, missingLogIndex)
		if err != nil {
			if errors.Is(err, errMissingRequiredTransactionField) {
				// Etherscan occasionally emits incomplete contract-creation/internal rows.
				// Keep the valid rows from this page and let the next scan retry the range.
				continue
			}
			return nil, err
		}
		transfers = append(transfers, transfer)
	}
	return transfers, nil
}

func normalizeOne(raw rawTransfer, action string, missingLogIndex int64) (store.Transfer, error) {
	blockNumber, err := parseInt(raw.BlockNumber, "blockNumber")
	if err != nil {
		return store.Transfer{}, err
	}
	timestamp, err := parseInt(raw.TimeStamp, "timeStamp")
	if err != nil {
		return store.Transfer{}, err
	}
	to := raw.To
	if action == "txlist" && to == "" {
		to = raw.ContractAddress
	}
	if raw.Hash == "" || raw.From == "" || to == "" || raw.Value == "" {
		return store.Transfer{}, fmt.Errorf("%w: %w", ErrMalformedResponse, errMissingRequiredTransactionField)
	}
	if value, ok := new(big.Int).SetString(raw.Value, 10); !ok || value.Sign() < 0 {
		return store.Transfer{}, fmt.Errorf("%w: invalid value", ErrMalformedResponse)
	}
	transfer := store.Transfer{
		Chain:            "ethereum",
		ChainID:          1,
		TxHash:           raw.Hash,
		BlockNumber:      blockNumber,
		BlockTime:        time.Unix(timestamp, 0).UTC(),
		From:             raw.From,
		To:               to,
		AssetType:        "eth",
		Asset:            "ETH",
		Amount:           raw.Value,
		Source:           action,
		TraceID:          raw.TraceID,
		ObservedAt:       time.Now().UTC(),
		TransferKind:     "transfer",
		TransactionGroup: "1:" + strings.ToLower(raw.Hash),
	}
	if action == "tokentx" {
		var decimals int64
		metadataComplete := raw.TokenDecimal != ""
		if metadataComplete {
			decimals, err = parseInt(raw.TokenDecimal, "tokenDecimal")
			if err != nil || decimals > 255 {
				if err == nil {
					err = fmt.Errorf("%w: invalid tokenDecimal", ErrMalformedResponse)
				}
				return store.Transfer{}, err
			}
		}
		logIndex := missingLogIndex
		if raw.LogIndex != "" {
			logIndex, err = parseInt(raw.LogIndex, "logIndex")
			if err != nil {
				return store.Transfer{}, err
			}
		}
		if raw.ContractAddress == "" {
			return store.Transfer{}, fmt.Errorf("%w: missing contractAddress", ErrMalformedResponse)
		}
		transfer.AssetType = "erc20"
		transfer.Asset = raw.ContractAddress
		transfer.Symbol = raw.TokenSymbol
		transfer.TokenName = raw.TokenName
		transfer.Decimals = int32(decimals)
		transfer.Amount = ""
		transfer.TokenValue = raw.Value
		transfer.LogIndex = logIndex
		transfer.LogIndexSynthetic = raw.LogIndex == ""
		transfer.TokenMetadataComplete = metadataComplete
		const zero = "0x0000000000000000000000000000000000000000"
		switch {
		case strings.EqualFold(raw.From, zero):
			transfer.TransferKind = "mint"
		case strings.EqualFold(to, zero):
			transfer.TransferKind = "burn"
		}
	}
	return transfer, nil
}

func tokenIdentityKey(raw rawTransfer) string {
	return strings.Join([]string{
		strings.ToLower(raw.Hash),
		strings.ToLower(raw.ContractAddress),
		strings.ToLower(raw.From),
		strings.ToLower(raw.To),
		raw.Value,
	}, "\x00")
}

func syntheticLogIndex(key string, occurrence int64) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(key))
	_, _ = hasher.Write([]byte("\x00" + strconv.FormatInt(occurrence, 10)))
	return -int64(hasher.Sum64()&uint64(^uint64(0)>>1)) - 1
}

func parseInt(value, field string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%w: invalid %s", ErrMalformedResponse, field)
	}
	return parsed, nil
}
