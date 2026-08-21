package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/fundgraph"
	"github.com/labstack/echo/v4"
)

type stubEdgeProvider struct {
	query  fundgraph.EdgeQuery
	result fundgraph.EdgePage
	err    error
}

func (s *stubEdgeProvider) Edges(_ context.Context, query fundgraph.EdgeQuery) (fundgraph.EdgePage, error) {
	s.query = query
	return s.result, s.err
}

func TestEdgeHandlerMapsQueryAndReturnsPage(t *testing.T) {
	provider := &stubEdgeProvider{result: fundgraph.EdgePage{Items: []fundgraph.Edge{{TxHash: "0xabc"}}, DataThroughBlock: 100, DataStatus: "synced"}}
	handler := NewEdgeHandler(provider)
	e := echo.New()
	url := "/api/v1/edges?address=0x0000000000000000000000000000000000000001&address=0x0000000000000000000000000000000000000002&direction=out&asset=ETH&fromBlock=10&toBlock=20&limit=25&cursor=opaque"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	res := httptest.NewRecorder()
	if err := handler.Get(e.NewContext(req, res)); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || len(provider.query.Addresses) != 2 || provider.query.Direction != "out" || provider.query.Asset != "ETH" || provider.query.FromBlock != 10 || provider.query.ToBlock != 20 || provider.query.Limit != 25 || provider.query.Cursor != "opaque" {
		t.Fatalf("status=%d query=%+v body=%s", res.Code, provider.query, res.Body.String())
	}
}

func TestEdgeHandlerRejectsBadNumbersAndMapsDomainErrors(t *testing.T) {
	e := echo.New()
	handler := NewEdgeHandler(&stubEdgeProvider{})
	res := httptest.NewRecorder()
	if err := handler.Get(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/edges?address=x&limit=bad", nil), res)); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "invalid_request") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{err: fundgraph.ErrInvalidQuery, status: http.StatusBadRequest, code: "invalid_request"},
		{err: fundgraph.ErrAddressNotSynced, status: http.StatusConflict, code: "address_not_synced"},
	} {
		provider := &stubEdgeProvider{err: test.err}
		res = httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/edges?address=0x0000000000000000000000000000000000000001", nil)
		if err := NewEdgeHandler(provider).Get(e.NewContext(request, res)); err != nil {
			t.Fatal(err)
		}
		if res.Code != test.status || !strings.Contains(res.Body.String(), test.code) {
			t.Fatalf("error=%v status=%d body=%s", test.err, res.Code, res.Body.String())
		}
	}

	provider := &stubEdgeProvider{err: errors.New("database unavailable")}
	res = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/edges?address=0x0000000000000000000000000000000000000001", nil)
	if err := NewEdgeHandler(provider).Get(e.NewContext(request, res)); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
