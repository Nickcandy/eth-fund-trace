package profile

import (
	"context"
	"errors"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

const RuleVersion = "hot-wallet-v1"

var ErrAddressNotSynced = errors.New("address is not synced")

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	FindAddressProfile(context.Context, string, string, string, int64) (store.AddressProfile, bool, error)
	AddressActivity(context.Context, string, string) (store.AddressActivity, error)
	SaveAddressProfile(context.Context, store.AddressProfile) error
}

type Profiler struct {
	repository Repository
	clock      func() time.Time
}

func New(repository Repository, clock func() time.Time) *Profiler {
	if clock == nil {
		clock = time.Now
	}
	return &Profiler{repository: repository, clock: clock}
}

func (p *Profiler) Get(ctx context.Context, chain, address string) (store.AddressProfile, error) {
	metadata, found, err := p.repository.FindAddress(ctx, chain, address)
	if err != nil {
		return store.AddressProfile{}, err
	}
	if !found || (metadata.SyncStatus != "synced" && metadata.LastSyncedAt.IsZero()) {
		return store.AddressProfile{}, ErrAddressNotSynced
	}
	if existing, found, err := p.repository.FindAddressProfile(ctx, chain, address, RuleVersion, metadata.LatestSyncedBlock); err != nil {
		return store.AddressProfile{}, err
	} else if found {
		return existing, nil
	}
	activity, err := p.repository.AddressActivity(ctx, chain, address)
	if err != nil {
		return store.AddressProfile{}, err
	}
	result := evaluate(chain, address, metadata.LatestSyncedBlock, activity, p.clock().UTC())
	if err := p.repository.SaveAddressProfile(ctx, result); err != nil {
		return store.AddressProfile{}, err
	}
	return result, nil
}

func evaluate(chain, address string, dataThroughBlock int64, activity store.AddressActivity, computedAt time.Time) store.AddressProfile {
	features := activity.Features
	result := store.AddressProfile{
		Chain: chain, ChainID: 1, Address: address, RuleVersion: RuleVersion,
		DataThroughBlock: dataThroughBlock, WindowEnd: activity.LatestTransferAt,
		Features: features, ComputedAt: computedAt,
	}
	if !activity.LatestTransferAt.IsZero() {
		result.WindowStart = activity.LatestTransferAt.Add(-30 * 24 * time.Hour)
	}
	if features.WindowTransfers < 10 {
		result.Classification = "insufficient_data"
		return result
	}

	result.Score += tier(int(features.WindowTransfers), []threshold{{10, 5}, {30, 12}, {100, 22}, {300, 30}})
	result.Score += tier(features.UniqueCounterparts, []threshold{{10, 5}, {20, 10}, {50, 18}, {200, 25}})
	result.Score += tier(features.ActiveDays, []threshold{{5, 5}, {10, 10}, {20, 15}})
	collection := behaviorScore(features.UniqueSenders, features.Outgoing)
	distribution := behaviorScore(features.UniqueRecipients, features.Incoming)
	result.Score += collection + distribution

	if features.WindowTransfers >= 100 {
		result.Signals = append(result.Signals, "high_frequency")
	}
	if features.UniqueCounterparts >= 50 {
		result.Signals = append(result.Signals, "many_counterparties")
	}
	if collection >= 10 {
		result.Signals = append(result.Signals, "collection_pattern")
	}
	if distribution >= 10 {
		result.Signals = append(result.Signals, "distribution_pattern")
	}

	switch {
	case result.Score >= 70 && features.Incoming >= 5 && features.Outgoing >= 5:
		result.Classification, result.SuspectedHotWallet = "suspected_hot_wallet", true
	case result.Score >= 50:
		result.Classification = "high_activity_candidate"
	default:
		result.Classification = "low_signal"
	}
	return result
}

type threshold struct {
	minimum int
	points  int
}

func tier(value int, thresholds []threshold) int {
	points := 0
	for _, item := range thresholds {
		if value >= item.minimum {
			points = item.points
		}
	}
	return points
}

func behaviorScore(counterparties int, oppositeDirection int64) int {
	if oppositeDirection < 1 {
		return 0
	}
	switch {
	case counterparties >= 50:
		return 15
	case counterparties >= 20:
		return 10
	case counterparties >= 10:
		return 5
	default:
		return 0
	}
}
