"use client";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import Sidebar from "../components/Sidebar";
import ActionDialog, { type ActionDialogRequest } from "../components/ActionDialog";
import { apiFetch, can } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";

type User = {
  ID: string; Name: string; Email: string; Username: string; Role: string;
  isActive: boolean; isDeleted: boolean; permissions?: string[]; lastLoginAt?: string;
};

const permissionOptions: [string, string][] = [
  ["dashboard", "Dashboard"], ["pos", "POS"], ["sales", "Sales"], ["sales_returns", "Sales returns"],
  ["own_sales", "Own sales"], ["cashier_session", "Cashier session"], ["products", "Products"],
  ["purchases", "Purchases"], ["inventory", "Inventory"], ["expiry", "Expiry"], ["damage", "Damage"],
  ["reports", "Reports"], ["contacts", "Customers / suppliers"], ["accounting", "Accounting"],
  ["cash_bank", "Cash / bank"], ["financial_reports", "Financial reports"],
];
const roleLabel = (r: string) => r.replaceAll("_", " ").replace(/\b\w/g, (c) => c.toUpperCase());

export default function UsersPage() {
  useRequireSession();
  const [rows, setRows] = useState<User[]>([]);
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(true);
  const [message, setMessage] = useState("");
  const [roles, setRoles] = useState<{ ID: string; Name: string; Key: string }[]>([]);

  const load = () => { setBusy(true); apiFetch("/users").then(setRows).catch((e) => setMessage(e.message)).finally(() => setBusy(false)); };
  useEffect(() => { load(); apiFetch("/roles").then(setRoles).catch(() => undefined); }, []);

  const filtered = useMemo(() => rows.filter((u) => `${u.Name} ${u.Email} ${u.Username} ${u.Role}`.toLowerCase().includes(query.toLowerCase())), [rows, query]);
  const activeCount = rows.filter((u) => u.isActive && !u.isDeleted).length;

  return <main className="shell"><Sidebar active="Users" /><section className="app">
    <header className="topbar"><div><span className="crumb">Administration / Access control</span><b>Users</b></div>
      {can("users.add") && <Link className="new" href="/users/new">＋ Add user</Link>}
    </header>
    <div className="page productPage">
      <div className="pageHeading">
        <div><p>ACCESS CONTROL</p><h1>Team &amp; permissions</h1><small>Create accounts, assign roles and control exactly what each person can do.</small></div>
        <div className="catalogStats"><span><b>{activeCount}</b> Active</span><span><b>{rows.length}</b> Total</span></div>
      </div>
      <div className="productToolbar">
        <label className="catalogSearch"><span>⌕</span><input placeholder="Search name, email, username or role" value={query} onChange={(e) => setQuery(e.target.value)} /></label>
      </div>
      {message && <button className="catalogMessage" onClick={() => setMessage("")}>{message}<span>×</span></button>}
      <section className="productTableCard">
        {busy ? <div className="dashboardEmpty">Loading users…</div>
          : filtered.length === 0 ? <div className="dashboardEmpty"><span>◇</span><p>No matching users.</p></div>
          : <div className="productTableWrap"><table className="productTable"><thead><tr>
              <th>User</th><th>Role</th><th>Status</th><th>Last login</th><th>Actions</th>
            </tr></thead><tbody>
              {filtered.map((u) => <UserRow key={u.ID} user={u} roles={roles} reload={load} setMessage={setMessage} />)}
            </tbody></table></div>}
      </section>
    </div>
  </section></main>;
}

function UserRow({ user, roles, reload, setMessage }: { user: User; roles: { ID: string; Name: string; Key: string }[]; reload: () => void; setMessage: (m: string) => void }) {
  const [showPerms, setShowPerms] = useState(false);
  const [selected, setSelected] = useState<string[]>(user.permissions || []);
  const [saving, setSaving] = useState(false);
  const [history, setHistory] = useState<any[] | null>(null);
  const [historyTitle, setHistoryTitle] = useState("");
  const [editor, setEditor] = useState<"profile" | "password" | null>(null);
  const [form, setForm] = useState({ name: user.Name, email: user.Email, username: user.Username, role: user.Role, password: "", confirmPassword: "" });
  const [formError, setFormError] = useState("");
  const [dialog, setDialog] = useState<ActionDialogRequest | null>(null);
  useEffect(() => setSelected(user.permissions || []), [user.permissions]);

  const act = async (fn: () => Promise<any>, ok?: string) => { try { await fn(); if (ok) setMessage(ok); reload(); } catch (e: any) { setMessage(e.message); } };
  const flip = (p: string) => setSelected((x) => x.includes(p) ? x.filter((v) => v !== p) : [...x, p]);
  const savePerms = async () => { setSaving(true); try { await apiFetch(`/users/${user.ID}/permissions`, { method: "PUT", body: JSON.stringify({ permissions: selected }) }); setShowPerms(false); reload(); } catch (e: any) { setMessage(e.message); } finally { setSaving(false); } };
  const openEditor = (kind: "profile" | "password") => {
    setForm({ name: user.Name, email: user.Email, username: user.Username, role: user.Role, password: "", confirmPassword: "" });
    setFormError(""); setEditor(kind);
  };
  const submitEditor = async (event: React.FormEvent) => {
    event.preventDefault(); setFormError(""); setSaving(true);
    try {
      if (editor === "profile") {
        if (!form.name.trim() || !form.username.trim() || !form.role) throw new Error("Name, username and role are required.");
        if (form.email && !/^\S+@\S+\.\S+$/.test(form.email)) throw new Error("Enter a valid email address.");
        await apiFetch(`/users/${user.ID}`, { method: "PUT", body: JSON.stringify({ Name: form.name.trim(), Email: form.email.trim(), Username: form.username.trim(), Role: form.role, isActive: user.isActive }) });
        setMessage("User updated.");
      } else {
        if (form.password.length < 8) throw new Error("Password must contain at least 8 characters.");
        if (form.password !== form.confirmPassword) throw new Error("Passwords do not match.");
        await apiFetch(`/users/${user.ID}/reset-password`, { method: "POST", body: JSON.stringify({ password: form.password }) });
        setMessage("Password reset.");
      }
      setEditor(null); reload();
    } catch (e: any) { setFormError(e.message); } finally { setSaving(false); }
  };
  const toggleActive = () => act(() => apiFetch(`/users/${user.ID}`, { method: "PUT", body: JSON.stringify({ Name: user.Name, Role: user.Role, isActive: !user.isActive }) }));
  const remove = () => setDialog({ title: "Archive user account", message: `${user.Name} will lose system access. Historical transactions and activity remain unchanged.`, confirmLabel: "Archive user", tone: "danger", exactValue: user.Username, reasonRequired: true, reasonLabel: `Type ${user.Username} to confirm`, onConfirm: async () => { await apiFetch(`/users/${user.ID}`, { method: "DELETE" }); setMessage("User archived."); reload(); } });
  const restore = () => act(() => apiFetch(`/users/${user.ID}/restore`, { method: "POST" }), "User restored.");
  const showHistory = async (kind: "login-history" | "activity") => {
    setHistoryTitle(kind === "login-history" ? "Login history" : "Activity history");
    try { setHistory(await apiFetch(`/users/${user.ID}/${kind}`)); } catch (e: any) { setMessage(e.message); }
  };

  return <>
    <tr>
      <td><div className="productIdentity"><i>{user.Name?.[0]?.toUpperCase() || "U"}</i><span><b>{user.Name}</b><small>{user.Email} · {user.Username}</small></span></div></td>
      <td><b>{roleLabel(user.Role)}</b></td>
      <td><button className={`status ${user.isDeleted ? "inactive" : user.isActive ? "" : "low"}`} onClick={toggleActive} disabled={user.isDeleted}>{user.isDeleted ? "Deleted" : user.isActive ? "Active" : "Inactive"}</button></td>
      <td><small>{user.lastLoginAt ? new Date(user.lastLoginAt).toLocaleString() : "Never"}</small></td>
      <td><div className="productActions">
        <button onClick={() => openEditor("profile")} disabled={user.isDeleted}>Edit</button>
        <button onClick={() => openEditor("password")} disabled={user.isDeleted}>Reset password</button>
        {user.isDeleted ? <button className="restoreLink" onClick={restore}>Restore</button> : <button className="dangerLink" onClick={remove}>Delete</button>}
        <button onClick={() => showHistory("login-history")}>Logins</button>
        <button onClick={() => showHistory("activity")}>Activity</button>
        {user.Role !== "super_admin" && <button onClick={() => setShowPerms((s) => !s)}>Permissions</button>}
      </div></td>
    </tr>
    {showPerms && <tr><td colSpan={5}>
      <div className="permissionPanel" style={{ position: "static", width: "auto" }}>
        {permissionOptions.map(([key, label]) => <label key={key}><input type="checkbox" checked={selected.includes(key)} onChange={() => flip(key)} />{label}</label>)}
        <button className="new" disabled={saving} onClick={savePerms}>{saving ? "Saving…" : "Save access"}</button>
      </div>
    </td></tr>}
    {history && <><button className="historyBackdrop" aria-label="Close" onClick={() => setHistory(null)} />
      <aside className="priceHistoryPanel"><header><div><small>{user.Name.toUpperCase()}</small><h2>{historyTitle}</h2></div><button onClick={() => setHistory(null)}>×</button></header>
        <p>Last login: {user.lastLoginAt ? new Date(user.lastLoginAt).toLocaleString() : "Never"}</p>
        {history.length ? <div className="priceTimeline">{history.map((x: any, i: number) => <article key={x.id || i}><i>{(x.action || "L")[0].toUpperCase()}</i><div><b>{x.action || `${x.success ? "Successful" : "Failed"} login`}</b><small>{new Date(x.createdAt).toLocaleString()} · {x.ipAddress || x.entityType || ""}</small></div></article>)}</div> : <div className="dashboardEmpty">No history recorded.</div>}
      </aside></>}
    {editor && <div className="actionDialogBackdrop" role="presentation"><form className="actionDialog userEditorDialog" role="dialog" aria-modal="true" aria-labelledby="user-editor-title" onSubmit={submitEditor}>
      <header><div><small>USER MANAGEMENT</small><h2 id="user-editor-title">{editor === "profile" ? "Edit user account" : "Reset password"}</h2></div><button type="button" onClick={() => setEditor(null)} disabled={saving} aria-label="Close">×</button></header>
      {editor === "profile" ? <div className="dialogFieldGrid">
        <label>Full name<input autoFocus value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} required /></label>
        <label>Email<input type="email" value={form.email} onChange={e => setForm({ ...form, email: e.target.value })} /></label>
        <label>Username<input value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} required /></label>
        <label>Role<select value={form.role} onChange={e => setForm({ ...form, role: e.target.value })} required>{roles.map(role => <option key={role.ID} value={role.Key}>{role.Name}</option>)}</select></label>
      </div> : <div className="dialogFieldGrid">
        <p className="dialogHelp">Create a new password for {user.Name}. It must contain at least 8 characters.</p>
        <label>New password<input autoFocus type="password" minLength={8} autoComplete="new-password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} required /></label>
        <label>Confirm password<input type="password" minLength={8} autoComplete="new-password" value={form.confirmPassword} onChange={e => setForm({ ...form, confirmPassword: e.target.value })} required /></label>
      </div>}
      {formError && <div className="formError" role="alert">{formError}</div>}
      <footer><button type="button" onClick={() => setEditor(null)} disabled={saving}>Cancel</button><button className="new" disabled={saving}>{saving ? "Saving…" : editor === "profile" ? "Save changes" : "Reset password"}</button></footer>
    </form></div>}
    <ActionDialog request={dialog} close={() => setDialog(null)} />
  </>;
}
