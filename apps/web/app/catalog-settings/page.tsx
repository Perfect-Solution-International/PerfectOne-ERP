"use client";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Sidebar from "../components/Sidebar";
import ActionDialog, { type ActionDialogRequest } from "../components/ActionDialog";
import { apiFetch, can } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";

type Kind = "categories" | "subcategories" | "brands" | "units";
type Item = { id: string; name: string; isActive: boolean; categoryId: string; detail: string; productCount: number };
const tabs: { key: Kind; label: string; single: string; hint: string }[] = [
  { key: "categories", label: "Categories", single: "category", hint: "Primary product groups" },
  { key: "subcategories", label: "Subcategories", single: "subcategory", hint: "Groups assigned to a category" },
  { key: "brands", label: "Brands", single: "brand", hint: "Manufacturers and product brands" },
  { key: "units", label: "Units", single: "unit", hint: "Stock and selling measurements" },
];

export default function CatalogueSettings() {
  useRequireSession();
  const router = useRouter();
  const initial = useSearchParams().get("tab") as Kind | null;
  const [kind, setKind] = useState<Kind>(tabs.some((t) => t.key === initial) ? (initial as Kind) : "categories");
  const [items, setItems] = useState<Item[]>([]);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState("name");
  const [message, setMessage] = useState("");
  const [dialog, setDialog] = useState<ActionDialogRequest | null>(null);

  const load = () => apiFetch(`/catalog-settings/${kind}`).then(setItems).catch((e: any) => setMessage(e.message));
  useEffect(() => { load(); }, [kind]);

  const list = useMemo(() => items.filter((x) => `${x.name} ${x.detail}`.toLowerCase().includes(query.toLowerCase()))
    .sort((a, b) => sort === "usage" ? b.productCount - a.productCount : sort === "status" ? Number(b.isActive) - Number(a.isActive) : a.name.localeCompare(b.name)), [items, query, sort]);

  const toggle = async (x: Item) => { try { await apiFetch(`/catalog-settings/${kind}/${x.id}/status`, { method: "POST", body: JSON.stringify({ isActive: !x.isActive }) }); load(); } catch (e: any) { setMessage(e.message); } };
  const remove = (x: Item) => setDialog({ title: `Delete ${x.name}?`, message: x.productCount ? `This ${current.single} is assigned to ${x.productCount} products and deletion will be blocked. Deactivate it instead.` : "This unused master-data record will be permanently deleted.", confirmLabel: "Delete record", tone: "danger", onConfirm: async () => { await apiFetch(`/catalog-settings/${kind}/${x.id}`, { method: "DELETE" }); setMessage("Item deleted."); load(); } });
  const current = tabs.find((x) => x.key === kind)!;

  return <main className="shell"><Sidebar active="Products" /><section className="app">
    <header className="topbar"><div><span className="crumb">Inventory / Catalogue setup</span><b>Categories &amp; attributes</b></div>
      <div className="topbarActions">
        {can("products.add") && <Link className="new" href={`/catalog-settings/new?kind=${kind}`}>＋ New {current.single}</Link>}
        <Link className="ghostButton" href="/barcodes">Barcode manager</Link>
      </div>
    </header>
    <div className="page taxonomyPage">
      <div className="pageHeading"><div><p>CATALOGUE STRUCTURE</p><h1>Product classification</h1><small>Manage reusable categories, brands and measurement units.</small></div></div>
      <div className="taxonomyTabs">{tabs.map((t) => <button key={t.key} className={kind === t.key ? "active" : ""} onClick={() => { setKind(t.key); setQuery(""); }}><b>{t.label}</b><small>{t.hint}</small></button>)}</div>
      <section className="taxonomyList">
        <div className="taxonomyTools">
          <label className="catalogSearch"><span>⌕</span><input placeholder={`Search ${current.label.toLowerCase()}`} value={query} onChange={(e) => setQuery(e.target.value)} /></label>
          <select value={sort} onChange={(e) => setSort(e.target.value)}><option value="name">Sort: Name</option><option value="usage">Sort: Product usage</option><option value="status">Sort: Status</option></select>
        </div>
        {message && <button className="catalogMessage" onClick={() => setMessage("")}>{message}<span>×</span></button>}
        <div className="taxonomyRows">
          {list.map((x) => <article key={x.id}>
            <i>{x.name[0].toUpperCase()}</i>
            <div><b>{x.name}</b><small>{kind === "subcategories" ? x.detail : kind === "units" ? `Symbol: ${x.detail}` : `${x.productCount} product${x.productCount === 1 ? "" : "s"}`}</small></div>
            <span className={`status ${x.isActive ? "" : "inactive"}`}>{x.isActive ? "Active" : "Inactive"}</span>
            <div className="rowActions">
              {can("products.edit") && <><button onClick={() => router.push(`/catalog-settings/new?kind=${kind}&id=${x.id}`)}>Edit</button><button onClick={() => toggle(x)}>{x.isActive ? "Deactivate" : "Activate"}</button></>}
              {can("products.delete") && <button className="dangerLink" onClick={() => remove(x)}>Delete</button>}
            </div>
          </article>)}
          {list.length === 0 && <div className="dashboardEmpty">No records found.</div>}
        </div>
      </section>
    </div>
  </section><ActionDialog request={dialog} close={() => setDialog(null)} /></main>;
}
