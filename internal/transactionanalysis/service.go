package transactionanalysis

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

const (
	EthereumV3Factory = "0x1f98431c8ad98523631ae4a59f267346ea31f984"
	EthereumWETH      = "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
	transferTopic     = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	depositTopic      = "0xe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c"
	withdrawalTopic   = "0x7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65"
	swapTopic         = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"
	zeroAddress       = "0x0000000000000000000000000000000000000000"
)

var (
	ErrInvalidHash      = errors.New("invalid transaction hash")
	ErrUnsupportedChain = errors.New("unsupported transaction analysis chain")
)

var officialContracts = map[string]string{
	"0xe592427a0aece92de3edee1f18e0157c05861564": "Uniswap V3 SwapRouter",
	"0x68b3465833fb72a70ecdf485e0e4c7bd8665fc45": "Uniswap SwapRouter02",
	"0xef1c6e67703c7bd7107eed8303fbe6ec2554bf6b": "Uniswap Universal Router",
	"0x66a9893cc07d91d95644aedd05d03f95e1dba8af": "Uniswap Universal Router",
	EthereumWETH:      "Wrapped Ether",
	EthereumV3Factory: "Uniswap V3 Factory",
}

// Source provides transaction facts and read-only contract calls.
type Source interface {
	TransactionByHash(context.Context, string) (etherscan.RPCTransaction, error)
	TransactionReceipt(context.Context, string) (etherscan.RPCReceipt, error)
	Call(context.Context, string, string) (string, error)
}

// Repository persists transaction analysis and pool metadata caches.
type Repository interface {
	FindTransactionAnalysis(context.Context, string, string) (store.TransactionAnalysis, bool, error)
	SaveTransactionAnalysis(context.Context, store.TransactionAnalysis) error
	FindPoolMetadata(context.Context, string, string) (store.PoolMetadata, bool, error)
	SavePoolMetadata(context.Context, store.PoolMetadata) error
}

// Service analyzes confirmed Ethereum transaction receipts.
type Service struct {
	source Source
	repo   Repository
	now    func() time.Time
}

// New creates a transaction analysis service.
func New(source Source, repo Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{source: source, repo: repo, now: now}
}

// SupportsContract reports whether this analyzer has protocol rules for an entry contract.
func (s *Service) SupportsContract(address string) bool {
	_, ok := officialContracts[strings.ToLower(address)]
	return ok
}

// Analyze returns cached analysis or derives it from Etherscan Proxy facts.
func (s *Service) Analyze(ctx context.Context, chain, txHash string) (store.TransactionAnalysis, error) {
	chain = strings.ToLower(strings.TrimSpace(chain))
	if chain == "" {
		chain = "ethereum"
	}
	if chain != "ethereum" {
		return store.TransactionAnalysis{}, ErrUnsupportedChain
	}
	txHash = strings.ToLower(strings.TrimSpace(txHash))
	if !validHash(txHash) {
		return store.TransactionAnalysis{}, ErrInvalidHash
	}
	if cached, found, err := s.repo.FindTransactionAnalysis(ctx, chain, txHash); err != nil {
		return store.TransactionAnalysis{}, fmt.Errorf("find transaction analysis: %w", err)
	} else if found {
		return cached, nil
	}
	transaction, err := s.source.TransactionByHash(ctx, txHash)
	if err != nil {
		return store.TransactionAnalysis{}, fmt.Errorf("fetch transaction: %w", err)
	}
	receipt, err := s.source.TransactionReceipt(ctx, txHash)
	if err != nil {
		return store.TransactionAnalysis{}, fmt.Errorf("fetch receipt: %w", err)
	}
	blockNumber, err := hexInt64(receipt.BlockNumber)
	if err != nil {
		return store.TransactionAnalysis{}, fmt.Errorf("parse receipt block: %w", etherscan.ErrMalformedResponse)
	}
	from, fromOK := normalizedAddress(transaction.From, false)
	to, toOK := normalizedAddress(transaction.To, true)
	value, valueOK := hexDecimal(transaction.Value)
	if !fromOK || !toOK || !valueOK || (receipt.Status != "0x0" && receipt.Status != "0x1") {
		return store.TransactionAnalysis{}, fmt.Errorf("validate transaction fields: %w", etherscan.ErrMalformedResponse)
	}
	analysis := store.TransactionAnalysis{
		Chain: "ethereum", ChainID: 1, TxHash: txHash, BlockNumber: blockNumber,
		From: from, To: to, Value: value, Input: transaction.Input,
		Succeeded: receipt.Status == "0x1", Transfers: []store.ReceiptTransfer{}, Swaps: []store.SwapEvent{}, Wraps: []store.WrapEvent{},
		Quality: store.AnalysisQuality{Status: "complete", Evidence: []string{"transaction", "receipt"}}, AnalyzedAt: s.now().UTC(),
	}
	if name, ok := officialContracts[analysis.To]; ok {
		analysis.EntryContract, analysis.EntryContractName = analysis.To, name
	}
	if !analysis.Succeeded {
		analysis.Quality.Status = "partial"
		analysis.Quality.Issues = append(analysis.Quality.Issues, "transaction execution failed")
	} else {
		s.parseLogs(ctx, receipt.Logs, &analysis)
	}
	s.finalizeRoute(&analysis)
	if err := s.repo.SaveTransactionAnalysis(ctx, analysis); err != nil {
		return store.TransactionAnalysis{}, fmt.Errorf("save transaction analysis: %w", err)
	}
	return analysis, nil
}

func (s *Service) parseLogs(ctx context.Context, logs []etherscan.RPCLog, analysis *store.TransactionAnalysis) {
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}
		index, err := hexInt64(log.LogIndex)
		if err != nil {
			analysis.Quality.Status = "partial"
			analysis.Quality.Issues = append(analysis.Quality.Issues, "log with invalid index ignored")
			continue
		}
		topic := strings.ToLower(log.Topics[0])
		switch topic {
		case transferTopic:
			transfer, ok := parseTransfer(log, index)
			if ok {
				analysis.Transfers = append(analysis.Transfers, transfer)
			} else {
				analysis.Quality.Status = "partial"
				analysis.Quality.Issues = append(analysis.Quality.Issues, "malformed Transfer log ignored")
			}
		case depositTopic, withdrawalTopic:
			wrap, ok := parseWrap(log, index, topic)
			if ok {
				analysis.Wraps = append(analysis.Wraps, wrap)
			} else {
				analysis.Quality.Status = "partial"
				analysis.Quality.Issues = append(analysis.Quality.Issues, "malformed WETH log ignored")
			}
		case swapTopic:
			swap, ok := parseSwap(log, index)
			if !ok {
				analysis.Quality.Status = "partial"
				analysis.Quality.Issues = append(analysis.Quality.Issues, "malformed V3-shaped Swap log ignored")
				continue
			}
			metadata, err := s.poolMetadata(ctx, swap.Pool)
			if err != nil {
				analysis.Quality.Status = "partial"
				analysis.Quality.Issues = append(analysis.Quality.Issues, "pool metadata unavailable")
				swap.Evidence = []string{"receipt Swap topic; pool identity unverified"}
			} else if metadata.Verified {
				swap.Verified, swap.Protocol, swap.Version, swap.Fee = true, "uniswap", "v3", metadata.Fee
				if swap.AmountIn == swap.rawAmount0 {
					swap.TokenIn, swap.TokenOut = metadata.Token0, metadata.Token1
				} else {
					swap.TokenIn, swap.TokenOut = metadata.Token1, metadata.Token0
				}
				swap.Evidence = []string{"receipt Swap log", "pool factory() matches official Uniswap V3 Factory"}
			} else {
				swap.Evidence = []string{"receipt Swap topic", "pool factory() does not match official Uniswap V3 Factory"}
			}
			analysis.Swaps = append(analysis.Swaps, swap.SwapEvent)
		}
	}
	sort.Slice(analysis.Swaps, func(i, j int) bool { return analysis.Swaps[i].LogIndex < analysis.Swaps[j].LogIndex })
	sort.Slice(analysis.Wraps, func(i, j int) bool { return analysis.Wraps[i].LogIndex < analysis.Wraps[j].LogIndex })
}

type parsedSwap struct {
	store.SwapEvent
	rawAmount0 string
}

func parseSwap(log etherscan.RPCLog, index int64) (parsedSwap, bool) {
	words, ok := words(log.Data, 5)
	if !ok || len(log.Topics) < 3 {
		return parsedSwap{}, false
	}
	amount0, ok0 := signedWord(words[0])
	amount1, ok1 := signedWord(words[1])
	if !ok0 || !ok1 || amount0.Sign()*amount1.Sign() != -1 {
		return parsedSwap{}, false
	}
	result := parsedSwap{SwapEvent: store.SwapEvent{Pool: address(log.Address), Sender: topicAddress(log.Topics[1]), Recipient: topicAddress(log.Topics[2]), LogIndex: index}}
	if amount0.Sign() > 0 {
		result.AmountIn, result.AmountOut, result.rawAmount0 = amount0.String(), new(big.Int).Abs(amount1).String(), amount0.String()
	} else {
		result.AmountIn, result.AmountOut, result.rawAmount0 = amount1.String(), new(big.Int).Abs(amount0).String(), amount0.String()
	}
	return result, true
}

func parseTransfer(log etherscan.RPCLog, index int64) (store.ReceiptTransfer, bool) {
	words, ok := words(log.Data, 1)
	if !ok || len(log.Topics) < 3 {
		return store.ReceiptTransfer{}, false
	}
	amount, ok := unsignedWord(words[0])
	return store.ReceiptTransfer{Token: address(log.Address), From: topicAddress(log.Topics[1]), To: topicAddress(log.Topics[2]), Amount: amount, LogIndex: index}, ok
}

func parseWrap(log etherscan.RPCLog, index int64, topic string) (store.WrapEvent, bool) {
	words, ok := words(log.Data, 1)
	if !ok || len(log.Topics) < 2 || address(log.Address) != EthereumWETH {
		return store.WrapEvent{}, false
	}
	amount, ok := unsignedWord(words[0])
	kind := "deposit"
	if topic == withdrawalTopic {
		kind = "withdrawal"
	}
	return store.WrapEvent{Type: kind, Account: topicAddress(log.Topics[1]), Amount: amount, LogIndex: index, Evidence: "WETH contract receipt log"}, ok
}

func (s *Service) poolMetadata(ctx context.Context, pool string) (store.PoolMetadata, error) {
	if cached, found, err := s.repo.FindPoolMetadata(ctx, "ethereum", pool); err != nil {
		return store.PoolMetadata{}, fmt.Errorf("find pool metadata: %w", err)
	} else if found {
		return cached, nil
	}
	token0Raw, err := s.source.Call(ctx, pool, "0x0dfe1681")
	if err != nil {
		return store.PoolMetadata{}, fmt.Errorf("read token0: %w", err)
	}
	token1Raw, err := s.source.Call(ctx, pool, "0xd21220a7")
	if err != nil {
		return store.PoolMetadata{}, fmt.Errorf("read token1: %w", err)
	}
	feeRaw, err := s.source.Call(ctx, pool, "0xddca3f43")
	if err != nil {
		return store.PoolMetadata{}, fmt.Errorf("read fee: %w", err)
	}
	factoryRaw, err := s.source.Call(ctx, pool, "0xc45a0155")
	if err != nil {
		return store.PoolMetadata{}, fmt.Errorf("read factory: %w", err)
	}
	fee, err := hexInt64(feeRaw)
	if err != nil || fee > int64(^uint32(0)>>1) {
		return store.PoolMetadata{}, fmt.Errorf("invalid pool fee")
	}
	metadata := store.PoolMetadata{Chain: "ethereum", Pool: pool, Token0: returnAddress(token0Raw), Token1: returnAddress(token1Raw), Fee: int32(fee), Factory: returnAddress(factoryRaw), ObservedAt: s.now().UTC()}
	if metadata.Token0 == "" || metadata.Token1 == "" || metadata.Factory == "" {
		return store.PoolMetadata{}, fmt.Errorf("invalid pool metadata")
	}
	metadata.Verified = metadata.Factory == EthereumV3Factory
	if err := s.repo.SavePoolMetadata(ctx, metadata); err != nil {
		return store.PoolMetadata{}, fmt.Errorf("save pool metadata: %w", err)
	}
	return metadata, nil
}

func (s *Service) finalizeRoute(analysis *store.TransactionAnalysis) {
	verified := make([]int, 0, len(analysis.Swaps))
	for i := range analysis.Swaps {
		if !analysis.Swaps[i].Verified {
			continue
		}
		verified = append(verified, i)
		output, ambiguous := resolveOutput(analysis.Swaps[i], analysis.Transfers)
		analysis.Swaps[i].OutputAddress = output
		analysis.Quality.AmbiguousRoute = analysis.Quality.AmbiguousRoute || ambiguous
	}
	if len(verified) == 0 {
		analysis.Quality.Status = "partial"
		analysis.Quality.Issues = append(analysis.Quality.Issues, "no verified Uniswap V3 pool")
		return
	}
	for i := 1; i < len(verified); i++ {
		if analysis.Swaps[verified[i-1]].TokenOut != analysis.Swaps[verified[i]].TokenIn {
			analysis.Quality.AmbiguousRoute = true
		}
	}
	if len(verified) != len(analysis.Swaps) {
		analysis.Quality.AmbiguousRoute = true
	}
	last := analysis.Swaps[verified[len(verified)-1]]
	_, outputIsContract := officialContracts[last.OutputAddress]
	if last.OutputAddress == "" || outputIsContract {
		analysis.Quality.AmbiguousRoute = true
	}
	if analysis.Quality.AmbiguousRoute {
		analysis.Quality.Status = "partial"
		analysis.Quality.Issues = append(analysis.Quality.Issues, "overall output route is ambiguous")
	} else {
		analysis.FinalOutputAddress = last.OutputAddress
		analysis.Quality.Evidence = append(analysis.Quality.Evidence, "verified Uniswap V3 pool logs")
	}
}

func resolveOutput(swap store.SwapEvent, transfers []store.ReceiptTransfer) (string, bool) {
	candidates := make(map[string]bool)
	for _, transfer := range transfers {
		if transfer.Token == swap.TokenOut && transfer.From == swap.Pool && transfer.Amount == swap.AmountOut {
			candidates[transfer.To] = true
		}
	}
	if len(candidates) != 1 {
		return "", len(candidates) > 1
	}
	current := ""
	for candidate := range candidates {
		current = candidate
	}
	seen := map[string]bool{current: true}
	for {
		next := make(map[string]bool)
		for _, transfer := range transfers {
			if transfer.Token == swap.TokenOut && transfer.From == current && transfer.To != swap.Pool && transfer.To != zeroAddress {
				if seen[transfer.To] {
					return "", true
				}
				next[transfer.To] = true
			}
		}
		if len(next) == 0 {
			break
		}
		if len(next) > 1 {
			return "", true
		}
		for candidate := range next {
			current = candidate
			seen[current] = true
		}
	}
	return current, false
}

func validHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, ok := new(big.Int).SetString(value[2:], 16)
	return ok
}

func words(value string, count int) ([]string, bool) {
	value = strings.TrimPrefix(value, "0x")
	if len(value) != count*64 {
		return nil, false
	}
	result := make([]string, count)
	for i := range result {
		result[i] = value[i*64 : (i+1)*64]
		if _, ok := new(big.Int).SetString(result[i], 16); !ok {
			return nil, false
		}
	}
	return result, true
}

func signedWord(value string) (*big.Int, bool) {
	number, ok := new(big.Int).SetString(value, 16)
	if !ok {
		return nil, false
	}
	if number.Bit(255) == 1 {
		number.Sub(number, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return number, true
}

func unsignedWord(value string) (string, bool) {
	number, ok := new(big.Int).SetString(value, 16)
	if !ok {
		return "", false
	}
	return number.String(), true
}

func hexInt64(value string) (int64, error) {
	value = strings.TrimPrefix(value, "0x")
	return strconv.ParseInt(value, 16, 64)
}

func hexDecimal(value string) (string, bool) {
	number, ok := new(big.Int).SetString(strings.TrimPrefix(value, "0x"), 16)
	if !ok {
		return "", false
	}
	return number.String(), true
}

func address(value string) string { return strings.ToLower(value) }

func normalizedAddress(value string, allowEmpty bool) (string, bool) {
	value = address(value)
	if value == "" && allowEmpty {
		return "", true
	}
	if len(value) != 42 || !strings.HasPrefix(value, "0x") {
		return "", false
	}
	_, ok := new(big.Int).SetString(value[2:], 16)
	return value, ok
}

func topicAddress(value string) string {
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	if len(value) != 64 {
		return ""
	}
	return "0x" + value[24:]
}

func returnAddress(value string) string {
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	if len(value) != 64 {
		return ""
	}
	return "0x" + value[24:]
}
