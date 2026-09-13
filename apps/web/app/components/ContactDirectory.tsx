"use client";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { useRouter, useSearchParams } from "next/navigation";
import Sidebar from "./Sidebar";
import ActionDialog, { type ActionDialogRequest } from "./ActionDialog";
import { apiFetch, can } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";
import { money as cash, moneyFigure } from "../lib/format";

export default function ContactDirectory({ kind }: { kind: "suppliers" | "customers" }) {
  useRequireSession();
  const router = useRouter();
  const searchParams = useSearchParams();
  const supplier = kind === "suppliers";
  const noun = supplier ? "supplier" : "customer";
  const permission = supplier ? "suppliers" : "customers";
  const ledgerMode = searchParams.get("view") === "ledger";
  const [rows, setRows] = useState<any[]>([]);
  const [query, setQuery] = useState("");
  const [profile, setProfile] = useState<any>(null);
  const [profileTab, setProfileTab] = useState<"overview" | "products" | "history">("overview");
  const [message, setMessage] = useState("");
  const [dialog, setDialog] = useState<ActionDialogRequest | null>(null);

  const load = () => apiFetch(`/directory/${kind}`).then(setRows).catch((e) => setMessage(e.message));
  useEffect(() => { load(); }, [kind]);
  useEffect(() => {
    if (!profile) return;
    const previousOverflow = document.body.style.overflow;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setProfile(null);
    };
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [profile]);

  const list = useMemo(() => rows.filter((x) => `${x.name} ${x.company} ${x.phone} ${x.email}`.toLowerCase().includes(query.toLowerCase())), [rows, query]);
  const dueFigure = moneyFigure(rows.reduce((total, row) => total + Number(row.balance || 0), 0));
  const remove = (x: any) => setDialog({ title: `Remove ${x.name}?`, message: "Accounts with transactions will be archived so ledgers and financial reports remain intact. Only unused accounts are permanently deleted.", confirmLabel: `Remove ${noun}`, tone: "danger", onConfirm: async () => { const r = await apiFetch(`/directory/${kind}/${x.id}`, { method: "DELETE" }); setMessage(r.result === "archived" ? "Account archived to preserve history." : "Unused account deleted."); load(); } });
  const toggle = async (x: any) => { await apiFetch(`/directory/${kind}/${x.id}/status`, { method: "POST", body: JSON.stringify({ isActive: !x.isActive }) }); load(); };
  const show = async (x: any) => { setProfileTab("overview"); const [data, products] = await Promise.all([apiFetch(`/directory/${kind}/${x.id}`), supplier ? apiFetch(`/procurement/suppliers/${x.id}/products`) : Promise.resolve([])]); setProfile({ contact: x, data, products }); };

  return <main className="shell"><Sidebar active={supplier ? "Suppliers" : "Customers"} /><section className="app">
    <header className="topbar"><div><span className="crumb">Directory / {supplier ? "Suppliers" : "Customers"}</span><b>{supplier ? "Supplier" : "Customer"} management</b></div>
      {can(`${permission}.add`) && <Link className="new" href={`/${kind}/new`}>＋ Add {noun}</Link>}
    </header>
    <div className="page directoryPage">
      <div className="pageHeading">
        <div><p>{ledgerMode ? "ACCOUNT HISTORY" : supplier ? "SUPPLIER LIST" : "CUSTOMER LIST"}</p><h1>{ledgerMode ? `${supplier ? "Supplier" : "Customer"} accounts` : supplier ? "Suppliers" : "Customers"}</h1><small>{ledgerMode ? `Select a ${noun} to view purchases, payments, returns and the complete account history.` : `Manage ${noun} contact details, credit limits and account history.`}</small></div>
        <div className="catalogStats"><span><b>{rows.filter((x) => x.isActive).length}</b> Active</span><span><Figure value={rows.reduce((a, x) => a + x.balance, 0)}/> Amount due</span></div>
      </div>
      <div className="productToolbar"><label className="catalogSearch"><span>⌕</span><input placeholder="Search name, phone, email or company" value={query} onChange={(e) => setQuery(e.target.value)} /></label></div>
      {message && <button className="catalogMessage" onClick={() => setMessage("")}>{message}<span>×</span></button>}
      <section className="directoryTable"><div className="productTableWrap"><table><thead><tr><th>{supplier ? "Supplier" : "Customer"}</th><th>Contact details</th><th>Amount due</th><th>Status</th><th>Actions</th></tr></thead><tbody>
        {list.map((x) => <tr key={x.id}>
          <td><div className="directoryIdentity"><i>{x.name[0]}</i><span><b>{x.name}</b><small>{x.company || x.detail}</small></span></div></td>
          <td><b>{x.phone || "No phone"}</b><small>{x.email || "No email"}</small></td>
          <td><b>{cash(x.balance)}</b><small>Limit {cash(x.creditLimit)}</small></td>
          <td><span className={`status ${x.isActive ? "" : "inactive"}`}>{x.isActive ? "Active" : "Archived"}</span></td>
          <td><div className="productActions">
            <button onClick={() => show(x)}>{ledgerMode ? "View ledger" : "Profile"}</button>
            {can(`${permission}.edit`) && <><button onClick={() => router.push(`/${kind}/new?id=${x.id}`)}>Edit</button><button onClick={() => toggle(x)}>{x.isActive ? "Deactivate" : "Activate"}</button></>}
            {can(`${permission}.delete`) && <button className="dangerLink" onClick={() => remove(x)}>Delete</button>}
          </div></td>
        </tr>)}
      </tbody></table></div></section>
    </div>
  </section>
  {false && profile && <><button className="historyBackdrop" onClick={() => setProfile(null)} /><aside className="contactProfile">
    <header><div><small>ACCOUNT PROFILE</small><h2>{profile.contact.name}</h2></div><button onClick={() => setProfile(null)}>×</button></header>
    <div className="profileMetrics"><Metric name="Total purchases" value={profile.data.total} /><Metric name="Amount paid" value={profile.data.paid} /><Metric name="Amount due" value={profile.data.outstanding} /><Metric name="Returns" value={profile.data.returns} /></div>
    <nav className="profileTabs"><button className={profileTab === "overview" ? "active" : ""} onClick={() => setProfileTab("overview")}>Overview</button>{supplier && <button className={profileTab === "products" ? "active" : ""} onClick={() => setProfileTab("products")}>Products <span>{profile.products.length}</span></button>}<button className={profileTab === "history" ? "active" : ""} onClick={() => setProfileTab("history")}>Account history</button></nav>
    {profileTab === "overview" && <><section className="contactInfo"><h3>Contact information</h3><p><b>Phone</b>{profile.contact.phone || "Not provided"}</p><p><b>Email</b>{profile.contact.email || "Not provided"}</p><p><b>Address</b>{profile.contact.address || "Not provided"}</p></section>{supplier && <div className="profileQuickActions"><Link href={`/products/new?supplierId=${profile.contact.id}`}>Create new product</Link><Link href={`/procurement/grns/new?supplierId=${profile.contact.id}`}>Receive stock</Link><Link href="/finance?section=supplier-payments">Pay supplier</Link></div>}</>}
    {profileTab === "products" && <section className="supplierProfileProducts"><header><div><h3>Products from this supplier</h3><small>Pricing, current stock and recent purchase information.</small></div><Link href={`/products/new?supplierId=${profile.contact.id}`}>+ New product</Link></header>{profile.products.length ? profile.products.map((x: any) => <article key={x.productId}><span><b>{x.name}</b><small>{x.sku}{x.supplierProductCode ? ` · ${x.supplierProductCode}` : ""}</small></span><div><small>Last cost</small><b>{cash(x.lastPurchasePrice || x.defaultPurchasePrice)}</b></div><div><small>Selling</small><b>{cash(x.recommendedSellingPrice || x.sellingPrice)}</b></div><div><small>Stock</small><b>{x.stock}</b></div><Link href={`/products/${x.productId}`}>View</Link></article>) : <div className="profileEmpty"><b>No products assigned yet</b><span>Create a product or assign one while receiving stock.</span></div>}</section>}
    {profileTab === "history" && <><h3>Account history</h3><div className="profileLedger">{profile.data.ledger.map((x: any) => <article key={x.id}><i>{x.type[0].toUpperCase()}</i><span><b>{x.type.replaceAll("_", " ")}</b><small>{new Date(x.createdAt).toLocaleString()}</small></span><strong>{cash(x.debit || x.credit)}</strong></article>)}</div>{!profile.data.ledger.length && <div className="profileEmpty"><b>No transactions yet</b><span>Purchases, returns and payments will appear here.</span></div>}</>}
  </aside></>}
  {profile && createPortal(<ProfileDrawer profile={profile} supplier={supplier} tab={profileTab} setTab={setProfileTab} close={() => setProfile(null)} />, document.body)}
  <ActionDialog request={dialog} close={() => setDialog(null)} />
  </main>;
}
function Figure({ value }: { value: number }) { const f = moneyFigure(value); return <b className={f.className} title={f.title}>{f.text}</b>; }
function Metric({ name, value }: any) { return <span><small>{name}</small><Figure value={value}/></span>; }

function ProfileDrawer({ profile, supplier, tab, setTab, close }: any) {
  return <div className="contactProfileLayer">
    <button className="contactProfileBackdrop" aria-label="Close profile" onClick={close} />
    <aside className="contactProfile" role="dialog" aria-modal="true" aria-labelledby="contact-profile-title">
      <header className="contactProfileHeader">
        <div className="contactProfileIdentity"><i>{profile.contact.name?.[0] || "S"}</i><span><small>{supplier ? "SUPPLIER PROFILE" : "CUSTOMER PROFILE"}</small><h2 id="contact-profile-title">{profile.contact.name}</h2><p>{profile.contact.company || (profile.contact.isActive ? "Active account" : "Archived account")}</p></span></div>
        <button className="contactProfileClose" aria-label="Close profile" onClick={close}>×</button>
      </header>
      <div className="contactProfileBody">
        {supplier && <div className="profileQuickActions"><Link href={`/procurement/grns/new?supplierId=${profile.contact.id}`}>Receive stock</Link><Link href={`/products/new?supplierId=${profile.contact.id}`}>Add supplier product</Link><Link href="/finance?section=supplier-payments">Record payment</Link></div>}
        <div className="profileMetrics"><Metric name={supplier ? "Total purchases" : "Total sales"} value={profile.data.total} /><Metric name="Amount paid" value={profile.data.paid} /><Metric name="Amount due" value={profile.data.outstanding} /><Metric name="Returns" value={profile.data.returns} /></div>
        <nav className="profileTabs" aria-label="Profile sections"><button className={tab === "overview" ? "active" : ""} onClick={() => setTab("overview")}>Overview</button>{supplier && <button className={tab === "products" ? "active" : ""} onClick={() => setTab("products")}>Products <span>{profile.products.length}</span></button>}<button className={tab === "history" ? "active" : ""} onClick={() => setTab("history")}>Account history</button></nav>
        <div className="contactProfileContent">
          {tab === "overview" && <section className="contactInfo"><h3>Contact information</h3><p><b>Phone</b><span>{profile.contact.phone || "Not provided"}</span></p><p><b>Email</b><span>{profile.contact.email || "Not provided"}</span></p><p><b>Address</b><span>{profile.contact.address || "Not provided"}</span></p></section>}
          {tab === "products" && <section className="supplierProfileProducts"><header><div><h3>Products from this supplier</h3><small>Current pricing, stock and supplier product codes.</small></div><Link href={`/products/new?supplierId=${profile.contact.id}`}>+ New product</Link></header>{profile.products.length ? profile.products.map((x: any) => <article key={`${x.productId}-${x.supplierProductCode || "default"}`}><span><b>{x.name}</b><small>{x.sku}{x.supplierProductCode ? ` · ${x.supplierProductCode}` : ""}</small></span><div><small>Last cost</small><b>{cash(x.lastPurchasePrice || x.defaultPurchasePrice)}</b></div><div><small>Selling</small><b>{cash(x.recommendedSellingPrice || x.sellingPrice)}</b></div><div><small>Stock</small><b>{x.stock}</b></div><Link href={`/products/${x.productId}`}>View</Link></article>) : <div className="profileEmpty"><b>No products assigned yet</b><span>Create a product or assign one while receiving stock.</span></div>}</section>}
          {tab === "history" && <section className="profileHistory"><h3>Account history</h3><div className="profileLedger">{profile.data.ledger.map((x: any) => <article key={x.id}><i>{x.type[0].toUpperCase()}</i><span><b>{x.type.replaceAll("_", " ")}</b><small>{new Date(x.createdAt).toLocaleString()}</small></span><strong>{cash(x.debit || x.credit)}</strong></article>)}</div>{!profile.data.ledger.length && <div className="profileEmpty"><b>No transactions yet</b><span>Purchases, returns and payments will appear here.</span></div>}</section>}
        </div>
      </div>
    </aside>
  </div>;
}
