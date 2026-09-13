"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { apiFetch } from "../lib/api";
import { count, metricValueClass, money, moneyCompact } from "../lib/format";

const periods = [
  { key: "today", label: "Today" }, { key: "yesterday", label: "Yesterday" },
  { key: "week", label: "This Week" }, { key: "month", label: "This Month" },
  { key: "year", label: "This Year" }, { key: "custom", label: "Custom" },
];
const definitions: Record<string, [string, IconName, "money" | "count", string]> = {
  todaySales: ["Today Sales", "trendUp", "money", "positive"], todayPurchases: ["Today Purchases", "cart", "money", "neutral"],
  monthlySales: ["Monthly Sales", "chart", "money", "positive"], monthlyPurchases: ["Monthly Purchases", "truck", "money", "neutral"],
  stockValue: ["Stock Value", "box", "money", "accent"], lowStock: ["Low Stock Items", "alertTriangle", "count", "warning"],
  outOfStock: ["Out of Stock", "xCircle", "count", "danger"], nearExpiry: ["Near Expiry", "clock", "count", "warning"],
  expired: ["Expired Items", "alertOctagon", "count", "danger"], damagedItems: ["Damaged Items", "brokenBox", "count", "danger"],
  customerReceivables: ["Customers Owe Us", "users", "money", "accent"], supplierPayables: ["We Owe Suppliers", "supplier", "money", "neutral"],
  cashBalance: ["Cash Balance", "banknote", "money", "positive"], bankBalance: ["Bank Balance", "bank", "money", "accent"],
  expenses: ["Expenses", "minusCircle", "money", "danger"], profit: ["Profit", "plusCircle", "money", "positive"],
  invoiceCount: ["Invoices", "chart", "count", "accent"], averageBasket: ["Average Basket", "cart", "money", "positive"],
};

export default function Dashboard() {
  const [period, setPeriod] = useState("today");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [data, setData] = useState<any>({ metrics: {}, cashierPerformance: [], bestSellingProducts: [] });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = async () => {
    setLoading(true); setError("");
    try { setData(await apiFetch(`/dashboard?period=${period}&from=${from}&to=${to}`)); }
    catch (exception: any) { setError(exception.message); }
    finally { setLoading(false); }
  };
  useEffect(() => { if (period !== "custom" || (from && to)) void load(); }, [period]);

  return <section className="dashboard">
    <div className="dashboardHero"><div><p>BUSINESS OVERVIEW</p><h1>Good day, here’s your store</h1><small>Live performance and operational health at a glance.</small></div><div className="periodPicker">{periods.map((item) => <button key={item.key} className={period === item.key ? "active" : ""} onClick={() => setPeriod(item.key)}>{item.label}</button>)}</div></div>
    {period === "custom" && <div className="customRange"><label>From<input type="date" value={from} onChange={(event) => setFrom(event.target.value)}/></label><label>To<input type="date" value={to} onChange={(event) => setTo(event.target.value)}/></label><button className="new" disabled={!from || !to} onClick={load}>Apply range</button></div>}
    {error && <div className="notice errorNotice">{error}</div>}
    <div className="dashboardMeta"><span>{data.from && `${data.from} — ${data.to}`}</span>{loading && <b>Refreshing…</b>}</div>
    <div className="dashboardMetrics">{Object.entries(data.metrics || {}).map(([key, value]) => { const item = definitions[key] || [key, "box" as IconName, "count", "neutral"]; const isMoney = item[2] === "money"; const exact = isMoney ? money(value) : count(value); const shown = isMoney ? moneyCompact(value) : count(value); return <article key={key} className={`metricCard ${item[3]}`}><span className="metricIcon"><Icon name={item[1]}/></span><div><small>{item[0]}</small><b className={metricValueClass(shown)} title={exact}>{shown}</b></div></article>; })}</div>
    <div className="dashboardPanels"><section className="insightCard"><header><div><p>TEAM PERFORMANCE</p><h2>Cashier performance</h2></div><span>{data.cashierPerformance?.length || 0} cashiers</span></header>{data.cashierPerformance?.length ? <div className="performanceList">{data.cashierPerformance.map((item: any, index: number) => <div key={item.name}><span className="rank">{index + 1}</span><div><b>{item.name}</b><small>{item.sales} sales · Avg. {money(item.average)}</small></div><strong>{money(item.total)}</strong></div>)}</div> : <Empty text="No cashier sales in this period."/>}</section>
      <section className="insightCard"><header><div><p>PRODUCT INSIGHTS</p><h2>Best selling products</h2></div><span>{data.bestSellingProducts?.length || 0} products</span></header>{data.bestSellingProducts?.length ? <div className="performanceList">{data.bestSellingProducts.map((item: any, index: number) => <div key={item.name}><span className="rank productRank">{index + 1}</span><div><b>{item.name}</b><small>{Number(item.quantity).toLocaleString()} units sold</small></div><strong>{money(item.total)}</strong></div>)}</div> : <Empty text="No product sales in this period."/>}</section>
      <section className="insightCard"><header><div><p>ACTION CENTRE</p><h2>Risks requiring attention</h2></div><span>{data.alerts?.length || 0} alerts</span></header>{data.alerts?.length ? <div className="performanceList">{data.alerts.map((item:any,index:number)=><Link href={item.href} key={item.title}><span className="rank">{index+1}</span><div><b>{item.title}</b><small>Open workspace to resolve</small></div><strong>{Number(item.value||0)}</strong></Link>)}</div>:<Empty text="No operational alerts."/>}</section>
      {data.insights?.procurement&&<section className="insightCard"><header><div><p>PROCUREMENT</p><h2>Purchase order pipeline</h2></div><span>live</span></header><div className="performanceList">{[["Pending orders",data.insights.procurement.pending],["Partially received",data.insights.procurement.partial],["Overdue deliveries",data.insights.procurement.overdue]].map((item:any,index)=><div key={item[0]}><span className="rank">{index+1}</span><div><b>{item[0]}</b><small>Current procurement workload</small></div><strong>{item[1]}</strong></div>)}</div></section>}
      <section className="insightCard"><header><div><p>BRANCH PERFORMANCE</p><h2>Sales by location</h2></div><span>{data.branchPerformance?.length||0} branches</span></header>{data.branchPerformance?.length?<div className="performanceList">{data.branchPerformance.map((item:any,index:number)=><div key={item.name}><span className="rank">{index+1}</span><div><b>{item.name}</b><small>{item.count} invoices</small></div><strong>{money(item.total)}</strong></div>)}</div>:<Empty text="No branch activity in this period."/>}</section>
      <section className="insightCard"><header><div><p>RECENT ACTIVITY</p><h2>Business audit feed</h2></div><span>{data.activity?.length||0} events</span></header>{data.activity?.length?<div className="performanceList">{data.activity.map((item:any,index:number)=><div key={`${item.at}-${index}`}><span className="rank">{index+1}</span><div><b>{item.action} · {item.module}</b><small>{item.user} · {new Date(item.at).toLocaleString()}</small></div></div>)}</div>:<Empty text="No recent audit activity."/>}</section>
    </div>
  </section>;
}

function Empty({ text }: { text: string }) { return <div className="dashboardEmpty"><span><Icon name="inbox"/></span><p>{text}</p></div>; }

type IconName = "trendUp" | "cart" | "chart" | "truck" | "box" | "alertTriangle" | "xCircle" | "clock" | "alertOctagon" | "brokenBox" | "users" | "supplier" | "banknote" | "bank" | "minusCircle" | "plusCircle" | "inbox";

function Icon({ name }: { name: IconName }) {
  const paths: Record<IconName, React.ReactNode> = {
    trendUp: <><path d="M3 17l6-6 4 4 8-8" /><path d="M15 7h6v6" /></>,
    cart: <><path d="M3 4h2l2.3 10.2a2 2 0 0 0 2 1.6h7.9a2 2 0 0 0 2-1.6L21 7H6" /><circle cx="10" cy="20" r="1" /><circle cx="18" cy="20" r="1" /></>,
    chart: <><path d="M4 20V10M10 20V4M16 20v-7M22 20H2" /></>,
    truck: <><path d="M3 6h11v10H3zM14 10h4l3 3v3h-7z" /><circle cx="7" cy="18" r="2" /><circle cx="18" cy="18" r="2" /></>,
    box: <><path d="m4 7 8-4 8 4-8 4-8-4Z" /><path d="M4 7v10l8 4 8-4V7M12 11v10" /></>,
    alertTriangle: <><path d="M12 3 2 21h20L12 3Z" /><path d="M12 10v5M12 18h.01" /></>,
    xCircle: <><circle cx="12" cy="12" r="9" /><path d="m9 9 6 6M15 9l-6 6" /></>,
    clock: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 3" /></>,
    alertOctagon: <><path d="M8 3h8l5 5v8l-5 5H8l-5-5V8l5-5Z" /><path d="M12 8v5M12 16h.01" /></>,
    brokenBox: <><path d="m4 7 8-4 8 4-8 4-8-4Z" /><path d="M4 7v10l8 4 8-4V7" /><path d="m9 12 2 3 2-4 2 2" /></>,
    users: <><circle cx="9" cy="8" r="3" /><path d="M3 20v-2a5 5 0 0 1 5-5h2a5 5 0 0 1 5 5v2M16 4a3 3 0 0 1 0 6M18 13a5 5 0 0 1 3 5v2" /></>,
    supplier: <><path d="M4 20V8l8-5 8 5v12M8 20v-7h8v7M3 20h18" /><path d="M9 9h6" /></>,
    banknote: <><rect x="2" y="6" width="20" height="12" rx="2" /><circle cx="12" cy="12" r="3" /><path d="M6 9h.01M18 15h.01" /></>,
    bank: <><path d="M3 10 12 4l9 6M4 10v10M20 10v10M8 10v10M16 10v10M2 21h20" /></>,
    minusCircle: <><circle cx="12" cy="12" r="9" /><path d="M8 12h8" /></>,
    plusCircle: <><circle cx="12" cy="12" r="9" /><path d="M12 8v8M8 12h8" /></>,
    inbox: <><path d="M4 12h4l2 3h4l2-3h4" /><path d="M4 12 5.5 5h13L20 12v7a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-7Z" /></>,
  };
  return <svg viewBox="0 0 24 24" aria-hidden="true">{paths[name]}</svg>;
}
