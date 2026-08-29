package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/syncer"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type stubSyncManager struct {
	request syncer.Request
	chain   string
	address string
	job     store.SyncJob
	err     error
}

func (s *stubSyncManager) Enqueue(_ context.Context, request syncer.Request) (store.SyncJob, error) {
	s.request = request
	return s.job, s.err
}

func (s *stubSyncManager) Job(context.Context, string) (store.SyncJob, error) { return s.job, s.err }

func (s *stubSyncManager) LatestJob(_ context.Context, chain, address string) (store.SyncJob, error) {
	s.chain = chain
	s.address = address
	return s.job, s.err
}

func TestSyncHandlerAcceptsJob(t *testing.T) {
	id := primitive.NewObjectID()
	manager := &stubSyncManager{job: store.SyncJob{ID: id, Status: "queued", CreatedAt: time.Now()}}
	handler := NewSyncHandler(manager)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", strings.NewReader(`{"address":"0x0000000000000000000000000000000000000001","startBlock":21525891,"endBlock":25860787}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	ctx := e.NewContext(req, res)

	if err := handler.Enqueue(ctx); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusAccepted || manager.request.Chain != "ethereum" || manager.request.StartBlock != 21525891 || manager.request.EndBlock != 25860787 || manager.request.NeighborLimit != 10 || !strings.Contains(res.Body.String(), id.Hex()) {
		t.Fatalf("status=%d request=%+v body=%s", res.Code, manager.request, res.Body.String())
	}
}

func TestSyncHandlerRejectsInvalidRequest(t *testing.T) {
	handler := NewSyncHandler(&stubSyncManager{err: syncer.ErrInvalidRequest})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", strings.NewReader(`{"address":"bad"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	ctx := e.NewContext(req, res)

	if err := handler.Enqueue(ctx); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "invalid_request") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestSyncHandlerReturnsLatestJobForAddress(t *testing.T) {
	id := primitive.NewObjectID()
	address := "0x0000000000000000000000000000000000000001"
	manager := &stubSyncManager{job: store.SyncJob{ID: id, Chain: "ethereum", Address: address, Status: "partial", CreatedAt: time.Now(), MaxRecordsPerAction: 100_000, TruncatedActions: []string{"txlist"}}}
	handler := NewSyncHandler(manager)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-jobs/latest?chain=ethereum&address="+address, nil)
	res := httptest.NewRecorder()
	ctx := e.NewContext(req, res)

	if err := handler.LatestJob(ctx); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || manager.chain != "ethereum" || manager.address != address || !containsAll(res.Body.String(), id.Hex(), `"maxRecordsPerAction":100000`, `"truncatedActions":["txlist"]`) {
		t.Fatalf("status=%d chain=%s address=%s body=%s", res.Code, manager.chain, manager.address, res.Body.String())
	}
}

func TestSyncHandlerLatestJobReturnsNotFound(t *testing.T) {
	handler := NewSyncHandler(&stubSyncManager{err: mongo.ErrNoDocuments})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-jobs/latest?chain=ethereum&address=0x0000000000000000000000000000000000000001", nil)
	res := httptest.NewRecorder()
	ctx := e.NewContext(req, res)

	if err := handler.LatestJob(ctx); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusNotFound || !strings.Contains(res.Body.String(), "job_not_found") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
