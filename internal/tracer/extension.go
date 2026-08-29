package tracer

import (
	"context"
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

var ErrInvalidExtension = errors.New("invalid trace extension")

type ExtensionRequest struct {
	Chain     string `json:"chain"`
	Address   string `json:"address"`
	Direction string `json:"direction"`
	Depth     int    `json:"depth"`
}

type extensionAnchor struct {
	AssetMode string
	Asset     string
	Amount    string
	FromBlock int64
	ToBlock   int64
	Path      []string
}

func ValidateExtension(request ExtensionRequest) error {
	if request.Chain != "ethereum" || request.Direction != "in" && request.Direction != "out" || request.Depth != 1 {
		return ErrInvalidExtension
	}
	_, err := ethaddr.Normalize(request.Address)
	if err != nil {
		return ErrInvalidExtension
	}
	return nil
}

// ExtendBranch traces one direction and one hop from a node already present in root.
func (g *Graph) ExtendBranch(ctx context.Context, root Result, request ExtensionRequest) (Result, error) {
	if err := ValidateExtension(request); err != nil {
		return Result{}, err
	}
	request.Address, _ = ethaddr.Normalize(request.Address)
	anchorNode, found := resultNode(root.Nodes, request.Chain, request.Address)
	if !found || anchorNode.Terminal {
		return Result{}, ErrInvalidExtension
	}
	anchors := extensionAnchors(root, request)
	if len(anchors) == 0 {
		return Result{}, ErrInvalidExtension
	}
	result := Result{
		Nodes: []Node{anchorNode}, DataThroughBlock: root.DataThroughBlock, DataThroughBlocks: root.DataThroughBlocks,
		DataStatus: root.DataStatus, RuleVersion: traceRuleVersion,
	}
	if anchorNode.Protocol == "thorchain" && containsRole(anchorNode.Roles, "router") && request.Direction == "out" {
		return g.extendTHORChainRouter(ctx, root, result, request)
	}
	for _, anchor := range anchors {
		transfers, err := g.repository.QueryTransfers(ctx, store.TransferQuery{
			Chain: request.Chain, Addresses: []string{request.Address}, Direction: request.Direction,
			AssetMode: anchor.AssetMode, Asset: anchor.Asset, FromBlock: anchor.FromBlock, ToBlock: anchor.ToBlock,
			Limit: traceTransferRecordCap,
		})
		if err != nil {
			return Result{}, err
		}
		if len(transfers) >= traceTransferRecordCap {
			result.DataStatus = "partial"
			result.Nodes[0].Terminal, result.Nodes[0].StopReason = true, "high_frequency"
			continue
		}
		sort.SliceStable(transfers, func(i, j int) bool {
			if request.Direction == "out" {
				return transfers[i].BlockNumber < transfers[j].BlockNumber
			}
			return transfers[i].BlockNumber > transfers[j].BlockNumber
		})
		budget, ok := new(big.Int).SetString(anchor.Amount, 10)
		if !ok || budget.Sign() <= 0 {
			continue
		}
		for _, transfer := range transfers {
			summary := transferSummary(transfer)
			amount, valid := new(big.Int).SetString(summary.TotalAmount, 10)
			if !valid || amount.Sign() <= 0 || budget.Sign() <= 0 {
				continue
			}
			if amount.Cmp(budget) > 0 {
				amount.Set(budget)
			}
			summary.TotalAmount = amount.String()
			budget.Sub(budget, amount)
			other := strings.ToLower(summary.To)
			if request.Direction == "in" {
				other = strings.ToLower(summary.From)
			}
			if other == "" || pathContains(anchor.Path, other) || !traceableSummaryAmount(request.Chain, summary) {
				continue
			}
			metadata, exists, metadataErr := g.repository.FindAddress(ctx, request.Chain, other)
			if metadataErr != nil {
				return Result{}, metadataErr
			}
			requiredFrom, requiredThrough := g.requiredStartBlocks[request.Chain], root.DataThroughBlock
			if request.Direction == "out" {
				requiredFrom = transfer.BlockNumber
			} else {
				requiredThrough = transfer.BlockNumber
			}
			dependencyStopReason := g.dependencyStopReason(request.Chain, other)
			if dependencyStopReason == "" && (!exists || !isHighFrequencyAddress(metadata) && !g.addressCovered(metadata, requiredFrom, requiredThrough)) {
				dependency := AddressNotSyncedError{Chain: request.Chain, Address: other}
				if request.Direction == "out" {
					dependency.StartBlock = transfer.BlockNumber
				} else {
					dependency.EndBlock = transfer.BlockNumber
				}
				return Result{}, dependency
			}
			labels, labelErr := g.repository.ListLabels(ctx, request.Chain, other)
			if labelErr != nil {
				return Result{}, labelErr
			}
			identity := addressIdentity(metadata)
			terminal := metadata.IsTerminal || isHighFrequencyAddress(metadata) || dependencyStopReason != "" || hasTerminalLabel(labels)
			if dependencyStopReason != "" {
				result.DataStatus = "partial"
			}
			path := append(append([]string(nil), anchor.Path...), other)
			result.Edges = append(result.Edges, edgeFromSummary(summary, anchorNode.Depth+1, path))
			result.Nodes = appendNode(result.Nodes, Node{Chain: request.Chain, Address: other, Depth: anchorNode.Depth + 1, Terminal: terminal, AddressType: identity.AddressType, Protocol: identity.Protocol, Roles: identity.Roles, StopReason: dependencyStopReason})
			result.Paths = append(result.Paths, path)
			result.MoneyTransfers = append(result.MoneyTransfers, moneyTransfer(summary, ""))
			result.MoneyStates = append(result.MoneyStates,
				store.MoneyState{Chain: summary.Chain, Address: strings.ToLower(summary.From), Direction: "out", AssetType: summary.AssetType, Asset: summary.Asset, Amount: summary.TotalAmount, RemainingAmount: summary.TotalAmount, EntryTxHash: summary.Representative.TxHash, EntryBlock: summary.Representative.BlockNumber, Path: path, Evidence: "transfer", Inferred: true},
				store.MoneyState{Chain: summary.Chain, Address: strings.ToLower(summary.To), Direction: "in", AssetType: summary.AssetType, Asset: summary.Asset, Amount: summary.TotalAmount, RemainingAmount: summary.TotalAmount, EntryTxHash: summary.Representative.TxHash, EntryBlock: summary.Representative.BlockNumber, Path: path, Evidence: "transfer", Inferred: true},
			)
		}
	}
	if len(result.Edges) == 0 && !result.Nodes[0].Terminal {
		result.Nodes[0].Terminal = true
		result.Nodes[0].StopReason = "no_matching_transfers"
	}
	return result, nil
}

func (g *Graph) extendTHORChainRouter(ctx context.Context, root, result Result, request ExtensionRequest) (Result, error) {
	if g.analyzer == nil {
		result.Nodes[0].Terminal, result.Nodes[0].StopReason = true, "unsupported_contract"
		return result, nil
	}
	for _, enteringEdge := range root.Edges {
		if !strings.EqualFold(enteringEdge.To, request.Address) || enteringEdge.TxHash == "" || len(enteringEdge.Path) == 0 || !strings.EqualFold(enteringEdge.Path[len(enteringEdge.Path)-1], request.Address) {
			continue
		}
		enteringTransfer := store.Transfer{
			Chain: enteringEdge.Chain, TxHash: enteringEdge.TxHash, BlockNumber: enteringEdge.FirstBlock,
			From: enteringEdge.From, To: enteringEdge.To, AssetType: enteringEdge.AssetType, Asset: enteringEdge.Asset,
			Symbol: enteringEdge.Symbol, Decimals: enteringEdge.Decimals, TokenMetadataComplete: enteringEdge.TokenMetadataComplete,
		}
		if enteringEdge.AssetType == "eth" || strings.EqualFold(enteringEdge.Asset, "ETH") {
			enteringTransfer.Amount = enteringEdge.TotalAmount
		} else {
			enteringTransfer.TokenValue = enteringEdge.TotalAmount
		}
		assetMode, asset := edgeAsset(enteringEdge)
		state := branchState{Address: request.Address, Direction: "out", AssetMode: assetMode, Asset: asset, EnteringTx: enteringTransfer, Amount: enteringEdge.TotalAmount, Contract: true, Identity: store.AddressIdentity{AddressType: "contract", Protocol: "thorchain", Roles: []string{"router"}}, Path: enteringEdge.Path}
		conversion, ok, err := g.conversionTransfer(ctx, request.Chain, state, enteringEdge.TxHash)
		if err != nil {
			return Result{}, err
		}
		if !ok || conversion.Protocol != "thorchain" || pathContains(enteringEdge.Path, conversion.Transfer.To) {
			continue
		}
		summary := transferSummary(conversion.Transfer)
		path := append(append([]string(nil), enteringEdge.Path...), strings.ToLower(conversion.Transfer.To))
		edge := edgeFromSummary(summary, result.Nodes[0].Depth+1, path)
		edge.Protocol, edge.ProtocolAction, edge.ProtocolMemo = conversion.Protocol, conversion.ProtocolAction, conversion.ProtocolMemo
		result.Edges = append(result.Edges, edge)
		node := Node{Chain: request.Chain, Address: strings.ToLower(conversion.Transfer.To), Depth: result.Nodes[0].Depth + 1}
		if conversion.VaultAddress != "" {
			node.Protocol, node.Roles = "thorchain", []string{"thorchain_vault"}
		}
		result.Nodes = appendNode(result.Nodes, node)
		result.Paths = append(result.Paths, path)
		result.MoneyTransfers = append(result.MoneyTransfers, moneyTransfer(summary, ""))
		result.MoneyStates = append(result.MoneyStates,
			store.MoneyState{Chain: summary.Chain, Address: strings.ToLower(summary.From), Direction: "out", AssetType: summary.AssetType, Asset: summary.Asset, Amount: summary.TotalAmount, RemainingAmount: summary.TotalAmount, EntryTxHash: summary.Representative.TxHash, EntryBlock: summary.Representative.BlockNumber, Path: path, Evidence: "thorchain", Inferred: false},
			store.MoneyState{Chain: summary.Chain, Address: strings.ToLower(summary.To), Direction: "in", AssetType: summary.AssetType, Asset: summary.Asset, Amount: summary.TotalAmount, RemainingAmount: summary.TotalAmount, EntryTxHash: summary.Representative.TxHash, EntryBlock: summary.Representative.BlockNumber, Path: path, Evidence: "thorchain", Inferred: false},
		)
	}
	if len(result.Edges) == 0 {
		result.Nodes[0].Terminal, result.Nodes[0].StopReason = true, "ambiguous_conversion"
	}
	return result, nil
}

func extensionAnchors(root Result, request ExtensionRequest) []extensionAnchor {
	grouped := make(map[string]*extensionAnchor)
	for _, edge := range root.Edges {
		matches := request.Direction == "out" && strings.EqualFold(edge.To, request.Address) || request.Direction == "in" && strings.EqualFold(edge.From, request.Address)
		if !matches || len(edge.Path) == 0 || !strings.EqualFold(edge.Path[len(edge.Path)-1], request.Address) {
			continue
		}
		mode, asset := edgeAsset(edge)
		key := mode + "|" + strings.ToLower(asset)
		anchor := grouped[key]
		if anchor == nil {
			anchor = &extensionAnchor{AssetMode: mode, Asset: asset, Amount: "0", Path: append([]string(nil), edge.Path...)}
			grouped[key] = anchor
		}
		anchor.Amount = addDecimal(anchor.Amount, edge.TotalAmount)
		if request.Direction == "out" && (anchor.FromBlock == 0 || edge.FirstBlock < anchor.FromBlock) {
			anchor.FromBlock = edge.FirstBlock
		}
		if request.Direction == "in" && edge.LatestBlock > anchor.ToBlock {
			anchor.ToBlock = edge.LatestBlock
		}
		if len(edge.Path) < len(anchor.Path) {
			anchor.Path = append(anchor.Path[:0], edge.Path...)
		}
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]extensionAnchor, 0, len(keys))
	for _, key := range keys {
		result = append(result, *grouped[key])
	}
	return result
}

func edgeAsset(edge Edge) (string, string) {
	if edge.AssetType == "eth" || strings.EqualFold(edge.Asset, "ETH") {
		return "eth", "ETH"
	}
	return "contract", strings.ToLower(edge.Asset)
}

func resultNode(nodes []Node, chain, address string) (Node, bool) {
	for _, node := range nodes {
		if node.Chain == chain && strings.EqualFold(node.Address, address) {
			return node, true
		}
	}
	return Node{}, false
}

func appendNode(nodes []Node, node Node) []Node {
	for index := range nodes {
		if nodes[index].Chain == node.Chain && strings.EqualFold(nodes[index].Address, node.Address) {
			if node.Terminal {
				nodes[index].Terminal, nodes[index].StopReason = true, node.StopReason
			}
			if node.AddressType != "" && node.AddressType != "unknown" {
				nodes[index].AddressType = node.AddressType
			}
			if node.Protocol != "" {
				nodes[index].Protocol = node.Protocol
			}
			for _, role := range node.Roles {
				if !containsRole(nodes[index].Roles, role) {
					nodes[index].Roles = append(nodes[index].Roles, role)
				}
			}
			return nodes
		}
	}
	return append(nodes, node)
}

func MergeResults(root, extension Result) Result {
	root.Labels = appendLabels(root.Labels, extension.Labels)
	for _, node := range extension.Nodes {
		root.Nodes = appendNode(root.Nodes, node)
	}
	edges := make(map[string]struct{}, len(root.Edges))
	for _, edge := range root.Edges {
		edges[edgeKey(edge)] = struct{}{}
	}
	for _, edge := range extension.Edges {
		if _, exists := edges[edgeKey(edge)]; !exists {
			root.Edges = append(root.Edges, edge)
			edges[edgeKey(edge)] = struct{}{}
		}
	}
	paths := make(map[string]struct{}, len(root.Paths))
	for _, path := range root.Paths {
		paths[strings.Join(path, "|")] = struct{}{}
	}
	for _, path := range extension.Paths {
		key := strings.Join(path, "|")
		if _, exists := paths[key]; !exists {
			root.Paths = append(root.Paths, path)
			paths[key] = struct{}{}
		}
	}
	if extension.DataStatus == "partial" {
		root.DataStatus = "partial"
	}
	root.MoneyTransfers = appendUniqueMoneyTransfers(root.MoneyTransfers, extension.MoneyTransfers)
	root.MoneyStates = appendUniqueMoneyStates(root.MoneyStates, extension.MoneyStates)
	states := append([]store.MoneyState(nil), root.MoneyStates...)
	consumeMoneyStates(states)
	root.MoneyStates = states
	root.Ledgers = buildLedgers(states)
	root.Reconciliation = "complete"
	for _, ledger := range root.Ledgers {
		if ledger.Status != "complete" {
			root.Reconciliation = "partial"
			break
		}
	}
	return root
}

func appendLabels(current, extra []store.Label) []store.Label {
	seen := make(map[string]struct{}, len(current))
	for _, label := range current {
		seen[label.Chain+"|"+strings.ToLower(label.Address)+"|"+label.Type+"|"+label.Source] = struct{}{}
	}
	for _, label := range extra {
		key := label.Chain + "|" + strings.ToLower(label.Address) + "|" + label.Type + "|" + label.Source
		if _, exists := seen[key]; !exists {
			current = append(current, label)
			seen[key] = struct{}{}
		}
	}
	return current
}

func edgeKey(edge Edge) string {
	return strings.Join([]string{edge.Chain, edge.TxHash, strings.ToLower(edge.From), strings.ToLower(edge.To), strings.ToLower(edge.Asset), edge.Kind, edge.TotalAmount, strconv.FormatInt(edge.FirstBlock, 10), strconv.FormatInt(edge.LatestBlock, 10), strings.Join(edge.Path, ">")}, "|")
}

func appendUniqueMoneyTransfers(current, extra []store.MoneyTransfer) []store.MoneyTransfer {
	seen := make(map[string]struct{}, len(current))
	for _, transfer := range current {
		seen[moneyTransferKey(transfer)] = struct{}{}
	}
	for _, transfer := range extra {
		if _, exists := seen[moneyTransferKey(transfer)]; !exists {
			current = append(current, transfer)
			seen[moneyTransferKey(transfer)] = struct{}{}
		}
	}
	return current
}

func appendUniqueMoneyStates(current, extra []store.MoneyState) []store.MoneyState {
	seen := make(map[string]struct{}, len(current))
	for _, state := range current {
		seen[moneyStateKey(state)] = struct{}{}
	}
	for _, state := range extra {
		if _, exists := seen[moneyStateKey(state)]; !exists {
			current = append(current, state)
			seen[moneyStateKey(state)] = struct{}{}
		}
	}
	return current
}

func moneyStateKey(state store.MoneyState) string {
	return strings.Join([]string{state.Chain, strings.ToLower(state.Address), state.Direction, strings.ToLower(state.Asset), state.EntryTxHash, strings.Join(state.Path, ">")}, "|")
}

func moneyTransferKey(transfer store.MoneyTransfer) string {
	return strings.Join([]string{transfer.Chain, transfer.TxHash, strings.ToLower(transfer.From), strings.ToLower(transfer.To), strings.ToLower(transfer.Asset)}, "|")
}
