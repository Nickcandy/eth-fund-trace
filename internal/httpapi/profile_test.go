package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/profile"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
)

type stubProfiler struct {
	result store.AddressProfile
	err    error
}

func (s stubProfiler) Get(context.Context, string, string) (store.AddressProfile, error) {
	return s.result, s.err
}

func TestProfileHandlerReturnsProfile(t *testing.T) {
	handler := NewProfileHandler(stubProfiler{result: store.AddressProfile{RuleVersion: profile.RuleVersion, Score: 80}})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	res := httptest.NewRecorder()
	ctx := e.NewContext(req, res)
	ctx.SetPath("/api/v1/addresses/:address/profile")
	ctx.SetParamNames("address")
	ctx.SetParamValues("0x0000000000000000000000000000000000000001")
	if err := handler.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), profile.RuleVersion) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestProfileHandlerRequiresSyncedAddress(t *testing.T) {
	handler := NewProfileHandler(stubProfiler{err: profile.ErrAddressNotSynced})
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	res := httptest.NewRecorder()
	ctx := e.NewContext(req, res)
	ctx.SetParamNames("address")
	ctx.SetParamValues("0x0000000000000000000000000000000000000001")
	if err := handler.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "address_not_synced") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
