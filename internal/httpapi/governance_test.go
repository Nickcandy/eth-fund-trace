package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestGovernanceAddsRequestIDAndProtectsAPI(t *testing.T) {
	e := echo.New()
	UseGovernance(e, GovernanceConfig{APIKey: "secret", Timeout: time.Second, BodyLimit: "1K", RequestsPerSecond: 100, Burst: 10})
	e.GET("/healthz", func(c echo.Context) error { return c.NoContent(200) })
	e.GET("/api/v1/test", func(c echo.Context) error { return c.NoContent(200) })

	health := httptest.NewRecorder()
	e.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != 200 {
		t.Fatalf("health status=%d", health.Code)
	}

	unauthorized := httptest.NewRecorder()
	e.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if unauthorized.Code != http.StatusUnauthorized || unauthorized.Header().Get(echo.HeaderXRequestID) == "" {
		t.Fatalf("response=%d headers=%v", unauthorized.Code, unauthorized.Header())
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	request.Header.Set(echo.HeaderAuthorization, "Bearer secret")
	e.ServeHTTP(authorized, request)
	if authorized.Code != 200 {
		t.Fatalf("status=%d body=%s", authorized.Code, authorized.Body.String())
	}
}

func TestGovernanceRejectsOversizedBodyAndRateLimit(t *testing.T) {
	e := echo.New()
	UseGovernance(e, GovernanceConfig{DisableAuth: true, BodyLimit: "8B", Timeout: time.Second, RequestsPerSecond: 1, Burst: 1})
	e.POST("/api/v1/test", func(c echo.Context) error { return c.NoContent(200) })
	large := httptest.NewRecorder()
	e.ServeHTTP(large, httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader("123456789")))
	if large.Code != http.StatusRequestEntityTooLarge || !strings.Contains(large.Body.String(), `"code":"request_too_large"`) {
		t.Fatalf("large status=%d", large.Code)
	}

	e2 := echo.New()
	UseGovernance(e2, GovernanceConfig{DisableAuth: true, BodyLimit: "1K", Timeout: time.Second, RequestsPerSecond: 1, Burst: 1})
	e2.GET("/api/v1/test", func(c echo.Context) error { return c.NoContent(200) })
	e2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	limited := httptest.NewRecorder()
	e2.ServeHTTP(limited, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("limited status=%d", limited.Code)
	}
}

func TestGovernanceReturnsGatewayTimeout(t *testing.T) {
	e := echo.New()
	UseGovernance(e, GovernanceConfig{DisableAuth: true, Timeout: time.Millisecond, BodyLimit: "1K", RequestsPerSecond: 100, Burst: 10})
	e.GET("/api/v1/slow", func(c echo.Context) error { <-c.Request().Context().Done(); return c.Request().Context().Err() })
	response := httptest.NewRecorder()
	e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/slow", nil))
	if response.Code != http.StatusGatewayTimeout || !strings.Contains(response.Body.String(), `"code":"request_timeout"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGovernanceFailsClosedWithoutAuthenticationConfiguration(t *testing.T) {
	e := echo.New()
	UseGovernance(e, GovernanceConfig{Timeout: time.Second, BodyLimit: "1K", RequestsPerSecond: 100, Burst: 10})
	e.GET("/api/v1/test", func(c echo.Context) error { return c.NoContent(200) })
	response := httptest.NewRecorder()
	e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "authentication_not_configured") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
