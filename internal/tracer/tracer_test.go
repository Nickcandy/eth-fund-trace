package tracer

import (
	"context"
	"errors"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

type fakeRepository struct {
	addresses map[string]store.Address
	transfers []store.Transfer
	labels    map[string][]store.Label
	calls     []store.TransferQuery
	bridges   map[string][]store.CrossChainLink
}

func (r *fakeRepository) FindAddress(_ context.Context, _, address string) (store.Address, bool, error) {
	v, ok := r.addresses[address]
	return v, ok, nil
}
func (r *fakeRepository) QueryTransfers(_ context.Context, q store.TransferQuery) ([]store.Transfer, error) {
	r.calls = append(r.calls, q)
	return r.transfers, nil
}
func (r *fakeRepository) ListLabels(_ context.Context, _, address string) ([]store.Label, error) {
	return r.labels[address], nil
}
func (r *fakeRepository) ListCrossChainLinks(_ context.Context, chain, address string, _ int64) ([]store.CrossChainLink, error) {
	return r.bridges[chain+":"+address], nil
}

func TestTraceBatchesFrontierAndPrunesTerminal(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	terminal := "0x0000000000000000000000000000000000000002"
	next := "0x0000000000000000000000000000000000000003"
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced", LatestSyncedBlock: 100}, terminal: {SyncStatus: "synced", IsTerminal: true, LatestSyncedBlock: 100}, next: {SyncStatus: "synced", LatestSyncedBlock: 100}}, transfers: []store.Transfer{{TxHash: "0x1", From: seed, To: terminal, Asset: "ETH", Amount: "2"}, {TxHash: "0x2", From: terminal, To: next, Asset: "ETH", Amount: "1"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Depth: 3, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || len(result.Edges) != 1 || len(r.calls) != 1 {
		t.Fatalf("result=%+v calls=%d", result, len(r.calls))
	}
}

func TestTraceRejectsInvalidAndUnsyncedQueries(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "discovered"}}}
	if _, err := New(r).Trace(context.Background(), Query{Address: seed, Depth: 6}); err != ErrInvalidQuery {
		t.Fatalf("err=%v", err)
	}
	if _, err := New(r).Trace(context.Background(), Query{Address: seed}); !errors.Is(err, ErrAddressNotSynced) {
		t.Fatalf("err=%v", err)
	}
}

func TestTraceExpandsConfirmedBridgeIntoBase(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	target := "0x0000000000000000000000000000000000000002"
	next := "0x0000000000000000000000000000000000000003"
	link := store.CrossChainLink{SourceChain: "ethereum", SourceAddress: seed, SourceTxHash: "0xsource", TargetChain: "base", TargetAddress: target, TargetTxHash: "0xtarget", Status: "confirmed"}
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced", LatestSyncedBlock: 100}, target: {SyncStatus: "synced", LatestSyncedBlock: 200}, next: {SyncStatus: "synced", LatestSyncedBlock: 200}}, transfers: []store.Transfer{{Chain: "base", TxHash: "0xbase", From: target, To: next, Asset: "ETH", Amount: "1"}}, labels: map[string][]store.Label{}, bridges: map[string][]store.CrossChainLink{"ethereum:" + seed: {link}}}
	result, err := New(r).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 2, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleVersion != "trace-v2" || len(result.BridgeEdges) != 1 || len(result.Edges) != 1 || result.Edges[0].Transfer.Chain != "base" || result.DataThroughBlocks["base"] != 200 {
		t.Fatalf("result=%+v", result)
	}
}

func TestTraceRetainsMintEdgeWithoutExpandingZeroAddress(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	zero := "0x0000000000000000000000000000000000000000"
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced"}}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0xmint", From: zero, To: seed, AssetType: "erc20", Asset: "0x0000000000000000000000000000000000000010", TokenValue: "1"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "in", Depth: 2, TopN: 10})
	if err != nil || len(result.Edges) != 1 || len(result.Nodes) != 2 || !result.Nodes[1].Terminal {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestTraceBothKeepsUpstreamAndDownstreamBranchesDirectional(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	upstream := "0x0000000000000000000000000000000000000002"
	upstreamParent := "0x0000000000000000000000000000000000000003"
	upstreamSibling := "0x0000000000000000000000000000000000000004"
	downstream := "0x0000000000000000000000000000000000000005"
	downstreamChild := "0x0000000000000000000000000000000000000006"
	downstreamSibling := "0x0000000000000000000000000000000000000007"
	addresses := map[string]store.Address{}
	for _, address := range []string{seed, upstream, upstreamParent, upstreamSibling, downstream, downstreamChild, downstreamSibling} {
		addresses[address] = store.Address{SyncStatus: "synced", LatestSyncedBlock: 100}
	}
	r := &fakeRepository{addresses: addresses, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0x01", BlockNumber: 10, From: upstream, To: seed, Asset: "ETH", Amount: "1"},
		{Chain: "ethereum", TxHash: "0x02", BlockNumber: 9, From: upstreamParent, To: upstream, Asset: "ETH", Amount: "1"},
		{Chain: "ethereum", TxHash: "0x03", BlockNumber: 8, From: upstream, To: upstreamSibling, Asset: "ETH", Amount: "1"},
		{Chain: "ethereum", TxHash: "0x04", BlockNumber: 10, From: seed, To: downstream, Asset: "ETH", Amount: "1"},
		{Chain: "ethereum", TxHash: "0x05", BlockNumber: 9, From: downstream, To: downstreamChild, Asset: "ETH", Amount: "1"},
		{Chain: "ethereum", TxHash: "0x06", BlockNumber: 8, From: downstreamSibling, To: downstream, Asset: "ETH", Amount: "1"},
	}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "both", Depth: 2, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, node := range result.Nodes {
		seen[node.Address] = true
	}
	if !seen[upstreamParent] || !seen[downstreamChild] {
		t.Fatalf("expected directional ancestors/descendants, nodes=%+v", result.Nodes)
	}
	if seen[upstreamSibling] || seen[downstreamSibling] {
		t.Fatalf("side branches leaked into trace, nodes=%+v", result.Nodes)
	}
}

func TestTraceBothExpandsSameAddressInBothDirections(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	shared := "0x0000000000000000000000000000000000000002"
	parent := "0x0000000000000000000000000000000000000003"
	child := "0x0000000000000000000000000000000000000004"
	addresses := map[string]store.Address{}
	for _, address := range []string{seed, shared, parent, child} {
		addresses[address] = store.Address{SyncStatus: "synced"}
	}
	r := &fakeRepository{addresses: addresses, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{TxHash: "0x1", BlockNumber: 4, From: shared, To: seed, Asset: "ETH", Amount: "1"},
		{TxHash: "0x2", BlockNumber: 3, From: seed, To: shared, Asset: "ETH", Amount: "1"},
		{TxHash: "0x3", BlockNumber: 2, From: parent, To: shared, Asset: "ETH", Amount: "1"},
		{TxHash: "0x4", BlockNumber: 1, From: shared, To: child, Asset: "ETH", Amount: "1"},
	}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "both", Depth: 2, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, node := range result.Nodes {
		seen[node.Address] = true
	}
	if !seen[parent] || !seen[child] {
		t.Fatalf("both directional states were not expanded: %+v", result.Nodes)
	}
}

func TestBridgeTargetRespectsBranchDirection(t *testing.T) {
	link := store.CrossChainLink{SourceChain: "ethereum", SourceAddress: "0x1", TargetChain: "base", TargetAddress: "0x2"}
	if _, ok := bridgeTarget(link, Node{Chain: "ethereum", Address: "0x1"}, "in"); ok {
		t.Fatal("incoming branch followed an outgoing bridge")
	}
	if _, ok := bridgeTarget(link, Node{Chain: "base", Address: "0x2"}, "out"); ok {
		t.Fatal("outgoing branch followed an incoming bridge")
	}
	if target, ok := bridgeTarget(link, Node{Chain: "ethereum", Address: "0x1"}, "out"); !ok || target.Address != "0x2" {
		t.Fatalf("target=%+v ok=%v", target, ok)
	}
}
