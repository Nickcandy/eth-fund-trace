package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context, *readpref.ReadPref) error { return s.err }

func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "healthy", want: 200},
		{name: "unavailable", err: errors.New("mongo unavailable"), want: 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest("GET", "/healthz", nil)
			res := httptest.NewRecorder()
			ctx := e.NewContext(req, res)
			if err := NewHealthHandler(stubPinger{err: tt.err}).Handle(ctx); err != nil {
				t.Fatal(err)
			}
			if res.Code != tt.want {
				t.Fatalf("status = %d, want %d", res.Code, tt.want)
			}
		})
	}
}
