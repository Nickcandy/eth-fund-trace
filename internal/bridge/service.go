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
	HasTransferEvidence(context.Context, string, string, int64, string, string, string) (bool, error)
	QueryCrossChainLinks(context.Context, store.BridgeLinkQuery) ([]store.CrossChainLink, error)
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
	SourceAsset    string   `json:"sourceAsset"`
	SourceAmount   string   `json:"sourceAmount"`
	TargetAsset    string   `json:"targetAsset"`
	TargetAmount   string   `json:"targetAmount"`
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
	sourceAsset, sourceAmount, sourceFactOK := normalizeFact(request.SourceAsset, request.SourceAmount)
	targetAsset, targetAmount, targetFactOK := normalizeFact(request.TargetAsset, request.TargetAmount)
	if sourceErr != nil || targetErr != nil || source.Name == target.Name || sourceAddressErr != nil || targetAddressErr != nil || bridgeAddressErr != nil || !transactionHash.MatchString(request.SourceTxHash) || !transactionHash.MatchString(request.TargetTxHash) || request.SourceLogIndex < 0 || request.TargetLogIndex < 0 || len(request.Evidence) == 0 || !sourceFactOK || !targetFactOK {
		return store.CrossChainLink{}, ErrInvalidRequest
	}
	sourceExists, err := s.repository.HasTransferEvidence(ctx, source.Name, strings.ToLower(request.SourceTxHash), request.SourceLogIndex, sourceAddress, sourceAsset, sourceAmount)
	if err != nil {
		return store.CrossChainLink{}, err
	}
	targetExists, err := s.repository.HasTransferEvidence(ctx, target.Name, strings.ToLower(request.TargetTxHash), request.TargetLogIndex, targetAddress, targetAsset, targetAmount)
	if err != nil {
		return store.CrossChainLink{}, err
	}
	if !sourceExists || !targetExists {
		return store.CrossChainLink{}, ErrEvidenceNotFound
	}
	link := store.CrossChainLink{SourceChain: source.Name, SourceChainID: source.ID, SourceTxHash: strings.ToLower(request.SourceTxHash), SourceLogIndex: request.SourceLogIndex, SourceAddress: sourceAddress, TargetChain: target.Name, TargetChainID: target.ID, TargetTxHash: strings.ToLower(request.TargetTxHash), TargetLogIndex: request.TargetLogIndex, TargetAddress: targetAddress, BridgeAddress: bridgeAddress, SourceAsset: sourceAsset, SourceAmount: sourceAmount, TargetAsset: targetAsset, TargetAmount: targetAmount, Evidence: request.Evidence, Status: "confirmed", ObservedAt: s.clock().UTC()}
	return s.repository.UpsertCrossChainLink(ctx, link)
}

var ErrEvidenceNotFound = errors.New("cross-chain transfer evidence not found")

func normalizeFact(asset, amount string) (string, string, bool) {
	value, ok := new(big.Int).SetString(amount, 10)
	if !ok || value.Sign() < 0 {
		return "", "", false
	}
	if strings.EqualFold(asset, "ETH") {
		return "ETH", amount, true
	}
	normalized, err := ethaddr.Normalize(asset)
	return normalized, amount, err == nil
}

func (s *Service) List(ctx context.Context, chainName, address string, limit int64) ([]store.CrossChainLink, error) {
	return s.Query(ctx, store.BridgeLinkQuery{Chain: chainName, Address: address, Limit: limit})
}

func (s *Service) Query(ctx context.Context, query store.BridgeLinkQuery) ([]store.CrossChainLink, error) {
	chain, err := chains.Resolve(query.Chain)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	normalized, err := ethaddr.Normalize(query.Address)
	if err != nil || query.Limit < 0 || query.Limit > 500 || !validFilter(query.Status, "initiated", "proven", "finalized", "completed", "confirmed", "failed", "ambiguous") || !validFilter(query.Direction, "deposit", "withdrawal") || (query.Protocol != "" && query.Protocol != ProtocolOfficialOPStack) {
		return nil, ErrInvalidRequest
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	query.Chain, query.Address = chain.Name, normalized
	return s.repository.QueryCrossChainLinks(ctx, query)
}

func validFilter(value string, allowed ...string) bool {
	if value == "" {
		return true
	}
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
