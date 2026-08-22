package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestRegisterWebServesAssetsAndSPAFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("console"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	if err := RegisterWeb(e, dir); err != nil {
		t.Fatal(err)
	}
	for route, expected := range map[string]string{"/app.js": "asset", "/trace/ethereum": "console"} {
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("%s: %d %q", route, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("api fallback status = %d", response.Code)
	}
}
