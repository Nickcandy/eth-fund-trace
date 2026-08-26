package propagation

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	Version         = "propagation-v4"
	RiskRuleVersion = "risk-association-v2"
)

type Config struct {
	MaxHops, MaxNodes, MaxEdges, PerNodeCandidateCap, MaxPathsPerTarget int
	PerChannelLimit, MaxAssetChannels                                   int
}

func DefaultConfig() Config {
	return Config{MaxHops: 3, MaxNodes: 10000, MaxEdges: 50000, PerNodeCandidateCap: 50, MaxPathsPerTarget: 3, PerChannelLimit: 20, MaxAssetChannels: 100}
}

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	ListLabels(context.Context, string, string) ([]store.Label, error)
	ListRiskLabels(context.Context, string, int64) ([]store.Label, error)
	ListTransferAssets(context.Context, string, string, string, int64, int) (store.AssetChannelResult, error)
	PropagationCandidates(context.Context, store.CandidateQuery) (store.CandidateResult, error)
	ListCrossChainLinks(context.Context, string, string, int64) ([]store.CrossChainLink, error)
	FindTransactionAnalysis(context.Context, string, string) (store.TransactionAnalysis, bool, error)
}

type Coverage struct {
	Chain                   string `bson:"chain" json:"chain"`
	Address                 string `bson:"address" json:"address"`
	Direction               string `bson:"direction" json:"direction"`
	Asset                   string `bson:"asset" json:"asset"`
	store.CandidateCoverage `bson:",inline" json:",inline"`
}

type ScoreFactors struct {
	SourceBase      int     `bson:"sourceBase" json:"sourceBase"`
	LabelConfidence float64 `bson:"labelConfidence" json:"labelConfidence"`
	HopFactor       float64 `bson:"hopFactor" json:"hopFactor"`
	TimeFactor      float64 `bson:"timeFactor" json:"timeFactor"`
	AmountFactor    float64 `bson:"amountFactor" json:"amountFactor"`
	ProtocolFactor  float64 `bson:"protocolFactor" json:"protocolFactor"`
}

type TransactionEvidence struct {
	Chain       string    `bson:"chain" json:"chain"`
	TxHash      string    `bson:"txHash" json:"txHash"`
	BlockNumber int64     `bson:"blockNumber" json:"blockNumber"`
	BlockTime   time.Time `bson:"blockTime,omitempty" json:"blockTime,omitempty"`
	From        string    `bson:"from" json:"from"`
	To          string    `bson:"to" json:"to"`
	Amount      string    `bson:"amount" json:"amount"`
	Protocol    string    `bson:"protocol" json:"protocol"`
}

type PathEvidence struct {
	Nodes        []string              `bson:"nodes" json:"nodes"`
	Transactions []TransactionEvidence `bson:"transactions" json:"transactions"`
	Factors      ScoreFactors          `bson:"factors" json:"factors"`
	Score        int                   `bson:"score" json:"score"`
}

type DirectRisk struct {
	Present bool          `bson:"present" json:"present"`
	Score   int           `bson:"score" json:"score"`
	Labels  []store.Label `bson:"labels" json:"labels"`
}

type Association struct {
	SourceLabelID string       `bson:"sourceLabelId" json:"sourceLabelId"`
	SourceAddress string       `bson:"sourceAddress" json:"sourceAddress"`
	SourceType    string       `bson:"sourceType" json:"sourceType"`
	TargetChain   string       `bson:"targetChain" json:"targetChain"`
	TargetAddress string       `bson:"targetAddress" json:"targetAddress"`
	Direction     string       `bson:"direction" json:"direction"`
	Asset         string       `bson:"asset" json:"asset"`
	Confidence    float64      `bson:"confidence" json:"confidence"`
	Score         int          `bson:"score" json:"score"`
	Distance      int          `bson:"distance" json:"distance"`
	Level         string       `bson:"level" json:"level"`
	Path          PathEvidence `bson:"path" json:"path"`
	Paths         [][]string   `bson:"paths" json:"paths"`
	TxHashes      [][]string   `bson:"txHashes" json:"txHashes"`
	CycleDetected bool         `bson:"cycleDetected,omitempty" json:"cycleDetected,omitempty"`
}

type NodeRiskAssessment struct {
	Chain        string        `bson:"chain" json:"chain"`
	Address      string        `bson:"address" json:"address"`
	Status       string        `bson:"status" json:"status"`
	Score        int           `bson:"score" json:"score"`
	Level        string        `bson:"level" json:"level"`
	DirectRisk   DirectRisk    `bson:"directRisk" json:"directRisk"`
	Associations []Association `bson:"associations" json:"associations"`
}

type Result struct {
	Status             string               `bson:"status" json:"status"`
	Score              int                  `bson:"score" json:"score"`
	Level              string               `bson:"level" json:"level"`
	DirectRisk         DirectRisk           `bson:"directRisk" json:"directRisk"`
	Nodes              []NodeRiskAssessment `bson:"nodes" json:"nodes"`
	Associations       []Association        `bson:"associations" json:"associations"`
	Coverage           []Coverage           `bson:"coverage" json:"coverage"`
	MissingAddresses   []string             `bson:"missingAddresses" json:"missingAddresses"`
	CandidateCoverage  float64              `bson:"candidateCoverage" json:"candidateCoverage"`
	RiskRuleVersion    string               `bson:"ruleVersion" json:"ruleVersion"`
	PropagationVersion string               `bson:"propagationVersion" json:"propagationVersion"`
	DataThroughBlock   int64                `bson:"dataThroughBlock" json:"dataThroughBlock"`
	VisitedNodes       int                  `bson:"visitedNodes" json:"visitedNodes"`
	EdgeCount          int                  `bson:"edgeCount" json:"edgeCount"`
	Truncated          bool                 `bson:"truncated" json:"truncated"`
	TruncationReason   string               `bson:"truncationReason,omitempty" json:"truncationReason,omitempty"`
}

type edgeEvidence struct {
	transaction    TransactionEvidence
	amountFactor   float64
	timeFactor     float64
	protocolFactor float64
}

type sourceOccurrence struct {
	label store.Label
	index int
}

type searchState struct {
	chain, address, direction, assetMode, asset string
	path                                        []string
	edges                                       []edgeEvidence
	sources                                     []sourceOccurrence
}

type Engine struct {
	repository Repository
	clock      func() time.Time
}

func NewEngine(repository Repository) *Engine {
	return &Engine{repository: repository, clock: time.Now}
}

// Run evaluates the target against already synchronized facts only.
func (e *Engine) Run(ctx context.Context, chain, targetAddress, direction, asset string, dataThroughBlock int64, _ []store.Label, _ []string, config Config, progress func(int, int, int) error) (Result, error) {
	result := Result{
		Status: "complete", RiskRuleVersion: RiskRuleVersion, PropagationVersion: Version, CandidateCoverage: 1,
		Nodes: []NodeRiskAssessment{}, Associations: []Association{}, Coverage: []Coverage{}, MissingAddresses: []string{},
		DirectRisk: DirectRisk{Labels: []store.Label{}},
	}
	metadata, found, err := e.repository.FindAddress(ctx, chain, targetAddress)
	if err != nil {
		return result, fmt.Errorf("find propagation target: %w", err)
	}
	if !found || metadata.SyncStatus != "synced" {
		return result, fmt.Errorf("propagation target is not synchronized")
	}
	result.DataThroughBlock = dataThroughBlock
	if result.DataThroughBlock <= 0 || result.DataThroughBlock > metadata.LatestSyncedBlock {
		result.DataThroughBlock = metadata.LatestSyncedBlock
	}
	if config.MaxHops <= 0 || config.MaxHops > 3 {
		config.MaxHops = 3
	}
	directions := []string{direction}
	if direction == "both" {
		directions = []string{"in", "out"}
	}
	queue := make([]searchState, 0, len(directions))
	for _, value := range directions {
		mode, normalizedAsset := assetMode(asset)
		channels := []store.AssetChannel{{AssetMode: mode, Asset: normalizedAsset}}
		if strings.EqualFold(asset, "all") {
			observed, assetErr := e.repository.ListTransferAssets(ctx, chain, targetAddress, value, result.DataThroughBlock, config.MaxAssetChannels)
			if assetErr != nil {
				return result, fmt.Errorf("list propagation assets: %w", assetErr)
			}
			channels = observed.Items
			if observed.Truncated {
				markPartial(&result, "asset_channels")
			}
		}
		for _, channel := range channels {
			queue = append(queue, searchState{chain: chain, address: targetAddress, direction: value, assetMode: channel.AssetMode, asset: channel.Asset, path: []string{nodeKey(chain, targetAddress)}})
		}
	}
	if len(queue) == 0 {
		queue = append(queue, searchState{chain: chain, address: targetAddress, direction: directions[0], assetMode: "eth", asset: "ETH", path: []string{nodeKey(chain, targetAddress)}})
	}
	seen := make(map[string]struct{})
	visited := make(map[string]struct{})
	direct := make(map[string]DirectRisk)
	associations := make(map[string]Association)
	missing := make(map[string]struct{})
	coverageSum, coverageCount := 0.0, 0
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		current := queue[0]
		queue = queue[1:]
		stateKey := strings.Join([]string{current.chain, current.address, current.direction, current.asset}, ":")
		if _, ok := seen[stateKey]; ok {
			continue
		}
		seen[stateKey] = struct{}{}
		currentNode := nodeKey(current.chain, current.address)
		visited[currentNode] = struct{}{}
		if progress != nil {
			if err := progress(len(current.edges), len(visited), result.EdgeCount); err != nil {
				return result, fmt.Errorf("save propagation progress: %w", err)
			}
		}
		addressMetadata, synchronized, err := e.repository.FindAddress(ctx, current.chain, current.address)
		if err != nil {
			return result, fmt.Errorf("find propagation node: %w", err)
		}
		if !synchronized || addressMetadata.SyncStatus != "synced" {
			missing[currentNode] = struct{}{}
			result.Status = "partial"
			continue
		}
		labels, err := e.repository.ListLabels(ctx, current.chain, current.address)
		if err != nil {
			return result, fmt.Errorf("list propagation labels: %w", err)
		}
		riskLabels := deterministicLabels(labels)
		if len(riskLabels) > 0 {
			direct[currentNode] = directAssessment(riskLabels)
			for _, label := range riskLabels {
				current.sources = append(current.sources, sourceOccurrence{label: label, index: len(current.path) - 1})
				for targetIndex := 0; targetIndex < len(current.path)-1; targetIndex++ {
					mergeAssociation(associations, associationFor(label, current, len(current.path)-1, targetIndex, e.clock()))
				}
			}
		}
		for _, source := range current.sources {
			if source.index < len(current.path)-1 {
				mergeAssociation(associations, associationFor(source.label, current, source.index, len(current.path)-1, e.clock()))
			}
		}
		if len(current.edges) >= config.MaxHops || (len(current.edges) > 0 && terminal(addressMetadata, labels)) {
			continue
		}
		forcedLabels, err := e.repository.ListRiskLabels(ctx, current.chain, int64(config.PerNodeCandidateCap))
		if err != nil {
			return result, fmt.Errorf("list deterministic risk labels: %w", err)
		}
		candidates, err := e.repository.PropagationCandidates(ctx, store.CandidateQuery{Chain: current.chain, Address: current.address, Direction: current.direction, AssetMode: current.assetMode, Asset: current.asset, PerChannelLimit: config.PerChannelLimit, Limit: config.PerNodeCandidateCap, ToBlock: result.DataThroughBlock, ForcedCounterparties: riskAddresses(forcedLabels, config.PerNodeCandidateCap)})
		if err != nil {
			return result, fmt.Errorf("query propagation candidates: %w", err)
		}
		result.Coverage = append(result.Coverage, Coverage{Chain: current.chain, Address: current.address, Direction: current.direction, Asset: current.asset, CandidateCoverage: candidates.Coverage})
		coverageSum += coverageRatio(candidates.Coverage)
		coverageCount++
		if candidates.Coverage.Truncated {
			markPartial(&result, "candidate_coverage")
		}
		for _, summary := range candidates.Items {
			if result.EdgeCount >= config.MaxEdges || len(visited) >= config.MaxNodes {
				markPartial(&result, budgetReason(result.EdgeCount, config.MaxEdges))
				queue = nil
				break
			}
			nextAddress := summary.To
			if current.direction == "in" {
				nextAddress = summary.From
			}
			next := current
			next.address = strings.ToLower(nextAddress)
			if next.address == current.address {
				continue
			}
			next.assetMode, next.asset, next.address, err = e.confirmedSwap(ctx, current, next, summary)
			if err != nil {
				return result, err
			}
			result.EdgeCount++
			edge := transferEvidence(current.chain, summary, candidates.Coverage, e.clock())
			if next.address != strings.ToLower(nextAddress) || next.asset != current.asset {
				edge.transaction.Protocol, edge.protocolFactor = "verified_swap", 0.9
			}
			next.path = appendCopy(current.path, nodeKey(next.chain, next.address))
			next.edges = appendEdge(current.edges, edge)
			next.sources = append([]sourceOccurrence(nil), current.sources...)
			if contains(current.path, nodeKey(next.chain, next.address)) {
				continue
			}
			queue = append(queue, next)
		}
		bridges, err := e.repository.ListCrossChainLinks(ctx, current.chain, current.address, int64(config.PerChannelLimit))
		if err != nil {
			return result, fmt.Errorf("list confirmed bridge links: %w", err)
		}
		for _, link := range bridges {
			next, ok := bridgeState(current, link)
			if !ok || !linkWithinBlock(current, link, result.DataThroughBlock) || len(current.edges) >= config.MaxHops {
				continue
			}
			result.EdgeCount++
			next.path = appendCopy(current.path, nodeKey(next.chain, next.address))
			next.edges = appendEdge(current.edges, bridgeEvidence(current, link, e.clock()))
			next.sources = append([]sourceOccurrence(nil), current.sources...)
			if !contains(current.path, nodeKey(next.chain, next.address)) {
				queue = append(queue, next)
			}
		}
	}
	if coverageCount > 0 {
		result.CandidateCoverage = coverageSum / float64(coverageCount)
	}
	for value := range missing {
		result.MissingAddresses = append(result.MissingAddresses, value)
	}
	sort.Strings(result.MissingAddresses)
	result.VisitedNodes = len(visited)
	for _, item := range associations {
		result.Associations = append(result.Associations, item)
	}
	sortAssociations(result.Associations)
	result.Nodes = buildNodeAssessments(visited, direct, result.Associations, result.Status)
	root := assessmentByKey(result.Nodes, nodeKey(chain, targetAddress))
	result.Score, result.Level, result.DirectRisk = root.Score, root.Level, root.DirectRisk
	if result.Status == "partial" && result.Score == 0 {
		result.Status = "unknown"
	}
	return result, nil
}

func associationFor(label store.Label, state searchState, sourceIndex, targetIndex int, now time.Time) Association {
	start, end := sourceIndex, targetIndex
	if start > end {
		start, end = end, start
	}
	edges := state.edges[start:end]
	nodes := append([]string(nil), state.path[start:end+1]...)
	if sourceIndex > targetIndex {
		reverse(nodes)
	}
	factors := ScoreFactors{SourceBase: sourceBase(label), LabelConfidence: labelConfidence(label.Confidence), HopFactor: hopFactor(len(edges)), TimeFactor: 1, AmountFactor: 1, ProtocolFactor: 1}
	txs := make([]TransactionEvidence, 0, len(edges))
	for _, edge := range edges {
		factors.TimeFactor = math.Min(factors.TimeFactor, edge.timeFactor)
		factors.AmountFactor = math.Min(factors.AmountFactor, edge.amountFactor)
		factors.ProtocolFactor = math.Min(factors.ProtocolFactor, edge.protocolFactor)
		txs = append(txs, edge.transaction)
	}
	if sourceIndex > targetIndex {
		reverseTransactions(txs)
	}
	score := int(math.Round(float64(factors.SourceBase) * factors.LabelConfidence * factors.HopFactor * factors.TimeFactor * factors.AmountFactor * factors.ProtocolFactor))
	parts := strings.SplitN(state.path[targetIndex], ":", 2)
	path := PathEvidence{Nodes: nodes, Transactions: txs, Factors: factors, Score: score}
	hashes := make([]string, 0, len(txs))
	for _, tx := range txs {
		hashes = append(hashes, tx.TxHash)
	}
	return Association{SourceLabelID: label.ID.Hex(), SourceAddress: strings.ToLower(label.Address), SourceType: label.Type, TargetChain: parts[0], TargetAddress: parts[1], Direction: state.direction, Asset: state.asset, Confidence: float64(score) / 100, Score: score, Distance: len(edges), Level: scoreLevel(score), Path: path, Paths: [][]string{nodes}, TxHashes: [][]string{hashes}}
}

func mergeAssociation(values map[string]Association, candidate Association) {
	key := candidate.SourceAddress + ":" + candidate.TargetChain + ":" + candidate.TargetAddress + ":" + candidate.Asset
	if current, ok := values[key]; !ok || candidate.Score > current.Score {
		values[key] = candidate
	}
}

func buildNodeAssessments(visited map[string]struct{}, direct map[string]DirectRisk, associations []Association, status string) []NodeRiskAssessment {
	byNode := make(map[string][]Association)
	for _, item := range associations {
		key := nodeKey(item.TargetChain, item.TargetAddress)
		byNode[key] = append(byNode[key], item)
	}
	result := make([]NodeRiskAssessment, 0, len(visited))
	for key := range visited {
		parts := strings.SplitN(key, ":", 2)
		items := byNode[key]
		if items == nil {
			items = []Association{}
		}
		directRisk := direct[key]
		if directRisk.Labels == nil {
			directRisk.Labels = []store.Label{}
		}
		score := max(directRisk.Score, aggregateAssociations(items))
		result = append(result, NodeRiskAssessment{Chain: parts[0], Address: parts[1], Status: status, Score: score, Level: scoreLevel(score), DirectRisk: directRisk, Associations: items})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Chain+result[i].Address < result[j].Chain+result[j].Address })
	return result
}

func aggregateAssociations(items []Association) int {
	best := make(map[string]int)
	for _, item := range items {
		best[item.SourceAddress] = max(best[item.SourceAddress], item.Score)
	}
	remaining := 1.0
	for _, score := range best {
		remaining *= 1 - float64(score)/100
	}
	return int(math.Round(100 * (1 - remaining)))
}

func directAssessment(labels []store.Label) DirectRisk {
	if labels == nil {
		labels = []store.Label{}
	}
	result := DirectRisk{Present: len(labels) > 0, Labels: labels}
	for _, label := range labels {
		result.Score = max(result.Score, int(math.Round(float64(sourceBase(label))*labelConfidence(label.Confidence))))
	}
	return result
}

func deterministicLabels(labels []store.Label) []store.Label {
	result := make([]store.Label, 0, len(labels))
	for _, label := range labels {
		if riskSource(label) {
			result = append(result, label)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.Hex() < result[j].ID.Hex() })
	return result
}

func riskSource(label store.Label) bool {
	return (label.Source == "manual" || label.Source == "public-list") && (label.RiskLevel == "medium" || label.RiskLevel == "high")
}

func sourceBase(label store.Label) int {
	if label.RiskLevel == "high" {
		return 100
	}
	if label.RiskLevel == "medium" {
		return 60
	}
	return 0
}

func hopFactor(distance int) float64 {
	switch distance {
	case 1:
		return 1
	case 2:
		return 0.65
	case 3:
		return 0.4
	default:
		return 0

	}
}

func timeFactor(at, now time.Time) float64 {
	if at.IsZero() {
		return 0.4
	}
	age := now.Sub(at)
	switch {
	case age <= 30*24*time.Hour:
		return 1
	case age <= 180*24*time.Hour:
		return 0.8
	case age <= 365*24*time.Hour:
		return 0.6
	default:
		return 0.4
	}
}

func amountFactor(part, total string) float64 {
	p, okP := new(big.Int).SetString(part, 10)
	t, okT := new(big.Int).SetString(total, 10)
	if !okP || !okT || t.Sign() <= 0 {
		return 0.25
	}
	ratio := new(big.Rat).SetFrac(p, t)
	switch {
	case ratio.Cmp(big.NewRat(1, 10)) >= 0:
		return 1
	case ratio.Cmp(big.NewRat(1, 100)) >= 0:
		return 0.75
	case ratio.Cmp(big.NewRat(1, 1000)) >= 0:
		return 0.5
	default:
		return 0.25
	}
}

func transferEvidence(chain string, summary store.CounterpartySummary, coverage store.CandidateCoverage, now time.Time) edgeEvidence {
	tx := summary.LatestTransfer
	if tx.TxHash == "" {
		tx = summary.Representative
	}
	return edgeEvidence{transaction: TransactionEvidence{Chain: chain, TxHash: tx.TxHash, BlockNumber: tx.BlockNumber, BlockTime: tx.BlockTime, From: tx.From, To: tx.To, Amount: transferAmount(tx)}, amountFactor: amountFactor(summary.TotalAmount, coverage.TotalAmount), timeFactor: timeFactor(summary.LatestTime, now), protocolFactor: 1}
}

func bridgeEvidence(current searchState, link store.CrossChainLink, now time.Time) edgeEvidence {
	block, hash, from, to, amount := link.SourceBlock, link.SourceTxHash, link.SourceAddress, link.TargetAddress, link.SourceAmount
	if current.direction == "in" {
		block, hash, from, to, amount = link.TargetBlock, link.TargetTxHash, link.TargetAddress, link.SourceAddress, link.TargetAmount
	}
	return edgeEvidence{transaction: TransactionEvidence{Chain: current.chain, TxHash: hash, BlockNumber: block, From: from, To: to, Amount: amount, Protocol: "confirmed_bridge"}, amountFactor: 1, timeFactor: timeFactor(link.ObservedAt, now), protocolFactor: 0.9}
}

func (e *Engine) confirmedSwap(ctx context.Context, current, next searchState, summary store.CounterpartySummary) (string, string, string, error) {
	analysis, found, err := e.repository.FindTransactionAnalysis(ctx, current.chain, summary.Representative.TxHash)
	if err != nil {
		return "", "", "", fmt.Errorf("find confirmed swap analysis: %w", err)
	}
	if !found || !analysis.Succeeded || analysis.Quality.Status != "complete" || analysis.Quality.AmbiguousRoute {
		return next.assetMode, next.asset, next.address, nil
	}
	for _, swap := range analysis.Swaps {
		if swap.Verified && strings.EqualFold(swap.TokenIn, current.asset) && swap.TokenOut != "" && swap.OutputAddress != "" {
			mode, value := assetMode(swap.TokenOut)
			return mode, value, strings.ToLower(swap.OutputAddress), nil
		}
	}
	return next.assetMode, next.asset, next.address, nil
}

func terminal(metadata store.Address, labels []store.Label) bool {
	if metadata.IsTerminal {
		return true
	}
	for _, label := range labels {
		switch strings.ToLower(label.Type) {
		case "exchange", "exchange_hot_wallet", "hot_wallet", "router", "pool", "bridge":
			return true
		}
	}
	return false
}

func bridgeState(current searchState, link store.CrossChainLink) (searchState, bool) {
	if current.direction == "out" && link.Status == "confirmed" && link.SourceChain == current.chain && strings.EqualFold(link.SourceAddress, current.address) && strings.EqualFold(link.SourceAsset, current.asset) {
		next := current
		next.chain, next.address = link.TargetChain, strings.ToLower(link.TargetAddress)
		next.assetMode, next.asset = assetMode(link.TargetAsset)
		return next, true
	}
	if current.direction == "in" && link.Status == "confirmed" && link.TargetChain == current.chain && strings.EqualFold(link.TargetAddress, current.address) && strings.EqualFold(link.TargetAsset, current.asset) {
		next := current
		next.chain, next.address = link.SourceChain, strings.ToLower(link.SourceAddress)
		next.assetMode, next.asset = assetMode(link.SourceAsset)
		return next, true
	}
	return searchState{}, false
}

func AssociationRecord(item Association, block int64, now time.Time) (store.InferredRiskAssociation, error) {
	labelID, err := primitive.ObjectIDFromHex(item.SourceLabelID)
	if err != nil {
		return store.InferredRiskAssociation{}, err
	}
	return store.InferredRiskAssociation{SourceLabelID: labelID, SourceAddress: item.SourceAddress, SourceType: item.SourceType, TargetChain: item.TargetChain, TargetAddress: item.TargetAddress, Direction: item.Direction, Asset: item.Asset, PropagationVersion: Version, RuleVersion: RiskRuleVersion, DataThroughBlock: block, Confidence: item.Confidence, Score: item.Score, Paths: item.Paths, TxHashes: item.TxHashes, BestPathEvidence: item.Path, ComputedAt: now}, nil
}

func riskAddresses(labels []store.Label, limit int) []string {
	result := make([]string, 0, min(limit, len(labels)))
	seen := make(map[string]struct{})
	for _, label := range labels {
		address := strings.ToLower(label.Address)
		if !riskSource(label) {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		result = append(result, address)
		if len(result) == limit {
			break
		}
	}
	return result
}

func assetMode(asset string) (string, string) {
	if strings.EqualFold(asset, "ETH") {
		return "eth", "ETH"
	}
	return "contract", strings.ToLower(asset)
}

func scoreLevel(score int) string {
	switch {
	case score >= 70:
		return "strong"
	case score >= 40:
		return "medium"
	case score > 0:
		return "weak"
	default:
		return "no_evidence"
	}
}

func labelConfidence(value float64) float64 { return math.Max(0, math.Min(1, value)) }
func nodeKey(chain, address string) string  { return strings.ToLower(chain + ":" + address) }
func appendCopy(values []string, value string) []string {
	return append(append([]string(nil), values...), value)
}
func appendEdge(values []edgeEvidence, value edgeEvidence) []edgeEvidence {
	return append(append([]edgeEvidence(nil), values...), value)
}
func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func coverageRatio(value store.CandidateCoverage) float64 {
	parsed, ok := new(big.Rat).SetString(value.AmountCoverage)
	if !ok || parsed == nil {
		return 0
	}
	result, _ := parsed.Float64()
	return result
}
func markPartial(result *Result, reason string) {
	result.Status, result.Truncated = "partial", true
	if result.TruncationReason == "" {
		result.TruncationReason = reason
	}
}
func budgetReason(edges, maxEdges int) string {
	if edges >= maxEdges {
		return "max_edges"
	}
	return "max_nodes"
}
func linkWithinBlock(current searchState, link store.CrossChainLink, block int64) bool {
	if block <= 0 {
		return true
	}
	if current.direction == "out" && link.SourceBlock > 0 {
		return link.SourceBlock <= block
	}
	if current.direction == "in" && link.TargetBlock > 0 {
		return link.TargetBlock <= block
	}
	return true
}
func transferAmount(value store.Transfer) string {
	if value.Amount != "" {
		return value.Amount
	}
	return value.TokenValue
}
func reverse(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
func reverseTransactions(values []TransactionEvidence) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
func sortAssociations(values []Association) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Score != values[j].Score {
			return values[i].Score > values[j].Score
		}
		return values[i].TargetChain+values[i].TargetAddress+values[i].SourceAddress < values[j].TargetChain+values[j].TargetAddress+values[j].SourceAddress
	})
}
func assessmentByKey(values []NodeRiskAssessment, key string) NodeRiskAssessment {
	for _, value := range values {
		if nodeKey(value.Chain, value.Address) == key {
			return value
		}
	}
	return NodeRiskAssessment{Status: "unknown", Level: "no_evidence"}
}
