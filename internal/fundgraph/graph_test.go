package fundgraph

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

type graphRepository struct {
	addresses map[string]store.Address
	pages     [][]store.Transfer
	queries   []store.TransferQuery
}

func (r *graphRepository) FindAddress(_ context.Context, _, address string) (store.Address, bool, error) {
	value, ok := r.addresses[address]
	return value, ok, nil
}

func (r *graphRepository) QueryTransfers(_ context.Context, query store.TransferQuery) ([]store.Transfer, error) {
	r.queries = append(r.queries, query)
	page := r.pages[0]
	r.pages = r.pages[1:]
	return page, nil
}

func TestGraphReturnsStableCursorPages(t *testing.T) {
	address := "0x0000000000000000000000000000000000000001"
	repository := &graphRepository{
		addresses: map[string]store.Address{address: {SyncStatus: "synced", NormalSyncedTo: 100, InternalSyncedTo: 100, TokenSyncedTo: 100}},
		pages: [][]store.Transfer{
			{
				{BlockNumber: 30, TxHash: "0xc", Source: "txlist", Asset: "ETH", From: address, To: "0x2", Amount: "3"},
				{BlockNumber: 20, TxHash: "0xb", Source: "txlist", Asset: "ETH", From: "0x2", To: address, Amount: "2"},
				{BlockNumber: 10, TxHash: "0xa", Source: "txlist", Asset: "ETH", From: address, To: "0x3", Amount: "1"},
			},
			{{BlockNumber: 10, TxHash: "0xa", Source: "txlist", Asset: "ETH", From: address, To: "0x3", Amount: "1"}},
		},
	}
	graph := New(repository)
	first, err := graph.Edges(context.Background(), EdgeQuery{Chain: "ethereum", Addresses: []string{address}, Direction: "both", Asset: "ETH", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.DataThroughBlock != 100 {
		t.Fatalf("first page=%+v", first)
	}
	second, err := graph.Edges(context.Background(), EdgeQuery{Chain: "ethereum", Addresses: []string{address}, Direction: "both", Asset: "ETH", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || repository.queries[1].After == nil || repository.queries[1].After.BlockNumber != 20 {
		t.Fatalf("second page=%+v query=%+v", second, repository.queries[1])
	}
}

func TestGraphRejectsUnsyncedAddressAndInvalidCursor(t *testing.T) {
	address := "0x0000000000000000000000000000000000000001"
	graph := New(&graphRepository{addresses: map[string]store.Address{address: {SyncStatus: "discovered"}}})
	_, err := graph.Edges(context.Background(), EdgeQuery{Addresses: []string{address}})
	if !errors.Is(err, ErrAddressNotSynced) {
		t.Fatalf("error=%v, want address not synced", err)
	}

	graph = New(&graphRepository{addresses: map[string]store.Address{address: {SyncStatus: "synced", NormalSyncedTo: 100, InternalSyncedTo: 100, TokenSyncedTo: 100}}})
	_, err = graph.Edges(context.Background(), EdgeQuery{Addresses: []string{address}, Cursor: "bad"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("error=%v, want invalid query", err)
	}
}

func TestGraphKeepsPreviouslySyncedDataAvailableDuringRefreshFailure(t *testing.T) {
	address := "0x0000000000000000000000000000000000000001"
	repository := &graphRepository{
		addresses: map[string]store.Address{address: {SyncStatus: "failed", NormalSyncedTo: 90, InternalSyncedTo: 90, TokenSyncedTo: 90, LastSyncedAt: time.Unix(1, 0)}},
		pages:     [][]store.Transfer{{}},
	}
	page, err := New(repository).Edges(context.Background(), EdgeQuery{Addresses: []string{address}})
	if err != nil {
		t.Fatal(err)
	}
	if page.DataThroughBlock != 90 {
		t.Fatalf("dataThroughBlock=%d, want 90", page.DataThroughBlock)
	}
	if page.DataStatus != "stale" {
		t.Fatalf("dataStatus=%q, want stale", page.DataStatus)
	}
}
