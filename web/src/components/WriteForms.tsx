import { useState, type FormEvent } from "react";
import type { BridgeInput, Chain } from "../api/types";

interface BridgeFormProps { chain: Chain; address: string; onSubmit: (input: BridgeInput) => Promise<void> }

export function WriteForms({ chain, address, onBridge }: { chain: Chain; address: string; onBridge: BridgeFormProps["onSubmit"] }) {
  return <div className="write-grid"><BridgeForm chain={chain} address={address} onSubmit={onBridge}/></div>;
}

function BridgeForm({ chain, address, onSubmit }: BridgeFormProps) {
  const [message,setMessage]=useState("");
  const submit=async(event:FormEvent<HTMLFormElement>)=>{event.preventDefault();const data=new FormData(event.currentTarget);try{await onSubmit({sourceChain:String(data.get("sourceChain")) as Chain,targetChain:String(data.get("targetChain")) as Chain,sourceAddress:address,targetAddress:String(data.get("targetAddress")),bridgeAddress:String(data.get("bridgeAddress")),sourceTxHash:String(data.get("sourceTxHash")),targetTxHash:String(data.get("targetTxHash")),sourceLogIndex:Number(data.get("sourceLogIndex")),targetLogIndex:Number(data.get("targetLogIndex")),sourceAsset:String(data.get("sourceAsset")),targetAsset:String(data.get("targetAsset")),sourceAmount:String(data.get("sourceAmount")),targetAmount:String(data.get("targetAmount")),evidence:lines(data.get("evidence"))});setMessage("桥接关系已保存")}catch(error){setMessage(errorMessage(error))}};
  return <form onSubmit={submit}><h3>提交确认式桥接</h3><div className="form-pair"><select name="sourceChain" defaultValue={chain}><option value="ethereum">Ethereum</option><option value="base">Base</option></select><select name="targetChain" defaultValue={chain==="base"?"ethereum":"base"}><option value="ethereum">Ethereum</option><option value="base">Base</option></select></div>{["targetAddress","bridgeAddress","sourceTxHash","targetTxHash","sourceAsset","targetAsset","sourceAmount","targetAmount"].map(name=><input name={name} required key={name} placeholder={name}/>)}<div className="form-pair"><input name="sourceLogIndex" type="number" min="0" defaultValue="0"/><input name="targetLogIndex" type="number" min="0" defaultValue="0"/></div><textarea name="evidence" required placeholder="外部证据，每行一条"/><button>验证并保存</button>{message&&<p className="form-message">{message}</p>}</form>;
}

const lines=(value:FormDataEntryValue|null)=>String(value??"").split("\n").map(item=>item.trim()).filter(Boolean);
const errorMessage=(error:unknown)=>error instanceof Error?error.message:"提交失败";
