package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
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

type recentSource struct{}

type recentBoundarySource struct {
	ranges map[string][][2]int64
}

type recordingSource struct {
	fakeSource
	ranges       [][2]int64
	actionRanges map[string][][2]int64
}

func (s *recordingSource) ListTransactions(_ context.Context, address string, start, end int64) ([]store.Transfer, error) {
	s.ranges = append(s.ranges, [2]int64{start, end})
	return s.transfers(address, "txlist", start, end)
}
func (s *recordingSource) ListTransactionsWithProgress(ctx context.Context, address string, start, end int64, progress etherscan.ProgressFunc) ([]store.Transfer, error) {
	s.ranges = append(s.ranges, [2]int64{start, end})
	s.record("txlist", start, end)
	return s.transfersWithProgress(ctx, address, "txlist", start, end, progress)
}

func (s *recordingSource) ListInternalTransactionsWithProgress(ctx context.Context, address string, start, end int64, progress etherscan.ProgressFunc) ([]store.Transfer, error) {
	s.record("txlistinternal", start, end)
	return s.transfersWithProgress(ctx, address, "txlistinternal", start, end, progress)
}

func (s *recordingSource) ListTokenTransfersWithProgress(ctx context.Context, address string, start, end int64, progress etherscan.ProgressFunc) ([]store.Transfer, error) {
	s.record("tokentx", start, end)
	return s.transfersWithProgress(ctx, address, "tokentx", start, end, progress)
}

func (s *recordingSource) record(action string, start, end int64) {
	if s.actionRanges == nil {
		s.actionRanges = make(map[string][][2]int64)
	}
	s.actionRanges[action] = append(s.actionRanges[action], [2]int64{start, end})
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

func (*recentSource) LatestBlock(context.Context) (int64, error) { return 20, nil }

func (*recentSource) ListTransactions(_ context.Context, address string, _, _ int64) ([]store.Transfer, error) {
	return recentTransfers(address, "txlist"), nil
}

func (*recentSource) ListInternalTransactions(_ context.Context, address string, _, _ int64) ([]store.Transfer, error) {
	return recentTransfers(address, "txlistinternal"), nil
}

func (*recentSource) ListTokenTransfers(_ context.Context, address string, _, _ int64) ([]store.Transfer, error) {
	return recentTransfers(address, "tokentx"), nil
}

func recentTransfers(address, source string) []store.Transfer {
	result := make([]store.Transfer, 0, 5)
	for block := int64(20); block >= 16; block-- {
		result = append(result, store.Transfer{TxHash: fmt.Sprintf("0x%s%d", source, block), BlockNumber: block, From: address, To: "0x0000000000000000000000000000000000000002", AssetType: "eth", Asset: "ETH", Amount: "1", Source: source})
	}
	return result
}

func (s *recentBoundarySource) LatestBlock(context.Context) (int64, error) { return 20, nil }

func (s *recentBoundarySource) ListTransactions(_ context.Context, address string, start, end int64) ([]store.Transfer, error) {
	return s.records(address, "txlist", start, end)
}

func (s *recentBoundarySource) ListInternalTransactions(_ context.Context, address string, start, end int64) ([]store.Transfer, error) {
	return s.records(address, "txlistinternal", start, end)
}

func (s *recentBoundarySource) ListTokenTransfers(_ context.Context, address string, start, end int64) ([]store.Transfer, error) {
	return s.records(address, "tokentx", start, end)
}

func (s *recentBoundarySource) records(address, action string, start, end int64) ([]store.Transfer, error) {
	if s.ranges == nil {
		s.ranges = make(map[string][][2]int64)
	}
	s.ranges[action] = append(s.ranges[action], [2]int64{start, end})
	blocks := []int64{end, end - 1, end - 2}
	result := make([]store.Transfer, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, store.Transfer{TxHash: fmt.Sprintf("0x%s%d", action, block), BlockNumber: block, From: address, To: "0x0000000000000000000000000000000000000002", AssetType: "eth", Asset: "ETH", Amount: "1", Source: action})
	}
	if end == 20 {
		return result, &etherscan.PageLimitError{Action: action, MaxPages: 1, LastBlock: 18}
	}
	return result, nil
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

func (r *memoryRepository) EnsureAddressActionCounts(_ context.Context, chain string, chainID int64, address string, limit int64) (map[string]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.addresses[address]
	if value.ActionRecordCounts != nil {
		return cloneActionCounts(value.ActionRecordCounts), nil
	}
	counts := map[string]int64{"txlist": 0, "txlistinternal": 0, "tokentx": 0}
	for _, transfer := range r.transfers {
		if transfer.Chain == chain && (transfer.From == address || transfer.To == address) && counts[transfer.Source] < limit {
			counts[transfer.Source]++
		}
	}
	value.Chain, value.ChainID, value.Address = chain, chainID, address
	value.ActionRecordCounts = counts
	r.addresses[address] = value
	return cloneActionCounts(counts), nil
}

func cloneActionCounts(counts map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(counts))
	for action, count := range counts {
		result[action] = count
	}
	return result
}

func (r *memoryRepository) AddAddressActionRecords(_ context.Context, _, address, action string, count int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.addresses[address]
	if value.ActionRecordCounts == nil {
		value.ActionRecordCounts = make(map[string]int64)
	}
	value.ActionRecordCounts[action] += count
	r.addresses[address] = value
	return nil
}

func (r *memoryRepository) FindSyncCheckpoints(_ context.Context, chain, address string, startBlock, maxRecordsPerAction int64) (map[string]int64, error) {
	var latest store.SyncJob
	for _, job := range r.jobs {
		if job.Chain == chain && job.Address == address && job.StartBlock == startBlock && job.CreatedAt.After(latest.CreatedAt) {
			latest = job
		}
	}
	result := map[string]int64{}
	if latest.Status != "failed" && latest.Status != "stopped" {
		return result, nil
	}
	if latest.CoverageVersion != store.SyncCoverageVersion {
		return result, nil
	}
	if latest.MaxRecordsPerAction != maxRecordsPerAction {
		return result, nil
	}
	for k, v := range latest.Progress.ActionCheckpoints {
		result[k] = v
	}
	return result, nil
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{addresses: make(map[string]store.Address), jobs: make(map[primitive.ObjectID]store.SyncJob)}
}

func TestFetchRangeHonorsRecordLimit(t *testing.T) {
	repository := newMemoryRepository()
	manager := &Manager{repository: repository}
	call := func(context.Context, string, int64, int64) ([]store.Transfer, error) {
		transfers := make([]store.Transfer, 10)
		for i := range transfers {
			transfers[i] = store.Transfer{TxHash: fmt.Sprintf("0x%d", i), From: "0x0000000000000000000000000000000000000001", To: "0x0000000000000000000000000000000000000002", AssetType: "eth", Asset: "ETH", Amount: "1"}
		}
		return transfers, nil
	}
	written, err := manager.fetchRange(etherscan.WithRecordLimit(context.Background(), 3), call, "txlist", "ethereum", 1, "0x0000000000000000000000000000000000000001", 0, 10, map[string]int64{}, map[string]struct{}{}, func(func(*store.SyncProgress)) {})
	if err != nil {
		t.Fatal(err)
	}
	if written != 3 || len(repository.transfers) != 3 {
		t.Fatalf("written=%d transfers=%d, want hard limit 3", written, len(repository.transfers))
	}
}

func TestFetchRecentRangePassesRecordLimitToSource(t *testing.T) {
	repository := newMemoryRepository()
	manager := &Manager{repository: repository, config: Config{MaxRecordsPerAction: 2}}
	observedLimit := int64(0)
	call := func(ctx context.Context, _ string, _, _ int64) ([]store.Transfer, error) {
		observedLimit = etherscan.RecordLimit(ctx)
		transfers := make([]store.Transfer, 4)
		for i := range transfers {
			transfers[i] = store.Transfer{TxHash: fmt.Sprintf("0x%d", i), From: "0x0000000000000000000000000000000000000001", To: "0x0000000000000000000000000000000000000002", AssetType: "eth", Asset: "ETH", Amount: "1"}
		}
		if observedLimit > 0 {
			return transfers[:observedLimit], etherscan.ErrRecordLimit
		}
		return transfers, nil
	}

	written, truncated, err := manager.fetchRecentRange(context.Background(), call, "txlist", "ethereum", 1, "0x0000000000000000000000000000000000000001", 0, 10, 2, map[string]int64{}, map[string]struct{}{}, func(func(*store.SyncProgress)) {})
	if err != nil {
		t.Fatal(err)
	}
	if observedLimit != 2 || written != 2 || !truncated {
		t.Fatalf("source limit=%d written=%d truncated=%v, want 2, 2, true", observedLimit, written, truncated)
	}
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

func (r *memoryRepository) CompleteAddressSync(_ context.Context, _, address string, coverage store.AddressSyncCoverage, syncedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.addresses[address]
	earliest := max(coverage.NormalFrom, max(coverage.InternalFrom, coverage.TokenFrom))
	latest := min(coverage.NormalTo, min(coverage.InternalTo, coverage.TokenTo))
	if latest < earliest {
		return fmt.Errorf("invalid address sync coverage")
	}
	value.NormalSyncedFrom, value.NormalSyncedTo = coverage.NormalFrom, coverage.NormalTo
	value.InternalSyncedFrom, value.InternalSyncedTo = coverage.InternalFrom, coverage.InternalTo
	value.TokenSyncedFrom, value.TokenSyncedTo = coverage.TokenFrom, coverage.TokenTo
	value.LastSyncedAt, value.SyncStatus = syncedAt, "synced"
	r.addresses[address] = value
	return nil
}

func (r *memoryRepository) CompleteAddressPartial(_ context.Context, _, address string, _ int64, maxRecordsPerAction int64, reason string, syncedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.addresses[address]
	value.LastSyncedAt, value.SyncStatus, value.SyncError = syncedAt, "partial", reason
	value.SyncMaxRecordsPerAction = maxRecordsPerAction
	r.addresses[address] = value
	return nil
}

func (r *memoryRepository) FailAddressSync(_ context.Context, _, address, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.addresses[address]
	value.SyncStatus, value.SyncError = "failed", message
	r.addresses[address] = value
	return nil
}

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

func (r *memoryRepository) FindLatestSyncJob(_ context.Context, chain, address string) (store.SyncJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest store.SyncJob
	found := false
	for _, job := range r.jobs {
		if job.Chain == chain && job.Address == address && (!found || job.CreatedAt.After(latest.CreatedAt)) {
			latest = job
			found = true
		}
	}
	if !found {
		return store.SyncJob{}, mongo.ErrNoDocuments
	}
	return latest, nil
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

func TestManagerSyncsSeedWithoutCache(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	source := &fakeSource{latest: 100}
	repository := newMemoryRepository()
	manager := New(source, repository, Config{
		DisableCache: true, Confirmations: 12, QueueSize: 10, Clock: func() time.Time { return now },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	request := Request{Chain: "ethereum", Address: "0x0000000000000000000000000000000000000001"}
	for range 2 {
		job, err := manager.Enqueue(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		job = waitForJob(t, manager, job.ID.Hex())
		if job.Status != "succeeded" || job.CachedAddresses != 0 {
			t.Fatalf("unexpected job: %+v", job)
		}
	}
	if source.latestCalls != 2 {
		t.Fatalf("latest calls = %d, want 2", source.latestCalls)
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

func TestManagerStopClearsRunningAddressState(t *testing.T) {
	seen, proceed := make(chan struct{}), make(chan struct{})
	source := &fakeSource{latest: 100, progressSeen: seen, progressContinue: proceed}
	repository := newMemoryRepository()
	manager := New(source, repository, Config{QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	address := "0x0000000000000000000000000000000000000001"
	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: address})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("sync did not start")
	}
	if err := manager.Stop(context.Background(), job.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	metadata, _, err := repository.FindAddress(context.Background(), "ethereum", address)
	if err != nil || metadata.SyncStatus != "failed" || metadata.SyncError != "stopped by user" {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	close(proceed)
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

func TestManagerUsesChainDefaultStartBlock(t *testing.T) {
	repository := newMemoryRepository()
	manager := NewMulti(map[string]Source{
		"ethereum": &fakeSource{},
		"base":     &fakeSource{},
	}, repository, Config{StartBlocks: map[string]int64{"ethereum": 21525891, "base": 24450127}})

	tests := []struct {
		chain      string
		address    string
		startBlock int64
		want       int64
	}{
		{chain: "ethereum", address: "0x0000000000000000000000000000000000000001", want: 21525891},
		{chain: "base", address: "0x0000000000000000000000000000000000000001", want: 24450127},
		{chain: "ethereum", address: "0x0000000000000000000000000000000000000002", startBlock: 22000000, want: 22000000},
	}
	for _, test := range tests {
		job, err := manager.Enqueue(context.Background(), Request{Chain: test.chain, Address: test.address, StartBlock: test.startBlock})
		if err != nil {
			t.Fatal(err)
		}
		if job.StartBlock != test.want {
			t.Fatalf("chain %s start block = %d, want %d", test.chain, job.StartBlock, test.want)
		}
	}
}

func TestManagerRespectsExplicitEndBlock(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	source := &recordingSource{fakeSource: fakeSource{latest: 1_000}}
	repository := newMemoryRepository()
	manager := New(source, repository, Config{QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 100, EndBlock: 200})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" || job.EndBlock != 200 {
		t.Fatalf("job=%+v", job)
	}
	want := [][2]int64{{100, 200}}
	for _, action := range []string{"txlist", "txlistinternal", "tokentx"} {
		if got := source.actionRanges[action]; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s ranges=%v, want %v", action, got, want)
		}
	}
}

func TestManagerBackfillsInternalHistoryFromConfiguredStart(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	source := &recordingSource{fakeSource: fakeSource{latest: 1_000_000}}
	repository := newMemoryRepository()
	repository.addresses[seed] = store.Address{
		Chain: "ethereum", Address: seed, SyncStatus: "synced",
		NormalSyncedFrom: 1, NormalSyncedTo: 1_000_000,
		InternalSyncedFrom: 900_001, InternalSyncedTo: 1_000_000,
		TokenSyncedFrom: 1, TokenSyncedTo: 1_000_000,
		LastSyncedAt: time.Now(),
	}
	manager := New(source, repository, Config{QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" {
		t.Fatalf("job=%+v", job)
	}
	if got := source.actionRanges["txlistinternal"]; fmt.Sprint(got) != fmt.Sprint([][2]int64{{1, 900_000}}) {
		t.Fatalf("txlistinternal ranges=%v", got)
	}
	if len(source.actionRanges["txlist"]) != 0 || len(source.actionRanges["tokentx"]) != 0 {
		t.Fatalf("full-range actions should remain cached: %v", source.actionRanges)
	}
}

func TestManagerBackfillsEachActionFromItsOwnCoverage(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	source := &recordingSource{fakeSource: fakeSource{latest: 1_000_000}}
	repository := newMemoryRepository()
	repository.addresses[seed] = store.Address{
		Chain: "ethereum", Address: seed, SyncStatus: "synced",
		NormalSyncedFrom: 100_001, NormalSyncedTo: 1_000_000,
		InternalSyncedFrom: 200_001, InternalSyncedTo: 1_000_000,
		TokenSyncedFrom: 300_001, TokenSyncedTo: 1_000_000,
		LastSyncedAt: time.Now(),
	}
	manager := New(source, repository, Config{QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" {
		t.Fatalf("job=%+v", job)
	}
	want := map[string][][2]int64{
		"txlist":         {{1, 100_000}},
		"txlistinternal": {{1, 200_000}},
		"tokentx":        {{1, 300_000}},
	}
	for action, ranges := range want {
		if got := source.actionRanges[action]; fmt.Sprint(got) != fmt.Sprint(ranges) {
			t.Fatalf("%s ranges=%v, want %v", action, got, ranges)
		}
	}
}

func TestCoverageDoesNotMergeAcrossUnknownBlocks(t *testing.T) {
	from, to := mergeCoverage(200, 300, 1, 100)
	if from != 200 || to != 300 {
		t.Fatalf("coverage=%d-%d, want 200-300", from, to)
	}
}

func TestCoverageAcceptsRangeStartingAtGenesis(t *testing.T) {
	if intervals := coverageIntervals(0, 100, 0, 50); fmt.Sprint(intervals) != fmt.Sprint([][2]int64{{51, 100}}) {
		t.Fatalf("intervals=%v, want [[51 100]]", intervals)
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

func TestManagerResumesFailedActionFromPersistedCheckpoint(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	source := &recordingSource{fakeSource: fakeSource{latest: 20}}
	repository := newMemoryRepository()
	repository.jobs[primitive.NewObjectID()] = store.SyncJob{Chain: "ethereum", Address: seed, StartBlock: 1, CoverageVersion: store.SyncCoverageVersion, Status: "failed", CreatedAt: time.Now(), Progress: store.SyncProgress{ActionCheckpoints: map[string]int64{"txlist": 10}}}
	manager := New(source, repository, Config{QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()
	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" || len(source.ranges) == 0 || source.ranges[0] != [2]int64{11, 20} {
		t.Fatalf("job=%+v ranges=%v", job, source.ranges)
	}
}

func TestManagerDiscardsCheckpointFromLegacyCoveragePolicy(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	source := &recordingSource{fakeSource: fakeSource{latest: 20}}
	repository := newMemoryRepository()
	repository.jobs[primitive.NewObjectID()] = store.SyncJob{
		Chain: "ethereum", Address: seed, StartBlock: 1, Status: "failed", CreatedAt: time.Now(),
		Progress: store.SyncProgress{ActionCheckpoints: map[string]int64{"txlistinternal": 10}},
	}
	manager := New(source, repository, Config{QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" || len(source.actionRanges["txlistinternal"]) == 0 || source.actionRanges["txlistinternal"][0] != [2]int64{1, 20} {
		t.Fatalf("job=%+v internal ranges=%v", job, source.actionRanges["txlistinternal"])
	}
}

func TestManagerResumesStoppedActionFromPersistedCheckpoint(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	source := &recordingSource{fakeSource: fakeSource{latest: 20}}
	repository := newMemoryRepository()
	repository.jobs[primitive.NewObjectID()] = store.SyncJob{Chain: "ethereum", Address: seed, StartBlock: 1, CoverageVersion: store.SyncCoverageVersion, Status: "stopped", CreatedAt: time.Now(), Progress: store.SyncProgress{ActionCheckpoints: map[string]int64{"txlist": 10}}}
	manager := New(source, repository, Config{QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()
	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" || len(source.ranges) == 0 || source.ranges[0] != [2]int64{11, 20} {
		t.Fatalf("job=%+v ranges=%v", job, source.ranges)
	}
}

func TestManagerLimitsEachActionToRecentRecordsAndMarksPartial(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	repository := newMemoryRepository()
	profileCalls := 0
	manager := New(&recentSource{}, repository, Config{
		QueueSize: 10, MaxRecordsPerAction: 2,
		AfterAddressSynced: func(context.Context, string, string) error {
			profileCalls++
			return errors.New("partial address must not be profiled")
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()
	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	metadata := repository.addresses[seed]
	if job.Status != "partial" || job.ErrorCode != "high_frequency" || job.MaxRecordsPerAction != 2 {
		t.Fatalf("job=%+v", job)
	}
	if fmt.Sprint(job.TruncatedActions) != fmt.Sprint([]string{"txlist", "high_frequency"}) {
		t.Fatalf("truncated actions=%v", job.TruncatedActions)
	}
	if len(repository.transfers) != 2 {
		t.Fatalf("transfers=%d, want 2", len(repository.transfers))
	}
	for index, transfer := range repository.transfers {
		if transfer.BlockNumber != 20-int64(index) {
			t.Fatalf("transfer[%d]=%+v, want newest records", index, transfer)
		}
	}
	if metadata.ActionRecordCounts["txlist"] != 2 || metadata.ActionRecordCounts["txlistinternal"] != 0 || metadata.ActionRecordCounts["tokentx"] != 0 {
		t.Fatalf("action record counts=%v", metadata.ActionRecordCounts)
	}
	if metadata.SyncStatus != "partial" || metadata.SyncError != "high_frequency" || metadata.SyncMaxRecordsPerAction != 2 {
		t.Fatalf("metadata=%+v", metadata)
	}
	if profileCalls != 0 {
		t.Fatalf("profile calls=%d, want 0", profileCalls)
	}

	cached, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	cached = waitForJob(t, manager, cached.ID.Hex())
	if cached.Status != "partial" || cached.ErrorCode != "high_frequency" || cached.CachedAddresses != 1 {
		t.Fatalf("cached job=%+v", cached)
	}
	if profileCalls != 0 {
		t.Fatalf("profile calls after cache=%d, want 0", profileCalls)
	}
}

func TestManagerClassifiesPersistedActionCountWithoutFetching(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	source := &fakeSource{latest: 100}
	repository := newMemoryRepository()
	repository.addresses[seed] = store.Address{Chain: "ethereum", Address: seed, SyncStatus: "synced"}
	repository.transfers = []store.Transfer{
		{Chain: "ethereum", TxHash: "0x1", From: seed, To: "0x0000000000000000000000000000000000000002", Source: "txlist"},
		{Chain: "ethereum", TxHash: "0x2", From: seed, To: "0x0000000000000000000000000000000000000003", Source: "txlist"},
	}
	manager := New(source, repository, Config{QueueSize: 10, MaxRecordsPerAction: 2})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	metadata := repository.addresses[seed]
	if job.Status != "partial" || job.ErrorCode != "high_frequency" || source.latestCalls != 0 || source.actionCalls != 0 {
		t.Fatalf("job=%+v latestCalls=%d actionCalls=%d", job, source.latestCalls, source.actionCalls)
	}
	if metadata.SyncStatus != "partial" || metadata.SyncError != "high_frequency" || metadata.ActionRecordCounts["txlist"] != 2 {
		t.Fatalf("metadata=%+v", metadata)
	}
}

func TestManagerWithActionLimitFetchesOnlyCoverageGap(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	now := time.Unix(1_700_000_000, 0).UTC()
	source := &recordingSource{fakeSource: fakeSource{latest: 110}}
	repository := newMemoryRepository()
	repository.addresses[seed] = store.Address{
		Chain: "ethereum", Address: seed, SyncStatus: "synced", LastSyncedAt: now.Add(-time.Hour),
		NormalSyncedFrom: 1, NormalSyncedTo: 100, InternalSyncedFrom: 1, InternalSyncedTo: 100, TokenSyncedFrom: 1, TokenSyncedTo: 100,
		ActionRecordCounts: map[string]int64{"txlist": 1, "txlistinternal": 1, "tokentx": 1},
	}
	manager := New(source, repository, Config{QueueSize: 10, MaxRecordsPerAction: 50, Clock: func() time.Time { return now }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()

	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "succeeded" {
		t.Fatalf("job=%+v", job)
	}
	for _, action := range []string{"txlist", "txlistinternal", "tokentx"} {
		if fmt.Sprint(source.actionRanges[action]) != fmt.Sprint([][2]int64{{101, 110}}) {
			t.Fatalf("%s ranges=%v", action, source.actionRanges[action])
		}
	}
}

func TestManagerContinuesRecentLimitAcrossPageBoundaries(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	source := &recentBoundarySource{}
	repository := newMemoryRepository()
	manager := New(source, repository, Config{QueueSize: 10, MaxRecordsPerAction: 4})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = manager.Run(ctx) }()
	job, err := manager.Enqueue(context.Background(), Request{Chain: "ethereum", Address: seed, StartBlock: 1})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForJob(t, manager, job.ID.Hex())
	if job.Status != "partial" || len(repository.transfers) != 4 || len(source.ranges) != 1 {
		t.Fatalf("job=%+v transfers=%d", job, len(repository.transfers))
	}
	for action, ranges := range source.ranges {
		if fmt.Sprint(ranges) != fmt.Sprint([][2]int64{{1, 20}, {1, 18}}) {
			t.Fatalf("%s ranges=%v", action, ranges)
		}
	}
	for index, transfer := range repository.transfers {
		if transfer.BlockNumber != 20-int64(index%4) {
			t.Fatalf("transfer[%d]=%+v", index, transfer)
		}
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
		NormalSyncedFrom: 100, NormalSyncedTo: 150, InternalSyncedFrom: 100, InternalSyncedTo: 150, TokenSyncedFrom: 100, TokenSyncedTo: 150,
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
	from, to, covered := address.CommonCoverage()
	if job.Status != "succeeded" || !covered || from != 50 || to != 188 || source.actionCalls != 6 {
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
