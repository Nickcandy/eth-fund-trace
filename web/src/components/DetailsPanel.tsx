import { ArrowRight, ChevronRight, Tag, X } from "lucide-react";
import { useState } from "react";
import type {
  AddressProfile,
  AddressResponse,
  Label,
  LabelInput,
  NativeBalance,
  RiskResult,
} from "../api/types";
import type { GraphEdgeModel, GraphNodeModel } from "../graph/model";
import {
  chainLabel,
  DETAIL_AMOUNT_FRACTION_DIGITS,
  formatAssetAmount,
  shortAddress,
  stopReasonLabel,
} from "../lib/format";
import { LabelForm } from "./LabelForm";

interface Props {
  node?: GraphNodeModel;
  edge?: GraphEdgeModel;
  address?: AddressResponse;
  balance?: NativeBalance;
  balanceLoading?: boolean;
  balanceError?: string;
  profile?: AddressProfile;
  labels: Label[];
  labelsLoading?: boolean;
  labelsError?: string;
  risk?: RiskResult;
  onLabel: (input: LabelInput) => Promise<boolean | void>;
  onClose: () => void;
  onFocus: (chain: string, address: string) => void;
}

export function DetailsPanel({
  node,
  edge,
  address,
  balance,
  balanceLoading,
  balanceError,
  profile,
  labels,
  labelsLoading,
  labelsError,
  risk,
  onLabel,
  onClose,
  onFocus,
}: Props) {
  const [tab, setTab] = useState<"overview" | "labels">("overview");
  if (!node && !edge) return null;
  const presentation = node ? riskPresentation(labels, risk) : undefined;
  return (
    <aside className="details-panel" aria-label="链路详情">
      <header>
        <div>
          <span className="eyebrow">链路证据</span>
          <h2>{node ? "地址详情" : "资金边详情"}</h2>
        </div>
        <button title="关闭详情" onClick={onClose}>
          <X size={18} />
        </button>
      </header>
      {node && (
        <>
          <section className="identity-block">
            <span className={`chain-badge ${node.chain}`}>
              {chainLabel(node.chain)}
            </span>
            {node.chain === "bitcoin" ? (
              <a
                href={`https://mempool.space/address/${node.address}`}
                target="_blank"
                rel="noreferrer"
              >
                <code>{node.address}</code>
              </a>
            ) : (
              <code>{node.address}</code>
            )}
            {node.roles?.map((role) => (
              <strong key={role}>{roleName(role)}</strong>
            ))}
            {node.protocol && (
              <small>
                {node.protocol} · {node.addressType ?? "unknown"}
              </small>
            )}
            {node.terminal && <small>{stopReasonLabel(node.stopReason)}</small>}
            {node.chain === "ethereum" && (
              <button
                className="text-command"
                onClick={() => onFocus(node.chain, node.address)}
              >
                以此地址分析 <ChevronRight size={15} />
              </button>
            )}
          </section>
          {node.chain === "ethereum" && (
            <nav
              className="details-tabs"
              role="tablist"
              aria-label="地址详情视图"
            >
              <button
                role="tab"
                aria-selected={tab === "overview"}
                className={tab === "overview" ? "active" : ""}
                onClick={() => setTab("overview")}
              >
                概览
              </button>
              <button
                role="tab"
                aria-selected={tab === "labels"}
                className={tab === "labels" ? "active" : ""}
                onClick={() => setTab("labels")}
              >
                标签
              </button>
            </nav>
          )}
          {node.chain !== "ethereum" ? (
            <section>
              <h3>确认状态</h3>
              <div className="evidence-card">
                <strong>Bitcoin 链上已确认</strong>
              </div>
            </section>
          ) : tab === "overview" ? (
            <>
              <section>
                <h3>实时余额</h3>
                {balanceLoading ? (
                  <p className="empty-copy">正在读取链上余额</p>
                ) : balance ? (
                  <div className="evidence-card">
                    <div>
                      <strong>
                        {formatAssetAmount(
                          balance.amount,
                          balance.decimals,
                          balance.asset,
                          DETAIL_AMOUNT_FRACTION_DIGITS,
                        )}
                      </strong>
                      <span>区块 {balance.blockNumber.toLocaleString()}</span>
                    </div>
                    <p>RPC 最新已确认区块快照</p>
                  </div>
                ) : (
                  <p className="empty-copy">{balanceError ?? "余额暂不可用"}</p>
                )}
              </section>
              <section className="risk-summary">
                <div className={`risk-score ${presentation?.level ?? "none"}`}>
                  <strong>{presentation?.score ?? "--"}</strong>
                  <span>调查优先级</span>
                </div>
                <div>
                  <h3>{presentation?.title}</h3>
                  <p>{risk?.ruleVersion ?? "direct-label-v1"}</p>
                </div>
              </section>
              <section>
                <h3>画像</h3>
                <div className="metric-grid">
                  <Metric
                    label="生命周期交易"
                    value={profile?.features.lifetimeTransfers ?? "-"}
                  />
                  <Metric
                    label="30 日交易"
                    value={profile?.features.windowTransfers ?? "-"}
                  />
                  <Metric
                    label="对手方"
                    value={profile?.features.uniqueCounterparties ?? "-"}
                  />
                  <Metric
                    label="活跃天"
                    value={profile?.features.activeDays ?? "-"}
                  />
                  <Metric
                    label="转入"
                    value={profile?.features.incoming ?? "-"}
                  />
                  <Metric
                    label="转出"
                    value={profile?.features.outgoing ?? "-"}
                  />
                </div>
                <p className="muted">
                  {profile?.classification ?? "画像尚未加载"} ·{" "}
                  {profile?.ruleVersion ?? "hot-wallet-v1"}
                </p>
              </section>
              {address?.address && (
                <section>
                  <h3>同步状态</h3>
                  <p>
                    {address.address.syncStatus} · 区块{" "}
                    {commonCoverageEnd(address.address).toLocaleString()}
                  </p>
                </section>
              )}
            </>
          ) : (
            <section className="labels-tab">
              <h3>
                <Tag size={15} /> 确定性标签
              </h3>
              {labelsLoading ? (
                <p className="empty-copy">正在加载标签</p>
              ) : labelsError ? (
                <p className="label-form-message error" role="alert">
                  {labelsError}
                </p>
              ) : labels.length ? (
                labels.map((label) => (
                  <div
                    className="evidence-row"
                    key={`${label.type}-${label.source}`}
                  >
                    <span>{label.type}</span>
                    <strong>{Math.round(label.confidence * 100)}%</strong>
                    <small>{label.source}</small>
                  </div>
                ))
              ) : (
                <p className="empty-copy">无人工或公开标签</p>
              )}
              <LabelForm
                key={`${node.chain}:${node.address}`}
                chain={node.chain as LabelInput["chain"]}
                address={node.address}
                onSubmit={onLabel}
              />
            </section>
          )}
        </>
      )}
      {edge && (
        <>
          <section className="edge-summary">
            <span>{edge.sourceType}</span>
            <strong>
              {edge.bidirectional
                ? "1 笔已确认 Swap"
                : `${edge.count.toLocaleString()} 笔累计 ${edge.assetSymbol}`}
            </strong>
            {edge.swapLegs?.length ? (
              edge.swapLegs.map((leg) => (
                <p key={`${leg.source}-${leg.asset}`}>
                  {formatAssetAmount(
                    leg.totalAmount,
                    leg.decimals,
                    leg.assetSymbol,
                    DETAIL_AMOUNT_FRACTION_DIGITS,
                  )}{" "}
                  · {edgeEndpoint(leg.source)} <ArrowRight size={12} />{" "}
                  {edgeEndpoint(leg.target)}
                </p>
              ))
            ) : (
              <p>
                {formatAssetAmount(
                  edge.totalAmount,
                  edge.decimals,
                  edge.assetSymbol,
                  DETAIL_AMOUNT_FRACTION_DIGITS,
                )}
                {edge.decimals === undefined && " · 精度未知"}
              </p>
            )}
            <small>
              {edge.bidirectional
                ? "同一交易的输入与输出资产"
                : "地址关系历史累计，不代表该金额来自查询中心"}
            </small>
            {edge.conversionStatus && (
              <small>
                转换检查 {edge.conversionScanned ?? 0}/{edge.count} ·{" "}
                {edge.conversionStatus === "partial" ? "部分" : "完整"}
              </small>
            )}
            <div className={`edge-flow ${edge.flow ?? "return"}`}>
              <span>
                {edge.bidirectional ? "资产双向交换" : edgeFlowLabel(edge.flow)}
              </span>
              <code>
                {edgeEndpoint(edge.source)} <ArrowRight size={12} />{" "}
                {edgeEndpoint(edge.target)}
              </code>
            </div>
            {(edge.firstBlock ||
              edge.latestBlock ||
              edge.firstTime ||
              edge.latestTime) && (
              <div className="edge-meta">
                {edge.firstBlock || edge.latestBlock ? (
                  <span>
                    区块范围{" "}
                    <strong>
                      {formatBlockRange(edge.firstBlock, edge.latestBlock)}
                    </strong>
                  </span>
                ) : null}
                {edge.firstTime || edge.latestTime ? (
                  <span>
                    时间范围{" "}
                    <strong>
                      {formatTimeRange(edge.firstTime, edge.latestTime)}
                    </strong>
                  </span>
                ) : null}
              </div>
            )}
          </section>
          {edge.protocol === "thorchain" && (
            <section>
              <div className="evidence-card">
                <div>
                  <strong>{thorchainActionLabel(edge.protocolAction)}</strong>
                  <span>
                    {edge.conversionStatus === "partial"
                      ? "验证暂不可用"
                      : "已确认"}
                  </span>
                </div>
                {edge.txHash &&
                (edge.targetChain === "bitcoin" || edge.chain === "bitcoin") ? (
                  <a
                    href={`https://mempool.space/tx/${edge.txHash}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    <code>{edge.txHash}</code>
                  </a>
                ) : edge.txHash ? (
                  <code>{edge.txHash}</code>
                ) : null}
                {edge.protocolMemo && <p>{edge.protocolMemo}</p>}
                {edge.sourceTxHash && (
                  <p>
                    Ethereum 入站 <code>{edge.sourceTxHash}</code>
                    {edge.sourceAmount &&
                      ` · ${formatAssetAmount(edge.sourceAmount, 18, edge.sourceAsset ?? "ETH", DETAIL_AMOUNT_FRACTION_DIGITS)}`}
                  </p>
                )}
              </div>
            </section>
          )}
          {edge.conversionEvidence?.map((item) => (
            <section key={item.txHash}>
              <div className="evidence-card">
                <div>
                  <strong>
                    {item.protocol} · {item.version}
                  </strong>
                  <span>
                    {item.status === "complete" ? "已确认" : "未确认转换"}
                  </span>
                </div>
                <code>{item.txHash}</code>
                <p>
                  {item.tokenIn} {item.amountIn} <ArrowRight size={12} />{" "}
                  {item.tokenOut} {item.amountOut}
                </p>
                {item.liquidityProvider && (
                  <p>
                    流动性对手方 <code>{item.liquidityProvider}</code>
                  </p>
                )}
                {item.recipient && (
                  <p>
                    接收方 <code>{item.recipient}</code>
                  </p>
                )}
                {item.evidence.map((value) => (
                  <code key={value}>{value}</code>
                ))}
              </div>
            </section>
          ))}
        </>
      )}
    </aside>
  );
}
function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
function roleName(role: string) {
  if (role === "router") return "THORChain Router";
  if (role === "thorchain_vault") return "THORChain Vault";
  if (role === "cross_chain_bridge") return "跨链桥终点";
  if (role === "cross_chain_recipient") return "跨链收款地址";
  if (role === "kyberswap_router") return "KyberSwap Router";
  if (role === "kyberswap_executor") return "KyberSwap Executor";
  if (role === "pool") return "流动性池";
  if (role === "woo_x_wallet") return "WOO X 钱包";
  if (role === "woo_x_vault") return "WOO X 金库";
  if (role === "woo_x_staking_cold") return "WOO X Staking 冷钱包";
  if (role === "woo_x_team") return "WOO X 团队钱包";
  if (role === "woo_x_treasury") return "WOO X Treasury";
  if (role === "woo_x_deployer") return "WOO X Deployer";
  return role;
}
function commonCoverageEnd(address: AddressResponse["address"]) {
  return Math.min(
    address.normalSyncedTo ?? 0,
    address.internalSyncedTo ?? 0,
    address.tokenSyncedTo ?? 0,
  );
}
function edgeEndpoint(value: string) {
  return shortAddress(value.slice(value.indexOf(":") + 1), 12);
}
function edgeFlowLabel(flow?: GraphEdgeModel["flow"]) {
  return flow === "inbound"
    ? "资金流入查询中心"
    : flow === "outbound"
      ? "资金从查询中心流出"
      : "逆向或同层转账";
}
function thorchainActionLabel(action?: string) {
  return (
    (
      {
        router_inbound: "THORChain · 进入 Router",
        vault_migration: "THORChain · Vault 迁移",
        protocol_outbound: "THORChain · 协议出站",
        cross_chain_swap: "THORChain · 跨链兑换出站",
        refund: "THORChain · 退款",
      } as Record<string, string>
    )[action ?? ""] ?? "THORChain · 协议转移"
  );
}
function formatEdgeTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "时间未知"
    : date.toLocaleString("zh-CN", { dateStyle: "medium", timeStyle: "short" });
}
function formatBlockRange(first?: number, latest?: number) {
  if (first && latest && first !== latest)
    return `${first.toLocaleString()} - ${latest.toLocaleString()}`;
  return (latest ?? first)?.toLocaleString() ?? "区块未知";
}
function formatTimeRange(first?: string, latest?: string) {
  if (first && latest && first !== latest)
    return `${formatEdgeTime(first)} - ${formatEdgeTime(latest)}`;
  return formatEdgeTime(latest ?? first ?? "");
}
function riskPresentation(labels: Label[], fallback?: RiskResult) {
  const directHigh = labels.some((label) => label.riskLevel === "high");
  const score = directHigh
    ? Math.max(70, fallback?.score ?? 0)
    : (fallback?.score ?? 0);
  const level =
    directHigh || fallback?.level === "known_high"
      ? "known_high"
      : score > 0
        ? "suspected"
        : "none";
  const title = directHigh
    ? "确定性风险标签"
    : score > 0
      ? "直接风险证据"
      : "无直接风险标签";
  return { score: String(score), level, title };
}
