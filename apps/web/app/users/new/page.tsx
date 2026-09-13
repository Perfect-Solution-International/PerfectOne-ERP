"use client";
import Link from "next/link";
import { useState } from "react";
import { useRouter } from "next/navigation";
import Sidebar from "../../components/Sidebar";
import { apiFetch } from "../../lib/api";
import { useRequireSession } from "../../lib/useRequireSession";

const roles = [
  ["cashier", "Cashier"],
  ["stock_manager", "Stock manager"],
  ["accountant", "Accountant"],
  ["manager", "Manager"],
  ["super_admin", "Super admin"],
];

export default function NewUser() {
  useRequireSession();
  const router = useRouter();
  const [form, setForm] = useState({ name: "", email: "", username: "", password: "", role: "cashier" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const set = (k: string, v: string) => setForm((x) => ({ ...x, [k]: v }));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true); setError("");
    try {
      await apiFetch("/users", { method: "POST", body: JSON.stringify({ Name: form.name, Email: form.email, Username: form.username, Password: form.password, Role: form.role }) });
      router.push("/users");
    } catch (err: any) {
      setError(err.message.includes("duplicate") ? "That email or username is already in use." : err.message);
    } finally { setBusy(false); }
  };

  return <main className="shell"><Sidebar active="Users" /><section className="app">
    <header className="topbar"><div><span className="crumb">Administration / Users</span><b>Add user</b></div><Link className="ghostButton" href="/users">Cancel</Link></header>
    <div className="page formPage">
      <div className="formIntro"><span className="pageIcon">U</span><p>NEW TEAM MEMBER</p><h1>Create a user account</h1><small>They sign in with the username or email below. You can fine-tune permissions after the account is created.</small></div>
      <form className="saasForm" onSubmit={submit}>
        {error && <button type="button" className="formError" onClick={() => setError("")}>{error}</button>}
        <div className="formSection">
          <div><b>Account details</b><small>Shown across activity logs and receipts.</small></div>
          <label>Full name<input autoFocus value={form.name} onChange={(e) => set("name", e.target.value)} placeholder="e.g. Kasun Perera" required /></label>
          <label>Email<input type="email" value={form.email} onChange={(e) => set("email", e.target.value)} placeholder="name@store.lk" required /></label>
          <label>Username<input value={form.username} onChange={(e) => set("username", e.target.value)} placeholder="Used for quick POS sign-in" required /></label>
          <label>Temporary password<input type="text" value={form.password} onChange={(e) => set("password", e.target.value)} placeholder="At least 8 characters" minLength={8} required /></label>
        </div>
        <div className="formSection">
          <div><b>Role</b><small>Sets the starting permissions for this account.</small></div>
          <label>Assigned role<select value={form.role} onChange={(e) => set("role", e.target.value)}>{roles.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select></label>
        </div>
        <footer><Link href="/users">Cancel</Link><button className="new" disabled={busy}>{busy ? "Creating…" : "Create user"}</button></footer>
      </form>
    </div>
  </section></main>;
}
