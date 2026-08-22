package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Nickcandy/eth-fund-trace/internal/tracer"
	"github.com/labstack/echo/v4"
)

type RiskProvider interface {
	Trace(context.Context, tracer.Query) (tracer.Result, error)
}
type RiskHandler struct{ provider RiskProvider }

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
	result, err := h.provider.Trace(c.Request().Context(), query)
	switch {
	case errors.Is(err, tracer.ErrAddressNotSynced):
		return writeError(c, http.StatusConflict, "address_not_synced", err.Error(), false)
	case err != nil:
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	return c.JSON(http.StatusOK, result.Risk)
}
