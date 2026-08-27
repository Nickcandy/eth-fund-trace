//go:build integration

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestListTransferAssetsReturnsIndependentETHAndTokenChannels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_asset_channels_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer db.Drop(context.Background())
	address := "0x0000000000000000000000000000000000000001"
	token := "0xdac17f958d2ee523a2206206994597c13d831ec7"
	if _, err := s.BulkUpsertTransfers(ctx, []Transfer{
		{Chain: "ethereum", ChainID: 1, TxHash: "0xeth", BlockNumber: 10, From: address, To: "0x0000000000000000000000000000000000000002", AssetType: "eth", Asset: "ETH", Amount: "1", Source: "txlist"},
		{Chain: "ethereum", ChainID: 1, TxHash: "0xtoken", BlockNumber: 11, From: address, To: "0x0000000000000000000000000000000000000003", AssetType: "erc20", Asset: token, TokenValue: "2", Source: "tokentx", LogIndex: 1},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := s.ListTransferAssets(ctx, "ethereum", address, "out", 11, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []AssetChannel{{AssetMode: "eth", Asset: "ETH"}, {AssetMode: "contract", Asset: token}}
	if len(result.Items) != len(want) || result.Truncated {
		t.Fatalf("result=%+v", result)
	}
	for index := range want {
		if result.Items[index] != want[index] {
			t.Fatalf("items=%+v want=%+v", result.Items, want)
		}
	}
	bounded, err := s.ListTransferAssets(ctx, "ethereum", address, "out", 11, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bounded.Truncated || len(bounded.Items) != 1 || bounded.Items[0] != (AssetChannel{AssetMode: "eth", Asset: "ETH"}) {
		t.Fatalf("bounded=%+v", bounded)
	}
}

func TestTransferUpsertIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}

	db := client.Database("eth_fund_trace_m1_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	transfer := Transfer{Chain: "ethereum", TxHash: "0xabc", Source: "txlist", Asset: "ETH", Amount: "1", ObservedAt: time.Now()}
	if err := s.UpsertTransfer(ctx, transfer); err != nil {
		t.Fatal(err)
	}
	transfer.Amount = "2"
	if err := s.UpsertTransfer(ctx, transfer); err != nil {
		t.Fatal(err)
	}
	count, err := db.Collection(TransfersCollection).CountDocuments(ctx, bson.D{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("transfer count = %d, want 1", count)
	}

	assertDuplicateKey(t, ctx, db.Collection(AddressesCollection), Address{Chain: "ethereum", Address: "0x1"})
	assertDuplicateKey(t, ctx, db.Collection(LabelsCollection), Label{Chain: "ethereum", Address: "0x1", Type: "exchange", Source: "manual"})
}

func TestTransactionAnalysisCachesAreIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_m11_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	analysis := TransactionAnalysis{Chain: "ethereum", TxHash: "0xabc", Value: "1", Swaps: []SwapEvent{}, Transfers: []ReceiptTransfer{}, Wraps: []WrapEvent{}}
	if err := s.SaveTransactionAnalysis(ctx, analysis); err != nil {
		t.Fatal(err)
	}
	analysis.Value = "2"
	if err := s.SaveTransactionAnalysis(ctx, analysis); err != nil {
		t.Fatal(err)
	}
	metadata := PoolMetadata{Chain: "ethereum", Pool: "0xpool", Verified: true}
	if err := s.SavePoolMetadata(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePoolMetadata(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	for collection, want := range map[string]int64{TransactionAnalysesCollection: 1, PoolMetadataCollection: 1} {
		count, err := db.Collection(collection).CountDocuments(ctx, bson.D{})
		if err != nil || count != want {
			t.Fatalf("%s count=%d err=%v", collection, count, err)
		}
	}
	if cached, found, err := s.FindTransactionAnalysis(ctx, "ethereum", "0xabc"); err != nil || !found || cached.Value != "2" {
		t.Fatalf("cached=%+v found=%v err=%v", cached, found, err)
	}
}

func TestM3BulkNeighborsAndInterruptedJobs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_m3_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	seed := "0x0000000000000000000000000000000000000001"
	neighborA := "0x0000000000000000000000000000000000000002"
	neighborB := "0x0000000000000000000000000000000000000003"
	transfers := []Transfer{
		{Chain: "ethereum", TxHash: "0x1", Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: neighborA, Amount: "1"},
		{Chain: "ethereum", TxHash: "0x2", Source: "txlist", Asset: "ETH", AssetType: "eth", From: neighborA, To: seed, Amount: "2"},
		{Chain: "ethereum", TxHash: "0x3", Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: neighborB, Amount: "3"},
		{Chain: "ethereum", TxHash: "0x4", Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: neighborB, Amount: "0"},
	}
	if _, err := s.BulkUpsertTransfers(ctx, transfers); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BulkUpsertTransfers(ctx, transfers); err != nil {
		t.Fatal(err)
	}
	count, err := db.Collection(TransfersCollection).CountDocuments(ctx, bson.D{})
	if err != nil || count != 4 {
		t.Fatalf("count=%d err=%v, want 4", count, err)
	}
	neighbors, err := s.TopNeighbors(ctx, "ethereum", seed, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 2 || neighbors[0] != neighborA || neighbors[1] != neighborB {
		t.Fatalf("neighbors=%v, want interaction-count ordering", neighbors)
	}
	neighbors, err = s.TopNeighbors(ctx, "ethereum", seed, 0)
	if err != nil || len(neighbors) != 0 {
		t.Fatalf("zero-limit neighbors=%v err=%v, want empty", neighbors, err)
	}

	job := SyncJob{Chain: "ethereum", ChainID: 1, Address: seed, Status: "running", CreatedAt: time.Now()}
	if err := s.CreateSyncJob(ctx, &job); err != nil {
		t.Fatal(err)
	}
	if err := s.FailInterruptedJobs(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	job, err = s.GetSyncJob(ctx, job.ID)
	if err != nil || job.Status != "failed" || job.ErrorCode != "interrupted" || !job.Retryable {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestM4ActivityAggregationAndProfileSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_m4_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	seed := "0x0000000000000000000000000000000000000001"
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	transfers := []Transfer{
		{Chain: "ethereum", TxHash: "0xold", Source: "txlist", Asset: "ETH", AssetType: "eth", From: "0x2", To: seed, Amount: "1", BlockTime: base.Add(-40 * 24 * time.Hour)},
		{Chain: "ethereum", TxHash: "0xin", Source: "txlist", Asset: "ETH", AssetType: "eth", From: "0x2", To: seed, Amount: "2", BlockTime: base},
		{Chain: "ethereum", TxHash: "0xout", Source: "tokentx", Asset: "0xtoken", AssetType: "erc20", From: seed, To: "0x3", TokenValue: "3", BlockTime: base.Add(24 * time.Hour)},
		{Chain: "ethereum", TxHash: "0xzero", Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: "0x4", Amount: "0", BlockTime: base.Add(48 * time.Hour)},
	}
	if _, err := s.BulkUpsertTransfers(ctx, transfers); err != nil {
		t.Fatal(err)
	}
	activity, err := s.AddressActivity(ctx, "ethereum", seed)
	if err != nil {
		t.Fatal(err)
	}
	features := activity.Features
	if features.LifetimeTransfers != 3 || features.WindowTransfers != 2 || features.Incoming != 1 || features.Outgoing != 1 || features.UniqueCounterparts != 2 || features.ActiveDays != 2 || features.ETHTransfers != 1 || features.ERC20Transfers != 1 {
		t.Fatalf("activity=%+v", activity)
	}
	profile := AddressProfile{Chain: "ethereum", Address: seed, RuleVersion: "hot-wallet-v1", DataThroughBlock: 10, Score: 12, ComputedAt: time.Now()}
	if err := s.SaveAddressProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	profile.Score = 15
	if err := s.SaveAddressProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	stored, found, err := s.FindAddressProfile(ctx, "ethereum", seed, "hot-wallet-v1", 10)
	if err != nil || !found || stored.Score != 15 {
		t.Fatalf("profile=%+v found=%v err=%v", stored, found, err)
	}
}

func TestM5TransferQueryFiltersAndPaginates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_m5_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	seed := "0x0000000000000000000000000000000000000001"
	neighbor := "0x0000000000000000000000000000000000000002"
	token := "0x0000000000000000000000000000000000000010"
	transfers := []Transfer{
		{Chain: "ethereum", TxHash: "0xff", BlockNumber: 12, Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: neighbor, Amount: "0"},
		{Chain: "ethereum", TxHash: "0xcc", BlockNumber: 11, Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: neighbor, Amount: "3"},
		{Chain: "ethereum", TxHash: "0xbb", BlockNumber: 10, Source: "txlistinternal", TraceID: "1", Asset: "ETH", AssetType: "eth", From: neighbor, To: seed, Amount: "2"},
		{Chain: "ethereum", TxHash: "0xbb", BlockNumber: 10, Source: "tokentx", LogIndex: 2, Asset: token, AssetType: "erc20", From: seed, To: neighbor, TokenValue: "4"},
		{Chain: "ethereum", TxHash: "0xaa", BlockNumber: 9, Source: "txlist", Asset: "ETH", AssetType: "eth", From: neighbor, To: seed, Amount: "1"},
	}
	if _, err := s.BulkUpsertTransfers(ctx, transfers); err != nil {
		t.Fatal(err)
	}

	first, err := s.QueryTransfers(ctx, TransferQuery{Chain: "ethereum", Addresses: []string{seed}, Direction: "both", AssetMode: "all", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].TxHash != "0xcc" || first[1].Source != "txlistinternal" {
		t.Fatalf("first page=%+v", first)
	}
	after := TransferCursor{BlockNumber: first[1].BlockNumber, TxHash: first[1].TxHash, Source: first[1].Source, TraceID: first[1].TraceID, LogIndex: first[1].LogIndex, Asset: first[1].Asset}
	second, err := s.QueryTransfers(ctx, TransferQuery{Chain: "ethereum", Addresses: []string{seed}, Direction: "both", AssetMode: "all", Limit: 10, After: &after})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[0].Source != "tokentx" || second[1].TxHash != "0xaa" {
		t.Fatalf("second page=%+v", second)
	}

	outgoingToken, err := s.QueryTransfers(ctx, TransferQuery{Chain: "ethereum", Addresses: []string{seed}, Direction: "out", AssetMode: "contract", Asset: token, Limit: 10})
	if err != nil || len(outgoingToken) != 1 || outgoingToken[0].Source != "tokentx" {
		t.Fatalf("token edges=%+v err=%v", outgoingToken, err)
	}
	incomingETH, err := s.QueryTransfers(ctx, TransferQuery{Chain: "ethereum", Addresses: []string{seed}, Direction: "in", AssetMode: "eth", FromBlock: 10, ToBlock: 10, Limit: 10})
	if err != nil || len(incomingETH) != 1 || incomingETH[0].Source != "txlistinternal" {
		t.Fatalf("incoming ETH=%+v err=%v", incomingETH, err)
	}
	multiAddress, err := s.QueryTransfers(ctx, TransferQuery{Chain: "ethereum", Addresses: []string{seed, neighbor}, Direction: "out", AssetMode: "erc20", Limit: 10})
	if err != nil || len(multiAddress) != 1 {
		t.Fatalf("multi-address ERC-20=%+v err=%v", multiAddress, err)
	}
	neighborB := "0x0000000000000000000000000000000000000003"
	neighborC := "0x0000000000000000000000000000000000000004"
	if _, err := s.BulkUpsertTransfers(ctx, []Transfer{
		{Chain: "ethereum", TxHash: "0x01", BlockNumber: 1, Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: neighbor, Amount: "100"},
		{Chain: "ethereum", TxHash: "0x02", BlockNumber: 1, Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: neighborB, Amount: "60"},
		{Chain: "ethereum", TxHash: "0x03", BlockNumber: 1, Source: "txlist", Asset: "ETH", AssetType: "eth", From: seed, To: neighborC, Amount: "50"},
	}); err != nil {
		t.Fatal(err)
	}
	topETH, err := s.TopCounterparties(ctx, CounterpartyQuery{Chain: "ethereum", Address: seed, Direction: "out", AssetMode: "eth", Asset: "ETH"})
	if err != nil || len(topETH) != 2 || topETH[0].To != neighbor || topETH[0].TotalAmount != "103" || topETH[0].TransferCount != 2 || topETH[1].To != neighborB {
		t.Fatalf("amount-ranked counterparties=%+v err=%v", topETH, err)
	}
}

func TestM6M7TraceJobsAndLabels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_m6_m7_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	address := "0x0000000000000000000000000000000000000001"
	label := Label{Chain: "ethereum", ChainID: 1, Address: address, Type: "hacker", RiskLevel: "high", Confidence: 0.8, Source: "manual", Evidence: []string{"case-1"}, ObservedAt: time.Now()}
	storedLabel, err := s.UpsertLabel(ctx, label)
	if err != nil {
		t.Fatal(err)
	}
	if storedLabel.ID.IsZero() {
		t.Fatal("upserted label is missing its persisted ID")
	}
	label.Confidence = 1
	replacedLabel, err := s.UpsertLabel(ctx, label)
	if err != nil {
		t.Fatal(err)
	}
	if replacedLabel.ID != storedLabel.ID {
		t.Fatalf("label ID changed across upsert: %s != %s", replacedLabel.ID.Hex(), storedLabel.ID.Hex())
	}
	labels, err := s.ListLabels(ctx, "ethereum", address)
	if err != nil || len(labels) != 1 || labels[0].Confidence != 1 {
		t.Fatalf("labels=%+v err=%v", labels, err)
	}

	job := TraceJob{Chain: "ethereum", SeedAddress: address, Direction: "both", Depth: 3, Status: "running", RuleVersion: "trace-v1", CreatedAt: time.Now()}
	if err := s.CreateTraceJob(ctx, &job); err != nil {
		t.Fatal(err)
	}
	if err := s.FailInterruptedTraceJobs(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetTraceJob(ctx, job.ID)
	if err != nil || stored.Status != "failed" || stored.ErrorCode != "interrupted" {
		t.Fatalf("job=%+v err=%v", stored, err)
	}
}

func TestPropagationJobResultDecodesAsJSONObject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_propagation_result_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	job := PropagationJob{
		IdempotencyKey: "result-shape", Chain: "ethereum", TargetAddress: "0x0000000000000000000000000000000000000001",
		Status: "succeeded", Result: bson.D{{Key: "associations", Value: bson.A{}}, {Key: "propagationVersion", Value: "propagation-v2"}},
	}
	if err := s.CreatePropagationJob(ctx, &job); err != nil {
		t.Fatal(err)
	}
	for name, load := range map[string]func() (PropagationJob, error){
		"by ID":  func() (PropagationJob, error) { return s.GetPropagationJob(ctx, job.ID) },
		"by key": func() (PropagationJob, error) { return s.FindPropagationJobByKey(ctx, job.IdempotencyKey) },
	} {
		t.Run(name, func(t *testing.T) {
			stored, err := load()
			if err != nil {
				t.Fatal(err)
			}
			result, ok := stored.Result.(map[string]any)
			if !ok {
				t.Fatalf("result type = %T, want JSON object", stored.Result)
			}
			if _, ok := result["associations"].([]any); !ok {
				t.Fatalf("associations type = %T, want array", result["associations"])
			}
		})
	}
}

func TestM9CrossChainLinksAreIdempotentAndChainScoped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_m9_test")
	if err := db.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	sharedAddress := "0x0000000000000000000000000000000000000009"
	if _, err := db.Collection(AddressesCollection).InsertMany(ctx, []any{Address{Chain: "ethereum", ChainID: 1, Address: sharedAddress}, Address{Chain: "base", ChainID: 8453, Address: sharedAddress}}); err != nil {
		t.Fatalf("same address must be isolated by chain: %v", err)
	}
	link := CrossChainLink{SourceChain: "ethereum", SourceChainID: 1, SourceTxHash: "0xsource", SourceLogIndex: 1, SourceAddress: "0x0000000000000000000000000000000000000001", SourceAsset: "ETH", SourceAmount: "2", TargetChain: "base", TargetChainID: 8453, TargetTxHash: "0xtarget", TargetLogIndex: 2, TargetAddress: "0x0000000000000000000000000000000000000002", TargetAsset: "ETH", TargetAmount: "2", BridgeAddress: "0x0000000000000000000000000000000000000003", Status: "confirmed", Evidence: []string{"provider:1"}, ObservedAt: time.Now()}
	first, err := s.UpsertCrossChainLink(ctx, link)
	if err != nil {
		t.Fatal(err)
	}
	link.TargetAmount = "3"
	second, err := s.UpsertCrossChainLink(ctx, link)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("ids differ: %s %s", first.ID, second.ID)
	}
	source, err := s.ListCrossChainLinks(ctx, "ethereum", link.SourceAddress, 10)
	if err != nil || len(source) != 1 || source[0].TargetAmount != "3" {
		t.Fatalf("source=%+v err=%v", source, err)
	}
	wrongChain, err := s.ListCrossChainLinks(ctx, "base", link.SourceAddress, 10)
	if err != nil || len(wrongChain) != 0 {
		t.Fatalf("wrongChain=%+v err=%v", wrongChain, err)
	}
}

func assertDuplicateKey(t *testing.T, ctx context.Context, collection *mongo.Collection, document any) {
	t.Helper()
	if _, err := collection.InsertOne(ctx, document); err != nil {
		t.Fatal(err)
	}
	if _, err := collection.InsertOne(ctx, document); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("error = %v, want duplicate key", err)
	}
}
