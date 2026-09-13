"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import Image from "next/image";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { apiFetch, clearSession, getSession, type Session } from "../lib/api";
import "./Sidebar.css";

type Submenu = { label: string; permission?: string; href: string };
type Menu = { label: string; permission: string; href?: string; icon: IconName; subs?: Submenu[] };
type MenuGroup = { label: string; menus: Menu[] };
type IconName = "home" | "cart" | "box" | "truck" | "users" | "supplier" | "chart" | "wallet" | "file" | "shield" | "user" | "search" | "store" | "logout" | "plus" | "chevron" | "close" | "menu";

const GROUPS: MenuGroup[] = [
  { label: "Workspace", menus: [{ label: "Dashboard", permission: "dashboard", href: "/", icon: "home" }] },
  { label: "Sales", menus: [{ label: "Sales", permission: "pos", icon: "cart", subs: [
    { label: "New sale", href: "/pos" }, { label: "Sales history", href: "/sales" },
    { label: "Sales returns", permission: "sales_returns", href: "/sales?status=has_returns" },
    { label: "Cashier sessions", permission: "cashier_session", href: "/cashier-sessions" },
    { label: "Discounts & promotions", permission: "promotions.view", href: "/promotions" },
  ] }] },
  { label: "Purchases", menus: [{ label: "Purchases", permission: "purchases", icon: "truck", subs: [
    { label: "Purchase orders", href: "/procurement/orders" }, { label: "Receive stock (GRN)", href: "/procurement/grns" },
    { label: "Return items to supplier", href: "/procurement/returns" },
  ] }] },
  { label: "Stock & products", menus: [{ label: "Stock & products", permission: "inventory", icon: "box", subs: [
    { label: "Current stock", href: "/inventory" }, { label: "Products", permission: "products", href: "/products" },
    { label: "Categories, brands & units", permission: "products", href: "/catalog-settings" },
    { label: "Print barcode labels", permission: "products", href: "/barcodes" },
    { label: "Count & adjust stock", href: "/inventory?section=counts" }, { label: "Transfer stock", href: "/inventory?section=transfers" },
    { label: "Repack & record wastage", href: "/inventory/conversions" },
    { label: "Expiry & batches", permission: "expiry", href: "/expiry" }, { label: "Damaged items", permission: "damage", href: "/inventory?section=damage" },
  ] }] },
  { label: "Business partners", menus: [
    { label: "Customers", permission: "contacts", icon: "users", subs: [{ label: "Customer list", href: "/customers" }, { label: "Receive customer payment", permission: "accounting", href: "/finance?section=customer-receipts" }, { label: "Customer accounts", href: "/customers?view=ledger" }] },
    { label: "Suppliers", permission: "contacts", icon: "supplier", subs: [{ label: "Supplier list", href: "/suppliers" }, { label: "Pay suppliers", permission: "accounting", href: "/finance?section=supplier-payments" }, { label: "Supplier accounts", href: "/suppliers?view=ledger" }] },
  ] },
  { label: "Money & accounting", menus: [
    { label: "Money & banking", permission: "cash_bank", icon: "wallet", subs: [{ label: "Cash transactions", href: "/finance" }, { label: "Receive customer payment", href: "/finance?section=customer-receipts" }, { label: "Pay suppliers", href: "/finance?section=supplier-payments" }, { label: "Expenses", href: "/finance?section=expenses" }, { label: "Transfer money", href: "/finance?section=transfers" }, { label: "Cash & bank accounts", href: "/finance?section=accounts" }] },
    { label: "Accounting", permission: "accounting", icon: "file", subs: [{ label: "Accounts list", href: "/accounting" }, { label: "Journal entries", href: "/accounting?section=journals" }] },
  ] },
  { label: "Reports", menus: [
    { label: "Business reports", permission: "reports", href: "/reports", icon: "chart" },
    { label: "Financial reports", permission: "financial_reports", icon: "chart", subs: [
      { label: "Trial balance", href: "/accounting?section=trial-balance" }, { label: "Profit & loss", href: "/accounting?section=profit-loss" },
      { label: "Balance sheet", href: "/accounting?section=balance-sheet" }, { label: "General ledger", href: "/accounting?section=general-ledger" },
      { label: "Reconciliation", href: "/accounting?section=reconciliation" },
    ] },
  ] },
  { label: "System management", menus: [
    { label: "Users", permission: "users", href: "/users", icon: "user" }, { label: "User roles & access", permission: "roles", href: "/roles", icon: "shield" },
    { label: "Stores & branches", permission: "inventory", href: "/branches", icon: "store" }, { label: "Approval requests", permission: "inventory", href: "/approvals", icon: "shield" },
    { label: "Alerts & archived records", permission: "notifications.view", href: "/admin-centre", icon: "shield" }, { label: "Import & export data", permission: "users", href: "/data-tools", icon: "file" },
    { label: "Backup & restore", permission: "roles", href: "/backups", icon: "store" }, { label: "Offline transactions", permission: "sync.view", href: "/sync", icon: "file" },
    { label: "System settings", permission: "roles", href: "/settings", icon: "store" },
  ] },
];

const hasAccess = (session: Session | null, permission?: string) => !!session && (!permission || session.Role === "super_admin" || session.Permissions?.includes(permission));

export default function Sidebar({ active: _active }: { active?: string }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [session, setSession] = useState<Session | null>(null);
  const [ready, setReady] = useState(false);
  const [open, setOpen] = useState("");
  const [mobile, setMobile] = useState(false);
  const [query, setQuery] = useState("");
  const [now, setNow] = useState<Date | null>(null);

  useEffect(() => { setSession(getSession()); setReady(true); }, []);
  useEffect(() => { setNow(new Date()); const timer = setInterval(() => setNow(new Date()), 1000); return () => clearInterval(timer); }, []);

  const permitted = useMemo(() => GROUPS.map((group) => ({
    ...group,
    menus: group.menus.map((menu) => ({ ...menu, subs: (menu.subs || []).filter((sub) => hasAccess(session, sub.permission || menu.permission)) })).filter((menu) => hasAccess(session, menu.permission) || (menu.subs?.length || 0) > 0),
  })).filter((group) => group.menus.length > 0), [session]);

  const groups = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return permitted;
    return permitted.map((group) => ({ ...group, menus: group.menus.map((menu) => menu.label.toLowerCase().includes(needle) ? menu : ({ ...menu, subs: menu.subs?.filter((sub) => sub.label.toLowerCase().includes(needle)) })).filter((menu) => menu.label.toLowerCase().includes(needle) || (menu.subs?.length || 0) > 0) })).filter((group) => group.menus.length > 0);
  }, [permitted, query]);

  const allMenus = permitted.flatMap((group) => group.menus);
  const hrefMatches = (href?: string) => {
    if (!href) return false;
    const [targetPath, queryString = ""] = href.split("?");
    if (pathname !== targetPath) return false;
    const expected = new URLSearchParams(queryString);
    if (!expected.size) return searchParams.size === 0;
    return [...expected].every(([key, value]) => searchParams.get(key) === value);
  };
  const submenuRoutes = allMenus.flatMap((menu) => (menu.subs || []).map((sub) => ({ menu, sub })));
  const exactSub = submenuRoutes.find(({ sub }) => hrefMatches(sub.href));
  const nestedSub = submenuRoutes.filter(({ sub }) => {
    const [targetPath, queryString = ""] = sub.href.split("?");
    return !queryString && targetPath !== "/" && pathname.startsWith(`${targetPath}/`);
  }).sort((a, b) => b.sub.href.length - a.sub.href.length)[0];
  const transferSub = pathname.startsWith("/inventory/transfers/") ? submenuRoutes.find(({ sub }) => sub.href === "/inventory?section=transfers") : undefined;
  const urlSub = exactSub || transferSub || nestedSub;
  const urlMenu = allMenus.find((menu) => hrefMatches(menu.href));
  const selectedMenu = urlSub?.menu || urlMenu;
  const selectedSub = urlSub?.sub;
  const isSubActive = (sub: Submenu) => selectedSub?.href === sub.href;
  const isMenuActive = (menu: Menu) => selectedMenu?.label === menu.label;

  useEffect(() => {
    const current = permitted.flatMap((group) => group.menus).find((menu) => menu.subs?.length && isMenuActive(menu));
    if (current) setOpen(current.label);
  }, [pathname, searchParams, ready, permitted]);

  const closeMobile = () => setMobile(false);
  const signOut = () => apiFetch("/auth/logout", { method: "POST" }).catch(() => {}).finally(() => { clearSession(); router.replace("/login"); });

  return <>
    <button className="mobileMenuToggle floatingGlassButton" aria-label="Open navigation" onClick={() => setMobile(true)}><Icon name="menu" /></button>
    <header className="globalStatusBar">
      {now && <div className="statusBarClock"><small>{now.toLocaleDateString(undefined, { weekday: "short", day: "2-digit", month: "short", year: "numeric" })}</small><b>{now.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</b></div>}
      <div className="statusBarUser">
        <span className="statusBarAvatar">{session?.Name?.slice(0, 2).toUpperCase() || "U"}</span>
        <div><b>{session?.Name || "Loading user"}</b><small>{session?.Role?.replaceAll("_", " ") || "workspace member"}</small></div>
      </div>
    </header>
    {mobile && <button className="navBackdrop" aria-label="Close navigation" onClick={closeMobile} />}
    <aside className={`sidebar modernSidebar ${mobile ? "mobileOpen" : ""}`} aria-label="Application navigation">
      <header className="sidebarHeader">
        <Link href="/" className="sidebarLogo sidebarLogoFull" onClick={closeMobile}><Image src="/perfectone-mark.png" width={1922} height={818} alt="PerfectOne ERP" className="sidebarLogoImg" priority/></Link>
        <button className="mobileClose" onClick={closeMobile} aria-label="Close navigation"><Icon name="close" /></button>
      </header>

      <label className="menuSearch"><Icon name="search" /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Find a menu…" aria-label="Find a menu" />{query && <button onClick={() => setQuery("")} type="button" aria-label="Clear menu search"><Icon name="close" /></button>}</label>

      <nav className="sidebarNav" aria-label="Main menu">
        {!ready && <div className="sidebarLoading">Loading your workspace…</div>}
        {ready && groups.map((group) => <section className="navGroup" key={group.label} aria-label={group.label}>
          <span className="navGroupLabel">{group.label}</span>
          <div className="navGroupItems">{group.menus.map((menu) => {
            const selected = !!isMenuActive(menu);
            const expanded = !!query || open === menu.label;
            return <div className="menuBlock" key={menu.label}>
              {!!menu.subs?.length ? <button type="button" className={`menuRow menuParent ${selected ? "active" : ""}`} onClick={() => setOpen((current) => current === menu.label ? "" : menu.label)} aria-expanded={expanded}>
                <span className="menuIcon"><Icon name={menu.icon} /></span><span className="menuText">{menu.label}</span><span className="menuChevron"><Icon name="chevron" /></span>
              </button> : <Link className={`menuRow menuDirect ${selected ? "active" : ""}`} href={menu.href!} onClick={closeMobile} aria-current={selected ? "page" : undefined}>
                <span className="menuIcon"><Icon name={menu.icon} /></span><span className="menuText">{menu.label}</span>
              </Link>}
              {!!menu.subs?.length && expanded && <div className="subMenu">{menu.subs.map((sub) => <Link className={isSubActive(sub) ? "active" : ""} key={sub.label} href={sub.href} onClick={closeMobile}><span />{sub.label}</Link>)}</div>}
            </div>;
          })}</div>
        </section>)}
        {ready && groups.length === 0 && <div className="noMenuResults">No menu matches “{query}”.</div>}
      </nav>

      <footer className="sidebarFooter">
        <button className="signOut signOutWide" onClick={signOut}><Icon name="logout" /><span>Sign out</span></button>
        <div className="sidebarVersion"><b>PerfectOne ERP</b><span>Version 3.0.0</span></div>
        <a className="sidebarDeveloper" href="https://perfectsolutioninternational.com/" target="_blank" rel="noopener noreferrer"><small>DEVELOPED BY</small><Image src="/perfect-solutions-international.png" width={1287} height={397} alt="Perfect Solutions International" /></a>
      </footer>
    </aside>
  </>;
}

function Icon({ name }: { name: IconName }) {
  const paths: Record<IconName, React.ReactNode> = {
    home: <><path d="m3 11 9-8 9 8" /><path d="M5 10v10h14V10M9 20v-6h6v6" /></>,
    cart: <><path d="M3 4h2l2.3 10.2a2 2 0 0 0 2 1.6h7.9a2 2 0 0 0 2-1.6L21 7H6" /><circle cx="10" cy="20" r="1" /><circle cx="18" cy="20" r="1" /></>,
    box: <><path d="m4 7 8-4 8 4-8 4-8-4Z" /><path d="M4 7v10l8 4 8-4V7M12 11v10" /></>,
    truck: <><path d="M3 6h11v10H3zM14 10h4l3 3v3h-7z" /><circle cx="7" cy="18" r="2" /><circle cx="18" cy="18" r="2" /></>,
    users: <><circle cx="9" cy="8" r="3" /><path d="M3 20v-2a5 5 0 0 1 5-5h2a5 5 0 0 1 5 5v2M16 4a3 3 0 0 1 0 6M18 13a5 5 0 0 1 3 5v2" /></>,
    supplier: <><path d="M4 20V8l8-5 8 5v12M8 20v-7h8v7M3 20h18" /><path d="M9 9h6" /></>,
    chart: <><path d="M4 20V10M10 20V4M16 20v-7M22 20H2" /></>,
    wallet: <><path d="M4 6h14a2 2 0 0 1 2 2v11H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h12" /><path d="M20 11h-5a2 2 0 0 0 0 4h5" /></>,
    file: <><path d="M6 3h9l4 4v14H6zM15 3v5h4M9 13h7M9 17h7" /></>,
    shield: <><path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6l-8-3Z" /><path d="m9 12 2 2 4-4" /></>,
    user: <><circle cx="12" cy="8" r="4" /><path d="M4 21a8 8 0 0 1 16 0" /></>,
    search: <><circle cx="11" cy="11" r="7" /><path d="m20 20-4-4" /></>,
    store: <><path d="M4 10v10h16V10M3 4h18l-1 6H4L3 4Z" /><path d="M8 20v-6h8v6" /></>,
    logout: <><path d="M10 17l5-5-5-5M15 12H3M14 4h5a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-5" /></>,
    plus: <path d="M12 5v14M5 12h14" />,
    chevron: <path d="m9 7 5 5-5 5" />,
    close: <path d="m6 6 12 12M18 6 6 18" />,
    menu: <path d="M4 7h16M4 12h16M4 17h16" />,
  };
  return <svg viewBox="0 0 24 24" aria-hidden="true">{paths[name]}</svg>;
}
