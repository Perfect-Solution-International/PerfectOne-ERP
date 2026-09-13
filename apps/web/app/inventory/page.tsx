"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import Sidebar from "../components/Sidebar";
import ActionDialog, { type ActionDialogRequest } from "../components/ActionDialog";
import { apiFetch } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";

const tabs = ["Movement history", "Stock count", "Stock transfers", "Batches & expiry", "Damaged stock"] as const;
type Tab = typeof tabs[number];
const newLink: Partial<Record<Tab, { href: string; label: string }>> = {
  "Stock count": { href: "/inventory/new?type=count", label: "＋ New stock count" },
  "Stock transfers": { href: "/inventory/new?type=transfer", label: "＋ New transfer" },
  "Damaged stock": { href: "/inventory/new?type=damage", label: "＋ Record damage" },
};
const doneMessage: Record<string, string> = {
  count: "Physical count posted as an immutable stock adjustment.",
  transfer: "Transfer draft created. Approve it before dispatch.",
  damage: "Damage draft created. Stock changes only after finalization.",
};

export default function InventoryPage() {
  useRequireSession();
  const searchParams = useSearchParams();
  const done = searchParams.get("done");
  const [tab, setTab] = useState<Tab>("Movement history");
  const [rows, setRows] = useState<any[]>([]);
  const [query, setQuery] = useState("");
  const [message, setMessage] = useState(done ? doneMessage[done] || "" : "");
  const [error, setError] = useState("");
  const [dialog, setDialog] = useState<ActionDialogRequest | null>(null);

  useEffect(() => {
    const requested = searchParams.get("section");
    const match: Record<string, Tab> = { movements: "Movement history", counts: "Stock count", transfers: "Stock transfers", batches: "Batches & expiry", expiry: "Batches & expiry", damage: "Damaged stock" };
    if (requested && match[requested]) setTab(match[requested]);
  }, [searchParams]);

  const path = tab === "Movement history" ? `/inventory/movements?q=${encodeURIComponent(query)}`
    : tab === "Stock count" ? "/inventory/adjustment-requests"
    : tab === "Stock transfers" ? "/inventory/transfer-documents"
    : tab === "Batches & expiry" ? "/stock/batches" : "/damages";
  const load = async () => { try { setRows(await apiFetch(path)); setError(""); } catch (issue: any) { setError(issue.message); } };
  useEffect(() => { void load(); }, [tab, query]);

  const visibleRows = useMemo(() => {
    if (!query.trim() || tab === "Movement history") return rows;
    const needle = query.trim().toLowerCase();
    return rows.filter((row) => Object.values(row).some((value) => String(value ?? "").toLowerCase().includes(needle)));
  }, [rows, query, tab]);

  const action = async (url: string, needsReason = false) => {
    const label = url.split("/").pop()?.replaceAll("-", " ") || "update";
    setDialog({ title: `${label[0].toUpperCase()}${label.slice(1)} inventory record`, message: needsReason ? "This action changes controlled stock history and cannot be silently deleted later." : "Review the record before continuing with this inventory workflow step.", confirmLabel: label, tone: needsReason ? "danger" : "default", reasonRequired: needsReason, onConfirm: async (reason) => { await apiFetch(url, { method: "POST", body: JSON.stringify({ reason, requestId: crypto.randomUUID() }) }); setMessage("Inventory workflow updated successfully."); setError(""); await load(); } });
  };

  const cta = newLink[tab];
  return <main className="shell"><Sidebar active="Inventory control" /><section className="app inventoryApp">
    <header className="topbar"><div><span className="crumb">Inventory / Control centre</span><b>Stock operations</b></div>
      <div className="topbarActions">
        {cta && <Link className="new" href={cta.href}>{cta.label}</Link>}
        <button className="ghostButton" onClick={() => window.print()}>Print</button>
      </div>
    </header>
    <div className="page inventoryPage">
      <div className="pageHeading"><div><p>CONTROLLED INVENTORY</p><h1>Stock workflow centre</h1><small>Count, transfer and write off stock with a permanent movement history.</small></div></div>
      <section className="inventoryFlow"><span><b>1</b> Create draft / count</span><i>→</i><span><b>2</b> Review impact</span><i>→</i><span><b>3</b> Approve or finalize</span><i>→</i><span><b>4</b> Immutable ledger</span></section>
      <nav className="accountingTabs">{tabs.map((item) => <button className={tab === item ? "active" : ""} key={item} onClick={() => { setTab(item); setMessage(""); setError(""); }}>{item}</button>)}</nav>
      {message && <div className="financeNotice success">{message}<button onClick={() => setMessage("")}>×</button></div>}
      {error && <div className="financeNotice error">{error}<button onClick={() => setError("")}>×</button></div>}
      <section className="inventorySearch"><label>Search {tab.toLowerCase()}<input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Product, SKU, reference, status or movement type" /></label><div><b>{visibleRows.length}</b><small>matching records</small></div></section>
      {tab === "Batches & expiry" && <section className="inventoryCallout"><span>FEFO</span><div><b>Batch stock is ordered by expiry date</b><small>Use Expiry Management for date editing, near-expiry filters and controlled expired-stock removal.</small></div><Link href="/expiry">Open expiry management →</Link></section>}
      <InventoryTable tab={tab} rows={visibleRows} action={action} />
    </div>
    <ActionDialog request={dialog} close={() => setDialog(null)} />
  </section></main>;
}

function printTransfer(row: any) {
  const popup = window.open("", "_blank", "width=760,height=820");
  if (!popup) return;
  const safe = (value: any) => String(value ?? "").replace(/[&<>"']/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c] || c));
  popup.document.write(`<!doctype html><html><head><title>${safe(row.documentNo || "Stock transfer")}</title><style>body{font:14px Arial,sans-serif;color:#172033;padding:42px}h1{font-size:24px}table{width:100%;border-collapse:collapse;margin:28px 0}th,td{padding:12px;border:1px solid #d9dfeb;text-align:left}.meta{display:grid;grid-template-columns:1fr 1fr;gap:12px}.sign{display:flex;justify-content:space-between;margin-top:80px}.sign span{border-top:1px solid #172033;padding-top:8px;width:30%;text-align:center}@media print{button{display:none}}</style></head><body><h1>Stock ${safe(row.status === "received" ? "Receiving Note" : row.status === "dispatched" ? "Dispatch Note" : "Transfer Note")}</h1><div class="meta"><div><b>Document:</b> ${safe(row.documentNo || row.reference || row.id)}</div><div><b>Status:</b> ${safe(row.status)}</div><div><b>From:</b> ${safe(row.from)}</div><div><b>To:</b> ${safe(row.to)}</div><div><b>Date:</b> ${safe(new Date(row.createdAt).toLocaleDateString("en-LK"))}</div><div><b>Reference:</b> ${safe(row.reference || "—")}</div></div><table><thead><tr><th>Product</th><th>Batch</th><th>Quantity</th></tr></thead><tbody><tr><td>${safe(row.product)}</td><td>${safe(row.batchNo || "General stock")}</td><td>${safe(row.quantity)}</td></tr></tbody></table><p><b>Notes:</b> ${safe(row.notes || "—")}</p><div class="sign"><span>Prepared by</span><span>Dispatched by</span><span>Received by</span></div><button onclick="window.print()">Print / Save PDF</button></body></html>`);
  popup.document.close(); popup.focus();
}

function InventoryTable({ tab, rows, action }: any) {
  return <section className="inventoryRecords"><header><div><small>WORKFLOW HISTORY</small><h2>{tab}</h2></div><span>{rows.length} records</span></header>
    <div className="tableScroll"><table><thead><tr><th>Product / reference</th><th>Details</th><th>Quantity</th><th>Status / date</th><th>Actions</th></tr></thead><tbody>
      {!rows.length && <tr><td colSpan={5} className="emptyTable">No records yet.</td></tr>}
      {rows.map((x: any, i: number) => <tr key={x.id || i}>
        <td><b>{x.product || x.sku || x.batchNo || "Stock movement"}</b><small>{x.sku || x.reference || (x.from && `${x.from} → ${x.to}`) || "—"}</small></td>
        <td><b>{x.kind || x.reason || x.batchNo || "Inventory record"}</b><small>{x.notes || x.user || (x.expiry && `Expires ${new Date(x.expiry).toLocaleDateString("en-LK")}`) || "—"}</small></td>
        <td className={Number(x.quantity ?? x.available) >= 0 ? "amountIn" : "amountOut"}><b>{x.quantity ?? x.available ?? "—"}</b>{x.systemQuantity !== undefined && <small>{x.systemQuantity} → {x.physicalQuantity}</small>}</td>
        <td><span className={`financeStatus ${x.status || "posted"}`}>{x.status || "posted"}</span><small>{formatDate(x.createdAt || x.reportedAt || x.expiry)}</small></td>
        <td><div className="recordActions">
          {tab === "Stock transfers" && <>
            <button onClick={() => printTransfer(x)}>Print document</button>
            <Link href={`/inventory/transfers/${x.id}`}>Open lines</Link>
            {x.status === "draft" && <button onClick={() => action(`/inventory/transfer-documents/${x.id}/approve`)}>Approve</button>}
            {["draft", "approved"].includes(x.status) && <button onClick={() => action(`/inventory/transfer-documents/${x.id}/cancel`, true)}>Cancel</button>}
            {x.status === "approved" && <button onClick={() => action(`/inventory/transfer-documents/${x.id}/dispatch`)}>Dispatch</button>}
            {x.status === "dispatched" && <button onClick={() => action(`/inventory/transfer-documents/${x.id}/receive`)}>Receive</button>}
          </>}
          {tab === "Stock count" && <>{x.status === "draft" && <><button onClick={() => action(`/inventory/adjustment-requests/${x.id}/approve`)}>Approve</button><button onClick={() => action(`/inventory/adjustment-requests/${x.id}/reject`, true)}>Reject</button></>}{x.status === "approved" && <button onClick={() => action(`/inventory/adjustment-requests/${x.id}/finalize`)}>Finalize</button>}</>}
          {tab === "Batches & expiry" && x.status === "available" && x.expiry && new Date(x.expiry) < new Date() && <button onClick={() => action(`/inventory/batches/${x.id}/expire`, true)}>Remove expired</button>}
          {tab === "Damaged stock" && x.status === "draft" && <><button onClick={() => action(`/inventory/damages/${x.id}/finalize`)}>Finalize</button><button onClick={() => action(`/inventory/damages/${x.id}/cancel`, true)}>Cancel</button></>}
        </div></td>
      </tr>)}
    </tbody></table></div>
  </section>;
}
function formatDate(value: any) { return value ? new Date(value).toLocaleDateString("en-LK") : "—"; }
