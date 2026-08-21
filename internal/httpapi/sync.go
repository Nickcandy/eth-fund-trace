package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/syncer"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/mongo"
)

type SyncManager interface {
	Enqueue(context.Context, syncer.Request) (store.SyncJob, error)
	Job(context.Context, string) (store.SyncJob, error)
}

type SyncHandler struct {
	manager SyncManager
}

func NewSyncHandler(manager SyncManager) *SyncHandler {
	return &SyncHandler{manager: manager}
}

type syncRequest struct {
	Chain         string `json:"chain"`
	Address       string `json:"address"`
	StartBlock    int64  `json:"startBlock"`
	NeighborLimit *int   `json:"neighborLimit"`
}

func (h *SyncHandler) Enqueue(c echo.Context) error {
	var body syncRequest
	if err := c.Bind(&body); err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid JSON request", false)
	}
	limit := 10
	if body.NeighborLimit != nil {
		limit = *body.NeighborLimit
	}
	if body.Chain == "" {
		body.Chain = "ethereum"
	}
	job, err := h.manager.Enqueue(c.Request().Context(), syncer.Request{Chain: body.Chain, Address: body.Address, StartBlock: body.StartBlock, NeighborLimit: limit})
	if err != nil {
		return h.writeManagerError(c, err)
	}
	return c.JSON(http.StatusAccepted, jobResponse(job))
}

func (h *SyncHandler) Job(c echo.Context) error {
	job, err := h.manager.Job(c.Request().Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return writeError(c, http.StatusNotFound, "job_not_found", "sync job not found", false)
		}
		return h.writeManagerError(c, err)
	}
	return c.JSON(http.StatusOK, jobResponse(job))
}

func (h *SyncHandler) writeManagerError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, syncer.ErrInvalidRequest):
		return writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
	case errors.Is(err, syncer.ErrQueueFull):
		return writeError(c, http.StatusServiceUnavailable, "sync_queue_full", err.Error(), true)
	default:
		return writeError(c, http.StatusInternalServerError, "internal_error", "internal server error", true)
	}
}

func jobResponse(job store.SyncJob) map[string]any {
	return map[string]any{
		"jobId":               job.ID.Hex(),
		"chain":               job.Chain,
		"address":             job.Address,
		"status":              job.Status,
		"createdAt":           job.CreatedAt,
		"startedAt":           job.StartedAt,
		"finishedAt":          job.FinishedAt,
		"safeHead":            job.SafeHead,
		"totalAddresses":      job.TotalAddresses,
		"completedAddresses":  job.CompletedAddresses,
		"cachedAddresses":     job.CachedAddresses,
		"fetched":             job.Fetched,
		"actionCounts":        job.ActionCounts,
		"successfulNeighbors": job.SuccessfulNeighbors,
		"failedNeighbors":     job.FailedNeighbors,
		"error":               job.Error,
		"errorCode":           job.ErrorCode,
		"retryable":           job.Retryable,
	}
}

func writeError(c echo.Context, status int, code, message string, retryable bool) error {
	return c.JSON(status, map[string]any{"error": map[string]any{"code": code, "message": message, "retryable": retryable}})
}
