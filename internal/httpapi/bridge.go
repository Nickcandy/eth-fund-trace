package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Nickcandy/eth-fund-trace/internal/bridge"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
)

type BridgeProvider interface {
	Create(context.Context, bridge.CreateRequest) (store.CrossChainLink, error)
	List(context.Context, string, string, int64) ([]store.CrossChainLink, error)
}

type BridgeHandler struct{ provider BridgeProvider }

func NewBridgeHandler(provider BridgeProvider) *BridgeHandler {
	return &BridgeHandler{provider: provider}
}

func (h *BridgeHandler) Create(c echo.Context) error {
	var request bridge.CreateRequest
	if err := c.Bind(&request); err != nil {
		return writeError(c, 400, "invalid_request", "invalid JSON request", false)
	}
	link, err := h.provider.Create(c.Request().Context(), request)
	if errors.Is(err, bridge.ErrInvalidRequest) {
		return writeError(c, 400, "invalid_request", err.Error(), false)
	}
	if err != nil {
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	return c.JSON(http.StatusCreated, link)
}

func (h *BridgeHandler) List(c echo.Context) error {
	limit, err := parseOptionalInt64(c.QueryParam("limit"))
	if err != nil {
		return writeError(c, 400, "invalid_request", "invalid limit", false)
	}
	links, err := h.provider.List(c.Request().Context(), c.QueryParam("chain"), c.QueryParam("address"), limit)
	if errors.Is(err, bridge.ErrInvalidRequest) {
		return writeError(c, 400, "invalid_request", err.Error(), false)
	}
	if err != nil {
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	return c.JSON(http.StatusOK, map[string]any{"items": links})
}
