package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

type nativeBalanceStub struct {
	block  int64
	amount string
}

func (s nativeBalanceStub) BlockNumber(context.Context) (int64, error) { return s.block, nil }
func (s nativeBalanceStub) Balance(context.Context, string, int64) (string, error) {
	return s.amount, nil
}

func TestBalanceHandlerReturnsBlockPinnedNativeBalance(t *testing.T) {
	address := "0x0000000000000000000000000000000000000001"
	e := echo.New()
	h := NewBalanceHandler(map[string]NativeBalanceProvider{"ethereum": nativeBalanceStub{block: 123, amount: "1250000000000000000"}}, func() time.Time {
		return time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	})
	e.GET("/api/v1/addresses/:address/balance", h.Get)
	res := httptest.NewRecorder()
	e.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/addresses/"+address+"/balance?chain=ethereum", nil))
	if res.Code != http.StatusOK || !containsAll(res.Body.String(), `"amount":"1250000000000000000"`, `"blockNumber":123`, `"decimals":18`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
