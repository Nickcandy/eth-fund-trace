package tracer

import (
	"context"
	"encoding/json"
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
	FindLatestTraceJob(context.Context, string, string, string, int, string, string) (store.TraceJob, error)
	FindLatestTraceExtension(context.Context, primitive.ObjectID) (store.TraceJob, error)
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
var ErrExtensionActive = errors.New("trace extension already running")

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
	job := store.TraceJob{Chain: request.Query.Chain, SeedAddress: request.Query.Address, Direction: request.Query.Direction, Depth: request.Query.Depth, Asset: request.Query.Asset, Status: "queued", CreatedAt: m.clock().UTC(), RuleVersion: traceRuleVersion}
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

func (m *Manager) EnqueueExtension(ctx context.Context, rootID string, request ExtensionRequest) (store.TraceJob, error) {
	parsed, err := primitive.ObjectIDFromHex(rootID)
	if err != nil {
		return store.TraceJob{}, ErrInvalidExtension
	}
	root, err := m.jobs.GetTraceJob(ctx, parsed)
	if err != nil {
		return store.TraceJob{}, err
	}
	if !root.RootTraceJobID.IsZero() || root.Status != "succeeded" && root.Status != "partial" {
		return store.TraceJob{}, ErrInvalidExtension
	}
	request.Chain = root.Chain
	if err := ValidateExtension(request); err != nil {
		return store.TraceJob{}, err
	}
	rootResult, err := decodeResult(root.Result)
	if err != nil || len(extensionAnchors(rootResult, request)) == 0 {
		return store.TraceJob{}, ErrInvalidExtension
	}
	key := "extension:" + rootID
	m.mu.Lock()
	if _, active := m.active[key]; active {
		m.mu.Unlock()
		return store.TraceJob{}, ErrExtensionActive
	}
	job := store.TraceJob{
		Chain: root.Chain, SeedAddress: root.SeedAddress, Direction: request.Direction, Depth: 1, Asset: root.Asset,
		Status: "queued", CreatedAt: m.clock().UTC(), RuleVersion: traceRuleVersion, RootTraceJobID: parsed,
		ExtensionAddress: strings.ToLower(request.Address), ExtensionDirection: request.Direction,
	}
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

func (m *Manager) LatestExtension(ctx context.Context, rootID string) (store.TraceJob, error) {
	parsed, err := primitive.ObjectIDFromHex(rootID)
	if err != nil {
		return store.TraceJob{}, err
	}
	return m.jobs.FindLatestTraceExtension(ctx, parsed)
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
	return m.jobs.FindLatestTraceJob(ctx, normalized.Chain, address, normalized.Direction, normalized.Depth, normalized.Asset, traceRuleVersion)
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
		if !job.RootTraceJobID.IsZero() {
			m.release("extension:" + job.RootTraceJobID.Hex())
		}
		return
	}
	if !job.RootTraceJobID.IsZero() {
		m.processExtension(jobCtx, &job)
		return
	}
	key := queryKey(Query{Chain: job.Chain, Address: job.SeedAddress, Direction: job.Direction, Depth: job.Depth, Asset: job.Asset})
	defer m.release(key)
	job.Status = "waiting_sync"
	job.StartedAt = m.clock().UTC()
	if !m.save(ctx, &job) {
		m.fail(ctx, &job, errors.New("failed to persist trace job"))
		return
	}
	request := Query{Chain: job.Chain, Address: job.SeedAddress, Direction: job.Direction, Depth: job.Depth, Asset: job.Asset}
	partialSync := false
	seedHighFrequency := false
	if m.syncJobs != nil {
		metadata, found, metadataErr := m.graph.repository.FindAddress(jobCtx, job.Chain, job.SeedAddress)
		if metadataErr != nil {
			m.fail(ctx, &job, metadataErr)
			return
		}
		seedHighFrequency = found && isHighFrequencyAddress(metadata)
		if seedHighFrequency {
			partialSync = true
			job.ErrorCode = "high_frequency"
			job.Error = "high_frequency"
		}
	}
	if m.syncJobs != nil && !seedHighFrequency {
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
		if current.ErrorCode == "high_frequency" {
			job.ErrorCode = current.ErrorCode
			job.Error = current.Error
		}
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
			syncJob, syncErr := m.syncJobs.Enqueue(ctx, syncer.Request{Chain: dependencyChain, Address: unsynced.Address, StartBlock: unsynced.StartBlock, EndBlock: unsynced.EndBlock, NeighborLimit: 0})
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
			if current.ErrorCode == "high_frequency" {
				job.ErrorCode = current.ErrorCode
				job.Error = current.Error
			}
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

func (m *Manager) processExtension(ctx context.Context, job *store.TraceJob) {
	key := "extension:" + job.RootTraceJobID.Hex()
	defer m.release(key)
	root, err := m.jobs.GetTraceJob(ctx, job.RootTraceJobID)
	if err != nil {
		m.fail(ctx, job, err)
		return
	}
	rootResult, err := decodeResult(root.Result)
	if err != nil {
		m.fail(ctx, job, err)
		return
	}
	request := ExtensionRequest{Chain: job.Chain, Address: job.ExtensionAddress, Direction: job.ExtensionDirection, Depth: 1}
	anchors := extensionAnchors(rootResult, request)
	job.Status, job.StartedAt = "waiting_sync", m.clock().UTC()
	if !m.save(ctx, job) {
		m.fail(ctx, job, errors.New("failed to persist trace extension"))
		return
	}
	if m.syncJobs != nil {
		startBlock, endBlock := extensionSyncBounds(anchors)
		syncJob, syncErr := m.syncJobs.Enqueue(ctx, syncer.Request{Chain: job.Chain, Address: job.ExtensionAddress, StartBlock: startBlock, EndBlock: endBlock, NeighborLimit: 0})
		if syncErr != nil {
			m.fail(ctx, job, syncErr)
			return
		}
		job.SyncJobIDs = append(job.SyncJobIDs, syncJob.ID.Hex())
		if !m.save(ctx, job) {
			m.fail(ctx, job, errors.New("failed to persist trace extension"))
			return
		}
		if _, syncErr = m.waitSync(ctx, syncJob.ID.Hex()); syncErr != nil {
			if !errors.Is(syncErr, context.Canceled) {
				m.fail(ctx, job, syncErr)
			}
			return
		}
	}
	job.Status = "running"
	if !m.save(ctx, job) {
		m.fail(ctx, job, errors.New("failed to persist trace extension"))
		return
	}
	extension, traceErr := m.graph.ExtendBranch(ctx, rootResult, request)
	for traceErr != nil {
		var unsynced AddressNotSyncedError
		if !errors.As(traceErr, &unsynced) || m.syncJobs == nil {
			break
		}
		job.Status = "waiting_sync"
		if !m.save(ctx, job) {
			m.fail(ctx, job, errors.New("failed to persist trace extension"))
			return
		}
		syncJob, syncErr := m.syncJobs.Enqueue(ctx, syncer.Request{Chain: unsynced.Chain, Address: unsynced.Address, StartBlock: unsynced.StartBlock, EndBlock: unsynced.EndBlock, NeighborLimit: 0})
		if syncErr != nil {
			traceErr = syncErr
			break
		}
		job.SyncJobIDs = append(job.SyncJobIDs, syncJob.ID.Hex())
		if !m.save(ctx, job) {
			m.fail(ctx, job, errors.New("failed to persist trace extension"))
			return
		}
		if _, syncErr = m.waitSync(ctx, syncJob.ID.Hex()); syncErr != nil {
			traceErr = syncErr
			break
		}
		job.Status = "running"
		extension, traceErr = m.graph.ExtendBranch(ctx, rootResult, request)
	}
	if traceErr != nil {
		m.fail(ctx, job, traceErr)
		return
	}
	root, err = m.jobs.GetTraceJob(ctx, job.RootTraceJobID)
	if err != nil {
		m.fail(ctx, job, err)
		return
	}
	rootResult, err = decodeResult(root.Result)
	if err != nil {
		m.fail(ctx, job, err)
		return
	}
	merged := MergeResults(rootResult, extension)
	root.Result, root.VisitedNodes, root.EdgeCount = merged, len(merged.Nodes), len(merged.Edges)
	if merged.DataStatus == "partial" {
		root.Status = "partial"
	}
	if err := m.jobs.SaveTraceJob(ctx, root); err != nil {
		m.fail(ctx, job, err)
		return
	}
	job.Status, job.Result, job.CurrentDepth = "succeeded", extension, 1
	if extension.DataStatus == "partial" {
		job.Status = "partial"
	}
	job.VisitedNodes, job.EdgeCount, job.FinishedAt = len(extension.Nodes), len(extension.Edges), m.clock().UTC()
	if !m.save(ctx, job) {
		m.fail(ctx, job, errors.New("failed to persist trace extension"))
	}
}

func extensionSyncBounds(anchors []extensionAnchor) (int64, int64) {
	var start, end int64
	for _, anchor := range anchors {
		if anchor.FromBlock > 0 && (start == 0 || anchor.FromBlock < start) {
			start = anchor.FromBlock
		}
		if anchor.ToBlock > end {
			end = anchor.ToBlock
		}
	}
	return start, end
}

func decodeResult(value any) (Result, error) {
	if result, ok := value.(Result); ok {
		return result, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return Result{}, err
	}
	var result Result
	err = json.Unmarshal(data, &result)
	return result, err
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
	return strings.ToLower(q.Chain + ":" + q.Address + ":" + q.Direction + ":" + q.Asset + ":" + fmt.Sprint(q.Depth))
}
