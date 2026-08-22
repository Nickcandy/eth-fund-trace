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

func (s *labelStub) UpsertLabel(_ context.Context, v store.Label) error { s.value = v; return nil }
func (s *labelStub) ListLabels(context.Context, string, string) ([]store.Label, error) {
	return s.labels, nil
}
func TestLabelHandlerValidatesAndPersistsEvidence(t *testing.T) {
	s := &labelStub{}
	h := NewLabelHandler(s)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/labels", strings.NewReader(`{"address":"0x0000000000000000000000000000000000000001","type":"hacker","source":"manual","confidence":0.8,"evidence":["case-1"]}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	if err := h.Create(e.NewContext(req, res)); err != nil {
		t.Fatal(err)
	}
	if res.Code != 201 || s.value.Confidence != 0.8 || len(s.value.Evidence) != 1 {
		t.Fatalf("status=%d value=%+v", res.Code, s.value)
	}
}
func TestLabelHandlerRejectsConfidence(t *testing.T) {
	h := NewLabelHandler(&labelStub{})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"address":"0x0000000000000000000000000000000000000001","type":"hacker","source":"manual","confidence":2}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	_ = h.Create(e.NewContext(req, res))
	if res.Code != 400 {
		t.Fatalf("status=%d", res.Code)
	}
}

type traceStub struct{ job store.TraceJob }

func (s traceStub) Enqueue(context.Context, tracer.Request) (store.TraceJob, error) {
	return s.job, nil
}
func (s traceStub) Job(context.Context, string) (store.TraceJob, error) { return s.job, nil }


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
