"use client";

import { useEffect, useState } from "react";
import Sidebar from "../components/Sidebar";
import { apiFetch } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";

const tabs = ["Expiry alerts", "Batches", "Stock adjustment", "Cashier sessions", "Cash & bank", "Accounting reports"];
type Product = { ID: string; Name: string; SKU: string; Stock: number; Unit?: string };

export default function Operations() {
  useRequireSession();
  const [tab, setTab] = useState("Expiry alerts");
  const [data, setData] = useState<any[]>([]);
  const [accounts, setAccounts] = useState<any[]>([]);
  const [products, setProducts] = useState<Product[]>([]);
  const [selectedProduct, setSelectedProduct] = useState("");
  const [message, setMessage] = useState("");

  const load = () => {
    const path = tab === "Expiry alerts" ? "/stock/expiry-alerts" : tab === "Batches" ? "/stock/batches" : tab === "Cashier sessions" ? "/cashier-sessions" : tab === "Cash & bank" ? "/cash-accounts" : "/reports/accounting";
    if (tab === "Stock adjustment") {
      apiFetch("/products").then((rows) => {
        setProducts(rows);
        setSelectedProduct((current) => current || rows[0]?.ID || "");
      }).catch((error) => setMessage(error.message));
      setData([]);
      return;
    }
    apiFetch(path).then((result) => setData(tab === "Accounting reports" ? [result] : result)).catch((error) => setMessage(error.message));
    if (tab === "Cash & bank") apiFetch("/cash-accounts").then(setAccounts);
  };

  useEffect(load, [tab]);

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    const fields = new FormData(form);
    try {
      if (tab === "Stock adjustment") {
        await apiFetch("/stock/adjustments", { method: "POST", body: JSON.stringify({
          productId: fields.get("productId"), physicalQuantity: Number(fields.get("physicalQuantity")),
          reason: fields.get("reason"), notes: fields.get("notes"), requestId: crypto.randomUUID(),
        }) });
      }
      if (tab === "Cash & bank") {
        await apiFetch("/cash-transactions", { method: "POST", body: JSON.stringify({
          accountId: fields.get("accountId"), type: fields.get("type"), amount: Number(fields.get("amount")), description: fields.get("description"),
        }) });
      }
      setMessage("Saved successfully.");
      form.reset();
      setSelectedProduct(products[0]?.ID || "");
      load();
    } catch (error: any) { setMessage(error.message); }
  };

  const product = products.find((item) => item.ID === selectedProduct);
  return <main className="shell"><Sidebar active={tab === "Cash & bank" ? "Cash & Bank" : "Accounting"}/><section className="app"><header className="topbar"><div><span className="crumb">Workspace / Operations</span><b>Operations control</b></div></header><div className="page"><div className="pageHeading"><div><p>BACK OFFICE</p><h1>Inventory, cash & accounting</h1><small>Daily operational tools in one workspace.</small></div></div><div className="operationsLayout"><nav className="tabRail">{tabs.map((item) => <button className={item === tab ? "active" : ""} onClick={() => { setMessage(""); setTab(item); }} key={item}><span>{item[0]}</span>{item}</button>)}</nav><section className="operationContent">{message && <div className="notice">{message}</div>}{tab === "Stock adjustment" ? <OperationForm title="Stock count adjustment" onSubmit={save}><label>Product<select name="productId" value={selectedProduct} onChange={(event) => setSelectedProduct(event.target.value)} required>{products.map((item) => <option value={item.ID} key={item.ID}>{item.Name} · {item.SKU}</option>)}</select></label><div className="notice">System quantity: <b>{product?.Stock ?? 0} {product?.Unit || "units"}</b>. Enter the physically counted quantity below.</div><label>Physical quantity<input name="physicalQuantity" type="number" min="0" step="0.001" placeholder="Counted quantity" required/></label><label>Reason<select name="reason" required defaultValue=""><option value="" disabled>Select a reason</option><option>Physical stock count</option><option>Data correction</option><option>Found stock</option><option>Stock shortage</option><option>Other</option></select></label><label>Notes<input name="notes" placeholder="Count sheet, explanation or reference"/></label></OperationForm> : tab === "Cash & bank" ? <OperationForm title="Record cash / bank transaction" onSubmit={save}><select name="accountId">{accounts.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select><select name="type"><option value="deposit">Deposit</option><option value="withdrawal">Withdrawal</option><option value="expense">Expense</option><option value="income">Income</option></select><input name="amount" type="number" min="0.01" step="0.01" placeholder="Amount" required/><input name="description" placeholder="Description"/></OperationForm> : <DataPanel title={tab} rows={data}/>}</section></div></div></section></main>;
}

function OperationForm({ title, onSubmit, children }: any) { return <form className="operationForm" onSubmit={onSubmit}><header><p>NEW ENTRY</p><h2>{title}</h2></header><div>{children}</div><footer><button className="new">Save record</button></footer></form>; }
const humanize = (key: string) => key.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/[_-]+/g, " ").replace(/^\w/, (c) => c.toUpperCase());
const isoDate = /^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}|$)/;
function formatCell(value: any): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (typeof value === "number") return value.toLocaleString("en-LK");
  if (typeof value === "string" && isoDate.test(value)) { const d = new Date(value); return value.length <= 10 ? d.toLocaleDateString("en-LK") : d.toLocaleString("en-LK"); }
  if (typeof value === "object") return Object.entries(value).map(([k, v]) => `${humanize(k)}: ${formatCell(v)}`).join(", ");
  return String(value);
}
function DataPanel({ title, rows }: any) {
  const columns = rows.length ? Object.keys(rows[0]) : [];
  return <section className="dataCard"><header><div><p>LIVE DATA</p><h2>{title}</h2></div><span className="countBadge">{rows.length} records</span></header>{rows.length ? <div className="tableScroll"><table><thead><tr>{columns.map((key) => <th key={key}>{humanize(key)}</th>)}</tr></thead><tbody>{rows.map((row: any, index: number) => <tr key={index}>{columns.map((key) => <td key={key}>{formatCell(row[key])}</td>)}</tr>)}</tbody></table></div> : <div className="emptyState">No records available.</div>}</section>;
}
