package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/chains"
	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrInvalidRequest = errors.New("invalid sync request")
	ErrQueueFull      = errors.New("sync queue is full")
)

type Source interface {
	etherscan.Client
}

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	SetAddressSyncing(context.Context, string, int64, string) error
	CompleteAddressSync(context.Context, string, string, int64, int64, int64, int64, time.Time) error
	CompleteAddressPartial(context.Context, string, string, int64, int64, time.Time) error
	FailAddressSync(context.Context, string, string, string) error
	BulkUpsertTransfers(context.Context, []store.Transfer) (int64, error)
	UpsertDiscoveredAddresses(context.Context, string, int64, []string, time.Time) error
	TopNeighbors(context.Context, string, string, int) ([]string, error)
	CreateSyncJob(context.Context, *store.SyncJob) error
	GetSyncJob(context.Context, primitive.ObjectID) (store.SyncJob, error)
	FindLatestSyncJob(context.Context, string, string) (store.SyncJob, error)
	SaveSyncJob(context.Context, store.SyncJob) error
	FailInterruptedJobs(context.Context, time.Time) error
	FindSyncCheckpoints(context.Context, string, string, int64, int64, int64) (map[string]int64, error)
}

type Request struct {
	Chain         string
	Address       string
	StartBlock    int64
	NeighborLimit int
}

type Config struct {
	CacheTTL               time.Duration
	DisableCache           bool
	Confirmations          int64
	QueueSize              int
	InternalLookbackBlocks int64
	HistoryLookbackBlocks  int64
	MaxRecordsPerAction    int64
	StartBlocks            map[string]int64
	Clock                  func() time.Time
	AfterAddressSynced     func(context.Context, string, string) error
	OnTransfersPersisted   func(context.Context, string, []store.Transfer)
}

type queuedJob struct {
	id      primitive.ObjectID
	request Request
}

type Manager struct {
	sources    map[string]Source
	repository Repository
	config     Config
	queue      chan queuedJob
	mu         sync.Mutex
	active     map[string]primitive.ObjectID
	cancelMu   sync.Mutex
	cancels    map[string]context.CancelFunc
}

func New(source Source, repository Repository, config Config) *Manager {
	return NewMulti(map[string]Source{"ethereum": source}, repository, config)
}

func NewMulti(sources map[string]Source, repository Repository, config Config) *Manager {
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
	if config.HistoryLookbackBlocks < 0 {
		config.HistoryLookbackBlocks = 0
	}
	if config.MaxRecordsPerAction < 0 {
		config.MaxRecordsPerAction = 0
	}
	return &Manager{sources: sources, repository: repository, config: config, queue: make(chan queuedJob, config.QueueSize), active: make(map[string]primitive.ObjectID), cancels: make(map[string]context.CancelFunc)}
}

func (m *Manager) Enqueue(ctx context.Context, request Request) (store.SyncJob, error) {
	chain, chainErr := chains.Resolve(request.Chain)
	request.Chain = chain.Name
	if request.StartBlock == 0 {
		request.StartBlock = m.config.StartBlocks[request.Chain]
	}
	normalizedAddress, err := ethaddr.Normalize(request.Address)
	request.Address = normalizedAddress
	if chainErr != nil || m.sources[request.Chain] == nil || err != nil || request.StartBlock < 0 || request.NeighborLimit < 0 || request.NeighborLimit > 10 {
		return store.SyncJob{}, ErrInvalidRequest
	}
	key := request.Chain + ":" + request.Address
	m.mu.Lock()
	if id, ok := m.active[key]; ok {
		m.mu.Unlock()
		return m.repository.GetSyncJob(ctx, id)
	}
	job := store.SyncJob{
		ID: primitive.NewObjectID(), Chain: request.Chain, ChainID: chain.ID, Address: request.Address,
		StartBlock: request.StartBlock, NeighborLimit: request.NeighborLimit, Status: "queued",
		CreatedAt: m.config.Clock().UTC(), TotalAddresses: 1, ActionCounts: make(map[string]int64),
		InternalLookbackBlocks: m.config.InternalLookbackBlocks, MaxRecordsPerAction: m.config.MaxRecordsPerAction,
	}
	checkpoints, err := m.repository.FindSyncCheckpoints(ctx, request.Chain, request.Address, request.StartBlock, m.config.InternalLookbackBlocks, m.config.MaxRecordsPerAction)
	if err != nil {
		m.mu.Unlock()
		return store.SyncJob{}, err
	}
	job.Progress.ActionCheckpoints = checkpoints
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
func (m *Manager) Stop(ctx context.Context, id string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return ErrInvalidRequest
	}
	job, err := m.repository.GetSyncJob(ctx, objectID)
	if err != nil {
		return err
	}
	if job.Status == "queued" || job.Status == "running" {
		job.Status, job.ErrorCode, job.Error, job.Retryable = "stopped", "stopped_by_user", "stopped by user", false
		job.FinishedAt = m.config.Clock().UTC()
		if err := m.repository.SaveSyncJob(ctx, job); err != nil {
			return err
		}
		if err := m.repository.FailAddressSync(ctx, job.Chain, job.Address, job.Error); err != nil {
			return fmt.Errorf("mark stopped address failed: %w", err)
		}
		m.cancelMu.Lock()
		if cancel := m.cancels[id]; cancel != nil {
			cancel()
		}
		m.cancelMu.Unlock()
	}
	return nil
}

func (m *Manager) LatestJob(ctx context.Context, chainName, address string) (store.SyncJob, error) {
	chain, chainErr := chains.Resolve(chainName)
	normalizedAddress, addressErr := ethaddr.Normalize(address)
	if chainErr != nil || m.sources[chain.Name] == nil || addressErr != nil {
		return store.SyncJob{}, ErrInvalidRequest
	}
	return m.repository.FindLatestSyncJob(ctx, chain.Name, normalizedAddress)
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
	cached           bool
	fetched          int64
	actionCounts     map[string]int64
	truncatedActions []string
}

type progressReporter func(func(*store.SyncProgress))

func (m *Manager) process(ctx context.Context, queued queuedJob) {
	jobCtx, cancel := context.WithCancel(ctx)
	m.cancelMu.Lock()
	m.cancels[queued.id.Hex()] = cancel
	m.cancelMu.Unlock()
	defer func() { cancel(); m.cancelMu.Lock(); delete(m.cancels, queued.id.Hex()); m.cancelMu.Unlock() }()
	defer m.release(queued.request)
	source := m.sources[queued.request.Chain]
	chain, _ := chains.Resolve(queued.request.Chain)
	job, err := m.repository.GetSyncJob(ctx, queued.id)
	if err != nil {
		return
	}
	if job.Status == "stopped" {
		return
	}
	job.Status, job.StartedAt = "running", m.config.Clock().UTC()
	if err := m.repository.SaveSyncJob(ctx, job); err != nil {
		slog.Error("failed to mark sync job running", "job_id", job.ID.Hex(), "error", err)
		return
	}
	report := func(update func(*store.SyncProgress)) {
		update(&job.Progress)
		job.Progress.UpdatedAt = m.config.Clock().UTC()
		m.saveProgress(ctx, job)
	}

	var safeHead *int64
	getSafeHead := func() (int64, error) {
		if safeHead != nil {
			return *safeHead, nil
		}
		latest, err := source.LatestBlock(ctx)
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

	seedResult, err := m.syncAddress(jobCtx, source, chain.ID, queued.request, job.Progress.ActionCheckpoints, getSafeHead, report)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		m.finishFailed(ctx, &job, err)
		return
	}
	m.mergeResult(&job, seedResult)
	job.CompletedAddresses = 1
	job.ProcessedAddresses = 1

	var neighbors []string
	if queued.request.NeighborLimit > 0 {
		neighbors, err = m.repository.TopNeighbors(ctx, queued.request.Chain, queued.request.Address, queued.request.NeighborLimit)
		if err != nil {
			m.finishFailed(ctx, &job, err)
			return
		}
	}
	job.TotalAddresses = 1 + len(neighbors)
	m.saveProgress(ctx, job)
	for _, neighbor := range neighbors {
		request := queued.request
		request.Address, request.NeighborLimit = neighbor, 0
		checkpoints, checkpointErr := m.repository.FindSyncCheckpoints(ctx, request.Chain, request.Address, request.StartBlock, m.config.InternalLookbackBlocks, m.config.MaxRecordsPerAction)
		if checkpointErr != nil {
			m.finishFailed(ctx, &job, checkpointErr)
			return
		}
		result, syncErr := m.syncAddress(jobCtx, source, chain.ID, request, checkpoints, getSafeHead, report)
		if syncErr != nil {
			if errors.Is(syncErr, context.Canceled) {
				return
			}
			code, retryable := classify(syncErr)
			job.FailedNeighbors = append(job.FailedNeighbors, store.SyncFailure{Address: neighbor, Code: code, Message: syncErr.Error(), Retryable: retryable})
			job.ProcessedAddresses++
			m.saveProgress(ctx, job)
			continue
		}
		m.mergeResult(&job, result)
		job.CompletedAddresses++
		job.ProcessedAddresses++
		job.SuccessfulNeighbors = append(job.SuccessfulNeighbors, neighbor)
		m.saveProgress(ctx, job)
	}
	job.Status = "succeeded"
	if len(job.FailedNeighbors) > 0 || len(job.TruncatedActions) > 0 {
		job.Status = "partial"
	}
	if len(job.TruncatedActions) > 0 {
		job.ErrorCode = "record_limit"
		job.Error = "record limit reached: " + strings.Join(job.TruncatedActions, ",")
	}
	m.finish(ctx, &job)
}

func (m *Manager) syncAddress(ctx context.Context, source Source, chainID int64, request Request, checkpoints map[string]int64, getSafeHead func() (int64, error), report progressReporter) (addressResult, error) {
	now := m.config.Clock().UTC()
	address, exists, err := m.repository.FindAddress(ctx, request.Chain, request.Address)
	if err != nil {
		return addressResult{}, err
	}
	cacheInternalFrom := request.StartBlock
	cacheHistoryFrom := request.StartBlock
	if m.config.HistoryLookbackBlocks > 0 {
		cacheHistoryFrom = max(cacheHistoryFrom, address.LatestSyncedBlock-m.config.HistoryLookbackBlocks+1)
	}
	if exists && m.config.InternalLookbackBlocks > 0 {
		cacheInternalFrom = max(cacheInternalFrom, address.LatestSyncedBlock-m.config.InternalLookbackBlocks+1)
	}
	haveInternalFrom, haveInternalTo := address.InternalSyncedFrom, address.InternalSyncedTo
	if exists && address.SyncStatus == "synced" && haveInternalFrom == 0 && haveInternalTo == 0 {
		haveInternalFrom, haveInternalTo = address.EarliestSyncedBlock, address.LatestSyncedBlock
	}
	internalCached := exists && haveInternalFrom <= cacheInternalFrom && haveInternalTo >= address.LatestSyncedBlock
	fullCache := address.SyncStatus == "synced" && cacheHistoryFrom >= address.EarliestSyncedBlock && internalCached
	partialCache := address.SyncStatus == "partial" && m.config.MaxRecordsPerAction > 0 && address.SyncMaxRecordsPerAction == m.config.MaxRecordsPerAction
	if !m.config.DisableCache && exists && (fullCache || partialCache) && now.Sub(address.LastSyncedAt) < m.config.CacheTTL {
		if m.config.AfterAddressSynced != nil {
			if err := m.config.AfterAddressSynced(ctx, request.Chain, request.Address); err != nil {
				return addressResult{}, err
			}
		}
		return addressResult{cached: true, actionCounts: make(map[string]int64)}, nil
	}
	safeHead, err := getSafeHead()
	if err != nil {
		return addressResult{}, err
	}
	if request.StartBlock > safeHead {
		return addressResult{}, fmt.Errorf("%w: start block exceeds safe head", ErrInvalidRequest)
	}
	historyFrom := request.StartBlock
	if m.config.HistoryLookbackBlocks > 0 {
		historyFrom = max(historyFrom, safeHead-m.config.HistoryLookbackBlocks+1)
	}
	internalFrom := historyFrom
	if m.config.InternalLookbackBlocks > 0 {
		internalFrom = max(internalFrom, safeHead-m.config.InternalLookbackBlocks+1)
	}
	if err := m.repository.SetAddressSyncing(ctx, request.Chain, chainID, request.Address); err != nil {
		return addressResult{}, err
	}

	intervals := make([][2]int64, 0, 2)
	earliest := historyFrom
	latest := safeHead
	if exists && address.SyncStatus == "synced" {
		earliest, latest = address.EarliestSyncedBlock, address.LatestSyncedBlock
		if historyFrom < earliest {
			intervals = append(intervals, [2]int64{historyFrom, earliest - 1})
			earliest = historyFrom
		}
		if safeHead > latest {
			intervals = append(intervals, [2]int64{latest + 1, safeHead})
			latest = safeHead
		}
	} else {
		intervals = append(intervals, [2]int64{historyFrom, safeHead})
	}
	internalIntervals := coverageIntervals(internalFrom, safeHead, haveInternalFrom, haveInternalTo)
	if m.config.MaxRecordsPerAction > 0 {
		intervals = [][2]int64{{historyFrom, safeHead}}
		internalIntervals = [][2]int64{{internalFrom, safeHead}}
	}

	result := addressResult{actionCounts: make(map[string]int64)}
	discovered := make(map[string]struct{})
	for _, action := range m.actions(source, func(page etherscan.PageProgress) {
		report(func(progress *store.SyncProgress) {
			progress.CurrentAddress = request.Address
			progress.CurrentAction = page.Action
			progress.RangeStart = page.StartBlock
			progress.RangeEnd = page.EndBlock
			progress.CurrentPage = page.Page
			progress.PagesFetched++
			progress.RecordsRead += int64(page.Items)
		})
	}) {
		actionIntervals := intervals
		if action.name == "txlistinternal" {
			actionIntervals = internalIntervals
		}
		for _, interval := range actionIntervals {
			if m.config.MaxRecordsPerAction > 0 {
				end := interval[1]
				if checkpoint, found := checkpoints[action.name]; found {
					end = min(end, checkpoint)
				}
				if end < interval[0] {
					continue
				}
				count, truncated, fetchErr := m.fetchRecentRange(ctx, action.call, action.name, request.Chain, chainID, request.Address, interval[0], end, m.config.MaxRecordsPerAction, discovered, report)
				if fetchErr != nil {
					if err := m.repository.FailAddressSync(ctx, request.Chain, request.Address, fetchErr.Error()); err != nil {
						slog.Error("failed to persist address sync failure", "address", request.Address, "error", err)
					}
					return addressResult{}, fetchErr
				}
				result.fetched += count
				result.actionCounts[action.name] += count
				if truncated {
					result.truncatedActions = append(result.truncatedActions, action.name)
				}
				continue
			}
			start := interval[0]
			if checkpoint, found := checkpoints[action.name]; found && checkpoint >= start {
				start = checkpoint + 1
			}
			if start > interval[1] {
				continue
			}
			count, fetchErr := m.fetchRange(ctx, action.call, action.name, request.Chain, chainID, request.Address, start, interval[1], discovered, report)
			if fetchErr != nil {
				if err := m.repository.FailAddressSync(ctx, request.Chain, request.Address, fetchErr.Error()); err != nil {
					slog.Error("failed to persist address sync failure", "address", request.Address, "error", err)
				}
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
	if err := m.repository.UpsertDiscoveredAddresses(ctx, request.Chain, chainID, addresses, now); err != nil {
		return addressResult{}, err
	}
	if len(result.truncatedActions) > 0 {
		if err := m.repository.CompleteAddressPartial(ctx, request.Chain, request.Address, safeHead, m.config.MaxRecordsPerAction, now); err != nil {
			return addressResult{}, err
		}
	} else {
		if err := m.repository.CompleteAddressSync(ctx, request.Chain, request.Address, earliest, latest, internalFrom, safeHead, now); err != nil {
			return addressResult{}, err
		}
	}
	if m.config.AfterAddressSynced != nil {
		if err := m.config.AfterAddressSynced(ctx, request.Chain, request.Address); err != nil {
			return addressResult{}, err
		}
	}
	return result, nil
}

func coverageIntervals(wantFrom, wantTo, haveFrom, haveTo int64) [][2]int64 {
	if haveFrom <= 0 || haveTo < haveFrom {
		return [][2]int64{{wantFrom, wantTo}}
	}
	intervals := make([][2]int64, 0, 2)
	if wantFrom < haveFrom {
		intervals = append(intervals, [2]int64{wantFrom, haveFrom - 1})
	}
	if wantTo > haveTo {
		intervals = append(intervals, [2]int64{max(wantFrom, haveTo+1), wantTo})
	}
	return intervals
}

type action struct {
	name string
	call func(context.Context, string, int64, int64) ([]store.Transfer, error)
}

func (m *Manager) actions(source Source, progress etherscan.ProgressFunc) []action {
	if source, ok := source.(etherscan.ProgressClient); ok {
		return []action{
			{name: "txlist", call: func(ctx context.Context, address string, start, end int64) ([]store.Transfer, error) {
				return source.ListTransactionsWithProgress(ctx, address, start, end, progress)
			}},
			{name: "txlistinternal", call: func(ctx context.Context, address string, start, end int64) ([]store.Transfer, error) {
				return source.ListInternalTransactionsWithProgress(ctx, address, start, end, progress)
			}},
			{name: "tokentx", call: func(ctx context.Context, address string, start, end int64) ([]store.Transfer, error) {
				return source.ListTokenTransfersWithProgress(ctx, address, start, end, progress)
			}},
		}
	}
	return []action{
		{name: "txlist", call: source.ListTransactions},
		{name: "txlistinternal", call: source.ListInternalTransactions},
		{name: "tokentx", call: source.ListTokenTransfers},
	}
}

func (m *Manager) fetchRange(ctx context.Context, call func(context.Context, string, int64, int64) ([]store.Transfer, error), actionName, chain string, chainID int64, address string, start, end int64, discovered map[string]struct{}, report progressReporter) (int64, error) {
	transfers, err := call(ctx, address, start, end)
	if errors.Is(err, etherscan.ErrPageLimit) {
		if start == end {
			return 0, fmt.Errorf("%w: action=%s block=%d", etherscan.ErrPageLimit, actionName, start)
		}
		if len(transfers) == 0 {
			middle := start + (end-start)/2
			report(func(progress *store.SyncProgress) { progress.SplitCount++ })
			left, leftErr := m.fetchRange(ctx, call, actionName, chain, chainID, address, start, middle, discovered, report)
			if leftErr != nil {
				return 0, leftErr
			}
			right, rightErr := m.fetchRange(ctx, call, actionName, chain, chainID, address, middle+1, end, discovered, report)
			return left + right, rightErr
		}
		resumeBlock := transfers[len(transfers)-1].BlockNumber
		var limitErr *etherscan.PageLimitError
		if errors.As(err, &limitErr) {
			resumeBlock = limitErr.LastBlock
		}
		complete := transfers[:0]
		for _, transfer := range transfers {
			if transfer.BlockNumber < resumeBlock {
				complete = append(complete, transfer)
			}
		}
		transfers = complete
		written, writeErr := m.persistTransfers(ctx, transfers, chain, chainID, discovered, report)
		if writeErr != nil {
			return 0, writeErr
		}
		if resumeBlock > start {
			report(func(progress *store.SyncProgress) {
				if progress.ActionCheckpoints == nil {
					progress.ActionCheckpoints = map[string]int64{}
				}
				progress.ActionCheckpoints[actionName] = resumeBlock - 1
			})
		}
		report(func(progress *store.SyncProgress) { progress.SplitCount++ })
		if resumeBlock <= start {
			blockCount, blockErr := m.fetchRange(ctx, call, actionName, chain, chainID, address, start, start, discovered, report)
			if blockErr != nil || start == end {
				return written + blockCount, blockErr
			}
			rest, restErr := m.fetchRange(ctx, call, actionName, chain, chainID, address, start+1, end, discovered, report)
			return written + blockCount + rest, restErr
		}
		rest, restErr := m.fetchRange(ctx, call, actionName, chain, chainID, address, resumeBlock, end, discovered, report)
		return written + rest, restErr
	}
	if err != nil {
		return 0, err
	}
	written, err := m.persistTransfers(ctx, transfers, chain, chainID, discovered, report)
	if err == nil {
		report(func(progress *store.SyncProgress) {
			if progress.ActionCheckpoints == nil {
				progress.ActionCheckpoints = map[string]int64{}
			}
			progress.ActionCheckpoints[actionName] = end
		})
	}
	return written, err
}

func (m *Manager) fetchRecentRange(ctx context.Context, call func(context.Context, string, int64, int64) ([]store.Transfer, error), actionName, chain string, chainID int64, address string, start, end, limit int64, discovered map[string]struct{}, report progressReporter) (int64, bool, error) {
	if limit <= 0 || end < start {
		return 0, false, nil
	}
	transfers, err := call(ctx, address, start, end)
	if err != nil && !errors.Is(err, etherscan.ErrPageLimit) {
		return 0, false, err
	}
	if !errors.Is(err, etherscan.ErrPageLimit) {
		truncated := int64(len(transfers)) > limit
		if truncated {
			transfers = transfers[:limit]
		}
		written, writeErr := m.persistTransfers(ctx, transfers, chain, chainID, discovered, report)
		if writeErr == nil {
			report(func(progress *store.SyncProgress) {
				if progress.ActionCheckpoints == nil {
					progress.ActionCheckpoints = map[string]int64{}
				}
				progress.ActionCheckpoints[actionName] = start - 1
			})
		}
		return written, truncated, writeErr
	}

	var limitErr *etherscan.PageLimitError
	if !errors.As(err, &limitErr) {
		return 0, false, err
	}
	boundary := limitErr.LastBlock
	if boundary <= start || boundary >= end {
		if int64(len(transfers)) > limit {
			transfers = transfers[:limit]
		}
		written, writeErr := m.persistTransfers(ctx, transfers, chain, chainID, discovered, report)
		return written, true, writeErr
	}
	complete := transfers[:0]
	for _, transfer := range transfers {
		if transfer.BlockNumber > boundary {
			complete = append(complete, transfer)
		}
	}
	if int64(len(complete)) >= limit {
		complete = complete[:limit]
		written, writeErr := m.persistTransfers(ctx, complete, chain, chainID, discovered, report)
		return written, true, writeErr
	}
	written, writeErr := m.persistTransfers(ctx, complete, chain, chainID, discovered, report)
	if writeErr != nil {
		return 0, false, writeErr
	}
	report(func(progress *store.SyncProgress) {
		if progress.ActionCheckpoints == nil {
			progress.ActionCheckpoints = map[string]int64{}
		}
		progress.ActionCheckpoints[actionName] = boundary
		progress.SplitCount++
	})
	rest, truncated, restErr := m.fetchRecentRange(ctx, call, actionName, chain, chainID, address, start, boundary, limit-int64(len(complete)), discovered, report)
	return written + rest, truncated, restErr
}

func (m *Manager) persistTransfers(ctx context.Context, transfers []store.Transfer, chain string, chainID int64, discovered map[string]struct{}, report progressReporter) (int64, error) {
	for index := range transfers {
		transfers[index].Chain, transfers[index].ChainID = chain, chainID
		transfers[index].TransactionGroup = fmt.Sprintf("%d:%s", chainID, strings.ToLower(transfers[index].TxHash))
		transfers[index].From = strings.ToLower(transfers[index].From)
		transfers[index].To = strings.ToLower(transfers[index].To)
		if transfers[index].AssetType == "erc20" {
			transfers[index].Asset = strings.ToLower(transfers[index].Asset)
		}
		if _, err := ethaddr.Normalize(transfers[index].From); err == nil && transfers[index].From != zeroAddress {
			discovered[transfers[index].From] = struct{}{}
		}
		if _, err := ethaddr.Normalize(transfers[index].To); err == nil && transfers[index].To != zeroAddress {
			discovered[transfers[index].To] = struct{}{}
		}
	}
	for startIndex := 0; startIndex < len(transfers); startIndex += 1000 {
		endIndex := min(startIndex+1000, len(transfers))
		if _, err := m.repository.BulkUpsertTransfers(ctx, transfers[startIndex:endIndex]); err != nil {
			return 0, err
		}
		if m.config.OnTransfersPersisted != nil {
			m.config.OnTransfersPersisted(ctx, chain, transfers[startIndex:endIndex])
		}
		count := int64(endIndex - startIndex)
		report(func(progress *store.SyncProgress) { progress.RecordsWritten += count })
	}
	return int64(len(transfers)), nil
}

const zeroAddress = "0x0000000000000000000000000000000000000000"

func (m *Manager) mergeResult(job *store.SyncJob, result addressResult) {
	if job.ActionCounts == nil {
		job.ActionCounts = make(map[string]int64)
	}
	job.Fetched += result.fetched
	if result.cached {
		job.CachedAddresses++
	}
	for name, count := range result.actionCounts {
		job.ActionCounts[name] += count
	}
	for _, action := range result.truncatedActions {
		if !slices.Contains(job.TruncatedActions, action) {
			job.TruncatedActions = append(job.TruncatedActions, action)
		}
	}
}

func (m *Manager) saveProgress(ctx context.Context, job store.SyncJob) {
	if err := m.repository.SaveSyncJob(ctx, job); err != nil {
		slog.Error("failed to persist sync job progress", "job_id", job.ID.Hex(), "error", err)
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
	if err := m.repository.SaveSyncJob(ctx, *job); err != nil {
		slog.Error("failed to persist final sync job state", "job_id", job.ID.Hex(), "status", job.Status, "error", err)
	}
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
