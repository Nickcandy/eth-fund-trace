package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/propagation"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type propagationProviderStub struct{ job store.PropagationJob }

func (s propagationProviderStub) Enqueue(context.Context, propagation.Request) (store.PropagationJob, error) {
	return s.job, nil
}
func (s propagationProviderStub) Job(context.Context, string) (store.PropagationJob, error) {
	return s.job, nil
}
func (s propagationProviderStub) Stop(context.Context, string) (store.PropagationJob, error) {
	return s.job, nil
}

func TestPropagationHandlerCreatesAsynchronousJob(t *testing.T) {
	id := primitive.NewObjectID()
	e := echo.New()
	handler := NewPropagationHandler(propagationProviderStub{job: store.PropagationJob{ID: id, Status: "queued"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/propagation-jobs", strings.NewReader(`{"chain":"ethereum","targetAddress":"0x0000000000000000000000000000000000000001","direction":"both","asset":"ETH"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	c := e.NewContext(req, res)
	if err := handler.Create(c); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusAccepted || !strings.Contains(res.Body.String(), id.Hex()) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
