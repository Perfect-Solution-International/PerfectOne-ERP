"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import ActionDialog, { type ActionDialogRequest } from "../components/ActionDialog";
import Sidebar from "../components/Sidebar";
import { apiFetch } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";

const modules = ["pos", "sales", "purchases", "products", "inventory", "expiry", "customers", "suppliers", "reports", "accounting", "cash_bank", "users", "roles"];
const actions = ["view", "add", "edit", "delete", "approve", "cancel", "print", "export"];
const label = (value: string) => value.replaceAll("_", " ").replace(/\b\w/g, character => character.toUpperCase());

export default function RolesPage() {
  useRequireSession();
  const [roles, setRoles] = useState<any[]>([]);
  const [selected, setSelected] = useState<any>(null);
  const [permissions, setPermissions] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ tone: "success" | "error"; text: string } | null>(null);
  const [dialog, setDialog] = useState<ActionDialogRequest | null>(null);
  const load = () => apiFetch("/roles").then((data: any[]) => { setRoles(data); setSelected((current: any) => data.find(role => role.ID === current?.ID) || data[0] || null); });
  useEffect(() => { void load().catch((issue: any) => setNotice({ tone: "error", text: issue.message })); }, []);
  useEffect(() => setPermissions(selected?.permissions || []), [selected]);
  const flip = (permission: string) => setPermissions(current => current.includes(permission) ? current.filter(value => value !== permission) : [...current, permission]);
  const save = async () => {
    if (!selected) return; setBusy(true); setNotice(null);
    try { await apiFetch(`/roles/${selected.ID}/permissions`, { method: "PUT", body: JSON.stringify({ permissions }) }); await load(); setNotice({ tone: "success", text: `${selected.Name} permissions saved.` }); }
    catch (issue: any) { setNotice({ tone: "error", text: issue.message }); } finally { setBusy(false); }
  };
  const requestDelete = () => selected && setDialog({ title: "Delete custom role", message: `Delete ${selected.Name}? Roles assigned to users cannot be deleted.`, confirmLabel: "Delete role", tone: "danger", exactValue: selected.Name, reasonRequired: true, reasonLabel: `Type ${selected.Name} to confirm`, onConfirm: async () => { await apiFetch(`/roles/${selected.ID}`, { method: "DELETE" }); setSelected(null); await load(); setNotice({ tone: "success", text: "Role deleted." }); } });
  return <main className="shell"><Sidebar active="Roles & Permissions"/><section className="app">
    <header className="topbar"><div><span className="crumb">Workspace / Access control</span><b>Roles & permissions</b></div><Link className="new" href="/roles/new">+ Create role</Link></header>
    <div className="page rolesPage">
      {notice && <button className={`catalogMessage ${notice.tone}`} onClick={() => setNotice(null)}>{notice.text}<span>×</span></button>}
      <aside className="roleList"><div className="sectionTitle"><div><p>TEAM ROLES</p><h2>Access profiles</h2></div><span>{roles.length}</span></div>
        {roles.map(role => <button key={role.ID} className={selected?.ID === role.ID ? "active" : ""} onClick={() => setSelected(role)}><span className="roleAvatar">{role.Name[0]}</span><span><b>{role.Name}</b><small>{role.userCount} users · {role.isSystem ? "System" : "Custom"}</small></span></button>)}
      </aside>
      <section className="roleMatrix">{selected ? <><div className="roleMatrixHead"><div><p>PERMISSION MATRIX</p><h1>{selected.Name}</h1><small>{selected.Description || "Choose exactly what this role can do in each module."}</small></div>{!selected.isSystem && <button className="dangerButton" onClick={requestDelete}>Delete role</button>}</div>
        {selected.Key === "super_admin" ? <div className="fullAccessNotice"><b>Protected full access</b><span>Super Admin always has every permission and cannot be restricted.</span></div> : <><div className="matrixLegend"><span>Changes apply to every user assigned to this role.</span><b>{permissions.filter(permission => permission.includes(".")).length} actions enabled</b></div>
          <div className="productTableWrap"><table><thead><tr><th>Module</th>{actions.map(action => <th key={action}>{label(action)}</th>)}</tr></thead><tbody>{modules.map(module => <tr key={module}><td><b>{label(module)}</b></td>{actions.map(action => { const key = `${module}.${action}`; return <td key={key}><input aria-label={`${label(module)} ${label(action)}`} type="checkbox" checked={permissions.includes(key)} onChange={() => flip(key)}/></td>; })}</tr>)}</tbody></table></div>
          <div className="stickySave"><span>Review the matrix before saving.</span><button className="new" disabled={busy} onClick={save}>{busy ? "Saving…" : "Save permissions"}</button></div></>}
      </> : <div className="emptyState">Select a role to manage permissions.</div>}</section>
    </div><ActionDialog request={dialog} close={() => setDialog(null)}/>
  </section></main>;
}
