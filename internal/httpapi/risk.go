package httpapi

import (
	"context"
	"net/http"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/tracer"
	"github.com/labstack/echo/v4"
)

type RiskHandler struct{ provider RiskProvider }

type RiskProvider interface {
	Enqueue(context.Context, tracer.Request) (store.TraceJob, error)
}

func NewRiskHandler(provider RiskProvider) *RiskHandler { return &RiskHandler{provider: provider} }

func (h *RiskHandler) Get(c echo.Context) error {
	depth, err := queryInt(c, "depth")
	if err != nil {
		return writeError(c, 400, "invalid_request", "invalid depth", false)
	}
	topN, err := queryInt(c, "topN")
	if err != nil {
		return writeError(c, 400, "invalid_request", "invalid topN", false)
	}
	query := tracer.Query{Chain: c.QueryParam("chain"), Address: c.QueryParam("address"), Direction: c.QueryParam("direction"), Asset: c.QueryParam("asset"), Depth: depth, TopN: topN}
	if err := tracer.ValidateQuery(query); err != nil {
		return writeError(c, 400, "invalid_request", err.Error(), false)
	}
	job, err := h.provider.Enqueue(c.Request().Context(), tracer.Request{Query: query})
	if err != nil {
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	return c.JSON(http.StatusAccepted, map[string]any{"traceJobId": job.ID.Hex(), "status": job.Status})
}
