"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Sidebar from "./Sidebar";
import { apiFetch } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";

const blank = { name: "", company: "", phone: "", email: "", address: "", taxInformation: "", openingBalance: 0, creditLimit: 0, paymentTerms: "", customerType: "retail", notes: "" };

export default function ContactForm({ kind }: { kind: "customers" | "suppliers" }) {
  useRequireSession();
  const supplier = kind === "suppliers";
  const noun = supplier ? "supplier" : "customer";
  const router = useRouter();
  const editId = useSearchParams().get("id");
  const [form, setForm] = useState<any>(blank);
  const [loaded, setLoaded] = useState(!editId);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const set = (k: string, v: any) => setForm((x: any) => ({ ...x, [k]: v }));

  useEffect(() => {
    if (!editId) return;
    apiFetch(`/directory/${kind}`).then((rows: any[]) => {
      const row = rows.find((r) => r.id === editId);
      if (row) setForm({ ...blank, ...row, paymentTerms: supplier ? row.detail : "", customerType: supplier ? "retail" : row.detail || "retail" });
      setLoaded(true);
    }).catch((e) => { setError(e.message); setLoaded(true); });
  }, [editId, kind, supplier]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setError("");
    try {
      await apiFetch(`/directory/${kind}${editId ? `/${editId}` : ""}`, { method: editId ? "PUT" : "POST", body: JSON.stringify(form) });
      router.push(`/${kind}`);
    } catch (err: any) { setError(err.message); } finally { setBusy(false); }
  };

  return <main className="shell"><Sidebar active={supplier ? "Suppliers" : "Customers"} /><section className="app">
    <header className="topbar"><div><span className="crumb">Directory / {supplier ? "Suppliers" : "Customers"}</span><b>{editId ? `Edit ${noun}` : `Add ${noun}`}</b></div><Link className="ghostButton" href={`/${kind}`}>Cancel</Link></header>
    <div className="page formPage">
      <div className="formIntro"><span className="pageIcon">{supplier ? "S" : "C"}</span><p>{editId ? "EDIT ACCOUNT" : "NEW ACCOUNT"}</p><h1>{editId ? form.name || "Edit account" : `Add a ${noun}`}</h1><small>Opening balances create permanent ledger records.</small></div>
      {!loaded ? <div className="dashboardEmpty">Loading account…</div> : <form className="saasForm" onSubmit={submit}>
        {error && <button type="button" className="formError" onClick={() => setError("")}>{error}</button>}
        <div className="formSection">
          <div><b>Contact details</b><small>How you reach this {noun}.</small></div>
          <label>Name<input autoFocus value={form.name} onChange={(e) => set("name", e.target.value)} required /></label>
          {supplier && <label>Company<input value={form.company} onChange={(e) => set("company", e.target.value)} /></label>}
          <label>Phone<input value={form.phone} onChange={(e) => set("phone", e.target.value)} /></label>
          <label>Email<input type="email" value={form.email} onChange={(e) => set("email", e.target.value)} /></label>
          <label className="wide">Address<input value={form.address} onChange={(e) => set("address", e.target.value)} /></label>
          {supplier && <label>Tax information<input value={form.taxInformation} onChange={(e) => set("taxInformation", e.target.value)} /></label>}
        </div>
        <div className="formSection">
          <div><b>Credit &amp; terms</b><small>Controls limits and starting balance.</small></div>
          {!editId && <label>Opening balance<input type="number" min="0" step="0.01" value={form.openingBalance || ""} onChange={(e) => set("openingBalance", Number(e.target.value))} /></label>}
          <label>Credit limit<input type="number" min="0" step="0.01" value={form.creditLimit || ""} onChange={(e) => set("creditLimit", Number(e.target.value))} /></label>
          {supplier
            ? <label>Payment terms<input value={form.paymentTerms} onChange={(e) => set("paymentTerms", e.target.value)} placeholder="e.g. Net 30" /></label>
            : <label>Customer type<select value={form.customerType} onChange={(e) => set("customerType", e.target.value)}><option value="retail">Retail</option><option value="wholesale">Wholesale</option><option value="corporate">Corporate</option></select></label>}
          <label className="wide">Notes<textarea rows={3} value={form.notes} onChange={(e) => set("notes", e.target.value)} /></label>
        </div>
        <footer><Link href={`/${kind}`}>Cancel</Link><button className="new" disabled={busy}>{busy ? "Saving…" : editId ? "Save changes" : `Create ${noun}`}</button></footer>
      </form>}
    </div>
  </section></main>;
}
