package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/bridge"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
)

type bridgeProviderStub struct{ created bridge.CreateRequest }

func (s *bridgeProviderStub) Create(_ context.Context, request bridge.CreateRequest) (store.CrossChainLink, error) {
	s.created = request
	return store.CrossChainLink{SourceChain: "ethereum", TargetChain: "base", Status: "confirmed"}, nil
}
func (s *bridgeProviderStub) List(context.Context, string, string, int64) ([]store.CrossChainLink, error) {
	return []store.CrossChainLink{{SourceChain: "ethereum", TargetChain: "base"}}, nil
}

func TestBridgeHandlerCreatesAndListsConfirmedLinks(t *testing.T) {
	provider := &bridgeProviderStub{}
	e := echo.New()
	handler := NewBridgeHandler(provider)
	e.POST("/api/v1/bridge-links", handler.Create)
	e.GET("/api/v1/bridge-links", handler.List)
	body := `{"sourceChain":"ethereum","targetChain":"base","evidence":["provider:1"]}`
	created := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bridge-links", strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(created, request)
	if created.Code != http.StatusCreated || provider.created.TargetChain != "base" {
		t.Fatalf("status=%d body=%s request=%+v", created.Code, created.Body.String(), provider.created)
	}
	listed := httptest.NewRecorder()
	e.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/v1/bridge-links?chain=ethereum&address=0x0000000000000000000000000000000000000001", nil))
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), `"targetChain":"base"`) {
		t.Fatalf("status=%d body=%s", listed.Code, listed.Body.String())
	}
}
