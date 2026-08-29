package transactionanalysis

import (
	"context"
	"encoding/hex"
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
	// AnalysisVersion invalidates cached analyses when semantic rules change.
	AnalysisVersion   = "transaction-analysis-v6"
	EthereumV3Factory = "0x1f98431c8ad98523631ae4a59f267346ea31f984"
	EthereumWETH      = "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
	// KyberSwapRouter is the Ethereum MetaAggregation Router supported by the RFQ adapter.
	KyberSwapRouter = "0x6131b5fae19ea4f9d964eac0408e4408b66337b5"
	// KyberSwapExecutor is the Ethereum Executor supported by the RFQ adapter.
	KyberSwapExecutor = "0x6e4141d33021b52c91c28608403db4a0ffb50ec6"
	THORChainRouter   = "0xd37bbe5744d730a1d98d8dc97c42f0ca46ad7146"
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

const EthereumL1StandardBridge = "0x3154cf16ccdb4c6d922629664174b904d80f2c35"

var officialContracts = map[string]string{
	"0xe592427a0aece92de3edee1f18e0157c05861564": "Uniswap V3 SwapRouter",
	"0x68b3465833fb72a70ecdf485e0e4c7bd8665fc45": "Uniswap SwapRouter02",
	"0xef1c6e67703c7bd7107eed8303fbe6ec2554bf6b": "Uniswap Universal Router",
	"0x66a9893cc07d91d95644aedd05d03f95e1dba8af": "Uniswap Universal Router",
	EthereumWETH:             "Wrapped Ether",
	EthereumV3Factory:        "Uniswap V3 Factory",
	KyberSwapRouter:          "KyberSwap MetaAggregation Router",
	KyberSwapExecutor:        "KyberSwap Executor",
	THORChainRouter:          "THORChain Router v4",
	EthereumL1StandardBridge: "Base L1 Standard Bridge",
}

var contractIdentities = map[string]store.AddressIdentity{
	KyberSwapRouter:          {AddressType: "contract", Protocol: "kyberswap", Roles: []string{"kyberswap_router"}},
	KyberSwapExecutor:        {AddressType: "contract", Protocol: "kyberswap", Roles: []string{"kyberswap_executor"}},
	THORChainRouter:          {AddressType: "contract", Protocol: "thorchain", Roles: []string{"router"}},
	EthereumV3Factory:        {AddressType: "contract", Protocol: "uniswap", Roles: []string{"factory"}},
	EthereumWETH:             {AddressType: "contract", Protocol: "weth", Roles: []string{"wrapped_native_token"}},
	EthereumL1StandardBridge: {AddressType: "contract", Protocol: "bridge", Roles: []string{"cross_chain_bridge"}},
}

// wooXIdentities contains the Ethereum addresses publicly labelled for WOO X.
// The list is based on the public EVM CEX address registry (2025-01-07,
// https://gist.github.com/xfwil/07dadf39ae559829132952734ca524f3), with role
// names preserving the distinction between operational wallets and vaults.
var wooXIdentities = map[string]store.AddressIdentity{
	"0x03dd167d62e1dfc223ffd7b37fc8bf45fb973478": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}},
	"0x63dfe4e34a3bfc00eb0220786238a7c6cef8ffc4": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}},
	"0xe505bf08c03cc0fa4e0fdfa2487e2c11085b3fd9": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}},
	"0xea319fd75766f5180018f8e760f51c3d3c457496": {AddressType: "contract", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}},
	"0x1e6dce7ce381774286abb8c9aac461bb7b1c4b05": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}},
	"0xeef97691d3307b4e61522170f648ee2df1312fee": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}},
	"0x594203e46e0b41b1edb54a551e7784c194d1335b": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}},
	"0x607e062e3986a16283047beaed1a7dc3e220ff0e": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}},
	"0x0d83f81bc9f1e8252f87a4109bbf0d90171c81df": {AddressType: "contract", Protocol: "woo_x", Roles: []string{"woo_x_wallet", "woo_x_staking_cold"}},
	"0x1326a1f39746726fdcfe88d83effe5451606ae85": {AddressType: "contract", Protocol: "woo_x", Roles: []string{"woo_x_wallet", "woo_x_vault"}},
	"0xf0b8660476ea1af0f363de8816e3e7cd1c8f1fde": {AddressType: "contract", Protocol: "woo_x", Roles: []string{"woo_x_wallet", "woo_x_vault"}},
	"0xe2933566f172d08f8c90144fed5ae28e9d54b1ec": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet", "woo_x_team"}},
	"0x15271e572267def474366bb683719cc59489efbe": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet", "woo_x_treasury"}},
	"0xd7d8bcae65537cb5079a4fb249b9fbb4526e4084": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet", "woo_x_treasury"}},
	"0xfa2d1f15557170f6c4a4c5249e77f534184cdb79": {AddressType: "contract", Protocol: "woo_x", Roles: []string{"woo_x_wallet", "woo_x_treasury"}},
	"0xe64eb20471491956338eedc0f98242bc3ad0c91b": {AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet", "woo_x_deployer"}},
}

// Source provides transaction facts and read-only contract calls.
type Source interface {
	TransactionByHash(context.Context, string) (etherscan.RPCTransaction, error)
	TransactionReceipt(context.Context, string) (etherscan.RPCReceipt, error)
	Call(context.Context, string, string) (string, error)
	CodeAt(context.Context, string) (string, error)
	InternalTransactionsByHash(context.Context, string) ([]etherscan.InternalTransaction, error)
}

// InspectAddress returns a chain-confirmed account type and known protocol roles.
func (s *Service) InspectAddress(ctx context.Context, chain, address string) (store.AddressIdentity, error) {
	if strings.ToLower(strings.TrimSpace(chain)) != "ethereum" {
		return store.AddressIdentity{}, ErrUnsupportedChain
	}
	address = strings.ToLower(strings.TrimSpace(address))
	if identity, ok := wooXIdentities[address]; ok {
		return identity, nil
	}
	code, err := s.source.CodeAt(ctx, address)
	if err != nil {
		return store.AddressIdentity{}, fmt.Errorf("fetch address bytecode: %w", err)
	}
	if code == "0x" || code == "0x0" {
		return store.AddressIdentity{AddressType: "eoa"}, nil
	}
	if identity, ok := contractIdentities[address]; ok {
		return identity, nil
	}
	pool, found, err := s.repo.FindPoolMetadata(ctx, chain, address)
	if err != nil {
		return store.AddressIdentity{}, fmt.Errorf("find pool identity: %w", err)
	}
	if found && pool.Verified {
		return store.AddressIdentity{AddressType: "contract", Protocol: "uniswap", Roles: []string{"pool"}}, nil
	}
	return store.AddressIdentity{AddressType: "contract"}, nil
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
	} else if found && cached.AnalysisVersion == AnalysisVersion {
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
	internalTransactions, internalErr := s.source.InternalTransactionsByHash(ctx, txHash)
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
		AnalysisVersion: AnalysisVersion, Chain: "ethereum", ChainID: 1, TxHash: txHash, BlockNumber: blockNumber,
		From: from, To: to, Value: value, Input: transaction.Input,
		Succeeded: receipt.Status == "0x1", Transfers: []store.ReceiptTransfer{}, Swaps: []store.SwapEvent{}, Wraps: []store.WrapEvent{}, InternalCalls: []store.InternalCall{}, Conversions: []store.SwapConversion{},
		Quality: store.AnalysisQuality{Status: "complete", Evidence: []string{"transaction", "receipt"}}, AnalyzedAt: s.now().UTC(),
	}
	if internalErr != nil {
		analysis.Quality.Status = "partial"
		analysis.Quality.Issues = append(analysis.Quality.Issues, "internal transactions unavailable")
	} else {
		for _, call := range internalTransactions {
			analysis.InternalCalls = append(analysis.InternalCalls, store.InternalCall{From: strings.ToLower(call.From), To: strings.ToLower(call.To), Value: call.Value, Type: call.Type, TraceID: call.TraceID, IsError: call.IsError})
		}
		analysis.Quality.Evidence = append(analysis.Quality.Evidence, "etherscan txlistinternal by transaction hash")
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
	switch {
	case analysis.Succeeded && analysis.To == THORChainRouter:
		if !parseTHORChainCall(&analysis) {
			analysis.Quality.Status = "partial"
			analysis.Quality.Issues = append(analysis.Quality.Issues, "unsupported or malformed THORChain calldata")
		} else if isTHORChainTransferOutAction(analysis.ProtocolAction) && !verifiedTHORChainTransferOut(analysis) {
			analysis.Quality.Status = "partial"
			analysis.Quality.Issues = append(analysis.Quality.Issues, "THORChain transferOut evidence mismatch")
		} else if isTHORChainTransferOutAction(analysis.ProtocolAction) {
			analysis.Quality.Evidence = append(analysis.Quality.Evidence, "verified THORChain transferOut")
		}
	case analysis.To == KyberSwapRouter:
		s.finalizeKyberRFQ(&analysis, internalErr)
	default:
		s.finalizeRoute(&analysis)
		s.appendUniswapConversion(&analysis)
	}
	if err := s.repo.SaveTransactionAnalysis(ctx, analysis); err != nil {
		return store.TransactionAnalysis{}, fmt.Errorf("save transaction analysis: %w", err)
	}
	return analysis, nil
}

func parseTHORChainCall(analysis *store.TransactionAnalysis) bool {
	if !analysis.Succeeded || analysis.To != THORChainRouter || len(analysis.Input) < 10 || !strings.EqualFold(analysis.Input[2:10], "574da717") {
		return false
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(analysis.Input[10:], "0x"))
	if err != nil || len(raw) < 128 {
		return false
	}
	word := func(offset int) []byte { return raw[offset : offset+32] }
	destination := "0x" + hex.EncodeToString(word(0)[12:])
	asset := "0x" + hex.EncodeToString(word(32)[12:])
	amount := new(big.Int).SetBytes(word(64)).String()
	offsetValue := new(big.Int).SetBytes(word(96))
	if !offsetValue.IsInt64() {
		return false
	}
	offset := offsetValue.Int64()
	if offset < 0 || offset > int64(len(raw))-32 {
		return false
	}
	lengthValue := new(big.Int).SetBytes(raw[offset : offset+32])
	if !lengthValue.IsInt64() {
		return false
	}
	length := lengthValue.Int64()
	start := offset + 32
	if length < 0 || length > int64(len(raw))-start {
		return false
	}
	memoBytes, err := hex.DecodeString(hex.EncodeToString(raw[start : start+length]))
	if err != nil {
		return false
	}
	analysis.ProtocolMemo = string(memoBytes)
	analysis.ProtocolDestination = destination
	analysis.ProtocolAsset = asset
	if asset == zeroAddress {
		analysis.ProtocolAsset = "ETH"
	}
	analysis.ProtocolAmount = amount
	parts := strings.SplitN(analysis.ProtocolMemo, ":", 2)
	action := strings.ToUpper(parts[0])
	switch action {
	case "MIGRATE":
		analysis.ProtocolAction = "vault_migration"
	case "OUT":
		analysis.ProtocolAction = "protocol_outbound"
	case "SWAP":
		analysis.ProtocolAction = "cross_chain_swap"
	case "REFUND":
		analysis.ProtocolAction = "refund"
	default:
		analysis.ProtocolAction = "protocol_internal"
	}
	return true
}

func verifiedTHORChainMigration(analysis store.TransactionAnalysis) bool {
	return analysis.ProtocolAction == "vault_migration" && verifiedTHORChainTransferOut(analysis)
}

func isTHORChainTransferOutAction(action string) bool {
	switch action {
	case "vault_migration", "protocol_outbound", "cross_chain_swap", "refund":
		return true
	default:
		return false
	}
}

func verifiedTHORChainTransferOut(analysis store.TransactionAnalysis) bool {
	if !analysis.Succeeded || analysis.To != THORChainRouter || !isTHORChainTransferOutAction(analysis.ProtocolAction) || analysis.ProtocolDestination == "" || analysis.ProtocolAmount == "" {
		return false
	}
	if analysis.ProtocolAsset == "ETH" {
		return hasInternalCall(analysis.InternalCalls, THORChainRouter, analysis.ProtocolDestination, analysis.ProtocolAmount)
	}
	for _, transfer := range analysis.Transfers {
		if transfer.Token == analysis.ProtocolAsset && transfer.From == THORChainRouter && transfer.To == analysis.ProtocolDestination && transfer.Amount == analysis.ProtocolAmount {
			return true
		}
	}
	return false
}

func (s *Service) finalizeKyberRFQ(analysis *store.TransactionAnalysis, internalErr error) {
	partial := store.SwapConversion{Protocol: "kyberswap", Version: "rfq", Status: "partial", Initiator: analysis.From, Router: KyberSwapRouter, Executor: KyberSwapExecutor}
	if !analysis.Succeeded {
		partial.Issues = []string{"transaction execution failed"}
		analysis.Conversions = append(analysis.Conversions, partial)
		analysis.Quality.Status = "partial"
		return
	}
	if internalErr != nil {
		partial.Issues = []string{"internal transactions unavailable"}
		analysis.Conversions = append(analysis.Conversions, partial)
		analysis.Quality.Status = "partial"
		return
	}
	for _, call := range analysis.InternalCalls {
		if call.IsError {
			partial.Issues = []string{"failed internal call"}
			analysis.Conversions = append(analysis.Conversions, partial)
			analysis.Quality.Status = "partial"
			return
		}
	}
	candidates := make([]store.SwapConversion, 0, 1)
	for _, input := range analysis.Transfers {
		if input.From != analysis.From || input.To != KyberSwapExecutor || input.Token == EthereumWETH {
			continue
		}
		for _, payment := range analysis.Transfers {
			if payment.From != KyberSwapExecutor || payment.Token != input.Token || payment.Amount != input.Amount {
				continue
			}
			provider := payment.To
			for _, output := range analysis.Transfers {
				if output.Token != EthereumWETH || output.From != provider || output.To != KyberSwapExecutor {
					continue
				}
				if !hasWithdrawalFunding(analysis.Wraps, analysis.InternalCalls, KyberSwapExecutor, output.Amount) {
					continue
				}
				recipients := internalRecipients(analysis.InternalCalls, KyberSwapExecutor, output.Amount)
				if len(recipients) != 1 {
					continue
				}
				candidates = append(candidates, store.SwapConversion{
					Protocol: "kyberswap", Version: "rfq", Status: "complete", Initiator: analysis.From, Router: KyberSwapRouter, Executor: KyberSwapExecutor,
					LiquidityProvider: provider, Recipient: recipients[0], TokenIn: input.Token, AmountIn: input.Amount, TokenOut: "ETH", AmountOut: output.Amount,
					Evidence: []string{"receipt transfer: initiator to executor", "receipt transfer: liquidity provider WETH to executor", "receipt transfer: executor token payment to liquidity provider", "WETH withdrawal log", "internal ETH calls"},
				})
			}
		}
	}
	if len(candidates) != 1 {
		partial.Issues = []string{"RFQ participants or amounts are ambiguous"}
		analysis.Conversions = append(analysis.Conversions, partial)
		analysis.Quality.Status = "partial"
		return
	}
	analysis.Conversions = append(analysis.Conversions, candidates[0])
	analysis.FinalOutputAddress = candidates[0].Recipient
	analysis.Quality.Status = "complete"
	analysis.Quality.AmbiguousRoute = false
	analysis.Quality.Evidence = append(analysis.Quality.Evidence, "verified KyberSwap RFQ transfer and internal-call balance")
}

func hasInternalCall(calls []store.InternalCall, from, to, value string) bool {
	for _, call := range calls {
		if !call.IsError && call.From == from && call.To == to && (value == "" || call.Value == value) {
			return true
		}
	}
	return false
}

func internalRecipients(calls []store.InternalCall, from, value string) []string {
	result := make([]string, 0, 1)
	for _, call := range calls {
		if !call.IsError && call.From == from && call.Value == value && call.To != EthereumWETH {
			result = append(result, call.To)
		}
	}
	return result
}

func hasWithdrawalFunding(wraps []store.WrapEvent, calls []store.InternalCall, account, payoutAmount string) bool {
	payout, ok := new(big.Int).SetString(payoutAmount, 10)
	if !ok {
		return false
	}
	for _, wrap := range wraps {
		if wrap.Type != "withdrawal" || wrap.Account != account || !hasInternalCall(calls, EthereumWETH, account, wrap.Amount) {
			continue
		}
		withdrawn, valid := new(big.Int).SetString(wrap.Amount, 10)
		if !valid {
			continue
		}
		delta := new(big.Int).Sub(withdrawn, payout)
		if delta.Sign() < 0 {
			delta.Neg(delta)
		}
		if delta.Cmp(big.NewInt(1)) <= 0 {
			return true
		}
	}
	return false
}

func (s *Service) appendUniswapConversion(analysis *store.TransactionAnalysis) {
	if analysis.Quality.Status != "complete" || len(analysis.Swaps) == 0 {
		return
	}
	first, last := analysis.Swaps[0], analysis.Swaps[len(analysis.Swaps)-1]
	analysis.Conversions = append(analysis.Conversions, store.SwapConversion{Protocol: "uniswap", Version: "v3", Status: "complete", Initiator: analysis.From, Router: analysis.To, Recipient: analysis.FinalOutputAddress, TokenIn: first.TokenIn, AmountIn: first.AmountIn, TokenOut: last.TokenOut, AmountOut: last.AmountOut, Evidence: []string{"verified Uniswap V3 pool logs"}})
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
