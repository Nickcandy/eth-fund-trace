import { useState, type FormEvent } from "react";
import type { Chain, LabelInput } from "../api/types";

interface Props { chain: Chain; address: string; onSubmit: (input: LabelInput) => Promise<void> }

export function LabelForm({ chain, address, onSubmit }: Props) {
  const [state, setState] = useState<"idle" | "saving" | "success" | "error">("idle");
  const [message, setMessage] = useState("");
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setState("saving");
    setMessage("");
    try {
      await onSubmit({
        chain,
        address,
        type: String(data.get("type")),
        source: String(data.get("source")) as LabelInput["source"],
        riskLevel: String(data.get("risk")) as LabelInput["riskLevel"],
        confidence: Number(data.get("confidence")),
        note: String(data.get("note")),
        evidence: lines(data.get("evidence")),
      });
      setState("success");
      setMessage("标签已保存，重新追踪后传播生效");
    } catch (error) {
      setState("error");
      setMessage(error instanceof Error ? error.message : "标签保存失败");
    }
  };

  return <form className="label-form" onSubmit={submit}>
    <label><span>标签类型</span><input name="type" required placeholder="标签类型，如 exchange" /></label>
    <label><span>来源</span><select name="source" aria-label="来源"><option value="manual">人工</option><option value="public-list">公开名单</option></select></label>
    <label><span>风险等级</span><select name="risk" aria-label="风险等级"><option value="">无风险级别</option><option value="low">低</option><option value="medium">中</option><option value="high">高</option></select></label>
    <label><span>置信度</span><input name="confidence" aria-label="置信度" type="number" min="0" max="1" step="0.01" defaultValue="1" /></label>
    <label><span>证据</span><textarea name="evidence" placeholder="证据，每行一条" /></label>
    <label><span>备注</span><input name="note" placeholder="备注" /></label>
    <button type="submit" disabled={state === "saving"}>{state === "saving" ? "保存中" : "保存标签"}</button>
    {message && <p className={`label-form-message ${state}`} role={state === "error" ? "alert" : "status"}>{message}</p>}
  </form>;
}

const lines = (value: FormDataEntryValue | null) => String(value ?? "").split("\n").map((item) => item.trim()).filter(Boolean);
