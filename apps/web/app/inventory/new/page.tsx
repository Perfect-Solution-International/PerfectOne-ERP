"use client";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Sidebar from "../../components/Sidebar";
import { apiFetch } from "../../lib/api";
import { useRequireSession } from "../../lib/useRequireSession";

type Type = "count" | "transfer" | "damage";
const meta: Record<Type, { crumb: string; title: string; help: string }> = {
  count: { crumb: "Stock count", title: "Record a physical stock count", help: "Enter what you counted. The system calculates and records the difference as an immutable adjustment." },
  transfer: { crumb: "Stock transfer", title: "Create a stock transfer draft", help: "Stock leaves the source at dispatch and enters the destination only at receipt." },
  damage: { crumb: "Damaged stock", title: "Record damaged stock", help: "Save a reviewable draft. Finalization writes off stock and posts the accounting loss." },
};

export default function NewInventoryDoc() {
  useRequireSession();
  const router = useRouter();
  const type = ((useSearchParams().get("type") as Type) in meta ? useSearchParams().get("type") : "count") as Type;
  const info = meta[type];

  const [products, setProducts] = useState<any[]>([]);
  const [branches, setBranches] = useState<any[]>([]);
  const [batches, setBatches] = useState<any[]>([]);
  const [productId, setProductId] = useState("");
  const [sourceBranchId, setSourceBranchId] = useState("");
  const [sourceStock, setSourceStock] = useState<any[]>([]);
  const [transferLines, setTransferLines] = useState<any[]>([]);
  const [transferQty, setTransferQty] = useState(1);
  const [transferBatchId, setTransferBatchId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    Promise.all([apiFetch("/products"), apiFetch("/inventory/branches"), apiFetch("/stock/batches")])
      .then(([p, b, bt]) => { setProducts(p); setBranches(b); setBatches(bt); })
      .catch((e) => setError(e.message));
  }, []);

  useEffect(() => {
    if (type !== "transfer" || !sourceBranchId) { setSourceStock([]); return; }
    apiFetch(`/inventory/branches/${sourceBranchId}/stock`).then(setSourceStock).catch((e) => setError(e.message));
  }, [sourceBranchId, type]);

  const product = products.find((p) => p.ID === productId);
  const transferProduct = sourceStock.find((p) => p.id === productId);
  const transferBatches = useMemo(() => batches.filter((x) => x.productId === productId && x.branchId === sourceBranchId && Number(x.available) > 0), [batches, productId, sourceBranchId]);
  const transferBatch = transferBatches.find((x) => x.id === transferBatchId);
  const transferAvailable = transferBatch ? Math.min(Number(transferProduct?.quantity || 0), Number(transferBatch.available || 0)) : Number(transferProduct?.quantity || 0);
  const batchMatches = useMemo(() => batches.filter((x) => !productId || x.product === product?.Name), [batches, productId, product]);

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault(); setBusy(true); setError("");
    const d: any = Object.fromEntries(new FormData(e.currentTarget));
    d.requestId = crypto.randomUUID();
    try {
      if (type === "count") {
        await apiFetch("/inventory/adjustment-requests", { method: "POST", body: JSON.stringify({ productId: d.productId, physicalQuantity: Number(d.physicalQuantity), reason: d.reason, notes: d.notes, requestId: d.requestId }) });
        router.push("/inventory?done=count");
      } else if (type === "transfer") {
        const lines = transferLines.length ? transferLines : [{ productId: d.productId, batchId: transferBatchId, quantity: Number(d.quantity) }];
        if (!lines.length) throw new Error("Add at least one product to the transfer.");
        await apiFetch("/inventory/transfer-documents", { method: "POST", body: JSON.stringify({ fromBranchId:d.fromBranchId, toBranchId:d.toBranchId, reference:d.reference, notes:d.notes, lines, requestId:crypto.randomUUID() }) });
        router.push("/inventory?done=transfer");
      } else {
        d.quantity = Number(d.quantity);
        await apiFetch("/inventory/damages", { method: "POST", body: JSON.stringify(d) });
        router.push("/inventory?done=damage");
      }
    } catch (err: any) { setError(err.message); setBusy(false); }
  };

  return <main className="shell"><Sidebar active="Inventory control" /><section className="app inventoryApp">
    <header className="topbar"><div><span className="crumb">Inventory / {info.crumb}</span><b>{info.title}</b></div><Link className="ghostButton" href="/inventory">Cancel</Link></header>
    <div className="page inventoryPage">
      <div className="pageHeading"><div><p>CONTROLLED INVENTORY</p><h1>{info.title}</h1><small>{info.help}</small></div></div>
      {error && <div className="financeNotice error">{error}<button onClick={() => setError("")}>×</button></div>}
      <form className="inventoryWorkflowForm" onSubmit={submit}>
        <header className="inventoryFormHeader"><span>1</span><div><h2>{info.title}</h2><p>{info.help}</p></div></header>
        <div className="inventoryFormGrid">
          {type === "count" && <>
            <label>Product<select name="productId" value={productId} onChange={(e) => setProductId(e.target.value)} required><option value="">Select product</option>{products.map((x) => <option key={x.ID} value={x.ID}>{x.Name} — {x.SKU}</option>)}</select></label>
            <div className="stockSnapshot"><small>System quantity</small><b>{Number(product?.Stock || 0).toLocaleString("en-LK")}</b><span>{product?.Unit || "units"}</span></div>
            <label>Physical quantity<input name="physicalQuantity" type="number" min="0" step="0.001" required /></label>
            <label>Adjustment reason<select name="reason" required><option value="">Select reason</option><option>Scheduled physical count</option><option>Stock shortage</option><option>Found stock</option><option>Data correction</option></select></label>
            <label className="wide">Notes<input name="notes" placeholder="Count sheet number and explanation" /></label>
          </>}
          {type === "transfer" && <>
            <label>Source branch<select name="fromBranchId" value={sourceBranchId} onChange={(e) => { setSourceBranchId(e.target.value); setProductId(""); setTransferBatchId(""); }} required><option value="">Select source</option>{branches.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</select></label>
            <label>Destination branch<select name="toBranchId" required><option value="">Select destination</option>{branches.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</select></label>
            <label>Product<select name="productId" value={productId} onChange={(e) => setProductId(e.target.value)} disabled={!sourceBranchId} required={!transferLines.length}><option value="">{sourceBranchId ? "Select product" : "Select source branch first"}</option>{sourceStock.filter((x) => x.quantity > 0).map((x) => <option key={x.id} value={x.id}>{x.name} — available {x.quantity} {x.unit}</option>)}</select></label>
            <label>Batch (optional)<select value={transferBatchId} onChange={(e) => setTransferBatchId(e.target.value)} disabled={!productId}><option value="">General stock</option>{transferBatches.map((x) => <option key={x.id} value={x.id}>{x.batchNo || "Unnumbered"} ({x.available} available)</option>)}</select></label>
            <label>Quantity<input name="quantity" type="number" value={transferQty} onChange={(e)=>setTransferQty(Number(e.target.value))} min="0.001" max={transferAvailable || undefined} step="0.001" disabled={!productId} required={!transferLines.length} /></label>
            {transferProduct && <div className="stockSnapshot"><small>Available at source</small><b>{Number(transferProduct.quantity).toLocaleString("en-LK")}</b><span>{transferProduct.unit}</span></div>}
            <button type="button" className="ghostButton" disabled={!transferProduct||transferQty<=0||transferQty>transferAvailable} onClick={()=>{setTransferLines(old=>[...old,{productId,batchId:transferBatchId,quantity:transferQty,product:transferProduct.name,unit:transferProduct.unit,batchNo:transferBatch?.batchNo||"General stock"}]);setProductId("");setTransferBatchId("");setTransferQty(1)}}>+ Add transfer line</button>
            {transferLines.length>0&&<div className="wide transferLineList">{transferLines.map((x,i)=><div key={`${x.productId}-${x.batchId}-${i}`}><span><b>{x.product}</b><small>{x.quantity} {x.unit} · {x.batchNo}</small></span><button type="button" onClick={()=>setTransferLines(a=>a.filter((_,n)=>n!==i))}>Remove</button></div>)}</div>}
            <label>Reference<input name="reference" placeholder="Transfer request number" /></label>
            <label>Notes<input name="notes" placeholder="Reason or handling instructions" /></label>
          </>}
          {type === "damage" && <>
            <label>Product<select name="productId" value={productId} onChange={(e) => setProductId(e.target.value)} required><option value="">Select product</option>{products.map((x) => <option key={x.ID} value={x.ID}>{x.Name} — stock {x.Stock}</option>)}</select></label>
            <label>Batch<select name="batchId"><option value="">No specific batch</option>{batchMatches.map((x) => <option key={x.id} value={x.id}>{x.batchNo || "Unnumbered batch"} — available {x.available}</option>)}</select></label>
            <label>Damaged quantity<input name="quantity" type="number" min="0.001" step="0.001" required /></label>
            <label>Reason<select name="reason" required><option value="">Select reason</option><option>Broken packaging</option><option>Quality issue</option><option>Spillage</option><option>Pest damage</option><option>Handling damage</option><option>Other</option></select></label>
            <label className="wide">Notes<input name="notes" placeholder="Evidence, incident or approval reference" /></label>
          </>}
        </div>
        <footer>
          <div><b>{type === "count" ? "Expected impact" : "Workflow"}</b><small>{type === "count" ? "The difference updates branch stock, global stock and the stock ledger." : type === "transfer" ? "Draft → Approved → Dispatched → Received" : "Stock decreases and an inventory-loss journal is created on finalization."}</small></div>
          <button className="new" disabled={busy}>{busy ? "Saving…" : type === "count" ? "Post adjustment" : type === "transfer" ? "Save transfer draft" : "Save damage draft"}</button>
        </footer>
      </form>
    </div>
  </section></main>;
}
