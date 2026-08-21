package syncer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrInvalidRequest = errors.New("invalid sync request")
	ErrQueueFull      = errors.New("sync queue is full")
	addressPattern    = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
)

type Source interface {
	etherscan.Client
}

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	SetAddressSyncing(context.Context, string, int64, string) error
	CompleteAddressSync(context.Context, string, string, int64, int64, time.Time) error
	FailAddressSync(context.Context, string, string, string) error
	BulkUpsertTransfers(context.Context, []store.Transfer) (int64, error)
	UpsertDiscoveredAddresses(context.Context, string, int64, []string, time.Time) error
	TopNeighbors(context.Context, string, string, int) ([]string, error)
	CreateSyncJob(context.Context, *store.SyncJob) error
	GetSyncJob(context.Context, primitive.ObjectID) (store.SyncJob, error)
	SaveSyncJob(context.Context, store.SyncJob) error
	FailInterruptedJobs(context.Context, time.Time) error
}

type Request struct {
	Chain         string
	Address       string
	StartBlock    int64
	NeighborLimit int
}

type Config struct {
	CacheTTL      time.Duration
	Confirmations int64
	QueueSize     int
	Clock         func() time.Time
}

type queuedJob struct {
	id      primitive.ObjectID
	request Request
}

type Manager struct {
	source     Source
	repository Repository
	config     Config
	queue      chan queuedJob
	mu         sync.Mutex
	active     map[string]primitive.ObjectID
}

func New(source Source, repository Repository, config Config) *Manager {
	if config.CacheTTL <= 0 {
		config.CacheTTL = 15 * time.Minute
	}
	if config.Confirmations < 0 {
		config.Confirmations = 0
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 100
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Manager{source: source, repository: repository, config: config, queue: make(chan queuedJob, config.QueueSize), active: make(map[string]primitive.ObjectID)}
}

func (m *Manager) Enqueue(ctx context.Context, request Request) (store.SyncJob, error) {
	request.Chain = strings.ToLower(strings.TrimSpace(request.Chain))
	if request.Chain == "" {
		request.Chain = "ethereum"
	}
	request.Address = strings.ToLower(strings.TrimSpace(request.Address))
	if request.Chain != "ethereum" || !addressPattern.MatchString(request.Address) || request.StartBlock < 0 || request.NeighborLimit < 0 || request.NeighborLimit > 10 {
		return store.SyncJob{}, ErrInvalidRequest
	}
	key := request.Chain + ":" + request.Address
	m.mu.Lock()
	if id, ok := m.active[key]; ok {
		m.mu.Unlock()
		return m.repository.GetSyncJob(ctx, id)
	}
	job := store.SyncJob{
		ID: primitive.NewObjectID(), Chain: request.Chain, ChainID: 1, Address: request.Address,
		StartBlock: request.StartBlock, NeighborLimit: request.NeighborLimit, Status: "queued",
		CreatedAt: m.config.Clock().UTC(), TotalAddresses: 1, ActionCounts: make(map[string]int64),
	}
	if err := m.repository.CreateSyncJob(ctx, &job); err != nil {
		m.mu.Unlock()
		return store.SyncJob{}, err
	}
	m.active[key] = job.ID
	m.mu.Unlock()

	select {
	case m.queue <- queuedJob{id: job.ID, request: request}:
		return job, nil
	default:
		m.release(request)
		job.Status, job.ErrorCode, job.Error, job.Retryable = "failed", "queue_full", ErrQueueFull.Error(), true
		job.FinishedAt = m.config.Clock().UTC()
		_ = m.repository.SaveSyncJob(ctx, job)
		return store.SyncJob{}, ErrQueueFull
	}
}

func (m *Manager) Job(ctx context.Context, id string) (store.SyncJob, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return store.SyncJob{}, ErrInvalidRequest
	}
	return m.repository.GetSyncJob(ctx, objectID)
}

func (m *Manager) Run(ctx context.Context) error {
	if err := m.repository.FailInterruptedJobs(ctx, m.config.Clock().UTC()); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case queued := <-m.queue:
			m.process(ctx, queued)
		}
	}
}

type addressResult struct {
	cached       bool
	fetched      int64
	actionCounts map[string]int64
}

func (m *Manager) process(ctx context.Context, queued queuedJob) {
	defer m.release(queued.request)
	job, err := m.repository.GetSyncJob(ctx, queued.id)
	if err != nil {
		return
	}
	job.Status, job.StartedAt = "running", m.config.Clock().UTC()
	_ = m.repository.SaveSyncJob(ctx, job)

	var safeHead *int64
	getSafeHead := func() (int64, error) {
		if safeHead != nil {
			return *safeHead, nil
		}
		latest, err := m.source.LatestBlock(ctx)
		if err != nil {
			return 0, err
		}
		value := latest - m.config.Confirmations
		if value < 0 {
			value = 0
		}
		safeHead = &value
		job.SafeHead = value
		return value, nil
	}

	seedResult, err := m.syncAddress(ctx, queued.request, getSafeHead)
	if err != nil {
		m.finishFailed(ctx, &job, err)
		return
	}
	m.mergeResult(&job, seedResult)
	job.CompletedAddresses = 1

	neighbors, err := m.repository.TopNeighbors(ctx, queued.request.Chain, queued.request.Address, queued.request.NeighborLimit)
	if err != nil {
		m.finishFailed(ctx, &job, err)
		return
	}
	job.TotalAddresses = 1 + len(neighbors)
	for _, neighbor := range neighbors {
		request := queued.request
		request.Address, request.NeighborLimit = neighbor, 0
		result, syncErr := m.syncAddress(ctx, request, getSafeHead)
		if syncErr != nil {
			code, retryable := classify(syncErr)
			job.FailedNeighbors = append(job.FailedNeighbors, store.SyncFailure{Address: neighbor, Code: code, Message: syncErr.Error(), Retryable: retryable})
			continue
		}
		m.mergeResult(&job, result)
		job.CompletedAddresses++
		job.SuccessfulNeighbors = append(job.SuccessfulNeighbors, neighbor)
	}
	job.Status = "succeeded"
	if len(job.FailedNeighbors) > 0 {
		job.Status = "partial"
	}
	m.finish(ctx, &job)
}

func (m *Manager) syncAddress(ctx context.Context, request Request, getSafeHead func() (int64, error)) (addressResult, error) {
	now := m.config.Clock().UTC()
	address, exists, err := m.repository.FindAddress(ctx, request.Chain, request.Address)
	if err != nil {
		return addressResult{}, err
	}
	if exists && address.SyncStatus == "synced" && request.StartBlock >= address.EarliestSyncedBlock && now.Sub(address.LastSyncedAt) < m.config.CacheTTL {
		return addressResult{cached: true, actionCounts: make(map[string]int64)}, nil
	}
	safeHead, err := getSafeHead()
	if err != nil {
		return addressResult{}, err
	}
	if err := m.repository.SetAddressSyncing(ctx, request.Chain, 1, request.Address); err != nil {
		return addressResult{}, err
	}

	intervals := make([][2]int64, 0, 2)
	earliest := request.StartBlock
	latest := safeHead
	if exists && address.SyncStatus == "synced" {
		earliest, latest = address.EarliestSyncedBlock, address.LatestSyncedBlock
		if request.StartBlock < earliest {
			intervals = append(intervals, [2]int64{request.StartBlock, earliest - 1})
			earliest = request.StartBlock
		}
		if safeHead > latest {
			intervals = append(intervals, [2]int64{latest + 1, safeHead})
			latest = safeHead
		}
	} else if request.StartBlock <= safeHead {
		intervals = append(intervals, [2]int64{request.StartBlock, safeHead})
	} else {
		return addressResult{}, fmt.Errorf("%w: start block exceeds safe head", ErrInvalidRequest)
	}

	result := addressResult{actionCounts: make(map[string]int64)}
	discovered := make(map[string]struct{})
	for _, interval := range intervals {
		for _, action := range m.actions() {
			count, fetchErr := m.fetchRange(ctx, action.call, action.name, request.Address, interval[0], interval[1], discovered)
			if fetchErr != nil {
				_ = m.repository.FailAddressSync(ctx, request.Chain, request.Address, fetchErr.Error())
				return addressResult{}, fetchErr
			}
			result.fetched += count
			result.actionCounts[action.name] += count
		}
	}
	addresses := make([]string, 0, len(discovered))
	delete(discovered, request.Address)
	for value := range discovered {
		addresses = append(addresses, value)
	}
	if err := m.repository.UpsertDiscoveredAddresses(ctx, request.Chain, 1, addresses, now); err != nil {
		return addressResult{}, err
	}
	if err := m.repository.CompleteAddressSync(ctx, request.Chain, request.Address, earliest, latest, now); err != nil {
		return addressResult{}, err
	}
	return result, nil
}

type action struct {
	name string
	call func(context.Context, string, int64, int64) ([]store.Transfer, error)
}

func (m *Manager) actions() []action {
	return []action{
		{name: "txlist", call: m.source.ListTransactions},
		{name: "txlistinternal", call: m.source.ListInternalTransactions},
		{name: "tokentx", call: m.source.ListTokenTransfers},
	}
}

func (m *Manager) fetchRange(ctx context.Context, call func(context.Context, string, int64, int64) ([]store.Transfer, error), actionName, address string, start, end int64, discovered map[string]struct{}) (int64, error) {
	transfers, err := call(ctx, address, start, end)
	if errors.Is(err, etherscan.ErrPageLimit) {
		if start == end {
			return 0, fmt.Errorf("%w: action=%s block=%d", etherscan.ErrPageLimit, actionName, start)
		}
		middle := start + (end-start)/2
		left, leftErr := m.fetchRange(ctx, call, actionName, address, start, middle, discovered)
		if leftErr != nil {
			return 0, leftErr
		}
		right, rightErr := m.fetchRange(ctx, call, actionName, address, middle+1, end, discovered)
		return left + right, rightErr
	}
	if err != nil {
		return 0, err
	}
	for index := range transfers {
		transfers[index].Chain, transfers[index].ChainID = "ethereum", 1
		transfers[index].From = strings.ToLower(transfers[index].From)
		transfers[index].To = strings.ToLower(transfers[index].To)
		if transfers[index].AssetType == "erc20" {
			transfers[index].Asset = strings.ToLower(transfers[index].Asset)
		}
		if addressPattern.MatchString(transfers[index].From) {
			discovered[transfers[index].From] = struct{}{}
		}
		if addressPattern.MatchString(transfers[index].To) {
			discovered[transfers[index].To] = struct{}{}
		}
	}
	for startIndex := 0; startIndex < len(transfers); startIndex += 1000 {
		endIndex := min(startIndex+1000, len(transfers))
		if _, err := m.repository.BulkUpsertTransfers(ctx, transfers[startIndex:endIndex]); err != nil {
			return 0, err
		}
	}
	return int64(len(transfers)), nil
}

func (m *Manager) mergeResult(job *store.SyncJob, result addressResult) {
	job.Fetched += result.fetched
	if result.cached {
		job.CachedAddresses++
	}
	for name, count := range result.actionCounts {
		job.ActionCounts[name] += count
	}
}

func (m *Manager) finishFailed(ctx context.Context, job *store.SyncJob, err error) {
	job.Status = "failed"
	job.ErrorCode, job.Retryable = classify(err)
	job.Error = err.Error()
	m.finish(ctx, job)
}

func (m *Manager) finish(ctx context.Context, job *store.SyncJob) {
	job.FinishedAt = m.config.Clock().UTC()
	job.DurationMS = job.FinishedAt.Sub(job.StartedAt).Milliseconds()
	_ = m.repository.SaveSyncJob(ctx, *job)
}

func (m *Manager) release(request Request) {
	m.mu.Lock()
	delete(m.active, request.Chain+":"+request.Address)
	m.mu.Unlock()
}

func classify(err error) (string, bool) {
	switch {
	case errors.Is(err, etherscan.ErrRateLimited):
		return "etherscan_rate_limited", true
	case errors.Is(err, etherscan.ErrTransient), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "transient", true
	case errors.Is(err, etherscan.ErrPageLimit):
		return "page_limit", false
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_request", false
	default:
		return "sync_failed", true
	}
}
