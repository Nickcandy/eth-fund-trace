package tracer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestTraceUsesConcreteTransfersAndFiltersSmallAmounts(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	a := "0x0000000000000000000000000000000000000002"
	b := "0x0000000000000000000000000000000000000003"
	c := "0x0000000000000000000000000000000000000004"
	d := "0x0000000000000000000000000000000000000005"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: {SyncStatus: "synced"}, a: {SyncStatus: "synced"}, b: {SyncStatus: "synced"}, c: {SyncStatus: "synced"}, d: {SyncStatus: "synced"},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xa1", BlockNumber: 10, BlockTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), From: seed, To: a, AssetType: "eth", Asset: "ETH", Amount: "400000000000000000"},
		{Chain: "ethereum", TxHash: "0xa2", BlockNumber: 12, BlockTime: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), From: seed, To: a, AssetType: "eth", Asset: "ETH", Amount: "300000000000000000"},
		{Chain: "ethereum", TxHash: "0xa3", BlockNumber: 11, BlockTime: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC), From: seed, To: a, AssetType: "eth", Asset: "ETH", Amount: "200000000000000000"},
		{Chain: "ethereum", TxHash: "0xb1", From: seed, To: b, AssetType: "eth", Asset: "ETH", Amount: "500000000000000000"},
		{Chain: "ethereum", TxHash: "0xc1", From: seed, To: c, AssetType: "eth", Asset: "ETH", Amount: "100000000000000000"},
		{Chain: "ethereum", TxHash: "0xd1", From: seed, To: d, AssetType: "eth", Asset: "ETH", Amount: "5000000000000000"},
	}}

	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 5 || len(result.Nodes) != 4 {
		t.Fatalf("edges=%+v nodes=%+v, want five concrete transfers and no small recipient", result.Edges, result.Nodes)
	}
	if result.Edges[0].TransferCount != 1 || result.Edges[0].TotalAmount != "500000000000000000" {
		t.Fatalf("first edge=%+v, want one concrete transfer", result.Edges[0])
	}
	foundSmall := false
	for _, transfer := range result.MoneyTransfers {
		foundSmall = foundSmall || transfer.StopReason == store.StopSmallAmount
	}
	if !foundSmall {
		t.Fatalf("money transfers=%+v, want small amount terminal", result.MoneyTransfers)
	}
}

func TestTraceRootIncludesOfficialEthereumTokensAndRejectsSymbolSpoof(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	ethRecipient := "0x0000000000000000000000000000000000000002"
	usdtRecipient := "0x0000000000000000000000000000000000000003"
	spoofRecipient := "0x0000000000000000000000000000000000000004"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: {SyncStatus: "synced"}, ethRecipient: {SyncStatus: "synced"}, usdtRecipient: {SyncStatus: "synced"}, spoofRecipient: {SyncStatus: "synced"},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xeth", From: seed, To: ethRecipient, AssetType: "eth", Asset: "ETH", Symbol: "ETH", Decimals: 18, Amount: "1000000000000000000"},
		{Chain: "ethereum", TxHash: "0xusdt", From: seed, To: usdtRecipient, AssetType: "erc20", Asset: ethereumUSDT, Symbol: "USDT", Decimals: 6, TokenMetadataComplete: true, TokenValue: "1000000000000"},
		{Chain: "ethereum", TxHash: "0xspoof", From: seed, To: spoofRecipient, AssetType: "erc20", Asset: "0x0000000000000000000000000000000000000099", Symbol: "USDT", Decimals: 6, TokenMetadataComplete: true, TokenValue: "9999999999999"},
	}}

	result, err := New(r).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 {
		t.Fatalf("edges=%+v, want ETH and official USDT only", result.Edges)
	}
	var usdtEdge *Edge
	for index := range result.Edges {
		if result.Edges[index].Asset == ethereumUSDT {
			usdtEdge = &result.Edges[index]
		}
	}
	if usdtEdge == nil || usdtEdge.TotalAmount != "1000000000000" || usdtEdge.Symbol != "USDT" || usdtEdge.Decimals != 6 {
		t.Fatalf("USDT edge=%+v", usdtEdge)
	}
	if len(r.calls) != 1 || r.calls[0].AssetMode != "all" {
		t.Fatalf("root queries=%+v, want one concrete all-asset query", r.calls)
	}
}

func TestRootAssetsAreScopedByChain(t *testing.T) {
	ethereum := rootAssets("ethereum")
	if len(ethereum) != 5 || ethereum[0].Asset != "ETH" || ethereum[3].Asset != ethereumUSDT {
		t.Fatalf("ethereum root assets=%+v", ethereum)
	}
	base := rootAssets("base")
	if len(base) != 3 || base[0].Asset != "ETH" || base[1].Asset != baseUSDC || base[2].Asset != baseWETH {
		t.Fatalf("base root assets=%+v", base)
	}
}

type fakeRepository struct {
	addresses  map[string]store.Address
	identities map[string]store.AddressIdentity
	transfers  []store.Transfer
	labels     map[string][]store.Label
	calls      []store.TransferQuery
	bridges    map[string][]store.CrossChainLink
}

type addressInspectorStub struct {
	identity store.AddressIdentity
}

func (s addressInspectorStub) InspectAddress(context.Context, string, string) (store.AddressIdentity, error) {
	return s.identity, nil
}

type failingAddressInspector struct{}

func completeAddress(from, to int64) store.Address {
	return store.Address{
		SyncStatus: "synced", EarliestSyncedBlock: from, LatestSyncedBlock: to,
		NormalSyncedFrom: from, NormalSyncedTo: to, InternalSyncedFrom: from,
		InternalSyncedTo: to, TokenSyncedFrom: from, TokenSyncedTo: to,
	}
}

func (failingAddressInspector) InspectAddress(context.Context, string, string) (store.AddressIdentity, error) {
	return store.AddressIdentity{}, errors.New("unexpected upstream address inspection")
}

type transactionAnalyzerStub struct {
	analysis store.TransactionAnalysis
}

type transactionAnalyzerMap struct {
	contract string
	analyses map[string]store.TransactionAnalysis
	calls    []string
}

func (s *transactionAnalyzerMap) Analyze(_ context.Context, _, txHash string) (store.TransactionAnalysis, error) {
	s.calls = append(s.calls, txHash)
	return s.analyses[txHash], nil
}

func (s *transactionAnalyzerMap) SupportsContract(address string) bool { return address == s.contract }

func (s transactionAnalyzerStub) Analyze(context.Context, string, string) (store.TransactionAnalysis, error) {
	return s.analysis, nil
}

func (s transactionAnalyzerStub) SupportsContract(address string) bool {
	return address == s.analysis.To || address == s.analysis.EntryContract
}

func (r *fakeRepository) FindAddress(_ context.Context, _, address string) (store.Address, bool, error) {
	v, ok := r.addresses[address]
	if ok && v.SyncStatus == "synced" && v.NormalSyncedTo == 0 && v.InternalSyncedTo == 0 && v.TokenSyncedTo == 0 {
		v.NormalSyncedFrom, v.InternalSyncedFrom, v.TokenSyncedFrom = v.EarliestSyncedBlock, v.EarliestSyncedBlock, v.EarliestSyncedBlock
		v.NormalSyncedTo, v.InternalSyncedTo, v.TokenSyncedTo = v.LatestSyncedBlock, v.LatestSyncedBlock, v.LatestSyncedBlock
	}
	return v, ok, nil
}

func (r *fakeRepository) SetAddressIdentity(_ context.Context, _, address string, identity store.AddressIdentity) error {
	if r.identities == nil {
		r.identities = make(map[string]store.AddressIdentity)
	}
	r.identities[address] = identity
	return nil
}
func (r *fakeRepository) QueryTransfers(_ context.Context, q store.TransferQuery) ([]store.Transfer, error) {
	r.calls = append(r.calls, q)
	result := make([]store.Transfer, 0, len(r.transfers))
	for _, transfer := range r.transfers {
		connected := false
		for _, address := range q.Addresses {
			if (q.Direction == "in" && transfer.To == address) || (q.Direction == "out" && transfer.From == address) || (q.Direction == "both" && (transfer.From == address || transfer.To == address)) {
				connected = true
				break
			}
		}
		if !connected {
			continue
		}
		if q.FromBlock > 0 && transfer.BlockNumber < q.FromBlock || q.ToBlock > 0 && transfer.BlockNumber > q.ToBlock {
			continue
		}
		switch q.AssetMode {
		case "eth":
			if transfer.AssetType != "eth" && transfer.Asset != "ETH" {
				continue
			}
		case "erc20":
			if transfer.AssetType != "erc20" {
				continue
			}
		case "contract":
			if transfer.Asset != q.Asset {
				continue
			}
		}
		result = append(result, transfer)
	}
	return result, nil
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
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced", LatestSyncedBlock: 100}, terminal: {SyncStatus: "synced", IsTerminal: true, LatestSyncedBlock: 100}, next: {SyncStatus: "synced", LatestSyncedBlock: 100}}, transfers: []store.Transfer{{TxHash: "0x1", From: seed, To: terminal, Asset: "ETH", Amount: "2000000000000000000"}, {TxHash: "0x2", From: terminal, To: next, Asset: "ETH", Amount: "1000000000000000000"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || len(result.Edges) != 1 || len(r.calls) != 2 {
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

func TestTraceRejectsRecordLimitedAddressAsIncomplete(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: {SyncStatus: "partial", SyncError: "record_limit", SyncMaxRecordsPerAction: 100_000, LatestSyncedBlock: 100},
	}, labels: map[string][]store.Label{}}

	_, err := New(r).WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1})
	if !errors.Is(err, ErrAddressNotSynced) {
		t.Fatalf("err=%v, want incomplete address", err)
	}
}

func TestTraceExistingDataOnlySkipsCoverageAndAddressInspection(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	neighbor := "0x0000000000000000000000000000000000000002"
	r := &fakeRepository{
		addresses: map[string]store.Address{
			seed:     {SyncStatus: "failed", LatestSyncedBlock: 100},
			neighbor: {SyncStatus: "discovered", AddressType: "unknown"},
		},
		transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0x1", BlockNumber: 90, From: seed, To: neighbor, AssetType: "eth", Asset: "ETH", Amount: "1000000000000000000"}},
		labels:    map[string][]store.Label{},
	}

	result, err := New(r).
		WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).
		WithAddressInspector(failingAddressInspector{}).
		WithExistingDataOnly(true).
		Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.DataStatus != "partial" || len(result.Edges) != 1 || len(result.Nodes) != 2 {
		t.Fatalf("result=%+v", result)
	}
}

func TestTraceRequiresDependencyHistoryCoverage(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	dependency := "0x0000000000000000000000000000000000000002"
	r := &fakeRepository{
		addresses: map[string]store.Address{
			seed:       completeAddress(50, 100),
			dependency: {SyncStatus: "synced", NormalSyncedFrom: 90, NormalSyncedTo: 100, InternalSyncedFrom: 50, InternalSyncedTo: 100, TokenSyncedFrom: 50, TokenSyncedTo: 100, LatestSyncedBlock: 100},
		},
		transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0x1", BlockNumber: 95, From: seed, To: dependency, AssetType: "eth", Asset: "ETH", Amount: "1000000000000000000"}},
		labels:    map[string][]store.Label{},
	}

	_, err := New(r).WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	var unsynced AddressNotSyncedError
	if !errors.As(err, &unsynced) || unsynced.Address != dependency {
		t.Fatalf("error = %v, want dependency %s to require history sync", err, dependency)
	}
}

func TestTraceRequiresDependencyCurrentCoverage(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	dependency := "0x0000000000000000000000000000000000000002"
	r := &fakeRepository{
		addresses: map[string]store.Address{
			seed:       completeAddress(50, 100),
			dependency: {SyncStatus: "synced", NormalSyncedFrom: 50, NormalSyncedTo: 90, InternalSyncedFrom: 50, InternalSyncedTo: 90, TokenSyncedFrom: 50, TokenSyncedTo: 90, LatestSyncedBlock: 90},
		},
		transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0x1", BlockNumber: 80, From: seed, To: dependency, AssetType: "eth", Asset: "ETH", Amount: "1000000000000000000"}},
		labels:    map[string][]store.Label{},
	}

	_, err := New(r).WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	var unsynced AddressNotSyncedError
	if !errors.As(err, &unsynced) || unsynced.Address != dependency {
		t.Fatalf("error = %v, want dependency %s to require current sync", err, dependency)
	}
}

func TestTraceInspectsContractBeforeRequiringDependencyHistory(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	executor := "0x6e4141d33021b52c91c28608403db4a0ffb50ec6"
	r := &fakeRepository{
		addresses: map[string]store.Address{
			seed: func() store.Address { value := completeAddress(50, 100); value.AddressType = "eoa"; return value }(),
		},
		transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0x1", BlockNumber: 95, From: seed, To: executor, AssetType: "eth", Asset: "ETH", Amount: "1000000000000000000"}},
		labels:    map[string][]store.Label{},
	}
	inspector := addressInspectorStub{identity: store.AddressIdentity{AddressType: "contract", Protocol: "kyberswap", Roles: []string{"kyberswap_executor"}}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{Chain: "ethereum", TxHash: "0x1", To: executor, Succeeded: true, Quality: store.AnalysisQuality{Status: "partial"}}}

	result, err := New(r).
		WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).
		WithAddressInspector(inspector).
		WithTransactionAnalyzer(analyzer).
		Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || result.Nodes[1].Address != executor || result.Nodes[1].AddressType != "contract" || result.Nodes[1].Protocol != "kyberswap" || !slices.Equal(result.Nodes[1].Roles, []string{"kyberswap_executor"}) {
		t.Fatalf("nodes=%+v", result.Nodes)
	}
	if got := r.identities[executor]; got.AddressType != "contract" {
		t.Fatalf("persisted identity=%+v", got)
	}
}

func TestTraceStopsAtConfirmedBridge(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	bridge := "0x0000000000000000000000000000000000000002"
	target := "0x0000000000000000000000000000000000000003"
	next := "0x0000000000000000000000000000000000000003"
	link := store.CrossChainLink{SourceChain: "ethereum", SourceAddress: bridge, SourceTxHash: "0xsource", TargetChain: "base", TargetAddress: target, TargetTxHash: "0xtarget", Status: "confirmed"}
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced", LatestSyncedBlock: 100}, bridge: {SyncStatus: "synced", LatestSyncedBlock: 100}, target: {SyncStatus: "synced", LatestSyncedBlock: 200}, next: {SyncStatus: "synced", LatestSyncedBlock: 200}}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0xsource", BlockNumber: 90, From: seed, To: bridge, Asset: "ETH", Amount: "1000000000000000000"}, {Chain: "base", TxHash: "0xbase", From: target, To: next, Asset: "ETH", Amount: "1000000000000000000"}}, labels: map[string][]store.Label{}, bridges: map[string][]store.CrossChainLink{"ethereum:" + bridge: {link}}}
	result, err := New(r).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleVersion != traceRuleVersion || len(result.BridgeEdges) != 1 || len(result.Edges) != 1 || result.DataThroughBlocks["base"] != 0 {
		t.Fatalf("result=%+v", result)
	}
	if !result.Nodes[1].Terminal || result.Nodes[1].StopReason != "cross_chain_bridge" {
		t.Fatalf("bridge node=%+v", result.Nodes[1])
	}
}

func TestTraceRootExcludesTokenMint(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	zero := "0x0000000000000000000000000000000000000000"
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced"}}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0xmint", From: zero, To: seed, AssetType: "erc20", Asset: "0x0000000000000000000000000000000000000010", TokenValue: "1"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "in", Depth: 2})
	if err != nil || len(result.Edges) != 0 || len(result.Nodes) != 1 {
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
		{Chain: "ethereum", TxHash: "0x01", BlockNumber: 10, From: upstream, To: seed, Asset: "ETH", Amount: "1000000000000000000"},
		{Chain: "ethereum", TxHash: "0x02", BlockNumber: 9, From: upstreamParent, To: upstream, Asset: "ETH", Amount: "1000000000000000000"},
		{Chain: "ethereum", TxHash: "0x03", BlockNumber: 8, From: upstream, To: upstreamSibling, Asset: "ETH", Amount: "1000000000000000000"},
		{Chain: "ethereum", TxHash: "0x04", BlockNumber: 10, From: seed, To: downstream, Asset: "ETH", Amount: "1000000000000000000"},
		{Chain: "ethereum", TxHash: "0x05", BlockNumber: 11, From: downstream, To: downstreamChild, Asset: "ETH", Amount: "1000000000000000000"},
		{Chain: "ethereum", TxHash: "0x06", BlockNumber: 8, From: downstreamSibling, To: downstream, Asset: "ETH", Amount: "1000000000000000000"},
	}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "both", Depth: 2})
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

func TestTraceUsesMonotonicBlockWindows(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	upstream := "0x0000000000000000000000000000000000000002"
	validParent := "0x0000000000000000000000000000000000000003"
	lateParent := "0x0000000000000000000000000000000000000004"
	downstream := "0x0000000000000000000000000000000000000005"
	validChild := "0x0000000000000000000000000000000000000006"
	earlyChild := "0x0000000000000000000000000000000000000007"
	addresses := map[string]store.Address{}
	for _, address := range []string{seed, upstream, validParent, lateParent, downstream, validChild, earlyChild} {
		addresses[address] = store.Address{SyncStatus: "synced", LatestSyncedBlock: 100}
	}
	r := &fakeRepository{addresses: addresses, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{TxHash: "0xin", BlockNumber: 10, From: upstream, To: seed, Asset: "ETH", Amount: "1000000000000000000"},
		{TxHash: "0xvalid-parent", BlockNumber: 9, From: validParent, To: upstream, Asset: "ETH", Amount: "1000000000000000000"},
		{TxHash: "0xlate-parent", BlockNumber: 11, From: lateParent, To: upstream, Asset: "ETH", Amount: "100000000000000000000"},
		{TxHash: "0xout", BlockNumber: 10, From: seed, To: downstream, Asset: "ETH", Amount: "1000000000000000000"},
		{TxHash: "0xvalid-child", BlockNumber: 11, From: downstream, To: validChild, Asset: "ETH", Amount: "1000000000000000000"},
		{TxHash: "0xearly-child", BlockNumber: 9, From: downstream, To: earlyChild, Asset: "ETH", Amount: "100000000000000000000"},
	}}

	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "both", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, node := range result.Nodes {
		seen[node.Address] = true
	}
	if !seen[validParent] || !seen[validChild] || seen[lateParent] || seen[earlyChild] {
		t.Fatalf("nodes=%+v", result.Nodes)
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
		{TxHash: "0x1", BlockNumber: 4, From: shared, To: seed, Asset: "ETH", Amount: "1000000000000000000"},
		{TxHash: "0x2", BlockNumber: 3, From: seed, To: shared, Asset: "ETH", Amount: "1000000000000000000"},
		{TxHash: "0x3", BlockNumber: 2, From: parent, To: shared, Asset: "ETH", Amount: "1000000000000000000"},
		{TxHash: "0x4", BlockNumber: 5, From: shared, To: child, Asset: "ETH", Amount: "1000000000000000000"},
	}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "both", Depth: 2})
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

func TestTraceRootKeepsConcreteTransfers(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	large := "0x0000000000000000000000000000000000000002"
	small := "0x0000000000000000000000000000000000000003"
	spam := "0x0000000000000000000000000000000000000004"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:  {SyncStatus: "synced"},
		large: {SyncStatus: "synced"},
		small: {SyncStatus: "synced"},
		spam:  {SyncStatus: "synced"},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{TxHash: "0xnew", BlockNumber: 3, From: seed, To: small, AssetType: "eth", Asset: "ETH", Amount: "2000000000000000000"},
		{TxHash: "0xold", BlockNumber: 1, From: seed, To: large, AssetType: "eth", Asset: "ETH", Amount: "100000000000000000000"},
		{TxHash: "0xspam", BlockNumber: 4, From: seed, To: spam, AssetType: "erc20", Asset: "0xtoken", TokenValue: "999999"},
	}}

	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 || result.Edges[0].To != large || result.Edges[0].TotalAmount != "100000000000000000000" || result.Edges[1].To != small {
		t.Fatalf("root edges=%+v, want concrete ETH transfers", result.Edges)
	}
	if len(r.calls) != 1 || r.calls[0].AssetMode != "all" {
		t.Fatalf("root query=%+v, want one all-asset query", r.calls)
	}
}

func TestTraceSwitchesAssetOnlyForVerifiedContractConversion(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0x0000000000000000000000000000000000000002"
	recipient := "0x0000000000000000000000000000000000000003"
	token := ethereumUSDT
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:      {SyncStatus: "synced"},
		router:    {SyncStatus: "synced", IsContract: true},
		recipient: {SyncStatus: "synced"},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xswap", BlockNumber: 10, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: "10000000000000000000"},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xswap", BlockNumber: 10, From: seed, To: router, Succeeded: true,
		FinalOutputAddress: recipient,
		Swaps:              []store.SwapEvent{{Verified: true, TokenIn: ethereumWETH, TokenOut: token, AmountIn: "10000000000000000000", AmountOut: "250000000"}},
		Wraps:              []store.WrapEvent{{Type: "deposit", Account: seed, Amount: "10000000000000000000"}},
		Quality:            store.AnalysisQuality{Status: "complete", AmbiguousRoute: false},
	}}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 {
		t.Fatalf("edges=%+v, want ETH edge and verified conversion edge", result.Edges)
	}
	conversion := result.Edges[1]
	if conversion.Kind != "swap" || conversion.Asset != token || conversion.From != router || conversion.To != recipient || conversion.TotalAmount != "250000000" {
		t.Fatalf("conversion=%+v", conversion)
	}
}

func TestTraceBuildsKyberSwapEdgeWithBoundedEvidence(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	executor := "0x6e4141d33021b52c91c28608403db4a0ffb50ec6"
	recipient := "0x0000000000000000000000000000000000000003"
	provider := "0x67336cec42645f55059eff241cb02ea5cc52ff86"
	usdt := "0xdac17f958d2ee523a2206206994597c13d831ec7"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:      {SyncStatus: "synced", AddressType: "eoa"},
		executor:  {AddressType: "contract", IsContract: true, Protocol: "kyberswap", Roles: []string{"kyberswap_executor"}},
		recipient: {SyncStatus: "synced", AddressType: "eoa"},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xswap", BlockNumber: 10, From: seed, To: executor, AssetType: "erc20", Asset: usdt, TokenValue: "1000000000000"},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xswap", From: seed, To: executor, Succeeded: true, FinalOutputAddress: recipient,
		Conversions: []store.SwapConversion{{Protocol: "kyberswap", Version: "rfq", Status: "complete", Initiator: seed, Router: "0xrouter", Executor: executor, LiquidityProvider: provider, Recipient: recipient, TokenIn: usdt, AmountIn: "1000000000000", TokenOut: "ETH", AmountOut: "274823886000000000000", Evidence: []string{"receipt transfers", "internal ETH calls"}}},
		Quality:     store.AnalysisQuality{Status: "complete"},
	}}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 || result.Edges[1].Kind != "swap" || result.Edges[1].Asset != "ETH" || result.Edges[1].TotalAmount != "274823886000000000000" {
		t.Fatalf("edges=%+v", result.Edges)
	}
	if len(result.Edges[1].ConversionEvidence) != 1 || result.Edges[1].ConversionEvidence[0].TxHash != "0xswap" || result.Edges[1].ConversionEvidence[0].Protocol != "kyberswap" || result.Edges[1].ConversionEvidence[0].LiquidityProvider != provider {
		t.Fatalf("evidence=%+v", result.Edges[1].ConversionEvidence)
	}
}

func TestTraceAnalyzesEachAnchoredContractTransaction(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0x0000000000000000000000000000000000000002"
	recipient := "0x0000000000000000000000000000000000000003"
	token := ethereumUSDT
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced"}, router: {SyncStatus: "synced", IsContract: true}, recipient: {SyncStatus: "synced"}}, labels: map[string][]store.Label{}}
	analyzer := &transactionAnalyzerMap{contract: router, analyses: make(map[string]store.TransactionAnalysis)}
	for i := 1; i <= 2; i++ {
		hash := fmt.Sprintf("0x%02d", i)
		amount := fmt.Sprintf("%d000000000000000000", i)
		r.transfers = append(r.transfers, store.Transfer{Chain: "ethereum", TxHash: hash, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: amount})
		analyzer.analyses[hash] = store.TransactionAnalysis{Chain: "ethereum", TxHash: hash, From: seed, To: router, Succeeded: true, FinalOutputAddress: recipient, Swaps: []store.SwapEvent{{Verified: true, TokenIn: ethereumWETH, TokenOut: token, AmountIn: amount, AmountOut: "10000000"}}, Wraps: []store.WrapEvent{{Type: "deposit", Account: seed, Amount: amount}}, Quality: store.AnalysisQuality{Status: "complete"}}
	}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(analyzer.calls) != 2 || len(result.Edges) != 4 {
		t.Fatalf("calls=%d edges=%+v", len(analyzer.calls), result.Edges)
	}
	for _, edge := range result.Edges {
		if edge.TransferCount != 1 {
			t.Fatalf("edge=%+v, want concrete transaction", edge)
		}
	}
}

func TestTraceStopsAtUnsupportedContract(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	contract := "0x0000000000000000000000000000000000000002"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:     {SyncStatus: "synced", AddressType: "eoa"},
		contract: {AddressType: "contract", IsContract: true},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xcall", BlockNumber: 10, From: seed, To: contract, AssetType: "eth", Asset: "ETH", Amount: "10000000000000000000"},
	}}

	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || !result.Nodes[1].Terminal || result.Nodes[1].StopReason != "unsupported_contract" {
		t.Fatalf("nodes=%+v", result.Nodes)
	}
	if len(r.calls) != 1 {
		t.Fatalf("contract was expanded: calls=%+v", r.calls)
	}
}

func TestTraceDoesNotSwitchAssetForUnverifiedContractConversion(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0x0000000000000000000000000000000000000002"
	token := "0x0000000000000000000000000000000000000010"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:   {SyncStatus: "synced"},
		router: {SyncStatus: "synced", IsContract: true},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xswap", BlockNumber: 10, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: "10000000000000000000"},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xswap", From: seed, To: router, Succeeded: true,
		Swaps:   []store.SwapEvent{{Verified: false, TokenIn: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2", TokenOut: token, AmountIn: "10", AmountOut: "250"}},
		Wraps:   []store.WrapEvent{{Type: "deposit", Account: seed, Amount: "10000000000000000000"}},
		Quality: store.AnalysisQuality{Status: "complete"},
	}}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 1 || result.Edges[0].Asset != "ETH" {
		t.Fatalf("unverified conversion changed asset: %+v", result.Edges)
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

func TestAddressCoveredRequiresEveryActionRange(t *testing.T) {
	graph := New(&fakeRepository{}).WithRequiredStartBlocks(map[string]int64{"ethereum": 50})
	complete := completeAddress(50, 100)
	if !graph.addressCovered("ethereum", complete, 100) {
		t.Fatal("complete action coverage was rejected")
	}
	for _, missing := range []string{"normal", "internal", "token"} {
		address := complete
		switch missing {
		case "normal":
			address.NormalSyncedFrom = 51
		case "internal":
			address.InternalSyncedTo = 99
		case "token":
			address.TokenSyncedFrom = 51
		}
		if graph.addressCovered("ethereum", address, 100) {
			t.Fatalf("missing %s coverage was accepted", missing)
		}
	}
}

func TestFIFOReconciliationReportsUnexplainedOutgoing(t *testing.T) {
	states := []store.MoneyState{
		{Address: "0xa", Asset: "ETH", Direction: "in", Amount: "10", RemainingAmount: "10", EntryBlock: 1},
		{Address: "0xa", Asset: "ETH", Direction: "out", Amount: "15", RemainingAmount: "15", EntryBlock: 2},
	}
	consumeMoneyStates(states)
	ledgers := buildLedgers(states)
	if len(ledgers) != 1 || states[0].RemainingAmount != "0" || states[1].RemainingAmount != "5" {
		t.Fatalf("states=%+v ledgers=%+v", states, ledgers)
	}
	ledger := ledgers[0]
	if ledger.ExplainedAmount != "10" || ledger.UnexplainedAmount != "5" || ledger.OpeningAmount != "5" || ledger.Status != "partial" {
		t.Fatalf("ledger=%+v", ledger)
	}
}

func TestTraceDownstreamCannotExpandBeyondEnteringAmount(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	middle := "0x0000000000000000000000000000000000000002"
	first := "0x0000000000000000000000000000000000000003"
	second := "0x0000000000000000000000000000000000000004"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: {SyncStatus: "synced"}, middle: {SyncStatus: "synced"}, first: {SyncStatus: "synced"}, second: {SyncStatus: "synced"},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xin", BlockNumber: 1, From: seed, To: middle, AssetType: "eth", Asset: "ETH", Amount: "1000000000000000000"},
		{Chain: "ethereum", TxHash: "0xfirst", BlockNumber: 2, From: middle, To: first, AssetType: "eth", Asset: "ETH", Amount: "600000000000000000"},
		{Chain: "ethereum", TxHash: "0xsecond", BlockNumber: 3, From: middle, To: second, AssetType: "eth", Asset: "ETH", Amount: "600000000000000000"},
	}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 3 || result.Edges[1].TotalAmount != "600000000000000000" || result.Edges[2].TotalAmount != "400000000000000000" {
		t.Fatalf("edges=%+v, downstream expansion exceeded entering amount", result.Edges)
	}
}
