package bridge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/chainrpc"
	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

type analyzerSource struct {
	receipts map[string]etherscan.RPCReceipt
	logs     []chainrpc.Log
}

func (s *analyzerSource) TransactionReceipt(_ context.Context, hash string) (etherscan.RPCReceipt, error) {
	value, ok := s.receipts[hash]
	if !ok {
		return etherscan.RPCReceipt{}, chainrpc.ErrPending
	}
	return value, nil
}
func (s *analyzerSource) Logs(context.Context, chainrpc.LogFilter) ([]chainrpc.Log, error) {
	return s.logs, nil
}

type analyzerRepository struct {
	links map[string]store.CrossChainLink
}

func (r *analyzerRepository) UpsertCrossChainLink(_ context.Context, link store.CrossChainLink) (store.CrossChainLink, error) {
	if r.links == nil {
		r.links = map[string]store.CrossChainLink{}
	}
	r.links[link.IdentityKey] = link
	return link, nil
}
func (r *analyzerRepository) QueryCrossChainLinks(context.Context, store.BridgeLinkQuery) ([]store.CrossChainLink, error) {
	result := make([]store.CrossChainLink, 0, len(r.links))
	for _, link := range r.links {
		result = append(result, link)
	}
	return result, nil
}
func (r *analyzerRepository) HasTargetTransferEvidence(context.Context, string, string, string, string, string) (bool, error) {
	return true, nil
}
func (r *analyzerRepository) HasSourceTransferEvidence(context.Context, string, string, string, string, string) (bool, error) {
	return true, nil
}

func TestAnalyzerRecognizesETHDepositWithoutClaimingUnlinkedCompletion(t *testing.T) {
	from, to := addressWord("1"), addressWord("2")
	source := &analyzerSource{receipts: map[string]etherscan.RPCReceipt{"0xsource": {TransactionHash: "0xsource", BlockNumber: "0x10", Status: "0x1", Logs: []etherscan.RPCLog{{Address: EthereumL1StandardBridge, Topics: []string{eventTopic("ETHBridgeInitiated(address,address,uint256,bytes)"), from, to}, Data: uintWord(25), LogIndex: "0x3"}}}}, logs: []chainrpc.Log{{Address: BaseL2StandardBridge, Topics: []string{eventTopic("ETHBridgeFinalized(address,address,uint256,bytes)"), from, to}, Data: uintWord(25), LogIndex: "0x4", TransactionHash: "0xtarget", BlockNumber: "0x20"}}}
	repo := &analyzerRepository{}
	analyzer := NewAnalyzer(map[string]BridgeSource{"ethereum": source, "base": source}, repo, time.Now)
	links, err := analyzer.Analyze(context.Background(), "ethereum", "0xsource")
	if err != nil || len(links) != 1 {
		t.Fatalf("links=%+v err=%v", links, err)
	}
	link := links[0]
	if link.Status != "initiated" || link.Direction != "deposit" || link.SourceAsset != "ETH" || link.SourceAmount != "25" || link.TargetTxHash != "" {
		t.Fatalf("link=%+v", link)
	}
}

func TestAnalyzerRecognizesERC20WithdrawalAndUnknownContract(t *testing.T) {
	local, remote, from, to := addressWord("a"), addressWord("b"), addressWord("1"), addressWord("2")
	source := &analyzerSource{receipts: map[string]etherscan.RPCReceipt{
		"0xwithdraw": {TransactionHash: "0xwithdraw", BlockNumber: "0x30", Status: "0x1", Logs: []etherscan.RPCLog{{Address: BaseL2StandardBridge, Topics: []string{eventTopic("ERC20BridgeInitiated(address,address,address,address,uint256,bytes)"), local, remote, from}, Data: addressData(to) + uintWord(99)[2:], LogIndex: "0x5"}}},
		"0xunknown":  {TransactionHash: "0xunknown", BlockNumber: "0x30", Status: "0x1", Logs: []etherscan.RPCLog{{Address: "0x0000000000000000000000000000000000000099", Topics: []string{eventTopic("ETHBridgeInitiated(address,address,uint256,bytes)"), from, to}, Data: uintWord(1), LogIndex: "0x0"}}},
	}}
	analyzer := NewAnalyzer(map[string]BridgeSource{"ethereum": source, "base": source}, &analyzerRepository{}, time.Now)
	links, err := analyzer.Analyze(context.Background(), "base", "0xwithdraw")
	if err != nil || len(links) != 1 || links[0].Direction != "withdrawal" || links[0].SourceAsset != "0x000000000000000000000000000000000000000a" || links[0].TargetAsset != "0x000000000000000000000000000000000000000b" {
		t.Fatalf("links=%+v err=%v", links, err)
	}
	links, err = analyzer.Analyze(context.Background(), "base", "0xunknown")
	if err != nil || len(links) != 0 {
		t.Fatalf("unknown links=%+v err=%v", links, err)
	}
}

func TestTargetERC20EventReversesLocalAndRemoteTokens(t *testing.T) {
	link := store.CrossChainLink{Direction: "withdrawal", SourceAsset: "0x000000000000000000000000000000000000000a", TargetAsset: "0x000000000000000000000000000000000000000b", SourceAddress: "0x0000000000000000000000000000000000000001", TargetAddress: "0x0000000000000000000000000000000000000002", SourceAmount: "99"}
	log := chainrpc.Log{Address: EthereumL1StandardBridge, Topics: []string{eventTopic("ERC20BridgeFinalized(address,address,address,address,uint256,bytes)"), addressWord("b"), addressWord("a"), addressWord("1")}, Data: addressWord("2") + uintWord(99)[2:]}
	if _, ok, err := parseTargetEvent(link, log); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func addressWord(hex string) string  { return "0x" + fmt.Sprintf("%064s", hex) }
func addressData(word string) string { return word }
func uintWord(value int) string      { return fmt.Sprintf("0x%064x", value) }
