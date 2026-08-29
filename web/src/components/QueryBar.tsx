import { Search } from "lucide-react";
import type { FormEvent } from "react";
import type { TraceQuery } from "../api/types";

interface Props {
  value: TraceQuery;
  onChange: (value: TraceQuery) => void;
  onSubmit: () => void;
  busy: boolean;
  error?: string;
  mode?: "address" | "transaction";
  onModeChange?: (mode: "address" | "transaction") => void;
  txHash?: string;
  onTxHashChange?: (value: string) => void;
}

export function QueryBar({
  value,
  onChange,
  onSubmit,
  busy,
  error,
  mode = "address",
  onModeChange,
  txHash = "",
  onTxHashChange,
}: Props) {
  const submit = (event: FormEvent) => {
    event.preventDefault();
    onSubmit();
  };
  return (
    <form className="query-bar" onSubmit={submit} aria-label="资金链路查询">
      <div className="query-mode" role="group" aria-label="查询模式">
        <button
          type="button"
          className={mode === "address" ? "active" : ""}
          onClick={() => onModeChange?.("address")}
        >
          地址
        </button>
        <button
          type="button"
          className={mode === "transaction" ? "active" : ""}
          onClick={() => onModeChange?.("transaction")}
        >
          交易哈希
        </button>
      </div>
      {mode === "transaction" ? (
        <label className="address-field">
          <span>交易哈希</span>
          <input
            aria-label="交易哈希"
            value={txHash}
            onChange={(event) => onTxHashChange?.(event.target.value.trim())}
            placeholder="0x..."
            spellCheck={false}
          />
        </label>
      ) : (
        <label className="address-field">
          <span>分析地址</span>
          <input
            aria-label="分析地址"
            value={value.address}
            onChange={(event) =>
              onChange({ ...value, address: event.target.value.trim() })
            }
            placeholder="0x..."
            spellCheck={false}
          />
        </label>
      )}
      <label>
        <span>网络</span>
        <select aria-label="网络" value="ethereum" disabled>
          <option value="ethereum">Ethereum</option>
        </select>
      </label>
      {mode === "address" && (
        <>
          <label>
            <span>方向</span>
            <select
              aria-label="方向"
              value={value.direction}
              onChange={(event) =>
                onChange({
                  ...value,
                  direction: event.target.value as TraceQuery["direction"],
                })
              }
            >
              <option value="both">双向</option>
              <option value="in">上游</option>
              <option value="out">下游</option>
            </select>
          </label>
          <label className="number-field">
            <span>深度</span>
            <input
              aria-label="深度"
              type="number"
              min={1}
              max={5}
              value={value.depth}
              onChange={(event) =>
                onChange({ ...value, depth: Number(event.target.value) })
              }
            />
          </label>
        </>
      )}
      <button className="primary-command" type="submit" disabled={busy}>
        <Search size={17} />
        {busy ? "分析中" : "开始分析"}
      </button>
      {error && (
        <div className="query-error" role="alert">
          {error}
        </div>
      )}
    </form>
  );
}
