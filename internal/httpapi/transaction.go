package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/transactionanalysis"
	"github.com/labstack/echo/v4"
)

// TransactionAnalyzer provides confirmed transaction analysis.
type TransactionAnalyzer interface {
	Analyze(context.Context, string, string) (store.TransactionAnalysis, error)
}

// TransactionHandler serves transaction analysis requests.
type TransactionHandler struct{ analyzer TransactionAnalyzer }

// NewTransactionHandler creates a transaction analysis handler.
func NewTransactionHandler(analyzer TransactionAnalyzer) *TransactionHandler {
	return &TransactionHandler{analyzer: analyzer}
}

// Get returns one confirmed transaction analysis.
func (h *TransactionHandler) Get(c echo.Context) error {
	analysis, err := h.analyzer.Analyze(c.Request().Context(), c.QueryParam("chain"), c.Param("txHash"))
	if err == nil {
		return c.JSON(http.StatusOK, analysis)
	}
	switch {
	case errors.Is(err, transactionanalysis.ErrInvalidHash), errors.Is(err, transactionanalysis.ErrUnsupportedChain):
		return writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
	case errors.Is(err, etherscan.ErrNotFound):
		return writeError(c, http.StatusNotFound, "transaction_not_found", "transaction not found", false)
	case errors.Is(err, etherscan.ErrPending):
		return writeError(c, http.StatusConflict, "receipt_pending", "transaction receipt is not confirmed", true)
	case errors.Is(err, etherscan.ErrRateLimited):
		return writeError(c, http.StatusTooManyRequests, "upstream_rate_limited", "transaction data provider rate limited", true)
	case errors.Is(err, etherscan.ErrAPI), errors.Is(err, etherscan.ErrTransient), errors.Is(err, etherscan.ErrMalformedResponse), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return writeError(c, http.StatusServiceUnavailable, "upstream_unavailable", "transaction data provider unavailable", true)
	default:
		return writeError(c, http.StatusInternalServerError, "internal_error", "internal server error", true)
	}
}
