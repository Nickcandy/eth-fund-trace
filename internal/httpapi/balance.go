package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/chains"
	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/labstack/echo/v4"
)

type NativeBalanceProvider interface {
	BlockNumber(context.Context) (int64, error)
	Balance(context.Context, string, int64) (string, error)
}

type BalanceHandler struct {
	providers map[string]NativeBalanceProvider
	clock     func() time.Time
}

func NewBalanceHandler(providers map[string]NativeBalanceProvider, clock func() time.Time) *BalanceHandler {
	return &BalanceHandler{providers: providers, clock: clock}
}

func (h *BalanceHandler) Get(c echo.Context) error {
	chain, chainErr := chains.Resolve(c.QueryParam("chain"))
	address, addressErr := ethaddr.Normalize(c.Param("address"))
	if chainErr != nil || addressErr != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "invalid chain or address", false)
	}
	provider := h.providers[chain.Name]
	if provider == nil {
		return writeError(c, http.StatusServiceUnavailable, "balance_unavailable", "chain RPC is not configured", true)
	}
	blockNumber, err := provider.BlockNumber(c.Request().Context())
	if err != nil {
		return writeError(c, http.StatusServiceUnavailable, "balance_unavailable", "current balance is unavailable", true)
	}
	amount, err := provider.Balance(c.Request().Context(), address, blockNumber)
	if err != nil {
		return writeError(c, http.StatusServiceUnavailable, "balance_unavailable", "current balance is unavailable", true)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"chain": chain.Name, "chainId": chain.ID, "address": address, "asset": "ETH", "amount": amount,
		"decimals": 18, "blockNumber": blockNumber, "fetchedAt": h.clock().UTC(),
	})
}
