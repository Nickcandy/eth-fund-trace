package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/profile"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
)

type ProfileProvider interface {
	Get(context.Context, string, string) (store.AddressProfile, error)
}

type ProfileHandler struct {
	profiler ProfileProvider
}

func NewProfileHandler(profiler ProfileProvider) *ProfileHandler {
	return &ProfileHandler{profiler: profiler}
}

func (h *ProfileHandler) Get(c echo.Context) error {
	chain := strings.ToLower(c.QueryParam("chain"))
	if chain == "" {
		chain = "ethereum"
	}
	address, err := ethaddr.Normalize(c.Param("address"))
	if err != nil || chain != "ethereum" {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid chain or address", false)
	}
	result, err := h.profiler.Get(c.Request().Context(), chain, address)
	if errors.Is(err, profile.ErrAddressNotSynced) {
		return writeError(c, http.StatusConflict, "address_not_synced", err.Error(), false)
	}
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "internal_error", "internal server error", true)
	}
	return c.JSON(http.StatusOK, result)
}
