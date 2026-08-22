package bridge

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/chains"
	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

var ErrInvalidRequest = errors.New("invalid cross-chain link")
var transactionHash = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

type Repository interface {
	UpsertCrossChainLink(context.Context, store.CrossChainLink) (store.CrossChainLink, error)
	ListCrossChainLinks(context.Context, string, string, int64) ([]store.CrossChainLink, error)
}

type CreateRequest struct {
	SourceChain    string   `json:"sourceChain"`
	SourceTxHash   string   `json:"sourceTxHash"`
	SourceLogIndex int64    `json:"sourceLogIndex"`
	SourceAddress  string   `json:"sourceAddress"`
	TargetChain    string   `json:"targetChain"`
	TargetTxHash   string   `json:"targetTxHash"`
	TargetLogIndex int64    `json:"targetLogIndex"`
	TargetAddress  string   `json:"targetAddress"`
	BridgeAddress  string   `json:"bridgeAddress"`
	Asset          string   `json:"asset"`
	Amount         string   `json:"amount"`
	Evidence       []string `json:"evidence"`
}

type Service struct {
	repository Repository
	clock      func() time.Time
}

func New(repository Repository) *Service { return &Service{repository: repository, clock: time.Now} }

func (s *Service) Create(ctx context.Context, request CreateRequest) (store.CrossChainLink, error) {
	source, sourceErr := chains.Resolve(request.SourceChain)
	target, targetErr := chains.Resolve(request.TargetChain)
	sourceAddress, sourceAddressErr := ethaddr.Normalize(request.SourceAddress)
	targetAddress, targetAddressErr := ethaddr.Normalize(request.TargetAddress)
	bridgeAddress, bridgeAddressErr := ethaddr.Normalize(request.BridgeAddress)
	amount, amountOK := new(big.Int).SetString(request.Amount, 10)
	asset := request.Asset
	if strings.EqualFold(asset, "ETH") {
		asset = "ETH"
	} else {
		asset, _ = ethaddr.Normalize(asset)
	}
	if sourceErr != nil || targetErr != nil || source.Name == target.Name || sourceAddressErr != nil || targetAddressErr != nil || bridgeAddressErr != nil || !transactionHash.MatchString(request.SourceTxHash) || !transactionHash.MatchString(request.TargetTxHash) || request.SourceLogIndex < 0 || request.TargetLogIndex < 0 || len(request.Evidence) == 0 || !amountOK || amount.Sign() < 0 || asset == "" {
		return store.CrossChainLink{}, ErrInvalidRequest
	}
	link := store.CrossChainLink{SourceChain: source.Name, SourceChainID: source.ID, SourceTxHash: strings.ToLower(request.SourceTxHash), SourceLogIndex: request.SourceLogIndex, SourceAddress: sourceAddress, TargetChain: target.Name, TargetChainID: target.ID, TargetTxHash: strings.ToLower(request.TargetTxHash), TargetLogIndex: request.TargetLogIndex, TargetAddress: targetAddress, BridgeAddress: bridgeAddress, Asset: asset, Amount: request.Amount, Evidence: request.Evidence, Status: "confirmed", ObservedAt: s.clock().UTC()}
	return s.repository.UpsertCrossChainLink(ctx, link)
}

func (s *Service) List(ctx context.Context, chainName, address string, limit int64) ([]store.CrossChainLink, error) {
	chain, err := chains.Resolve(chainName)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	normalized, err := ethaddr.Normalize(address)
	if err != nil || limit < 0 || limit > 500 {
		return nil, ErrInvalidRequest
	}
	if limit == 0 {
		limit = 100
	}
	return s.repository.ListCrossChainLinks(ctx, chain.Name, normalized, limit)
}
