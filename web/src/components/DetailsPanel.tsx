import { ChevronRight, ExternalLink, ShieldAlert, Tag, X } from "lucide-react";
import type { AddressProfile, AddressResponse, Label, RiskResult } from "../api/types";
import type { GraphEdgeModel, GraphNodeModel } from "../graph/model";
import { chainLabel, formatChainAmount, shortAddress } from "../lib/format";

interface Props { node?: GraphNodeModel; edge?: GraphEdgeModel; address?: AddressResponse; profile?: AddressProfile; labels: Label[]; risk?: RiskResult; onClose: () => void; onFocus: (chain: string, address: string) => void }

export function DetailsPanel({ node, edge, address, profile, labels, risk, onClose, onFocus }: Props) {
  if (!node && !edge) return null;
  return <aside className="details-panel" aria-label="链路详情">
    <header><div><span className="eyebrow">链路证据</span><h2>{node ? "地址详情" : "资金边详情"}</h2></div><button title="关闭详情" onClick={onClose}><X size={18} /></button></header>
    {node && <>
      <section className="identity-block"><span className={`chain-badge ${node.chain}`}>{chainLabel(node.chain)}</span><code>{node.address}</code><button className="text-command" onClick={() => onFocus(node.chain, node.address)}>以此地址分析 <ChevronRight size={15} /></button></section>
      <section className="risk-summary"><div className={`risk-score ${risk?.level ?? "none"}`}><strong>{risk?.score ?? 0}</strong><span>风险分</span></div><div><h3>{risk?.level === "known_high" ? "已知高风险" : risk?.level === "suspected" ? "疑似风险" : "暂无结论"}</h3><p>{risk?.ruleVersion ?? "risk-v1"} · {risk?.propagationVersion ?? "propagation-v1"}</p></div></section>
      <section><h3>画像</h3><div className="metric-grid"><Metric label="生命周期交易" value={profile?.features.lifetimeTransfers ?? "-"}/><Metric label="30 日交易" value={profile?.features.windowTransfers ?? "-"}/><Metric label="对手方" value={profile?.features.uniqueCounterparties ?? "-"}/><Metric label="活跃天" value={profile?.features.activeDays ?? "-"}/><Metric label="转入" value={profile?.features.incoming ?? "-"}/><Metric label="转出" value={profile?.features.outgoing ?? "-"}/></div><p className="muted">{profile?.classification ?? "画像尚未加载"} · {profile?.ruleVersion ?? "hot-wallet-v1"}</p></section>
      <section><h3><Tag size={15}/> 确定性标签</h3>{labels.length ? labels.map((label) => <div className="evidence-row" key={`${label.type}-${label.source}`}><span>{label.type}</span><strong>{Math.round(label.confidence * 100)}%</strong><small>{label.source}</small></div>) : <p className="empty-copy">无人工或公开标签</p>}</section>
      <section><h3><ShieldAlert size={15}/> 风险证据</h3>{risk?.evidence?.length ? risk.evidence.map((item, i) => <div className="evidence-card" key={`${item.address}-${i}`}><div><strong>{item.labelType}</strong><span>{item.score} 分</span></div><p>{item.direction} · {item.distance} 跳 · 置信度 {Math.round(item.confidence * 100)}%</p>{item.txHashes.map((hash) => <code key={hash}>{shortAddress(hash, 10)}</code>)}</div>) : <p className="empty-copy">当前规则未发现风险证据</p>}</section>
      {address?.address && <section><h3>同步状态</h3><p>{address.address.syncStatus} · 区块 {address.address.latestSyncedBlock.toLocaleString()}</p></section>}
    </>}
    {edge && <><section className="edge-summary"><span>{edge.sourceType}</span><strong>{edge.count} 笔 {edge.assetSymbol}</strong><p>{formatChainAmount(edge.totalAmount, edge.decimals)}{edge.decimals === undefined && " · 精度未知"}</p></section><section><h3>交易哈希</h3>{edge.facts.map((fact) => <div className="tx-row" key={`${fact.txHash}-${fact.logIndex}`}><code>{shortAddress(fact.txHash, 10)}</code><span>#{fact.blockNumber}</span><ExternalLink size={13}/></div>)}{edge.bridge && <div className="evidence-card"><strong>确认式桥接</strong><p>{edge.bridge.link.sourceChain} → {edge.bridge.link.targetChain}</p>{edge.bridge.link.evidence.map((item) => <code key={item}>{item}</code>)}</div>}</section></>}
  </aside>;
}
function Metric({ label, value }: { label: string; value: string | number }) { return <div><span>{label}</span><strong>{value}</strong></div>; }
