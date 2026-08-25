package tracer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/syncer"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type JobRepository interface {
	CreateTraceJob(context.Context, *store.TraceJob) error
	GetTraceJob(context.Context, primitive.ObjectID) (store.TraceJob, error)
	FindLatestTraceJob(context.Context, string, string, string, int, int, string, string) (store.TraceJob, error)
	SaveTraceJob(context.Context, store.TraceJob) error
	FailInterruptedTraceJobs(context.Context, time.Time) error
}
type SyncJobs interface {
	Enqueue(context.Context, syncer.Request) (store.SyncJob, error)
	Job(context.Context, string) (store.SyncJob, error)
	Stop(context.Context, string) error
}
type Request struct{ Query Query }
type Manager struct {
	graph    *Graph
	jobs     JobRepository
	syncJobs SyncJobs
	queue    chan primitive.ObjectID
	mu       sync.Mutex
	active   map[string]primitive.ObjectID
	clock    func() time.Time
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

var ErrQueueFull = errors.New("trace queue is full")

func NewManager(graph *Graph, jobs JobRepository, syncJobs SyncJobs) *Manager {
	return &Manager{graph: graph, jobs: jobs, syncJobs: syncJobs, queue: make(chan primitive.ObjectID, 100), active: make(map[string]primitive.ObjectID), clock: time.Now, cancels: make(map[string]context.CancelFunc)}
}
func (m *Manager) Enqueue(ctx context.Context, request Request) (store.TraceJob, error) {
	normalized, err := normalize(request.Query)
	if err != nil {
		return store.TraceJob{}, err
	}
	request.Query = normalized
	address, addressErr := ethaddr.Normalize(request.Query.Address)
	if addressErr != nil {
		return store.TraceJob{}, ErrInvalidQuery
	}
	request.Query.Address = address
	normalized.Address = address
	key := queryKey(normalized)
	m.mu.Lock()
	if id, ok := m.active[key]; ok {
		m.mu.Unlock()
		return m.jobs.GetTraceJob(ctx, id)
	}
	job := store.TraceJob{Chain: request.Query.Chain, SeedAddress: request.Query.Address, Direction: request.Query.Direction, Depth: request.Query.Depth, TopN: request.Query.TopN, Asset: request.Query.Asset, Status: "queued", CreatedAt: m.clock().UTC(), RuleVersion: traceRuleVersion}
	if err := m.jobs.CreateTraceJob(ctx, &job); err != nil {
		m.mu.Unlock()
		return store.TraceJob{}, err
	}
	m.active[key] = job.ID
	m.mu.Unlock()
	select {
	case m.queue <- job.ID:
		return job, nil
	default:
		m.release(key)
		return store.TraceJob{}, ErrQueueFull
	}
}
func (m *Manager) Job(ctx context.Context, id string) (store.TraceJob, error) {
	parsed, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return store.TraceJob{}, err
	}
	return m.jobs.GetTraceJob(ctx, parsed)
}
func (m *Manager) Stop(ctx context.Context, id string) (store.TraceJob, error) {
	parsed, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return store.TraceJob{}, err
	}
	job, err := m.jobs.GetTraceJob(ctx, parsed)
	if err != nil {
		return store.TraceJob{}, err
	}
	if job.Status == "queued" || job.Status == "waiting_sync" || job.Status == "running" {
		job.Status, job.ErrorCode, job.Error, job.Retryable = "stopped", "stopped_by_user", "stopped by user", false
		job.FinishedAt = m.clock().UTC()
		if err := m.jobs.SaveTraceJob(ctx, job); err != nil {
			return store.TraceJob{}, err
		}
		m.cancelMu.Lock()
		if cancel := m.cancels[id]; cancel != nil {
			cancel()
		}
		m.cancelMu.Unlock()
		for _, syncID := range job.SyncJobIDs {
			_ = m.syncJobs.Stop(ctx, syncID)
		}
	}
	return job, nil
}
func (m *Manager) LatestJob(ctx context.Context, query Query) (store.TraceJob, error) {
	normalized, err := normalize(query)
	if err != nil {
		return store.TraceJob{}, err
	}
	address, err := ethaddr.Normalize(normalized.Address)
	if err != nil {
		return store.TraceJob{}, ErrInvalidQuery
	}
	return m.jobs.FindLatestTraceJob(ctx, normalized.Chain, address, normalized.Direction, normalized.Depth, normalized.TopN, normalized.Asset, traceRuleVersion)
}
func (m *Manager) Run(ctx context.Context) error {
	if err := m.jobs.FailInterruptedTraceJobs(ctx, m.clock().UTC()); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case id := <-m.queue:
			m.process(ctx, id)
		}
	}
}
func (m *Manager) process(ctx context.Context, id primitive.ObjectID) {
	jobCtx, cancel := context.WithCancel(ctx)
	m.cancelMu.Lock()
	m.cancels[id.Hex()] = cancel
	m.cancelMu.Unlock()
	defer func() { cancel(); m.cancelMu.Lock(); delete(m.cancels, id.Hex()); m.cancelMu.Unlock() }()
	job, err := m.jobs.GetTraceJob(ctx, id)
	if err != nil {
		m.releaseID(id)
		return
	}
	if job.Status == "stopped" {
		return
	}
	key := queryKey(Query{Chain: job.Chain, Address: job.SeedAddress, Direction: job.Direction, Depth: job.Depth, TopN: job.TopN, Asset: job.Asset})
	defer m.release(key)
	job.Status = "waiting_sync"
	job.StartedAt = m.clock().UTC()
	if !m.save(ctx, &job) {
		m.fail(ctx, &job, errors.New("failed to persist trace job"))
		return
	}
	request := Query{Chain: job.Chain, Address: job.SeedAddress, Direction: job.Direction, Depth: job.Depth, TopN: job.TopN, Asset: job.Asset}
	partialSync := false
	if m.syncJobs != nil {
		syncJob, syncErr := m.syncJobs.Enqueue(ctx, syncer.Request{Chain: job.Chain, Address: job.SeedAddress, NeighborLimit: 0})
		if syncErr != nil {
			m.fail(ctx, &job, syncErr)
			return
		}
		job.SyncJobIDs = []string{syncJob.ID.Hex()}
		if !m.save(ctx, &job) {
			m.fail(ctx, &job, errors.New("failed to persist trace job"))
			return
		}
		current, pollErr := m.waitSync(jobCtx, syncJob.ID.Hex())
		if pollErr != nil {
			if errors.Is(pollErr, context.Canceled) {
				return
			}
			m.fail(ctx, &job, pollErr)
			return
		}
		partialSync = current.Status == "partial"
	}
	job.Status = "running"
	if !m.save(ctx, &job) {
		m.fail(ctx, &job, errors.New("failed to persist trace job"))
		return
	}
	result, traceErr := m.graph.Trace(jobCtx, request)
	for traceErr != nil {
		var unsynced AddressNotSyncedError
		if errors.As(traceErr, &unsynced) && m.syncJobs != nil && unsynced.Address != "" {
			job.Status = "waiting_sync"
			if !m.save(ctx, &job) {
				m.fail(ctx, &job, errors.New("failed to persist trace job"))
				return
			}
			dependencyChain := unsynced.Chain
			if dependencyChain == "" {
				dependencyChain = job.Chain
			}
			syncJob, syncErr := m.syncJobs.Enqueue(ctx, syncer.Request{Chain: dependencyChain, Address: unsynced.Address, NeighborLimit: 0})
			if syncErr != nil {
				traceErr = syncErr
				break
			}
			job.SyncJobIDs = append(job.SyncJobIDs, syncJob.ID.Hex())
			if !m.save(ctx, &job) {
				m.fail(ctx, &job, errors.New("failed to persist trace job"))
				return
			}
			current, pollErr := m.waitSync(jobCtx, syncJob.ID.Hex())
			if pollErr != nil {
				if errors.Is(pollErr, context.Canceled) {
					return
				}
				traceErr = pollErr
				break
			}
			partialSync = partialSync || current.Status == "partial"
			result, traceErr = m.graph.Trace(jobCtx, request)
			continue
		}
		break
	}
	if traceErr != nil {
		m.fail(ctx, &job, traceErr)
		return
	}
	job.Status = "succeeded"
	if partialSync {
		job.Status = "partial"
	}
	job.Result = result
	job.CurrentDepth = job.Depth
	job.VisitedNodes = len(result.Nodes)
	job.EdgeCount = len(result.Edges)
	job.DataThroughBlock = result.DataThroughBlock
	job.FinishedAt = m.clock().UTC()
	if !m.save(ctx, &job) {
		m.fail(ctx, &job, errors.New("failed to persist trace job"))
		return
	}
}
func (m *Manager) fail(ctx context.Context, job *store.TraceJob, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		job.ErrorCode = "interrupted"
	} else {
		job.ErrorCode = "trace_failed"
	}
	job.Status = "failed"
	job.Error = err.Error()
	job.Retryable = true
	job.FinishedAt = m.clock().UTC()
	saveCtx := ctx
	if ctx.Err() != nil {
		saveCtx = context.Background()
	}
	_ = m.jobs.SaveTraceJob(saveCtx, *job)
}
func (m *Manager) release(key string) { m.mu.Lock(); delete(m.active, key); m.mu.Unlock() }

func (m *Manager) releaseID(id primitive.ObjectID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, activeID := range m.active {
		if activeID == id {
			delete(m.active, key)
		}
	}
}

func (m *Manager) save(ctx context.Context, job *store.TraceJob) bool {
	return m.jobs.SaveTraceJob(ctx, *job) == nil
}

func (m *Manager) waitSync(ctx context.Context, id string) (store.SyncJob, error) {
	for {
		current, err := m.syncJobs.Job(ctx, id)
		if err != nil {
			return store.SyncJob{}, err
		}
		switch current.Status {
		case "succeeded", "partial":
			return current, nil
		case "failed":
			return current, errors.New(current.Error)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return store.SyncJob{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func queryKey(q Query) string {
	return strings.ToLower(q.Chain + ":" + q.Address + ":" + q.Direction + ":" + q.Asset + ":" + fmt.Sprint(q.Depth) + ":" + fmt.Sprint(q.TopN))
}
