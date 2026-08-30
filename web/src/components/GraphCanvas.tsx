import {
  Background,
  BackgroundVariant,
  BaseEdge,
  Controls,
  EdgeLabelRenderer,
  MarkerType,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  getSmoothStepPath,
  useReactFlow,
  useUpdateNodeInternals,
  type Edge,
  type EdgeProps,
} from "@xyflow/react";
import "../graph-overrides.css";
import {
  CircleDollarSign,
  Download,
  FileJson,
  Maximize2,
  RotateCcw,
} from "lucide-react";
import { toPng } from "html-to-image";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  formatAssetAmount,
  GRAPH_AMOUNT_FRACTION_DIGITS,
} from "../lib/format";
import { layoutGraph, NODE_HEIGHT, NODE_WIDTH } from "../graph/layout";
import type {
  GraphEdgeModel,
  GraphModel,
  GraphNodeModel,
} from "../graph/model";
import { FundNode, type FundFlowNode } from "./FundNode";

interface Props {
  model: GraphModel;
  onSelectNode: (id: string) => void;
  onSelectEdge: (edge: GraphEdgeModel) => void;
  onFocusAddress: (chain: string, address: string) => void;
  onRelayout: () => void;
  onExpand?: (action: GraphExpansionAction) => void;
  extension?: { address?: string; direction?: "in" | "out"; status?: string };
}
export interface GraphExpansionAction {
  nodeId: string;
  chain: string;
  address: string;
  side: "left" | "right";
  mode: "expand" | "trace";
}

interface InteractiveEdgeData extends Record<string, unknown> {
  label: string;
  flow: NonNullable<GraphEdgeModel["flow"]>;
  showLabel: boolean;
  onSelect: () => void;
}
type InteractiveEdge = Edge<InteractiveEdgeData, "interactive">;
const nodeTypes = { fund: FundNode };
const edgeTypes = { interactive: InteractiveGraphEdge };
export type AssetFilter = "all" | "ETH" | "BTC" | "USDT" | "erc20";

export function edgeLabelVisible(
  showAll: boolean,
  selected: boolean,
  touchesSeed: boolean,
) {
  return showAll || selected || touchesSeed;
}
export function thorchainEdgeLabel(action?: string) {
  return (
    (
      {
        router_inbound: "进入 THORChain Router",
        vault_migration: "THORChain Vault 迁移",
        protocol_outbound: "THORChain 协议出站",
        cross_chain_swap: "THORChain 跨链兑换",
        refund: "THORChain 退款",
      } as Record<string, string>
    )[action ?? ""] ?? "THORChain 协议转移"
  );
}

function displayAssetSymbol(symbol: string | undefined): string | undefined {
  if (!symbol || /^0x[0-9a-f]{40}$/i.test(symbol)) return undefined;
  return symbol;
}
export function matchesAssetFilter(edge: GraphEdgeModel, filter: AssetFilter) {
  if (filter === "all") return true;
  if (edge.swapLegs?.length)
    return edge.swapLegs.some((leg) =>
      matchesAsset(leg.assetType, leg.asset, leg.assetSymbol, filter),
    );
  return matchesAsset(edge.assetType, edge.asset, edge.assetSymbol, filter);
}

function matchesAsset(
  assetType: string | undefined,
  asset: string,
  symbol: string,
  filter: Exclude<AssetFilter, "all">,
) {
  if (filter === "ETH")
    return (
      assetType === "eth" ||
      assetType === "native" ||
      asset.toUpperCase() === "ETH"
    );
  if (filter === "BTC") return symbol.toUpperCase() === "BTC";
  if (filter === "USDT") return symbol.toUpperCase() === "USDT";
  return assetType === "erc20" && symbol.toUpperCase() !== "USDT";
}

export function branchNodeIDs(
  nodeID: string,
  side: "left" | "right",
  nodes: GraphNodeModel[],
  edges: GraphEdgeModel[],
): string[] {
  if (!nodes.some((node) => node.id === nodeID)) return [];
  const result = new Set<string>();
  for (const edge of edges) {
    const other =
      side === "right" && edge.source === nodeID
        ? edge.target
        : side === "left" && edge.target === nodeID
          ? edge.source
          : undefined;
    if (other) result.add(other);
  }
  return [...result];
}

export function expansionMode(
  node: GraphNodeModel,
  rightBranches: string[],
): "expand" | "trace" | "none" {
  if (rightBranches.length > 0) return "expand";
  return node.terminal || node.seed ? "none" : "trace";
}

export function expansionPathKeys(
  targetID: string,
  side: "left" | "right",
  nodes: GraphNodeModel[],
  edges: GraphEdgeModel[],
): string[] {
  const nodeByID = new Map(nodes.map((node) => [node.id, node]));
  const keys: string[] = [`${targetID}:${side}`];
  let current = nodeByID.get(targetID);
  const visited = new Set<string>();
  while (current && !current.seed && !visited.has(current.id)) {
    visited.add(current.id);
    const parent = edges
      .flatMap((edge) =>
        edge.source === current!.id
          ? [edge.target]
          : edge.target === current!.id
            ? [edge.source]
            : [],
      )
      .map((id) => nodeByID.get(id))
      .filter(
        (node): node is GraphNodeModel =>
          !!node && Math.abs(node.hop) < Math.abs(current!.hop),
      )
      .sort((left, right) => Math.abs(right.hop) - Math.abs(left.hop))[0];
    if (!parent) break;
    keys.push(`${parent.id}:${side}`);
    current = parent;
  }
  return keys;
}

export function revealedNodeIDs(
  nodes: GraphNodeModel[],
  edges: GraphEdgeModel[],
  expanded: Set<string>,
): Set<string> {
  const revealed = new Set(
    nodes.filter((node) => node.seed).map((node) => node.id),
  );
  for (const key of expanded) {
    const split = key.lastIndexOf(":");
    const nodeID = key.slice(0, split);
    const side = key.slice(split + 1) as "left" | "right";
    for (const branchID of branchNodeIDs(nodeID, side, nodes, edges))
      revealed.add(branchID);
  }
  return revealed;
}

function InteractiveGraphEdge(props: EdgeProps<InteractiveEdge>) {
  const [path, labelX, labelY] = getSmoothStepPath({
    ...props,
    borderRadius: 10,
    offset: 28,
  });
  const style = {
    ...props.style,
    strokeWidth: props.selected ? 4 : props.style?.strokeWidth,
  };
  const showLabel = props.selected || props.data?.showLabel;
  return (
    <>
      <BaseEdge
        path={path}
        markerStart={props.markerStart}
        markerEnd={props.markerEnd}
        style={style}
        interactionWidth={24}
      />
      {showLabel && (
        <EdgeLabelRenderer>
          <button
            className={`edge-label ${props.data?.flow ?? "return"} nodrag nopan`}
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
            }}
            onClick={(event) => {
              event.stopPropagation();
              props.data?.onSelect();
            }}
          >
            {props.data?.label}
          </button>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

function download(name: string, href: string) {
  const anchor = document.createElement("a");
  anchor.download = name;
  anchor.href = href;
  anchor.click();
}

function Canvas({
  model,
  onSelectNode,
  onSelectEdge,
  onFocusAddress,
  onRelayout,
  onExpand,
  extension,
}: Props) {
  const [positions, setPositions] = useState(
    new Map<string, { x: number; y: number }>(),
  );
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const showLowConfidence = true;
  const [assetFilter, setAssetFilter] = useState<AssetFilter>("all");
  const [showEdgeLabels, setShowEdgeLabels] = useState(true);
  const [selectedEdgeID, setSelectedEdgeID] = useState<string>();
  const wrapper = useRef<HTMLDivElement>(null);
  const initialViewFitted = useRef(false);
  const flow = useReactFlow();
  const updateNodeInternals = useUpdateNodeInternals();
  useEffect(() => {
    let active = true;
    layoutGraph(model).then((value) => {
      if (active) setPositions(value);
    });
    return () => {
      active = false;
    };
  }, [model]);
  useEffect(() => {
    setSelectedEdgeID(undefined);
  }, [model]);
  useEffect(() => {
    if (!positions.size || initialViewFitted.current) return;
    initialViewFitted.current = true;
    window.setTimeout(() => flow.fitView({ padding: 0.18, duration: 350 }), 20);
  }, [flow, positions]);
  useEffect(() => {
    if (positions.size)
      window.setTimeout(() => updateNodeInternals([...positions.keys()]), 40);
  }, [positions, updateNodeInternals]);
  useEffect(() => {
    if (
      !extension?.address ||
      !["succeeded", "partial"].includes(extension.status ?? "")
    )
      return;
    const node = model.nodes.find(
      (item) => item.address.toLowerCase() === extension.address?.toLowerCase(),
    );
    if (!node) return;
    const side = extension.direction === "in" ? "left" : "right";
    const keys = expansionPathKeys(node.id, side, model.nodes, model.edges);
    setExpanded((current) => {
      const next = new Set(current);
      for (const key of keys) next.add(key);
      return next;
    });
  }, [
    extension?.address,
    extension?.direction,
    extension?.status,
    model.edges,
    model.nodes,
  ]);

  const filteredModelEdges = useMemo(
    () => model.edges.filter((edge) => matchesAssetFilter(edge, assetFilter)),
    [assetFilter, model.edges],
  );
  const nodes = useMemo(() => {
    const revealed = revealedNodeIDs(model.nodes, filteredModelEdges, expanded);
    return model.nodes
      .filter((node) => revealed.has(node.id))
      .map((node): FundFlowNode => {
        const controls = (["left", "right"] as const).map((side) => {
          const outward =
            node.hop === 0 ||
            (side === "left" && node.hop < 0) ||
            (side === "right" && node.hop > 0);
          if (!outward) return { mode: "none", expanded: false } as const;
          const branches = branchNodeIDs(
            node.id,
            side,
            model.nodes,
            filteredModelEdges,
          );
          const direction = side === "left" ? "in" : "out";
          const matchesExtension =
            extension?.address?.toLowerCase() === node.address.toLowerCase() &&
            extension.direction === direction;
          return {
            mode: expansionMode(node, branches),
            expanded: expanded.has(`${node.id}:${side}`),
            status:
              matchesExtension &&
              ["queued", "waiting_sync", "running"].includes(
                extension?.status ?? "",
              )
                ? ("running" as const)
                : matchesExtension && extension?.status === "failed"
                  ? ("failed" as const)
                  : undefined,
            disabled:
              !matchesExtension &&
              ["queued", "waiting_sync", "running"].includes(
                extension?.status ?? "",
              ),
          };
        });
        return {
          id: node.id,
          type: "fund",
          initialWidth: NODE_WIDTH,
          initialHeight: NODE_HEIGHT,
          position: positions.get(node.id) ?? { x: node.hop * 320, y: 0 },
          data: {
            ...node,
            risk:
              !showLowConfidence &&
              (node.inferenceConfidence ?? 1) < 0.7 &&
              node.risk === "suspected"
                ? "normal"
                : node.risk,
            labelTypes:
              !showLowConfidence && (node.inferenceConfidence ?? 1) < 0.7
                ? []
                : node.labelTypes,
            onFocus: onFocusAddress,
            onExpand: (side) => {
              const control = side === "left" ? controls[0] : controls[1];
              if (control.mode === "trace") {
                onExpand?.({
                  nodeId: node.id,
                  chain: node.chain,
                  address: node.address,
                  side,
                  mode: "trace",
                });
                return;
              }
              const key = `${node.id}:${side}`;
              setExpanded((current) => {
                const next = new Set(current);
                next.has(key) ? next.delete(key) : next.add(key);
                return next;
              });
              onExpand?.({
                nodeId: node.id,
                chain: node.chain,
                address: node.address,
                side,
                mode: "expand",
              });
            },
            leftExpansion: controls[0],
            rightExpansion: controls[1],
          },
        };
      });
  }, [
    expanded,
    extension,
    filteredModelEdges,
    model.nodes,
    onExpand,
    onFocusAddress,
    positions,
    showLowConfidence,
  ]);
  const nodeIDs = useMemo(() => new Set(nodes.map((node) => node.id)), [nodes]);
  const seedIDs = useMemo(
    () =>
      new Set(model.nodes.filter((node) => node.seed).map((node) => node.id)),
    [model.nodes],
  );
  const nodeHops = useMemo(
    () => new Map(model.nodes.map((node) => [node.id, node.hop])),
    [model.nodes],
  );
  const edgeLookup = useMemo(
    () => new Map(filteredModelEdges.map((edge) => [edge.id, edge])),
    [filteredModelEdges],
  );
  const edges = useMemo(
    () =>
      filteredModelEdges
        .filter((edge) => nodeIDs.has(edge.source) && nodeIDs.has(edge.target))
        .map((edge): InteractiveEdge => {
          const swap = edge.bidirectional && edge.swapLegs?.length === 2;
          const flowDirection = edge.flow ?? "return";
          const stroke = swap
            ? "#59c98c"
            : flowDirection === "inbound"
              ? "#2fb6a8"
              : flowDirection === "outbound"
                ? "#438bea"
                : "#d0a44c";
          const amount = formatAssetAmount(
            edge.totalAmount,
            edge.decimals,
            displayAssetSymbol(edge.assetSymbol),
            GRAPH_AMOUNT_FRACTION_DIGITS,
          );
          const swapAmount = edge.swapLegs
            ?.map((leg) =>
                formatAssetAmount(
                  leg.totalAmount,
                  leg.decimals,
                  displayAssetSymbol(leg.assetSymbol),
                  GRAPH_AMOUNT_FRACTION_DIGITS,
                ),
            )
            .join(" ⇄ ");
          const directionLabel =
            flowDirection === "inbound"
              ? "流入"
              : flowDirection === "outbound"
                ? "流出"
                : "逆向";
          const protocolKind =
            edge.protocol === "thorchain"
              ? `${thorchainEdgeLabel(edge.protocolAction)} · `
              : "";
          const prefix = swap
            ? `${edge.chain} · Swap · `
            : `${directionLabel} · ${protocolKind}${edge.kind === "mint" ? "铸造 · " : edge.kind === "burn" ? "销毁 · " : ""}${edge.count} 笔 · `;
          const sourceHop = nodeHops.get(edge.source) ?? 0;
          const targetHop = nodeHops.get(edge.target) ?? 0;
          const leftToRight = targetHop >= sourceHop;
          return {
            id: edge.id,
            source: edge.source,
            target: edge.target,
            type: "interactive",
            sourceHandle: leftToRight ? "source-right" : "source-left",
            targetHandle: leftToRight ? "target-left" : "target-right",
            selected: selectedEdgeID === edge.id,
            style: { stroke, strokeWidth: 2 },
            markerStart: swap
              ? {
                  type: MarkerType.ArrowClosed,
                  color: stroke,
                  width: 16,
                  height: 16,
                }
              : undefined,
            markerEnd: {
              type: MarkerType.ArrowClosed,
              color: stroke,
              width: 16,
              height: 16,
            },
            data: {
              label: `${prefix}${swapAmount ?? amount}`,
              flow: flowDirection,
              showLabel: edgeLabelVisible(
                showEdgeLabels,
                selectedEdgeID === edge.id,
                seedIDs.has(edge.source) || seedIDs.has(edge.target),
              ),
              onSelect: () => {
                setSelectedEdgeID(edge.id);
                onSelectEdge(edge);
              },
            },
          };
        }),
    [
      filteredModelEdges,
      nodeHops,
      nodeIDs,
      onSelectEdge,
      seedIDs,
      selectedEdgeID,
      showEdgeLabels,
    ],
  );
  const exportPNG = async () => {
    if (wrapper.current)
      download(
        "fund-trace.png",
        await toPng(wrapper.current, {
          backgroundColor: "#101317",
          pixelRatio: 2,
        }),
      );
  };
  const exportJSON = () =>
    download(
      "fund-trace.json",
      URL.createObjectURL(
        new Blob([JSON.stringify(model, null, 2)], {
          type: "application/json",
        }),
      ),
    );
  return (
    <div className="graph-canvas" ref={wrapper} data-testid="graph-canvas">
      <div className="swimlane ethereum-lane">
        <span>ETHEREUM</span>
      </div>
      <div className="swimlane bitcoin-lane">
        <span>BITCOIN</span>
      </div>
      <ReactFlow<FundFlowNode, InteractiveEdge>
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        minZoom={0.12}
        maxZoom={1.8}
        nodesDraggable
        onNodeClick={(_, node) => {
          setSelectedEdgeID(undefined);
          onSelectNode(node.id);
        }}
        onEdgeClick={(_, edge) => {
          const value = edgeLookup.get(edge.id);
          if (value) {
            setSelectedEdgeID(edge.id);
            onSelectEdge(value);
          }
        }}
        onPaneClick={() => setSelectedEdgeID(undefined)}
        fitView
      >
        <Background
          color="#2a3038"
          gap={26}
          size={1}
          variant={BackgroundVariant.Dots}
        />
        <Controls showInteractive={false} />
        <Panel position="top-left" className="graph-toolbar">
          <button
            title="适应视图"
            onClick={() => flow.fitView({ padding: 0.18, duration: 300 })}
          >
            <Maximize2 size={16} />
          </button>
          <button title="重新布局" onClick={onRelayout}>
            <RotateCcw size={16} />
          </button>
          <button
            className={showEdgeLabels ? "active" : ""}
            title={showEdgeLabels ? "隐藏全部边金额" : "显示全部边金额"}
            onClick={() => setShowEdgeLabels(!showEdgeLabels)}
          >
            <CircleDollarSign size={16} />
            <span>金额</span>
          </button>
          <label title="筛选图中资产">
            <CircleDollarSign size={16} />
            <select
              aria-label="资产筛选"
              value={assetFilter}
              onChange={(event) => {
                setAssetFilter(event.target.value as AssetFilter);
                setSelectedEdgeID(undefined);
              }}
            >
              {
                <>
                  <option value="all">全部资产</option>
                  <option value="ETH">ETH</option>
                  <option value="BTC">BTC</option>
                  <option value="USDT">USDT</option>
                  <option value="erc20">其他 ERC-20</option>
                </>
              }
            </select>
          </label>
          <button title="导出 PNG" onClick={exportPNG}>
            <Download size={16} />
          </button>
          <button title="导出 JSON" onClick={exportJSON}>
            <FileJson size={16} />
          </button>
        </Panel>
        <Panel position="top-center" className="flow-legend">
          <span>
            <i className="inbound" />
            流入查询中心
          </span>
          <span>
            <i className="outbound" />
            从查询中心流出
          </span>
          <span>
            <i className="return" />
            逆向或同层
          </span>
        </Panel>
      </ReactFlow>
      {model.nodes.length > 500 && (
        <div className="scale-notice">
          当前图含 {model.nodes.length} 个节点，已保持聚合显示
        </div>
      )}
    </div>
  );
}

export function GraphCanvas(props: Props) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}
