import { ArrowRight, ExternalLink, GitBranch, List, RefreshCw } from "lucide-react";
import type { TransactionAnalysis } from "../api/types";

interface Props { analysis: TransactionAnalysis; onTrace: (address: string) => void }

const short = (value: string) => `${value.slice(0, 8)}...${value.slice(-6)}`;
const explorer = (kind: "tx" | "address", value: string) => `https://etherscan.io/${kind}/${value}`;

export function TransactionView({ analysis, onTrace }: Props) {
  return <section className="transaction-view" aria-label="交易分析结果">
    <header className="transaction-heading">
      <div><span className="eyebrow">TRANSACTION EVIDENCE</span><h2>{analysis.entryContractName ?? "链上交易事实"}</h2></div>
      <span className={`quality-badge ${analysis.quality.status}`}>{analysis.quality.status === "complete" ? "证据完整" : "部分证据"}</span>
    </header>
    <div className="transaction-facts">
      <Fact label="交易" value={analysis.txHash} href={explorer("tx", analysis.txHash)} />
      <Fact label="发起地址" value={analysis.from} href={explorer("address", analysis.from)} />
      <Fact label="入口合约" value={analysis.to} href={explorer("address", analysis.to)} detail={analysis.entryContractName} />
      <Fact label="区块 / 状态" value={`${analysis.blockNumber.toLocaleString()} / ${analysis.succeeded ? "成功" : "失败"}`} />
    </div>
    <div className="analysis-grid">
      <section><h3><GitBranch size={15}/> Swap 路径</h3>
        {analysis.swaps.length === 0 ? <p className="empty-copy">未发现经过官方 Factory 验证的 Uniswap V3 Swap。</p> : analysis.swaps.map((swap, index) =>
          <article className={`swap-row ${swap.verified ? "verified" : "unknown"}`} key={`${swap.pool}-${swap.logIndex}`}>
            <div className="swap-index">{index + 1}</div><div className="swap-body">
              <div className="swap-title"><strong>{swap.verified ? "Uniswap V3" : "未验证协议"}</strong><span>log #{swap.logIndex} · fee {swap.fee ?? "?"}</span></div>
              <a href={explorer("address", swap.pool)} target="_blank" rel="noreferrer">Pool {short(swap.pool)} <ExternalLink size={11}/></a>
              <div className="swap-flow"><code>{swap.amountIn ?? "?"}</code><small>{swap.tokenIn ? short(swap.tokenIn) : "未知资产"}</small><ArrowRight size={16}/><code>{swap.amountOut ?? "?"}</code><small>{swap.tokenOut ? short(swap.tokenOut) : "未知资产"}</small></div>
              <p>{swap.evidence.join(" · ")}</p>
            </div>
          </article>)}
        <h3 className="transfer-heading"><List size={15}/> Receipt Transfer 事实</h3>
        {analysis.transfers.length === 0 ? <p className="empty-copy">Receipt 中没有 ERC-20 Transfer 日志。</p> : analysis.transfers.map(transfer => <div className="transfer-fact" key={`${transfer.token}-${transfer.logIndex}`}>
          <a href={explorer("address", transfer.token)} target="_blank" rel="noreferrer">{short(transfer.token)}</a><code>{transfer.amount}</code><span>{short(transfer.from)} <ArrowRight size={11}/> {short(transfer.to)} · log #{transfer.logIndex}</span>
        </div>)}
      </section>
      <aside><h3><RefreshCw size={15}/> WETH 包装事件</h3>
        {analysis.wraps.length === 0 ? <p className="empty-copy">本交易没有 WETH 包装或解包事件。</p> : analysis.wraps.map(wrap => <div className="wrap-row" key={`${wrap.type}-${wrap.logIndex}`}><strong>{wrap.type === "deposit" ? "包装 ETH" : "解包 WETH"}</strong><code>{wrap.amount}</code><span>{short(wrap.account)} · log #{wrap.logIndex}</span></div>)}
        <h3>质量说明</h3><p className="quality-copy">{analysis.quality.ambiguousRoute ? "存在多 Pool 或无法唯一连接的路线，仅展示逐段事实。" : "逐段 Swap 可连接为明确输出路线。"}</p>
        {analysis.quality.issues?.map(issue => <p className="quality-issue" key={issue}>{issue}</p>)}
        {analysis.finalOutputAddress && <button className="trace-output" onClick={() => onTrace(analysis.finalOutputAddress!)}><GitBranch size={15}/>继续追踪 {short(analysis.finalOutputAddress)}</button>}
      </aside>
    </div>
  </section>;
}

function Fact({ label, value, detail, href }: { label: string; value: string; detail?: string; href?: string }) {
  const content = <><code>{value.length > 28 ? short(value) : value}</code>{detail && <small>{detail}</small>}{href && <ExternalLink size={11}/>}</>;
  return <div><span>{label}</span>{href ? <a href={href} target="_blank" rel="noreferrer">{content}</a> : content}</div>;
}
