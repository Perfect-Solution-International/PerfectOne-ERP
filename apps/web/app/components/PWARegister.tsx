"use client";
import { useEffect } from "react";
import { retryAllOfflineTransactions } from "../lib/api";

export default function PWARegister() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production" || !("serviceWorker" in navigator)) return;
    const register = () => navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(() => undefined);
    const reconnect = () => { void retryAllOfflineTransactions(); };
    const visible = () => { if (document.visibilityState === "visible" && navigator.onLine) reconnect(); };
    const timer = window.setInterval(() => { if (navigator.onLine) reconnect(); }, 30000);
    window.addEventListener("load", register); window.addEventListener("online", reconnect); document.addEventListener("visibilitychange", visible);
    if (document.readyState === "complete") void register();
    return () => { window.clearInterval(timer); window.removeEventListener("load", register); window.removeEventListener("online", reconnect); document.removeEventListener("visibilitychange", visible); };
  }, []);
  return null;
}
