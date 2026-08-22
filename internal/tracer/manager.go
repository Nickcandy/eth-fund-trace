package tracer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/syncer"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type JobRepository interface {
	CreateTraceJob(context.Context, *store.TraceJob) error
	GetTraceJob(context.Context, primitive.ObjectID) (store.TraceJob, error)
	SaveTraceJob(context.Context, store.TraceJob) error
	FailInterruptedTraceJobs(context.Context, time.Time) error
}
type SyncJobs interface {
	Enqueue(context.Context, syncer.Request) (store.SyncJob, error)
	Job(context.Context, string) (store.SyncJob, error)
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
}

var ErrQueueFull = errors.New("trace queue is full")

func NewManager(graph *Graph, jobs JobRepository, syncJobs SyncJobs) *Manager {
	return &Manager{graph: graph, jobs: jobs, syncJobs: syncJobs, queue: make(chan primitive.ObjectID, 100), active: make(map[string]primitive.ObjectID), clock: time.Now}
}
func (m *Manager) Enqueue(ctx context.Context, request Request) (store.TraceJob, error) {
	key := strings.ToLower(request.Query.Chain + ":" + request.Query.Address + ":" + request.Query.Direction + ":" + request.Query.Asset)
	m.mu.Lock()
	if id, ok := m.active[key]; ok {
		m.mu.Unlock()
		return m.jobs.GetTraceJob(ctx, id)
	}
	job := store.TraceJob{Chain: request.Query.Chain, SeedAddress: request.Query.Address, Direction: request.Query.Direction, Depth: request.Query.Depth, TopN: request.Query.TopN, Asset: request.Query.Asset, Status: "queued", CreatedAt: m.clock().UTC(), RuleVersion: "trace-v1"}
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
func (m *Manager) Run(ctx context.Context) error {
	if err := m.jobs.FailInterruptedTraceJobs(ctx, m.clock().UTC()); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case id := <-m.queue:
			go m.process(ctx, id)
		}
	}
}
func (m *Manager) process(ctx context.Context, id primitive.ObjectID) {
	job, err := m.jobs.GetTraceJob(ctx, id)
	if err != nil {
		return
	}
	job.Status = "waiting_sync"
	job.StartedAt = m.clock().UTC()
	_ = m.jobs.SaveTraceJob(ctx, job)
	request := Query{Chain: job.Chain, Address: job.SeedAddress, Direction: job.Direction, Depth: job.Depth, TopN: job.TopN, Asset: job.Asset}
	if m.syncJobs != nil {
		syncJob, syncErr := m.syncJobs.Enqueue(ctx, syncer.Request{Chain: job.Chain, Address: job.SeedAddress})
		if syncErr != nil {
			m.fail(ctx, &job, syncErr)
			return
		}
		job.SyncJobIDs = []string{syncJob.ID.Hex()}
		_ = m.jobs.SaveTraceJob(ctx, job)
		for {
			current, syncErr := m.syncJobs.Job(ctx, syncJob.ID.Hex())
			if syncErr != nil {
				m.fail(ctx, &job, syncErr)
				return
			}
			if current.Status == "succeeded" || current.Status == "partial" {
				break
			}
			if current.Status == "failed" {
				m.fail(ctx, &job, errors.New(current.Error))
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	job.Status = "running"
	_ = m.jobs.SaveTraceJob(ctx, job)
	result, traceErr := m.graph.Trace(ctx, request)
	if traceErr != nil {
		var unsynced AddressNotSyncedError
		if errors.As(traceErr, &unsynced) && m.syncJobs != nil && unsynced.Address != "" && !strings.EqualFold(unsynced.Address, request.Address) {
			job.Status = "waiting_sync"
			_ = m.jobs.SaveTraceJob(ctx, job)
			syncJob, syncErr := m.syncJobs.Enqueue(ctx, syncer.Request{Chain: job.Chain, Address: unsynced.Address})
			if syncErr == nil {
				job.SyncJobIDs = append(job.SyncJobIDs, syncJob.ID.Hex())
				_ = m.jobs.SaveTraceJob(ctx, job)
				for {
					current, pollErr := m.syncJobs.Job(ctx, syncJob.ID.Hex())
					if pollErr != nil {
						traceErr = pollErr
						break
					}
					if current.Status == "succeeded" || current.Status == "partial" {
						traceErr = nil
						break
					}
					if current.Status == "failed" {
						traceErr = errors.New(current.Error)
						break
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(100 * time.Millisecond):
					}
				}
				if traceErr == nil {
					result, traceErr = m.graph.Trace(ctx, request)
				}
			} else {
				traceErr = syncErr
			}
		}
	}
	if traceErr != nil {
		m.fail(ctx, &job, traceErr)
		return
	}
	job.Status = "succeeded"
	job.Result = result
	job.CurrentDepth = job.Depth
	job.VisitedNodes = len(result.Nodes)
	job.EdgeCount = len(result.Edges)
	job.DataThroughBlock = result.DataThroughBlock
	job.FinishedAt = m.clock().UTC()
	_ = m.jobs.SaveTraceJob(ctx, job)
	m.release(strings.ToLower(job.Chain + ":" + job.SeedAddress + ":" + job.Direction + ":" + job.Asset))
}
func (m *Manager) fail(ctx context.Context, job *store.TraceJob, err error) {
	job.Status = "failed"
	job.Error = err.Error()
	job.ErrorCode = "trace_failed"
	job.Retryable = true
	job.FinishedAt = m.clock().UTC()
	_ = m.jobs.SaveTraceJob(ctx, *job)
	m.release(strings.ToLower(job.Chain + ":" + job.SeedAddress + ":" + job.Direction + ":" + job.Asset))
}
func (m *Manager) release(key string) { m.mu.Lock(); delete(m.active, key); m.mu.Unlock() }
