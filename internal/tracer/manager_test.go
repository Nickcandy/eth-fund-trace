package tracer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/syncer"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type traceJobRepositoryStub struct {
	job store.TraceJob
}

type extensionJobRepository struct {
	jobs map[primitive.ObjectID]store.TraceJob
}

func (r *extensionJobRepository) CreateTraceJob(_ context.Context, job *store.TraceJob) error {
	if job.ID.IsZero() {
		job.ID = primitive.NewObjectID()
	}
	r.jobs[job.ID] = *job
	return nil
}
func (r *extensionJobRepository) GetTraceJob(_ context.Context, id primitive.ObjectID) (store.TraceJob, error) {
	job, ok := r.jobs[id]
	if !ok {
		return store.TraceJob{}, errors.New("job not found")
	}
	return job, nil
}
func (r *extensionJobRepository) FindLatestTraceJob(context.Context, string, string, string, int, string, string) (store.TraceJob, error) {
	return store.TraceJob{}, errors.New("not implemented")
}
func (r *extensionJobRepository) FindLatestTraceExtension(_ context.Context, rootID primitive.ObjectID) (store.TraceJob, error) {
	var latest store.TraceJob
	for _, job := range r.jobs {
		if job.RootTraceJobID == rootID && job.CreatedAt.After(latest.CreatedAt) {
			latest = job
		}
	}
	if latest.ID.IsZero() {
		return store.TraceJob{}, errors.New("job not found")
	}
	return latest, nil
}
func (r *extensionJobRepository) SaveTraceJob(_ context.Context, job store.TraceJob) error {
	r.jobs[job.ID] = job
	return nil
}
func (*extensionJobRepository) FailInterruptedTraceJobs(context.Context, time.Time) error { return nil }

func (r *traceJobRepositoryStub) CreateTraceJob(_ context.Context, job *store.TraceJob) error {
	if job.ID.IsZero() {
		job.ID = primitive.NewObjectID()
	}
	r.job = *job
	return nil
}

func (r *traceJobRepositoryStub) GetTraceJob(context.Context, primitive.ObjectID) (store.TraceJob, error) {
	return r.job, nil
}

func (r *traceJobRepositoryStub) FindLatestTraceJob(context.Context, string, string, string, int, string, string) (store.TraceJob, error) {
	return r.job, nil
}

func (r *traceJobRepositoryStub) FindLatestTraceExtension(context.Context, primitive.ObjectID) (store.TraceJob, error) {
	return r.job, nil
}

func (r *traceJobRepositoryStub) SaveTraceJob(_ context.Context, job store.TraceJob) error {
	r.job = job
	return nil
}

func (*traceJobRepositoryStub) FailInterruptedTraceJobs(context.Context, time.Time) error { return nil }

type rejectingSyncJobs struct {
	enqueueCalls int
}

func (s *rejectingSyncJobs) Enqueue(context.Context, syncer.Request) (store.SyncJob, error) {
	s.enqueueCalls++
	return store.SyncJob{}, errors.New("high-frequency seed must not be synced again")
}

func (*rejectingSyncJobs) Job(context.Context, string) (store.SyncJob, error) {
	return store.SyncJob{}, errors.New("unexpected sync job lookup")
}

func (*rejectingSyncJobs) Stop(context.Context, string) error { return nil }

type recordingSyncJobs struct {
	stopped []string
}

type successfulZeroProgressSyncJobs struct {
	enqueueCalls []syncer.Request
}

func (s *successfulZeroProgressSyncJobs) Enqueue(_ context.Context, request syncer.Request) (store.SyncJob, error) {
	s.enqueueCalls = append(s.enqueueCalls, request)
	return store.SyncJob{ID: primitive.NewObjectID(), Status: "succeeded"}, nil
}

func (*successfulZeroProgressSyncJobs) Job(context.Context, string) (store.SyncJob, error) {
	return store.SyncJob{Status: "succeeded", Fetched: 0}, nil
}

func (*successfulZeroProgressSyncJobs) Stop(context.Context, string) error { return nil }

func (*recordingSyncJobs) Enqueue(context.Context, syncer.Request) (store.SyncJob, error) {
	return store.SyncJob{}, errors.New("unexpected enqueue")
}

func (*recordingSyncJobs) Job(context.Context, string) (store.SyncJob, error) {
	return store.SyncJob{}, errors.New("unexpected lookup")
}

func (s *recordingSyncJobs) Stop(_ context.Context, id string) error {
	s.stopped = append(s.stopped, id)
	return nil
}

func TestTraceManagerStopRootStopsActiveExtensionAndItsSyncJobs(t *testing.T) {
	rootID := primitive.NewObjectID()
	extensionID := primitive.NewObjectID()
	jobs := &extensionJobRepository{jobs: map[primitive.ObjectID]store.TraceJob{
		rootID: {ID: rootID, Status: "succeeded", CreatedAt: time.Unix(1, 0)},
		extensionID: {
			ID: extensionID, RootTraceJobID: rootID, Status: "waiting_sync", CreatedAt: time.Unix(2, 0),
			SyncJobIDs: []string{"sync-1", "sync-2"},
		},
	}}
	syncJobs := &recordingSyncJobs{}
	stopped, err := NewManager(New(&fakeRepository{}), jobs, syncJobs).Stop(context.Background(), rootID.Hex())
	if err != nil {
		t.Fatal(err)
	}
	if stopped.ID != extensionID || stopped.Status != "stopped" {
		t.Fatalf("stopped=%+v", stopped)
	}
	if got := jobs.jobs[extensionID].Status; got != "stopped" {
		t.Fatalf("extension status=%s", got)
	}
	if len(syncJobs.stopped) != 2 || syncJobs.stopped[0] != "sync-1" || syncJobs.stopped[1] != "sync-2" {
		t.Fatalf("stopped sync jobs=%v", syncJobs.stopped)
	}
}

func TestTraceManagerDoesNotResyncHighFrequencySeed(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	id := primitive.NewObjectID()
	jobs := &traceJobRepositoryStub{job: store.TraceJob{
		ID: id, Chain: "ethereum", SeedAddress: seed, Direction: "out", Depth: 1, Asset: "ETH", Status: "queued",
	}}
	repository := &fakeRepository{addresses: map[string]store.Address{
		seed: {SyncStatus: "partial", SyncError: "high_frequency", SyncMaxRecordsPerAction: 50_000},
	}, labels: map[string][]store.Label{}}
	syncJobs := &rejectingSyncJobs{}

	NewManager(New(repository), jobs, syncJobs).process(context.Background(), id)

	if syncJobs.enqueueCalls != 0 {
		t.Fatalf("sync enqueue calls=%d, want 0", syncJobs.enqueueCalls)
	}
	if jobs.job.Status != "partial" || jobs.job.ErrorCode != "high_frequency" {
		t.Fatalf("job=%+v", jobs.job)
	}
	result, ok := jobs.job.Result.(Result)
	if !ok || len(result.Nodes) != 1 || !result.Nodes[0].Terminal || result.Nodes[0].StopReason != "high_frequency" {
		t.Fatalf("result=%+v", jobs.job.Result)
	}
}

func TestTraceManagerStopsRetryingDependencyWhenSyncMakesNoProgress(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	dependency := "0x0000000000000000000000000000000000000002"
	id := primitive.NewObjectID()
	jobs := &traceJobRepositoryStub{job: store.TraceJob{
		ID: id, Chain: "ethereum", SeedAddress: seed, Direction: "out", Depth: 2, Asset: "all", Status: "queued",
	}}
	repository := &fakeRepository{
		addresses: map[string]store.Address{
			seed:       completeAddress(0, 100),
			dependency: completeAddress(10, 99),
		},
		labels: map[string][]store.Label{},
		transfers: []store.Transfer{{
			Chain: "ethereum", TxHash: "0xdependency", BlockNumber: 10,
			From: seed, To: dependency, AssetType: "eth", Asset: "ETH", Amount: "100000000000000000",
		}},
	}
	syncJobs := &successfulZeroProgressSyncJobs{}

	NewManager(New(repository), jobs, syncJobs).process(context.Background(), id)

	if len(syncJobs.enqueueCalls) != 2 {
		t.Fatalf("sync enqueue calls=%d, want seed plus one dependency sync", len(syncJobs.enqueueCalls))
	}
	if jobs.job.Status != "partial" {
		t.Fatalf("job status=%s, want partial; error=%s", jobs.job.Status, jobs.job.Error)
	}
	result, ok := jobs.job.Result.(Result)
	if !ok || len(result.Nodes) != 2 || !result.Nodes[1].Terminal || result.Nodes[1].StopReason != "missing_data" {
		t.Fatalf("result=%+v", jobs.job.Result)
	}
}

func TestTraceManagerFailPreservesStoppedStatus(t *testing.T) {
	id := primitive.NewObjectID()
	jobs := &traceJobRepositoryStub{job: store.TraceJob{
		ID: id, Status: "stopped", ErrorCode: "stopped_by_user", Error: "stopped by user",
	}}
	jobFromWorker := jobs.job
	jobFromWorker.Status = "running"

	NewManager(New(&fakeRepository{}), jobs, nil).fail(context.Background(), &jobFromWorker, context.Canceled)

	if jobs.job.Status != "stopped" || jobs.job.ErrorCode != "stopped_by_user" {
		t.Fatalf("job=%+v", jobs.job)
	}
}

func TestTraceManagerExtensionMergesIntoRootWithoutReplacingIt(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	anchor := "0x0000000000000000000000000000000000000002"
	next := "0x0000000000000000000000000000000000000003"
	rootID := primitive.NewObjectID()
	rootResult := Result{Nodes: []Node{{Chain: "ethereum", Address: seed}, {Chain: "ethereum", Address: anchor, Depth: 1}}, Edges: []Edge{{Chain: "ethereum", From: seed, To: anchor, AssetType: "eth", Asset: "ETH", TotalAmount: "100000000000000000", FirstBlock: 10, LatestBlock: 10, Path: []string{seed, anchor}}}, DataThroughBlock: 100, DataStatus: "synced", RuleVersion: traceRuleVersion}
	jobs := &extensionJobRepository{jobs: map[primitive.ObjectID]store.TraceJob{rootID: {ID: rootID, Chain: "ethereum", SeedAddress: seed, Direction: "both", Depth: 3, Asset: "all", Status: "succeeded", Result: rootResult, RuleVersion: traceRuleVersion}}}
	repository := &fakeRepository{addresses: map[string]store.Address{anchor: completeAddress(0, 100), next: completeAddress(0, 100)}, labels: map[string][]store.Label{}, transfers: []store.Transfer{{Chain: "ethereum", TxHash: "0xnext", BlockNumber: 11, From: anchor, To: next, AssetType: "eth", Asset: "ETH", Amount: "50000000000000000"}}}
	manager := NewManager(New(repository), jobs, nil)

	extension, err := manager.EnqueueExtension(context.Background(), rootID.Hex(), ExtensionRequest{Address: anchor, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	manager.process(context.Background(), extension.ID)

	root := jobs.jobs[rootID]
	merged, err := decodeResult(root.Result)
	if err != nil {
		t.Fatal(err)
	}
	if root.Status != "succeeded" || len(merged.Nodes) != 3 || len(merged.Edges) != 2 {
		t.Fatalf("root=%+v result=%+v", root, merged)
	}
	completed := jobs.jobs[extension.ID]
	if completed.Status != "succeeded" || completed.RootTraceJobID != rootID || completed.ExtensionAddress != anchor || completed.ExtensionDirection != "out" {
		t.Fatalf("extension=%+v", completed)
	}
}

func TestTraceManagerExtensionStopsRetryingDependencyWhenSyncMakesNoProgress(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	anchor := "0x0000000000000000000000000000000000000002"
	dependency := "0x0000000000000000000000000000000000000003"
	rootID := primitive.NewObjectID()
	rootResult := Result{
		Nodes:            []Node{{Chain: "ethereum", Address: seed}, {Chain: "ethereum", Address: anchor, Depth: 1}},
		Edges:            []Edge{{Chain: "ethereum", From: seed, To: anchor, AssetType: "eth", Asset: "ETH", TotalAmount: "100000000000000000", FirstBlock: 10, LatestBlock: 10, Path: []string{seed, anchor}}},
		DataThroughBlock: 100, DataStatus: "synced", RuleVersion: traceRuleVersion,
	}
	jobs := &extensionJobRepository{jobs: map[primitive.ObjectID]store.TraceJob{
		rootID: {ID: rootID, Chain: "ethereum", SeedAddress: seed, Direction: "both", Depth: 3, Asset: "all", Status: "succeeded", Result: rootResult, RuleVersion: traceRuleVersion},
	}}
	repository := &fakeRepository{
		addresses: map[string]store.Address{dependency: completeAddress(11, 99)},
		labels:    map[string][]store.Label{},
		transfers: []store.Transfer{{
			Chain: "ethereum", TxHash: "0xdependency", BlockNumber: 11,
			From: anchor, To: dependency, AssetType: "eth", Asset: "ETH", Amount: "50000000000000000",
		}},
	}
	syncJobs := &successfulZeroProgressSyncJobs{}
	manager := NewManager(New(repository), jobs, syncJobs)
	extension, err := manager.EnqueueExtension(context.Background(), rootID.Hex(), ExtensionRequest{Address: anchor, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}

	manager.process(context.Background(), extension.ID)

	if len(syncJobs.enqueueCalls) != 2 {
		t.Fatalf("sync enqueue calls=%d, want extension plus one dependency sync", len(syncJobs.enqueueCalls))
	}
	completed := jobs.jobs[extension.ID]
	if completed.Status != "partial" {
		t.Fatalf("extension status=%s, want partial; error=%s", completed.Status, completed.Error)
	}
	result, err := decodeResult(completed.Result)
	if err != nil || len(result.Nodes) != 2 || !result.Nodes[1].Terminal || result.Nodes[1].StopReason != "missing_data" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
