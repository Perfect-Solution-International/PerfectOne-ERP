"use client";

import { useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Dashboard from "./components/Dashboard";
import Sidebar from "./components/Sidebar";
import { useRequireSession } from "./lib/useRequireSession";

const legacyRoutes: Record<string, string> = {
  "Sales history": "/sales",
  Products: "/products",
  "Low-stock alerts": "/inventory?section=low-stock",
  "Expiry management": "/expiry",
  "Damaged items": "/inventory?section=damage",
  "Purchase history": "/purchases",
  "Customer list": "/customers",
  "Supplier list": "/suppliers",
  "New purchase": "/purchases",
  "Customer payments": "/finance?section=customer-receipts",
  "Supplier ledger": "/suppliers",
  Users: "/users",
};

export default function HomePage() {
  useRequireSession();
  const router = useRouter();
  const searchParams = useSearchParams();
  const legacyTab = searchParams.get("tab");
  const destination = legacyTab ? legacyRoutes[legacyTab] : undefined;

  useEffect(() => { if (destination) router.replace(destination); }, [destination, router]);

  if (destination) return <main className="shell"><Sidebar active="Overview"/><section className="app"><div className="page"><div className="dashboardEmpty">Opening {legacyTab}…</div></div></section></main>;
  return <main className="shell"><Sidebar active="Overview"/><section className="app"><Dashboard/></section></main>;
}
