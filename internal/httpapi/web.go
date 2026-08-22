package httpapi

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
)

// RegisterWeb serves a Vite distribution directory and falls back to index.html
// only for browser routes. API and health paths keep their normal 404 behavior.
func RegisterWeb(e *echo.Echo, root string) error {
	dist := os.DirFS(root)
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return err
	}
	files := http.FileServer(http.FS(dist))
	e.GET("/*", echo.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "." {
			clean = "index.html"
		}
		if _, err := fs.Stat(dist, clean); err == nil {
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})))
	return nil
}
