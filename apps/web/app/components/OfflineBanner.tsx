"use client";
import { useOffline } from "next/offline";
import { offlineTransactions } from "../lib/api";

export default function OfflineBanner() {
  const offline = useOffline();
  if (!offline) return null;
  const pending = offlineTransactions().length;
  return <div className="globalOfflineBanner" role="status"><b>Offline mode</b><span>{pending ? `${pending} transaction${pending === 1 ? "" : "s"} waiting to synchronize.` : "Live data is unavailable. POS uses the last secure device snapshot."}</span></div>;
}
