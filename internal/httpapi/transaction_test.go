package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/transactionanalysis"
	"github.com/labstack/echo/v4"
)

type transactionAnalyzerStub struct {
	chain, hash string
	analysis    store.TransactionAnalysis
	err         error
}

func (s *transactionAnalyzerStub) Analyze(_ context.Context, chain, hash string) (store.TransactionAnalysis, error) {
	s.chain, s.hash = chain, hash
	return s.analysis, s.err
}

func TestTransactionHandlerReturnsAnalysis(t *testing.T) {
	hash := "0x" + strings.Repeat("a", 64)
	stub := &transactionAnalyzerStub{analysis: store.TransactionAnalysis{Chain: "ethereum", TxHash: hash, Swaps: []store.SwapEvent{}}}
	e := echo.New()
	e.GET("/api/v1/transactions/:txHash", NewTransactionHandler(stub).Get)
	res := httptest.NewRecorder()
	e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+hash+"?chain=ethereum", nil))
	if res.Code != http.StatusOK || stub.chain != "ethereum" || stub.hash != hash || !strings.Contains(res.Body.String(), `"swaps":[]`) {
		t.Fatalf("status=%d chain=%s hash=%s body=%s", res.Code, stub.chain, stub.hash, res.Body.String())
	}
}

func TestTransactionHandlerMapsErrors(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{transactionanalysis.ErrInvalidHash, 400, "invalid_request"},
		{transactionanalysis.ErrUnsupportedChain, 400, "invalid_request"},
		{etherscan.ErrNotFound, 404, "transaction_not_found"},
		{etherscan.ErrPending, 409, "receipt_pending"},
		{etherscan.ErrRateLimited, 429, "upstream_rate_limited"},
		{etherscan.ErrTransient, 503, "upstream_unavailable"},
		{errors.New("mongo unavailable"), 500, "internal_error"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			e := echo.New()
			e.GET("/api/v1/transactions/:txHash", NewTransactionHandler(&transactionAnalyzerStub{err: test.err}).Get)
			res := httptest.NewRecorder()
			e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/transactions/0x1", nil))
			if res.Code != test.status || !strings.Contains(res.Body.String(), test.code) {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
}
