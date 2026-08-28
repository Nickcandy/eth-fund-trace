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
