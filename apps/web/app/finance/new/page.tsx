"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Sidebar from "../../components/Sidebar";
import ActionDialog, { type ActionDialogRequest } from "../../components/ActionDialog";
import { apiFetch } from "../../lib/api";
import { useRequireSession } from "../../lib/useRequireSession";
import { money } from "../../lib/format";

type Type = "expense" | "account" | "transfer";

const meta: Record<Type, { crumb: string; title: string; help: string; back: string }> = {
  expense: { crumb: "Expenses", title: "Record an expense", help: "Saved as a draft. Finalising it creates the cashbook and journal entries.", back: "/finance?section=expenses" },
  account: { crumb: "Accounts", title: "Add a cash or bank account", help: "A linked General Ledger account is created automatically.", back: "/finance?section=accounts" },
  transfer: { crumb: "Account transfers", title: "Transfer between accounts", help: "A single transfer creates matched cashbook and General Ledger entries.", back: "/finance?section=transfers" },
};

export default function NewFinanceRecord() {
  useRequireSession();
  const router = useRouter();
  const type = ((useSearchParams().get("type") as Type) in meta ? useSearchParams().get("type") : "expense") as Type;
  const info = meta[type];
  const [accounts, setAccounts] = useState<any[]>([]);
  const [categories, setCategories] = useState<any[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [dialog, setDialog] = useState<ActionDialogRequest | null>(null);
  const today = new Date().toISOString().slice(0, 10);

  useEffect(() => {
    const calls: Promise<any>[] = [apiFetch("/cash-accounts")];
    if (type === "expense") calls.push(apiFetch("/expense-categories"));
    Promise.all(calls).then(([acc, cats]) => { setAccounts(acc); if (cats) setCategories(cats); }).catch((e) => setError(e.message));
  }, [type]);

  const accType = (id: any) => accounts.find((a) => a.id === id)?.type || "cash";
  const addCategory = () => setDialog({ title: "Add expense category", message: "Create a reusable category for expense entry and financial reporting.", confirmLabel: "Create category", reasonRequired: true, reasonLabel: "Category name", onConfirm: async name => { await apiFetch("/expense-categories", { method: "POST", body: JSON.stringify({ name }) }); setCategories(await apiFetch("/expense-categories")); } });

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault(); setBusy(true); setError("");
    const d = new FormData(e.currentTarget);
    try {
      if (type === "expense") {
        await apiFetch("/expenses", { method: "POST", body: JSON.stringify({ categoryId: d.get("categoryId"), accountId: d.get("accountId"), paymentMethod: accType(d.get("accountId")), description: d.get("description"), amount: Number(d.get("amount")), date: d.get("date"), reference: d.get("reference"), notes: d.get("notes"), requestId: crypto.randomUUID() }) });
      } else if (type === "account") {
        await apiFetch("/cash-accounts", { method: "POST", body: JSON.stringify({ name: d.get("name"), type: d.get("type"), accountNumber: d.get("accountNumber"), openingBalance: Number(d.get("openingBalance")), description: d.get("description") }) });
      } else {
        await apiFetch("/finance/transfers", { method: "POST", body: JSON.stringify({ fromAccountId: d.get("fromAccountId"), toAccountId: d.get("toAccountId"), amount: Number(d.get("amount")), date: d.get("date"), reference: d.get("reference"), notes: d.get("notes"), requestId: crypto.randomUUID() }) });
      }
      router.push(info.back);
    } catch (err: any) { setError(err.message); setBusy(false); }
  };

  return <main className="shell"><Sidebar active="Cash & bank" /><section className="app financeApp">
    <header className="topbar"><div><span className="crumb">Finance / {info.crumb}</span><b>{info.title}</b></div><Link className="ghostButton" href={info.back}>Cancel</Link></header>
    <div className="page formPage">
      <div className="formIntro"><span className="pageIcon">{type[0].toUpperCase()}</span><p>NEW {info.crumb.toUpperCase()}</p><h1>{info.title}</h1><small>{info.help}</small></div>
      <form className="saasForm" onSubmit={submit}>
        {error && <button type="button" className="formError" onClick={() => setError("")}>{error}</button>}
        {type === "expense" && <div className="formSection">
          <div><b>Expense</b><small>What was spent and from where.</small></div>
          <label>Category<span className="inlineSelect"><select name="categoryId" required><option value="">Select category</option>{categories.filter((c) => c.active ?? c.isActive).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select><button type="button" onClick={addCategory}>＋</button></span></label>
          <label>Payment account<select name="accountId" required><option value="">Select account</option>{accounts.map((a) => <option key={a.id} value={a.id}>{a.name} — {money(a.balance)}</option>)}</select></label>
          <label className="wide">Description<input name="description" placeholder="What was this expense for?" required /></label>
          <label>Amount<input name="amount" type="number" min="0.01" step="0.01" required /></label>
          <label>Expense date<input name="date" type="date" defaultValue={today} /></label>
          <label>Reference<input name="reference" placeholder="Bill or voucher number" /></label>
          <label className="wide">Notes<textarea name="notes" rows={3} placeholder="Optional internal notes" /></label>
        </div>}
        {type === "account" && <div className="formSection">
          <div><b>Account</b><small>Cash drawer or bank account.</small></div>
          <label>Account name<input name="name" placeholder="e.g. Main till or Commercial Bank" required /></label>
          <label>Account type<select name="type"><option value="cash">Cash</option><option value="bank">Bank</option></select></label>
          <label>Account number<input name="accountNumber" placeholder="Optional bank account number" /></label>
          <label>Opening balance<input name="openingBalance" type="number" min="0" step="0.01" defaultValue="0" /></label>
          <label className="wide">Description<textarea name="description" rows={3} placeholder="Branch, till or account purpose" /></label>
        </div>}
        {type === "transfer" && <div className="formSection">
          <div><b>Transfer</b><small>Move money between two accounts.</small></div>
          <label>From account<select name="fromAccountId" required><option value="">Select source</option>{accounts.map((a) => <option key={a.id} value={a.id}>{a.name} — {money(a.balance)}</option>)}</select></label>
          <label>To account<select name="toAccountId" required><option value="">Select destination</option>{accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}</select></label>
          <label>Amount<input name="amount" type="number" min="0.01" step="0.01" required /></label>
          <label>Transfer date<input name="date" type="date" defaultValue={today} /></label>
          <label>Reference<input name="reference" placeholder="Deposit slip or transfer ID" /></label>
          <label className="wide">Notes<input name="notes" placeholder="Purpose of transfer" /></label>
        </div>}
        <footer><Link href={info.back}>Cancel</Link><button className="new" disabled={busy}>{busy ? "Saving…" : type === "account" ? "Create account" : type === "transfer" ? "Record transfer" : "Save expense"}</button></footer>
      </form>
    </div>
    <ActionDialog request={dialog} close={() => setDialog(null)}/>
  </section></main>;
}
