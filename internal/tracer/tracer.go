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
	traceRuleVersion    = "trace-v5"
	conversionScanLimit = 20
)

type AddressNotSyncedError struct{ Chain, Address string }

func (e AddressNotSyncedError) Error() string { return ErrAddressNotSynced.Error() + ": " + e.Address }
func (e AddressNotSyncedError) Unwrap() error { return ErrAddressNotSynced }

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	QueryTransfers(context.Context, store.TransferQuery) ([]store.Transfer, error)
	TopCounterparties(context.Context, store.CounterpartyQuery) ([]store.CounterpartySummary, error)
	TopRelationshipTransfers(context.Context, store.CounterpartyQuery, int) ([]store.Transfer, error)
	ListLabels(context.Context, string, string) ([]store.Label, error)
	ListCrossChainLinks(context.Context, string, string, int64) ([]store.CrossChainLink, error)
}

type TransactionAnalyzer interface {
	Analyze(context.Context, string, string) (store.TransactionAnalysis, error)
	SupportsContract(string) bool
}

type Query struct {
	Chain, Address, Direction, Asset string
	Depth, TopN                      int
}
type Node struct {
	Chain    string `json:"chain"`
	Address  string `json:"address"`
	Depth    int    `json:"depth"`
	Terminal bool   `json:"terminal"`
}
type NodeRef struct {
	Chain   string `json:"chain"`
	Address string `json:"address"`
}
type BridgeEdge struct {
	Link  store.CrossChainLink `json:"link"`
	Depth int                  `json:"depth"`
	Path  []NodeRef            `json:"path"`
}
type Edge struct {
	Chain                 string    `bson:"chain" json:"chain"`
	From                  string    `bson:"from" json:"from"`
	To                    string    `bson:"to" json:"to"`
	AssetType             string    `bson:"assetType" json:"assetType"`
	Asset                 string    `bson:"asset" json:"asset"`
	Symbol                string    `bson:"symbol,omitempty" json:"symbol,omitempty"`
	Decimals              int32     `bson:"decimals" json:"decimals"`
	TokenMetadataComplete bool      `bson:"tokenMetadataComplete,omitempty" json:"tokenMetadataComplete"`
	TotalAmount           string    `bson:"totalAmount" json:"totalAmount"`
	TransferCount         int64     `bson:"transferCount" json:"transferCount"`
	Kind                  string    `bson:"kind" json:"kind"`
	Depth                 int       `bson:"depth" json:"depth"`
	Path                  []string  `bson:"path" json:"path"`
	FirstBlock            int64     `bson:"firstBlock,omitempty" json:"firstBlock,omitempty"`
	FirstTime             time.Time `bson:"firstTime,omitempty" json:"firstTime,omitempty"`
	LatestBlock           int64     `bson:"latestBlock,omitempty" json:"latestBlock,omitempty"`
	LatestTime            time.Time `bson:"latestTime,omitempty" json:"latestTime,omitempty"`
	ConversionStatus      string    `bson:"conversionStatus,omitempty" json:"conversionStatus,omitempty"`
	ConversionScanned     int       `bson:"conversionScanned,omitempty" json:"conversionScanned,omitempty"`
}
type Result struct {
	Nodes             []Node               `bson:"nodes" json:"nodes"`
	Edges             []Edge               `bson:"edges" json:"edges"`
	BridgeEdges       []BridgeEdge         `bson:"bridgeEdges,omitempty" json:"bridgeEdges,omitempty"`
	CrossChainPaths   [][]NodeRef          `bson:"crossChainPaths,omitempty" json:"crossChainPaths,omitempty"`
	Paths             [][]string           `bson:"paths,omitempty" json:"paths,omitempty"`
	DataThroughBlock  int64                `bson:"dataThroughBlock" json:"dataThroughBlock"`
	DataThroughBlocks map[string]int64     `bson:"dataThroughBlocks,omitempty" json:"dataThroughBlocks,omitempty"`
	DataStatus        string               `bson:"dataStatus" json:"dataStatus"`
	Labels            []risk.InferredLabel `bson:"labels,omitempty" json:"labels,omitempty"`
	Risk              risk.Result          `bson:"risk" json:"risk"`
	RuleVersion       string               `bson:"ruleVersion" json:"ruleVersion"`
	branchStates      []branchState
}

type branchState struct {
	Address       string
	Direction     string
	AssetMode     string
	Asset         string
	EnteringQuery store.CounterpartyQuery
	EnteringEdge  int
	Path          []string
}
type bridgeBranchState struct {
	Node      Node
	Direction string
	Path      []NodeRef
}

type Graph struct {
	repository Repository
	analyzer   TransactionAnalyzer
}

func New(repository Repository) *Graph { return &Graph{repository: repository} }

func (g *Graph) WithTransactionAnalyzer(analyzer TransactionAnalyzer) *Graph {
	g.analyzer = analyzer
	return g
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
	return g.traceWithBridges(ctx, query)
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
	if !found || metadata.SyncStatus != "synced" {
		return Result{}, AddressNotSyncedError{Chain: q.Chain, Address: seed}
	}
	result := Result{DataThroughBlock: metadata.LatestSyncedBlock, DataStatus: "synced", RuleVersion: traceRuleVersion}
	seedLabels, err := g.repository.ListLabels(ctx, q.Chain, seed)
	if err != nil {
		return Result{}, err
	}
	directions := []string{q.Direction}
	if q.Direction == "both" {
		directions = []string{"in", "out"}
	}
	assets := rootAssets(q.Chain)
	frontier := make([]branchState, 0, len(directions)*len(assets))
	visitedStates := make(map[string]bool, len(directions)*len(assets))
	visitedNodes := map[string]bool{seed: true}
	seedTerminal := metadata.IsTerminal || hasTerminalLabel(seedLabels)
	result.Nodes = append(result.Nodes, Node{Chain: q.Chain, Address: seed, Depth: 0, Terminal: seedTerminal})
	for _, direction := range directions {
		for _, asset := range assets {
			state := branchState{Address: seed, Direction: direction, AssetMode: asset.AssetMode, Asset: asset.Asset, Path: []string{seed}}
			frontier = append(frontier, state)
			visitedStates[branchStateKey(state)] = true
			result.branchStates = append(result.branchStates, state)
		}
	}
	for depth := 0; depth < q.Depth && len(frontier) > 0 && len(visitedNodes) < 5000; depth++ {
		sort.Slice(frontier, func(i, j int) bool {
			return branchStateKey(frontier[i]) < branchStateKey(frontier[j])
		})
		next := make([]branchState, 0)
		expand := func(state branchState, candidates []store.CounterpartySummary) error {
			for _, summary := range candidates {
				if token, ok := knownTokenFor(q.Chain, summary.Asset); ok {
					summary.AssetType = "erc20"
					summary.Asset = token.Contract
					summary.Symbol = token.Symbol
					summary.Decimals = token.Decimals
					summary.TokenMetadataComplete = true
				}
				other := strings.ToLower(summary.To)
				if state.Direction == "in" {
					other = strings.ToLower(summary.From)
				}
				if other == "" {
					continue
				}
				path := append(append([]string(nil), state.Path...), other)
				edge := edgeFromSummary(summary, depth+1, path)
				result.Edges = append(result.Edges, edge)
				edgeIndex := len(result.Edges) - 1
				if other == zeroAddress {
					if !visitedNodes[other] && len(visitedNodes) < 5000 {
						visitedNodes[other] = true
						result.Nodes = append(result.Nodes, Node{Chain: q.Chain, Address: other, Depth: depth + 1, Terminal: true})
					}
					continue
				}
				otherMetadata, otherFound, metadataErr := g.repository.FindAddress(ctx, q.Chain, other)
				if metadataErr != nil {
					return metadataErr
				}
				if !otherFound || otherMetadata.SyncStatus != "synced" {
					return AddressNotSyncedError{Chain: q.Chain, Address: other}
				}
				assetMode, asset := summaryAsset(summary)
				relation := store.CounterpartyQuery{Chain: q.Chain, Address: state.Address, Counterparty: other, Direction: state.Direction, AssetMode: assetMode, Asset: asset}
				nextState := branchState{Address: other, Direction: state.Direction, AssetMode: assetMode, Asset: asset, EnteringQuery: relation, EnteringEdge: edgeIndex, Path: path}
				stateKey := branchStateKey(nextState)
				if visitedStates[stateKey] || len(visitedNodes) >= 5000 {
					continue
				}
				otherLabels, labelsErr := g.repository.ListLabels(ctx, q.Chain, other)
				if labelsErr != nil {
					return labelsErr
				}
				terminal := otherMetadata.IsTerminal || hasTerminalLabel(otherLabels)
				visitedStates[stateKey] = true
				result.branchStates = append(result.branchStates, nextState)
				isNewNode := !visitedNodes[other]
				visitedNodes[other] = true
				if !terminal {
					next = append(next, nextState)
				}
				if isNewNode {
					result.Nodes = append(result.Nodes, Node{Chain: q.Chain, Address: other, Depth: depth + 1, Terminal: terminal})
				}
				result.Paths = append(result.Paths, path)
			}
			return nil
		}
		for _, state := range frontier {
			if nodeTerminal(result.Nodes, state.Address) {
				continue
			}
			if state.EnteringQuery.Address != "" && g.analyzer != nil && g.analyzer.SupportsContract(state.Address) && q.Chain == "ethereum" {
				transfers, queryErr := g.repository.TopRelationshipTransfers(ctx, state.EnteringQuery, conversionScanLimit+1)
				if queryErr != nil {
					return Result{}, queryErr
				}
				scanned := min(len(transfers), conversionScanLimit)
				if state.EnteringEdge >= 0 && state.EnteringEdge < len(result.Edges) {
					result.Edges[state.EnteringEdge].ConversionScanned = scanned
					result.Edges[state.EnteringEdge].ConversionStatus = "complete"
					if len(transfers) > conversionScanLimit {
						result.Edges[state.EnteringEdge].ConversionStatus = "partial"
					}
				}
				converted := make([]store.Transfer, 0, scanned)
				for _, transfer := range transfers[:scanned] {
					conversionState := state
					conversionState.EnteringQuery = store.CounterpartyQuery{}
					conversionState.AssetMode, conversionState.Asset = transferAsset(transfer)
					conversion, ok, conversionErr := g.conversionTransfer(ctx, q.Chain, conversionState, transfer.TxHash)
					if conversionErr != nil {
						return Result{}, conversionErr
					}
					if ok {
						converted = append(converted, conversion)
					}
				}
				if len(converted) > 0 {
					aggregates := aggregateConversions(converted, q.TopN)
					assets := make([]string, 0, len(aggregates))
					for asset := range aggregates {
						assets = append(assets, asset)
					}
					sort.Strings(assets)
					for _, asset := range assets {
						summaries := aggregates[asset]
						if err := expand(state, summaries); err != nil {
							return Result{}, err
						}
					}
				}
				continue
			}
			summaries, queryErr := g.repository.TopCounterparties(ctx, store.CounterpartyQuery{Chain: q.Chain, Address: state.Address, Direction: state.Direction, AssetMode: state.AssetMode, Asset: state.Asset, TopN: q.TopN})
			if queryErr != nil {
				return Result{}, queryErr
			}
			if err := expand(state, summaries); err != nil {
				return Result{}, err
			}
		}
		frontier = next
	}
	result.Risk = risk.Result{Level: "no_conclusion", RuleVersion: risk.RiskVersion, PropagationVersion: risk.PropagationVersion}
	return result, nil
}

func edgeFromSummary(summary store.CounterpartySummary, depth int, path []string) Edge {
	kind := summary.Representative.TransferKind
	if kind == "" {
		kind = "transfer"
	}
	return Edge{Chain: summary.Chain, From: summary.From, To: summary.To, AssetType: summary.AssetType, Asset: summary.Asset, Symbol: summary.Symbol, Decimals: summary.Decimals, TokenMetadataComplete: summary.TokenMetadataComplete, TotalAmount: summary.TotalAmount, TransferCount: summary.TransferCount, Kind: kind, Depth: depth, Path: path, FirstBlock: summary.EarliestBlock, FirstTime: summary.EarliestTime, LatestBlock: summary.LatestBlock, LatestTime: summary.LatestTime}
}

func summaryAsset(summary store.CounterpartySummary) (string, string) {
	if summary.AssetType == "eth" || strings.EqualFold(summary.Asset, "ETH") {
		return "eth", "ETH"
	}
	return "contract", strings.ToLower(summary.Asset)
}

func aggregateConversions(transfers []store.Transfer, topN int) map[string][]store.CounterpartySummary {
	byAsset := make(map[string]map[string]*store.CounterpartySummary)
	for _, transfer := range transfers {
		assetKey := strings.ToLower(transfer.Asset)
		if byAsset[assetKey] == nil {
			byAsset[assetKey] = make(map[string]*store.CounterpartySummary)
		}
		key := strings.ToLower(transfer.From + "|" + transfer.To)
		summary := byAsset[assetKey][key]
		if summary == nil {
			summary = &store.CounterpartySummary{Chain: transfer.Chain, ChainID: transfer.ChainID, From: transfer.From, To: transfer.To, AssetType: transfer.AssetType, Asset: transfer.Asset, Symbol: transfer.Symbol, Decimals: transfer.Decimals, TokenMetadataComplete: transfer.TokenMetadataComplete, TotalAmount: "0", EarliestBlock: transfer.BlockNumber, EarliestTime: transfer.BlockTime, LatestBlock: transfer.BlockNumber, LatestTime: transfer.BlockTime, LatestTransfer: transfer, Representative: transfer}
			byAsset[assetKey][key] = summary
		}
		amount, ok := new(big.Int).SetString(transferAmount(transfer), 10)
		if !ok {
			continue
		}
		total, _ := new(big.Int).SetString(summary.TotalAmount, 10)
		summary.TotalAmount = total.Add(total, amount).String()
		summary.TransferCount++
		if transfer.BlockNumber < summary.EarliestBlock {
			summary.EarliestBlock, summary.EarliestTime = transfer.BlockNumber, transfer.BlockTime
		}
		if transfer.BlockNumber > summary.LatestBlock {
			summary.LatestBlock, summary.LatestTime, summary.LatestTransfer = transfer.BlockNumber, transfer.BlockTime, transfer
		}
	}
	result := make(map[string][]store.CounterpartySummary, len(byAsset))
	for asset, values := range byAsset {
		for _, summary := range values {
			result[asset] = append(result[asset], *summary)
		}
		sort.Slice(result[asset], func(i, j int) bool {
			left, _ := new(big.Int).SetString(result[asset][i].TotalAmount, 10)
			right, _ := new(big.Int).SetString(result[asset][j].TotalAmount, 10)
			if comparison := left.Cmp(right); comparison != 0 {
				return comparison > 0
			}
			return result[asset][i].To < result[asset][j].To
		})
		if len(result[asset]) > topN {
			result[asset] = result[asset][:topN]
		}
	}
	return result
}

func branchStateKey(state branchState) string {
	return strings.Join([]string{strings.ToLower(state.Address), state.Direction, state.AssetMode, strings.ToLower(state.Asset)}, "|")
}

func transferAsset(transfer store.Transfer) (string, string) {
	if transfer.AssetType == "eth" || strings.EqualFold(transfer.Asset, "ETH") {
		return "eth", "ETH"
	}
	return "contract", strings.ToLower(transfer.Asset)
}

func compareTransferAmount(left, right store.Transfer) int {
	leftAmount, leftOK := new(big.Int).SetString(transferAmount(left), 10)
	rightAmount, rightOK := new(big.Int).SetString(transferAmount(right), 10)
	if !leftOK || !rightOK {
		return 0
	}
	return leftAmount.Cmp(rightAmount)
}

func transferAmount(transfer store.Transfer) string {
	if transfer.AssetType == "eth" || strings.EqualFold(transfer.Asset, "ETH") {
		return transfer.Amount
	}
	return transfer.TokenValue
}

func (g *Graph) conversionTransfer(ctx context.Context, chain string, state branchState, txHash string) (store.Transfer, bool, error) {
	analysis, err := g.analyzer.Analyze(ctx, chain, txHash)
	if err != nil {
		return store.Transfer{}, false, fmt.Errorf("analyze contract conversion: %w", err)
	}
	if !analysis.Succeeded || analysis.Quality.Status != "complete" || analysis.Quality.AmbiguousRoute || !strings.EqualFold(analysis.TxHash, txHash) {
		return store.Transfer{}, false, nil
	}
	if !strings.EqualFold(analysis.To, state.Address) && !strings.EqualFold(analysis.EntryContract, state.Address) {
		return store.Transfer{}, false, nil
	}
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
	return store.Transfer{
		Chain: analysis.Chain, ChainID: chainID, TxHash: analysis.TxHash, BlockNumber: analysis.BlockNumber,
		From: strings.ToLower(from), To: strings.ToLower(to), AssetType: "erc20", Asset: strings.ToLower(asset),
		TokenValue: amount, TransferKind: kind, TransactionGroup: fmt.Sprintf("%d:%s", chainID, strings.ToLower(analysis.TxHash)), Source: "transactionanalysis",
	}
}

func hasWrap(wraps []store.WrapEvent, kind string) bool {
	for _, wrap := range wraps {
		if wrap.Type == kind {
			return true
		}
	}
	return false
}

func (g *Graph) traceWithBridges(ctx context.Context, query Query) (Result, error) {
	q, err := normalize(query)
	if err != nil {
		return Result{}, err
	}
	result, err := g.traceSameChain(ctx, q)
	if err != nil {
		return Result{}, err
	}
	result.RuleVersion = traceRuleVersion
	result.DataThroughBlocks = map[string]int64{q.Chain: result.DataThroughBlock}
	visited := make(map[string]bool)
	seedRef := NodeRef{Chain: q.Chain, Address: strings.ToLower(q.Address)}
	nodeByAddress := make(map[string]Node, len(result.Nodes))
	for _, node := range result.Nodes {
		visited[nodeKey(node.Chain, node.Address)] = true
		nodeByAddress[nodeKey(node.Chain, node.Address)] = node
	}
	queue := make([]bridgeBranchState, 0, len(result.branchStates))
	for _, branch := range result.branchStates {
		refs := make([]NodeRef, len(branch.Path))
		for i, address := range branch.Path {
			refs[i] = NodeRef{Chain: q.Chain, Address: address}
		}
		if len(refs) > 0 {
			endpoint := nodeByAddress[nodeRefKey(refs[len(refs)-1])]
			queue = append(queue, bridgeBranchState{Node: endpoint, Direction: branch.Direction, Path: refs})
		}
	}
	if len(queue) == 0 {
		queue = append(queue, bridgeBranchState{Node: result.Nodes[0], Direction: q.Direction, Path: []NodeRef{seedRef}})
	}
	seenQueueStates := make(map[string]bool)
	seenTargetStates := make(map[string]bool)
	bridgeSeen := make(map[string]bool)
	factSeen := make(map[string]bool)
	for _, edge := range result.Edges {
		factSeen[aggregateEdgeKey(edge)] = true
	}
	for index := 0; index < len(queue) && len(visited) < 5000; index++ {
		state := queue[index]
		node := state.Node
		queueKey := nodeKey(node.Chain, node.Address) + "|" + state.Direction
		if seenQueueStates[queueKey] {
			continue
		}
		seenQueueStates[queueKey] = true
		if node.Terminal || node.Depth >= q.Depth {
			continue
		}
		links, linkErr := g.repository.ListCrossChainLinks(ctx, node.Chain, node.Address, 500)
		if linkErr != nil {
			return Result{}, linkErr
		}
		for _, link := range links {
			other, ok := bridgeTarget(link, node, state.Direction)
			if !ok {
				continue
			}
			bridgeID := bridgeKey(link)
			if bridgeSeen[bridgeID] {
				continue
			}
			bridgeSeen[bridgeID] = true
			basePath := state.Path
			if len(basePath) == 0 {
				basePath = []NodeRef{{Chain: node.Chain, Address: node.Address}}
			}
			bridgePath := append(append([]NodeRef(nil), basePath...), other)
			bridgeDepth := node.Depth + 1
			result.BridgeEdges = append(result.BridgeEdges, BridgeEdge{Link: link, Depth: bridgeDepth, Path: bridgePath})
			result.CrossChainPaths = append(result.CrossChainPaths, bridgePath)
			otherKey := nodeRefKey(other)
			targetStateKey := otherKey + "|" + state.Direction
			if seenTargetStates[targetStateKey] || len(visited) >= 5000 {
				continue
			}
			seenTargetStates[targetStateKey] = true
			remaining := q.Depth - bridgeDepth
			if remaining == 0 {
				metadata, found, metadataErr := g.repository.FindAddress(ctx, other.Chain, other.Address)
				if metadataErr != nil {
					return Result{}, metadataErr
				}
				if !found || metadata.SyncStatus != "synced" {
					return Result{}, AddressNotSyncedError(other)
				}
				labels, labelsErr := g.repository.ListLabels(ctx, other.Chain, other.Address)
				if labelsErr != nil {
					return Result{}, labelsErr
				}
				targetNode := Node{Chain: other.Chain, Address: other.Address, Depth: bridgeDepth, Terminal: metadata.IsTerminal || hasTerminalLabel(labels)}
				if !visited[otherKey] {
					visited[otherKey] = true
					result.Nodes = append(result.Nodes, targetNode)
				}
				queue = append(queue, bridgeBranchState{Node: targetNode, Direction: state.Direction, Path: bridgePath})
				result.DataThroughBlocks[other.Chain] = max(result.DataThroughBlocks[other.Chain], metadata.LatestSyncedBlock)
				continue
			}
			subQuery := q
			subQuery.Chain, subQuery.Address, subQuery.Depth = other.Chain, other.Address, max(1, remaining)
			subQuery.Direction = state.Direction
			sub, subErr := g.traceSameChain(ctx, subQuery)
			if subErr != nil {
				return Result{}, subErr
			}
			result.DataThroughBlocks[other.Chain] = max(result.DataThroughBlocks[other.Chain], sub.DataThroughBlock)
			subPaths := map[string][]NodeRef{otherKey: bridgePath}
			for _, subPath := range sub.Paths {
				if len(subPath) == 0 {
					continue
				}
				refs := append([]NodeRef(nil), bridgePath...)
				for _, address := range subPath[1:] {
					refs = append(refs, NodeRef{Chain: other.Chain, Address: address})
				}
				subPaths[nodeKey(other.Chain, subPath[len(subPath)-1])] = refs
			}
			for _, subNode := range sub.Nodes {
				key := nodeKey(subNode.Chain, subNode.Address)
				subNode.Depth += bridgeDepth
				if !visited[key] && len(visited) < 5000 {
					visited[key] = true
					result.Nodes = append(result.Nodes, subNode)
				}
				queue = append(queue, bridgeBranchState{Node: subNode, Direction: state.Direction, Path: subPaths[key]})
			}
			for _, edge := range sub.Edges {
				if edge.Depth+bridgeDepth > q.Depth || factSeen[aggregateEdgeKey(edge)] {
					continue
				}
				edge.Depth += bridgeDepth
				edge.Path = append(addresses(basePath), edge.Path[1:]...)
				result.Edges = append(result.Edges, edge)
				factSeen[aggregateEdgeKey(edge)] = true
			}
		}
	}
	return result, nil
}

func bridgeTarget(link store.CrossChainLink, node Node, direction string) (NodeRef, bool) {
	if direction != "in" && link.SourceChain == node.Chain && strings.EqualFold(link.SourceAddress, node.Address) {
		return NodeRef{Chain: link.TargetChain, Address: link.TargetAddress}, true
	}
	if direction != "out" && link.TargetChain == node.Chain && strings.EqualFold(link.TargetAddress, node.Address) {
		return NodeRef{Chain: link.SourceChain, Address: link.SourceAddress}, true
	}
	return NodeRef{}, false
}

func nodeKey(chain, address string) string { return strings.ToLower(chain + ":" + address) }
func nodeRefKey(node NodeRef) string       { return nodeKey(node.Chain, node.Address) }
func bridgeKey(link store.CrossChainLink) string {
	return strings.Join([]string{link.SourceChain, link.SourceTxHash, fmt.Sprint(link.SourceLogIndex), link.TargetChain, link.TargetTxHash, fmt.Sprint(link.TargetLogIndex)}, "|")
}
func addresses(path []NodeRef) []string {
	result := make([]string, len(path))
	for i := range path {
		result[i] = path[i].Address
	}
	return result
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

func nodeTerminal(nodes []Node, address string) bool {
	for _, node := range nodes {
		if strings.EqualFold(node.Address, address) {
			return node.Terminal
		}
	}
	return false
}

func normalize(q Query) (Query, error) {
	chain, chainErr := chains.Resolve(q.Chain)
	if chainErr != nil {
		return q, ErrInvalidQuery
	}
	q.Chain = chain.Name
	if q.Depth == 0 {
		q.Depth = 3
	}
	if q.TopN == 0 {
		q.TopN = 10
	}
	if q.Depth < 1 || q.Depth > 5 || q.TopN < 1 || q.TopN > 20 {
		return q, ErrInvalidQuery
	}
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
func aggregateEdgeKey(edge Edge) string {
	return strings.ToLower(strings.Join([]string{edge.Chain, edge.From, edge.To, edge.Asset, fmt.Sprint(edge.Depth)}, "|"))
}

const zeroAddress = "0x0000000000000000000000000000000000000000"
