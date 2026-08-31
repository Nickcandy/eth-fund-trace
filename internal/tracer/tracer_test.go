package tracer

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestTraceUsesConcreteTransfersIncludingSmallAmounts(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	a := "0x0000000000000000000000000000000000000002"
	b := "0x0000000000000000000000000000000000000003"
	c := "0x0000000000000000000000000000000000000004"
	d := "0x0000000000000000000000000000000000000005"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: completeAddress(0, 100), a: completeAddress(0, 100), b: completeAddress(0, 100), c: completeAddress(0, 100), d: completeAddress(0, 100),
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
	if len(result.Edges) != 6 || len(result.Nodes) != 5 {
		t.Fatalf("edges=%+v nodes=%+v, want every positive transfer and recipient", result.Edges, result.Nodes)
	}
	if result.Edges[0].TransferCount != 1 || result.Edges[0].TotalAmount != "500000000000000000" {
		t.Fatalf("first edge=%+v, want one concrete transfer", result.Edges[0])
	}
	for _, transfer := range result.MoneyTransfers {
		if transfer.StopReason != "" {
			t.Fatalf("money transfers=%+v, every positive amount must remain traceable", result.MoneyTransfers)
		}
	}
}

func TestTraceAutoModeContinuesBeyondFiveHopsToTerminal(t *testing.T) {
	addresses := make([]string, 8)
	repository := &fakeRepository{addresses: map[string]store.Address{}, labels: map[string][]store.Label{}}
	for i := range addresses {
		addresses[i] = fmt.Sprintf("0x%040x", i+1)
		repository.addresses[addresses[i]] = completeAddress(0, 100)
		if i > 0 {
			repository.transfers = append(repository.transfers, store.Transfer{
				Chain: "ethereum", TxHash: fmt.Sprintf("0x%d", i), BlockNumber: int64(i),
				From: addresses[i-1], To: addresses[i], AssetType: "eth", Asset: "ETH", Amount: "1",
			})
		}
	}
	terminal := repository.addresses[addresses[len(addresses)-1]]
	terminal.IsTerminal = true
	repository.addresses[addresses[len(addresses)-1]] = terminal

	result, err := New(repository).Trace(context.Background(), Query{Address: addresses[0], Direction: "out", Depth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 7 || len(result.Nodes) != 8 || !result.Nodes[7].Terminal {
		t.Fatalf("result=%+v, want seven hops ending at terminal", result)
	}
}

func TestTraceStopsAtKnownWOOXWallet(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	wooX := "0x63dfe4e34a3bfc00eb0220786238a7c6cef8ffc4"
	upstream := "0x0000000000000000000000000000000000000002"
	wooXAddress := store.Address{AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}, SyncStatus: "discovered"}
	repository := &fakeRepository{addresses: map[string]store.Address{
		seed: completeAddress(0, 100), wooX: wooXAddress, upstream: completeAddress(0, 100),
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xwoo", BlockNumber: 20, From: wooX, To: seed, AssetType: "eth", Asset: "ETH", Amount: "10"},
		{Chain: "ethereum", TxHash: "0xupstream", BlockNumber: 10, From: upstream, To: wooX, AssetType: "eth", Asset: "ETH", Amount: "10"},
	}}

	result, err := New(repository).Trace(context.Background(), Query{Address: seed, Direction: "in", Depth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 1 || len(result.Nodes) != 2 || result.Nodes[1].Address != wooX || !result.Nodes[1].Terminal || result.Nodes[1].StopReason != string(store.StopTerminal) {
		t.Fatalf("result=%+v, want WOO X wallet terminal", result)
	}
}

func TestTraceMarksAddressWithoutMatchingTransfersAsTerminal(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	repository := &fakeRepository{addresses: map[string]store.Address{seed: completeAddress(0, 100)}, labels: map[string][]store.Label{}}

	result, err := New(repository).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 || !result.Nodes[0].Terminal || result.Nodes[0].StopReason != string(store.StopNoMatchingTransfers) {
		t.Fatalf("result=%+v", result)
	}
}

func TestTraceHonorsCancellationDuringAutomaticExpansion(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	repository := &fakeRepository{addresses: map[string]store.Address{seed: completeAddress(0, 100)}, labels: map[string][]store.Label{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(repository).Trace(ctx, Query{Address: seed, Direction: "out", Depth: 0})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context cancellation", err)
	}
}

func TestTracePreservesCycleEdgeWithoutRequeueingIt(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	next := "0x0000000000000000000000000000000000000002"
	repository := &fakeRepository{addresses: map[string]store.Address{
		seed: completeAddress(0, 100), next: completeAddress(0, 100),
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xout", BlockNumber: 10, From: seed, To: next, AssetType: "eth", Asset: "ETH", Amount: "10"},
		{Chain: "ethereum", TxHash: "0xback", BlockNumber: 11, From: next, To: seed, AssetType: "eth", Asset: "ETH", Amount: "10"},
	}}

	result, err := New(repository).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 || len(result.Nodes) != 2 {
		t.Fatalf("result=%+v, want the cycle edge without another node", result)
	}
	if len(result.MoneyTransfers) != 2 || result.MoneyTransfers[1].StopReason != store.StopCycle {
		t.Fatalf("money transfers=%+v, want explicit cycle stop evidence", result.MoneyTransfers)
	}
}

func TestExtendBranchUsesAggregatedDownstreamAnchor(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	anchor := "0x0000000000000000000000000000000000000002"
	next := "0x0000000000000000000000000000000000000003"
	repository := &fakeRepository{addresses: map[string]store.Address{
		anchor: completeAddress(0, 100), next: completeAddress(0, 100),
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xnext", BlockNumber: 13, From: anchor, To: next, AssetType: "eth", Asset: "ETH", Amount: "120000000000000000"},
	}}
	root := Result{Nodes: []Node{{Chain: "ethereum", Address: seed}, {Chain: "ethereum", Address: anchor, Depth: 1}}, DataThroughBlock: 100, Edges: []Edge{
		{Chain: "ethereum", From: seed, To: anchor, AssetType: "eth", Asset: "ETH", TotalAmount: "40000000000000000", FirstBlock: 10, LatestBlock: 10, Path: []string{seed, anchor}},
		{Chain: "ethereum", From: seed, To: anchor, AssetType: "eth", Asset: "ETH", TotalAmount: "60000000000000000", FirstBlock: 12, LatestBlock: 12, Path: []string{seed, anchor}},
	}}

	result, err := New(repository).ExtendBranch(context.Background(), root, ExtensionRequest{Chain: "ethereum", Address: anchor, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.calls) != 1 || repository.calls[0].FromBlock != 10 || repository.calls[0].ToBlock != 0 || repository.calls[0].AssetMode != "eth" {
		t.Fatalf("queries=%+v", repository.calls)
	}
	if len(result.Edges) != 1 || result.Edges[0].TotalAmount != "100000000000000000" || len(result.Nodes) != 2 || result.Nodes[1].Address != next {
		t.Fatalf("result=%+v", result)
	}
}

func TestExtendBranchMarksNoMatchingTransfersTerminal(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	anchor := "0x0000000000000000000000000000000000000002"
	repository := &fakeRepository{addresses: map[string]store.Address{
		anchor: completeAddress(0, 100),
	}, labels: map[string][]store.Label{}}
	root := Result{Nodes: []Node{{Chain: "ethereum", Address: seed}, {Chain: "ethereum", Address: anchor, Depth: 1}}, DataThroughBlock: 100, Edges: []Edge{
		{Chain: "ethereum", From: seed, To: anchor, AssetType: "eth", Asset: "ETH", TotalAmount: "100000000000000000", FirstBlock: 10, LatestBlock: 10, Path: []string{seed, anchor}},
	}}

	result, err := New(repository).ExtendBranch(context.Background(), root, ExtensionRequest{Chain: "ethereum", Address: anchor, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 0 || len(result.Nodes) != 1 || !result.Nodes[0].Terminal || result.Nodes[0].StopReason != "no_matching_transfers" {
		t.Fatalf("result=%+v, want an explicit no-matching-transfers terminal", result)
	}
}

func TestExtendBranchMarksKnownWOOXWalletTerminal(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	anchor := "0x0000000000000000000000000000000000000002"
	wooX := "0x63dfe4e34a3bfc00eb0220786238a7c6cef8ffc4"
	wooXAddress := store.Address{AddressType: "eoa", Protocol: "woo_x", Roles: []string{"woo_x_wallet"}, SyncStatus: "discovered"}
	repository := &fakeRepository{addresses: map[string]store.Address{
		anchor: completeAddress(0, 100), wooX: wooXAddress,
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xwoo", BlockNumber: 20, From: wooX, To: anchor, AssetType: "eth", Asset: "ETH", Amount: "10"},
	}}
	root := Result{Nodes: []Node{{Chain: "ethereum", Address: seed}, {Chain: "ethereum", Address: anchor, Depth: 1}}, DataThroughBlock: 100, Edges: []Edge{
		{Chain: "ethereum", From: anchor, To: seed, AssetType: "eth", Asset: "ETH", TotalAmount: "10", FirstBlock: 30, LatestBlock: 30, Path: []string{seed, anchor}},
	}}

	result, err := New(repository).ExtendBranch(context.Background(), root, ExtensionRequest{Chain: "ethereum", Address: anchor, Direction: "in", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || result.Nodes[1].Address != wooX || !result.Nodes[1].Terminal || result.Nodes[1].StopReason != string(store.StopTerminal) {
		t.Fatalf("result=%+v, want WOO X wallet terminal", result)
	}
}

func TestExtendBranchUsesAggregatedUpstreamAnchor(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	anchor := "0x0000000000000000000000000000000000000002"
	previous := "0x0000000000000000000000000000000000000003"
	repository := &fakeRepository{addresses: map[string]store.Address{
		anchor: completeAddress(0, 100), previous: completeAddress(0, 100),
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xprevious", BlockNumber: 18, From: previous, To: anchor, AssetType: "eth", Asset: "ETH", Amount: "50000000000000000"},
	}}
	root := Result{Nodes: []Node{{Chain: "ethereum", Address: seed}, {Chain: "ethereum", Address: anchor, Depth: 1}}, DataThroughBlock: 100, Edges: []Edge{
		{Chain: "ethereum", From: anchor, To: seed, AssetType: "eth", Asset: "ETH", TotalAmount: "50000000000000000", FirstBlock: 20, LatestBlock: 25, Path: []string{seed, anchor}},
	}}

	result, err := New(repository).ExtendBranch(context.Background(), root, ExtensionRequest{Chain: "ethereum", Address: anchor, Direction: "in", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.calls) != 1 || repository.calls[0].FromBlock != 0 || repository.calls[0].ToBlock != 25 {
		t.Fatalf("queries=%+v", repository.calls)
	}
	if len(result.Edges) != 1 || result.Edges[0].From != previous || result.Edges[0].To != anchor {
		t.Fatalf("result=%+v", result)
	}
}

func TestMergeResultsDeduplicatesRepeatedExtension(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	next := "0x0000000000000000000000000000000000000002"
	root := Result{Nodes: []Node{{Chain: "ethereum", Address: seed}}, RuleVersion: traceRuleVersion, DataStatus: "synced"}
	extension := Result{Nodes: []Node{{Chain: "ethereum", Address: seed}, {Chain: "ethereum", Address: next, Depth: 1}}, Edges: []Edge{{Chain: "ethereum", From: seed, To: next, AssetType: "eth", Asset: "ETH", TotalAmount: "1", FirstBlock: 10, LatestBlock: 10, Path: []string{seed, next}}}, DataStatus: "synced"}

	merged := MergeResults(MergeResults(root, extension), extension)
	if len(merged.Nodes) != 2 || len(merged.Edges) != 1 {
		t.Fatalf("merged=%+v", merged)
	}
}

func TestMergeResultsKeepsTHORChainVaultIdentity(t *testing.T) {
	address := "0x0000000000000000000000000000000000000002"
	root := Result{Nodes: []Node{{Chain: "ethereum", Address: address, AddressType: "eoa"}}}
	extension := Result{Nodes: []Node{{Chain: "ethereum", Address: address, AddressType: "eoa", Protocol: "thorchain", Roles: []string{"thorchain_vault"}}}}
	merged := MergeResults(root, extension)
	if merged.Nodes[0].Protocol != "thorchain" || !slices.Contains(merged.Nodes[0].Roles, "thorchain_vault") {
		t.Fatalf("merged node=%+v", merged.Nodes[0])
	}
}

func TestTraceRootIncludesOnlyWhitelistedERC20ByContract(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	ethRecipient := "0x0000000000000000000000000000000000000002"
	usdtRecipient := "0x0000000000000000000000000000000000000003"
	spoofRecipient := "0x0000000000000000000000000000000000000004"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: completeAddress(0, 100), ethRecipient: completeAddress(0, 100), usdtRecipient: completeAddress(0, 100), spoofRecipient: completeAddress(0, 100),
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
		t.Fatalf("edges=%+v, want ETH and whitelisted USDT only", result.Edges)
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
	if slices.ContainsFunc(result.Edges, func(edge Edge) bool { return edge.To == spoofRecipient }) {
		t.Fatalf("unknown ERC-20 edge=%+v, want it excluded", result.Edges)
	}
	if len(r.calls) < 1 || r.calls[0].AssetMode != "all" {
		t.Fatalf("root queries=%+v, want the root to use one concrete all-asset query", r.calls)
	}
}

func TestRootAssetsAreEthereumOnly(t *testing.T) {
	ethereum := rootAssets("ethereum")
	if len(ethereum) != 5 || ethereum[0].Asset != "ETH" || ethereum[3].Asset != ethereumUSDT {
		t.Fatalf("ethereum root assets=%+v", ethereum)
	}
	if base := rootAssets("base"); len(base) != 1 || base[0].Asset != "ETH" {
		t.Fatalf("unsupported chain root assets=%+v", base)
	}
}

type fakeRepository struct {
	addresses      map[string]store.Address
	identities     map[string]store.AddressIdentity
	transfers      []store.Transfer
	queryTransfers func(store.TransferQuery) []store.Transfer
	labels         map[string][]store.Label
	calls          []store.TransferQuery
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
		SyncStatus:       "synced",
		NormalSyncedFrom: from, NormalSyncedTo: to, InternalSyncedFrom: from,
		InternalSyncedTo: to, TokenSyncedFrom: from, TokenSyncedTo: to,
	}
}

func completeContractAddress(from, to int64) store.Address {
	address := completeAddress(from, to)
	address.IsContract, address.AddressType = true, "contract"
	return address
}

func completeEOAAddress(from, to int64) store.Address {
	address := completeAddress(from, to)
	address.AddressType = "eoa"
	return address
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
	if r.queryTransfers != nil {
		return r.queryTransfers(q), nil
	}
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
func TestTraceBatchesFrontierAndPrunesTerminal(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	terminal := "0x0000000000000000000000000000000000000002"
	next := "0x0000000000000000000000000000000000000003"
	r := &fakeRepository{addresses: map[string]store.Address{seed: completeAddress(0, 100), terminal: completeAddress(0, 100), next: completeAddress(0, 100)}, transfers: []store.Transfer{{TxHash: "0x1", From: seed, To: terminal, Asset: "ETH", Amount: "2000000000000000000"}, {TxHash: "0x2", From: terminal, To: next, Asset: "ETH", Amount: "1000000000000000000"}}, labels: map[string][]store.Label{}}
	terminalAddress := r.addresses[terminal]
	terminalAddress.IsTerminal = true
	r.addresses[terminal] = terminalAddress
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
	if _, err := New(r).Trace(context.Background(), Query{Address: seed, Depth: -1}); err != ErrInvalidQuery {
		t.Fatalf("err=%v", err)
	}
	if _, err := New(r).Trace(context.Background(), Query{Address: seed}); !errors.Is(err, ErrAddressNotSynced) {
		t.Fatalf("err=%v", err)
	}
}

func TestTraceRejectsRecordLimitedAddressAsIncomplete(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: {SyncStatus: "partial", SyncError: "record_limit", SyncMaxRecordsPerAction: 100_000},
	}, labels: map[string][]store.Label{}}

	_, err := New(r).WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1})
	if !errors.Is(err, ErrAddressNotSynced) {
		t.Fatalf("err=%v, want incomplete address", err)
	}
}

func TestTraceMarksHighFrequencySeedTerminal(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: {SyncStatus: "partial", SyncError: "high_frequency", SyncMaxRecordsPerAction: 50_000},
	}, labels: map[string][]store.Label{}}

	result, err := New(r).WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.DataStatus != "partial" || len(result.Nodes) != 1 || !result.Nodes[0].Terminal || result.Nodes[0].StopReason != "high_frequency" {
		t.Fatalf("result=%+v", result)
	}
	if len(r.calls) != 0 {
		t.Fatalf("transfer queries=%d, want 0", len(r.calls))
	}
}

func TestTraceStopsExpansionAtFiftyThousandTransfers(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	queriedLimit := int64(0)
	r := &fakeRepository{
		addresses: map[string]store.Address{seed: completeAddress(0, 100)},
		labels:    map[string][]store.Label{},
		queryTransfers: func(query store.TransferQuery) []store.Transfer {
			queriedLimit = query.Limit
			return make([]store.Transfer, 50_000)
		},
	}

	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if queriedLimit != 50_000 || result.DataStatus != "partial" || len(result.Nodes) != 1 || !result.Nodes[0].Terminal || result.Nodes[0].StopReason != "high_frequency" {
		t.Fatalf("limit=%d result=%+v", queriedLimit, result)
	}
}

func TestTraceExistingDataOnlySkipsCoverageAndAddressInspection(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	neighbor := "0x0000000000000000000000000000000000000002"
	r := &fakeRepository{
		addresses: map[string]store.Address{
			seed:     {SyncStatus: "failed"},
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

func TestTraceAcceptsDownstreamDependencyCoverageFromAnchor(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	dependency := "0x0000000000000000000000000000000000000002"
	r := &fakeRepository{
		addresses: map[string]store.Address{
			seed:       completeAddress(50, 100),
			dependency: completeAddress(95, 100),
		},
		transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0x1", BlockNumber: 95, From: seed, To: dependency, AssetType: "eth", Asset: "ETH", Amount: "1000000000000000000"}},
		labels:    map[string][]store.Label{},
	}

	result, err := New(r).WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil || len(result.Edges) != 1 {
		t.Fatalf("result=%+v error=%v, want bounded downstream coverage accepted", result, err)
	}
}

func TestTraceAcceptsUpstreamDependencyCoverageThroughAnchor(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	dependency := "0x0000000000000000000000000000000000000002"
	r := &fakeRepository{
		addresses: map[string]store.Address{
			seed:       completeAddress(50, 100),
			dependency: completeAddress(50, 80),
		},
		transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0x1", BlockNumber: 80, From: dependency, To: seed, AssetType: "eth", Asset: "ETH", Amount: "1000000000000000000"}},
		labels:    map[string][]store.Label{},
	}

	result, err := New(r).WithRequiredStartBlocks(map[string]int64{"ethereum": 50}).Trace(context.Background(), Query{Address: seed, Direction: "in", Depth: 2})
	if err != nil || len(result.Edges) != 1 {
		t.Fatalf("result=%+v error=%v, want bounded upstream coverage accepted", result, err)
	}
}

func TestTraceRequiresDependencyCurrentCoverage(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	dependency := "0x0000000000000000000000000000000000000002"
	r := &fakeRepository{
		addresses: map[string]store.Address{
			seed:       completeAddress(50, 100),
			dependency: {SyncStatus: "synced", NormalSyncedFrom: 50, NormalSyncedTo: 90, InternalSyncedFrom: 50, InternalSyncedTo: 90, TokenSyncedFrom: 50, TokenSyncedTo: 90},
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

func TestTraceStopsAtKnownBridgeWithoutCrossChainAnalysis(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	bridge := "0x0000000000000000000000000000000000000002"
	bridgeAddress := completeContractAddress(0, 100)
	bridgeAddress.Protocol = "bridge"
	bridgeAddress.Roles = []string{"cross_chain_bridge"}
	r := &fakeRepository{addresses: map[string]store.Address{seed: completeAddress(0, 100), bridge: bridgeAddress}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0xsource", BlockNumber: 90, From: seed, To: bridge, AssetType: "eth", Asset: "ETH", Amount: "1000000000000000000"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleVersion != traceRuleVersion || len(result.Edges) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if !result.Nodes[1].Terminal || result.Nodes[1].StopReason != "cross_chain_bridge" {
		t.Fatalf("bridge node=%+v", result.Nodes[1])
	}
}

func TestTraceRootShowsTokenMintAsZeroAddressTerminal(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	zero := "0x0000000000000000000000000000000000000000"
	r := &fakeRepository{addresses: map[string]store.Address{seed: completeAddress(0, 100)}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0xmint", From: zero, To: seed, AssetType: "erc20", Asset: ethereumUSDT, TokenValue: "50000000"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "in", Depth: 2})
	if err != nil || len(result.Edges) != 1 || len(result.Nodes) != 2 || !result.Nodes[1].Terminal || result.Nodes[1].Address != zero {
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
		addresses[address] = completeAddress(0, 100)
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
		addresses[address] = completeAddress(0, 100)
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

func TestTraceBothStopsDirectionalBudgetsAtCycles(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	shared := "0x0000000000000000000000000000000000000002"
	parent := "0x0000000000000000000000000000000000000003"
	child := "0x0000000000000000000000000000000000000004"
	addresses := map[string]store.Address{}
	for _, address := range []string{seed, shared, parent, child} {
		addresses[address] = completeAddress(0, 100)
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
	if len(result.Nodes) != 2 {
		t.Fatalf("nodes=%+v, unrelated parent and child must not consume exhausted cycle budgets", result.Nodes)
	}
	cycleStops := 0
	for _, transfer := range result.MoneyTransfers {
		if transfer.StopReason == store.StopCycle {
			cycleStops++
		}
	}
	if cycleStops != 2 {
		t.Fatalf("money transfers=%+v, want one cycle stop in each direction", result.MoneyTransfers)
	}
}

func TestTraceRootKeepsConcreteTransfers(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	large := "0x0000000000000000000000000000000000000002"
	small := "0x0000000000000000000000000000000000000003"
	spam := "0x0000000000000000000000000000000000000004"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:  completeAddress(0, 100),
		large: completeAddress(0, 100),
		small: completeAddress(0, 100),
		spam:  completeAddress(0, 100),
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
		t.Fatalf("root edges=%+v, want every valid concrete transfer", result.Edges)
	}
	if len(r.calls) < 1 || r.calls[0].AssetMode != "all" {
		t.Fatalf("root query=%+v, want the root to use one all-asset query", r.calls)
	}
}

func TestTraceSwitchesAssetOnlyForVerifiedContractConversion(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0x0000000000000000000000000000000000000002"
	recipient := "0x0000000000000000000000000000000000000003"
	token := ethereumUSDT
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:      completeAddress(0, 100),
		router:    completeContractAddress(0, 100),
		recipient: completeAddress(0, 100),
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
		seed:      completeEOAAddress(0, 100),
		executor:  {AddressType: "contract", IsContract: true, Protocol: "kyberswap", Roles: []string{"kyberswap_executor"}},
		recipient: completeEOAAddress(0, 100),
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

func TestTraceMarksReturningKyberSwapWhenInitiatorIsRecipient(t *testing.T) {
	seed := "0x87aab7bac1308faf2a0d59da26b8379e18b26355"
	executor := "0x6e4141d33021b52c91c28608403db4a0ffb50ec6"
	provider := "0x67336cec42645f55059eff241cb02ea5cc52ff86"
	usdt := "0xdac17f958d2ee523a2206206994597c13d831ec7"
	txHash := "0xa83d5ad633dab303eff99cc35cf645fb20504fdf09f3f7bf92994f23aacf99be"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:     completeEOAAddress(0, 30_000_000),
		executor: {AddressType: "contract", IsContract: true, Protocol: "kyberswap", Roles: []string{"kyberswap_executor"}},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: txHash, BlockNumber: 22_989_558, From: seed, To: executor, AssetType: "erc20", Asset: usdt, Symbol: "USDT", Decimals: 6, TokenMetadataComplete: true, TokenValue: "1000000000000"},
		{Chain: "ethereum", TxHash: txHash, BlockNumber: 22_989_558, From: executor, To: seed, AssetType: "eth", Asset: "ETH", Amount: "274823886224587330677"},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: txHash, From: seed, To: executor, Succeeded: true, FinalOutputAddress: seed,
		Conversions: []store.SwapConversion{{Protocol: "kyberswap", Version: "rfq", Status: "complete", Initiator: seed, Router: "0x6131b5fae19ea4f9d964eac0408e4408b66337b5", Executor: executor, LiquidityProvider: provider, Recipient: seed, TokenIn: usdt, AmountIn: "1000000000000", TokenOut: "ETH", AmountOut: "274823886224587330677", Evidence: []string{"receipt transfers", "WETH withdrawal", "internal ETH payout"}}},
		Quality:     store.AnalysisQuality{Status: "complete"},
	}}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "both", Asset: "ETH", Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	var swapEdges []Edge
	for _, edge := range result.Edges {
		if edge.Kind == "swap" {
			swapEdges = append(swapEdges, edge)
		}
	}
	if len(result.Edges) != 2 || len(swapEdges) != 1 {
		t.Fatalf("edges=%+v, want two transaction legs with exactly one verified swap leg", result.Edges)
	}
	if len(swapEdges[0].ConversionEvidence) != 1 || swapEdges[0].ConversionEvidence[0].TxHash != txHash || swapEdges[0].ConversionEvidence[0].AmountOut != "274823886224587330677" {
		t.Fatalf("swap evidence=%+v", swapEdges[0].ConversionEvidence)
	}
}

func TestTraceContinuesThroughVerifiedTHORChainVaultMigration(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0xd37bbe5744d730a1d98d8dc97c42f0ca46ad7146"
	vault := "0x0000000000000000000000000000000000000003"
	next := "0x0000000000000000000000000000000000000004"
	amount := "375820107740000000000"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: completeEOAAddress(0, 100), router: completeContractAddress(0, 100), vault: completeEOAAddress(0, 100), next: completeEOAAddress(0, 100),
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xmigrate", BlockNumber: 10, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: amount},
		{Chain: "ethereum", TxHash: "0xnext", BlockNumber: 11, From: vault, To: next, AssetType: "eth", Asset: "ETH", Amount: "100000000000000000000"},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xmigrate", BlockNumber: 10, From: seed, To: router, Succeeded: true,
		ProtocolAction: "vault_migration", ProtocolMemo: "MIGRATE:22985236", ProtocolDestination: vault, ProtocolAsset: "ETH", ProtocolAmount: amount,
		InternalCalls: []store.InternalCall{{From: router, To: vault, Value: amount}}, Quality: store.AnalysisQuality{Status: "complete"},
	}}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 3 || result.Edges[1].Kind != "thorchain_vault_migration" || result.Edges[1].Protocol != "thorchain" || result.Edges[1].ProtocolMemo != "MIGRATE:22985236" || result.Edges[2].To != next {
		t.Fatalf("edges=%+v", result.Edges)
	}
	for _, node := range result.Nodes {
		if node.Address == vault && (node.Protocol != "thorchain" || !slices.Contains(node.Roles, "thorchain_vault")) {
			t.Fatalf("vault node=%+v", node)
		}
	}
}

func TestTraceContinuesThroughVerifiedTHORChainProtocolOutbound(t *testing.T) {
	vault := "0x0000000000000000000000000000000000000001"
	router := "0xd37bbe5744d730a1d98d8dc97c42f0ca46ad7146"
	recipient := "0x0000000000000000000000000000000000000003"
	next := "0x0000000000000000000000000000000000000004"
	amount := "375820107740000000000"
	r := &fakeRepository{addresses: map[string]store.Address{
		vault: completeEOAAddress(0, 100), router: {SyncStatus: "partial", SyncError: "high_frequency", SyncMaxRecordsPerAction: 50_000, AddressType: "contract", IsContract: true}, recipient: completeEOAAddress(0, 100), next: completeEOAAddress(0, 100),
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xout", BlockNumber: 10, From: vault, To: router, AssetType: "eth", Asset: "ETH", Amount: amount},
		{Chain: "ethereum", TxHash: "0xnext", BlockNumber: 11, From: recipient, To: next, AssetType: "eth", Asset: "ETH", Amount: "100000000000000000000"},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xout", BlockNumber: 10, From: vault, To: router, Succeeded: true,
		ProtocolAction: "protocol_outbound", ProtocolMemo: "OUT:ABC123", ProtocolDestination: recipient, ProtocolAsset: "ETH", ProtocolAmount: amount,
		InternalCalls: []store.InternalCall{{From: router, To: recipient, Value: amount}}, Quality: store.AnalysisQuality{Status: "complete"},
	}}

	inspector := addressInspectorStub{identity: store.AddressIdentity{AddressType: "contract", Protocol: "thorchain", Roles: []string{"router"}}}
	result, err := New(r).WithAddressInspector(inspector).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: vault, Direction: "out", Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 3 || result.Edges[1].Kind != "thorchain_protocol_outbound" || result.Edges[1].ProtocolAction != "protocol_outbound" || result.Edges[1].To != recipient || result.Edges[2].To != next {
		t.Fatalf("edges=%+v", result.Edges)
	}
	for _, node := range result.Nodes {
		if node.Address == router && (node.Protocol != "thorchain" || !containsRole(node.Roles, "router")) {
			t.Fatalf("router identity not restored: %+v", node)
		}
		if node.Address == recipient && containsRole(node.Roles, "thorchain_vault") {
			t.Fatalf("outbound recipient must not be marked as a vault: %+v", node)
		}
	}
}

func TestTraceAutomaticallyFollowsTHORChainProtocolOutbound(t *testing.T) {
	vault := "0x0000000000000000000000000000000000000001"
	router := "0xd37bbe5744d730a1d98d8dc97c42f0ca46ad7146"
	recipient := "0x0000000000000000000000000000000000000003"
	amount := "375820107740000000000"
	repository := &fakeRepository{addresses: map[string]store.Address{
		vault: completeEOAAddress(0, 100), router: completeContractAddress(0, 100),
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xout", BlockNumber: 10, From: vault, To: router, AssetType: "eth", Asset: "ETH", Amount: amount},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xout", BlockNumber: 10, From: vault, To: router, Succeeded: true,
		ProtocolAction: "protocol_outbound", ProtocolMemo: "OUT:ABC123", ProtocolDestination: recipient, ProtocolAsset: "ETH", ProtocolAmount: amount,
		InternalCalls: []store.InternalCall{{From: router, To: recipient, Value: amount}}, Quality: store.AnalysisQuality{Status: "complete"},
	}}
	inspector := addressInspectorStub{identity: store.AddressIdentity{AddressType: "contract", Protocol: "thorchain", Roles: []string{"router"}}}

	result, err := New(repository).WithAddressInspector(inspector).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: vault, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 || result.Edges[0].Protocol != "thorchain" || result.Edges[0].ProtocolAction != "protocol_outbound" || result.Edges[0].ProtocolMemo != "OUT:ABC123" {
		t.Fatalf("edges=%+v, want annotated THORChain entry and protocol outbound", result.Edges)
	}
	if result.Edges[1].To != recipient || result.Edges[1].Kind != "thorchain_protocol_outbound" {
		t.Fatalf("outbound=%+v, want final THORChain recipient", result.Edges[1])
	}
}

func TestExtendBranchFollowsTHORChainProtocolOutboundFromRouter(t *testing.T) {
	vault := "0x0000000000000000000000000000000000000001"
	router := "0xd37bbe5744d730a1d98d8dc97c42f0ca46ad7146"
	recipient := "0x0000000000000000000000000000000000000003"
	amount := "375820107740000000000"
	repository := &fakeRepository{addresses: map[string]store.Address{
		router: completeContractAddress(0, 100), recipient: completeEOAAddress(0, 100),
	}, labels: map[string][]store.Label{}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xout", BlockNumber: 10, From: vault, To: router, Succeeded: true,
		ProtocolAction: "protocol_outbound", ProtocolMemo: "OUT:ABC123", ProtocolDestination: recipient, ProtocolAsset: "ETH", ProtocolAmount: amount,
		InternalCalls: []store.InternalCall{{From: router, To: recipient, Value: amount}}, Quality: store.AnalysisQuality{Status: "complete"},
	}}
	root := Result{
		Nodes:            []Node{{Chain: "ethereum", Address: vault, Depth: 0}, {Chain: "ethereum", Address: router, Depth: 1, AddressType: "contract", Protocol: "thorchain", Roles: []string{"router"}}},
		Edges:            []Edge{{Chain: "ethereum", From: vault, To: router, AssetType: "eth", Asset: "ETH", TotalAmount: amount, TransferCount: 1, Kind: "transfer", Depth: 1, Path: []string{vault, router}, TxHash: "0xout", FirstBlock: 10, LatestBlock: 10, Protocol: "thorchain", ProtocolAction: "protocol_outbound", ProtocolMemo: "OUT:ABC123"}},
		DataThroughBlock: 100, DataStatus: "synced", RuleVersion: traceRuleVersion,
	}

	result, err := New(repository).WithTransactionAnalyzer(analyzer).ExtendBranch(context.Background(), root, ExtensionRequest{Chain: "ethereum", Address: router, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 1 || result.Edges[0].From != router || result.Edges[0].To != recipient || result.Edges[0].Kind != "thorchain_protocol_outbound" || result.Edges[0].ProtocolAction != "protocol_outbound" {
		t.Fatalf("result=%+v, want the verified THORChain protocol output", result)
	}
}

func TestTraceRejectsUnverifiedTHORChainVaultMigration(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0xd37bbe5744d730a1d98d8dc97c42f0ca46ad7146"
	vault := "0x0000000000000000000000000000000000000003"
	amount := "1000000000000000000"
	r := &fakeRepository{addresses: map[string]store.Address{seed: completeEOAAddress(0, 100), router: completeContractAddress(0, 100), vault: completeEOAAddress(0, 100)}, labels: map[string][]store.Label{}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0xmigrate", BlockNumber: 10, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: amount}}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{Chain: "ethereum", TxHash: "0xmigrate", BlockNumber: 10, From: seed, To: router, Succeeded: true, ProtocolAction: "vault_migration", ProtocolDestination: vault, ProtocolAsset: "ETH", ProtocolAmount: amount, Quality: store.AnalysisQuality{Status: "complete"}}}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 1 || len(result.Nodes) != 2 || !result.Nodes[1].Terminal || result.Nodes[1].StopReason != "ambiguous_conversion" {
		t.Fatalf("result=%+v", result)
	}
}

func TestTraceAddsConfirmedTHORChainBitcoinEndpoint(t *testing.T) {
	seed := "0x1476be0c524e15c735a1b3927bba993f03a59790"
	vault := "0x3986fd2fd3669fd32584b960cda8f82e0f772c35"
	btcRecipient := "bc1phghtqqpfwgw3jzaefey0z0epesyd77nyncllcer9s9rs0t4t4ttqhcpgd4"
	txHash := "0xd6cc09af0fe3b51225cc8db073eb4353ba657cf7041be6877c78c993e383a4c2"
	memo := "=:b:" + btcRecipient + ":725679114/1/0:-_/bgw:20/30"
	vaultMetadata := completeEOAAddress(0, 100)
	vaultMetadata.Protocol = "thorchain"
	vaultMetadata.Roles = []string{"thorchain_vault"}
	repository := &fakeRepository{
		addresses: map[string]store.Address{seed: completeEOAAddress(0, 100), vault: vaultMetadata},
		labels:    map[string][]store.Label{},
		transfers: []store.Transfer{{Chain: "ethereum", TxHash: txHash, BlockNumber: 10, From: seed, To: vault, AssetType: "eth", Asset: "ETH", Amount: "200650000000000000000"}},
	}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: txHash, From: seed, To: vault, Value: "200650000000000000000", Input: "0x" + hex.EncodeToString([]byte(memo)), Succeeded: true,
		ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btcRecipient, ProtocolAsset: "BTC.BTC", Quality: store.AnalysisQuality{Status: "complete"},
	}}
	verifier := crossChainVerifierStub{outbound: VerifiedCrossChainTransfer{
		SourceChain: "ethereum", TargetChain: "bitcoin", From: vault, To: btcRecipient, Asset: "BTC", Amount: "729777955",
		TxHash: "f9e417b7db1d7128327387c0971e377e5a694d28758fea4d43c64d3fae098634", BlockNumber: 917213,
	}}

	result, err := New(repository).WithTransactionAnalyzer(analyzer).WithCrossChainVerifier(verifier).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 || len(result.Nodes) != 3 {
		t.Fatalf("result=%+v, want ETH inbound and verified BTC endpoint", result)
	}
	outbound := result.Edges[1]
	if outbound.SourceChain != "ethereum" || outbound.TargetChain != "bitcoin" || outbound.From != vault || outbound.To != btcRecipient || outbound.Asset != "BTC" || outbound.TotalAmount != "729777955" || outbound.Kind != "thorchain_cross_chain_outbound" || outbound.SourceTxHash != txHash || outbound.SourceAmount != "200650000000000000000" {
		t.Fatalf("outbound=%+v", outbound)
	}
	var endpoint *Node
	var vaultNode *Node
	for index := range result.Nodes {
		if result.Nodes[index].Chain == "bitcoin" {
			endpoint = &result.Nodes[index]
		}
		if result.Nodes[index].Chain == "ethereum" && result.Nodes[index].Address == vault {
			vaultNode = &result.Nodes[index]
		}
	}
	if endpoint == nil || endpoint.Address != btcRecipient || !endpoint.Terminal || endpoint.Protocol != "thorchain" || !containsRole(endpoint.Roles, "cross_chain_recipient") {
		t.Fatalf("endpoint=%+v", endpoint)
	}
	if identity := repository.identities[vault]; identity.Protocol != "thorchain" || !containsRole(identity.Roles, "thorchain_vault") {
		t.Fatalf("vault identity=%+v", identity)
	}
	if vaultNode == nil || !vaultNode.Terminal || vaultNode.StopReason != "cross_chain_bridge" || vaultNode.Protocol != "thorchain" || !containsRole(vaultNode.Roles, "thorchain_vault") {
		t.Fatalf("vault node=%+v", vaultNode)
	}
}

func TestTraceAddsConfirmedMAYAChainBitcoinEndpoint(t *testing.T) {
	seed := "0x9951544f600e95c2ca5f967c0567b9bf50de72d0"
	router := "0xe3985e6b61b814f7cdb188766562ba71b446b46d"
	btc := "bc1q2yjae3xdnlwwk6hxf7jxjdt0waupl8qglv3qs7"
	txHash := "0x2b6fa2acfb7e87f3ca39331b071f3479ee6cff8a2967d0fddbb6459172a03afc"
	memo := "=:b:" + btc + ":115252390/3/0:ns:5"
	routerMetadata := completeContractAddress(0, 100)
	routerMetadata.Protocol = "mayachain"
	routerMetadata.Roles = []string{"router"}
	repository := &fakeRepository{
		addresses: map[string]store.Address{seed: completeEOAAddress(0, 100), router: routerMetadata},
		labels:    map[string][]store.Label{},
		transfers: []store.Transfer{{Chain: "ethereum", TxHash: txHash, BlockNumber: 10, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: "33000000000000000000"}},
	}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: txHash, From: seed, To: router, Value: "33000000000000000000", Succeeded: true,
		ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btc, ProtocolVault: "0x55c302924d5616f1f571425c6cf0762c25ebae81", ProtocolAsset: "BTC.BTC", Quality: store.AnalysisQuality{Status: "complete"},
	}}
	verifier := crossChainVerifierStub{outbound: VerifiedCrossChainTransfer{
		Protocol: "mayachain", SourceChain: "ethereum", TargetChain: "bitcoin", From: router, To: btc, Asset: "BTC", Amount: "120934527",
		TxHash: "046992e1705d36a84dc5c36322e6272e94f1ba2dcd49c6be7295ae51f0769901", BlockNumber: 917224,
	}}

	result, err := New(repository).WithTransactionAnalyzer(analyzer).WithCrossChainVerifier(verifier).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 || result.Edges[0].Protocol != "mayachain" || result.Edges[1].Kind != "mayachain_cross_chain_outbound" || result.Edges[1].To != btc || result.Edges[1].TotalAmount != "120934527" {
		t.Fatalf("edges=%+v", result.Edges)
	}
	var routerNode, btcNode *Node
	for index := range result.Nodes {
		if result.Nodes[index].Chain == "ethereum" && result.Nodes[index].Address == router {
			routerNode = &result.Nodes[index]
		}
		if result.Nodes[index].Chain == "bitcoin" && result.Nodes[index].Address == btc {
			btcNode = &result.Nodes[index]
		}
	}
	if len(result.Nodes) != 3 || routerNode == nil || routerNode.Protocol != "mayachain" || !routerNode.Terminal || btcNode == nil || btcNode.Protocol != "mayachain" || !btcNode.Terminal {
		t.Fatalf("nodes=%+v", result.Nodes)
	}
}

func TestTraceDoesNotAddUnverifiedTHORChainBitcoinEndpoint(t *testing.T) {
	seed := "0x1476be0c524e15c735a1b3927bba993f03a59790"
	vault := "0x3986fd2fd3669fd32584b960cda8f82e0f772c35"
	btcRecipient := "bc1phghtqqpfwgw3jzaefey0z0epesyd77nyncllcer9s9rs0t4t4ttqhcpgd4"
	txHash := "0xd6cc09af0fe3b51225cc8db073eb4353ba657cf7041be6877c78c993e383a4c2"
	memo := "=:b:" + btcRecipient + ":725679114/1/0:-_/bgw:20/30"
	vaultMetadata := completeEOAAddress(0, 100)
	vaultMetadata.IsTerminal = true
	repository := &fakeRepository{addresses: map[string]store.Address{seed: completeEOAAddress(0, 100), vault: vaultMetadata}, labels: map[string][]store.Label{}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: txHash, BlockNumber: 10, From: seed, To: vault, AssetType: "eth", Asset: "ETH", Amount: "200650000000000000000", Input: "0x" + hex.EncodeToString([]byte(memo))}}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{Chain: "ethereum", TxHash: txHash, From: seed, To: vault, Value: "200650000000000000000", Succeeded: true, ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btcRecipient, ProtocolAsset: "BTC.BTC", Quality: store.AnalysisQuality{Status: "complete"}}}

	result, err := New(repository).WithTransactionAnalyzer(analyzer).WithCrossChainVerifier(crossChainVerifierStub{}).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range result.Nodes {
		if node.Chain == "bitcoin" {
			t.Fatalf("unverified memo produced Bitcoin endpoint: %+v", result)
		}
	}
	if len(result.Edges) != 1 {
		t.Fatalf("edges=%+v, want only original Ethereum transfer", result.Edges)
	}
}

func TestTraceKeepsEthereumGraphWhenTHORChainVerificationIsUnavailable(t *testing.T) {
	seed := "0x1476be0c524e15c735a1b3927bba993f03a59790"
	vault := "0x3986fd2fd3669fd32584b960cda8f82e0f772c35"
	btc := "bc1phghtqqpfwgw3jzaefey0z0epesyd77nyncllcer9s9rs0t4t4ttqhcpgd4"
	memo := "=:b:" + btc
	txHash := "0xd6cc09af0fe3b51225cc8db073eb4353ba657cf7041be6877c78c993e383a4c2"
	vaultMetadata := completeEOAAddress(0, 100)
	vaultMetadata.IsTerminal = true
	repository := &fakeRepository{addresses: map[string]store.Address{seed: completeEOAAddress(0, 100), vault: vaultMetadata}, labels: map[string][]store.Label{}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: txHash, BlockNumber: 10, From: seed, To: vault, AssetType: "eth", Asset: "ETH", Amount: "100", Input: "0x" + hex.EncodeToString([]byte(memo))}}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{Chain: "ethereum", TxHash: txHash, From: seed, To: vault, Value: "100", Succeeded: true, ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btc, ProtocolAsset: "BTC.BTC"}}
	result, err := New(repository).WithTransactionAnalyzer(analyzer).WithCrossChainVerifier(crossChainVerifierStub{err: errors.New("status API unavailable")}).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.DataStatus != "partial" || len(result.Edges) != 1 || result.Edges[0].ConversionStatus != "partial" || len(result.Nodes) != 2 {
		t.Fatalf("result=%+v", result)
	}
}

func TestTraceKeepsVaultDepositsTransactionSpecific(t *testing.T) {
	seed := "0x1476be0c524e15c735a1b3927bba993f03a59790"
	vault := "0x3986fd2fd3669fd32584b960cda8f82e0f772c35"
	btc := "bc1phghtqqpfwgw3jzaefey0z0epesyd77nyncllcer9s9rs0t4t4ttqhcpgd4"
	memo := "=:b:" + btc
	input := "0x" + hex.EncodeToString([]byte(memo))
	tx1, tx2 := "0x01", "0x02"
	repository := &fakeRepository{addresses: map[string]store.Address{seed: completeEOAAddress(0, 100), vault: completeEOAAddress(0, 100)}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: tx1, BlockNumber: 10, From: seed, To: vault, AssetType: "eth", Asset: "ETH", Amount: "100", Input: input},
		{Chain: "ethereum", TxHash: tx2, BlockNumber: 11, From: seed, To: vault, AssetType: "eth", Asset: "ETH", Amount: "200", Input: input},
	}}
	analyzer := &transactionAnalyzerMap{analyses: map[string]store.TransactionAnalysis{
		tx1: {Chain: "ethereum", TxHash: tx1, From: seed, To: vault, Value: "100", Succeeded: true, ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btc, ProtocolAsset: "BTC.BTC"},
		tx2: {Chain: "ethereum", TxHash: tx2, From: seed, To: vault, Value: "200", Succeeded: true, ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btc, ProtocolAsset: "BTC.BTC"},
	}}
	verifier := crossChainVerifierMap{values: map[string]VerifiedCrossChainTransfer{tx2: {SourceChain: "ethereum", TargetChain: "bitcoin", From: vault, To: btc, Asset: "BTC", Amount: "50", TxHash: "btctx", BlockNumber: 20}}}
	result, err := New(repository).WithTransactionAnalyzer(analyzer).WithCrossChainVerifier(verifier).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 3 || result.Edges[0].TxHash != tx1 || result.Edges[0].ProtocolAction != "" || result.Edges[1].TxHash != tx2 || result.Edges[1].ProtocolAction != "router_inbound" || result.Edges[2].TxHash != "btctx" {
		t.Fatalf("edges=%+v", result.Edges)
	}
}

type crossChainVerifierStub struct {
	outbound VerifiedCrossChainTransfer
	ok       bool
	err      error
}

func (s crossChainVerifierStub) Verify(context.Context, store.TransactionAnalysis) (VerifiedCrossChainTransfer, bool, error) {
	if s.err != nil {
		return VerifiedCrossChainTransfer{}, false, s.err
	}
	if s.outbound.To != "" {
		return s.outbound, true, nil
	}
	return VerifiedCrossChainTransfer{}, s.ok, nil
}

type crossChainVerifierMap struct {
	values map[string]VerifiedCrossChainTransfer
}

func (s crossChainVerifierMap) Verify(_ context.Context, analysis store.TransactionAnalysis) (VerifiedCrossChainTransfer, bool, error) {
	value, ok := s.values[analysis.TxHash]
	return value, ok, nil
}

func TestTraceAnalyzesEachAnchoredContractTransaction(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0x0000000000000000000000000000000000000002"
	recipient := "0x0000000000000000000000000000000000000003"
	token := ethereumUSDT
	r := &fakeRepository{addresses: map[string]store.Address{seed: completeAddress(0, 100), router: completeContractAddress(0, 100), recipient: completeAddress(0, 100)}, labels: map[string][]store.Label{}}
	analyzer := &transactionAnalyzerMap{contract: router, analyses: make(map[string]store.TransactionAnalysis)}
	for i := 1; i <= 2; i++ {
		hash := fmt.Sprintf("0x%02d", i)
		amount := fmt.Sprintf("%d000000000000000000", i)
		r.transfers = append(r.transfers, store.Transfer{Chain: "ethereum", TxHash: hash, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: amount})
		analyzer.analyses[hash] = store.TransactionAnalysis{Chain: "ethereum", TxHash: hash, From: seed, To: router, Succeeded: true, FinalOutputAddress: recipient, Swaps: []store.SwapEvent{{Verified: true, TokenIn: ethereumWETH, TokenOut: token, AmountIn: amount, AmountOut: "100000000"}}, Wraps: []store.WrapEvent{{Type: "deposit", Account: seed, Amount: amount}}, Quality: store.AnalysisQuality{Status: "complete"}}
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
		seed:     completeEOAAddress(0, 100),
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
		seed:   completeAddress(0, 100),
		router: completeContractAddress(0, 100),
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

func TestAddressCoveredRequiresEveryActionRange(t *testing.T) {
	graph := New(&fakeRepository{}).WithRequiredStartBlocks(map[string]int64{"ethereum": 50})
	complete := completeAddress(50, 100)
	if !graph.addressCovered(complete, 50, 100) {
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
		if graph.addressCovered(address, 50, 100) {
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
		seed: completeAddress(0, 100), middle: completeAddress(0, 100), first: completeAddress(0, 100), second: completeAddress(0, 100),
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
