package tracer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/chains"
	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/risk"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

var ErrInvalidQuery = errors.New("invalid trace query")
var ErrAddressNotSynced = errors.New("address is not synced")

type AddressNotSyncedError struct{ Chain, Address string }

func (e AddressNotSyncedError) Error() string { return ErrAddressNotSynced.Error() + ": " + e.Address }
func (e AddressNotSyncedError) Unwrap() error { return ErrAddressNotSynced }

type Repository interface {
	FindAddress(context.Context, string, string) (store.Address, bool, error)
	QueryTransfers(context.Context, store.TransferQuery) ([]store.Transfer, error)
	ListLabels(context.Context, string, string) ([]store.Label, error)
	ListCrossChainLinks(context.Context, string, string, int64) ([]store.CrossChainLink, error)
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
	Transfer store.Transfer `json:"transfer"`
	Depth    int            `json:"depth"`
	Path     []string       `json:"path"`
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
	Address   string
	Direction string
	Path      []string
}
type bridgeBranchState struct {
	Node      Node
	Direction string
	Path      []NodeRef
}

type Graph struct{ repository Repository }

func New(repository Repository) *Graph { return &Graph{repository: repository} }

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
	result := Result{DataThroughBlock: metadata.LatestSyncedBlock, DataStatus: "synced", RuleVersion: "trace-v1"}
	seedLabels, err := g.repository.ListLabels(ctx, q.Chain, seed)
	if err != nil {
		return Result{}, err
	}
	seedState := branchState{Address: seed, Direction: q.Direction, Path: []string{seed}}
	frontier := []branchState{seedState}
	visitedStates := map[string]bool{branchStateKey(seed, q.Direction): true}
	visitedNodes := map[string]bool{seed: true}
	seedTerminal := metadata.IsTerminal || hasTerminalLabel(seedLabels)
	result.Nodes = append(result.Nodes, Node{Chain: q.Chain, Address: seed, Depth: 0, Terminal: seedTerminal})
	result.branchStates = append(result.branchStates, seedState)
	allTransfers := make([]store.Transfer, 0)
	resultFacts := make(map[string]bool)
	seenFacts := make(map[string]bool)
	for depth := 0; depth < q.Depth && len(frontier) > 0 && len(visitedNodes) < 5000; depth++ {
		sort.Slice(frontier, func(i, j int) bool {
			return branchStateKey(frontier[i].Address, frontier[i].Direction) < branchStateKey(frontier[j].Address, frontier[j].Direction)
		})
		next := make([]branchState, 0)
		groups := map[string][]branchState{}
		for _, state := range frontier {
			groups[state.Direction] = append(groups[state.Direction], state)
		}
		for _, direction := range []string{"both", "in", "out"} {
			states := groups[direction]
			for offset := 0; offset < len(states); offset += 500 {
				end := min(offset+500, len(states))
				batchStates := states[offset:end]
				batch := make([]string, len(batchStates))
				stateByAddress := make(map[string]branchState, len(batchStates))
				for index, state := range batchStates {
					batch[index], stateByAddress[state.Address] = state.Address, state
				}
				transfers, queryErr := g.repository.QueryTransfers(ctx, store.TransferQuery{Chain: q.Chain, Addresses: batch, Direction: direction, AssetMode: assetMode(q.Asset), Asset: assetValue(q.Asset), Limit: 10000})
				if queryErr != nil {
					return Result{}, queryErr
				}
				byAddress := make(map[string][]store.Transfer)
				for _, transfer := range transfers {
					if !seenFacts[edgeKey(transfer)] {
						allTransfers = append(allTransfers, transfer)
						seenFacts[edgeKey(transfer)] = true
					}
					for _, address := range batch {
						if direction == "both" {
							if strings.EqualFold(transfer.To, address) {
								byAddress[strings.ToLower(address)+"|in"] = append(byAddress[strings.ToLower(address)+"|in"], transfer)
							}
							if strings.EqualFold(transfer.From, address) {
								byAddress[strings.ToLower(address)+"|out"] = append(byAddress[strings.ToLower(address)+"|out"], transfer)
							}
						} else if (direction == "in" && strings.EqualFold(transfer.To, address)) || (direction == "out" && strings.EqualFold(transfer.From, address)) {
							byAddress[strings.ToLower(address)+"|"+direction] = append(byAddress[strings.ToLower(address)+"|"+direction], transfer)
						}
					}
				}
				for _, address := range batch {
					state := stateByAddress[address]
					if nodeTerminal(result.Nodes, address) {
						continue
					}
					directions := []string{direction}
					if direction == "both" {
						directions = []string{"in", "out"}
					}
					for _, selectedDirection := range directions {
						candidates := byAddress[strings.ToLower(address)+"|"+selectedDirection]
						sort.SliceStable(candidates, func(i, j int) bool { return edgeKey(candidates[i]) > edgeKey(candidates[j]) })
						count := 0
						for _, transfer := range candidates {
							if count >= q.TopN {
								break
							}
							other := strings.ToLower(transfer.To)
							if selectedDirection == "in" {
								other = strings.ToLower(transfer.From)
							} else if other == strings.ToLower(address) {
								other = strings.ToLower(transfer.From)
							}
							if other == "" {
								continue
							}
							path := append(append([]string(nil), state.Path...), other)
							if !resultFacts[edgeKey(transfer)] {
								result.Edges = append(result.Edges, Edge{Transfer: transfer, Depth: depth + 1, Path: path})
								resultFacts[edgeKey(transfer)] = true
							}
							count++
							if other == zeroAddress {
								if !visitedNodes[other] && len(visitedNodes) < 5000 {
									visitedNodes[other] = true
									result.Nodes = append(result.Nodes, Node{Chain: q.Chain, Address: other, Depth: depth + 1, Terminal: true})
								}
								continue
							}
							stateKey := branchStateKey(other, selectedDirection)
							if visitedStates[stateKey] || len(visitedNodes) >= 5000 {
								continue
							}
							otherMetadata, otherFound, metadataErr := g.repository.FindAddress(ctx, q.Chain, other)
							if metadataErr != nil {
								return Result{}, metadataErr
							}
							if !otherFound || otherMetadata.SyncStatus != "synced" {
								return Result{}, AddressNotSyncedError{Chain: q.Chain, Address: other}
							}
							otherLabels, labelsErr := g.repository.ListLabels(ctx, q.Chain, other)
							if labelsErr != nil {
								return Result{}, labelsErr
							}
							terminal := otherFound && otherMetadata.IsTerminal || hasTerminalLabel(otherLabels)
							visitedStates[stateKey] = true
							result.branchStates = append(result.branchStates, branchState{Address: other, Direction: selectedDirection, Path: path})
							isNewNode := !visitedNodes[other]
							visitedNodes[other] = true
							if !terminal {
								next = append(next, branchState{Address: other, Direction: selectedDirection, Path: path})
							}
							if isNewNode {
								result.Nodes = append(result.Nodes, Node{Chain: q.Chain, Address: other, Depth: depth + 1, Terminal: terminal})
							}
							result.Paths = append(result.Paths, path)
						}
					}
				}
			}
		}
		frontier = next
	}
	allLabels := append([]store.Label(nil), seedLabels...)
	for _, node := range result.Nodes[1:] {
		labels, labelErr := g.repository.ListLabels(ctx, q.Chain, node.Address)
		if labelErr != nil {
			return Result{}, labelErr
		}
		allLabels = append(allLabels, labels...)
	}
	result.Risk = risk.Analyze(seed, allTransfers, allLabels)
	result.Labels = result.Risk.InferredLabels
	return result, nil
}

func branchStateKey(address, direction string) string { return address + "|" + direction }

func (g *Graph) traceWithBridges(ctx context.Context, query Query) (Result, error) {
	q, err := normalize(query)
	if err != nil {
		return Result{}, err
	}
	result, err := g.traceSameChain(ctx, q)
	if err != nil {
		return Result{}, err
	}
	result.RuleVersion = "trace-v2"
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
		factSeen[edgeKey(edge.Transfer)] = true
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
				if edge.Depth+bridgeDepth > q.Depth || factSeen[edgeKey(edge.Transfer)] {
					continue
				}
				edge.Depth += bridgeDepth
				edge.Path = append(addresses(basePath), edge.Path[1:]...)
				result.Edges = append(result.Edges, edge)
				factSeen[edgeKey(edge.Transfer)] = true
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
	return q, nil
}
func assetMode(value string) string {
	switch strings.ToLower(value) {
	case "", "all":
		return "all"
	case "eth":
		return "eth"
	case "erc20":
		return "erc20"
	default:
		return "contract"
	}
}
func assetValue(value string) string {
	if strings.EqualFold(value, "eth") {
		return "ETH"
	}
	if assetMode(value) == "contract" {
		a, _ := ethaddr.Normalize(value)
		return a
	}
	return ""
}
func edgeKey(t store.Transfer) string {
	return fmt.Sprintf("%s|%020d|%s|%s|%s|%020d|%s", t.Chain, t.BlockNumber, t.TxHash, t.Source, t.TraceID, t.LogIndex, t.Asset)
}

const zeroAddress = "0x0000000000000000000000000000000000000000"
