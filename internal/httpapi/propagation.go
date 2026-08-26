package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Nickcandy/eth-fund-trace/internal/propagation"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/mongo"
)

// PropagationProvider manages persistent bounded propagation jobs.
type PropagationProvider interface {
	Enqueue(context.Context, propagation.Request) (store.PropagationJob, error)
	Job(context.Context, string) (store.PropagationJob, error)
	Stop(context.Context, string) (store.PropagationJob, error)
}

// PropagationHandler exposes propagation job resources.
type PropagationHandler struct{ manager PropagationProvider }

// NewPropagationHandler creates a propagation job handler.
func NewPropagationHandler(manager PropagationProvider) *PropagationHandler {
	return &PropagationHandler{manager: manager}
}

// Create accepts a bounded asynchronous propagation job.
func (h *PropagationHandler) Create(c echo.Context) error {
	var request propagation.Request
	if err := c.Bind(&request); err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid propagation request", false)
	}
	job, err := h.manager.Enqueue(c.Request().Context(), request)
	if errors.Is(err, propagation.ErrInvalidRequest) {
		return writeError(c, http.StatusBadRequest, "invalid_propagation_target", err.Error(), false)
	}
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "propagation_job_failed", err.Error(), true)
	}
	return c.JSON(http.StatusAccepted, job)
}

// Job returns one propagation job including its result when complete.
func (h *PropagationHandler) Job(c echo.Context) error {
	job, err := h.manager.Job(c.Request().Context(), c.Param("id"))
	if errors.Is(err, mongo.ErrNoDocuments) {
		return writeError(c, http.StatusNotFound, "propagation_job_not_found", "propagation job not found", false)
	}
	if err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid propagation job id", false)
	}
	return c.JSON(http.StatusOK, job)
}

// Stop cancels a runnable propagation job.
func (h *PropagationHandler) Stop(c echo.Context) error {
	job, err := h.manager.Stop(c.Request().Context(), c.Param("id"))
	if errors.Is(err, mongo.ErrNoDocuments) {
		return writeError(c, http.StatusNotFound, "propagation_job_not_found", "propagation job not found or already complete", false)
	}
	if err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
	}
	return c.JSON(http.StatusOK, job)
}
