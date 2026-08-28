package propagation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/chains"
	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	// ErrInvalidRequest indicates invalid propagation input.
	ErrInvalidRequest = errors.New("invalid propagation request")
)

// JobRepository persists and atomically leases propagation jobs.
type JobRepository interface {
	CreatePropagationJob(context.Context, *store.PropagationJob) error
	FindPropagationJobByKey(context.Context, string) (store.PropagationJob, error)
	GetPropagationJob(context.Context, primitive.ObjectID) (store.PropagationJob, error)
	ClaimPropagationJob(context.Context, time.Time, time.Time, int) (store.PropagationJob, error)
	SupersedeLegacyPropagationJobs(context.Context, time.Time, string) error
	ExtendPropagationLease(context.Context, primitive.ObjectID, time.Time) error
	UpdatePropagationProgress(context.Context, primitive.ObjectID, int, int, int) error
	SavePropagationJob(context.Context, store.PropagationJob) error
	StopPropagationJob(context.Context, primitive.ObjectID, time.Time) (store.PropagationJob, error)
	UpsertRiskAssociation(context.Context, store.InferredRiskAssociation) error
	MarkRiskAssociationsStale(context.Context, string, string, string, int64) error
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	ListLabels(context.Context, string, string) ([]store.Label, error)
	ListRiskLabels(context.Context, string, int64) ([]store.Label, error)
}

// Request identifies one bounded propagation job.
type Request struct {
	Chain         string `json:"chain"`
	TargetAddress string `json:"targetAddress"`
	Direction     string `json:"direction"`
	Asset         string `json:"asset"`
}

// Manager leases, executes, and persists propagation jobs.
type Manager struct {
	engine     *Engine
	repository JobRepository
	config     Config
	clock      func() time.Time
	lease      time.Duration
	poll       time.Duration
	maxRetries int
	cancelMu   sync.Mutex
	cancels    map[string]context.CancelFunc
}

// NewManager creates a persistent propagation worker.
func NewManager(engine *Engine, repository JobRepository) *Manager {
	return &Manager{engine: engine, repository: repository, config: DefaultConfig(), clock: time.Now, lease: time.Minute, poll: 250 * time.Millisecond, maxRetries: 3, cancels: make(map[string]context.CancelFunc)}
}

// Enqueue validates and idempotently creates a propagation task.
func (m *Manager) Enqueue(ctx context.Context, request Request) (store.PropagationJob, error) {
	resolvedChain, chainErr := chains.Resolve(request.Chain)
	request.Chain = resolvedChain.Name
	request.Direction = strings.ToLower(strings.TrimSpace(request.Direction))
	if request.Direction == "" {
		request.Direction = "both"
	}
	if request.Asset == "" {
		request.Asset = "all"
	}
	address, err := ethaddr.Normalize(request.TargetAddress)
	if err != nil || chainErr != nil || (request.Direction != "in" && request.Direction != "out" && request.Direction != "both") {
		return store.PropagationJob{}, ErrInvalidRequest
	}
	if !strings.EqualFold(request.Asset, "ETH") && !strings.EqualFold(request.Asset, "all") {
		request.Asset, err = ethaddr.Normalize(request.Asset)
		if err != nil {
			return store.PropagationJob{}, ErrInvalidRequest
		}
	}
	request.TargetAddress = address
	metadata, found, err := m.repository.FindAddress(ctx, request.Chain, address)
	if err != nil {
		return store.PropagationJob{}, fmt.Errorf("find propagation target: %w", err)
	}
	if !found || metadata.SyncStatus != "synced" {
		return store.PropagationJob{}, errors.New("propagation target is not synchronized")
	}
	_, dataThroughBlock, covered := metadata.CommonCoverage()
	if !covered {
		return store.PropagationJob{}, errors.New("propagation target has no complete action coverage")
	}
	riskLabels, err := m.repository.ListRiskLabels(ctx, request.Chain, 100000)
	if err != nil {
		return store.PropagationJob{}, fmt.Errorf("list propagation risk labels: %w", err)
	}
	key := idempotencyKey(request, riskLabels, dataThroughBlock)
	job := store.PropagationJob{IdempotencyKey: key, Chain: request.Chain, TargetAddress: address, Asset: normalizeAsset(request.Asset), Direction: request.Direction, Status: "queued", MaxHops: 3, MaxNodes: m.config.MaxNodes, MaxEdges: m.config.MaxEdges, MaxAssetChannels: m.config.MaxAssetChannels, PerNodeCandidateCap: m.config.PerNodeCandidateCap, MaxPathsPerTarget: m.config.MaxPathsPerTarget, DataThroughBlock: dataThroughBlock, RuleVersion: RiskRuleVersion, PropagationVersion: Version, CreatedAt: m.clock().UTC()}
	if err := m.repository.CreatePropagationJob(ctx, &job); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return m.repository.FindPropagationJobByKey(ctx, key)
		}
		return store.PropagationJob{}, fmt.Errorf("create propagation job: %w", err)
	}
	return job, nil
}

// Job returns a propagation task by ID.
func (m *Manager) Job(ctx context.Context, id string) (store.PropagationJob, error) {
	parsed, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return store.PropagationJob{}, ErrInvalidRequest
	}
	return m.repository.GetPropagationJob(ctx, parsed)
}

// Stop cancels a local worker and persistently stops the task.
func (m *Manager) Stop(ctx context.Context, id string) (store.PropagationJob, error) {
	parsed, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return store.PropagationJob{}, ErrInvalidRequest
	}
	job, err := m.repository.StopPropagationJob(ctx, parsed, m.clock().UTC())
	if err != nil {
		return store.PropagationJob{}, err
	}
	m.cancelMu.Lock()
	if cancel := m.cancels[id]; cancel != nil {
		cancel()
	}
	m.cancelMu.Unlock()
	return job, nil
}

// Run continuously leases persisted propagation jobs until cancellation.
func (m *Manager) Run(ctx context.Context) error {
	if err := m.repository.SupersedeLegacyPropagationJobs(ctx, m.clock().UTC(), Version); err != nil {
		return fmt.Errorf("supersede legacy propagation jobs: %w", err)
	}
	ticker := time.NewTicker(m.poll)
	defer ticker.Stop()
	for {
		if err := m.claimAndProcess(ctx); err != nil && !errors.Is(err, mongo.ErrNoDocuments) && !errors.Is(err, context.Canceled) {
			slog.Error("propagation worker iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) claimAndProcess(ctx context.Context) error {
	now := m.clock().UTC()
	job, err := m.repository.ClaimPropagationJob(ctx, now, now.Add(m.lease), m.maxRetries)
	if err != nil {
		return err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	m.cancelMu.Lock()
	m.cancels[job.ID.Hex()] = cancel
	m.cancelMu.Unlock()
	defer func() {
		cancel()
		m.cancelMu.Lock()
		delete(m.cancels, job.ID.Hex())
		m.cancelMu.Unlock()
	}()
	heartbeatError := make(chan error, 1)
	go m.heartbeat(jobCtx, job.ID, cancel, heartbeatError)
	{
		lastHop, lastNodes := -1, -100
		config := m.config
		config.MaxHops, config.MaxNodes, config.MaxEdges = job.MaxHops, job.MaxNodes, job.MaxEdges
		config.MaxAssetChannels = job.MaxAssetChannels
		config.PerNodeCandidateCap, config.MaxPathsPerTarget = job.PerNodeCandidateCap, job.MaxPathsPerTarget
		result, runErr := m.engine.Run(jobCtx, job.Chain, job.TargetAddress, job.Direction, job.Asset, job.DataThroughBlock, nil, nil, config, func(hop, nodes, edges int) error {
			if hop == lastHop && nodes-lastNodes < 100 {
				return nil
			}
			lastHop, lastNodes = hop, nodes
			return m.repository.UpdatePropagationProgress(jobCtx, job.ID, hop, nodes, edges)
		})
		err = runErr
		if runErr == nil {
			for _, item := range result.Associations {
				if err != nil {
					break
				}
				record, recordErr := AssociationRecord(item, result.DataThroughBlock, m.clock().UTC())
				if recordErr != nil {
					err = recordErr
					break
				}
				if saveErr := m.repository.UpsertRiskAssociation(jobCtx, record); saveErr != nil {
					err = saveErr
					break
				}
			}
			if err == nil {
				err = m.repository.MarkRiskAssociationsStale(jobCtx, job.Chain, job.TargetAddress, Version, result.DataThroughBlock)
			}
			if err == nil {
				job.Status, job.Result = "succeeded", result
				if result.Status == "partial" || result.Status == "unknown" {
					job.Status = "partial"
				}
				job.Error, job.ErrorCode, job.Retryable = "", "", false
				job.CurrentHop, job.VisitedNodes, job.EdgeCount = m.config.MaxHops, result.VisitedNodes, result.EdgeCount
				job.DataThroughBlock, job.Truncated, job.TruncationReason = result.DataThroughBlock, result.Truncated, result.TruncationReason
			}
		}
	}
	cancel()
	if heartbeatErr := <-heartbeatError; heartbeatErr != nil && err == nil {
		err = heartbeatErr
	}
	job.FinishedAt = m.clock().UTC()
	job.LeaseUntil = time.Time{}
	if err != nil {
		job.Status, job.Error, job.ErrorCode, job.Retryable = "failed", err.Error(), "propagation_failed", true
		if errors.Is(err, context.Canceled) {
			job.ErrorCode = "interrupted"
		}
	}
	saveCtx := ctx
	if ctx.Err() != nil {
		saveCtx = context.Background()
	}
	if saveErr := m.repository.SavePropagationJob(saveCtx, job); saveErr != nil && !errors.Is(saveErr, context.Canceled) {
		return fmt.Errorf("save propagation job: %w", saveErr)
	}
	return nil
}

func (m *Manager) heartbeat(ctx context.Context, id primitive.ObjectID, cancel context.CancelFunc, result chan<- error) {
	ticker := time.NewTicker(m.lease / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			result <- nil
			return
		case <-ticker.C:
			if err := m.repository.ExtendPropagationLease(ctx, id, m.clock().UTC().Add(m.lease)); err != nil {
				result <- fmt.Errorf("extend propagation lease: %w", err)
				cancel()
				return
			}
		}
	}
}

func normalizeAsset(asset string) string {
	if strings.EqualFold(asset, "ETH") {
		return "ETH"
	}
	if strings.EqualFold(asset, "all") {
		return "all"
	}
	return strings.ToLower(asset)
}
func idempotencyKey(request Request, labels []store.Label, block int64) string {
	latest := time.Time{}
	for _, label := range labels {
		if label.ObservedAt.After(latest) {
			latest = label.ObservedAt
		}
	}
	parts := []string{request.Chain, request.TargetAddress, request.Direction, normalizeAsset(request.Asset), Version, RiskRuleVersion, fmt.Sprint(block), fmt.Sprint(len(labels)), latest.UTC().Format(time.RFC3339Nano)}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "|"))))
}
