package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/risk"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/tracer"
	"github.com/labstack/echo/v4"
)

type addressProviderStub struct {
	address store.Address
	found   bool
	labels  []store.Label
}

func (s addressProviderStub) FindAddress(context.Context, string, string) (store.Address, bool, error) {
	return s.address, s.found, nil
}
func (s addressProviderStub) ListLabels(context.Context, string, string) ([]store.Label, error) {
	return s.labels, nil
}

func TestAddressHandlerReturnsMetadataAndLabels(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	e := echo.New()
	h := NewAddressHandler(addressProviderStub{address: store.Address{Chain: "base", ChainID: 8453, Address: seed, SyncStatus: "synced"}, found: true, labels: []store.Label{{Type: "exchange"}}})
	e.GET("/api/v1/addresses/:address", h.Get)
	res := httptest.NewRecorder()
	e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/addresses/"+seed+"?chain=base", nil))
	if res.Code != 200 || !containsAll(res.Body.String(), `"chainId":8453`, `"exchange"`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

type riskProviderStub struct {
	result tracer.Result
	err    error
}

func (s riskProviderStub) Trace(context.Context, tracer.Query) (tracer.Result, error) {
	return s.result, s.err
}

func TestRiskHandlerReturnsVersionedResultAndUnsyncedConflict(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	e := echo.New()
	e.GET("/api/v1/risk", NewRiskHandler(riskProviderStub{result: tracer.Result{Risk: risk.Result{Score: 70, Level: "known_high", RuleVersion: "risk-v1"}}}).Get)
	res := httptest.NewRecorder()
	e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/risk?address="+seed, nil))
	if res.Code != 200 || !containsAll(res.Body.String(), `"score":70`, `"risk-v1"`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	e2 := echo.New()
	e2.GET("/api/v1/risk", NewRiskHandler(riskProviderStub{err: tracer.AddressNotSyncedError{Address: seed}}).Get)
	conflict := httptest.NewRecorder()
	e2.ServeHTTP(conflict, httptest.NewRequest(http.MethodGet, "/api/v1/risk?address="+seed, nil))
	if conflict.Code != 409 || !errors.Is(tracer.AddressNotSyncedError{Address: seed}, tracer.ErrAddressNotSynced) {
		t.Fatalf("status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func containsAll(value string, expected ...string) bool {
	for _, item := range expected {
		if !strings.Contains(value, item) {
			return false
		}
	}
	return true
}
