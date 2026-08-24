package bridge

import (
	"context"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

type repositoryStub struct {
	saved       store.CrossChainLink
	hasEvidence bool
}

func (r *repositoryStub) UpsertCrossChainLink(_ context.Context, link store.CrossChainLink) (store.CrossChainLink, error) {
	r.saved = link
	return link, nil
}
func (r *repositoryStub) ListCrossChainLinks(context.Context, string, string, int64) ([]store.CrossChainLink, error) {
	return []store.CrossChainLink{r.saved}, nil
}
func (r *repositoryStub) HasTransferEvidence(context.Context, string, string, int64, string, string, string) (bool, error) {
	return r.hasEvidence, nil
}
func (r *repositoryStub) QueryCrossChainLinks(context.Context, store.BridgeLinkQuery) ([]store.CrossChainLink, error) {
	return []store.CrossChainLink{r.saved}, nil
}
func (r *repositoryStub) HasTargetTransferEvidence(context.Context, string, string, string, string, string) (bool, error) {
	return r.hasEvidence, nil
}
func (r *repositoryStub) HasSourceTransferEvidence(context.Context, string, string, string, string, string) (bool, error) {
	return r.hasEvidence, nil
}

func TestServiceCreatesConfirmedEthereumBaseLink(t *testing.T) {
	repository := &repositoryStub{hasEvidence: true}
	service := New(repository)
	link, err := service.Create(context.Background(), CreateRequest{
		SourceChain: "ethereum", SourceTxHash: txHash('a'), SourceAddress: address('1'), SourceLogIndex: 2,
		TargetChain: "base", TargetTxHash: txHash('b'), TargetAddress: address('2'), TargetLogIndex: 3,
		BridgeAddress: address('3'), SourceAsset: "ETH", SourceAmount: "10", TargetAsset: "ETH", TargetAmount: "9", Evidence: []string{"bridge-provider-id:1"},
	})
	if err != nil || link.SourceChainID != 1 || link.TargetChainID != 8453 || link.Status != "confirmed" {
		t.Fatalf("link=%+v err=%v", link, err)
	}
}

func TestServiceRejectsUnprovenOrSameChainLink(t *testing.T) {
	service := New(&repositoryStub{hasEvidence: true})
	_, err := service.Create(context.Background(), CreateRequest{SourceChain: "ethereum", TargetChain: "ethereum", SourceTxHash: txHash('a'), TargetTxHash: txHash('b'), SourceAddress: address('1'), TargetAddress: address('2'), BridgeAddress: address('3')})
	if err == nil {
		t.Fatal("same-chain link must fail")
	}
	_, err = service.Create(context.Background(), CreateRequest{SourceChain: "ethereum", TargetChain: "base", SourceTxHash: txHash('a'), TargetTxHash: txHash('b'), SourceAddress: address('1'), TargetAddress: address('2'), BridgeAddress: address('3')})
	if err == nil {
		t.Fatal("link without evidence must fail")
	}
}

func TestServiceRejectsMissingStoredTransferEvidence(t *testing.T) {
	service := New(&repositoryStub{})
	_, err := service.Create(context.Background(), CreateRequest{SourceChain: "ethereum", TargetChain: "base", SourceTxHash: txHash('a'), TargetTxHash: txHash('b'), SourceAddress: address('1'), TargetAddress: address('2'), BridgeAddress: address('3'), SourceAsset: "ETH", SourceAmount: "10", TargetAsset: "ETH", TargetAmount: "9", Evidence: []string{"provider:1"}})
	if err != ErrEvidenceNotFound {
		t.Fatalf("err=%v", err)
	}
}

func txHash(char byte) string {
	value := make([]byte, 66)
	value[0], value[1] = '0', 'x'
	for i := 2; i < len(value); i++ {
		value[i] = char
	}
	return string(value)
}
func address(char byte) string {
	value := make([]byte, 42)
	value[0], value[1] = '0', 'x'
	for i := 2; i < len(value); i++ {
		value[i] = char
	}
	return string(value)
}
