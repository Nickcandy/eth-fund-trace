package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/tracer"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
	h := NewAddressHandler(addressProviderStub{address: store.Address{Chain: "ethereum", ChainID: 1, Address: seed, SyncStatus: "synced"}, found: true, labels: []store.Label{{Type: "exchange"}}})
	e.GET("/api/v1/addresses/:address", h.Get)
	res := httptest.NewRecorder()
	e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/addresses/"+seed+"?chain=ethereum", nil))
	if res.Code != 200 || !containsAll(res.Body.String(), `"chainId":1`, `"exchange"`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

type riskProviderStub struct {
	job store.TraceJob
	err error
}

func (s riskProviderStub) Enqueue(context.Context, tracer.Request) (store.TraceJob, error) {
	return s.job, s.err
}

func TestRiskHandlerCreatesAsynchronousTraceJob(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	id := primitive.NewObjectID()
	e := echo.New()
	e.GET("/api/v1/risk", NewRiskHandler(riskProviderStub{job: store.TraceJob{ID: id, Status: "queued"}}).Get)
	res := httptest.NewRecorder()
	e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/risk?address="+seed, nil))
	if res.Code != http.StatusAccepted || !containsAll(res.Body.String(), id.Hex(), `"status":"queued"`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
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
