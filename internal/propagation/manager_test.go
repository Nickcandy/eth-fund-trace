package propagation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type managerRepository struct {
	*engineRepository
	mu           sync.Mutex
	jobs         map[primitive.ObjectID]store.PropagationJob
	keys         map[string]primitive.ObjectID
	associations []store.InferredRiskAssociation
}

func newManagerRepository(engine *engineRepository) *managerRepository {
	return &managerRepository{engineRepository: engine, jobs: make(map[primitive.ObjectID]store.PropagationJob), keys: make(map[string]primitive.ObjectID)}
}
func (r *managerRepository) CreatePropagationJob(_ context.Context, job *store.PropagationJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.keys[job.IdempotencyKey]; ok {
		return mongo.WriteException{WriteErrors: []mongo.WriteError{{Code: 11000}}}
	}
	job.ID = primitive.NewObjectID()
	r.jobs[job.ID], r.keys[job.IdempotencyKey] = *job, job.ID
	return nil
}
func (r *managerRepository) FindPropagationJobByKey(_ context.Context, key string) (store.PropagationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.keys[key]
	if !ok {
		return store.PropagationJob{}, mongo.ErrNoDocuments
	}
	return r.jobs[id], nil
}
func (r *managerRepository) GetPropagationJob(_ context.Context, id primitive.ObjectID) (store.PropagationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return store.PropagationJob{}, mongo.ErrNoDocuments
	}
	return job, nil
}
func (r *managerRepository) ClaimPropagationJob(_ context.Context, now, lease time.Time, maxRetries int) (store.PropagationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, job := range r.jobs {
		if job.Status == "queued" && job.RetryCount < maxRetries {
			job.Status, job.StartedAt, job.LeaseUntil, job.RetryCount = "running", now, lease, job.RetryCount+1
			r.jobs[id] = job
			return job, nil
		}
	}
	return store.PropagationJob{}, mongo.ErrNoDocuments
}
func (r *managerRepository) SupersedeLegacyPropagationJobs(context.Context, time.Time, string) error {
	return nil
}
func (r *managerRepository) ExtendPropagationLease(_ context.Context, id primitive.ObjectID, lease time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[id]
	job.LeaseUntil = lease
	r.jobs[id] = job
	return nil
}
func (r *managerRepository) UpdatePropagationProgress(_ context.Context, id primitive.ObjectID, hop, nodes, edges int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[id]
	job.CurrentHop, job.VisitedNodes, job.EdgeCount = hop, nodes, edges
	r.jobs[id] = job
	return nil
}
func (r *managerRepository) SavePropagationJob(_ context.Context, job store.PropagationJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.jobs[job.ID].Status == "stopped" {
		return context.Canceled
	}
	r.jobs[job.ID] = job
	return nil
}
func (r *managerRepository) StopPropagationJob(_ context.Context, id primitive.ObjectID, now time.Time) (store.PropagationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return store.PropagationJob{}, mongo.ErrNoDocuments
	}
	job.Status, job.FinishedAt = "stopped", now
	r.jobs[id] = job
	return job, nil
}
func (r *managerRepository) UpsertRiskAssociation(_ context.Context, item store.InferredRiskAssociation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.associations = append(r.associations, item)
	return nil
}
func (r *managerRepository) MarkRiskAssociationsStale(context.Context, string, string, string, int64) error {
	return nil
}

func TestManagerEnqueueIsIdempotentAndPersistsAssociations(t *testing.T) {
	source := "0x0000000000000000000000000000000000000001"
	target := "0x0000000000000000000000000000000000000002"
	label := store.Label{ID: primitive.NewObjectID(), Address: source, Type: "hacker", Source: "manual", RiskLevel: "high", Confidence: 1, ObservedAt: time.Now()}
	engineRepository := &engineRepository{
		addresses: map[string]store.Address{
			nodeKey("ethereum", source): {Chain: "ethereum", Address: source, SyncStatus: "synced", LatestSyncedBlock: 100},
			nodeKey("ethereum", target): {Chain: "ethereum", Address: target, SyncStatus: "synced", LatestSyncedBlock: 100},
		},
		labels:     map[string][]store.Label{nodeKey("ethereum", source): {label}},
		candidates: map[string]store.CandidateResult{"ethereum:" + source + ":out:ETH": {Items: []store.CounterpartySummary{summary(source, target, "0x1")}}},
		analyses:   map[string]store.TransactionAnalysis{}, bridges: map[string][]store.CrossChainLink{},
	}
	repository := newManagerRepository(engineRepository)
	manager := NewManager(NewEngine(repository), repository)
	request := Request{Chain: "ethereum", TargetAddress: source, Direction: "out", Asset: "ETH"}
	first, err := manager.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Enqueue(context.Background(), request)
	if err != nil || first.ID != second.ID {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	if err := manager.claimAndProcess(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := manager.Job(context.Background(), first.ID.Hex())
	if err != nil || completed.Status != "succeeded" || completed.VisitedNodes != 2 || len(repository.associations) != 1 {
		t.Fatalf("job=%+v associations=%+v err=%v", completed, repository.associations, err)
	}
	if repository.associations[0].TargetAddress != target || repository.associations[0].SourceLabelID != label.ID {
		t.Fatalf("association=%+v", repository.associations[0])
	}
}

func TestManagerAcceptsTargetWithoutDeterministicLabel(t *testing.T) {
	target := "0x0000000000000000000000000000000000000001"
	engineRepository := &engineRepository{addresses: map[string]store.Address{nodeKey("ethereum", target): {SyncStatus: "synced"}}, labels: map[string][]store.Label{nodeKey("ethereum", target): {{Type: "suspected_hot_wallet", Source: "profile", Confidence: 1}}}}
	repository := newManagerRepository(engineRepository)
	_, err := NewManager(NewEngine(repository), repository).Enqueue(context.Background(), Request{Chain: "ethereum", TargetAddress: target})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
}
