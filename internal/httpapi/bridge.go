package httpapi

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/bridge"
	"github.com/Nickcandy/eth-fund-trace/internal/chainrpc"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
)

type BridgeProvider interface {
	Create(context.Context, bridge.CreateRequest) (store.CrossChainLink, error)
	List(context.Context, string, string, int64) ([]store.CrossChainLink, error)
	Query(context.Context, store.BridgeLinkQuery) ([]store.CrossChainLink, error)
}

type BridgeAnalyzer interface {
	Analyze(context.Context, string, string) ([]store.CrossChainLink, error)
}
type BridgeScheduler interface{ Enqueue(string) error }
type BridgeHandler struct {
	provider  BridgeProvider
	analyzer  BridgeAnalyzer
	scheduler BridgeScheduler
}

func NewBridgeHandler(provider BridgeProvider) *BridgeHandler {
	return &BridgeHandler{provider: provider}
}
func (h *BridgeHandler) WithAutomation(analyzer BridgeAnalyzer, scheduler BridgeScheduler) *BridgeHandler {
	h.analyzer, h.scheduler = analyzer, scheduler
	return h
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
	if errors.Is(err, bridge.ErrEvidenceNotFound) {
		return writeError(c, http.StatusConflict, "bridge_evidence_not_found", err.Error(), false)
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
	links, err := h.provider.Query(c.Request().Context(), store.BridgeLinkQuery{Chain: c.QueryParam("chain"), Address: c.QueryParam("address"), Status: c.QueryParam("status"), Protocol: c.QueryParam("protocol"), Direction: c.QueryParam("direction"), Limit: limit})
	if errors.Is(err, bridge.ErrInvalidRequest) {
		return writeError(c, 400, "invalid_request", err.Error(), false)
	}
	if err != nil {
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	return c.JSON(http.StatusOK, map[string]any{"items": links})
}

func (h *BridgeHandler) Analyze(c echo.Context) error {
	if h.analyzer == nil {
		return writeError(c, http.StatusServiceUnavailable, "bridge_upstream_unavailable", "bridge RPC is not configured", true)
	}
	var request struct {
		Chain  string `json:"chain"`
		TxHash string `json:"txHash"`
	}
	if c.Bind(&request) != nil || !transactionHashPattern(request.TxHash) {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid bridge analysis request", false)
	}
	links, err := h.analyzer.Analyze(c.Request().Context(), request.Chain, request.TxHash)
	if err != nil {
		return bridgeHTTPError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"items": links})
}

func (h *BridgeHandler) Sync(c echo.Context) error {
	if h.scheduler == nil {
		return writeError(c, http.StatusServiceUnavailable, "bridge_upstream_unavailable", "bridge worker is not configured", true)
	}
	var request struct {
		LinkID string `json:"linkId"`
	}
	if c.Bind(&request) != nil || h.scheduler.Enqueue(request.LinkID) != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid bridge link ID", false)
	}
	return c.JSON(http.StatusAccepted, map[string]string{"status": "queued", "linkId": request.LinkID})
}

func transactionHashPattern(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}
func bridgeHTTPError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, bridge.ErrMalformedEvent):
		return writeError(c, 422, "bridge_malformed_event", err.Error(), false)
	case errors.Is(err, chainrpc.ErrRateLimited):
		return writeError(c, 429, "bridge_upstream_rate_limited", err.Error(), true)
	case errors.Is(err, chainrpc.ErrUnavailable), errors.Is(err, chainrpc.ErrPending):
		return writeError(c, 503, "bridge_upstream_unavailable", err.Error(), true)
	default:
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
}
