package tracer

import (
	"context"
	"errors"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

type fakeRepository struct {
	addresses map[string]store.Address
	transfers []store.Transfer
	labels    map[string][]store.Label
	calls     []store.TransferQuery
}

func (r *fakeRepository) FindAddress(_ context.Context, _, address string) (store.Address, bool, error) {
	v, ok := r.addresses[address]
	return v, ok, nil
}
func (r *fakeRepository) QueryTransfers(_ context.Context, q store.TransferQuery) ([]store.Transfer, error) {
	r.calls = append(r.calls, q)
	return r.transfers, nil
}
func (r *fakeRepository) ListLabels(_ context.Context, _, address string) ([]store.Label, error) {
	return r.labels[address], nil
}

func TestTraceBatchesFrontierAndPrunesTerminal(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	terminal := "0x0000000000000000000000000000000000000002"
	next := "0x0000000000000000000000000000000000000003"
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "synced", LatestSyncedBlock: 100}, terminal: {SyncStatus: "synced", IsTerminal: true, LatestSyncedBlock: 100}, next: {SyncStatus: "synced", LatestSyncedBlock: 100}}, transfers: []store.Transfer{{TxHash: "0x1", From: seed, To: terminal, Asset: "ETH", Amount: "2"}, {TxHash: "0x2", From: terminal, To: next, Asset: "ETH", Amount: "1"}}, labels: map[string][]store.Label{}}
	result, err := New(r).Trace(context.Background(), Query{Address: seed, Depth: 3, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || len(result.Edges) != 1 || len(r.calls) != 1 {
		t.Fatalf("result=%+v calls=%d", result, len(r.calls))
	}
}

func TestTraceRejectsInvalidAndUnsyncedQueries(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	r := &fakeRepository{addresses: map[string]store.Address{seed: {SyncStatus: "discovered"}}}
	if _, err := New(r).Trace(context.Background(), Query{Address: seed, Depth: 6}); err != ErrInvalidQuery {
		t.Fatalf("err=%v", err)
	}
	if _, err := New(r).Trace(context.Background(), Query{Address: seed}); !errors.Is(err, ErrAddressNotSynced) {
		t.Fatalf("err=%v", err)
	}
}
