package propagation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type engineRepository struct {
	addresses  map[string]store.Address
	labels     map[string][]store.Label
	assets     map[string]store.AssetChannelResult
	candidates map[string]store.CandidateResult
	analyses   map[string]store.TransactionAnalysis
	bridges    map[string][]store.CrossChainLink
	queries    []store.CandidateQuery
}

func (r *engineRepository) ListTransferAssets(_ context.Context, chain, address, direction string, _ int64, limit int) (store.AssetChannelResult, error) {
	result := r.assets[chain+":"+address+":"+direction]
	if len(result.Items) > limit {
		result.Items = append([]store.AssetChannel(nil), result.Items[:limit]...)
		result.Truncated = true
	}
	return result, nil
}

func (r *engineRepository) FindAddress(_ context.Context, chain, address string) (store.Address, bool, error) {
	value, ok := r.addresses[nodeKey(chain, address)]
	return value, ok, nil
}
func (r *engineRepository) ListLabels(_ context.Context, chain, address string) ([]store.Label, error) {
	return r.labels[nodeKey(chain, address)], nil
}
func (r *engineRepository) ListRiskLabels(_ context.Context, chain string, _ int64) ([]store.Label, error) {
	var result []store.Label
	for key, labels := range r.labels {
		if strings.HasPrefix(key, chain+":") {
			result = append(result, labels...)
		}
	}
	return result, nil
}
func (r *engineRepository) PropagationCandidates(_ context.Context, query store.CandidateQuery) (store.CandidateResult, error) {
	r.queries = append(r.queries, query)
	return r.candidates[query.Chain+":"+query.Address+":"+query.Direction+":"+query.Asset], nil
}
func (r *engineRepository) ListCrossChainLinks(_ context.Context, chain, address string, _ int64) ([]store.CrossChainLink, error) {
	return r.bridges[nodeKey(chain, address)], nil
}
func (r *engineRepository) FindTransactionAnalysis(_ context.Context, chain, hash string) (store.TransactionAnalysis, bool, error) {
	value, ok := r.analyses[chain+":"+hash]
	return value, ok, nil
}

func TestScoringFactors(t *testing.T) {
	if sourceBase(store.Label{RiskLevel: "high"}) != 100 || sourceBase(store.Label{RiskLevel: "medium"}) != 60 || sourceBase(store.Label{RiskLevel: "low"}) != 0 {
		t.Fatal("source base factors changed")
	}
	for distance, want := range map[int]float64{1: 1, 2: 0.65, 3: 0.4} {
		if got := hopFactor(distance); got != want {
			t.Fatalf("hop %d=%f want=%f", distance, got, want)
		}
	}
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	for days, want := range map[int]float64{30: 1, 31: 0.8, 181: 0.6, 366: 0.4} {
		if got := timeFactor(now.Add(-time.Duration(days)*24*time.Hour), now); got != want {
			t.Fatalf("days %d=%f want=%f", days, got, want)
		}
	}
	for part, want := range map[string]float64{"100": 1, "10": 0.75, "1": 0.5, "0": 0.25} {
		if got := amountFactor(part, "1000"); got != want {
			t.Fatalf("part %s=%f want=%f", part, got, want)
		}
	}
}

func TestAggregateAssociationsDeduplicatesSourceAndSaturates(t *testing.T) {
	items := []Association{{SourceAddress: "0x1", Score: 60}, {SourceAddress: "0x1", Score: 30}, {SourceAddress: "0x2", Score: 50}}
	if got := aggregateAssociations(items); got != 80 {
		t.Fatalf("aggregate=%d want=80", got)
	}
}

func TestRunFindsRiskSourceFromTargetAndScoresPath(t *testing.T) {
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	target, middle, source := address(1), address(2), address(3)
	label := riskLabel(source, "high", 1, now)
	repository := graphRepository([]string{target, middle, source})
	repository.labels[nodeKey("ethereum", source)] = []store.Label{label}
	repository.candidates[queryKey(target, "out", "ETH")] = candidate(target, middle, "0x1", "100", "100", now)
	repository.candidates[queryKey(middle, "out", "ETH")] = candidate(middle, source, "0x2", "100", "100", now)
	engine := NewEngine(repository)
	engine.clock = func() time.Time { return now }
	result, err := engine.Run(context.Background(), "ethereum", target, "out", "ETH", 100, nil, nil, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "complete" || result.Score != 65 || result.Level != "medium" || len(result.Associations) < 2 {
		t.Fatalf("result=%+v", result)
	}
	root := assessmentByKey(result.Nodes, nodeKey("ethereum", target))
	if len(root.Associations) != 1 || root.Associations[0].Path.Factors.HopFactor != 0.65 {
		t.Fatalf("root=%+v", root)
	}
}

func TestRunRiskTargetScoresItsSynchronizedDownstream(t *testing.T) {
	now := time.Now().UTC()
	source, downstream := address(1), address(2)
	repository := graphRepository([]string{source, downstream})
	repository.labels[nodeKey("ethereum", source)] = []store.Label{riskLabel(source, "high", 1, now)}
	repository.candidates[queryKey(source, "out", "ETH")] = candidate(source, downstream, "0x1", "100", "100", now)
	engine := NewEngine(repository)
	engine.clock = func() time.Time { return now }
	result, err := engine.Run(context.Background(), "ethereum", source, "out", "ETH", 100, nil, nil, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != 100 || assessmentByKey(result.Nodes, nodeKey("ethereum", downstream)).Score != 100 {
		t.Fatalf("result=%+v", result)
	}
}

func TestRunAllAssetsPropagatesEachObservedTransferAssetIndependently(t *testing.T) {
	now := time.Now().UTC()
	source, middle, downstream := address(1), address(2), address(3)
	usdt := "0xdac17f958d2ee523a2206206994597c13d831ec7"
	repository := graphRepository([]string{source, middle, downstream})
	repository.labels[nodeKey("ethereum", source)] = []store.Label{riskLabel(source, "high", 1, now)}
	repository.assets["ethereum:"+source+":out"] = store.AssetChannelResult{Items: []store.AssetChannel{
		{AssetMode: "eth", Asset: "ETH"},
		{AssetMode: "contract", Asset: usdt},
	}}
	repository.candidates[queryKey(source, "out", usdt)] = tokenCandidate(source, middle, "0x1", usdt, "100", "100", now)
	repository.candidates[queryKey(middle, "out", usdt)] = tokenCandidate(middle, downstream, "0x2", usdt, "100", "100", now)

	result, err := NewEngine(repository).Run(context.Background(), "ethereum", source, "out", "all", 100, nil, nil, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assessment := assessmentByKey(result.Nodes, nodeKey("ethereum", downstream))
	if assessment.Score != 65 || len(assessment.Associations) != 1 || assessment.Associations[0].Asset != usdt {
		t.Fatalf("downstream=%+v", assessment)
	}
}

func TestRunAllAssetsDoesNotSwitchAtAnUnverifiedIntermediateAddress(t *testing.T) {
	now := time.Now().UTC()
	source, middle, unrelated := address(1), address(2), address(3)
	usdt := "0xdac17f958d2ee523a2206206994597c13d831ec7"
	repository := graphRepository([]string{source, middle, unrelated})
	repository.labels[nodeKey("ethereum", source)] = []store.Label{riskLabel(source, "high", 1, now)}
	repository.assets["ethereum:"+source+":out"] = store.AssetChannelResult{Items: []store.AssetChannel{{AssetMode: "eth", Asset: "ETH"}}}
	repository.candidates[queryKey(source, "out", "ETH")] = candidate(source, middle, "0x1", "100", "100", now)
	repository.candidates[queryKey(middle, "out", usdt)] = tokenCandidate(middle, unrelated, "0x2", usdt, "100", "100", now)

	result, err := NewEngine(repository).Run(context.Background(), "ethereum", source, "out", "all", 100, nil, nil, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := assessmentByKey(result.Nodes, nodeKey("ethereum", unrelated)); got.Score != 0 || got.Address != "" {
		t.Fatalf("unrelated asset propagated without conversion: %+v", got)
	}
}

func TestRunReportsUnknownForMissingCandidateAndStopsPublicNode(t *testing.T) {
	now := time.Now().UTC()
	target, missing := address(1), address(2)
	repository := graphRepository([]string{target})
	repository.candidates[queryKey(target, "out", "ETH")] = candidate(target, missing, "0x1", "1", "100", now)
	engine := NewEngine(repository)
	engine.clock = func() time.Time { return now }
	result, err := engine.Run(context.Background(), "ethereum", target, "out", "ETH", 100, nil, nil, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "unknown" || len(result.MissingAddresses) != 1 {
		t.Fatalf("result=%+v", result)
	}

	router, beyond := address(3), address(4)
	repository = graphRepository([]string{target, router, beyond})
	repository.labels[nodeKey("ethereum", router)] = []store.Label{{Type: "router", Source: "public-list"}}
	repository.candidates[queryKey(target, "out", "ETH")] = candidate(target, router, "0x2", "100", "100", now)
	repository.candidates[queryKey(router, "out", "ETH")] = candidate(router, beyond, "0x3", "100", "100", now)
	result, err = NewEngine(repository).Run(context.Background(), "ethereum", target, "out", "ETH", 100, nil, nil, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := assessment(repository, result, beyond); ok {
		t.Fatalf("public node expanded: %+v", result.Nodes)
	}
}

func TestRunAppliesVerifiedSwapFactor(t *testing.T) {
	now := time.Now().UTC()
	target, router, source := address(1), address(2), address(3)
	token := address(99)
	repository := graphRepository([]string{target, source})
	repository.labels[nodeKey("ethereum", source)] = []store.Label{riskLabel(source, "high", 1, now)}
	repository.candidates[queryKey(target, "out", "ETH")] = candidate(target, router, "0xswap", "100", "100", now)
	repository.analyses["ethereum:0xswap"] = store.TransactionAnalysis{Succeeded: true, Quality: store.AnalysisQuality{Status: "complete"}, Swaps: []store.SwapEvent{{Verified: true, TokenIn: "ETH", TokenOut: token, OutputAddress: source}}}
	engine := NewEngine(repository)
	engine.clock = func() time.Time { return now }
	result, err := engine.Run(context.Background(), "ethereum", target, "out", "ETH", 100, nil, nil, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != 90 || result.Associations[0].Path.Factors.ProtocolFactor != 0.9 {
		t.Fatalf("result=%+v", result)
	}
}

func TestResultJSONUsesFrontendFieldNames(t *testing.T) {
	encoded, err := json.Marshal(Result{Status: "complete", Nodes: []NodeRiskAssessment{{Address: address(1)}}, Associations: []Association{{SourceLabelID: primitive.NewObjectID().Hex()}}})
	if err != nil {
		t.Fatal(err)
	}
	if text := string(encoded); !strings.Contains(text, `"sourceLabelId"`) || !strings.Contains(text, `"directRisk"`) || strings.Contains(text, `"SourceLabelID"`) {
		t.Fatalf("json=%s", text)
	}
}

func TestRunSerializesEmptyCollectionsAsArrays(t *testing.T) {
	target := address(1)
	repository := graphRepository([]string{target})
	result, err := NewEngine(repository).Run(context.Background(), "ethereum", target, "out", "ETH", 100, nil, nil, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, field := range []string{`"associations":[]`, `"missingAddresses":[]`, `"labels":[]`} {
		if !strings.Contains(text, field) {
			t.Fatalf("json=%s, want %s", text, field)
		}
	}
}

func graphRepository(addresses []string) *engineRepository {
	r := &engineRepository{addresses: map[string]store.Address{}, labels: map[string][]store.Label{}, assets: map[string]store.AssetChannelResult{}, candidates: map[string]store.CandidateResult{}, analyses: map[string]store.TransactionAnalysis{}, bridges: map[string][]store.CrossChainLink{}}
	for _, value := range addresses {
		r.addresses[nodeKey("ethereum", value)] = store.Address{Chain: "ethereum", Address: value, SyncStatus: "synced", NormalSyncedTo: 100, InternalSyncedTo: 100, TokenSyncedTo: 100}
	}
	return r
}

func tokenCandidate(from, to, hash, asset, amount, total string, at time.Time) store.CandidateResult {
	transfer := store.Transfer{Chain: "ethereum", From: from, To: to, AssetType: "erc20", Asset: asset, TokenValue: amount, TxHash: hash, BlockNumber: 100, BlockTime: at}
	return store.CandidateResult{Items: []store.CounterpartySummary{{Chain: "ethereum", From: from, To: to, AssetType: "erc20", Asset: asset, TotalAmount: amount, TransferCount: 1, LatestBlock: 100, LatestTime: at, LatestTransfer: transfer, Representative: transfer}}, Coverage: store.CandidateCoverage{SelectedCounterparties: 1, TotalCounterparties: 1, SelectedAmount: amount, TotalAmount: total, AmountCoverage: ratio(amount, total)}}
}

func candidate(from, to, hash, amount, total string, at time.Time) store.CandidateResult {
	transfer := store.Transfer{Chain: "ethereum", From: from, To: to, AssetType: "eth", Asset: "ETH", Amount: amount, TxHash: hash, BlockNumber: 100, BlockTime: at}
	return store.CandidateResult{Items: []store.CounterpartySummary{{Chain: "ethereum", From: from, To: to, AssetType: "eth", Asset: "ETH", TotalAmount: amount, TransferCount: 1, LatestBlock: 100, LatestTime: at, LatestTransfer: transfer, Representative: transfer}}, Coverage: store.CandidateCoverage{SelectedCounterparties: 1, TotalCounterparties: 1, SelectedAmount: amount, TotalAmount: total, AmountCoverage: ratio(amount, total)}}
}

func summary(from, to, hash string) store.CounterpartySummary {
	return candidate(from, to, hash, "1", "1", time.Now().UTC()).Items[0]
}

func riskLabel(value, level string, confidence float64, observed time.Time) store.Label {
	return store.Label{ID: primitive.NewObjectID(), Chain: "ethereum", Address: value, Type: "sanctions", Source: "manual", RiskLevel: level, Confidence: confidence, ObservedAt: observed}
}

func queryKey(value, direction, asset string) string {
	return "ethereum:" + value + ":" + direction + ":" + asset
}
func address(index int) string { return "0x" + strings.Repeat("0", 39) + string(rune('0'+index)) }
func ratio(part, total string) string {
	p, _ := new(bigInt).set(part)
	t, _ := new(bigInt).set(total)
	return formatRatio(p.value, t.value)
}

type bigInt struct{ value float64 }

func (b *bigInt) set(value string) (*bigInt, bool) {
	_, err := fmt.Sscan(value, &b.value)
	return b, err == nil
}
func formatRatio(part, total float64) string {
	if total == 0 {
		return "0"
	}
	return fmt.Sprintf("%.4f", part/total)
}
func assessment(_ *engineRepository, result Result, value string) (NodeRiskAssessment, bool) {
	for _, item := range result.Nodes {
		if item.Address == value {
			return item, true
		}
	}
	return NodeRiskAssessment{}, false
}
