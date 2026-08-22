import { useState, type FormEvent } from "react";
import type { BridgeInput, Chain, LabelInput } from "../api/types";

interface LabelFormProps { chain: Chain; address: string; onSubmit: (input: LabelInput) => Promise<void> }
interface BridgeFormProps { chain: Chain; address: string; onSubmit: (input: BridgeInput) => Promise<void> }

export function WriteForms({ chain, address, onLabel, onBridge }: { chain: Chain; address: string; onLabel: LabelFormProps["onSubmit"]; onBridge: BridgeFormProps["onSubmit"] }) {
  return <div className="write-grid"><LabelForm chain={chain} address={address} onSubmit={onLabel}/><BridgeForm chain={chain} address={address} onSubmit={onBridge}/></div>;
}

function LabelForm({ chain, address, onSubmit }: LabelFormProps) {
  const [message,setMessage]=useState("");
  const submit=async(event:FormEvent<HTMLFormElement>)=>{event.preventDefault();const data=new FormData(event.currentTarget);try{await onSubmit({chain,address,type:String(data.get("type")),source:String(data.get("source")) as LabelInput["source"],riskLevel:String(data.get("risk")) as LabelInput["riskLevel"],confidence:Number(data.get("confidence")),note:String(data.get("note")),evidence:lines(data.get("evidence"))});setMessage("标签已保存")}catch(error){setMessage(errorMessage(error))}};
  return <form onSubmit={submit}><h3>添加确定性标签</h3><input name="type" required placeholder="标签类型，如 exchange"/><select name="source"><option value="manual">人工</option><option value="public-list">公开名单</option></select><select name="risk"><option value="">无风险级别</option><option value="low">低</option><option value="medium">中</option><option value="high">高</option></select><input name="confidence" type="number" min="0" max="1" step="0.01" defaultValue="1"/><textarea name="evidence" placeholder="证据，每行一条"/><input name="note" placeholder="备注"/><button>保存标签</button>{message&&<p className="form-message">{message}</p>}</form>;
}

function BridgeForm({ chain, address, onSubmit }: BridgeFormProps) {
  const [message,setMessage]=useState("");
  const submit=async(event:FormEvent<HTMLFormElement>)=>{event.preventDefault();const data=new FormData(event.currentTarget);try{await onSubmit({sourceChain:String(data.get("sourceChain")) as Chain,targetChain:String(data.get("targetChain")) as Chain,sourceAddress:address,targetAddress:String(data.get("targetAddress")),bridgeAddress:String(data.get("bridgeAddress")),sourceTxHash:String(data.get("sourceTxHash")),targetTxHash:String(data.get("targetTxHash")),sourceLogIndex:Number(data.get("sourceLogIndex")),targetLogIndex:Number(data.get("targetLogIndex")),sourceAsset:String(data.get("sourceAsset")),targetAsset:String(data.get("targetAsset")),sourceAmount:String(data.get("sourceAmount")),targetAmount:String(data.get("targetAmount")),evidence:lines(data.get("evidence"))});setMessage("桥接关系已保存")}catch(error){setMessage(errorMessage(error))}};
  return <form onSubmit={submit}><h3>提交确认式桥接</h3><div className="form-pair"><select name="sourceChain" defaultValue={chain}><option value="ethereum">Ethereum</option><option value="base">Base</option></select><select name="targetChain" defaultValue={chain==="base"?"ethereum":"base"}><option value="ethereum">Ethereum</option><option value="base">Base</option></select></div>{["targetAddress","bridgeAddress","sourceTxHash","targetTxHash","sourceAsset","targetAsset","sourceAmount","targetAmount"].map(name=><input name={name} required key={name} placeholder={name}/>)}<div className="form-pair"><input name="sourceLogIndex" type="number" min="0" defaultValue="0"/><input name="targetLogIndex" type="number" min="0" defaultValue="0"/></div><textarea name="evidence" required placeholder="外部证据，每行一条"/><button>验证并保存</button>{message&&<p className="form-message">{message}</p>}</form>;
}

const lines=(value:FormDataEntryValue|null)=>String(value??"").split("\n").map(item=>item.trim()).filter(Boolean);
const errorMessage=(error:unknown)=>error instanceof Error?error.message:"提交失败";
