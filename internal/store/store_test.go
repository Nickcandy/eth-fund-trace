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
		{name: "label", model: Label{}, required: []string{"chain", "chainId", "address", "type", "source", "confidence", "observedAt"}},
		{name: "sync job", model: SyncJob{}, required: []string{"chain", "chainId", "address", "status", "createdAt", "fetched"}},
		{name: "address profile", model: AddressProfile{}, required: []string{"chain", "chainId", "address", "ruleVersion", "dataThroughBlock", "features", "score", "classification", "suspectedHotWallet", "computedAt"}},
		{name: "trace job", model: TraceJob{}, required: []string{"chain", "seedAddress", "direction", "depth", "topN", "status", "ruleVersion"}},
		{name: "transaction analysis", model: TransactionAnalysis{}, required: []string{"chain", "chainId", "txHash", "value", "transfers", "swaps", "wraps", "quality", "analyzedAt"}},
		{name: "pool metadata", model: PoolMetadata{}, required: []string{"chain", "pool", "token0", "token1", "fee", "factory", "verified", "observedAt"}},
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
	if AddressesCollection != "addresses" || TransfersCollection != "transfers" || LabelsCollection != "labels" || SyncJobsCollection != "sync_jobs" || ProfilesCollection != "address_profiles" || TraceJobsCollection != "trace_jobs" || TransactionAnalysesCollection != "transaction_analyses" || PoolMetadataCollection != "pool_metadata" {
		t.Fatal("unexpected collection name")
	}
}

func TestIndexModels(t *testing.T) {
	indexes := indexModels()
	if len(indexes[TransfersCollection]) != 7 {
		t.Fatalf("transfer index count = %d, want 7", len(indexes[TransfersCollection]))
	}
	if unique := indexes[TransfersCollection][0].Options.Unique; unique == nil || !*unique {
		t.Fatal("transfer identity index must be unique")
	}
	if unique := indexes[AddressesCollection][0].Options.Unique; unique == nil || !*unique {
		t.Fatal("address identity index must be unique")
	}
	if len(indexes[AddressesCollection]) != 2 {
		t.Fatalf("address index count = %d, want 2", len(indexes[AddressesCollection]))
	}
	if unique := indexes[LabelsCollection][0].Options.Unique; unique == nil || !*unique {
		t.Fatal("label identity index must be unique")
	}
	for _, collection := range []string{TransactionAnalysesCollection, PoolMetadataCollection} {
		if unique := indexes[collection][0].Options.Unique; unique == nil || !*unique {
			t.Fatalf("%s identity index must be unique", collection)
		}
	}
}

func TestLegacySyncCheckpointsRequireKnownCurrentAction(t *testing.T) {
	job := SyncJob{StartBlock: 100, SafeHead: 200}
	if got := legacySyncCheckpoints(job); len(got) != 0 {
		t.Fatalf("expected no inferred checkpoints before an action starts, got %v", got)
	}

	job.Progress.CurrentAction = "unknown"
	if got := legacySyncCheckpoints(job); len(got) != 0 {
		t.Fatalf("expected no inferred checkpoints for an unknown action, got %v", got)
	}
}

func TestLegacySyncCheckpointsResumeWithinObservedRange(t *testing.T) {
	job := SyncJob{StartBlock: 100, SafeHead: 200, Progress: SyncProgress{
		CurrentAction: "txlistinternal",
		RangeStart:    151,
		RangeEnd:      180,
	}}
	got := legacySyncCheckpoints(job)
	if got["txlist"] != 180 || got["txlistinternal"] != 150 {
		t.Fatalf("unexpected inferred checkpoints: %v", got)
	}
	if _, found := got["tokentx"]; found {
		t.Fatalf("future action must not have a checkpoint: %v", got)
	}
}
