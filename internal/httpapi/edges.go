package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/fundgraph"
	"github.com/labstack/echo/v4"
)

type EdgeProvider interface {
	Edges(context.Context, fundgraph.EdgeQuery) (fundgraph.EdgePage, error)
}

type EdgeHandler struct {
	graph EdgeProvider
}

func NewEdgeHandler(graph EdgeProvider) *EdgeHandler {
	return &EdgeHandler{graph: graph}
}

func (h *EdgeHandler) Get(c echo.Context) error {
	fromBlock, err := parseOptionalInt64(c.QueryParam("fromBlock"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid fromBlock", false)
	}
	toBlock, err := parseOptionalInt64(c.QueryParam("toBlock"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid toBlock", false)
	}
	limit, err := parseOptionalInt(c.QueryParam("limit"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid limit", false)
	}
	addresses := c.QueryParams()["address"]
	if len(addresses) == 1 && strings.Contains(addresses[0], ",") {
		addresses = strings.Split(addresses[0], ",")
	}
	page, err := h.graph.Edges(c.Request().Context(), fundgraph.EdgeQuery{
		Chain: c.QueryParam("chain"), Addresses: addresses, Direction: c.QueryParam("direction"), Asset: c.QueryParam("asset"),
		FromBlock: fromBlock, ToBlock: toBlock, Limit: limit, Cursor: c.QueryParam("cursor"),
	})
	switch {
	case errors.Is(err, fundgraph.ErrInvalidQuery):
		return writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
	case errors.Is(err, fundgraph.ErrAddressNotSynced):
		return writeError(c, http.StatusConflict, "address_not_synced", err.Error(), false)
	case err != nil:
		return writeError(c, http.StatusInternalServerError, "internal_error", "internal server error", true)
	default:
		return c.JSON(http.StatusOK, page)
	}
}

func parseOptionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseOptionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	return int(parsed), err
}
