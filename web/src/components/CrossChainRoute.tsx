import type { TransactionAnalysis } from "../api/types";
import { formatChainAmount } from "../lib/format";

interface Props {
  route: NonNullable<TransactionAnalysis["crossChain"]>;
}

const short = (value: string) => `${value.slice(0, 8)}...${value.slice(-6)}`;

export function CrossChainRoute({ route }: Props) {
  const sourceExplorer =
    route.sourceChainId === 42161 ? "https://arbiscan.io/tx/" : "https://etherscan.io/tx/";
  const targetExplorer = "https://etherscan.io/tx/";

  return (
    <div className="wrap-row">
      <strong>Relay {route.sourceChain} → {route.targetChain}</strong>
      <a href={`${sourceExplorer}${route.sourceTxHash}`} target="_blank" rel="noreferrer">
        源交易 <code>{short(route.sourceTxHash)}</code>
      </a>
      <a href={`${targetExplorer}${route.targetTxHash}`} target="_blank" rel="noreferrer">
        目标交易 <code>{short(route.targetTxHash)}</code>
      </a>
      <span>
        {formatChainAmount(route.sourceAmount, 18)} {route.sourceAsset} →{" "}
        {formatChainAmount(route.targetAmount, 18)} {route.targetAsset}
      </span>
      <span>协议费用 {formatChainAmount(route.feeAmount, 18)} ETH</span>
      <code title={route.requestId}>{short(route.requestId)}</code>
    </div>
  );
}
