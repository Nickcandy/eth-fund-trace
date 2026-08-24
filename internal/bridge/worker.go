package bridge

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/chainrpc"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type WorkerRepository interface {
	QueryCrossChainLinks(context.Context, store.BridgeLinkQuery) ([]store.CrossChainLink, error)
	FindCrossChainLink(context.Context, primitive.ObjectID) (store.CrossChainLink, error)
	UpsertCrossChainLink(context.Context, store.CrossChainLink) (store.CrossChainLink, error)
}

type WorkerConfig struct {
	Interval       time.Duration
	BatchSize      int64
	MaxConcurrency int
	MaxRetries     int
	Timeout        time.Duration
	Clock          func() time.Time
}

type Worker struct {
	analyzer *Analyzer
	repo     WorkerRepository
	config   WorkerConfig
	queue    chan primitive.ObjectID
}

func NewWorker(analyzer *Analyzer, repo WorkerRepository, config WorkerConfig) *Worker {
	if config.Interval <= 0 {
		config.Interval = time.Minute
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 50
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 2
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 8
	}
	if config.Timeout <= 0 {
		config.Timeout = 20 * time.Second
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Worker{analyzer: analyzer, repo: repo, config: config, queue: make(chan primitive.ObjectID, config.BatchSize)}
}

func (w *Worker) Enqueue(id string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return ErrInvalidRequest
	}
	select {
	case w.queue <- objectID:
		return nil
	default:
		return errors.New("bridge sync queue full")
	}
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case id := <-w.queue:
			if link, err := w.repo.FindCrossChainLink(ctx, id); err == nil {
				w.process(ctx, []store.CrossChainLink{link})
			}
		case <-ticker.C:
			links, err := w.repo.QueryCrossChainLinks(ctx, store.BridgeLinkQuery{DueBefore: w.config.Clock().UTC(), Limit: w.config.BatchSize})
			if err == nil {
				w.process(ctx, links)
			}
		}
	}
}

func (w *Worker) process(ctx context.Context, links []store.CrossChainLink) {
	sem := make(chan struct{}, w.config.MaxConcurrency)
	var group sync.WaitGroup
	for _, link := range links {
		if link.RetryCount >= w.config.MaxRetries || link.Status == "completed" || link.Status == "confirmed" {
			continue
		}
		group.Add(1)
		sem <- struct{}{}
		go func(link store.CrossChainLink) {
			defer group.Done()
			defer func() { <-sem }()
			callCtx, cancel := context.WithTimeout(ctx, w.config.Timeout)
			defer cancel()
			updated, err := w.analyzer.Refresh(callCtx, link)
			if err == nil {
				return
			}
			updated.RetryCount = link.RetryCount + 1
			updated.LastErrorCode = classifyBridgeError(err)
			delay := w.config.Interval * time.Duration(1<<min(updated.RetryCount, 6))
			updated.NextCheckAt = w.config.Clock().UTC().Add(delay)
			_, _ = w.repo.UpsertCrossChainLink(ctx, updated)
		}(link)
	}
	group.Wait()
}

func classifyBridgeError(err error) string {
	switch {
	case errors.Is(err, chainrpc.ErrRateLimited):
		return "bridge_upstream_rate_limited"
	case errors.Is(err, chainrpc.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		return "bridge_upstream_unavailable"
	case errors.Is(err, ErrMalformedEvent):
		return "bridge_malformed_event"
	case errors.Is(err, ErrAmbiguousMatch):
		return "bridge_ambiguous_match"
	default:
		return "bridge_sync_failed"
	}
}
