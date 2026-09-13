"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import Sidebar from "../components/Sidebar";
import ActionDialog, { type ActionDialogRequest } from "../components/ActionDialog";
import { apiFetch } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";
import { money, moneyFigure } from "../lib/format";

const tabs = ["Cashbook", "Customer receipts", "Supplier payments", "Expenses", "Account transfers", "Accounts"] as const;
type Tab = typeof tabs[number];


export default function FinancePage() {
  useRequireSession();
  const searchParams = useSearchParams();
  const [tab, setTab] = useState<Tab>("Cashbook");
  const [summary, setSummary] = useState<any>({});
  const [rows, setRows] = useState<any[]>([]);
  const [accounts, setAccounts] = useState<any[]>([]);
  const [customers, setCustomers] = useState<any[]>([]);
  const [suppliers, setSuppliers] = useState<any[]>([]);
  const [categories, setCategories] = useState<any[]>([]);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [dialog, setDialog] = useState<ActionDialogRequest | null>(null);
  const [accountEditor, setAccountEditor] = useState<any>(null);

  const loadBase = async () => {
    const [nextSummary, nextAccounts, nextCustomers, nextSuppliers, nextCategories] = await Promise.all([
      apiFetch("/finance/summary"), apiFetch("/cash-accounts"), apiFetch("/customers"), apiFetch("/suppliers"), apiFetch("/expense-categories"),
    ]);
    setSummary(nextSummary); setAccounts(nextAccounts); setCustomers(nextCustomers); setSuppliers(nextSuppliers); setCategories(nextCategories);
  };
  const loadRows = async () => {
    const path = tab === "Cashbook" ? "/finance/transactions" : tab === "Customer receipts" ? "/finance/payments/customers" : tab === "Supplier payments" ? "/finance/payments/suppliers" : tab === "Expenses" ? "/expenses" : tab === "Accounts" ? "/cash-accounts?includeInactive=true" : "/finance/transactions?type=transfer_out&referenceType=cash_transfer";
    setRows(await apiFetch(path));
  };
  const refresh = async () => { try { await Promise.all([loadBase(), loadRows()]); setError(""); } catch (issue: any) { setError(issue.message); } };
  useEffect(() => { void loadBase().catch((issue: any) => setError(issue.message)); }, []);
  useEffect(() => { const requested = searchParams.get("section"); const match: Record<string,Tab> = { "customer-receipts": "Customer receipts", "supplier-payments": "Supplier payments", expenses: "Expenses", transfers: "Account transfers", accounts: "Accounts" }; if (requested && match[requested]) setTab(match[requested]); }, [searchParams]);
  useEffect(() => { void loadRows().catch((issue: any) => setError(issue.message)); }, [tab]);

  const submit = async (event: React.FormEvent<HTMLFormElement>, path: string, body: (data: FormData) => any) => {
    event.preventDefault(); setBusy(true); setMessage(""); setError("");
    try { const form = event.currentTarget; await apiFetch(path, { method: "POST", body: JSON.stringify(body(new FormData(form))) }); form.reset(); setMessage("Saved successfully. The balances and history are now updated."); await refresh(); }
    catch (issue: any) { setError(issue.message); } finally { setBusy(false); }
  };
  const reverse = async (kind: "customer" | "supplier", id: string) => setDialog({ title: `Reverse ${kind} payment`, message: "This posts an opposite cashbook and accounting entry. The original payment remains in history.", confirmLabel: "Reverse payment", tone: "danger", reasonRequired: true, onConfirm: async (reason) => { await apiFetch(`/${kind}-payments/${id}/reverse`, { method: "POST", body: JSON.stringify({ reason, requestId: crypto.randomUUID() }) }); setMessage("Payment reversed with an audit trail."); await refresh(); } });
  const reverseTransfer = async (id: string) => setDialog({ title: "Reverse account transfer", message: "Both sides of this transfer and the General Ledger will be reversed together.", confirmLabel: "Reverse transfer", tone: "danger", reasonRequired: true, onConfirm: async (reason) => { await apiFetch(`/finance/transfers/${id}/reverse`, { method: "POST", body: JSON.stringify({ reason }) }); setMessage("Transfer reversed. Both accounts and the General Ledger are updated."); await refresh(); } });
  const expenseAction = async (id: string, action: string) => setDialog({ title: `${action[0].toUpperCase()}${action.slice(1)} expense`, message: action === "finalize" ? "Finalizing posts this expense to the cashbook and General Ledger." : "The original expense remains available in the audit history.", confirmLabel: action, tone: action === "finalize" ? "default" : "danger", reasonRequired: action !== "finalize", onConfirm: async (reason) => { await apiFetch(`/expenses/${id}/${action}`, { method: "POST", body: JSON.stringify({ reason }) }); setMessage(`Expense ${action} completed.`); await refresh(); } });
  const chequeAction = (kind:"customer"|"supplier", row:any, action:"clear"|"bounce") => setDialog({ title: `${action === "clear" ? "Clear" : "Bounce"} cheque ${row.chequeNumber}`, message: action === "clear" ? "Confirm that the bank has cleared this cheque." : "Bouncing reverses the payment, party balance, cashbook and accounting entries.", confirmLabel: action === "clear" ? "Mark cleared" : "Bounce & reverse", tone: action === "bounce" ? "danger" : "default", reasonRequired: action === "bounce", onConfirm: async (reason) => { await apiFetch(`/finance/payments/${kind}/${row.id}/cheque/${action}`, { method:"POST", body:JSON.stringify({ reason, note:reason, requestId:crypto.randomUUID() }) }); setMessage(`Cheque ${action === "clear" ? "cleared" : "bounced and reversed"}.`); await refresh(); } });
  const accountAction = (row: any, action: "edit" | "status") => { if (action === "edit") { setAccountEditor({ ...row }); return; } const active = !row.active; setDialog({ title: `${active ? "Restore" : "Deactivate"} financial account`, message: `${active ? "Restore" : "Deactivate"} ${row.name}? Historical cashbook and ledger entries remain available.`, confirmLabel: active ? "Restore account" : "Deactivate account", tone: active ? "default" : "danger", onConfirm: async () => { await apiFetch(`/cash-accounts/${row.id}/status`, { method: "POST", body: JSON.stringify({ active }) }); setMessage(`Account ${active ? "restored" : "deactivated"}. Historical transactions remain available.`); await refresh(); } }); };
  const saveAccount = async (event: React.FormEvent<HTMLFormElement>) => { event.preventDefault(); setBusy(true); setError(""); try { await apiFetch(`/cash-accounts/${accountEditor.id}`, { method: "PUT", body: JSON.stringify({ name: accountEditor.name.trim(), accountNumber: accountEditor.accountNumber || "", description: accountEditor.description || "" }) }); setAccountEditor(null); setMessage("Account details and linked General Ledger account updated."); await refresh(); } catch (issue: any) { setError(issue.message); } finally { setBusy(false); } };

  const newCta = tab === "Expenses" ? { href: "/finance/new?type=expense", label: "＋ New expense" }
    : tab === "Accounts" ? { href: "/finance/new?type=account", label: "＋ New account" }
    : tab === "Account transfers" ? { href: "/finance/new?type=transfer", label: "＋ New transfer" }
    : null;

  return <main className="shell"><Sidebar active="Cash & bank"/><section className="app financeApp">
    <header className="topbar"><div><span className="crumb">Money &amp; accounting / Daily transactions</span><b>Money &amp; banking</b></div><div className="topbarActions">{newCta && <Link className="new" href={newCta.href}>{newCta.label}</Link>}<button className="ghostButton" onClick={() => window.print()}>Print current view</button></div></header>
    <div className="page financePage"><div className="pageHeading"><div><p>MONEY &amp; BANKING</p><h1>Cash, bank and payments</h1><small>Record money once and automatically update account balances and accounting history.</small></div></div>
      <section className="financeMetrics"><Metric label="Cash available" figure={moneyFigure(summary.cashBalance)} tone="green"/><Metric label="Bank balance" figure={moneyFigure(summary.bankBalance)} tone="blue"/><Metric label="Customers owe us" figure={moneyFigure(summary.customerReceivables)} tone="amber"/><Metric label="We owe suppliers" figure={moneyFigure(summary.supplierPayables)} tone="red"/><Metric label="Expenses this month" figure={moneyFigure(summary.monthExpenses)} tone="slate"/></section>
      <nav className="financeTabs" aria-label="Finance sections">{tabs.map((item) => <button key={item} className={tab === item ? "active" : ""} onClick={() => { setTab(item); setMessage(""); setError(""); }}>{item}</button>)}</nav>
      {message && <div className="financeNotice success">{message}<button onClick={() => setMessage("")}>×</button></div>}{error && <div className="financeNotice error">{error}<button onClick={() => setError("")}>×</button></div>}
      <div className="financeWorkspace">
        <section className="financeActionPanel">
          {tab === "Customer receipts" && <PaymentForm title="Receive customer payment" help="Select the customer and deposit account. Their outstanding balance reduces after saving." parties={customers} accounts={accounts} busy={busy} onSubmit={(e: any) => submit(e, "/customer-payments", (d) => ({ partyId: d.get("partyId"), accountId: d.get("accountId"), method: accountType(accounts, d.get("accountId")), amount: Number(d.get("amount")), date: d.get("date"), reference: d.get("reference"), notes: d.get("notes"), chequeNumber:d.get("chequeNumber"), chequeDate:d.get("chequeDate"), requestId: crypto.randomUUID() }))}/>} 
          {tab === "Supplier payments" && <PaymentForm title="Pay a supplier" help="The payment reduces supplier payable and the selected cash or bank balance." parties={suppliers} accounts={accounts} busy={busy} onSubmit={(e: any) => submit(e, "/supplier-payments", (d) => ({ partyId: d.get("partyId"), accountId: d.get("accountId"), method: accountType(accounts, d.get("accountId")), amount: Number(d.get("amount")), date: d.get("date"), reference: d.get("reference"), notes: d.get("notes"), chequeNumber:d.get("chequeNumber"), chequeDate:d.get("chequeDate"), requestId: crypto.randomUUID() }))}/>} 
          {(tab === "Customer receipts" || tab === "Supplier payments") && <ChequeQueue rows={rows} kind={tab === "Customer receipts" ? "customer" : "supplier"} action={chequeAction}/>} 
          {(tab === "Expenses" || tab === "Account transfers" || tab === "Accounts") && <div className="financeGuide"><span>＋</span><h2>{tab === "Accounts" ? "Cash & bank accounts" : tab === "Account transfers" ? "Account transfers" : "Expenses"}</h2><p>{tab === "Accounts" ? "Each account gets a linked General Ledger account automatically." : tab === "Account transfers" ? "A transfer posts matched cashbook and ledger entries in one step." : "Record and finalise expenses to post them to the cashbook and journals."}</p>{newCta && <Link className="new" href={newCta.href}>{newCta.label}</Link>}</div>}
          {tab === "Cashbook" && <div className="financeGuide"><span>↕</span><h2>Immutable cashbook</h2><p>Sales, purchases, party payments, expenses and transfers appear here automatically. Correct a finalized transaction using its reversal action instead of deleting it.</p></div>}
        </section>
        <FinanceHistory tab={tab} rows={rows} reverse={reverse} reverseTransfer={reverseTransfer} expenseAction={expenseAction} accountAction={accountAction}/>
      </div>
    </div>
    {accountEditor && <div className="actionDialogBackdrop" role="presentation"><form className="actionDialog userEditorDialog" role="dialog" aria-modal="true" aria-labelledby="account-editor-title" onSubmit={saveAccount}><header><div><small>FINANCIAL ACCOUNT</small><h2 id="account-editor-title">Edit account details</h2></div><button type="button" onClick={() => setAccountEditor(null)} aria-label="Close">×</button></header><div className="dialogFieldGrid"><label>Account name<input autoFocus value={accountEditor.name} onChange={e => setAccountEditor({ ...accountEditor, name: e.target.value })} required/></label><label>Account number<input value={accountEditor.accountNumber || ""} onChange={e => setAccountEditor({ ...accountEditor, accountNumber: e.target.value })}/></label><label style={{gridColumn:"1 / -1"}}>Description<input value={accountEditor.description || ""} onChange={e => setAccountEditor({ ...accountEditor, description: e.target.value })}/></label></div><footer><button type="button" onClick={() => setAccountEditor(null)}>Cancel</button><button className="new" disabled={busy}>{busy ? "Saving…" : "Save changes"}</button></footer></form></div>}
    <ActionDialog request={dialog} close={() => setDialog(null)} />
  </section></main>;
}

function Metric({ label, figure, value, tone }: any) { return <article className={`financeMetric ${tone}`}><span/><div><small>{label}</small>{figure ? <b className={figure.className} title={figure.title}>{figure.text}</b> : <b>{value}</b>}</div></article>; }
function accountType(accounts: any[], id: FormDataEntryValue | null) { return accounts.find((item) => item.id === id)?.type || "cash"; }
function Field({ label, children, wide = false }: any) { return <label className={wide ? "wide" : ""}><span>{label}</span>{children}</label>; }
function FormShell({ title, help, busy, onSubmit, children }: any) { return <form className="financeForm" onSubmit={onSubmit}><header><span>NEW</span><div><h2>{title}</h2><p>{help}</p></div></header><div className="financeFormGrid">{children}</div><footer><small>Required fields must be completed before saving.</small><button className="new" disabled={busy}>{busy ? "Saving…" : "Save record"}</button></footer></form>; }
function PaymentForm({ title, help, parties, accounts, busy, onSubmit }: any) {
  const [party, setParty] = useState("");
  const [accountId, setAccountId] = useState("");
  const [cheque, setCheque] = useState(false);
  const selected = parties.find((item: any) => item.id === party);
  const bankSelected = accounts.find((item: any) => item.id === accountId)?.type === "bank";
  return <FormShell title={title} help={help} busy={busy} onSubmit={onSubmit}>
    <Field label="Customer / supplier"><select name="partyId" value={party} onChange={(e) => setParty(e.target.value)} required><option value="">Select account</option>{parties.map((item: any) => <option value={item.id} key={item.id}>{item.name} — due {money(item.balance)}</option>)}</select></Field>
    <Field label="Pay from / deposit to"><select name="accountId" value={accountId} onChange={(e) => { setAccountId(e.target.value); if (accounts.find((item:any)=>item.id===e.target.value)?.type !== "bank") setCheque(false); }} required><option value="">Select cash or bank</option>{accounts.map((item: any) => <option value={item.id} key={item.id}>{item.name} — {money(item.balance)}</option>)}</select></Field>
    {selected && <div className="formInsight"><small>Current outstanding</small><b>{money(selected.balance)}</b></div>}
    <Field label="Amount"><input name="amount" type="number" min="0.01" max={selected?.balance || undefined} step="0.01" required/></Field>
    <Field label="Payment date"><input name="date" type="date" defaultValue={new Date().toISOString().slice(0,10)}/></Field>
    {bankSelected && <label className="approvalCheck"><input type="checkbox" checked={cheque} onChange={(e)=>setCheque(e.target.checked)}/> This is a cheque payment</label>}
    {cheque && <><Field label="Cheque number"><input name="chequeNumber" required placeholder="Cheque number"/></Field><Field label="Cheque date"><input name="chequeDate" type="date" required/></Field></>}
    <Field label="Reference"><input name="reference" placeholder="Receipt or transfer reference"/></Field>
    <Field label="Internal notes" wide><textarea name="notes" placeholder="Optional notes"/></Field>
  </FormShell>;
}

function ChequeQueue({rows,kind,action}:any){const pending=rows.filter((x:any)=>x.clearanceStatus==="pending");if(!pending.length)return null;return <section className="financeGuide"><span>⌛</span><h2>Cheques awaiting clearance</h2><p>Confirm cleared cheques or bounce them with a mandatory reversal reason.</p><div className="policyGrid">{pending.map((x:any)=><article key={x.id}><div><b>{x.chequeNumber}</b><small>{x.party} · {money(x.amount)} · {formatDate(x.chequeDate)}</small></div><button onClick={()=>action(kind,x,"clear")}>Clear</button><button className="dangerButton" onClick={()=>action(kind,x,"bounce")}>Bounce</button></article>)}</div></section>}

function FinanceHistory({ tab, rows, reverse, reverseTransfer, expenseAction, accountAction }: any) {
  const columns = useMemo(() => tab === "Accounts" ? ["Account", "Type", "GL", "Balance", "Details", "Actions"] : tab === "Expenses" ? ["Date", "Category / description", "Account", "Amount", "Status", "Actions"] : tab.includes("payments") || tab === "Customer receipts" ? ["Date", "Party", "Account", "Amount", "Status", "Actions"] : tab === "Account transfers" ? ["Date", "From account", "Movement", "Reference", "Amount", "Actions"] : ["Date", "Account", "Type / description", "Reference", "Amount", "Status"], [tab]);
  return <section className="financeHistory"><header><div><small>RECENT ACTIVITY</small><h2>{tab}</h2></div><span>{rows.length} records</span></header><div className="tableScroll"><table><thead><tr>{columns.map((x) => <th key={x}>{x}</th>)}</tr></thead><tbody>{!rows.length && <tr><td colSpan={columns.length} className="emptyTable">No records yet.</td></tr>}{rows.map((row: any) => tab === "Accounts" ? <tr key={row.id}><td><b>{row.name}</b><small>{row.accountNumber || "No account number"}</small></td><td>{row.type}</td><td>{row.ledgerCode || "—"}</td><td><b>{money(row.balance)}</b></td><td>{row.description || "—"}<small><Status value={row.active ? "active" : "inactive"}/></small></td><td><div className="recordActions"><button onClick={() => accountAction(row,"edit")}>Edit</button><button className={!row.active ? "" : "dangerText"} onClick={() => accountAction(row,"status")}>{row.active ? "Deactivate" : "Restore"}</button></div></td></tr> : tab === "Expenses" ? <tr key={row.id}><td>{formatDate(row.date)}</td><td><b>{row.category}</b><small>{row.description}</small></td><td>{row.account || "—"}</td><td><b>{money(row.amount)}</b></td><td><Status value={row.status}/></td><td><div className="recordActions">{row.status === "draft" && <><button onClick={() => expenseAction(row.id,"finalize")}>Finalize</button><button onClick={() => expenseAction(row.id,"cancel")}>Cancel</button></>}{row.status === "finalized" && <button className="dangerText" onClick={() => expenseAction(row.id,"reverse")}>Reverse</button>}</div></td></tr> : tab === "Customer receipts" || tab === "Supplier payments" ? <tr key={row.id}><td>{formatDate(row.date)}</td><td><b>{row.party}</b><small>{row.reference || "No reference"}</small></td><td>{row.account || row.method}</td><td><b>{money(row.amount)}</b></td><td><Status value={row.status}/></td><td>{row.status === "finalized" && <button className="tableAction dangerText" onClick={() => reverse(tab === "Customer receipts" ? "customer" : "supplier", row.id)}>Reverse</button>}</td></tr> : <tr key={row.id}><td>{formatDate(row.date)}</td><td><b>{row.account}</b><small>{row.accountType}</small></td><td><b>{String(row.type || "").replaceAll("_"," ")}</b><small>{row.description || "—"}</small></td><td>{row.reference || "—"}</td><td className={row.type === "income" || row.type === "deposit" || row.type === "transfer_in" ? "amountIn" : "amountOut"}>{money(row.amount)}</td><td>{tab === "Account transfers" ? row.status === "posted" ? <button className="tableAction dangerText" onClick={() => reverseTransfer(row.journalEntryId)}>Reverse</button> : <Status value={row.status}/> : <Status value={row.status}/>}</td></tr>)}</tbody></table></div></section>;
}
function Status({ value }: any) { return <span className={`financeStatus ${value}`}>{value}</span>; }
function formatDate(value: any) { return value ? new Date(value).toLocaleDateString("en-LK") : "—"; }
