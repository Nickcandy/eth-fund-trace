import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import {
  AlertTriangle,
  CircleStop,
  Flame,
  Focus,
  Landmark,
  LoaderCircle,
  Minus,
  Plus,
} from "lucide-react";
import { chainLabel, shortAddress, stopReasonLabel } from "../lib/format";

export interface FundNodeData extends Record<string, unknown> {
  address: string;
  chain: string;
  seed: boolean;
  terminal: boolean;
  hotWallet: boolean;
  risk: string;
  labelTypes: string[];
  addressType?: "unknown" | "eoa" | "contract";
  protocol?: string;
  roles?: string[];
  stopReason?: string;
  onFocus: (chain: string, address: string) => void;
  onExpand: (side: "left" | "right") => void;
  leftExpansion: ExpansionControl;
  rightExpansion: ExpansionControl;
}
export interface ExpansionControl {
  mode: "expand" | "trace" | "none";
  expanded: boolean;
  status?: "running" | "failed";
  disabled?: boolean;
}
export type FundFlowNode = Node<FundNodeData, "fund">;

export function FundNode({ data, selected }: NodeProps<FundFlowNode>) {
  const state =
    data.risk === "high"
      ? "risk-high"
      : data.category === "high_frequency"
        ? "terminal"
        : data.terminal
          ? "terminal"
          : data.hotWallet
            ? "hot-wallet"
            : data.seed
              ? "seed"
              : "normal";
  return (
    <div className={`fund-node ${state} ${selected ? "selected" : ""}`}>
      <Handle
        id="target-left"
        className="handle-target"
        type="target"
        position={Position.Left}
      />
      <Handle
        id="source-left"
        className="handle-source"
        type="source"
        position={Position.Left}
      />
      <div className="node-heading">
        <span className={`chain-badge ${data.chain}`}>
          {chainLabel(data.chain)}
        </span>
        {data.seed && <span className="seed-label">查询中心</span>}
      </div>
      <div className="node-address" title={data.address}>
        {data.address === "0x0000000000000000000000000000000000000000"
          ? "零地址（铸造 / 销毁）"
          : shortAddress(data.address)}
      </div>
      <div className="node-signals">
        {data.roles?.map((role) => (
          <span key={role}>{roleLabel(role)}</span>
        ))}
        {data.risk === "high" && (
          <span>
            <AlertTriangle size={13} />
            高风险
          </span>
        )}
        {data.terminal && (
          <span>
            <CircleStop size={13} />
            {stopReasonLabel(data.stopReason)}
          </span>
        )}
        {data.hotWallet && (
          <span>
            <Flame size={13} />
            疑似热钱包
          </span>
        )}
        {data.category === "high_frequency" && (
          <span>
            <Flame size={13} />
            高频交易点
          </span>
        )}
        {data.category === "contract" && (
          <span>
            <Landmark size={13} />
            合约点
          </span>
        )}
        {!data.roles?.length &&
          !data.risk.includes("high") &&
          !data.terminal &&
          !data.hotWallet && (
            <span>
              <Landmark size={13} />
              {data.addressType === "contract" ? "合约" : "地址"}
            </span>
          )}
      </div>
      {data.chain === "ethereum" && (
        <button
          className="node-focus"
          title="以此地址重新分析"
          onClick={(event) => {
            event.stopPropagation();
            data.onFocus(data.chain, data.address);
          }}
        >
          <Focus size={14} />
        </button>
      )}
      <ExpansionButton
        side="left"
        control={data.leftExpansion}
        onClick={() => data.onExpand("left")}
      />
      <ExpansionButton
        side="right"
        control={data.rightExpansion}
        onClick={() => data.onExpand("right")}
      />
      <Handle
        id="target-right"
        className="handle-target"
        type="target"
        position={Position.Right}
      />
      <Handle
        id="source-right"
        className="handle-source"
        type="source"
        position={Position.Right}
      />
    </div>
  );
}

function ExpansionButton({
  side,
  control,
  onClick,
}: {
  side: "left" | "right";
  control: ExpansionControl;
  onClick: () => void;
}) {
  if (control.mode === "none") return null;
  const direction = side === "left" ? "上游" : "下游";
  const label =
    control.status === "running"
      ? `${direction}追踪中`
      : control.status === "failed"
        ? `重试${direction}`
        : control.mode === "trace"
          ? `继续追踪${direction}`
          : control.expanded
            ? `收起${direction}`
            : `展开${direction}`;
  return (
    <button
      className={`node-expand ${side} ${control.expanded ? "expanded" : ""} ${control.status ?? ""}`}
      title={label}
      aria-label={label}
      disabled={control.disabled || control.status === "running"}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
    >
      {control.status === "running" ? (
        <LoaderCircle className="spin" size={14} />
      ) : control.expanded ? (
        <Minus size={14} />
      ) : (
        <Plus size={14} />
      )}
      <span>{label}</span>
    </button>
  );
}

function roleLabel(role: string) {
  if (role === "router") return "THORChain Router";
  if (role === "thorchain_vault") return "THORChain Vault";
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
