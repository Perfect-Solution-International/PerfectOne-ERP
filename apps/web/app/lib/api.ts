export type Session = { Token: string; Name: string; Role: string; UserID: string; TenantID: string; BranchID: string; Permissions: string[] };
export type OfflineTransaction = { id: string; path: string; method: string; body: string; createdAt: string; userId: string; tenantId: string; branchId: string; terminalId: string; status: "pending" | "syncing" | "failed" | "conflict"; attempts: number; lastError?: string; nextRetryAt?: string };
export type SyncTransaction = { id:string;requestId:string;user:string;branch:string;terminalId:string;localTimestamp?:string;status:"syncing"|"synced"|"failed"|"conflict"|"discarded";attempts:number;lastError:string;responseCode:number;invoice:string;firstSeenAt:string;lastAttemptAt:string;syncedAt?:string;resolutionReason?:string;resolvedAt?:string };
export type POSCache = { tenantId: string; branchId: string; userId: string; products: any[]; customers: any[]; register: any | null; cachedAt: string };

const QUEUE = "grocerly_offline_transactions";
const TERMINAL = "grocerly_terminal_id";
const POS_CACHE = "grocerly_pos_cache";
const SYNC_LOCK = "perfectone_sync_lock";

export function can(permission: string) { const session = getSession(); return !!session && (session.Role === "super_admin" || session.Permissions?.includes(permission)); }
export function getSession(): Session | null { if (typeof window === "undefined") return null; const raw = localStorage.getItem("grocerly_session"); if (!raw) return null; try { return JSON.parse(raw); } catch { return null; } }
export function clearSession() { if (typeof window !== "undefined") { localStorage.removeItem("grocerly_session"); localStorage.removeItem(POS_CACHE); } }
export function offlineTransactions(): OfflineTransaction[] { if (typeof window === "undefined") return []; try { return JSON.parse(localStorage.getItem(QUEUE) || "[]"); } catch { return []; } }
export function currentOfflineTransactions(): OfflineTransaction[]{const session=getSession();return offlineTransactions().filter(x=>x.tenantId===session?.TenantID&&x.branchId===session?.BranchID&&x.userId===session?.UserID)}
export function reconcileOfflineQueue(server:SyncTransaction[]){const synced=new Set(server.filter(x=>x.status==="synced").map(x=>x.requestId));if(synced.size)saveQueue(offlineTransactions().filter(x=>!synced.has(x.id)))}
function queueChanged() { if (typeof window !== "undefined") window.dispatchEvent(new Event("grocerly-offline-queue")); }
function saveQueue(rows: OfflineTransaction[]) { localStorage.setItem(QUEUE, JSON.stringify(rows)); queueChanged(); }

export function cachedPOSData(): POSCache | null {
  if (typeof window === "undefined") return null;
  const session = getSession();
  try { const value: POSCache = JSON.parse(localStorage.getItem(POS_CACHE) || "null"); return value && session && value.tenantId === session.TenantID && value.branchId === session.BranchID && value.userId === session.UserID ? value : null; } catch { return null; }
}
export function cachePOSData(update: Partial<Pick<POSCache, "products" | "customers" | "register">>) {
  const session = getSession(); if (!session || typeof window === "undefined") return;
  const current = cachedPOSData();
  localStorage.setItem(POS_CACHE, JSON.stringify({ tenantId: session.TenantID, branchId: session.BranchID, userId: session.UserID, products: [], customers: [], register: null, ...current, ...update, cachedAt: new Date().toISOString() }));
}

function queueCheckout(path: string, options: RequestInit, reason: string) {
  const session = getSession(), body = String(options.body || ""), payload = JSON.parse(body), id = payload.requestId;
  if (!id) return "";
  const rows = offlineTransactions(); if (rows.some(row => row.id === id)) return id;
  let terminalId = localStorage.getItem(TERMINAL); if (!terminalId) { terminalId = crypto.randomUUID(); localStorage.setItem(TERMINAL, terminalId); }
  rows.push({ id, path, method: String(options.method || "POST"), body, createdAt: new Date().toISOString(), userId: session?.UserID || "", tenantId: session?.TenantID || "", branchId: session?.BranchID || "", terminalId, status: "pending", attempts: 0, lastError: reason });
  saveQueue(rows); return id;
}

function offlineQueuedError(path: string, options: RequestInit, reason: string) {
  const id = queueCheckout(path, options, reason);
  const error: any = new Error("Network unavailable. Sale saved safely to the offline sync queue.");
  error.offlineQueued = true; error.transactionId = id;
  return error;
}

export async function retryOfflineTransaction(id: string) {
  const rows = offlineTransactions(), item = rows.find(row => row.id === id); if (!item) return;
  const session = getSession(); if (!session || item.userId !== session.UserID || item.tenantId !== session.TenantID || item.branchId !== session.BranchID) throw new Error("Sign in as the original cashier at the original branch to synchronize this sale.");
  item.status="syncing";saveQueue(rows);
  try { await apiFetch(item.path, { method: item.method, body: item.body, headers: { "X-Offline-Retry": "true", "X-POS-Terminal": item.terminalId, "X-Local-Timestamp": item.createdAt } }); saveQueue(rows.filter(row => row.id !== id)); }
  catch (issue: any) { item.status = issue.status===409?"conflict":"failed"; item.attempts++; item.lastError = issue.message; item.nextRetryAt=issue.status===409?undefined:new Date(Date.now()+Math.min(300000,5000*2**Math.min(item.attempts,6))).toISOString(); saveQueue(rows); throw issue; }
}
function acquireSyncLock(){if(typeof window==="undefined")return "";const now=Date.now();try{const lock=JSON.parse(localStorage.getItem(SYNC_LOCK)||"null");if(lock?.until>now)return "";const owner=crypto.randomUUID();localStorage.setItem(SYNC_LOCK,JSON.stringify({owner,until:now+60000}));return JSON.parse(localStorage.getItem(SYNC_LOCK)||"null")?.owner===owner?owner:""}catch{return ""}}
export async function retryAllOfflineTransactions() { const owner=acquireSyncLock();if(!owner)return;try{for (const item of offlineTransactions()) { if (typeof navigator !== "undefined" && !navigator.onLine) break;if(item.status==="conflict"||item.nextRetryAt&&new Date(item.nextRetryAt).getTime()>Date.now())continue;try { await retryOfflineTransaction(item.id); } catch {} }}finally{try{if(JSON.parse(localStorage.getItem(SYNC_LOCK)||"null")?.owner===owner)localStorage.removeItem(SYNC_LOCK)}catch{}} }
export function discardOfflineTransaction(id: string) { saveQueue(offlineTransactions().filter(row => row.id !== id)); }

export async function apiFetch(path: string, options: RequestInit = {}): Promise<any> {
  const session = getSession();
  const headers: Record<string, string> = { "Content-Type": "application/json", ...((options.headers as Record<string, string>) || {}) };
  if (session?.Token) headers.Authorization = `Bearer ${session.Token}`;
  let response: Response;
  try { response = await fetch(`/api${path}`, { ...options, headers }); }
  catch (issue: any) {
    if (path === "/pos/checkout" && options.body && !headers["X-Offline-Retry"]) throw offlineQueuedError(path, options, issue.message || "Network unavailable");
    throw issue;
  }
  if (path === "/pos/checkout" && options.body && !headers["X-Offline-Retry"] && [502, 503, 504].includes(response.status)) throw offlineQueuedError(path, options, `Gateway unavailable (${response.status})`);
  if (response.status === 401) { clearSession(); if (typeof window !== "undefined") window.location.replace("/login"); throw new Error("Session expired"); }
  if (!response.ok) { const error:any=new Error((await response.text()).trim() || "Request failed");error.status=response.status;throw error; }
  if (response.status === 204) return {};
  const body = await response.text(); if (!body.trim()) return {};
  return (response.headers.get("content-type") || "").includes("application/json") ? JSON.parse(body) : body;
}
