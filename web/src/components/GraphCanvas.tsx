import {
  Background, BackgroundVariant, BaseEdge, Controls, EdgeLabelRenderer, MarkerType, Panel, ReactFlow, ReactFlowProvider,
  getBezierPath, useReactFlow, type Edge, type EdgeProps,
} from "@xyflow/react";
import { Download, Eye, EyeOff, FileJson, GitBranch, Layers, Maximize2, RotateCcw } from "lucide-react";
import { toPng } from "html-to-image";
import { useEffect, useMemo, useRef, useState } from "react";
import { formatAssetAmount, GRAPH_AMOUNT_FRACTION_DIGITS, shortAddress } from "../lib/format";
import { layoutGraph } from "../graph/layout";
import type { GraphEdgeModel, GraphModel } from "../graph/model";
import { FundNode, type FundFlowNode } from "./FundNode";

interface Props {
  model: GraphModel; aggregate: boolean; onAggregateChange: (value: boolean) => void;
  onSelectNode: (id: string) => void; onSelectEdge: (edge: GraphEdgeModel) => void;
  onFocusAddress: (chain: string, address: string) => void; onRelayout: () => void;
}

interface InteractiveEdgeData extends Record<string, unknown> { label: string; onSelect: () => void }
type InteractiveEdge = Edge<InteractiveEdgeData, "interactive">;
const nodeTypes = { fund: FundNode };
const edgeTypes = { interactive: InteractiveGraphEdge };

function InteractiveGraphEdge(props: EdgeProps<InteractiveEdge>) {
  const [path, labelX, labelY] = getBezierPath(props);
  return <><BaseEdge path={path} markerEnd={props.markerEnd} style={props.style}/><EdgeLabelRenderer><button className="edge-label nodrag nopan" style={{ transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)` }} onClick={(event)=>{event.stopPropagation();props.data?.onSelect()}}>{props.data?.label}</button></EdgeLabelRenderer></>;
}

function download(name: string, href: string) {
  const anchor = document.createElement("a"); anchor.download = name; anchor.href = href; anchor.click();
}

function Canvas({ model, aggregate, onAggregateChange, onSelectNode, onSelectEdge, onFocusAddress, onRelayout }: Props) {
  const [positions, setPositions] = useState(new Map<string, { x: number; y: number }>());
  const [visibleDepth, setVisibleDepth] = useState(5);
  const [showLowConfidence, setShowLowConfidence] = useState(true);
  const wrapper = useRef<HTMLDivElement>(null);
  const flow = useReactFlow();
  useEffect(() => { let active = true; layoutGraph(model).then((value) => { if (active) setPositions(value); }); return () => { active = false; }; }, [model]);
  useEffect(() => { if (positions.size) window.setTimeout(() => flow.fitView({ padding: 0.18, duration: 350 }), 20); }, [flow, positions]);

  const nodes = useMemo(() => model.nodes.filter((node) => Math.abs(node.hop) <= visibleDepth).map((node): FundFlowNode => ({
    id: node.id, type: "fund", position: positions.get(node.id) ?? { x: node.hop * 320, y: node.chain === "base" ? 180 : 0 },
    data: {
      ...node,
      risk: !showLowConfidence && (node.inferenceConfidence ?? 1) < 0.7 && node.risk === "suspected" ? "normal" : node.risk,
      labelTypes: !showLowConfidence && (node.inferenceConfidence ?? 1) < 0.7 ? [] : node.labelTypes,
      onFocus: onFocusAddress,
    },
  })), [model.nodes, onFocusAddress, positions, showLowConfidence, visibleDepth]);
  const nodeIDs = useMemo(() => new Set(nodes.map((node) => node.id)), [nodes]);
  const edgeLookup = useMemo(() => new Map(model.edges.map((edge) => [edge.id, edge])), [model.edges]);
  const edges = useMemo(() => model.edges.filter((edge) => nodeIDs.has(edge.source) && nodeIDs.has(edge.target)).map((edge): InteractiveEdge => {
    const internal = edge.sourceType === "txlistinternal";
    const token = edge.sourceType === "tokentx";
    const bridge = edge.kind === "bridge";
    const stroke = bridge ? "#ef8b2c" : token ? "#9b72e8" : internal ? "#20bfc5" : "#438bea";
    const amount = formatAssetAmount(edge.totalAmount, edge.decimals, shortAddress(edge.assetSymbol, 5), GRAPH_AMOUNT_FRACTION_DIGITS);
    const prefix = bridge ? `${edge.chain} · Bridge · ` : `${edge.kind === "mint" ? "铸造 · " : edge.kind === "burn" ? "销毁 · " : ""}${edge.count} 笔 · `;
    return {
      id: edge.id, source: edge.source, target: edge.target, type: "interactive", animated: bridge,
      data: { label: `${prefix}${amount}`, onSelect: () => onSelectEdge(edge) },
      style: { stroke, strokeWidth: bridge ? 3 : 2, strokeDasharray: bridge || internal ? "8 6" : undefined },
      markerEnd: { type: MarkerType.ArrowClosed, color: stroke, width: 16, height: 16 },
    };
  }), [model.edges, nodeIDs, onSelectEdge]);
  const exportPNG = async () => { if (wrapper.current) download("fund-trace.png", await toPng(wrapper.current, { backgroundColor: "#101317", pixelRatio: 2 })); };
  const exportJSON = () => download("fund-trace.json", URL.createObjectURL(new Blob([JSON.stringify(model, null, 2)], { type: "application/json" })));
  return (
    <div className="graph-canvas" ref={wrapper} data-testid="graph-canvas">
      <div className="swimlane ethereum-lane"><span>ETHEREUM</span></div><div className="swimlane base-lane"><span>BASE</span></div>
      <ReactFlow<FundFlowNode, InteractiveEdge> nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} minZoom={0.12} maxZoom={1.8} nodesDraggable onNodeClick={(_, node) => onSelectNode(node.id)} onEdgeClick={(_, edge) => { const value = edgeLookup.get(edge.id); if (value) onSelectEdge(value); }} fitView>
        <Background color="#2a3038" gap={26} size={1} variant={BackgroundVariant.Dots} />
        <Controls showInteractive={false} />
        <Panel position="top-left" className="graph-toolbar">
          <button title="适应视图" onClick={() => flow.fitView({ padding: .18, duration: 300 })}><Maximize2 size={16} /></button>
          <button title="重新布局" onClick={onRelayout}><RotateCcw size={16} /></button>
          <button className={aggregate ? "active" : ""} title="聚合事实边" onClick={() => onAggregateChange(!aggregate)}><Layers size={16} /><span>{aggregate ? "聚合" : "事实"}</span></button>
          <label title="显示的最大跳数"><GitBranch size={16} /><select aria-label="显示层级" value={visibleDepth} onChange={(event) => setVisibleDepth(Number(event.target.value))}>{[1,2,3,4,5].map((n) => <option key={n} value={n}>{n} 跳</option>)}</select></label>
          <button className={showLowConfidence ? "active" : ""} title="显示低置信度推断" onClick={() => setShowLowConfidence(!showLowConfidence)}>{showLowConfidence ? <Eye size={16}/> : <EyeOff size={16}/>}<span>低置信度</span></button>
          <button title="导出 PNG" onClick={exportPNG}><Download size={16} /></button>
          <button title="导出 JSON" onClick={exportJSON}><FileJson size={16} /></button>
        </Panel>
      </ReactFlow>
      {model.nodes.length > 500 && <div className="scale-notice">当前图含 {model.nodes.length} 个节点，已保持聚合显示</div>}
    </div>
  );
}

export function GraphCanvas(props: Props) { return <ReactFlowProvider><Canvas {...props} /></ReactFlowProvider>; }
