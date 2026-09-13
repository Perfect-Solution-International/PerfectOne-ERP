"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Sidebar from "../../components/Sidebar";
import { apiFetch } from "../../lib/api";
import { useRequireSession } from "../../lib/useRequireSession";

type Kind = "categories" | "subcategories" | "brands" | "units";
const meta: Record<Kind, { label: string; single: string; placeholder: string }> = {
  categories: { label: "Categories", single: "category", placeholder: "e.g. Beverages" },
  subcategories: { label: "Subcategories", single: "subcategory", placeholder: "e.g. Soft Drinks" },
  brands: { label: "Brands", single: "brand", placeholder: "e.g. Coca-Cola" },
  units: { label: "Units", single: "unit", placeholder: "e.g. Bottle" },
};

export default function NewCatalogRecord() {
  useRequireSession();
  const router = useRouter();
  const params = useSearchParams();
  const kind = ((params.get("kind") as Kind) in meta ? params.get("kind") : "categories") as Kind;
  const editId = params.get("id");
  const info = meta[kind];

  const [name, setName] = useState("");
  const [detail, setDetail] = useState("");
  const [categories, setCategories] = useState<any[]>([]);
  const [loaded, setLoaded] = useState(!editId);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => { if (kind === "subcategories") apiFetch("/catalog-settings/categories").then(setCategories).catch(() => {}); }, [kind]);
  useEffect(() => {
    if (!editId) return;
    apiFetch(`/catalog-settings/${kind}`).then((rows: any[]) => {
      const row = rows.find((r) => r.id === editId);
      if (row) { setName(row.name); setDetail(kind === "subcategories" ? row.categoryId : kind === "units" ? row.detail : ""); }
      setLoaded(true);
    }).catch((e) => { setError(e.message); setLoaded(true); });
  }, [editId, kind]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setError("");
    try {
      await apiFetch(`/catalog-settings/${kind}${editId ? `/${editId}` : ""}`, {
        method: editId ? "PUT" : "POST",
        body: JSON.stringify({ name, categoryId: kind === "subcategories" ? detail : "", symbol: kind === "units" ? detail : "" }),
      });
      router.push(`/catalog-settings?tab=${kind}`);
    } catch (err: any) { setError(err.message); } finally { setBusy(false); }
  };

  return <main className="shell"><Sidebar active="Products" /><section className="app">
    <header className="topbar"><div><span className="crumb">Inventory / Catalogue setup</span><b>{editId ? `Edit ${info.single}` : `New ${info.single}`}</b></div><Link className="ghostButton" href={`/catalog-settings?tab=${kind}`}>Cancel</Link></header>
    <div className="page formPage">
      <div className="formIntro"><span className="pageIcon">{info.single[0].toUpperCase()}</span><p>{editId ? "EDIT RECORD" : "NEW RECORD"}</p><h1>{editId ? name || `Edit ${info.single}` : `Add a ${info.single}`}</h1><small>Reusable across the product catalogue. Deactivate instead of deleting to keep history.</small></div>
      {!loaded ? <div className="dashboardEmpty">Loading…</div> : <form className="saasForm" onSubmit={submit}>
        {error && <button type="button" className="formError" onClick={() => setError("")}>{error}</button>}
        <div className="formSection">
          <div><b>{info.single[0].toUpperCase() + info.single.slice(1)} details</b><small>Shown wherever products are grouped.</small></div>
          <label>Name<input autoFocus required value={name} onChange={(e) => setName(e.target.value)} placeholder={info.placeholder} /></label>
          {kind === "subcategories" && <label>Parent category<select required value={detail} onChange={(e) => setDetail(e.target.value)}><option value="">Select category</option>{categories.filter((c) => c.isActive).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select></label>}
          {kind === "units" && <label>Unit symbol<input required value={detail} onChange={(e) => setDetail(e.target.value)} placeholder="e.g. kg, pc, L" /></label>}
        </div>
        <footer><Link href={`/catalog-settings?tab=${kind}`}>Cancel</Link><button className="new" disabled={busy}>{busy ? "Saving…" : editId ? "Save changes" : `Add ${info.single}`}</button></footer>
      </form>}
    </div>
  </section></main>;
}
