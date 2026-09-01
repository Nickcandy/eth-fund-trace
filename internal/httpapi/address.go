package httpapi

import (
	"context"
	"net/http"

	"github.com/Nickcandy/eth-fund-trace/internal/chains"
	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
)

type AddressProvider interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	ListLabels(context.Context, string, string) ([]store.Label, error)
}

type addressIdentityWriter interface {
	SetAddressIdentity(context.Context, string, string, store.AddressIdentity) error
}

type KnownAddressInspector interface {
	KnownAddressIdentity(string, string) (store.AddressIdentity, bool)
}

type AddressHandler struct {
	provider  AddressProvider
	inspector KnownAddressInspector
}

func NewAddressHandler(provider AddressProvider) *AddressHandler {
	return &AddressHandler{provider: provider}
}

// WithKnownAddressInspector refreshes static protocol identities in old address records.
func (h *AddressHandler) WithKnownAddressInspector(inspector KnownAddressInspector) *AddressHandler {
	h.inspector = inspector
	return h
}

func (h *AddressHandler) Get(c echo.Context) error {
	chain, chainErr := chains.Resolve(c.QueryParam("chain"))
	address, addressErr := ethaddr.Normalize(c.Param("address"))
	if chainErr != nil || addressErr != nil {
		return writeError(c, 400, "invalid_request", "invalid chain or address", false)
	}
	metadata, found, err := h.provider.FindAddress(c.Request().Context(), chain.Name, address)
	if err != nil {
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	if !found {
		return writeError(c, http.StatusNotFound, "address_not_found", "address has not been discovered", false)
	}
	if identity, known := h.knownIdentity(chain.Name, address); known {
		metadata.AddressType = identity.AddressType
		metadata.IsContract = identity.AddressType == "contract"
		metadata.Protocol = identity.Protocol
		metadata.Roles = identity.Roles
		if writer, ok := h.provider.(addressIdentityWriter); ok {
			if err := writer.SetAddressIdentity(c.Request().Context(), chain.Name, address, identity); err != nil {
				return writeError(c, 500, "internal_error", "internal server error", true)
			}
		}
	}
	labels, err := h.provider.ListLabels(c.Request().Context(), chain.Name, address)
	if err != nil {
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	return c.JSON(http.StatusOK, map[string]any{"address": metadata, "labels": labels})
}

func (h *AddressHandler) knownIdentity(chain, address string) (store.AddressIdentity, bool) {
	if h.inspector == nil {
		return store.AddressIdentity{}, false
	}
	return h.inspector.KnownAddressIdentity(chain, address)
}
