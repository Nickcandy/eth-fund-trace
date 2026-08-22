package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/labstack/echo/v4"
)

type LabelProvider interface {
	UpsertLabel(context.Context, store.Label) error
	ListLabels(context.Context, string, string) ([]store.Label, error)
}
type LabelHandler struct{ provider LabelProvider }

func NewLabelHandler(provider LabelProvider) *LabelHandler { return &LabelHandler{provider: provider} }

type labelRequest struct {
	Chain      string   `json:"chain"`
	Address    string   `json:"address"`
	Type       string   `json:"type"`
	RiskLevel  string   `json:"riskLevel"`
	Confidence *float64 `json:"confidence"`
	Source     string   `json:"source"`
	Note       string   `json:"note"`
	Evidence   []string `json:"evidence"`
}

func (h *LabelHandler) Create(c echo.Context) error {
	var body labelRequest
	if err := c.Bind(&body); err != nil {
		return writeError(c, 400, "invalid_request", "invalid JSON request", false)
	}
	chain := strings.ToLower(body.Chain)
	if chain == "" {
		chain = "ethereum"
	}
	address, err := ethaddr.Normalize(body.Address)
	if err != nil || chain != "ethereum" || body.Type == "" || (body.Source != "manual" && body.Source != "public-list") || !validRiskLevel(body.RiskLevel) {
		return writeError(c, 400, "invalid_request", "invalid label fields", false)
	}
	confidence := 1.0
	if body.Confidence != nil {
		confidence = *body.Confidence
	}
	if confidence < 0 || confidence > 1 {
		return writeError(c, 400, "invalid_request", "confidence must be between 0 and 1", false)
	}
	label := store.Label{Chain: chain, ChainID: 1, Address: address, Type: body.Type, RiskLevel: body.RiskLevel, Confidence: confidence, Source: body.Source, Note: body.Note, Evidence: body.Evidence, ObservedAt: time.Now().UTC()}
	if err := h.provider.UpsertLabel(c.Request().Context(), label); err != nil {
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	return c.JSON(http.StatusCreated, label)
}

func validRiskLevel(value string) bool {
	return value == "" || value == "low" || value == "medium" || value == "high"
}
func (h *LabelHandler) List(c echo.Context) error {
	chain := strings.ToLower(c.QueryParam("chain"))
	if chain == "" {
		chain = "ethereum"
	}
	address, err := ethaddr.Normalize(c.QueryParam("address"))
	if err != nil || chain != "ethereum" {
		return writeError(c, 400, "invalid_request", "invalid chain or address", false)
	}
	labels, err := h.provider.ListLabels(c.Request().Context(), chain, address)
	if err != nil {
		return writeError(c, 500, "internal_error", "internal server error", true)
	}
	return c.JSON(200, labels)
}

var _ = errors.New
