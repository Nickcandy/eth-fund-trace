import {
  Background, BackgroundVariant, BaseEdge, Controls, EdgeLabelRenderer, MarkerType, Panel, ReactFlow, ReactFlowProvider,
  getSmoothStepPath, useReactFlow, type Edge, type EdgeProps,
} from "@xyflow/react";
import { CircleDollarSign, Download, Eye, EyeOff, FileJson, GitBranch, Maximize2, RotateCcw } from "lucide-react";
import { toPng } from "html-to-image";
import { useEffect, useMemo, useRef, useState } from "react";
import { formatAssetAmount, GRAPH_AMOUNT_FRACTION_DIGITS, shortAddress } from "../lib/format";
import { layoutGraph } from "../graph/layout";
import type { GraphEdgeModel, GraphModel } from "../graph/model";
import { FundNode, type FundFlowNode } from "./FundNode";

interface Props {
  model: GraphModel;
  onSelectNode: (id: string) => void; onSelectEdge: (edge: GraphEdgeModel) => void;
  onFocusAddress: (chain: string, address: string) => void; onRelayout: () => void;
}

interface InteractiveEdgeData extends Record<string, unknown> {
  label: string; flow: NonNullable<GraphEdgeModel["flow"]>; showLabel: boolean; onSelect: () => void;
}
type InteractiveEdge = Edge<InteractiveEdgeData, "interactive">;
const nodeTypes = { fund: FundNode };
const edgeTypes = { interactive: InteractiveGraphEdge };
const DENSE_GRAPH_EDGE_LIMIT = 20;
export type AssetFilter = "all" | "ETH" | "USDT" | "erc20";

export function labelsVisibleByDefault(edgeCount: number) { return edgeCount <= DENSE_GRAPH_EDGE_LIMIT; }
export function edgeLabelVisible(showAll: boolean, selected: boolean, touchesSeed: boolean) { return showAll || selected || touchesSeed; }
export function matchesAssetFilter(edge: GraphEdgeModel, filter: AssetFilter) {
  if (filter === "all") return true;
  if (edge.kind === "bridge") return false;
  if (filter === "ETH") return edge.assetType === "eth" || edge.assetType === "native" || edge.asset.toUpperCase() === "ETH";
  if (filter === "USDT") return edge.assetSymbol.toUpperCase() === "USDT";
  return edge.assetType === "erc20" && edge.assetSymbol.toUpperCase() !== "USDT";
}

function InteractiveGraphEdge(props: EdgeProps<InteractiveEdge>) {
  const [path, labelX, labelY] = getSmoothStepPath({ ...props, borderRadius: 10, offset: 28 });
  const style = { ...props.style, strokeWidth: props.selected ? 4 : props.style?.strokeWidth };
  const showLabel = props.selected || props.data?.showLabel;
  return <><BaseEdge path={path} markerEnd={props.markerEnd} style={style} interactionWidth={24}/>{showLabel&&<EdgeLabelRenderer><button className={`edge-label ${props.data?.flow??"return"} nodrag nopan`} style={{ transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)` }} onClick={(event)=>{event.stopPropagation();props.data?.onSelect()}}>{props.data?.label}</button></EdgeLabelRenderer>}</>;
}

function download(name: string, href: string) {
  const anchor = document.createElement("a"); anchor.download = name; anchor.href = href; anchor.click();
}

function Canvas({ model, onSelectNode, onSelectEdge, onFocusAddress, onRelayout }: Props) {
  const [positions, setPositions] = useState(new Map<string, { x: number; y: number }>());
  const [visibleDepth, setVisibleDepth] = useState(5);
  const [showLowConfidence, setShowLowConfidence] = useState(true);
  const [assetFilter, setAssetFilter] = useState<AssetFilter>("all");
  const [showEdgeLabels, setShowEdgeLabels] = useState(() => labelsVisibleByDefault(model.edges.length));
  const [selectedEdgeID, setSelectedEdgeID] = useState<string>();
  const wrapper = useRef<HTMLDivElement>(null);
  const flow = useReactFlow();
  useEffect(() => { let active = true; layoutGraph(model).then((value) => { if (active) setPositions(value); }); return () => { active = false; }; }, [model]);
  useEffect(() => { setShowEdgeLabels(labelsVisibleByDefault(model.edges.length)); setSelectedEdgeID(undefined); }, [model]);
  useEffect(() => { if (positions.size) window.setTimeout(() => flow.fitView({ padding: 0.18, duration: 350 }), 20); }, [flow, positions]);

  const filteredModelEdges = useMemo(() => model.edges.filter((edge) => matchesAssetFilter(edge, assetFilter)), [assetFilter, model.edges]);
  const nodes = useMemo(() => {
    const connected = new Set(filteredModelEdges.flatMap((edge) => [edge.source, edge.target]));
    return model.nodes.filter((node) => (node.seed || connected.has(node.id)) && Math.abs(node.hop) <= visibleDepth).map((node): FundFlowNode => ({
    id: node.id, type: "fund", position: positions.get(node.id) ?? { x: node.hop * 320, y: node.chain === "base" ? 180 : 0 },
    data: {
      ...node,
      risk: !showLowConfidence && (node.inferenceConfidence ?? 1) < 0.7 && node.risk === "suspected" ? "normal" : node.risk,
      labelTypes: !showLowConfidence && (node.inferenceConfidence ?? 1) < 0.7 ? [] : node.labelTypes,
      onFocus: onFocusAddress,
    },
    }));
  }, [filteredModelEdges, model.nodes, onFocusAddress, positions, showLowConfidence, visibleDepth]);
  const nodeIDs = useMemo(() => new Set(nodes.map((node) => node.id)), [nodes]);
  const seedIDs = useMemo(() => new Set(model.nodes.filter((node) => node.seed).map((node) => node.id)), [model.nodes]);
  const nodeHops = useMemo(() => new Map(model.nodes.map((node) => [node.id, node.hop])), [model.nodes]);
  const edgeLookup = useMemo(() => new Map(filteredModelEdges.map((edge) => [edge.id, edge])), [filteredModelEdges]);
  const edges = useMemo(() => filteredModelEdges.filter((edge) => nodeIDs.has(edge.source) && nodeIDs.has(edge.target)).map((edge): InteractiveEdge => {
    const bridge = edge.kind === "bridge";
    const flowDirection = edge.flow ?? "return";
    const stroke = bridge ? "#ef8b2c" : flowDirection === "inbound" ? "#2fb6a8" : flowDirection === "outbound" ? "#438bea" : "#d0a44c";
    const amount = formatAssetAmount(edge.totalAmount, edge.decimals, shortAddress(edge.assetSymbol, 5), GRAPH_AMOUNT_FRACTION_DIGITS);
    const directionLabel = flowDirection === "inbound" ? "流入" : flowDirection === "outbound" ? "流出" : "逆向";
    const prefix = bridge ? `${edge.chain} · Bridge · ` : `${directionLabel} · ${edge.kind === "mint" ? "铸造 · " : edge.kind === "burn" ? "销毁 · " : ""}${edge.count} 笔 · `;
    const sourceHop = nodeHops.get(edge.source) ?? 0; const targetHop = nodeHops.get(edge.target) ?? 0;
    const leftToRight = targetHop >= sourceHop;
    return {
      id: edge.id, source: edge.source, target: edge.target, type: "interactive", animated: bridge,
      sourceHandle: leftToRight ? "source-right" : "source-left", targetHandle: leftToRight ? "target-left" : "target-right",
      selected: selectedEdgeID === edge.id,
      data: { label: `${prefix}${amount}`, flow: flowDirection, showLabel: edgeLabelVisible(showEdgeLabels, selectedEdgeID === edge.id, seedIDs.has(edge.source) || seedIDs.has(edge.target)), onSelect: () => { setSelectedEdgeID(edge.id); onSelectEdge(edge); } },
      style: { stroke, strokeWidth: bridge ? 3 : 2, strokeDasharray: bridge ? "8 6" : undefined },
      markerEnd: { type: MarkerType.ArrowClosed, color: stroke, width: 16, height: 16 },
    };
  }), [filteredModelEdges, nodeHops, nodeIDs, onSelectEdge, seedIDs, selectedEdgeID, showEdgeLabels]);
  const exportPNG = async () => { if (wrapper.current) download("fund-trace.png", await toPng(wrapper.current, { backgroundColor: "#101317", pixelRatio: 2 })); };
  const exportJSON = () => download("fund-trace.json", URL.createObjectURL(new Blob([JSON.stringify(model, null, 2)], { type: "application/json" })));
  return (
    <div className="graph-canvas" ref={wrapper} data-testid="graph-canvas">
      <div className="swimlane ethereum-lane"><span>ETHEREUM</span></div><div className="swimlane base-lane"><span>BASE</span></div>
      <ReactFlow<FundFlowNode, InteractiveEdge> nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} minZoom={0.12} maxZoom={1.8} nodesDraggable onNodeClick={(_, node) => { setSelectedEdgeID(undefined); onSelectNode(node.id); }} onEdgeClick={(_, edge) => { const value = edgeLookup.get(edge.id); if (value) { setSelectedEdgeID(edge.id); onSelectEdge(value); } }} onPaneClick={()=>setSelectedEdgeID(undefined)} fitView>
        <Background color="#2a3038" gap={26} size={1} variant={BackgroundVariant.Dots} />
        <Controls showInteractive={false} />
        <Panel position="top-left" className="graph-toolbar">
          <button title="适应视图" onClick={() => flow.fitView({ padding: .18, duration: 300 })}><Maximize2 size={16} /></button>
          <button title="重新布局" onClick={onRelayout}><RotateCcw size={16} /></button>
          <label title="显示的最大跳数"><GitBranch size={16} /><select aria-label="显示层级" value={visibleDepth} onChange={(event) => setVisibleDepth(Number(event.target.value))}>{[1,2,3,4,5].map((n) => <option key={n} value={n}>{n} 跳</option>)}</select></label>
          <button className={showLowConfidence ? "active" : ""} title="显示低置信度推断" onClick={() => setShowLowConfidence(!showLowConfidence)}>{showLowConfidence ? <Eye size={16}/> : <EyeOff size={16}/>}<span>低置信度</span></button>
          <button className={showEdgeLabels ? "active" : ""} title={showEdgeLabels?"隐藏全部边金额":"显示全部边金额"} onClick={() => setShowEdgeLabels(!showEdgeLabels)}><CircleDollarSign size={16}/><span>金额</span></button>
          <label title="筛选图中资产"><CircleDollarSign size={16}/><select aria-label="资产筛选" value={assetFilter} onChange={(event) => { setAssetFilter(event.target.value as AssetFilter); setSelectedEdgeID(undefined); }}>{<><option value="all">全部资产</option><option value="ETH">ETH</option><option value="USDT">USDT</option><option value="erc20">其他 ERC-20</option></>}</select></label>
          <button title="导出 PNG" onClick={exportPNG}><Download size={16} /></button>
          <button title="导出 JSON" onClick={exportJSON}><FileJson size={16} /></button>
        </Panel>
        <Panel position="top-center" className="flow-legend"><span><i className="inbound"/>流入查询中心</span><span><i className="outbound"/>从查询中心流出</span><span><i className="return"/>逆向或同层</span></Panel>
      </ReactFlow>
      {model.nodes.length > 500 && <div className="scale-notice">当前图含 {model.nodes.length} 个节点，已保持聚合显示</div>}
    </div>
  );
}

export function GraphCanvas(props: Props) { return <ReactFlowProvider><Canvas {...props} /></ReactFlowProvider>; }
