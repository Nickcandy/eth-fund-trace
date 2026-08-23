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
	mu               sync.Mutex
	latestCalls      int
	actionCalls      int
	latest           int64
	failAddress      string
	maxRange         int64
	progressOnce     sync.Once
	progressSeen     chan struct{}
	progressContinue chan struct{}
}

type boundarySource struct {
	transactionRanges [][2]int64
}

func (s *boundarySource) LatestBlock(context.Context) (int64, error) { return 20, nil }

func (s *boundarySource) ListTransactions(_ context.Context, address string, start, end int64) ([]store.Transfer, error) {
	s.transactionRanges = append(s.transactionRanges, [2]int64{start, end})
	if start == 0 && end == 20 {
		return []store.Transfer{{TxHash: "0xearly", BlockNumber: 1, From: address, To: "0x0000000000000000000000000000000000000002", AssetType: "eth", Asset: "ETH", Amount: "1", Source: "txlist"}}, &etherscan.PageLimitError{Action: "txlist", MaxPages: 100, LastBlock: 9}
	}
	return []store.Transfer{{TxHash: "0xlate", BlockNumber: end, From: address, To: "0x0000000000000000000000000000000000000002", AssetType: "eth", Asset: "ETH", Amount: "1", Source: "txlist"}}, nil
}

func (*boundarySource) ListInternalTransactions(context.Context, string, int64, int64) ([]store.Transfer, error) {
	return nil, nil
}

func (*boundarySource) ListTokenTransfers(context.Context, string, int64, int64) ([]store.Transfer, error) {
	return nil, nil
}

func (f *fakeSource) transfersWithProgress(ctx context.Context, address, source string, start, end int64, progress etherscan.ProgressFunc) ([]store.Transfer, error) {
	transfers, err := f.transfers(address, source, start, end)
	if err == nil && progress != nil {
		progress(etherscan.PageProgress{Action: source, Address: address, StartBlock: start, EndBlock: end, Page: 1, Items: len(transfers)})
		f.progressOnce.Do(func() {
			if f.progressSeen != nil {
				close(f.progressSeen)
			}
			if f.progressContinue != nil {
				select {
				case <-ctx.Done():
				case <-f.progressContinue:
				}
			}
		})
	}
	return transfers, err
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

func (f *fakeSource) ListTransactionsWithProgress(ctx context.Context, address string, start, end int64, progress etherscan.ProgressFunc) ([]store.Transfer, error) {
	return f.transfersWithProgress(ctx, address, "txlist", start, end, progress)
}

func (f *fakeSource) ListInternalTransactionsWithProgress(ctx context.Context, address string, start, end int64, progress etherscan.ProgressFunc) ([]store.Transfer, error) {
	return f.transfersWithProgress(ctx, address, "txlistinternal", start, end, progress)
}

func (f *fakeSource) ListTokenTransfersWithProgress(ctx context.Context, address string, start, end int64, progress etherscan.ProgressFunc) ([]store.Transfer, error) {
	return f.transfersWithProgress(ctx, address, "tokentx", start, end, progress)
}

type memoryRepository struct {
	mu                    sync.Mutex
	addresses             map[string]store.Address
	jobs                  map[primitive.ObjectID]store.SyncJob
	transfers             []store.Transfer
	neighbors             []string
	omitEmptyActionCounts bool
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
	job := r.jobs[id]
	if r.omitEmptyActionCounts && len(job.ActionCounts) == 0 {
		job.ActionCounts = nil
	}
	return job, nil
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
	profileCalls := 0
	manager := New(source, repository, Config{
		CacheTTL: 15 * time.Minute, Confirmations: 12, QueueSize: 10, Clock: func() time.Time { return now },
		AfterAddressSynced: func(context.Context, string, string) error { profileCalls++; return nil },
	})
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
	if profileCalls != 2 {
		t.Fatalf("profile calls = %d, want one after sync and one cached snapshot check", profileCalls)
	}
}

func TestManagerExposesPageProgressBeforeAddressCompletes(t *testing.T) {
	seen, proceed := make(chan struct{}), make(chan struct{})
	source := &fakeSource{latest: 100, progressSeen: seen, progressContinue: proceed}
	repository := newMemoryRepository()
	manager := New(source, repository, Config{Confirmations: 12, QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: "0x0000000000000000000000000000000000000001"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("page progress was not reported")
	}
	current, err := manager.Job(context.Background(), job.ID.Hex())
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "running" || current.Progress.PagesFetched != 1 || current.Progress.RecordsRead != 1 || current.Progress.CurrentAction != "txlist" || current.Progress.RangeEnd != 88 {
		t.Fatalf("progress = %+v, status = %s", current.Progress, current.Status)
	}
	close(proceed)
	completed := waitForJob(t, manager, job.ID.Hex())
	if completed.Progress.RecordsWritten != 3 || completed.Progress.PagesFetched != 3 {
		t.Fatalf("completed progress = %+v", completed.Progress)
	}
}

func TestManagerHandlesOmittedEmptyActionCountsAfterPersistence(t *testing.T) {
	source := &fakeSource{latest: 100}
	repository := newMemoryRepository()
	repository.omitEmptyActionCounts = true
	manager := New(source, repository, Config{CacheTTL: time.Minute, Confirmations: 12, QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: "0x0000000000000000000000000000000000000001", NeighborLimit: 0})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" || len(job.ActionCounts) != 3 {
		t.Fatalf("job=%+v", job)
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

func TestManagerPersistsCompleteBlocksAndResumesAtRawPageBoundary(t *testing.T) {
	source := &boundarySource{}
	repository := newMemoryRepository()
	manager := New(source, repository, Config{QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: "0x0000000000000000000000000000000000000001"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" || job.Fetched != 2 {
		t.Fatalf("job = %+v", job)
	}
	wantRanges := [][2]int64{{0, 20}, {9, 20}}
	if fmt.Sprint(source.transactionRanges) != fmt.Sprint(wantRanges) {
		t.Fatalf("transaction ranges = %v, want %v", source.transactionRanges, wantRanges)
	}
	if len(repository.transfers) != 2 || repository.transfers[0].BlockNumber != 1 || repository.transfers[1].BlockNumber != 20 {
		t.Fatalf("transfers = %+v, want early and late records", repository.transfers)
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
	if job.Status != "partial" || job.CompletedAddresses != 2 || job.ProcessedAddresses != 3 || len(job.FailedNeighbors) != 1 || job.FailedNeighbors[0].Address != failing || job.FailedNeighbors[0].Code != "etherscan_rate_limited" {
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

func TestManagerSynchronizesConfiguredBaseSource(t *testing.T) {
	repository := newMemoryRepository()
	baseSource := &fakeSource{latest: 20}
	manager := NewMulti(map[string]Source{"base": baseSource}, repository, Config{Confirmations: 2})
	seed := "0x0000000000000000000000000000000000000001"
	job, err := manager.Enqueue(context.Background(), Request{Chain: "base", Address: seed})
	if err != nil || job.ChainID != 8453 {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()
	completed := waitForJob(t, manager, job.ID.Hex())
	if completed.Status != "succeeded" || len(repository.transfers) == 0 || repository.transfers[0].Chain != "base" || repository.transfers[0].ChainID != 8453 {
		t.Fatalf("job=%+v transfers=%+v", completed, repository.transfers)
	}
}
