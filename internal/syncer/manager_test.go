package syncer

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeSource struct {
	mu          sync.Mutex
	latestCalls int
	actionCalls int
	latest      int64
	failAddress string
	maxRange    int64
}

func (f *fakeSource) LatestBlock(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.latestCalls++
	return f.latest, nil
}

func (f *fakeSource) transfers(address, source string, start, end int64) ([]store.Transfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actionCalls++
	if address == f.failAddress {
		return nil, etherscan.ErrRateLimited
	}
	if f.maxRange > 0 && end-start+1 > f.maxRange {
		return nil, etherscan.ErrPageLimit
	}
	return []store.Transfer{{Chain: "ethereum", ChainID: 1, TxHash: fmt.Sprintf("0x%s%d", source, end), BlockNumber: end, From: address, To: "0x0000000000000000000000000000000000000002", AssetType: "eth", Asset: "ETH", Amount: "1", Source: source}}, nil
}

func (f *fakeSource) ListTransactions(_ context.Context, address string, start, end int64) ([]store.Transfer, error) {
	return f.transfers(address, "txlist", start, end)
}

func (f *fakeSource) ListInternalTransactions(_ context.Context, address string, start, end int64) ([]store.Transfer, error) {
	return f.transfers(address, "txlistinternal", start, end)
}

func (f *fakeSource) ListTokenTransfers(_ context.Context, address string, start, end int64) ([]store.Transfer, error) {
	return f.transfers(address, "tokentx", start, end)
}

type memoryRepository struct {
	mu        sync.Mutex
	addresses map[string]store.Address
	jobs      map[primitive.ObjectID]store.SyncJob
	transfers []store.Transfer
	neighbors []string
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{addresses: make(map[string]store.Address), jobs: make(map[primitive.ObjectID]store.SyncJob)}
}

func (r *memoryRepository) FindAddress(_ context.Context, _, address string) (store.Address, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.addresses[address]
	return value, ok, nil
}

func (r *memoryRepository) SetAddressSyncing(_ context.Context, chain string, chainID int64, address string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.addresses[address]
	value.Chain, value.ChainID, value.Address, value.SyncStatus = chain, chainID, address, "running"
	r.addresses[address] = value
	return nil
}

func (r *memoryRepository) CompleteAddressSync(_ context.Context, _, address string, earliest, latest int64, syncedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.addresses[address]
	value.EarliestSyncedBlock, value.LatestSyncedBlock, value.HistorySyncedToBlock = earliest, latest, latest
	value.LastSyncedAt, value.SyncStatus = syncedAt, "synced"
	r.addresses[address] = value
	return nil
}

func (r *memoryRepository) FailAddressSync(context.Context, string, string, string) error { return nil }

func (r *memoryRepository) BulkUpsertTransfers(_ context.Context, transfers []store.Transfer) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transfers = append(r.transfers, transfers...)
	return int64(len(transfers)), nil
}

func (r *memoryRepository) UpsertDiscoveredAddresses(context.Context, string, int64, []string, time.Time) error {
	return nil
}

func (r *memoryRepository) TopNeighbors(context.Context, string, string, int) ([]string, error) {
	return r.neighbors, nil
}

func (r *memoryRepository) CreateSyncJob(_ context.Context, job *store.SyncJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job.ID.IsZero() {
		job.ID = primitive.NewObjectID()
	}
	r.jobs[job.ID] = *job
	return nil
}

func (r *memoryRepository) GetSyncJob(_ context.Context, id primitive.ObjectID) (store.SyncJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id], nil
}

func (r *memoryRepository) SaveSyncJob(_ context.Context, job store.SyncJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[job.ID] = job
	return nil
}

func (r *memoryRepository) FailInterruptedJobs(context.Context, time.Time) error { return nil }

func TestManagerSyncsSeedAndReusesFreshCache(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	source := &fakeSource{latest: 100}
	repository := newMemoryRepository()
	manager := New(source, repository, Config{CacheTTL: 15 * time.Minute, Confirmations: 12, QueueSize: 10, Clock: func() time.Time { return now }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	request := Request{Chain: "ethereum", Address: "0x0000000000000000000000000000000000000001", StartBlock: 0, NeighborLimit: 0}
	first, err := manager.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	first = waitForJob(t, manager, first.ID.Hex())
	if first.Status != "succeeded" || first.SafeHead != 88 || first.Fetched != 3 || first.CachedAddresses != 0 {
		t.Fatalf("unexpected first job: %+v", first)
	}

	second, err := manager.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second = waitForJob(t, manager, second.ID.Hex())
	if second.Status != "succeeded" || second.CachedAddresses != 1 {
		t.Fatalf("unexpected cached job: %+v", second)
	}
	if source.latestCalls != 1 || source.actionCalls != 3 {
		t.Fatalf("latest calls = %d, action calls = %d, want 1 and 3", source.latestCalls, source.actionCalls)
	}
}

func TestManagerSplitsRangesAtPageLimit(t *testing.T) {
	source := &fakeSource{latest: 20, maxRange: 5}
	repository := newMemoryRepository()
	manager := New(source, repository, Config{CacheTTL: time.Minute, QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: "0x0000000000000000000000000000000000000001"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" || job.Fetched <= 3 || source.actionCalls <= 3 {
		t.Fatalf("job=%+v actionCalls=%d, want split successful ranges", job, source.actionCalls)
	}
}

func TestManagerKeepsSeedWhenNeighborFails(t *testing.T) {
	failing := "0x0000000000000000000000000000000000000003"
	source := &fakeSource{latest: 100, failAddress: failing}
	repository := newMemoryRepository()
	repository.neighbors = []string{"0x0000000000000000000000000000000000000002", failing}
	manager := New(source, repository, Config{CacheTTL: time.Minute, Confirmations: 12, QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: "0x0000000000000000000000000000000000000001", NeighborLimit: 2})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "partial" || job.CompletedAddresses != 2 || len(job.FailedNeighbors) != 1 || job.FailedNeighbors[0].Address != failing || job.FailedNeighbors[0].Code != "etherscan_rate_limited" {
		t.Fatalf("unexpected partial job: %+v", job)
	}
}

func TestManagerBackfillsHistoryAndAdvancesLatestBlock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	seed := "0x0000000000000000000000000000000000000001"
	source := &fakeSource{latest: 200}
	repository := newMemoryRepository()
	repository.addresses[seed] = store.Address{
		Chain: "ethereum", ChainID: 1, Address: seed, SyncStatus: "synced",
		EarliestSyncedBlock: 100, LatestSyncedBlock: 150, HistorySyncedToBlock: 150,
		LastSyncedAt: now.Add(-time.Hour),
	}
	manager := New(source, repository, Config{CacheTTL: 15 * time.Minute, Confirmations: 12, QueueSize: 10, Clock: func() time.Time { return now }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 50})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	address := repository.addresses[seed]
	if job.Status != "succeeded" || address.EarliestSyncedBlock != 50 || address.LatestSyncedBlock != 188 || source.actionCalls != 6 {
		t.Fatalf("job=%+v address=%+v actionCalls=%d", job, address, source.actionCalls)
	}
}

func waitForJob(t *testing.T, manager *Manager, id string) store.SyncJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.Job(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" || job.Status == "partial" || job.Status == "failed" {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
	return store.SyncJob{}
}
