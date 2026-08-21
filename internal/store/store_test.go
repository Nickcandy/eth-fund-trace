package store

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func TestTransferUsesStringAmountsAndBSONFields(t *testing.T) {
	doc, err := bson.Marshal(Transfer{Chain: "ethereum", Amount: "1000000000000000000", TokenValue: "42", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	var raw bson.M
	if err := bson.Unmarshal(doc, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["amount"].(string); !ok {
		t.Fatalf("amount type = %T, want string", raw["amount"])
	}
	if _, ok := raw["tokenValue"].(string); !ok {
		t.Fatalf("tokenValue type = %T, want string", raw["tokenValue"])
	}
}

func TestModelBSONFields(t *testing.T) {
	tests := []struct {
		name     string
		model    any
		required []string
	}{
		{name: "address", model: Address{}, required: []string{"chain", "chainId", "address", "syncStatus"}},
		{name: "transfer", model: Transfer{}, required: []string{"chain", "chainId", "txHash", "from", "to", "assetType", "asset", "source", "observedAt"}},
		{name: "label", model: Label{}, required: []string{"chain", "chainId", "address", "type", "source", "observedAt"}},
		{name: "sync job", model: SyncJob{}, required: []string{"chain", "chainId", "address", "status", "startedAt", "fetched"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := bson.Marshal(tt.model)
			if err != nil {
				t.Fatal(err)
			}
			var raw bson.M
			if err := bson.Unmarshal(doc, &raw); err != nil {
				t.Fatal(err)
			}
			for _, field := range tt.required {
				if _, ok := raw[field]; !ok {
					t.Errorf("missing BSON field %q", field)
				}
			}
		})
	}
}

func TestCollectionNames(t *testing.T) {
	if AddressesCollection != "addresses" || TransfersCollection != "transfers" || LabelsCollection != "labels" || SyncJobsCollection != "sync_jobs" {
		t.Fatal("unexpected collection name")
	}
}

func TestIndexModels(t *testing.T) {
	indexes := indexModels()
	if len(indexes[TransfersCollection]) != 5 {
		t.Fatalf("transfer index count = %d, want 5", len(indexes[TransfersCollection]))
	}
	if unique := indexes[TransfersCollection][0].Options.Unique; unique == nil || !*unique {
		t.Fatal("transfer identity index must be unique")
	}
	if unique := indexes[AddressesCollection][0].Options.Unique; unique == nil || !*unique {
		t.Fatal("address identity index must be unique")
	}
	if unique := indexes[LabelsCollection][0].Options.Unique; unique == nil || !*unique {
		t.Fatal("label identity index must be unique")
	}
}
