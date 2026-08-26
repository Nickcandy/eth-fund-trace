package tracer

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestTraceTopNMeansCounterpartiesRankedByCumulativeAmount(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	a := "0x0000000000000000000000000000000000000002"
	b := "0x0000000000000000000000000000000000000003"
	c := "0x0000000000000000000000000000000000000004"
	d := "0x0000000000000000000000000000000000000005"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed: {SyncStatus: "synced"}, a: {SyncStatus: "synced"}, b: {SyncStatus: "synced"}, c: {SyncStatus: "synced"}, d: {SyncStatus: "synced"},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xa1", BlockNumber: 10, BlockTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), From: seed, To: a, AssetType: "eth", Asset: "ETH", Amount: "40"},
		{Chain: "ethereum", TxHash: "0xa2", BlockNumber: 12, BlockTime: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), From: seed, To: a, AssetType: "eth", Asset: "ETH", Amount: "30"},
		{Chain: "ethereum", TxHash: "0xa3", BlockNumber: 11, BlockTime: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC), From: seed, To: a, AssetType: "eth", Asset: "ETH", Amount: "20"},
		{Chain: "ethereum", TxHash: "0xb1", From: seed, To: b, AssetType: "eth", Asset: "ETH", Amount: "50"},
		{Chain: "ethereum", TxHash: "0xc1", From: seed, To: c, AssetType: "eth", Asset: "ETH", Amount: "10"},
		{Chain: "ethereum", TxHash: "0xd1", From: seed, To: d, AssetType: "eth", Asset: "ETH", Amount: "5"},
	}}

	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1, TopN: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 4 || len(result.Nodes) != 5 {
		t.Fatalf("edges=%+v nodes=%+v, want four counterparties", result.Edges, result.Nodes)
	}
	if result.Edges[0].TotalAmount != "90" || result.Edges[0].TransferCount != 3 {
		t.Fatalf("first edge=%+v, want total=90 count=3", result.Edges[0])
	}
	if result.Edges[0].LatestBlock != 12 || !result.Edges[0].LatestTime.Equal(time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first edge latest=%d %s", result.Edges[0].LatestBlock, result.Edges[0].LatestTime)
	}
	if result.Edges[0].FirstBlock != 10 || !result.Edges[0].FirstTime.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first edge earliest=%d %s", result.Edges[0].FirstBlock, result.Edges[0].FirstTime)
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

	result, err := New(r).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out", Depth: 1, TopN: 4})
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
	wantAssets := map[string]bool{"ETH": true, ethereumDAI: true, ethereumUSDC: true, ethereumUSDT: true, ethereumWETH: true}
	if len(r.summaryCalls) != len(wantAssets) {
		t.Fatalf("root queries=%+v, want %d assets", r.summaryCalls, len(wantAssets))
	}
	for _, call := range r.summaryCalls {
		if !wantAssets[call.Asset] || call.TopN != 4 {
			t.Fatalf("unexpected root query=%+v", call)
		}
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
	addresses    map[string]store.Address
	transfers    []store.Transfer
	labels       map[string][]store.Label
	calls        []store.TransferQuery
	summaryCalls []store.CounterpartyQuery
	bridges      map[string][]store.CrossChainLink
}

func (r *fakeRepository) TopCounterparties(_ context.Context, q store.CounterpartyQuery) ([]store.CounterpartySummary, error) {
	r.summaryCalls = append(r.summaryCalls, q)
	totals := map[string]*store.CounterpartySummary{}
	for _, transfer := range r.transfers {
		if (q.Direction == "in" && transfer.To != q.Address) || (q.Direction == "out" && transfer.From != q.Address) {
			continue
		}
		if q.AssetMode == "eth" && transfer.AssetType != "eth" && transfer.Asset != "ETH" {
			continue
		}
		if q.AssetMode == "contract" && transfer.Asset != q.Asset {
			continue
		}
		other := transfer.To
		if q.Direction == "in" {
			other = transfer.From
		}
		summary := totals[other]
		if summary == nil {
			summary = &store.CounterpartySummary{Chain: transfer.Chain, ChainID: transfer.ChainID, From: transfer.From, To: transfer.To, AssetType: transfer.AssetType, Asset: transfer.Asset, Symbol: transfer.Symbol, Decimals: transfer.Decimals, TokenMetadataComplete: transfer.TokenMetadataComplete, TotalAmount: "0", EarliestBlock: transfer.BlockNumber, EarliestTime: transfer.BlockTime, LatestBlock: transfer.BlockNumber, LatestTime: transfer.BlockTime, LatestTransfer: transfer, Representative: transfer}
			totals[other] = summary
		}
		left, _ := new(big.Int).SetString(summary.TotalAmount, 10)
		right, _ := new(big.Int).SetString(transferAmount(transfer), 10)
		summary.TotalAmount = new(big.Int).Add(left, right).String()
		summary.TransferCount++
		if transfer.BlockNumber < summary.EarliestBlock {
			summary.EarliestBlock, summary.EarliestTime = transfer.BlockNumber, transfer.BlockTime
		}
		if transfer.BlockNumber > summary.LatestBlock {
			summary.LatestBlock, summary.LatestTime, summary.LatestTransfer = transfer.BlockNumber, transfer.BlockTime, transfer
		}
		if compareTransferAmount(transfer, summary.Representative) > 0 {
			summary.Representative = transfer
		}
	}
	result := make([]store.CounterpartySummary, 0, len(totals))
	for _, summary := range totals {
		result = append(result, *summary)
	}
	sort.Slice(result, func(i, j int) bool {
		left, _ := new(big.Int).SetString(result[i].TotalAmount, 10)
		right, _ := new(big.Int).SetString(result[j].TotalAmount, 10)
		if comparison := left.Cmp(right); comparison != 0 {
			return comparison > 0
		}
		return result[i].From+result[i].To < result[j].From+result[j].To
	})
	if len(result) > q.TopN {
		result = result[:q.TopN]
	}
	return result, nil
}

func (r *fakeRepository) TopRelationshipTransfers(_ context.Context, q store.CounterpartyQuery, limit int) ([]store.Transfer, error) {
	result := make([]store.Transfer, 0)
	for _, transfer := range r.transfers {
		connected := (q.Direction == "out" && transfer.From == q.Address && transfer.To == q.Counterparty) || (q.Direction == "in" && transfer.To == q.Address && transfer.From == q.Counterparty)
		if connected {
			result = append(result, transfer)
		}
	}
	sort.Slice(result, func(i, j int) bool { return compareTransferAmount(result[i], result[j]) > 0 })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
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
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced", LatestSyncedBlock: 100}, terminal: {SyncStatus: "synced", IsTerminal: true, LatestSyncedBlock: 100}, next: {SyncStatus: "synced", LatestSyncedBlock: 100}}, transfers: []store.Transfer{{TxHash: "0x1", From: seed, To: terminal, Asset: "ETH", Amount: "2"}, {TxHash: "0x2", From: terminal, To: next, Asset: "ETH", Amount: "1"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Depth: 3, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || len(result.Edges) != 1 || len(r.summaryCalls) != 2*len(rootAssets("ethereum")) {
		t.Fatalf("result=%+v calls=%d", result, len(r.summaryCalls))
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
	if result.RuleVersion != traceRuleVersion || len(result.BridgeEdges) != 1 || len(result.Edges) != 1 || result.Edges[0].Chain != "base" || result.DataThroughBlocks["base"] != 200 {
		t.Fatalf("result=%+v", result)
	}
}

func TestTraceRootExcludesTokenMint(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	zero := "0x0000000000000000000000000000000000000000"
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced"}}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0xmint", From: zero, To: seed, AssetType: "erc20", Asset: "0x0000000000000000000000000000000000000010", TokenValue: "1"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "in", Depth: 2, TopN: 10})
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

func TestTraceRootRanksETHByAmount(t *testing.T) {
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
		{TxHash: "0xnew", BlockNumber: 3, From: seed, To: small, AssetType: "eth", Asset: "ETH", Amount: "2"},
		{TxHash: "0xold", BlockNumber: 1, From: seed, To: large, AssetType: "eth", Asset: "ETH", Amount: "100"},
		{TxHash: "0xspam", BlockNumber: 4, From: seed, To: spam, AssetType: "erc20", Asset: "0xtoken", TokenValue: "999999"},
	}}

	result, err := New(r).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1, TopN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 1 || result.Edges[0].To != large || result.Edges[0].TotalAmount != "100" {
		t.Fatalf("root edges=%+v, want largest native ETH transfer", result.Edges)
	}
	var ethQuery *store.CounterpartyQuery
	for index := range r.summaryCalls {
		if r.summaryCalls[index].AssetMode == "eth" {
			ethQuery = &r.summaryCalls[index]
		}
	}
	if len(r.summaryCalls) != len(rootAssets("ethereum")) || ethQuery == nil || ethQuery.TopN != 1 {
		t.Fatalf("root query=%+v, want amount-ranked ETH TopN", r.summaryCalls)
	}
}

func TestTraceSwitchesAssetOnlyForVerifiedContractConversion(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0x0000000000000000000000000000000000000002"
	recipient := "0x0000000000000000000000000000000000000003"
	token := "0x0000000000000000000000000000000000000010"
	r := &fakeRepository{addresses: map[string]store.Address{
		seed:      {SyncStatus: "synced"},
		router:    {SyncStatus: "synced", IsContract: true},
		recipient: {SyncStatus: "synced"},
	}, labels: map[string][]store.Label{}, transfers: []store.Transfer{
		{Chain: "ethereum", TxHash: "0xswap", BlockNumber: 10, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: "10"},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xswap", BlockNumber: 10, From: seed, To: router, Succeeded: true,
		FinalOutputAddress: recipient,
		Swaps:              []store.SwapEvent{{Verified: true, TokenIn: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2", TokenOut: token, AmountIn: "10", AmountOut: "250"}},
		Wraps:              []store.WrapEvent{{Type: "deposit", Account: seed, Amount: "10"}},
		Quality:            store.AnalysisQuality{Status: "complete", AmbiguousRoute: false},
	}}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2, TopN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 {
		t.Fatalf("edges=%+v, want ETH edge and verified conversion edge", result.Edges)
	}
	conversion := result.Edges[1]
	if conversion.Kind != "swap" || conversion.Asset != token || conversion.From != router || conversion.To != recipient || conversion.TotalAmount != "250" {
		t.Fatalf("conversion=%+v", conversion)
	}
}

func TestTraceCapsContractConversionScanAndMarksPartial(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	router := "0x0000000000000000000000000000000000000002"
	recipient := "0x0000000000000000000000000000000000000003"
	token := "0x0000000000000000000000000000000000000010"
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced"}, router: {SyncStatus: "synced", IsContract: true}, recipient: {SyncStatus: "synced"}}, labels: map[string][]store.Label{}}
	analyzer := &transactionAnalyzerMap{contract: router, analyses: make(map[string]store.TransactionAnalysis)}
	for i := 1; i <= 21; i++ {
		hash := fmt.Sprintf("0x%02d", i)
		r.transfers = append(r.transfers, store.Transfer{Chain: "ethereum", TxHash: hash, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: fmt.Sprint(i)})
		analyzer.analyses[hash] = store.TransactionAnalysis{Chain: "ethereum", TxHash: hash, From: seed, To: router, Succeeded: true, FinalOutputAddress: recipient, Swaps: []store.SwapEvent{{Verified: true, TokenIn: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2", TokenOut: token, AmountIn: fmt.Sprint(i), AmountOut: "1"}}, Wraps: []store.WrapEvent{{Type: "deposit", Account: seed, Amount: fmt.Sprint(i)}}, Quality: store.AnalysisQuality{Status: "complete"}}
	}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2, TopN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(analyzer.calls) != 20 || len(result.Edges) != 2 {
		t.Fatalf("calls=%d edges=%+v", len(analyzer.calls), result.Edges)
	}
	if result.Edges[0].ConversionStatus != "partial" || result.Edges[0].ConversionScanned != 20 {
		t.Fatalf("input edge=%+v", result.Edges[0])
	}
	if result.Edges[1].TransferCount != 20 || result.Edges[1].TotalAmount != "20" {
		t.Fatalf("conversion edge=%+v", result.Edges[1])
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
		{Chain: "ethereum", TxHash: "0xswap", BlockNumber: 10, From: seed, To: router, AssetType: "eth", Asset: "ETH", Amount: "10"},
	}}
	analyzer := transactionAnalyzerStub{analysis: store.TransactionAnalysis{
		Chain: "ethereum", TxHash: "0xswap", From: seed, To: router, Succeeded: true,
		Swaps:   []store.SwapEvent{{Verified: false, TokenIn: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2", TokenOut: token, AmountIn: "10", AmountOut: "250"}},
		Wraps:   []store.WrapEvent{{Type: "deposit", Account: seed, Amount: "10"}},
		Quality: store.AnalysisQuality{Status: "complete"},
	}}

	result, err := New(r).WithTransactionAnalyzer(analyzer).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 2, TopN: 1})
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
