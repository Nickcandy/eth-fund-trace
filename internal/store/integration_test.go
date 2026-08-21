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

func assertDuplicateKey(t *testing.T, ctx context.Context, collection *mongo.Collection, document any) {
	t.Helper()
	if _, err := collection.InsertOne(ctx, document); err != nil {
		t.Fatal(err)
	}
	if _, err := collection.InsertOne(ctx, document); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("error = %v, want duplicate key", err)
	}
}
