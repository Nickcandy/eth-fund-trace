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
}

func TestM6M7TraceJobsAndLabels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	if err != nil { t.Fatal(err) }
	defer client.Disconnect(context.Background())
	db := client.Database("eth_fund_trace_m6_m7_test")
	if err := db.Drop(ctx); err != nil { t.Fatal(err) }
	s := New(db)
	if err := s.Initialize(ctx); err != nil { t.Fatal(err) }

	address := "0x0000000000000000000000000000000000000001"
	label := Label{Chain: "ethereum", ChainID: 1, Address: address, Type: "hacker", RiskLevel: "high", Confidence: 0.8, Source: "manual", Evidence: []string{"case-1"}, ObservedAt: time.Now()}
	if err := s.UpsertLabel(ctx, label); err != nil { t.Fatal(err) }
	label.Confidence = 1
	if err := s.UpsertLabel(ctx, label); err != nil { t.Fatal(err) }
	labels, err := s.ListLabels(ctx, "ethereum", address)
	if err != nil || len(labels) != 1 || labels[0].Confidence != 1 { t.Fatalf("labels=%+v err=%v", labels, err) }

	job := TraceJob{Chain: "ethereum", SeedAddress: address, Direction: "both", Depth: 3, TopN: 10, Status: "running", RuleVersion: "trace-v1", CreatedAt: time.Now()}
	if err := s.CreateTraceJob(ctx, &job); err != nil { t.Fatal(err) }
	if err := s.FailInterruptedTraceJobs(ctx, time.Now()); err != nil { t.Fatal(err) }
	stored, err := s.GetTraceJob(ctx, job.ID)
	if err != nil || stored.Status != "failed" || stored.ErrorCode != "interrupted" { t.Fatalf("job=%+v err=%v", stored, err) }
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
