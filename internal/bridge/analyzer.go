package bridge

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/chainrpc"
	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"golang.org/x/crypto/sha3"
)

const (
	ProtocolOfficialOPStack = "official_opstack_bridge"
	AdapterVersion          = "bridge-opstack-v1"

	// Base mainnet deployments. Sources: Base contract-address reference and
	// OP Stack contracts-bedrock Predeploys library, verified 2026-08-24.
	EthereumL1StandardBridge = "0x3154cf16ccdb4c6d922629664174b904d80f2c35"
	EthereumOptimismPortal   = "0x49048044d57e1c92a77f79988d21fa8faf74e97e"
	EthereumL1Messenger      = "0x866e82a600a1414e583f7f13623f1aC5d58b0aFa"
	BaseL2Messenger          = "0x4200000000000000000000000000000000000007"
	BaseL2StandardBridge     = "0x4200000000000000000000000000000000000010"
	BaseL2ToL1MessagePasser  = "0x4200000000000000000000000000000000000016"
)

var (
	ErrMalformedEvent = errors.New("malformed bridge event")
	ErrAmbiguousMatch = errors.New("ambiguous bridge match")
)

type BridgeSource interface {
	TransactionReceipt(context.Context, string) (etherscan.RPCReceipt, error)
	Logs(context.Context, chainrpc.LogFilter) ([]chainrpc.Log, error)
}

type AnalyzerRepository interface {
	UpsertCrossChainLink(context.Context, store.CrossChainLink) (store.CrossChainLink, error)
	QueryCrossChainLinks(context.Context, store.BridgeLinkQuery) ([]store.CrossChainLink, error)
}

type Analyzer struct {
	sources       map[string]BridgeSource
	repo          AnalyzerRepository
	now           func() time.Time
	confirmations map[string]int64
}

func NewAnalyzer(sources map[string]BridgeSource, repo AnalyzerRepository, now func() time.Time) *Analyzer {
	if now == nil {
		now = time.Now
	}
	return &Analyzer{sources: sources, repo: repo, now: now, confirmations: map[string]int64{}}
}

func (a *Analyzer) WithConfirmations(values map[string]int64) *Analyzer {
	a.confirmations = values
	return a
}

func (a *Analyzer) Analyze(ctx context.Context, chain, txHash string) ([]store.CrossChainLink, error) {
	source := a.sources[chain]
	if source == nil || (chain != "ethereum" && chain != "base") {
		return nil, ErrInvalidRequest
	}
	receipt, err := source.TransactionReceipt(ctx, strings.ToLower(txHash))
	if err != nil {
		return nil, err
	}
	if receipt.Status == "0x0" {
		return []store.CrossChainLink{}, nil
	}
	if receipt.Status != "0x1" {
		return nil, fmt.Errorf("%w: invalid receipt status", ErrMalformedEvent)
	}
	if blockSource, ok := source.(interface {
		BlockNumber(context.Context) (int64, error)
	}); ok {
		block, blockErr := parseHexInt(receipt.BlockNumber)
		if blockErr != nil {
			return nil, ErrMalformedEvent
		}
		head, headErr := blockSource.BlockNumber(ctx)
		if headErr != nil {
			return nil, headErr
		}
		if head-block < a.confirmations[chain] {
			return nil, chainrpc.ErrPending
		}
	}

	result := make([]store.CrossChainLink, 0)
	for _, log := range receipt.Logs {
		fact, recognized, parseErr := parseSourceEvent(chain, log)
		if parseErr != nil {
			return nil, parseErr
		}
		if !recognized {
			continue
		}
		fact.SourceTxHash = strings.ToLower(receipt.TransactionHash)
		if fact.SourceTxHash == "" {
			fact.SourceTxHash = strings.ToLower(txHash)
		}
		fact.SourceBlock, err = parseHexInt(receipt.BlockNumber)
		if err != nil {
			return nil, fmt.Errorf("%w: block number", ErrMalformedEvent)
		}
		fact.Protocol, fact.AdapterVersion = ProtocolOfficialOPStack, AdapterVersion
		if fact.Direction == "withdrawal" {
			fact.MessageHash, fact.Nonce = withdrawalMessage(receipt.Logs)
		} else {
			fact.MessageHash, fact.Nonce = depositMessage(receipt.Logs)
		}
		fact.IdentityKey = identityKey(fact)
		fact.Status, fact.EvidenceLevel = "initiated", "strong"
		fact.ObservedAt, fact.LastCheckedAt = a.now().UTC(), a.now().UTC()
		fact.NextCheckAt = fact.LastCheckedAt.Add(time.Minute)

		matches, matchErr := a.findTarget(ctx, fact)
		if matchErr != nil {
			return nil, matchErr
		}
		if len(matches) > 1 {
			fact.Status, fact.EvidenceLevel = "ambiguous", "partial"
			fact.Evidence = append(fact.Evidence, "bridge_ambiguous_match")
		} else if len(matches) == 1 {
			applyTarget(&fact, matches[0])
		}
		saved, saveErr := a.repo.UpsertCrossChainLink(ctx, fact)
		if saveErr != nil {
			return nil, saveErr
		}
		result = append(result, saved)
	}
	return result, nil
}

func (a *Analyzer) Refresh(ctx context.Context, link store.CrossChainLink) (store.CrossChainLink, error) {
	if link.Direction == "withdrawal" && link.MessageHash != "" {
		if err := a.advanceWithdrawalLifecycle(ctx, &link); err != nil {
			return a.deferLink(ctx, link, err)
		}
		if link.Status == "failed" {
			link.NextCheckAt = time.Time{}
			return a.repo.UpsertCrossChainLink(ctx, link)
		}
	}
	matches, err := a.findTarget(ctx, link)
	link.LastCheckedAt = a.now().UTC()
	if err != nil {
		return a.deferLink(ctx, link, err)
	}
	if len(matches) == 1 {
		applyTarget(&link, matches[0])
	} else if len(matches) > 1 {
		link.Status, link.EvidenceLevel = "ambiguous", "partial"
	}
	if link.Status != "completed" {
		link.NextCheckAt = link.LastCheckedAt.Add(5 * time.Minute)
	}
	return a.repo.UpsertCrossChainLink(ctx, link)
}

func (a *Analyzer) deferLink(ctx context.Context, link store.CrossChainLink, err error) (store.CrossChainLink, error) {
	link.LastCheckedAt = a.now().UTC()
	link.NextCheckAt = link.LastCheckedAt.Add(5 * time.Minute)
	saved, saveErr := a.repo.UpsertCrossChainLink(ctx, link)
	if saveErr != nil {
		return link, saveErr
	}
	return saved, err
}

func (a *Analyzer) advanceWithdrawalLifecycle(ctx context.Context, link *store.CrossChainLink) error {
	source := a.sources["ethereum"]
	if source == nil {
		return nil
	}
	messageTopic := []string{strings.ToLower(link.MessageHash)}
	proven, err := source.Logs(ctx, chainrpc.LogFilter{FromBlock: "0x0", ToBlock: "latest", Address: EthereumOptimismPortal, Topics: [][]string{{eventTopic("WithdrawalProven(bytes32,address,address)")}, messageTopic}})
	if err != nil {
		return err
	}
	if len(proven) > 0 && statusRank(link.Status) < statusRank("proven") {
		if confirmed, confirmErr := a.logConfirmed(ctx, "ethereum", proven[0]); confirmErr != nil {
			return confirmErr
		} else if confirmed {
			link.Status = "proven"
			link.Evidence = append(link.Evidence, "portal_withdrawal_proven:"+strings.ToLower(proven[0].TransactionHash))
		}
	}
	finalized, err := source.Logs(ctx, chainrpc.LogFilter{FromBlock: "0x0", ToBlock: "latest", Address: EthereumOptimismPortal, Topics: [][]string{{eventTopic("WithdrawalFinalized(bytes32,bool)")}, messageTopic}})
	if err != nil {
		return err
	}
	if len(finalized) == 0 {
		return nil
	}
	confirmed, err := a.logConfirmed(ctx, "ethereum", finalized[0])
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}
	success, parseErr := dataUint(finalized[0].Data, 0)
	if parseErr != nil {
		return parseErr
	}
	if success == "0" {
		link.Status, link.EvidenceLevel = "failed", "confirmed"
		link.Evidence = append(link.Evidence, "portal_withdrawal_failed:"+strings.ToLower(finalized[0].TransactionHash))
		return nil
	}
	if statusRank(link.Status) < statusRank("finalized") {
		link.Status = "finalized"
		link.Evidence = append(link.Evidence, "portal_withdrawal_finalized:"+strings.ToLower(finalized[0].TransactionHash))
	}
	return nil
}

func (a *Analyzer) findTarget(ctx context.Context, link store.CrossChainLink) ([]chainrpc.Log, error) {
	source := a.sources[link.TargetChain]
	if source == nil {
		return nil, nil
	}
	address := BaseL2StandardBridge
	topic := finalizedTopic(link.SourceAsset == "ETH")
	if link.Direction == "withdrawal" && link.MessageHash != "" {
		portalLogs, err := source.Logs(ctx, chainrpc.LogFilter{FromBlock: "0x0", ToBlock: "latest", Address: EthereumOptimismPortal, Topics: [][]string{{eventTopic("WithdrawalFinalized(bytes32,bool)")}, {strings.ToLower(link.MessageHash)}}})
		if err != nil {
			return nil, err
		}
		verified := make([]chainrpc.Log, 0, len(portalLogs))
		for _, portalLog := range portalLogs {
			confirmed, confirmErr := a.logConfirmed(ctx, "ethereum", portalLog)
			if confirmErr != nil {
				return nil, confirmErr
			}
			if !confirmed {
				continue
			}
			receipt, receiptErr := source.TransactionReceipt(ctx, portalLog.TransactionHash)
			if receiptErr != nil {
				continue
			}
			for _, receiptLog := range receipt.Logs {
				candidate := chainrpc.Log{Address: receiptLog.Address, Topics: receiptLog.Topics, Data: receiptLog.Data, LogIndex: receiptLog.LogIndex, TransactionHash: portalLog.TransactionHash, BlockNumber: portalLog.BlockNumber}
				if _, ok, _ := parseTargetEvent(link, candidate); ok {
					verified = append(verified, candidate)
				}
			}
		}
		return verified, nil
	}
	if link.Direction == "withdrawal" {
		return nil, nil
	}
	if link.Direction == "deposit" && link.MessageHash != "" {
		relayLogs, err := source.Logs(ctx, chainrpc.LogFilter{FromBlock: "0x0", ToBlock: "latest", Address: BaseL2Messenger, Topics: [][]string{{eventTopic("RelayedMessage(bytes32)")}, {strings.ToLower(link.MessageHash)}}})
		if err != nil {
			return nil, err
		}
		verified := make([]chainrpc.Log, 0, len(relayLogs))
		for _, relayLog := range relayLogs {
			confirmed, confirmErr := a.logConfirmed(ctx, "base", relayLog)
			if confirmErr != nil {
				return nil, confirmErr
			}
			if !confirmed {
				continue
			}
			receipt, receiptErr := source.TransactionReceipt(ctx, relayLog.TransactionHash)
			if receiptErr != nil {
				continue
			}
			for _, receiptLog := range receipt.Logs {
				candidate := chainrpc.Log{Address: receiptLog.Address, Topics: receiptLog.Topics, Data: receiptLog.Data, LogIndex: receiptLog.LogIndex, TransactionHash: relayLog.TransactionHash, BlockNumber: relayLog.BlockNumber}
				if _, ok, _ := parseTargetEvent(link, candidate); ok {
					verified = append(verified, candidate)
				}
			}
		}
		return verified, nil
	}
	if link.Direction == "deposit" {
		return nil, nil
	}
	logs, err := source.Logs(ctx, chainrpc.LogFilter{FromBlock: "0x0", ToBlock: "latest", Address: address, Topics: [][]string{{topic}}})
	if err != nil {
		return nil, err
	}
	matched := logs[:0]
	var safeHead int64 = 1<<63 - 1
	if blockSource, ok := source.(interface {
		BlockNumber(context.Context) (int64, error)
	}); ok {
		head, headErr := blockSource.BlockNumber(ctx)
		if headErr != nil {
			return nil, headErr
		}
		safeHead = head - a.confirmations[link.TargetChain]
	}
	for _, candidate := range logs {
		block, blockErr := parseHexInt(candidate.BlockNumber)
		if blockErr != nil || block > safeHead {
			continue
		}
		parsed, ok, parseErr := parseTargetEvent(link, candidate)
		if parseErr != nil {
			continue
		}
		if ok {
			matched = append(matched, parsed)
		}
	}
	return matched, nil
}

func (a *Analyzer) logConfirmed(ctx context.Context, chain string, log chainrpc.Log) (bool, error) {
	source := a.sources[chain]
	blockSource, ok := source.(interface {
		BlockNumber(context.Context) (int64, error)
	})
	if !ok {
		return true, nil
	}
	block, err := parseHexInt(log.BlockNumber)
	if err != nil {
		return false, ErrMalformedEvent
	}
	head, err := blockSource.BlockNumber(ctx)
	if err != nil {
		return false, err
	}
	return block <= head-a.confirmations[chain], nil
}

func depositMessage(logs []etherscan.RPCLog) (string, string) {
	var value = new(big.Int)
	for _, log := range logs {
		if strings.EqualFold(log.Address, EthereumL1Messenger) && len(log.Topics) == 2 && strings.EqualFold(log.Topics[0], eventTopic("SentMessageExtension1(address,uint256)")) {
			if raw, err := dataWord(log.Data, 0); err == nil {
				value.SetString(raw, 16)
			}
		}
	}
	for _, log := range logs {
		if !strings.EqualFold(log.Address, EthereumL1Messenger) || len(log.Topics) != 2 || !strings.EqualFold(log.Topics[0], eventTopic("SentMessage(address,address,bytes,uint256,uint256)")) {
			continue
		}
		sender, err := dataAddress(log.Data, 0)
		if err != nil {
			continue
		}
		offsetRaw, err := dataWord(log.Data, 1)
		if err != nil {
			continue
		}
		offsetValue, ok := new(big.Int).SetString(offsetRaw, 16)
		if !ok || !offsetValue.IsInt64() {
			continue
		}
		nonceRaw, err := dataWord(log.Data, 2)
		if err != nil {
			continue
		}
		gasRaw, err := dataWord(log.Data, 3)
		if err != nil {
			continue
		}
		message, err := dynamicBytes(log.Data, int(offsetValue.Int64()))
		if err != nil {
			continue
		}
		target := topicAddress(log.Topics[1])
		if target == "" {
			continue
		}
		encoded := relayMessageEncoding(nonceRaw, sender, target, value, gasRaw, message)
		h := sha3.NewLegacyKeccak256()
		_, _ = h.Write(encoded)
		nonce, _ := new(big.Int).SetString(nonceRaw, 16)
		return fmt.Sprintf("0x%x", h.Sum(nil)), nonce.String()
	}
	return "", ""
}

func relayMessageEncoding(nonceHex, sender, target string, value *big.Int, gasHex string, message []byte) []byte {
	selectorHash := sha3.NewLegacyKeccak256()
	_, _ = selectorHash.Write([]byte("relayMessage(uint256,address,address,uint256,uint256,bytes)"))
	selector := selectorHash.Sum(nil)[:4]
	words := []string{nonceHex, addressWordHex(sender), addressWordHex(target), fmt.Sprintf("%064x", value), gasHex, fmt.Sprintf("%064x", 6*32), fmt.Sprintf("%064x", len(message))}
	payload := append([]byte{}, selector...)
	for _, word := range words {
		decoded, _ := hex.DecodeString(word)
		payload = append(payload, decoded...)
	}
	payload = append(payload, message...)
	if remainder := len(message) % 32; remainder != 0 {
		payload = append(payload, make([]byte, 32-remainder)...)
	}
	return payload
}

func dynamicBytes(data string, offset int) ([]byte, error) {
	value := strings.TrimPrefix(data, "0x")
	start := offset * 2
	if start < 0 || len(value) < start+64 {
		return nil, ErrMalformedEvent
	}
	length, ok := new(big.Int).SetString(value[start:start+64], 16)
	if !ok || !length.IsInt64() {
		return nil, ErrMalformedEvent
	}
	end := start + 64 + int(length.Int64())*2
	if end > len(value) {
		return nil, ErrMalformedEvent
	}
	decoded, err := hex.DecodeString(value[start+64 : end])
	if err != nil {
		return nil, ErrMalformedEvent
	}
	return decoded, nil
}

func addressWordHex(address string) string {
	return strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(address), "0x")
}

func withdrawalMessage(logs []etherscan.RPCLog) (string, string) {
	for _, log := range logs {
		if strings.ToLower(log.Address) != BaseL2ToL1MessagePasser || len(log.Topics) != 4 || strings.ToLower(log.Topics[0]) != eventTopic("MessagePassed(uint256,address,address,uint256,uint256,bytes,bytes32)") {
			continue
		}
		hash, err := dataWord(log.Data, 3)
		if err != nil {
			continue
		}
		nonce, ok := new(big.Int).SetString(strings.TrimPrefix(log.Topics[1], "0x"), 16)
		if !ok {
			continue
		}
		return "0x" + strings.ToLower(hash), nonce.String()
	}
	return "", ""
}

func parseSourceEvent(chain string, log etherscan.RPCLog) (store.CrossChainLink, bool, error) {
	if len(log.Topics) == 0 {
		return store.CrossChainLink{}, false, nil
	}
	emitter := strings.ToLower(log.Address)
	if (chain == "ethereum" && emitter != EthereumL1StandardBridge) || (chain == "base" && emitter != BaseL2StandardBridge) {
		return store.CrossChainLink{}, false, nil
	}
	link := store.CrossChainLink{SourceChain: chain, TargetChain: "base", SourceChainID: 1, TargetChainID: 8453, Direction: "deposit", BridgeAddress: emitter, Evidence: []string{"source_receipt:" + strings.ToLower(log.LogIndex)}}
	if chain == "base" {
		link.SourceChainID, link.TargetChainID, link.TargetChain, link.Direction = 8453, 1, "ethereum", "withdrawal"
	}
	link.SourceLogIndex, _ = parseHexInt(log.LogIndex)
	switch strings.ToLower(log.Topics[0]) {
	case eventTopic("ETHBridgeInitiated(address,address,uint256,bytes)"):
		if len(log.Topics) != 3 {
			return link, true, ErrMalformedEvent
		}
		amount, err := dataUint(log.Data, 0)
		if err != nil {
			return link, true, err
		}
		link.SourceAddress, link.TargetAddress = topicAddress(log.Topics[1]), topicAddress(log.Topics[2])
		link.SourceAsset, link.TargetAsset, link.SourceAmount, link.TargetAmount = "ETH", "ETH", amount, amount
	case eventTopic("ERC20BridgeInitiated(address,address,address,address,uint256,bytes)"):
		if len(log.Topics) != 4 {
			return link, true, ErrMalformedEvent
		}
		to, err := dataAddress(log.Data, 0)
		if err != nil {
			return link, true, err
		}
		amount, err := dataUint(log.Data, 1)
		if err != nil {
			return link, true, err
		}
		link.SourceAsset, link.TargetAsset = topicAddress(log.Topics[1]), topicAddress(log.Topics[2])
		link.SourceAddress, link.TargetAddress = topicAddress(log.Topics[3]), to
		link.SourceAmount, link.TargetAmount = amount, amount
	default:
		return store.CrossChainLink{}, false, nil
	}
	if link.SourceAddress == "" || link.TargetAddress == "" {
		return link, true, ErrMalformedEvent
	}
	return link, true, nil
}

func parseTargetEvent(link store.CrossChainLink, log chainrpc.Log) (chainrpc.Log, bool, error) {
	if len(log.Topics) == 0 || strings.ToLower(log.Address) != strings.ToLower(map[string]string{"deposit": BaseL2StandardBridge, "withdrawal": EthereumL1StandardBridge}[link.Direction]) {
		return log, false, nil
	}
	if link.SourceAsset == "ETH" {
		if strings.ToLower(log.Topics[0]) != finalizedTopic(true) || len(log.Topics) != 3 {
			return log, false, nil
		}
		amount, err := dataUint(log.Data, 0)
		if err != nil {
			return log, false, err
		}
		return log, topicAddress(log.Topics[1]) == link.SourceAddress && topicAddress(log.Topics[2]) == link.TargetAddress && amount == link.SourceAmount, nil
	}
	if strings.ToLower(log.Topics[0]) != finalizedTopic(false) || len(log.Topics) != 4 {
		return log, false, nil
	}
	to, err := dataAddress(log.Data, 0)
	if err != nil {
		return log, false, err
	}
	amount, err := dataUint(log.Data, 1)
	if err != nil {
		return log, false, err
	}
	return log, topicAddress(log.Topics[1]) == link.SourceAsset && topicAddress(log.Topics[2]) == link.TargetAsset && topicAddress(log.Topics[3]) == link.SourceAddress && to == link.TargetAddress && amount == link.SourceAmount, nil
}

func applyTarget(link *store.CrossChainLink, log chainrpc.Log) {
	link.TargetTxHash = strings.ToLower(log.TransactionHash)
	link.TargetLogIndex, _ = parseHexInt(log.LogIndex)
	link.TargetBlock, _ = parseHexInt(log.BlockNumber)
	link.Status, link.EvidenceLevel, link.NextCheckAt = "completed", "confirmed", time.Time{}
	link.RetryCount, link.LastErrorCode = 0, ""
	link.Evidence = append(link.Evidence, "target_receipt:"+link.TargetTxHash)
}

func identityKey(link store.CrossChainLink) string {
	if link.MessageHash != "" {
		return strings.Join([]string{ProtocolOfficialOPStack, link.SourceChain, strings.ToLower(link.MessageHash)}, ":")
	}
	return strings.Join([]string{ProtocolOfficialOPStack, link.SourceChain, strings.ToLower(link.SourceTxHash), strconv.FormatInt(link.SourceLogIndex, 10), link.TargetChain}, ":")
}

func finalizedTopic(eth bool) string {
	if eth {
		return eventTopic("ETHBridgeFinalized(address,address,uint256,bytes)")
	}
	return eventTopic("ERC20BridgeFinalized(address,address,address,address,uint256,bytes)")
}

func eventTopic(signature string) string {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write([]byte(signature))
	return fmt.Sprintf("0x%x", h.Sum(nil))
}
func topicAddress(value string) string {
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	if len(value) != 64 {
		return ""
	}
	return "0x" + value[24:]
}
func dataAddress(data string, word int) (string, error) {
	value, err := dataWord(data, word)
	if err != nil {
		return "", err
	}
	return topicAddress("0x" + value), nil
}
func dataUint(data string, word int) (string, error) {
	value, err := dataWord(data, word)
	if err != nil {
		return "", err
	}
	parsed, ok := new(big.Int).SetString(value, 16)
	if !ok {
		return "", ErrMalformedEvent
	}
	return parsed.String(), nil
}
func dataWord(data string, word int) (string, error) {
	value := strings.TrimPrefix(data, "0x")
	start := word * 64
	if start < 0 || len(value) < start+64 {
		return "", ErrMalformedEvent
	}
	return value[start : start+64], nil
}
func parseHexInt(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(value, "0x"), 16, 64)
}

func statusRank(status string) int {
	return map[string]int{"initiated": 1, "proven": 2, "finalized": 3, "completed": 4, "confirmed": 4, "failed": 5, "ambiguous": 5}[status]
}
