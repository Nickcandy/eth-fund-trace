package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/tracer"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/mongo"
)

type TraceProvider interface {
	Enqueue(context.Context, tracer.Request) (store.TraceJob, error)
	Job(context.Context, string) (store.TraceJob, error)
	LatestJob(context.Context, tracer.Query) (store.TraceJob, error)
	Stop(context.Context, string) (store.TraceJob, error)
}
type TraceHandler struct{ manager TraceProvider }

func NewTraceHandler(manager TraceProvider) *TraceHandler { return &TraceHandler{manager: manager} }
func (h *TraceHandler) Enqueue(c echo.Context) error {
	depth, err := queryInt(c, "depth")
	if err != nil {
		return writeError(c, 400, "invalid_request", "invalid depth", false)
	}
	query := tracer.Query{Chain: c.QueryParam("chain"), Address: c.QueryParam("address"), Direction: c.QueryParam("direction"), Depth: depth, Asset: c.QueryParam("asset")}
	if err := tracer.ValidateQuery(query); err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
	}
	job, err := h.manager.Enqueue(c.Request().Context(), tracer.Request{Query: query})
	if err != nil {
		return writeError(c, 400, "invalid_request", err.Error(), false)
	}
	return c.JSON(http.StatusAccepted, map[string]any{"traceJobId": job.ID.Hex(), "status": job.Status})
}
func (h *TraceHandler) Job(c echo.Context) error {
	job, err := h.manager.Job(c.Request().Context(), c.Param("id"))
	if errors.Is(err, mongo.ErrNoDocuments) {
		return writeError(c, 404, "trace_job_not_found", "trace job not found", false)
	}
	if err != nil {
		return writeError(c, 400, "invalid_request", "invalid trace job id", false)
	}
	return c.JSON(200, job)
}
func (h *TraceHandler) Stop(c echo.Context) error {
	job, err := h.manager.Stop(c.Request().Context(), c.Param("id"))
	if errors.Is(err, mongo.ErrNoDocuments) {
		return writeError(c, http.StatusNotFound, "trace_job_not_found", "trace job not found", false)
	}
	if err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
	}
	return c.JSON(http.StatusOK, job)
}
func (h *TraceHandler) LatestJob(c echo.Context) error {
	query, err := traceQuery(c)
	if err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
	}
	job, err := h.manager.LatestJob(c.Request().Context(), query)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return writeError(c, http.StatusNotFound, "trace_job_not_found", "trace job not found", false)
	}
	if err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
	}
	return c.JSON(http.StatusOK, job)
}

func traceQuery(c echo.Context) (tracer.Query, error) {
	depth, err := queryInt(c, "depth")
	if err != nil {
		return tracer.Query{}, errors.New("invalid depth")
	}
	query := tracer.Query{Chain: c.QueryParam("chain"), Address: c.QueryParam("address"), Direction: c.QueryParam("direction"), Depth: depth, Asset: c.QueryParam("asset")}
	if err := tracer.ValidateQuery(query); err != nil {
		return tracer.Query{}, err
	}
	return query, nil
}
func queryInt(c echo.Context, name string) (int, error) {
	value := c.QueryParam(name)
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}
