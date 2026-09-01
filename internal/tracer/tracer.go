package tracer

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/chains"
	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/risk"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

var ErrInvalidQuery = errors.New("invalid trace query")
var ErrAddressNotSynced = errors.New("address is not synced")

const (
	traceRuleVersion       = "trace-v9"
	traceTransferRecordCap = 50_000
)

type AddressNotSyncedError struct {
	Chain, Address       string
	StartBlock, EndBlock int64
}

func (e AddressNotSyncedError) Error() string { return ErrAddressNotSynced.Error() + ": " + e.Address }
func (e AddressNotSyncedError) Unwrap() error { return ErrAddressNotSynced }

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	SetAddressIdentity(context.Context, string, string, store.AddressIdentity) error
	QueryTransfers(context.Context, store.TransferQuery) ([]store.Transfer, error)
	ListLabels(context.Context, string, string) ([]store.Label, error)
}

type TransactionAnalyzer interface {
	Analyze(context.Context, string, string) (store.TransactionAnalysis, error)
}

type CrossChainVerifier interface {
	Verify(context.Context, store.TransactionAnalysis) (VerifiedCrossChainTransfer, bool, error)
}

type VerifiedCrossChainTransfer = store.VerifiedCrossChainTransfer

// AddressInspector resolves chain-confirmed account types before graph expansion.
type AddressInspector interface {
	InspectAddress(context.Context, string, string) (store.AddressIdentity, error)
}

type knownAddressInspector interface {
	KnownAddressIdentity(string, string) (store.AddressIdentity, bool)
}

type Query struct {
	Chain, Address, Direction, Asset string
	Depth                            int
}
type Node struct {
	Chain       string   `json:"chain"`
	Address     string   `json:"address"`
	Depth       int      `json:"depth"`
	Terminal    bool     `json:"terminal"`
	AddressType string   `json:"addressType"`
	Protocol    string   `json:"protocol,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	StopReason  string   `json:"stopReason,omitempty"`
}
type Edge struct {
	Chain                 string               `bson:"chain" json:"chain"`
	SourceChain           string               `bson:"sourceChain,omitempty" json:"sourceChain,omitempty"`
	TargetChain           string               `bson:"targetChain,omitempty" json:"targetChain,omitempty"`
	From                  string               `bson:"from" json:"from"`
	To                    string               `bson:"to" json:"to"`
	AssetType             string               `bson:"assetType" json:"assetType"`
	Asset                 string               `bson:"asset" json:"asset"`
	Symbol                string               `bson:"symbol,omitempty" json:"symbol,omitempty"`
	Decimals              int32                `bson:"decimals" json:"decimals"`
	TokenMetadataComplete bool                 `bson:"tokenMetadataComplete,omitempty" json:"tokenMetadataComplete"`
	TotalAmount           string               `bson:"totalAmount" json:"totalAmount"`
	TransferCount         int64                `bson:"transferCount" json:"transferCount"`
	Kind                  string               `bson:"kind" json:"kind"`
	Depth                 int                  `bson:"depth" json:"depth"`
	Path                  []string             `bson:"path" json:"path"`
	TxHash                string               `bson:"txHash,omitempty" json:"txHash,omitempty"`
	SourceTxHash          string               `bson:"sourceTxHash,omitempty" json:"sourceTxHash,omitempty"`
	SourceAmount          string               `bson:"sourceAmount,omitempty" json:"sourceAmount,omitempty"`
	SourceAsset           string               `bson:"sourceAsset,omitempty" json:"sourceAsset,omitempty"`
	FirstBlock            int64                `bson:"firstBlock,omitempty" json:"firstBlock,omitempty"`
	FirstTime             time.Time            `bson:"firstTime,omitempty" json:"firstTime,omitempty"`
	LatestBlock           int64                `bson:"latestBlock,omitempty" json:"latestBlock,omitempty"`
	LatestTime            time.Time            `bson:"latestTime,omitempty" json:"latestTime,omitempty"`
	ConversionStatus      string               `bson:"conversionStatus,omitempty" json:"conversionStatus,omitempty"`
	ConversionScanned     int                  `bson:"conversionScanned,omitempty" json:"conversionScanned,omitempty"`
	ConversionEvidence    []ConversionEvidence `bson:"conversionEvidence,omitempty" json:"conversionEvidence,omitempty"`
	Protocol              string               `bson:"protocol,omitempty" json:"protocol,omitempty"`
	ProtocolAction        string               `bson:"protocolAction,omitempty" json:"protocolAction,omitempty"`
	ProtocolMemo          string               `bson:"protocolMemo,omitempty" json:"protocolMemo,omitempty"`
}

// ConversionEvidence is a bounded transaction-level explanation for a semantic conversion edge.
type ConversionEvidence struct {
	TxHash            string   `bson:"txHash" json:"txHash"`
	Protocol          string   `bson:"protocol" json:"protocol"`
	Version           string   `bson:"version" json:"version"`
	Status            string   `bson:"status" json:"status"`
	Initiator         string   `bson:"initiator,omitempty" json:"initiator,omitempty"`
	Router            string   `bson:"router,omitempty" json:"router,omitempty"`
	Executor          string   `bson:"executor,omitempty" json:"executor,omitempty"`
	LiquidityProvider string   `bson:"liquidityProvider,omitempty" json:"liquidityProvider,omitempty"`
	Recipient         string   `bson:"recipient,omitempty" json:"recipient,omitempty"`
	TokenIn           string   `bson:"tokenIn,omitempty" json:"tokenIn,omitempty"`
	AmountIn          string   `bson:"amountIn,omitempty" json:"amountIn,omitempty"`
	TokenOut          string   `bson:"tokenOut,omitempty" json:"tokenOut,omitempty"`
	AmountOut         string   `bson:"amountOut,omitempty" json:"amountOut,omitempty"`
	Evidence          []string `bson:"evidence" json:"evidence"`
}
type Result struct {
	Nodes             []Node           `bson:"nodes" json:"nodes"`
	Edges             []Edge           `bson:"edges" json:"edges"`
	Paths             [][]string       `bson:"paths,omitempty" json:"paths,omitempty"`
	DataThroughBlock  int64            `bson:"dataThroughBlock" json:"dataThroughBlock"`
	DataThroughBlocks map[string]int64 `bson:"dataThroughBlocks,omitempty" json:"dataThroughBlocks,omitempty"`
	DataStatus        string           `bson:"dataStatus" json:"dataStatus"`
	Labels            []store.Label    `bson:"labels,omitempty" json:"labels,omitempty"`
	Risk              risk.Result      `bson:"risk" json:"risk"`
	RuleVersion       string           `bson:"ruleVersion" json:"ruleVersion"`
	branchStates      []branchState
	MoneyStates       []store.MoneyState    `bson:"moneyStates,omitempty" json:"moneyStates,omitempty"`
	MoneyTransfers    []store.MoneyTransfer `bson:"moneyTransfers,omitempty" json:"moneyTransfers,omitempty"`
	Ledgers           []store.AssetLedger   `bson:"ledgers,omitempty" json:"ledgers,omitempty"`
	Reconciliation    string                `bson:"reconciliation,omitempty" json:"reconciliation,omitempty"`
}

type branchState struct {
	Address       string
	Direction     string
	AssetMode     string
	Asset         string
	AnchorBlock   int64
	EnteringQuery store.CounterpartyQuery
	EnteringTx    store.Transfer
	EnteringEdge  int
	Amount        string
	Contract      bool
	Identity      store.AddressIdentity
	Path          []string
}

const maxTraceNodes = 5000

type Graph struct {
	repository           Repository
	analyzer             TransactionAnalyzer
	crossChainVerifiers  []CrossChainVerifier
	inspector            AddressInspector
	requiredStartBlocks  map[string]int64
	existingDataOnly     bool
	terminalDependencies map[string]string
}

func New(repository Repository) *Graph { return &Graph{repository: repository} }

func (g *Graph) WithTransactionAnalyzer(analyzer TransactionAnalyzer) *Graph {
	g.analyzer = analyzer
	return g
}

func (g *Graph) WithCrossChainVerifier(verifier CrossChainVerifier) *Graph {
	g.crossChainVerifiers = append(g.crossChainVerifiers, verifier)
	return g
}

// WithAddressInspector configures chain-level account type detection.
func (g *Graph) WithAddressInspector(inspector AddressInspector) *Graph {
	g.inspector = inspector
	return g
}

func (g *Graph) WithRequiredStartBlocks(blocks map[string]int64) *Graph {
	g.requiredStartBlocks = blocks
	return g
}

func (g *Graph) WithExistingDataOnly(enabled bool) *Graph {
	g.existingDataOnly = enabled
	return g
}

func (g *Graph) withTerminalDependencies(dependencies map[string]string) *Graph {
	copy := *g
	copy.terminalDependencies = dependencies
	return &copy
}

func dependencyAddressKey(chain, address string) string {
	return strings.ToLower(chain + ":" + address)
}

func (g *Graph) dependencyStopReason(chain, address string) string {
	return g.terminalDependencies[dependencyAddressKey(chain, address)]
}

func (g *Graph) addressCovered(address store.Address, from, through int64) bool {
	if g.existingDataOnly {
		return true
	}
	if address.SyncStatus != "synced" || through < from {
		return false
	}
	return address.NormalSyncedFrom <= from && address.NormalSyncedTo >= through &&
		address.InternalSyncedFrom <= from && address.InternalSyncedTo >= through &&
		address.TokenSyncedFrom <= from && address.TokenSyncedTo >= through
}

func isHighFrequencyAddress(address store.Address) bool {
	return address.SyncStatus == "partial" && address.SyncError == "high_frequency"
}

func addressIdentity(address store.Address) store.AddressIdentity {
	addressType := address.AddressType
	if addressType == "" {
		addressType = "unknown"
		if address.IsContract {
			addressType = "contract"
		}
	}
	return store.AddressIdentity{AddressType: addressType, Protocol: address.Protocol, Roles: address.Roles}
}

func ValidateQuery(query Query) error {
	q, err := normalize(query)
	if err != nil {
		return err
	}
	if _, err := ethaddr.Normalize(q.Address); err != nil {
		return ErrInvalidQuery
	}
	return nil
}

func (g *Graph) Trace(ctx context.Context, query Query) (Result, error) {
	result, err := g.traceSameChain(ctx, query)
	if err == nil {
		result.DataThroughBlocks = map[string]int64{"ethereum": result.DataThroughBlock}
	}
	return result, err
}

func (g *Graph) traceSameChain(ctx context.Context, query Query) (Result, error) {
	q, err := normalize(query)
	if err != nil {
		return Result{}, err
	}
	seed, err := ethaddr.Normalize(q.Address)
	if err != nil {
		return Result{}, ErrInvalidQuery
	}
	metadata, found, err := g.repository.FindAddress(ctx, q.Chain, seed)
	if err != nil {
		return Result{}, err
	}
	_, dataThroughBlock, covered := metadata.CommonCoverage()
	highFrequency := isHighFrequencyAddress(metadata)
	if g.existingDataOnly && !covered {
		covered = true
	}
	if !found || !highFrequency && (!covered || !g.addressCovered(metadata, g.requiredStartBlocks[q.Chain], dataThroughBlock)) {
		return Result{}, AddressNotSyncedError{Chain: q.Chain, Address: seed}
	}
	dataStatus := "synced"
	if g.existingDataOnly || metadata.SyncStatus == "partial" {
		dataStatus = "partial"
	}
	result := Result{DataThroughBlock: dataThroughBlock, DataStatus: dataStatus, RuleVersion: traceRuleVersion}
	seedLabels, err := g.repository.ListLabels(ctx, q.Chain, seed)
	if err != nil {
		return Result{}, err
	}
	result.Labels = append(result.Labels, seedLabels...)
	directions := []string{q.Direction}
	if q.Direction == "both" {
		directions = []string{"in", "out"}
	}
	assets := []traceAsset{{AssetMode: "all"}}
	frontier := make([]branchState, 0, len(directions)*len(assets))
	visitedStates := make(map[string]bool, len(directions)*len(assets))
	visitedNodes := map[string]bool{seed: true}
	seedStopReason := g.dependencyStopReason(q.Chain, seed)
	seedIdentity := addressIdentity(metadata)
	if identity, known := g.knownAddressIdentity(q.Chain, seed); known {
		seedIdentity = identity
		if err := g.repository.SetAddressIdentity(ctx, q.Chain, seed, identity); err != nil {
			return Result{}, err
		}
	}
	seedTerminal := metadata.IsTerminal || highFrequency || seedStopReason != "" || hasTerminalLabel(seedLabels) || isKnownWalletTerminal(seedIdentity) || isCrossChainBridge(seedIdentity)
	seedNode := Node{Chain: q.Chain, Address: seed, Depth: 0, Terminal: seedTerminal, AddressType: seedIdentity.AddressType, Protocol: seedIdentity.Protocol, Roles: seedIdentity.Roles}
	if highFrequency {
		seedNode.StopReason = "high_frequency"
	} else if seedStopReason != "" {
		seedNode.StopReason = seedStopReason
		result.DataStatus = "partial"
	} else if isKnownWalletTerminal(seedIdentity) {
		seedNode.StopReason = string(store.StopTerminal)
	} else if isCrossChainBridge(seedIdentity) {
		seedNode.StopReason = "cross_chain_bridge"
	}
	result.Nodes = append(result.Nodes, seedNode)
	for _, direction := range directions {
		for _, asset := range assets {
			state := branchState{Address: seed, Direction: direction, AssetMode: asset.AssetMode, Asset: asset.Asset, Identity: seedIdentity, Path: []string{seed}}
			frontier = append(frontier, state)
			visitedStates[branchStateKey(state)] = true
			result.branchStates = append(result.branchStates, state)
		}
	}
	for depth := 0; len(frontier) > 0 && len(visitedNodes) < maxTraceNodes; depth++ {
		sort.Slice(frontier, func(i, j int) bool {
			return branchStateKey(frontier[i]) < branchStateKey(frontier[j])
		})
		next := make([]branchState, 0)
		processedAddresses := make(map[string]bool)
		expandedAddresses := make(map[string]bool)
		expand := func(state branchState, candidates []store.CounterpartySummary, conversionEvidence map[string][]ConversionEvidence) error {
			sort.SliceStable(candidates, func(i, j int) bool {
				if state.Direction == "out" {
					return candidates[i].Representative.BlockNumber < candidates[j].Representative.BlockNumber
				}
				return candidates[i].Representative.BlockNumber > candidates[j].Representative.BlockNumber
			})
			var budget *big.Int
			if state.Amount != "" {
				budget, _ = new(big.Int).SetString(state.Amount, 10)
			}
			for _, summary := range candidates {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !supportedSummaryAsset(summary) {
					continue
				}
				canonicalizeSummaryAsset(q.Chain, &summary)
				if !aboveTraceThreshold(summary) {
					continue
				}
				other := strings.ToLower(summary.To)
				if state.Direction == "in" {
					other = strings.ToLower(summary.From)
				}
				if other == "" {
					continue
				}
				assetMode, asset := summaryAsset(summary)
				stateKey := branchStateKey(branchState{Address: other, Direction: state.Direction, AssetMode: assetMode, Asset: asset, AnchorBlock: summary.Representative.BlockNumber, EnteringTx: summary.Representative})
				if visitedStates[stateKey] {
					continue
				}
				amount, amountOK := new(big.Int).SetString(summary.TotalAmount, 10)
				if !amountOK || amount.Sign() <= 0 || budget != nil && budget.Sign() <= 0 {
					continue
				}
				if budget != nil && amount.Cmp(budget) > 0 {
					amount = new(big.Int).Set(budget)
				}
				if budget != nil {
					budget.Sub(budget, amount)
				}
				summary.TotalAmount = amount.String()
				if !aboveTraceThreshold(summary) {
					continue
				}
				path := append(append([]string(nil), state.Path...), other)
				if !visitedNodes[other] && len(visitedNodes) >= maxTraceNodes {
					result.DataStatus = "partial"
					setNodeTerminal(result.Nodes, state.Address, string(store.StopNodeLimit))
					continue
				}
				edge := edgeFromSummary(summary, depth+1, path)
				edge.ConversionEvidence = conversionEvidence[conversionKey(summary.From, summary.To, summary.Asset)]
				result.Edges = append(result.Edges, edge)
				expandedAddresses[strings.ToLower(state.Address)] = true
				edgeIndex := len(result.Edges) - 1
				result.MoneyTransfers = append(result.MoneyTransfers, moneyTransfer(summary, ""))
				result.MoneyStates = append(result.MoneyStates,
					store.MoneyState{Chain: summary.Chain, Address: strings.ToLower(summary.From), Direction: "out", AssetType: summary.AssetType, Asset: summary.Asset, Amount: summary.TotalAmount, RemainingAmount: summary.TotalAmount, EntryTxHash: summary.Representative.TxHash, EntryBlock: summary.Representative.BlockNumber, Path: path, Evidence: "transfer", Inferred: state.Amount != ""},
					store.MoneyState{Chain: summary.Chain, Address: strings.ToLower(summary.To), Direction: "in", AssetType: summary.AssetType, Asset: summary.Asset, Amount: summary.TotalAmount, RemainingAmount: summary.TotalAmount, EntryTxHash: summary.Representative.TxHash, EntryBlock: summary.Representative.BlockNumber, Path: path, Evidence: "transfer", Inferred: state.Amount != ""},
				)
				if other == zeroAddress {
					if !visitedNodes[other] && len(visitedNodes) < maxTraceNodes {
						visitedNodes[other] = true
						result.Nodes = append(result.Nodes, Node{Chain: q.Chain, Address: other, Depth: depth + 1, Terminal: true})
					}
					continue
				}
				otherMetadata, otherFound, metadataErr := g.repository.FindAddress(ctx, q.Chain, other)
				if metadataErr != nil {
					return metadataErr
				}
				otherHighFrequency := isHighFrequencyAddress(otherMetadata)
				dependencyStopReason := g.dependencyStopReason(q.Chain, other)
				identity := addressIdentity(otherMetadata)
				if knownIdentity, known := g.knownAddressIdentity(q.Chain, other); known {
					identity = knownIdentity
					if metadataErr = g.repository.SetAddressIdentity(ctx, q.Chain, other, identity); metadataErr != nil {
						return metadataErr
					}
					otherMetadata.AddressType = identity.AddressType
					otherMetadata.IsContract = identity.AddressType == "contract"
					otherMetadata.Protocol = identity.Protocol
					otherMetadata.Roles = identity.Roles
				} else if (identity.AddressType == "unknown" || identity.AddressType == "contract" && identity.Protocol == "") && g.inspector != nil && !g.existingDataOnly {
					identity, metadataErr = g.inspector.InspectAddress(ctx, q.Chain, other)
					if metadataErr != nil {
						return metadataErr
					}
					if metadataErr = g.repository.SetAddressIdentity(ctx, q.Chain, other, identity); metadataErr != nil {
						return metadataErr
					}
					otherMetadata.AddressType = identity.AddressType
					otherMetadata.IsContract = identity.AddressType == "contract"
					otherMetadata.Protocol = identity.Protocol
					otherMetadata.Roles = identity.Roles
				}
				contract := identity.AddressType == "contract"
				if err := g.annotateTHORChainRouterCall(ctx, q.Chain, identity, summary.Representative, &result.Edges[edgeIndex]); err != nil {
					return err
				}
				crossChainResolved, crossChainErr := g.appendVerifiedCrossChainEndpoints(ctx, q.Chain, state, identity, summary, depth+1, path, &result)
				if crossChainErr != nil {
					return crossChainErr
				}
				if crossChainResolved {
					if identity.Protocol == "" {
						identity.Protocol = "thorchain"
						if !containsRole(identity.Roles, "thorchain_vault") {
							identity.Roles = append(identity.Roles, "thorchain_vault")
						}
					}
					if err := g.repository.SetAddressIdentity(ctx, q.Chain, other, identity); err != nil {
						return err
					}
					for index := range result.Nodes {
						if result.Nodes[index].Chain == q.Chain && strings.EqualFold(result.Nodes[index].Address, other) {
							result.Nodes[index].Protocol = identity.Protocol
							result.Nodes[index].Roles = identity.Roles
						}
					}
				}
				requiredFrom, requiredThrough := g.requiredStartBlocks[q.Chain], dataThroughBlock
				if state.Direction == "out" {
					requiredFrom = summary.Representative.BlockNumber
				} else {
					requiredThrough = summary.Representative.BlockNumber
				}
				knownWalletTerminal := isKnownWalletTerminal(identity)
				if !crossChainResolved && !contract && !otherHighFrequency && !knownWalletTerminal && dependencyStopReason == "" && (!otherFound || !g.addressCovered(otherMetadata, requiredFrom, requiredThrough)) {
					dependency := AddressNotSyncedError{Chain: q.Chain, Address: other}
					if state.Direction == "out" {
						dependency.StartBlock = summary.Representative.BlockNumber
					} else {
						dependency.EndBlock = summary.Representative.BlockNumber
					}
					return dependency
				}
				if otherMetadata.SyncStatus == "partial" {
					result.DataStatus = "partial"
				}
				relation := store.CounterpartyQuery{Chain: q.Chain, Address: state.Address, Counterparty: other, Direction: state.Direction, AssetMode: assetMode, Asset: asset}
				nextState := branchState{Address: other, Direction: state.Direction, AssetMode: assetMode, Asset: asset, AnchorBlock: summary.Representative.BlockNumber, EnteringQuery: relation, EnteringTx: summary.Representative, EnteringEdge: edgeIndex, Amount: summary.TotalAmount, Contract: contract, Identity: identity, Path: path}
				if len(visitedNodes) >= maxTraceNodes {
					continue
				}
				otherLabels, labelsErr := g.repository.ListLabels(ctx, q.Chain, other)
				if labelsErr != nil {
					return labelsErr
				}
				result.Labels = appendLabels(result.Labels, otherLabels)
				terminal := crossChainResolved || otherMetadata.IsTerminal || otherHighFrequency && !contract || dependencyStopReason != "" || hasTerminalLabel(otherLabels) || knownWalletTerminal || isCrossChainBridge(identity)
				if dependencyStopReason != "" {
					result.DataStatus = "partial"
				}
				visitedStates[stateKey] = true
				result.branchStates = append(result.branchStates, nextState)
				isNewNode := !visitedNodes[other]
				visitedNodes[other] = true
				if !terminal && !crossChainResolved {
					next = append(next, nextState)
				}
				if isNewNode {
					node := Node{Chain: q.Chain, Address: other, Depth: depth + 1, Terminal: terminal, AddressType: identity.AddressType, Protocol: identity.Protocol, Roles: identity.Roles}
					if crossChainResolved {
						node.StopReason = "cross_chain_bridge"
					} else if otherHighFrequency && !contract {
						node.StopReason = "high_frequency"
					} else if dependencyStopReason != "" {
						node.StopReason = dependencyStopReason
					} else if knownWalletTerminal {
						node.StopReason = string(store.StopTerminal)
					} else if isCrossChainBridge(identity) {
						node.StopReason = "cross_chain_bridge"
					}
					result.Nodes = append(result.Nodes, node)
				}
				result.Paths = append(result.Paths, path)
			}
			return nil
		}
		for _, state := range frontier {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			if nodeTerminal(result.Nodes, state.Address) {
				continue
			}
			processedAddresses[strings.ToLower(state.Address)] = true
			if state.EnteringQuery.Address != "" && state.Contract && !isTHORChainVault(state.Identity) && g.existingDataOnly {
				setNodeTerminal(result.Nodes, state.Address, "unsupported_contract")
				continue
			}
			if state.EnteringQuery.Address != "" && state.Contract && !isTHORChainVault(state.Identity) {
				if g.analyzer == nil || q.Chain != "ethereum" || state.EnteringTx.TxHash == "" {
					setNodeTerminal(result.Nodes, state.Address, "unsupported_contract")
					continue
				}
				if state.EnteringEdge >= 0 && state.EnteringEdge < len(result.Edges) {
					result.Edges[state.EnteringEdge].ConversionScanned = 1
					result.Edges[state.EnteringEdge].ConversionStatus = "complete"
				}
				conversionState := state
				conversionState.EnteringQuery = store.CounterpartyQuery{}
				conversionState.AssetMode, conversionState.Asset = transferAsset(state.EnteringTx)
				conversion, ok, conversionErr := g.conversionTransfer(ctx, q.Chain, conversionState, state.EnteringTx.TxHash)
				if conversionErr != nil {
					return Result{}, conversionErr
				}
				if ok {
					if mergeExistingSwapLegs(result.Edges, conversion) {
						continue
					}
					edgeStart := len(result.Edges)
					aggregates := aggregateConversions([]analyzedConversion{conversion})
					outputState := state
					outputState.Amount = ""
					assets := make([]string, 0, len(aggregates))
					for asset := range aggregates {
						assets = append(assets, asset)
					}
					sort.Strings(assets)
					for _, asset := range assets {
						values := aggregates[asset]
						summaries := make([]store.CounterpartySummary, 0, len(values))
						evidence := make(map[string][]ConversionEvidence, len(values))
						for _, value := range values {
							summaries = append(summaries, value.Summary)
							evidence[conversionKey(value.Summary.From, value.Summary.To, value.Summary.Asset)] = value.Evidence
						}
						if err := expand(outputState, summaries, evidence); err != nil {
							return Result{}, err
						}
					}
					if conversion.Protocol != "" {
						for index := edgeStart; index < len(result.Edges); index++ {
							result.Edges[index].Protocol = conversion.Protocol
							result.Edges[index].ProtocolAction = conversion.ProtocolAction
							result.Edges[index].ProtocolMemo = conversion.ProtocolMemo
						}
						if conversion.VaultAddress != "" {
							identity, identityErr := g.markTHORChainVault(ctx, q.Chain, conversion.VaultAddress, result.Nodes, next)
							if identityErr != nil {
								return Result{}, identityErr
							}
							for index := range next {
								if strings.EqualFold(next[index].Address, conversion.VaultAddress) {
									next[index].Identity = identity
								}
							}
						}
					}
				} else {
					setNodeTerminal(result.Nodes, state.Address, "ambiguous_conversion")
				}
				continue
			}
			fromBlock, toBlock := int64(0), int64(0)
			if state.Direction == "out" {
				fromBlock = state.AnchorBlock
			} else {
				toBlock = state.AnchorBlock
			}
			transfers, queryErr := g.repository.QueryTransfers(ctx, store.TransferQuery{Chain: q.Chain, Addresses: []string{state.Address}, Direction: state.Direction, AssetMode: state.AssetMode, Asset: state.Asset, FromBlock: fromBlock, ToBlock: toBlock, Limit: traceTransferRecordCap})
			if queryErr != nil {
				return Result{}, queryErr
			}
			if len(transfers) >= traceTransferRecordCap {
				result.DataStatus = "partial"
				setNodeTerminal(result.Nodes, state.Address, "high_frequency")
				continue
			}
			summaries := make([]store.CounterpartySummary, 0, len(transfers))
			for _, transfer := range transfers {
				summaries = append(summaries, transferSummary(transfer))
			}
			if err := expand(state, summaries, nil); err != nil {
				return Result{}, err
			}
		}
		for address := range processedAddresses {
			if !expandedAddresses[address] && !nodeTerminal(result.Nodes, address) {
				setNodeTerminal(result.Nodes, address, string(store.StopNoMatchingTransfers))
			}
		}
		frontier = next
	}
	if len(frontier) > 0 {
		result.DataStatus = "partial"
		for _, state := range frontier {
			setNodeTerminal(result.Nodes, state.Address, string(store.StopNodeLimit))
		}
	}
	result.Risk = risk.Analyze(seed, nil, seedLabels)
	consumeMoneyStates(result.MoneyStates)
	result.Ledgers = buildLedgers(result.MoneyStates)
	result.Reconciliation = "complete"
	for _, ledger := range result.Ledgers {
		if ledger.Status != "complete" {
			result.Reconciliation = "partial"
			break
		}
	}
	return result, nil
}

func (g *Graph) knownAddressIdentity(chain, address string) (store.AddressIdentity, bool) {
	inspector, ok := g.inspector.(knownAddressInspector)
	if !ok {
		return store.AddressIdentity{}, false
	}
	return inspector.KnownAddressIdentity(chain, address)
}

func buildLedgers(states []store.MoneyState) []store.AssetLedger {
	ledgers := make(map[string]*store.AssetLedger)
	for _, state := range states {
		amount := state.Amount
		if amount == "" {
			continue
		}
		key := strings.ToLower(state.Address + "|" + state.Asset)
		ledger := ledgers[key]
		if ledger == nil {
			ledger = &store.AssetLedger{Address: state.Address, Asset: state.Asset, OpeningAmount: "0", IncomingAmount: "0", OutgoingAmount: "0", ExplainedAmount: "0", UnexplainedAmount: "0", Status: "complete"}
			ledgers[key] = ledger
		}
		if state.Direction == "out" {
			ledger.OutgoingAmount = addDecimal(ledger.OutgoingAmount, amount)
			ledger.UnexplainedAmount = addDecimal(ledger.UnexplainedAmount, state.RemainingAmount)
			ledger.OpeningAmount = ledger.UnexplainedAmount
			matched := subtractDecimal(amount, state.RemainingAmount)
			ledger.ExplainedAmount = addDecimal(ledger.ExplainedAmount, matched)
			if state.RemainingAmount != "0" {
				ledger.Status = "partial"
			}
		} else {
			ledger.IncomingAmount = addDecimal(ledger.IncomingAmount, amount)
		}
	}
	result := make([]store.AssetLedger, 0, len(ledgers))
	for _, ledger := range ledgers {
		result = append(result, *ledger)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Address+result[i].Asset < result[j].Address+result[j].Asset })
	return result
}

// consumeMoneyStates applies FIFO matching to inferred account balances.
func consumeMoneyStates(states []store.MoneyState) {
	sort.SliceStable(states, func(i, j int) bool {
		if states[i].Address != states[j].Address || states[i].Asset != states[j].Asset {
			return states[i].Address+states[i].Asset < states[j].Address+states[j].Asset
		}
		return states[i].EntryBlock < states[j].EntryBlock
	})
	queues := make(map[string][]*store.MoneyState)
	for i := range states {
		state := &states[i]
		key := strings.ToLower(state.Address + "|" + state.Asset)
		amount, ok := new(big.Int).SetString(state.Amount, 10)
		if !ok {
			continue
		}
		if state.Direction == "in" {
			queues[key] = append(queues[key], state)
			continue
		}
		for amount.Sign() > 0 && len(queues[key]) > 0 {
			incoming := queues[key][0]
			available, valid := new(big.Int).SetString(incoming.RemainingAmount, 10)
			if !valid || available.Sign() == 0 {
				queues[key] = queues[key][1:]
				continue
			}
			used := new(big.Int).Set(available)
			if used.Cmp(amount) > 0 {
				used.Set(amount)
			}
			available.Sub(available, used)
			amount.Sub(amount, used)
			incoming.RemainingAmount = available.String()
			if available.Sign() == 0 {
				queues[key] = queues[key][1:]
			}
		}
		state.RemainingAmount = amount.String()
	}
}

func addDecimal(left, right string) string {
	l, lok := new(big.Int).SetString(left, 10)
	r, rok := new(big.Int).SetString(right, 10)
	if !lok || !rok {
		return left
	}
	return new(big.Int).Add(l, r).String()
}

func subtractDecimal(left, right string) string {
	l, lok := new(big.Int).SetString(left, 10)
	r, rok := new(big.Int).SetString(right, 10)
	if !lok || !rok || l.Cmp(r) < 0 {
		return "0"
	}
	return new(big.Int).Sub(l, r).String()
}

func edgeFromSummary(summary store.CounterpartySummary, depth int, path []string) Edge {
	kind := summary.Representative.TransferKind
	if kind == "" {
		kind = "transfer"
	}
	return Edge{Chain: summary.Chain, From: summary.From, To: summary.To, AssetType: summary.AssetType, Asset: summary.Asset, Symbol: summary.Symbol, Decimals: summary.Decimals, TokenMetadataComplete: summary.TokenMetadataComplete, TotalAmount: summary.TotalAmount, TransferCount: summary.TransferCount, Kind: kind, Depth: depth, Path: path, TxHash: summary.Representative.TxHash, FirstBlock: summary.EarliestBlock, FirstTime: summary.EarliestTime, LatestBlock: summary.LatestBlock, LatestTime: summary.LatestTime}
}

func transferSummary(transfer store.Transfer) store.CounterpartySummary {
	amount := transferAmount(transfer)
	return store.CounterpartySummary{
		Chain: transfer.Chain, ChainID: transfer.ChainID, From: transfer.From, To: transfer.To,
		AssetType: transfer.AssetType, Asset: transfer.Asset, Symbol: transfer.Symbol,
		Decimals: transfer.Decimals, TokenMetadataComplete: transfer.TokenMetadataComplete,
		TotalAmount: amount, TransferCount: 1, EarliestBlock: transfer.BlockNumber,
		EarliestTime: transfer.BlockTime, LatestBlock: transfer.BlockNumber,
		LatestTime: transfer.BlockTime, LatestTransfer: transfer, Representative: transfer,
	}
}

func moneyTransfer(summary store.CounterpartySummary, reason store.StopReason) store.MoneyTransfer {
	return store.MoneyTransfer{
		Chain: summary.Chain, From: summary.From, To: summary.To, Asset: summary.Asset,
		Amount: summary.TotalAmount, TxHash: summary.Representative.TxHash,
		Kind: summary.Representative.TransferKind, BlockNumber: summary.Representative.BlockNumber,
		Evidence: "transfer", Inferred: summary.TotalAmount != transferAmount(summary.Representative), StopReason: reason,
	}
}

func summaryAsset(summary store.CounterpartySummary) (string, string) {
	if summary.AssetType == "eth" || strings.EqualFold(summary.Asset, "ETH") {
		return "eth", "ETH"
	}
	return "contract", strings.ToLower(summary.Asset)
}

type analyzedConversion struct {
	Transfer       store.Transfer
	Evidence       ConversionEvidence
	Protocol       string
	ProtocolAction string
	ProtocolMemo   string
	VaultAddress   string
}

type conversionAggregate struct {
	Summary  store.CounterpartySummary
	Evidence []ConversionEvidence
}

func aggregateConversions(conversions []analyzedConversion) map[string][]conversionAggregate {
	byAsset := make(map[string]map[string]*conversionAggregate)
	for _, conversion := range conversions {
		transfer := conversion.Transfer
		summary := transferSummary(transfer)
		canonicalizeSummaryAsset(transfer.Chain, &summary)
		if !aboveTraceThreshold(summary) {
			continue
		}
		assetKey := strings.ToLower(transfer.Asset)
		if byAsset[assetKey] == nil {
			byAsset[assetKey] = make(map[string]*conversionAggregate)
		}
		key := strings.ToLower(transfer.From + "|" + transfer.To)
		aggregate := byAsset[assetKey][key]
		if aggregate == nil {
			aggregate = &conversionAggregate{Summary: store.CounterpartySummary{Chain: transfer.Chain, ChainID: transfer.ChainID, From: transfer.From, To: transfer.To, AssetType: transfer.AssetType, Asset: transfer.Asset, Symbol: transfer.Symbol, Decimals: transfer.Decimals, TokenMetadataComplete: transfer.TokenMetadataComplete, TotalAmount: "0", EarliestBlock: transfer.BlockNumber, EarliestTime: transfer.BlockTime, LatestBlock: transfer.BlockNumber, LatestTime: transfer.BlockTime, LatestTransfer: transfer, Representative: transfer}}
			byAsset[assetKey][key] = aggregate
		}
		amount, ok := new(big.Int).SetString(transferAmount(transfer), 10)
		if !ok {
			continue
		}
		total, _ := new(big.Int).SetString(aggregate.Summary.TotalAmount, 10)
		aggregate.Summary.TotalAmount = total.Add(total, amount).String()
		aggregate.Summary.TransferCount++
		aggregate.Evidence = append(aggregate.Evidence, conversion.Evidence)
		if transfer.BlockNumber < aggregate.Summary.EarliestBlock {
			aggregate.Summary.EarliestBlock, aggregate.Summary.EarliestTime = transfer.BlockNumber, transfer.BlockTime
		}
		if transfer.BlockNumber > aggregate.Summary.LatestBlock {
			aggregate.Summary.LatestBlock, aggregate.Summary.LatestTime, aggregate.Summary.LatestTransfer = transfer.BlockNumber, transfer.BlockTime, transfer
		}
	}
	result := make(map[string][]conversionAggregate, len(byAsset))
	for asset, values := range byAsset {
		for _, aggregate := range values {
			result[asset] = append(result[asset], *aggregate)
		}
		sort.Slice(result[asset], func(i, j int) bool {
			left, _ := new(big.Int).SetString(result[asset][i].Summary.TotalAmount, 10)
			right, _ := new(big.Int).SetString(result[asset][j].Summary.TotalAmount, 10)
			if comparison := left.Cmp(right); comparison != 0 {
				return comparison > 0
			}
			return result[asset][i].Summary.To < result[asset][j].Summary.To
		})
	}
	return result
}

func conversionKey(from, to, asset string) string {
	return strings.ToLower(from + "|" + to + "|" + asset)
}

func mergeExistingSwapLegs(edges []Edge, conversion analyzedConversion) bool {
	if conversion.Evidence.Status != "complete" || conversion.Evidence.TxHash == "" || conversion.Evidence.Executor == "" || conversion.Evidence.Recipient == "" || conversion.Evidence.TokenOut == "" || conversion.Evidence.AmountOut == "" {
		return false
	}
	currentLeg := -1
	outputLeg := -1
	for index := range edges {
		edge := &edges[index]
		if !strings.EqualFold(edge.TxHash, conversion.Evidence.TxHash) {
			continue
		}
		if edgeMatchesTransfer(*edge, conversion.Transfer) {
			currentLeg = index
		}
		if strings.EqualFold(edge.From, conversion.Evidence.Executor) &&
			strings.EqualFold(edge.To, conversion.Evidence.Recipient) &&
			strings.EqualFold(edge.Asset, conversion.Evidence.TokenOut) &&
			edge.TotalAmount == conversion.Evidence.AmountOut {
			outputLeg = index
		}
	}
	if currentLeg < 0 || outputLeg < 0 {
		return false
	}
	edges[outputLeg].Kind = "swap"
	edges[outputLeg].Protocol = conversion.Evidence.Protocol
	edges[outputLeg].ConversionEvidence = appendConversionEvidence(edges[outputLeg].ConversionEvidence, conversion.Evidence)
	return true
}

func edgeMatchesTransfer(edge Edge, transfer store.Transfer) bool {
	return strings.EqualFold(edge.TxHash, transfer.TxHash) &&
		strings.EqualFold(edge.From, transfer.From) &&
		strings.EqualFold(edge.To, transfer.To) &&
		strings.EqualFold(edge.Asset, transfer.Asset) &&
		edge.TotalAmount == transferAmount(transfer)
}

func appendConversionEvidence(current []ConversionEvidence, evidence ConversionEvidence) []ConversionEvidence {
	for _, item := range current {
		if strings.EqualFold(item.TxHash, evidence.TxHash) && strings.EqualFold(item.Protocol, evidence.Protocol) && strings.EqualFold(item.Executor, evidence.Executor) {
			return current
		}
	}
	return append(current, evidence)
}

func branchStateKey(state branchState) string {
	return strings.Join([]string{strings.ToLower(state.Address), state.Direction, state.AssetMode, strings.ToLower(state.Asset), fmt.Sprint(state.AnchorBlock), strings.ToLower(state.EnteringTx.TxHash)}, "|")
}

func transferAsset(transfer store.Transfer) (string, string) {
	if transfer.AssetType == "eth" || strings.EqualFold(transfer.Asset, "ETH") {
		return "eth", "ETH"
	}
	return "contract", strings.ToLower(transfer.Asset)
}

func transferAmount(transfer store.Transfer) string {
	if transfer.AssetType == "eth" || strings.EqualFold(transfer.Asset, "ETH") {
		return transfer.Amount
	}
	return transfer.TokenValue
}

func (g *Graph) conversionTransfer(ctx context.Context, chain string, state branchState, txHash string) (analyzedConversion, bool, error) {
	analysis, err := g.analyzer.Analyze(ctx, chain, txHash)
	if err != nil {
		return analyzedConversion{}, false, fmt.Errorf("analyze contract conversion: %w", err)
	}
	if !analysis.Succeeded || analysis.Quality.Status != "complete" || analysis.Quality.AmbiguousRoute || !strings.EqualFold(analysis.TxHash, txHash) {
		return analyzedConversion{}, false, nil
	}
	if !strings.EqualFold(analysis.To, state.Address) && !strings.EqualFold(analysis.EntryContract, state.Address) && !conversionUsesExecutor(analysis.Conversions, state.Address) {
		return analyzedConversion{}, false, nil
	}
	if transfer, ok := thorchainProtocolTransfer(analysis, state); ok {
		vaultAddress := ""
		if analysis.ProtocolAction == "vault_migration" {
			vaultAddress = analysis.ProtocolDestination
			if state.Direction == "in" {
				vaultAddress = analysis.From
			}
		}
		return analyzedConversion{Transfer: transfer, Protocol: "thorchain", ProtocolAction: analysis.ProtocolAction, ProtocolMemo: analysis.ProtocolMemo, VaultAddress: vaultAddress}, true, nil
	}
	for _, conversion := range analysis.Conversions {
		transfer, ok := transferFromConversion(analysis, state, conversion)
		if !ok {
			continue
		}
		return analyzedConversion{Transfer: transfer, Evidence: ConversionEvidence{TxHash: analysis.TxHash, Protocol: conversion.Protocol, Version: conversion.Version, Status: conversion.Status, Initiator: conversion.Initiator, Router: conversion.Router, Executor: conversion.Executor, LiquidityProvider: conversion.LiquidityProvider, Recipient: conversion.Recipient, TokenIn: conversion.TokenIn, AmountIn: conversion.AmountIn, TokenOut: conversion.TokenOut, AmountOut: conversion.AmountOut, Evidence: conversion.Evidence}}, true, nil
	}
	transfer, ok, err := legacyConversionTransfer(analysis, state)
	return analyzedConversion{Transfer: transfer, Evidence: ConversionEvidence{TxHash: analysis.TxHash, Protocol: "uniswap", Version: "v3", Status: "complete", TokenIn: state.Asset, TokenOut: transfer.Asset, Evidence: []string{"verified Uniswap V3 pool logs"}}}, ok, err
}

func (g *Graph) appendVerifiedCrossChainEndpoints(ctx context.Context, chain string, state branchState, identity store.AddressIdentity, summary store.CounterpartySummary, depth int, path []string, result *Result) (bool, error) {
	if state.Direction != "out" || chain != "ethereum" || g.analyzer == nil || len(g.crossChainVerifiers) == 0 || summary.AssetType != "eth" {
		return false, nil
	}
	// New sync rows carry calldata. Legacy rows are lazily re-read for known
	// cross-chain endpoints because the analyzer can recover calldata by hash.
	if len(strings.TrimSpace(summary.Representative.Input)) <= 2 && !isCrossChainEndpoint(identity) && !isTHORChainVault(identity) {
		return false, nil
	}
	transfers := []store.Transfer{summary.Representative}
	if summary.TransferCount > 1 {
		var err error
		transfers, err = g.repository.QueryTransfers(ctx, store.TransferQuery{Chain: chain, Addresses: []string{state.Address}, Direction: "out", AssetMode: "eth", FromBlock: summary.EarliestBlock, ToBlock: summary.LatestBlock, Limit: traceTransferRecordCap})
		if err != nil {
			return false, fmt.Errorf("list possible THORChain inbounds: %w", err)
		}
	}
	resolved := false
	for _, transfer := range transfers {
		if !strings.EqualFold(transfer.From, summary.From) || !strings.EqualFold(transfer.To, summary.To) {
			continue
		}
		ok, err := g.appendVerifiedCrossChainEndpoint(ctx, chain, transfer, identity.Protocol, depth, path, result, summary.TransferCount == 1)
		if err != nil {
			return false, err
		}
		resolved = resolved || ok
	}
	return resolved, nil
}

func (g *Graph) appendVerifiedCrossChainEndpoint(ctx context.Context, chain string, transfer store.Transfer, protocol string, depth int, path []string, result *Result, annotateInbound bool) (bool, error) {
	analysis, err := g.analyzer.Analyze(ctx, chain, transfer.TxHash)
	if err != nil {
		return false, fmt.Errorf("analyze possible THORChain inbound: %w", err)
	}
	if !strings.EqualFold(analysis.Chain, chain) || !strings.EqualFold(analysis.TxHash, transfer.TxHash) || analysis.ProtocolAction != "router_inbound" || analysis.ProtocolMemo == "" || analysis.ProtocolDestination == "" || !analysis.Succeeded || !strings.EqualFold(analysis.From, transfer.From) || !strings.EqualFold(analysis.To, transfer.To) || analysis.Value != transfer.Amount {
		return false, nil
	}
	var outbound VerifiedCrossChainTransfer
	var ok bool
	for _, verifier := range g.crossChainVerifiers {
		outbound, ok, err = verifier.Verify(ctx, analysis)
		if err != nil || ok {
			break
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		result.DataStatus = "partial"
		if annotateInbound && len(result.Edges) > 0 {
			inbound := &result.Edges[len(result.Edges)-1]
			inbound.Protocol = protocol
			inbound.ProtocolAction = "router_inbound"
			inbound.ProtocolMemo = analysis.ProtocolMemo
			inbound.ConversionStatus = "partial"
			inbound.ConversionScanned = 1
		}
		return false, nil
	}
	if !ok || outbound.SourceChain != "ethereum" || outbound.TargetChain != "bitcoin" || !strings.EqualFold(outbound.From, transfer.To) || !strings.EqualFold(outbound.To, analysis.ProtocolDestination) || !strings.EqualFold(outbound.Asset, "BTC") || outbound.Amount == "" || outbound.TxHash == "" {
		return false, nil
	}
	if outbound.Protocol != "" {
		protocol = outbound.Protocol
	}
	if protocol == "" {
		protocol = "thorchain"
	}
	if annotateInbound && len(result.Edges) > 0 {
		inbound := &result.Edges[len(result.Edges)-1]
		inbound.Protocol = protocol
		inbound.ProtocolAction = "router_inbound"
		inbound.ProtocolMemo = analysis.ProtocolMemo
		inbound.ConversionStatus = "complete"
		inbound.ConversionScanned = 1
	}
	target := strings.ToLower(outbound.To)
	setVerifiedCrossChainSource(result.Nodes, analysis.To)
	crossChainPath := append(append([]string(nil), path...), target)
	result.Edges = append(result.Edges, Edge{
		Chain: "bitcoin", SourceChain: "ethereum", TargetChain: "bitcoin", From: strings.ToLower(outbound.From), To: target,
		AssetType: "native", Asset: "BTC", Symbol: "BTC", Decimals: 8, TotalAmount: outbound.Amount, TransferCount: 1,
		Kind: protocol + "_cross_chain_outbound", Depth: depth + 1, Path: crossChainPath, TxHash: strings.ToLower(outbound.TxHash),
		SourceTxHash: strings.ToLower(transfer.TxHash), SourceAmount: transfer.Amount, SourceAsset: "ETH",
		FirstBlock: outbound.BlockNumber, LatestBlock: outbound.BlockNumber, Protocol: protocol, ProtocolAction: "cross_chain_swap", ProtocolMemo: analysis.ProtocolMemo,
	})
	for _, node := range result.Nodes {
		if node.Chain == "bitcoin" && strings.EqualFold(node.Address, target) {
			return true, nil
		}
	}
	result.Nodes = append(result.Nodes, Node{Chain: "bitcoin", Address: target, Depth: depth + 1, Terminal: true, AddressType: "unknown", Protocol: protocol, Roles: []string{"cross_chain_recipient"}, StopReason: "verified_cross_chain_endpoint"})
	result.Paths = append(result.Paths, crossChainPath)
	return true, nil
}

func setVerifiedCrossChainSource(nodes []Node, address string) {
	for index := range nodes {
		if strings.EqualFold(nodes[index].Chain, "ethereum") && strings.EqualFold(nodes[index].Address, address) {
			nodes[index].Terminal = true
			nodes[index].StopReason = "cross_chain_bridge"
		}
	}
}

func (g *Graph) markTHORChainVault(ctx context.Context, chain, address string, nodes []Node, states []branchState) (store.AddressIdentity, error) {
	metadata, _, err := g.repository.FindAddress(ctx, chain, address)
	if err != nil {
		return store.AddressIdentity{}, err
	}
	identity := addressIdentity(metadata)
	identity.Protocol = "thorchain"
	if !containsRole(identity.Roles, "thorchain_vault") {
		identity.Roles = append(identity.Roles, "thorchain_vault")
	}
	if err := g.repository.SetAddressIdentity(ctx, chain, address, identity); err != nil {
		return store.AddressIdentity{}, err
	}
	for index := range nodes {
		if nodes[index].Chain == chain && strings.EqualFold(nodes[index].Address, address) {
			nodes[index].Protocol = identity.Protocol
			nodes[index].Roles = identity.Roles
		}
	}
	for index := range states {
		if states[index].Address == address {
			states[index].Identity = identity
		}
	}
	return identity, nil
}

func containsRole(roles []string, role string) bool {
	for _, value := range roles {
		if value == role {
			return true
		}
	}
	return false
}

func isTHORChainVault(identity store.AddressIdentity) bool {
	return identity.Protocol == "thorchain" && containsRole(identity.Roles, "thorchain_vault")
}

func isCrossChainBridge(identity store.AddressIdentity) bool {
	return containsRole(identity.Roles, "cross_chain_bridge")
}

func isCrossChainEndpoint(identity store.AddressIdentity) bool {
	return isCrossChainBridge(identity) ||
		(identity.Protocol == "thorchain" && containsRole(identity.Roles, "router")) ||
		(identity.Protocol == "mayachain" && containsRole(identity.Roles, "router"))
}

func (g *Graph) annotateTHORChainRouterCall(ctx context.Context, chain string, identity store.AddressIdentity, transfer store.Transfer, edge *Edge) error {
	if chain != "ethereum" || identity.Protocol != "thorchain" || !containsRole(identity.Roles, "router") {
		return nil
	}
	edge.Protocol = "thorchain"
	edge.ProtocolAction = "router_inbound"
	if g.analyzer == nil || transfer.TxHash == "" {
		return nil
	}
	analysis, err := g.analyzer.Analyze(ctx, chain, transfer.TxHash)
	if err != nil {
		return fmt.Errorf("analyze THORChain router call: %w", err)
	}
	if !strings.EqualFold(analysis.To, edge.To) || !analysis.Succeeded || analysis.Quality.Status != "complete" || analysis.ProtocolMemo == "" {
		return nil
	}
	if analysis.ProtocolAction == "router_inbound" {
		if !strings.EqualFold(analysis.From, transfer.From) || analysis.Value != transfer.Amount {
			return nil
		}
		edge.ProtocolAction = analysis.ProtocolAction
		edge.ProtocolMemo = analysis.ProtocolMemo
		return nil
	}
	if !verifiedTHORChainTransferOutAnalysis(analysis) {
		return nil
	}
	edge.ProtocolAction = analysis.ProtocolAction
	edge.ProtocolMemo = analysis.ProtocolMemo
	return nil
}

func thorchainProtocolTransfer(analysis store.TransactionAnalysis, state branchState) (store.Transfer, bool) {
	kind, supported := map[string]string{
		"vault_migration":   "thorchain_vault_migration",
		"protocol_outbound": "thorchain_protocol_outbound",
		"cross_chain_swap":  "thorchain_cross_chain_swap",
		"refund":            "thorchain_refund",
	}[analysis.ProtocolAction]
	if !supported || analysis.ProtocolDestination == "" || analysis.ProtocolAmount == "" || !verifiedTHORChainTransferOutAnalysis(analysis) {
		return store.Transfer{}, false
	}
	if state.Direction == "in" && analysis.ProtocolAction != "vault_migration" {
		return store.Transfer{}, false
	}
	assetMode, asset := transferAsset(state.EnteringTx)
	protocolMode, protocolAsset := "contract", strings.ToLower(analysis.ProtocolAsset)
	if strings.EqualFold(analysis.ProtocolAsset, "ETH") {
		protocolMode, protocolAsset = "eth", "ETH"
	}
	if assetMode != protocolMode || !strings.EqualFold(asset, protocolAsset) || transferAmount(state.EnteringTx) != analysis.ProtocolAmount {
		return store.Transfer{}, false
	}
	if state.Direction == "in" {
		return semanticTransfer(analysis, analysis.From, state.Address, analysis.ProtocolAsset, analysis.ProtocolAmount, kind), true
	}
	return semanticTransfer(analysis, state.Address, analysis.ProtocolDestination, analysis.ProtocolAsset, analysis.ProtocolAmount, kind), true
}

func verifiedTHORChainTransferOutAnalysis(analysis store.TransactionAnalysis) bool {
	if !analysis.Succeeded || analysis.Quality.Status != "complete" || !isTraceableTHORChainAction(analysis.ProtocolAction) {
		return false
	}
	if strings.EqualFold(analysis.ProtocolAsset, "ETH") {
		for _, call := range analysis.InternalCalls {
			if !call.IsError && strings.EqualFold(call.From, analysis.To) && strings.EqualFold(call.To, analysis.ProtocolDestination) && call.Value == analysis.ProtocolAmount {
				return true
			}
		}
		return false
	}
	for _, transfer := range analysis.Transfers {
		if strings.EqualFold(transfer.Token, analysis.ProtocolAsset) && strings.EqualFold(transfer.From, analysis.To) && strings.EqualFold(transfer.To, analysis.ProtocolDestination) && transfer.Amount == analysis.ProtocolAmount {
			return true
		}
	}
	return false
}

func isTraceableTHORChainAction(action string) bool {
	switch action {
	case "vault_migration", "protocol_outbound", "cross_chain_swap", "refund":
		return true
	default:
		return false
	}
}

func conversionUsesExecutor(conversions []store.SwapConversion, address string) bool {
	for _, conversion := range conversions {
		if strings.EqualFold(conversion.Executor, address) {
			return true
		}
	}
	return false
}

func transferFromConversion(analysis store.TransactionAnalysis, state branchState, conversion store.SwapConversion) (store.Transfer, bool) {
	if conversion.Status != "complete" || conversion.TokenIn == "" || conversion.AmountIn == "" || conversion.TokenOut == "" || conversion.AmountOut == "" {
		return store.Transfer{}, false
	}
	if state.Direction == "out" {
		if state.AssetMode == "eth" {
			if !strings.EqualFold(conversion.TokenIn, "ETH") && (!strings.EqualFold(conversion.TokenIn, "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2") || !hasWrap(analysis.Wraps, "deposit")) {
				return store.Transfer{}, false
			}
		} else if !strings.EqualFold(conversion.TokenIn, state.Asset) {
			return store.Transfer{}, false
		}
		if conversion.Recipient == "" {
			return store.Transfer{}, false
		}
		return semanticTransfer(analysis, state.Address, conversion.Recipient, conversion.TokenOut, conversion.AmountOut, "swap"), true
	}
	if state.AssetMode == "eth" {
		if !strings.EqualFold(conversion.TokenOut, "ETH") && !strings.EqualFold(conversion.TokenOut, "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2") {
			return store.Transfer{}, false
		}
	} else if !strings.EqualFold(conversion.TokenOut, state.Asset) {
		return store.Transfer{}, false
	}
	if conversion.Initiator == "" {
		return store.Transfer{}, false
	}
	return semanticTransfer(analysis, conversion.Initiator, state.Address, conversion.TokenIn, conversion.AmountIn, "swap"), true
}

func legacyConversionTransfer(analysis store.TransactionAnalysis, state branchState) (store.Transfer, bool, error) {
	if len(analysis.Swaps) == 0 {
		return wrapConversion(analysis, state)
	}
	for _, swap := range analysis.Swaps {
		if !swap.Verified || swap.TokenIn == "" || swap.TokenOut == "" || swap.AmountIn == "" || swap.AmountOut == "" {
			return store.Transfer{}, false, nil
		}
	}
	first := analysis.Swaps[0]
	last := analysis.Swaps[len(analysis.Swaps)-1]
	var from, to, asset, amount string
	if state.Direction == "out" {
		if state.AssetMode == "eth" {
			if !strings.EqualFold(first.TokenIn, "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2") || !hasWrap(analysis.Wraps, "deposit") {
				return store.Transfer{}, false, nil
			}
		} else if !strings.EqualFold(first.TokenIn, state.Asset) {
			return store.Transfer{}, false, nil
		}
		from, to, asset, amount = state.Address, analysis.FinalOutputAddress, last.TokenOut, last.AmountOut
		if to == "" {
			to = last.OutputAddress
		}
	} else {
		if state.AssetMode == "eth" {
			if !strings.EqualFold(last.TokenOut, "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2") || !hasWrap(analysis.Wraps, "withdrawal") {
				return store.Transfer{}, false, nil
			}
		} else if !strings.EqualFold(last.TokenOut, state.Asset) {
			return store.Transfer{}, false, nil
		}
		from, to, asset, amount = analysis.From, state.Address, first.TokenIn, first.AmountIn
	}
	if from == "" || to == "" || asset == "" || amount == "" {
		return store.Transfer{}, false, nil
	}
	return semanticTransfer(analysis, from, to, asset, amount, "swap"), true, nil
}

func wrapConversion(analysis store.TransactionAnalysis, state branchState) (store.Transfer, bool, error) {
	if state.AssetMode != "eth" {
		return store.Transfer{}, false, nil
	}
	kind := "deposit"
	if state.Direction == "in" {
		kind = "withdrawal"
	}
	for _, wrap := range analysis.Wraps {
		if wrap.Type != kind || wrap.Account == "" || wrap.Amount == "" {
			continue
		}
		from, to, transferKind := state.Address, wrap.Account, "wrap"
		if state.Direction == "in" {
			from, to, transferKind = wrap.Account, state.Address, "unwrap"
		}
		return semanticTransfer(analysis, from, to, "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2", wrap.Amount, transferKind), true, nil
	}
	return store.Transfer{}, false, nil
}

func semanticTransfer(analysis store.TransactionAnalysis, from, to, asset, amount, kind string) store.Transfer {
	chainID := analysis.ChainID
	if chainID == 0 {
		chainID = 1
	}
	transfer := store.Transfer{
		Chain: analysis.Chain, ChainID: chainID, TxHash: analysis.TxHash, BlockNumber: analysis.BlockNumber,
		From: strings.ToLower(from), To: strings.ToLower(to), AssetType: "erc20", Asset: strings.ToLower(asset),
		TokenValue: amount, TransferKind: kind, TransactionGroup: fmt.Sprintf("%d:%s", chainID, strings.ToLower(analysis.TxHash)), Source: "transactionanalysis",
	}
	if strings.EqualFold(asset, "ETH") {
		transfer.AssetType, transfer.Asset, transfer.Amount, transfer.TokenValue = "eth", "ETH", amount, ""
	}
	return transfer
}

func hasWrap(wraps []store.WrapEvent, kind string) bool {
	for _, wrap := range wraps {
		if wrap.Type == kind {
			return true
		}
	}
	return false
}

func pathContains(path []string, address string) bool {
	for _, item := range path {
		if strings.EqualFold(item, address) {
			return true
		}
	}
	return false
}
func hasTerminalLabel(labels []store.Label) bool {
	for _, label := range labels {
		value := strings.ToLower(label.Type)
		if value == "exchange" || value == "exchange_hot_wallet" || value == "hot_wallet" {
			return true
		}
	}
	return false
}

func isKnownWalletTerminal(identity store.AddressIdentity) bool {
	return identity.Protocol == "woo_x" && containsRole(identity.Roles, "woo_x_wallet")
}

func nodeTerminal(nodes []Node, address string) bool {
	for _, node := range nodes {
		if strings.EqualFold(node.Address, address) {
			return node.Terminal
		}
	}
	return false
}

func setNodeTerminal(nodes []Node, address, reason string) {
	for i := range nodes {
		if strings.EqualFold(nodes[i].Address, address) {
			nodes[i].Terminal = true
			nodes[i].StopReason = reason
		}
	}
}

func normalize(q Query) (Query, error) {
	chain, chainErr := chains.Resolve(q.Chain)
	if chainErr != nil {
		return q, ErrInvalidQuery
	}
	q.Chain = chain.Name
	if q.Depth < 0 {
		return q, ErrInvalidQuery
	}
	q.Depth = 0
	d := strings.ToLower(q.Direction)
	if d == "" {
		d = "both"
	}
	if d != "in" && d != "out" && d != "both" {
		return q, ErrInvalidQuery
	}
	q.Direction = d
	if q.Asset != "" && !strings.EqualFold(q.Asset, "all") && !strings.EqualFold(q.Asset, "eth") && !strings.EqualFold(q.Asset, "erc20") {
		if _, err := ethaddr.Normalize(q.Asset); err != nil {
			return q, ErrInvalidQuery
		}
	}
	q.Asset = "ETH"
	return q, nil
}

const zeroAddress = "0x0000000000000000000000000000000000000000"
