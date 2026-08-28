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

type labelStub struct {
	labels []store.Label
	value  store.Label
}

func (s *labelStub) UpsertLabel(_ context.Context, v store.Label) (store.Label, error) {
	v.ID = primitive.NewObjectID()
	s.value = v
	return v, nil
}
func (s *labelStub) ListLabels(context.Context, string, string) ([]store.Label, error) {
	return s.labels, nil
}
func TestLabelHandlerValidatesAndPersistsEvidence(t *testing.T) {
	s := &labelStub{}
	h := NewLabelHandler(s)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/labels", strings.NewReader(`{"chain":"ethereum","address":"0x0000000000000000000000000000000000000001","type":" hacker ","source":"manual","confidence":0.8,"note":" reviewed ","evidence":[" case-1 ","","case-2"]}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	if err := h.Create(e.NewContext(req, res)); err != nil {
		t.Fatal(err)
	}
	if res.Code != 201 || s.value.ID.IsZero() || s.value.Confidence != 0.8 || s.value.Type != "hacker" || s.value.Note != "reviewed" || len(s.value.Evidence) != 2 || s.value.Evidence[0] != "case-1" {
		t.Fatalf("status=%d value=%+v", res.Code, s.value)
	}
}

func TestLabelHandlerRequiresChainAndNonBlankType(t *testing.T) {
	tests := map[string]string{
		"missing chain": `{"address":"0x0000000000000000000000000000000000000001","type":"hacker","source":"manual"}`,
		"blank type":    `{"chain":"ethereum","address":"0x0000000000000000000000000000000000000001","type":"  ","source":"manual"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			h := NewLabelHandler(&labelStub{})
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/labels", strings.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			res := httptest.NewRecorder()
			_ = h.Create(e.NewContext(req, res))
			if res.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
}

func TestLabelHandlerListReturnsEmptyArray(t *testing.T) {
	h := NewLabelHandler(&labelStub{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/labels?chain=ethereum&address=0x0000000000000000000000000000000000000001", nil)
	res := httptest.NewRecorder()
	if err := h.List(e.NewContext(req, res)); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
func TestLabelHandlerRejectsConfidence(t *testing.T) {
	provider := &labelStub{}
	h := NewLabelHandler(provider)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"chain":"ethereum","address":"0x0000000000000000000000000000000000000001","type":"hacker","source":"manual","confidence":2}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	_ = h.Create(e.NewContext(req, res))
	if res.Code != http.StatusBadRequest || !provider.value.ID.IsZero() {
		t.Fatalf("status=%d persisted=%+v", res.Code, provider.value)
	}
}

type traceStub struct{ job store.TraceJob }

func (s traceStub) Enqueue(context.Context, tracer.Request) (store.TraceJob, error) {
	return s.job, nil
}
func (s traceStub) Job(context.Context, string) (store.TraceJob, error) { return s.job, nil }
func (s traceStub) LatestJob(context.Context, tracer.Query) (store.TraceJob, error) {
	return s.job, nil
}
func (s traceStub) Stop(context.Context, string) (store.TraceJob, error) { return s.job, nil }
func (s traceStub) EnqueueExtension(context.Context, string, tracer.ExtensionRequest) (store.TraceJob, error) {
	return s.job, nil
}
func (s traceStub) LatestExtension(context.Context, string) (store.TraceJob, error) {
	return s.job, nil
}

func TestTraceHandlerReturnsAccepted(t *testing.T) {
	id := primitive.NewObjectID()
	h := NewTraceHandler(traceStub{job: store.TraceJob{ID: id, Status: "queued"}})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trace?address=0x0000000000000000000000000000000000000001", nil)
	res := httptest.NewRecorder()
	_ = h.Enqueue(e.NewContext(req, res))
	if res.Code != 202 || !strings.Contains(res.Body.String(), id.Hex()) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestTraceHandlerReturnsLatestMatchingJob(t *testing.T) {
	id := primitive.NewObjectID()
	h := NewTraceHandler(traceStub{job: store.TraceJob{ID: id, SeedAddress: "0x0000000000000000000000000000000000000001", Status: "running"}})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trace-jobs/latest?chain=ethereum&address=0x0000000000000000000000000000000000000001&direction=both&depth=3&asset=all", nil)
	res := httptest.NewRecorder()
	if err := h.LatestJob(e.NewContext(req, res)); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), id.Hex()) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestTraceHandlerCreatesOneHopExtension(t *testing.T) {
	id := primitive.NewObjectID()
	h := NewTraceHandler(traceStub{job: store.TraceJob{ID: id, Status: "queued"}})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trace-jobs/root/extensions", strings.NewReader(`{"address":"0x0000000000000000000000000000000000000002","direction":"out","depth":1}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	ctx := e.NewContext(req, res)
	ctx.SetPath("/api/v1/trace-jobs/:id/extensions")
	ctx.SetParamNames("id")
	ctx.SetParamValues("root")
	_ = h.EnqueueExtension(ctx)
	if res.Code != http.StatusAccepted || !strings.Contains(res.Body.String(), id.Hex()) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
