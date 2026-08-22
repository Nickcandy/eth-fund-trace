import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import { AlertTriangle, CircleStop, Flame, Focus, Landmark } from "lucide-react";
import { chainLabel, shortAddress } from "../lib/format";

export interface FundNodeData extends Record<string, unknown> {
  address: string; chain: string; seed: boolean; terminal: boolean; hotWallet: boolean; risk: string; labelTypes: string[];
  onFocus: (chain: string, address: string) => void;
}
export type FundFlowNode = Node<FundNodeData, "fund">;

export function FundNode({ data, selected }: NodeProps<FundFlowNode>) {
  const state = data.risk === "high" ? "risk-high" : data.terminal ? "terminal" : data.hotWallet ? "hot-wallet" : data.seed ? "seed" : "normal";
  return (
    <div className={`fund-node ${state} ${selected ? "selected" : ""}`}>
      <Handle type="target" position={Position.Left} />
      <div className="node-heading"><span className={`chain-badge ${data.chain}`}>{chainLabel(data.chain)}</span>{data.seed && <span className="seed-label">查询中心</span>}</div>
      <div className="node-address" title={data.address}>{data.address === "0x0000000000000000000000000000000000000000" ? "零地址（铸造 / 销毁）" : shortAddress(data.address)}</div>
      <div className="node-signals">
        {data.risk === "high" && <span><AlertTriangle size={13} />高风险</span>}
        {data.terminal && <span><CircleStop size={13} />终点</span>}
        {data.hotWallet && <span><Flame size={13} />疑似热钱包</span>}
        {!data.risk.includes("high") && !data.terminal && !data.hotWallet && <span><Landmark size={13} />地址</span>}
      </div>
      <button className="node-focus" title="以此地址重新分析" onClick={(event) => { event.stopPropagation(); data.onFocus(data.chain, data.address); }}><Focus size={14} /></button>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
