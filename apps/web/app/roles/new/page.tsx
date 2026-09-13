"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import Sidebar from "../../components/Sidebar";
import { apiFetch } from "../../lib/api";
import { useRequireSession } from "../../lib/useRequireSession";

export default function NewRole() {
  useRequireSession();
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); setError(""); setBusy(true);
    try {
      await apiFetch("/roles", { method: "POST", body: JSON.stringify({ name: name.trim(), description: description.trim() }) });
      router.push("/roles");
    } catch (issue: any) { setError(issue.message || "Role could not be created."); } finally { setBusy(false); }
  };
  return <main className="shell"><Sidebar active="Roles & Permissions"/><section className="app">
    <header className="topbar"><div><span className="crumb">Access control / Roles</span><b>Create role</b></div><Link className="ghostButton" href="/roles">Cancel</Link></header>
    <div className="page formPage"><div className="formIntro"><span className="pageIcon">R</span><p>NEW ACCESS PROFILE</p><h1>Create a role</h1><small>Start with a clear role name. Configure its permissions on the next screen.</small></div>
      <form className="saasForm" onSubmit={submit}><div className="formSection"><div><b>Role details</b><small>Used across user accounts and activity records.</small></div>
        <label>Role name<input autoFocus value={name} onChange={e=>setName(e.target.value)} placeholder="e.g. Branch Supervisor" required/></label>
        <label>Description<textarea value={description} onChange={e=>setDescription(e.target.value)} placeholder="What is this role responsible for?" rows={4}/></label>
      </div>{error&&<div className="formError" role="alert">{error}</div>}<footer><Link href="/roles">Cancel</Link><button className="new" disabled={busy}>{busy?"Creating…":"Create role & continue"}</button></footer></form>
    </div>
  </section></main>;
}
