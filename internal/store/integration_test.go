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

func assertDuplicateKey(t *testing.T, ctx context.Context, collection *mongo.Collection, document any) {
	t.Helper()
	if _, err := collection.InsertOne(ctx, document); err != nil {
		t.Fatal(err)
	}
	if _, err := collection.InsertOne(ctx, document); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("error = %v, want duplicate key", err)
	}
}
