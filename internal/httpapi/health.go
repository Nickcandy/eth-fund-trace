package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type Pinger interface {
	Ping(context.Context, *readpref.ReadPref) error
}

type HealthHandler struct {
	pinger Pinger
}

func NewHealthHandler(pinger Pinger) *HealthHandler {
	return &HealthHandler{pinger: pinger}
}

func (h *HealthHandler) Handle(c echo.Context) error {
	if err := h.pinger.Ping(c.Request().Context(), nil); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "mongo": "unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok", "mongo": "ok"})
}
