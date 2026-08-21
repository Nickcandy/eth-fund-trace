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
		{name: "high activity candidate", features: store.ProfileFeatures{WindowTransfers: 100, Incoming: 96, Outgoing: 4, UniqueCounterparts: 50, UniqueSenders: 20, UniqueRecipients: 20, ActiveDays: 10}, classification: "high_activity_candidate", score: 70},
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

func TestProfilerScoresBehaviorThresholdsWithAnyOppositeActivity(t *testing.T) {
	for _, tt := range []struct {
		name    string
		senders int
		points  int
	}{
		{name: "ten senders", senders: 10, points: 5},
		{name: "twenty senders", senders: 20, points: 10},
		{name: "fifty senders", senders: 50, points: 15},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repository := &profileRepository{
				address: store.Address{SyncStatus: "synced", LatestSyncedBlock: 100},
				activity: store.AddressActivity{LatestTransferAt: time.Unix(1_700_000_000, 0), Features: store.ProfileFeatures{
					WindowTransfers: 10, Incoming: 9, Outgoing: 1, UniqueSenders: tt.senders,
				}},
			}
			result, err := New(repository, time.Now).Get(context.Background(), "ethereum", "0x1")
			if err != nil {
				t.Fatal(err)
			}
			if result.Score != 5+tt.points {
				t.Fatalf("score=%d, want %d", result.Score, 5+tt.points)
			}
		})
	}
}

func TestProfilerAppliesEveryScoreThreshold(t *testing.T) {
	tests := []struct {
		name     string
		features store.ProfileFeatures
		score    int
	}{
		{name: "transactions 10", features: store.ProfileFeatures{WindowTransfers: 10}, score: 5},
		{name: "transactions 30", features: store.ProfileFeatures{WindowTransfers: 30}, score: 12},
		{name: "transactions 100", features: store.ProfileFeatures{WindowTransfers: 100}, score: 22},
		{name: "transactions 300", features: store.ProfileFeatures{WindowTransfers: 300}, score: 30},
		{name: "counterparties 10", features: store.ProfileFeatures{WindowTransfers: 10, UniqueCounterparts: 10}, score: 10},
		{name: "counterparties 20", features: store.ProfileFeatures{WindowTransfers: 10, UniqueCounterparts: 20}, score: 15},
		{name: "counterparties 50", features: store.ProfileFeatures{WindowTransfers: 10, UniqueCounterparts: 50}, score: 23},
		{name: "counterparties 200", features: store.ProfileFeatures{WindowTransfers: 10, UniqueCounterparts: 200}, score: 30},
		{name: "active days 5", features: store.ProfileFeatures{WindowTransfers: 10, ActiveDays: 5}, score: 10},
		{name: "active days 10", features: store.ProfileFeatures{WindowTransfers: 10, ActiveDays: 10}, score: 15},
		{name: "active days 20", features: store.ProfileFeatures{WindowTransfers: 10, ActiveDays: 20}, score: 20},
		{name: "recipients 10", features: store.ProfileFeatures{WindowTransfers: 10, Incoming: 1, UniqueRecipients: 10}, score: 10},
		{name: "recipients 20", features: store.ProfileFeatures{WindowTransfers: 10, Incoming: 1, UniqueRecipients: 20}, score: 15},
		{name: "recipients 50", features: store.ProfileFeatures{WindowTransfers: 10, Incoming: 1, UniqueRecipients: 50}, score: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &profileRepository{
				address:  store.Address{SyncStatus: "synced", LatestSyncedBlock: 100},
				activity: store.AddressActivity{LatestTransferAt: time.Unix(1_700_000_000, 0), Features: tt.features},
			}
			result, err := New(repository, time.Now).Get(context.Background(), "ethereum", "0x1")
			if err != nil {
				t.Fatal(err)
			}
			if result.Score != tt.score {
				t.Fatalf("score=%d, want %d", result.Score, tt.score)
			}
		})
	}
}

func TestProfilerUsesPreviouslySyncedDataAfterRefreshFailure(t *testing.T) {
	repository := &profileRepository{
		address:  store.Address{SyncStatus: "failed", LatestSyncedBlock: 90, LastSyncedAt: time.Unix(1, 0)},
		activity: store.AddressActivity{Features: store.ProfileFeatures{WindowTransfers: 9}},
	}
	result, err := New(repository, time.Now).Get(context.Background(), "ethereum", "0x1")
	if err != nil {
		t.Fatal(err)
	}
	if result.DataThroughBlock != 90 {
		t.Fatalf("dataThroughBlock=%d, want 90", result.DataThroughBlock)
	}
}
