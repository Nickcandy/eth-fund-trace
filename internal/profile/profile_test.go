package profile

import (
	"context"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

type profileRepository struct {
	address  store.Address
	activity store.AddressActivity
	saved    store.AddressProfile
	found    bool
}

func (r *profileRepository) FindAddress(context.Context, string, string) (store.Address, bool, error) {
	return r.address, r.address.SyncStatus != "", nil
}

func (r *profileRepository) FindAddressProfile(context.Context, string, string, string, int64) (store.AddressProfile, bool, error) {
	return r.saved, r.found, nil
}

func (r *profileRepository) AddressActivity(context.Context, string, string) (store.AddressActivity, error) {
	return r.activity, nil
}

func (r *profileRepository) SaveAddressProfile(_ context.Context, value store.AddressProfile) error {
	r.saved, r.found = value, true
	return nil
}

func TestProfilerClassifiesHotWalletSignals(t *testing.T) {
	tests := []struct {
		name           string
		features       store.ProfileFeatures
		classification string
		score          int
		suspected      bool
	}{
		{name: "insufficient", features: store.ProfileFeatures{WindowTransfers: 9}, classification: "insufficient_data", score: 0},
		{name: "low signal", features: store.ProfileFeatures{WindowTransfers: 30, Incoming: 15, Outgoing: 15, UniqueCounterparts: 20, UniqueSenders: 10, UniqueRecipients: 10, ActiveDays: 5}, classification: "low_signal", score: 37},
		{name: "high activity candidate", features: store.ProfileFeatures{WindowTransfers: 100, Incoming: 96, Outgoing: 4, UniqueCounterparts: 50, UniqueSenders: 20, UniqueRecipients: 20, ActiveDays: 10}, classification: "high_activity_candidate", score: 65},
		{name: "suspected hot wallet", features: store.ProfileFeatures{WindowTransfers: 300, Incoming: 150, Outgoing: 150, UniqueCounterparts: 200, UniqueSenders: 50, UniqueRecipients: 50, ActiveDays: 20}, classification: "suspected_hot_wallet", score: 100, suspected: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &profileRepository{
				address:  store.Address{Chain: "ethereum", Address: "0x1", SyncStatus: "synced", LatestSyncedBlock: 100},
				activity: store.AddressActivity{LatestTransferAt: time.Unix(1_700_000_000, 0), Features: tt.features},
			}
			profiler := New(repository, func() time.Time { return time.Unix(1_700_000_100, 0) })
			result, err := profiler.Get(context.Background(), "ethereum", "0x1")
			if err != nil {
				t.Fatal(err)
			}
			if result.Classification != tt.classification || result.Score != tt.score || result.SuspectedHotWallet != tt.suspected || result.RuleVersion != RuleVersion {
				t.Fatalf("profile=%+v", result)
			}
		})
	}
}

func TestProfilerReusesVersionedSnapshot(t *testing.T) {
	existing := store.AddressProfile{Chain: "ethereum", Address: "0x1", RuleVersion: RuleVersion, DataThroughBlock: 100, Score: 77}
	repository := &profileRepository{address: store.Address{SyncStatus: "synced", LatestSyncedBlock: 100}, saved: existing, found: true}
	result, err := New(repository, time.Now).Get(context.Background(), "ethereum", "0x1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != 77 {
		t.Fatalf("profile=%+v, want cached snapshot", result)
	}
}
