"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

export type ActionDialogRequest = { title: string; message: string; confirmLabel?: string; tone?: "default" | "danger"; reasonRequired?: boolean; reasonLabel?: string; exactValue?: string; onConfirm: (value: string) => Promise<void> | void };

export default function ActionDialog({ request, close }: { request: ActionDialogRequest | null; close: () => void }) {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => { setValue(""); setError(""); setBusy(false); }, [request]);
  useEffect(() => {
    if (!request) return;
    const previousOverflow = document.body.style.overflow;
    const closeOnEscape = (event: KeyboardEvent) => { if (event.key === "Escape" && !busy) close(); };
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", closeOnEscape);
    return () => { document.body.style.overflow = previousOverflow; window.removeEventListener("keydown", closeOnEscape); };
  }, [request, busy, close]);

  if (!request) return null;
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    const clean = value.trim();
    if (request.reasonRequired && !clean) { setError("Please enter the required value."); return; }
    if (request.exactValue && clean !== request.exactValue) { setError(`Enter ${request.exactValue} exactly to continue.`); return; }
    setBusy(true); setError("");
    try { await request.onConfirm(clean); close(); }
    catch (caught: any) { setError(caught.message || "Action failed."); }
    finally { setBusy(false); }
  };

  return createPortal(<div className="actionDialogBackdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) close(); }}>
    <form className="actionDialog" role="dialog" aria-modal="true" aria-labelledby="action-dialog-title" onSubmit={submit}>
      <header><div><small>CONFIRM ACTION</small><h2 id="action-dialog-title">{request.title}</h2></div><button type="button" onClick={close} disabled={busy} aria-label="Close">×</button></header>
      <p>{request.message}</p>
      {request.reasonRequired && <label>{request.reasonLabel || "Reason"}<textarea autoFocus rows={3} value={value} onChange={(event) => setValue(event.target.value)} placeholder={request.exactValue || "Explain why this action is required"} required /></label>}
      {error && <div className="formError" role="alert">{error}</div>}
      <footer><button type="button" onClick={close} disabled={busy}>Go back</button><button className={request.tone === "danger" ? "dangerButton" : "new"} disabled={busy || !!request.reasonRequired && !value.trim()}>{busy ? "Processing..." : request.confirmLabel || "Confirm"}</button></footer>
    </form>
  </div>, document.body);
}
