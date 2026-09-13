"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import Sidebar from "../components/Sidebar";
import BusinessDocumentHeader from "../components/BusinessDocumentHeader";
import ActionDialog, { type ActionDialogRequest } from "../components/ActionDialog";
import { apiFetch } from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";
import "./procurement.css";
import { money } from "../lib/format";

type Mode = "po" | "grn";
type PurchaseLine = {
  lineId: string; productId: string; quantity: number; freeQuantity: number;
  unitId: string; unitSymbol: string; conversionFactor: number;
  unitCost: number; sellingPrice: number; wholesalePrice: number; discount: number; tax: number;
  batchNo: string; manufacturedDate: string; expiryDate: string;
  updatePurchasePrice: boolean; updateSellingPrice: boolean; editingPrices: boolean;
};

const tabs = ["Purchase orders", "GRN / Purchases", "Purchase returns"];
const tabRoutes: Record<string, string> = {
  "Purchase orders": "/procurement/orders",
  "GRN / Purchases": "/procurement/grns",
  "Purchase returns": "/procurement/returns",
};

const numberValue = (value: string) => Number(value || 0);
const makeLineId = () => (typeof crypto !== "undefined" ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`);

function newLine(product: any): PurchaseLine {
  const purchaseUnit = product.allowedUnits?.find((unit: any) => unit.isDefaultPurchase)
    || product.allowedUnits?.find((unit: any) => ["purchase", "both"].includes(unit.usage));
  return {
    lineId: makeLineId(), productId: product.productId, quantity: Number(product.minimumOrderQty || 1), freeQuantity: 0,
    unitId: purchaseUnit?.unitId || "", unitSymbol: purchaseUnit?.symbol || product.baseUnit || "unit", conversionFactor: Number(purchaseUnit?.factorToBase || 1),
    unitCost: Number(purchaseUnit?.purchasePrice ?? product.lastPurchasePrice ?? product.defaultPurchasePrice ?? product.purchasePrice ?? 0),
    sellingPrice: Number(product.recommendedSellingPrice || product.sellingPrice || 0),
    wholesalePrice: Number(product.wholesalePrice || product.productWholesalePrice || 0),
    discount: 0, tax: 0, batchNo: "", manufacturedDate: "", expiryDate: "",
    updatePurchasePrice: false, updateSellingPrice: false, editingPrices: false,
  };
}

export function ProcurementWorkspace({ initialTab = tabs[0], view = "full" }: { initialTab?: string; view?: "full" | "list" | "create" }) {
  useRequireSession();
  const router = useRouter();
  const [tab, setTab] = useState(tabs.includes(initialTab) ? initialTab : tabs[0]);
  const [rows, setRows] = useState<any[]>([]);
  const [products, setProducts] = useState<any[]>([]);
  const [suppliers, setSuppliers] = useState<any[]>([]);
  const [accounts, setAccounts] = useState<any[]>([]);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [receiving, setReceiving] = useState<any>(null);
  const [document, setDocument] = useState<any>(null);
  const [audit, setAudit] = useState<any>(null);
  const [related, setRelated] = useState<any>(null);
  const [editing, setEditing] = useState<any>(null);
  const [paying, setPaying] = useState<any>(null);
  const [dialog, setDialog] = useState<ActionDialogRequest | null>(null);
  const endpoint = tab === tabs[0] ? "/purchase-orders" : tab === tabs[1] ? "/procurement/grns" : "/purchase-returns";

  const load = async () => {
    try { setRows(await apiFetch(endpoint)); setError(""); }
    catch (issue: any) { setError(issue.message); }
  };
  const loadProducts = async () => setProducts(await apiFetch("/products"));

  useEffect(() => {
    Promise.all([apiFetch("/products"), apiFetch("/suppliers")]).then(([p, s]) => { setProducts(p); setSuppliers(s); }).catch((issue: any) => setError(issue.message));
    apiFetch("/cash-accounts").then(setAccounts).catch(() => setAccounts([]));
  }, []);
  useEffect(() => { load(); }, [tab]);
  useEffect(() => { setTab(tabs.includes(initialTab) ? initialTab : tabs[0]); }, [initialTab]);

  const action = async (path: string, needsReason = false) => {
    const label = path.split("/").pop()?.replaceAll("-", " ") || "continue";
    setDialog({ title: `${label[0].toUpperCase()}${label.slice(1)} document`, message: needsReason ? "This changes a controlled purchasing record. The original document and audit trail will remain available." : "Review the purchasing document before continuing to the next workflow state.", confirmLabel: label, tone: needsReason ? "danger" : "default", reasonRequired: needsReason, onConfirm: async (reason) => { await apiFetch(path, { method: "POST", body: JSON.stringify({ reason, requestId: crypto.randomUUID() }) }); setNotice("Workflow updated successfully."); setError(""); await load(); } });
  };
  const openDocument = async (row: any) => { try { setDocument(tab === tabs[1] ? await apiFetch(`/procurement/grns/${row.id}/detail`) : row); } catch (issue: any) { setError(issue.message); } };
  const openAudit = async (row: any) => { try { setAudit({ row, entries: await apiFetch(`/procurement/audit?entityId=${row.id}`) }); } catch (issue: any) { setError(issue.message); } };
  const openRelated = async (row: any) => { try { setRelated({ row, type: tab === tabs[0] ? "receipts" : "payments", data: await apiFetch(tab === tabs[0] ? `/purchase-orders/${row.id}/receipts` : `/procurement/catalog-grns/${row.id}/payments`) }); } catch (issue: any) { setError(issue.message); } };
  const openEdit = async (row: any) => { try { const detail = await apiFetch(`/procurement/grns/${row.id}/detail`); if (detail.purchaseOrderId) throw new Error("Cancel and recreate this PO receipt so its order-line reservation stays correct."); setEditing(detail); } catch (issue: any) { setError(issue.message); } };
  const openPayment = async (row: any) => { try { const detail = await apiFetch(`/procurement/grns/${row.id}/detail`); if (Number(detail.dueAmount) <= 0) throw new Error("This GRN is already fully paid."); setPaying(detail); } catch (issue: any) { setError(issue.message); } };
  const completed = async (text: string) => {
    if (view === "create") { router.push(`${tabRoutes[tab]}?done=1`); return; }
    setNotice(text); setError(""); await load();
  };

  const showForm = view !== "list";
  const showList = view !== "create";
  const crumbLast = view === "create" ? `New ${tab.toLowerCase()}` : "Control centre";

  return <main className="shell"><Sidebar active={tab} /><section className="app procurementApp">
    <header className="topbar"><div><span className="crumb">Purchases / {crumbLast}</span><b>Purchase management</b></div>
      <div className="topbarActions">
        {view === "list" && <Link className="new" href={`${tabRoutes[tab]}/new`}>＋ New {tab === tabs[0] ? "purchase order" : tab === tabs[1] ? "GRN" : "return"}</Link>}
        {view === "create" && <Link className="ghostButton" href={tabRoutes[tab]}>Cancel</Link>}
        {view !== "create" && <button className="ghostButton" onClick={() => window.print()}>Print</button>}
      </div>
    </header>
    <div className="page workflowPage procurementPage">
      <div className="pageHeading"><div><p>PURCHASES &amp; STOCK RECEIVING</p><h1>Purchase management</h1><small>Create purchase orders, receive supplier stock and record returns.</small></div></div>
      {view !== "create" && <nav className="workflowTabs" aria-label="Purchase sections">{tabs.map((item) => <Link className={item === tab ? "active" : ""} href={tabRoutes[item]} key={item}>{item === "GRN / Purchases" ? "Receive stock (GRN)" : item === "Purchase returns" ? "Return items to supplier" : item}</Link>)}</nav>}
      {error && <div className="catalogMessage errorMessage" role="alert"><span>{error}</span><button onClick={() => setError("")} aria-label="Dismiss error">×</button></div>}
      {notice && <div className="catalogMessage successMessage" role="status"><span>{notice}</span><button onClick={() => setNotice("")} aria-label="Dismiss message">×</button></div>}
      {showForm && tab !== tabs[2] && <EntryForm mode={tab === tabs[0] ? "po" : "grn"} products={products} suppliers={suppliers} accounts={accounts} reloadProducts={loadProducts} done={completed} fail={setError} />}
      {showForm && tab === tabs[2] && <ReturnForm done={() => completed("Purchase return draft saved.")} fail={setError} />}
      {showList && <Records tab={tab} rows={rows} action={action} receive={setReceiving} document={openDocument} audit={openAudit} related={openRelated} edit={openEdit} pay={openPayment} />}
      {receiving && <ReceivePODialog order={receiving} accounts={accounts} close={() => setReceiving(null)} fail={setError} saved={async () => { setReceiving(null); setNotice("Partial receipt saved as a draft GRN. Review and finalize it from GRN / Purchases."); await load(); }} />}
      {document && <ProcurementDocument data={document} type={tab} close={() => setDocument(null)} />}
      {audit && <AuditDrawer data={audit} close={() => setAudit(null)} />}
      {related && <RelatedDrawer value={related} close={() => setRelated(null)} />}
      {editing && <div className="workflowModal editGrnModal" role="dialog" aria-modal="true"><section><header><div><small>DRAFT CORRECTION</small><h2>Edit GRN {editing.invoiceNo}</h2></div><button onClick={() => setEditing(null)}>×</button></header><EntryForm mode="grn" products={products} suppliers={suppliers} accounts={accounts} reloadProducts={loadProducts} initialData={editing} editId={editing.id} done={async () => { setEditing(null); setNotice("Draft GRN updated."); await load(); }} fail={setError} /></section></div>}
      {paying && <GRNPaymentDialog grn={paying} accounts={accounts} close={() => setPaying(null)} saved={async () => { setPaying(null); setNotice("Supplier payment allocated to the GRN."); await load(); }} fail={setError} />}
    </div>
    <ActionDialog request={dialog} close={() => setDialog(null)} />
  </section></main>;
}

export default function Procurement() { return <ProcurementWorkspace view="list" />; }

function EntryForm({ mode, products, suppliers, accounts, reloadProducts, done, fail, initialData, editId }: any) {
  const initialForm = initialData ? { supplierId: initialData.supplierId, invoiceNo: initialData.invoiceNo, purchaseDate: initialData.purchaseDate, expectedDate: "", paymentMethod: initialData.paymentMethod || "credit", accountId: initialData.accountId || "", paidAmount: Number(initialData.initialPaidAmount || 0), discount: Number(initialData.discount || 0), tax: Number(initialData.tax || 0), notes: initialData.notes || "", items: (initialData.items || []).map((item: any) => ({ lineId: makeLineId(), productId: item.productId, quantity: Number(item.purchaseQuantity ?? item.quantity), freeQuantity: Number(item.freeQuantity || 0), unitCost: Number(item.unitCost || 0), sellingPrice: Number(item.sellingPrice || 0), wholesalePrice: Number(item.wholesalePrice || 0), discount: Number(item.discount || 0), tax: Number(item.tax || 0), batchNo: item.batchNo || "", manufacturedDate: item.manufacturedDate || "", expiryDate: item.expiryDate || "", updatePurchasePrice: Boolean(item.updatePurchasePrice), updateSellingPrice: Boolean(item.updateSellingPrice), editingPrices: false })) } : { supplierId: "", invoiceNo: "", purchaseDate: new Date().toISOString().slice(0, 10), expectedDate: "", paymentMethod: "credit", accountId: "", paidAmount: 0, discount: 0, tax: 0, notes: "", items: [] };
  const [form, setForm] = useState<any>(initialForm);
  const [catalogue, setCatalogue] = useState<any[]>([]);
  const [search, setSearch] = useState(""); const [assignId, setAssignId] = useState("");
  const [quick, setQuick] = useState(false); const [busy, setBusy] = useState(false); const [catalogueBusy, setCatalogueBusy] = useState(false);
  const set = (key: string, value: any) => setForm((current: any) => ({ ...current, [key]: value }));
  const updateLine = (lineId: string, key: keyof PurchaseLine, value: any) => setForm((current: any) => ({ ...current, items: current.items.map((line: PurchaseLine) => line.lineId === lineId ? { ...line, [key]: value } : line) }));

  const refreshCatalogue = async (supplierId = form.supplierId) => {
    if (!supplierId) { setCatalogue([]); return; }
    setCatalogueBusy(true);
    try { setCatalogue(await apiFetch(`/procurement/suppliers/${supplierId}/products`)); fail(""); }
    catch (issue: any) { fail(issue.message); }
    finally { setCatalogueBusy(false); }
  };
  useEffect(() => { if (initialData?.supplierId) refreshCatalogue(initialData.supplierId); }, []);
  const selectSupplier = async (supplierId: string) => { setForm((current: any) => ({ ...current, supplierId, items: [] })); setSearch(""); await refreshCatalogue(supplierId); };
  const addProduct = (product: any) => { fail(""); set("items", [...form.items, newLine(product)]); };
  const duplicateBatch = (source: PurchaseLine) => set("items", [...form.items, { ...source, lineId: makeLineId(), batchNo: "", manufacturedDate: "", expiryDate: "", quantity: 1, freeQuantity: 0, editingPrices: false }]);
  const removeLine = (lineId: string) => set("items", form.items.filter((line: PurchaseLine) => line.lineId !== lineId));
  const total = Math.max(0, form.items.reduce((sum: number, line: PurchaseLine) => sum + line.quantity * line.unitCost - line.discount + line.tax, 0) - form.discount + form.tax);
  const shownProducts = useMemo(() => catalogue.filter((product) => `${product.name} ${product.sku} ${product.barcode} ${product.supplierProductCode}`.toLowerCase().includes(search.trim().toLowerCase())), [catalogue, search]);

  const assign = async () => {
    if (!assignId || !form.supplierId) return;
    const product = products.find((item: any) => item.ID === assignId);
    try {
      await apiFetch(`/procurement/suppliers/${form.supplierId}/products`, { method: "POST", body: JSON.stringify({ productId: assignId, defaultPurchasePrice: product?.PurchasePrice || 0, recommendedSellingPrice: product?.SellingPrice || 0, wholesalePrice: product?.WholesalePrice || 0, minimumOrderQty: 1, packSize: 1, isActive: true }) });
      setAssignId(""); await refreshCatalogue();
    } catch (issue: any) { fail(issue.message); }
  };

  const validate = () => {
    if (!form.items.length) return "Select at least one product from the supplier catalogue.";
    {
      const batchKeys = new Set<string>();
      for (const line of form.items as PurchaseLine[]) {
        const product = catalogue.find((item) => item.productId === line.productId);
        if (product?.trackExpiry && (!line.batchNo.trim() || !line.expiryDate)) return `${product.name}: batch number and expiry date are required.`;
        const key = `${line.productId}|${line.batchNo.trim().toLowerCase()}|${line.expiryDate}`;
        if (batchKeys.has(key)) return `${product?.name || "Product"}: use a different batch or expiry date for each separate line.`;
        batchKeys.add(key);
        if (line.manufacturedDate && line.expiryDate && line.manufacturedDate > line.expiryDate) return `${product?.name || "Product"}: manufactured date cannot be after expiry date.`;
      }
    }
    return "";
  };
  const save = async (event: React.FormEvent) => {
    event.preventDefault(); const problem = validate(); if (problem) { fail(problem); return; }
    const submitter = (event.nativeEvent as SubmitEvent).submitter as HTMLButtonElement | null;
    const finalizeNow = !editId && mode === "grn" && submitter?.value === "finalize";
    setBusy(true);
    try {
      const items = form.items.map(({ lineId, editingPrices, unitSymbol, ...line }: PurchaseLine) => ({...line, enteredQuantity: line.quantity, quantity: line.quantity * line.conversionFactor, freeQuantity: line.freeQuantity * line.conversionFactor, enteredUnitCost: line.unitCost, unitCost: line.unitCost / line.conversionFactor}));
      const created = await apiFetch(editId ? `/procurement/catalog-grns/${editId}` : mode === "po" ? "/purchase-orders" : "/procurement/catalog-grns", { method: editId ? "PUT" : "POST", body: JSON.stringify({ ...form, items, requestId: crypto.randomUUID() }) });
      if (!editId) setForm((current: any) => ({ ...current, invoiceNo: "", notes: "", paidAmount: 0, accountId: "", discount: 0, tax: 0, items: [] }));
      if (finalizeNow) {
        try {
          await apiFetch(`/procurement/catalog-grns/${created.id}/finalize`, { method: "POST", body: JSON.stringify({ requestId: crypto.randomUUID() }) });
          await done("Purchase confirmed. Stock, supplier payable and accounting were updated.");
        } catch (finalizeIssue: any) {
          await done("Purchase was saved for review but could not be finalized.");
          fail(`Draft saved successfully. Finalization needs attention: ${finalizeIssue.message}`);
        }
      } else if (editId) {
        await done("Draft GRN updated.");
      } else {
        await done(mode === "po" ? "Purchase order draft saved." : "Purchase saved for review. Confirm it from the list when ready.");
      }
    } catch (issue: any) { fail(issue.message); }
    finally { setBusy(false); }
  };
  const activeStep = !form.supplierId ? 1 : form.items.length === 0 ? 2 : 3;

  return <><form className="workflowForm documentForm procurementForm" onSubmit={save}>
    <div className="formTitle"><div><h2>{editId ? "Edit draft goods received note" : mode === "po" ? "Create purchase order" : "Create goods received note"}</h2><small>{editId ? "Only this unposted draft can be corrected. Stock and accounting are unchanged." : mode === "po" ? "Plan the products and quantities to order." : "Record the supplier invoice, received batches and actual prices."}</small></div><button type="button" className="new" onClick={() => setQuick(true)} disabled={!form.supplierId}>+ New product</button></div>
    <ol className="purchaseSteps" aria-label="Purchase entry progress">{["Supplier & document", "Choose products", mode === "po" ? "Review order" : "Batch, price & review"].map((label, index) => <li className={activeStep >= index + 1 ? "active" : ""} key={label}><span>{index + 1}</span><b>{label}</b></li>)}</ol>

    <section className="entrySection"><div className="sectionHeading"><span>1</span><div><h3>Supplier &amp; document</h3><small>Choose the supplier before selecting products.</small></div></div>
      <div className="documentFields"><Field label="Supplier"><select required value={form.supplierId} onChange={(e) => selectSupplier(e.target.value)}><option value="">Select supplier</option>{suppliers.map((supplier: any) => <option key={supplier.id} value={supplier.id}>{supplier.name}</option>)}</select></Field>
        {mode === "grn" && <><Field label="Supplier invoice number"><input required placeholder="e.g. INV-1048" value={form.invoiceNo} onChange={(e) => set("invoiceNo", e.target.value)} /></Field><Field label="Purchase date"><input required type="date" value={form.purchaseDate} onChange={(e) => set("purchaseDate", e.target.value)} /></Field><Field label="Payment method"><select value={form.paymentMethod} onChange={(e) => setForm((current: any) => ({ ...current, paymentMethod: e.target.value, accountId: e.target.value === "credit" ? "" : current.accountId, paidAmount: e.target.value === "credit" ? 0 : current.paidAmount }))}><option value="credit">Credit</option><option value="cash">Cash</option><option value="bank">Bank</option></select></Field>{form.paymentMethod !== "credit" && <Field label="Payment account"><select required value={form.accountId} onChange={(e) => set("accountId", e.target.value)}><option value="">Select account</option>{accounts.filter((account: any) => account.type === form.paymentMethod || account.accountType === form.paymentMethod).map((account: any) => <option value={account.id} key={account.id}>{account.name}</option>)}</select></Field>}<Field label="Amount paid"><input type="number" min="0" max={total} step="0.01" disabled={form.paymentMethod === "credit"} value={form.paidAmount || ""} onChange={(e) => set("paidAmount", numberValue(e.target.value))} /></Field></>}
        {mode === "po" && <Field label="Expected delivery date"><input type="date" value={form.expectedDate} onChange={(e) => set("expectedDate", e.target.value)} /></Field>}
      </div>
    </section>

    <section className={`entrySection productPicker ${!form.supplierId ? "isDisabled" : ""}`}><div className="sectionHeading"><span>2</span><div><h3>Choose supplier products</h3><small>Prices shown here are defaults and are copied into the selected line.</small></div></div>
      {!form.supplierId ? <div className="emptyCatalogue compactEmpty">Select a supplier to load their product catalogue.</div> : <><div className="catalogueTools"><label className="catalogueSearch"><span>Search catalogue</span><input placeholder="Product name, SKU or barcode" value={search} onChange={(e) => setSearch(e.target.value)} /></label><label><span>Assign an existing product</span><select value={assignId} onChange={(e) => setAssignId(e.target.value)}><option value="">Choose product…</option>{products.filter((product: any) => !catalogue.some((item) => item.productId === product.ID)).map((product: any) => <option value={product.ID} key={product.ID}>{product.Name}</option>)}</select></label><button type="button" className="secondaryButton" onClick={assign} disabled={!assignId}>Assign</button></div>
        <div className="catalogueMeta"><b>{catalogueBusy ? "Loading catalogue…" : `${shownProducts.length} products available`}</b><small>Click a product again to add another batch or expiry variation.</small></div>
        <div className="catalogueGrid">{!catalogueBusy && shownProducts.map((product) => <button type="button" className="supplierProduct" key={product.productId} onClick={() => addProduct(product)}><span className="productAvatar">{product.name?.slice(0, 1).toUpperCase()}</span><span className="catalogueIdentity"><b>{product.name}</b><small>{product.sku || "No SKU"}{product.barcode ? ` · ${product.barcode}` : ""}</small></span><span><small>Last cost</small><b>{money(product.lastPurchasePrice || product.defaultPurchasePrice || product.purchasePrice)}</b></span><span><small>Stock</small><b>{Number(product.stock || 0).toLocaleString("en-LK")}</b></span><strong>+ Add{form.items.some((line: PurchaseLine) => line.productId === product.productId) ? " again" : ""}</strong></button>)}{!catalogueBusy && shownProducts.length === 0 && <div className="emptyCatalogue">No matching products. Assign an existing product or create a new one.</div>}</div>
      </>}
    </section>

    <section className="entrySection selectedSection"><div className="sectionHeading selectedHeading"><span>3</span><div><h3>Selected products</h3><small>{form.items.length} lines · {money(total)}</small></div></div>
      {form.items.length === 0 ? <div className="emptyCatalogue compactEmpty">Products you select will appear here with their current details.</div> : <div className="selectedLines">{form.items.map((line: PurchaseLine, index: number) => <PurchaseLineCard key={line.lineId} line={line} index={index} product={catalogue.find((item) => item.productId === line.productId)} mode={mode} update={updateLine} duplicate={duplicateBatch} remove={removeLine} />)}</div>}
    </section>

    <footer className="documentFooter purchaseFooter"><Field label="Document discount"><input type="number" min="0" step="0.01" value={form.discount || ""} onChange={(e) => set("discount", numberValue(e.target.value))} /></Field><Field label="Document tax"><input type="number" min="0" step="0.01" value={form.tax || ""} onChange={(e) => set("tax", numberValue(e.target.value))} /></Field><Field label="Notes"><input placeholder="Optional internal note" value={form.notes} onChange={(e) => set("notes", e.target.value)} /></Field><div className="purchaseTotal"><small>Document total</small><strong>{money(total)}</strong></div><div className="purchaseSubmitActions">{editId ? <button type="submit" className="new savePurchase" disabled={busy || !form.items.length}>{busy ? "Saving…" : "Save draft changes"}</button> : <>{mode === "grn" && <button type="submit" name="submitAction" value="review" className="secondaryButton savePurchase" disabled={busy || !form.items.length}>{busy ? "Please wait…" : "Save for review"}</button>}<button type="submit" name="submitAction" value={mode === "grn" ? "finalize" : "draft"} className="new savePurchase" disabled={busy || !form.items.length}>{busy ? "Please wait…" : mode === "grn" ? "Confirm & receive stock" : "Save purchase order draft"}</button></>}</div></footer>
</form>{quick && <QuickProduct supplierId={form.supplierId} close={() => setQuick(false)} saved={async () => { await reloadProducts(); await refreshCatalogue(); setQuick(false); }} fail={fail} />}</>;
}

function PurchaseLineCard({ line, index, product, mode, update, duplicate, remove }: any) {
  const currentCost = Number(product?.lastPurchasePrice || product?.defaultPurchasePrice || product?.purchasePrice || 0);
  const currentSale = Number(product?.recommendedSellingPrice || product?.sellingPrice || 0);
  const currentWholesale = Number(product?.wholesalePrice || product?.productWholesalePrice || 0);
  const changedCost = line.unitCost !== currentCost; const changedSale = line.sellingPrice !== currentSale || line.wholesalePrice !== currentWholesale;
  const margin = line.sellingPrice > 0 ? ((line.sellingPrice - line.unitCost) / line.sellingPrice) * 100 : 0;
  return <article className="purchaseLineCard">
    <header className="purchaseLineHeader">
      <div className="lineIndex">{index + 1}</div>
      <div className="lineProductIdentity"><h4>{product?.name || "Product"}</h4><p>{product?.sku || "No SKU"}{product?.barcode ? ` / ${product.barcode}` : ""}</p></div>
      <div className="productFacts">
        <span><small>Stock</small><b>{Number(product?.stock || 0).toLocaleString("en-LK")}</b></span>
        <span><small>Current cost</small><b>{money(currentCost)}</b></span>
        <span><small>Retail</small><b>{money(currentSale)}</b></span>
        <span><small>Wholesale</small><b>{money(currentWholesale)}</b></span>
        {product?.trackExpiry && <em>Expiry tracked</em>}
      </div>
      <button type="button" className="iconButton removeProduct" onClick={() => remove(line.lineId)} aria-label={`Remove ${product?.name || "product"}`}>×</button>
    </header>
    <div className="lineCoreFields">
      <Field label="Purchase unit"><select value={line.unitId} onChange={(e) => { const unit = product?.allowedUnits?.find((item: any) => item.unitId === e.target.value); update(line.lineId, "unitId", e.target.value); update(line.lineId, "unitSymbol", unit?.symbol || product?.baseUnit || "unit"); update(line.lineId, "conversionFactor", Number(unit?.factorToBase || 1)); if (unit?.purchasePrice != null) update(line.lineId, "unitCost", Number(unit.purchasePrice)); }}><option value="">Base unit ({product?.baseUnit || "unit"})</option>{product?.allowedUnits?.filter((unit: any) => ["purchase", "both"].includes(unit.usage)).map((unit: any) => <option key={unit.id || unit.unitId} value={unit.unitId}>{unit.name} ({unit.symbol}) · 1 = {unit.factorToBase} base units</option>)}</select></Field>
      <Field label={mode === "po" ? "Order quantity" : "Received quantity"}><input type="number" min="0.001" step="0.001" value={line.quantity} onChange={(e) => update(line.lineId, "quantity", numberValue(e.target.value))} /></Field>
      <Field label="Free quantity"><input type="number" min="0" step="0.001" value={line.freeQuantity || ""} onChange={(e) => update(line.lineId, "freeQuantity", numberValue(e.target.value))} /></Field>
      <>
        <Field label={product?.trackExpiry ? "Batch number *" : "Batch number"}><input required={product?.trackExpiry} placeholder="e.g. B-2409-A" value={line.batchNo} onChange={(e) => update(line.lineId, "batchNo", e.target.value)} /></Field>
        <Field label="Manufactured date"><input type="date" value={line.manufacturedDate} onChange={(e) => update(line.lineId, "manufacturedDate", e.target.value)} /></Field>
        <Field label={product?.trackExpiry ? "Expiry date *" : "Expiry date"}><input required={product?.trackExpiry} type="date" value={line.expiryDate} onChange={(e) => update(line.lineId, "expiryDate", e.target.value)} /></Field>
      </>
      <div className="lineAmount"><small>Base stock received</small><b>{((line.quantity + line.freeQuantity) * line.conversionFactor).toLocaleString("en-LK")} {product?.baseUnit || "units"}</b><small>Line total</small><strong>{money(line.quantity * line.unitCost - line.discount + line.tax)}</strong></div>
    </div>
    <div className="priceSummary">
      <div><span>Purchase {money(line.unitCost)}</span><span>Retail {money(line.sellingPrice)}</span><span>Wholesale {money(line.wholesalePrice)}</span><span className={margin < 0 ? "negative" : ""}>Margin {margin.toFixed(1)}%</span></div>
      <button type="button" className="secondaryButton" onClick={() => update(line.lineId, "editingPrices", !line.editingPrices)}>{line.editingPrices ? "Close price editor" : "Edit prices & charges"}</button>
    </div>
    {line.editingPrices && <div className="priceEditor">
      <Field label="Purchase price"><input type="number" min="0" step="0.01" value={line.unitCost || ""} onChange={(e) => update(line.lineId, "unitCost", numberValue(e.target.value))} /></Field>
      <Field label="Selling price"><input type="number" min="0" step="0.01" value={line.sellingPrice || ""} onChange={(e) => update(line.lineId, "sellingPrice", numberValue(e.target.value))} /></Field>
      <Field label="Wholesale price"><input type="number" min="0" step="0.01" value={line.wholesalePrice || ""} onChange={(e) => update(line.lineId, "wholesalePrice", numberValue(e.target.value))} /></Field>
      <Field label="Line discount"><input type="number" min="0" step="0.01" value={line.discount || ""} onChange={(e) => update(line.lineId, "discount", numberValue(e.target.value))} /></Field>
      <Field label="Line tax"><input type="number" min="0" step="0.01" value={line.tax || ""} onChange={(e) => update(line.lineId, "tax", numberValue(e.target.value))} /></Field>
      <div className="masterPriceOptions">
        {changedCost && <label><input type="checkbox" checked={line.updatePurchasePrice} onChange={(e) => update(line.lineId, "updatePurchasePrice", e.target.checked)} /> Update the product master purchase price</label>}
        {changedSale && <label><input type="checkbox" checked={line.updateSellingPrice} onChange={(e) => update(line.lineId, "updateSellingPrice", e.target.checked)} /> Update the product master retail &amp; wholesale prices</label>}
        {!changedCost && !changedSale && <small>Prices match the current product defaults.</small>}
      </div>
      {line.sellingPrice < line.unitCost && <p className="priceWarning">Warning: selling price is below purchase price.</p>}
    </div>}
    <footer className="lineActions"><button type="button" onClick={() => duplicate(line)}>+ Add another batch of this product</button><small>Use a unique batch or expiry date for each line.</small></footer>
  </article>;
}

function Records({ tab, rows, action, receive, document, audit, related, edit, pay }: any) {
  return <section className="workflowRecords procurementRecords"><header><div><h2>{tab}</h2><small>Use workflow actions instead of deleting completed documents.</small></div><span>{rows.length} records</span></header><div className="tableScroll"><table><thead><tr><th>Reference</th><th>Supplier / Date</th><th>Total / payment</th><th>Status</th><th>Actions</th></tr></thead><tbody>{rows.length === 0 && <tr><td colSpan={5} className="emptyTable">No records yet.</td></tr>}{rows.map((row: any) => <tr key={row.id}><td><b>{row.number || row.invoice}</b><small>{row.reason || row.notes || "No internal notes"}</small></td><td>{row.supplier}<small>{row.date && new Date(row.date).toLocaleDateString("en-LK")}</small></td><td><b>{money(row.total)}</b>{row.paid!==undefined&&<small>Paid {money(row.paid)} · Due {money(Math.max(0,row.total-row.paid))}</small>}</td><td><span className={`status workflow-${row.status}`}>{String(row.status).replaceAll("_"," ")}</span></td><td><div className="recordActions"><button onClick={() => document(row)}>View / print</button><button onClick={() => audit(row)}>Audit</button>{tab === tabs[0] && <><button onClick={() => related(row)}>Receipt history</button><button disabled={row.status !== "draft"} onClick={() => action(`/purchase-orders/${row.id}/approve`)}>Approve</button><button disabled={!['approved', 'ordered', 'partially_received'].includes(row.status)} onClick={() => receive(row)}>Receive stock</button><button onClick={() => action(`/purchase-orders/${row.id}/duplicate`)}>Duplicate</button><button disabled={["completed", "cancelled"].includes(row.status)} onClick={() => action(`/purchase-orders/${row.id}/cancel`, true)}>Cancel</button></>}{tab === tabs[1] && <><button onClick={() => related(row)}>Payments</button><button disabled={row.status !== "draft"} onClick={() => edit(row)}>Edit draft</button><button disabled={row.status !== "draft"} onClick={() => action(`/procurement/catalog-grns/${row.id}/finalize`)}>Finalize</button><button disabled={row.status !== "draft"} onClick={() => action(`/procurement/catalog-grns/${row.id}/cancel`, true)}>Cancel draft</button><button disabled={row.status !== "finalized" || Number(row.paid) >= Number(row.total)} onClick={() => pay(row)}>Pay balance</button><button disabled={row.status !== "finalized"} onClick={() => action(`/procurement/catalog-grns/${row.id}/reverse`, true)}>Reverse</button></>}{tab === tabs[2] && <><button disabled={row.status !== "draft"} onClick={() => action(`/purchase-returns/${row.id}/finalize`)}>Finalize</button><button disabled={row.status !== "draft"} onClick={() => action(`/purchase-returns/${row.id}/cancel`, true)}>Cancel</button></>}</div></td></tr>)}</tbody></table></div></section>;
}

function ReceivePODialog({ order, accounts, close, saved, fail }: any) {
  const available = (order.items || []).map((item: any) => ({ ...item, remaining: Number(item.quantity) - Number(item.received || 0) - Number(item.reserved || 0) })).filter((item: any) => item.remaining > 0);
  const [lines, setLines] = useState<any[]>(available.map((item: any) => ({ purchaseOrderItemId: item.id, product: item.product, quantity: item.remaining, remaining: item.remaining, orderedQuantity: Number(item.quantity), orderedUnitCost: Number(item.unitCost || 0), unitCost: Number(item.unitCost || 0), sellingPrice: Number(item.sellingPrice || 0), wholesalePrice: Number(item.wholesalePrice || 0), discount: Number(item.discount || 0), tax: Number(item.tax || 0), trackExpiry: Boolean(item.trackExpiry), batchNo: item.batchNo || "", manufacturedDate: item.manufacturedDate || "", expiryDate: item.expiryDate || "", selected: true })));
  const [method, setMethod] = useState("credit"), [account, setAccount] = useState(""), [paid, setPaid] = useState(0), [busy, setBusy] = useState(false);
  const chosen = lines.filter((line) => line.selected && line.quantity > 0);
  const orderedBase = (order.items || []).reduce((sum: number, line: any) => sum + Number(line.quantity) * Number(line.unitCost || 0), 0);
  const receivedWeight = chosen.reduce((sum, line) => sum + line.quantity * line.orderedUnitCost, 0);
  const allocationRatio = orderedBase > 0 ? Math.min(1, receivedWeight / orderedBase) : 1;
  const total = Math.max(0, chosen.reduce((sum, line) => sum + line.quantity * line.unitCost - line.discount * line.quantity / line.orderedQuantity + line.tax * line.quantity / line.orderedQuantity, 0) - Number(order.discount || 0) * allocationRatio + Number(order.tax || 0) * allocationRatio);
  const update = (index: number, key: string, value: any) => setLines((old) => old.map((line, position) => position === index ? { ...line, [key]: value } : line));
  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!chosen.length) { fail("Select at least one PO line to receive."); return; }
    const invalid = chosen.find((line) => line.quantity <= 0 || line.quantity > line.remaining || (line.trackExpiry && (!line.batchNo.trim() || !line.expiryDate)) || (line.manufacturedDate && line.expiryDate && line.manufacturedDate > line.expiryDate) || (line.expiryDate && line.expiryDate < new Date().toISOString().slice(0, 10)));
    if (invalid) { fail(`${invalid.product}: check quantity, batch, manufactured date and expiry date.`); return; }
    if (paid > total) { fail("Paid amount cannot exceed this receipt total."); return; }
    setBusy(true);
    try {
      const items = chosen.map(({ product, remaining, selected, orderedQuantity, orderedUnitCost, discount, tax, trackExpiry, ...line }) => line);
      await apiFetch(`/purchase-orders/${order.id}/convert-grn`, { method: "POST", body: JSON.stringify({ invoiceNo: new FormData(event.currentTarget).get("invoiceNo"), paymentMethod: method, accountId: account, paidAmount: paid, requestId: crypto.randomUUID(), items }) });
      await saved();
    } catch (issue: any) { fail(issue.message); } finally { setBusy(false); }
  };
  return <div className="workflowModal receiveModal" role="dialog" aria-modal="true"><form onSubmit={submit}>
    <div className="receiveHeader"><div><small>PARTIAL RECEIVING</small><h2>Receive purchase order {order.number}</h2><p>{order.supplier} · Only delivered quantities are moved into a draft GRN.</p></div><button type="button" onClick={close}>×</button></div>
    <section className="receiveDocument"><Field label="Supplier invoice number"><input name="invoiceNo" required placeholder="Enter supplier invoice" /></Field><Field label="Payment method"><select value={method} onChange={(event) => { setMethod(event.target.value); if (event.target.value === "credit") { setAccount(""); setPaid(0); } }}><option value="credit">Credit</option><option value="cash">Cash</option><option value="bank">Bank</option></select></Field>{method !== "credit" && <><Field label="Payment account"><select required value={account} onChange={(event) => setAccount(event.target.value)}><option value="">Select account</option>{accounts.filter((item: any) => item.type === method).map((item: any) => <option value={item.id} key={item.id}>{item.name} · {money(item.balance)}</option>)}</select></Field><Field label="Amount paid now"><input type="number" min="0" max={total} step="0.01" value={paid || ""} onChange={(event) => setPaid(numberValue(event.target.value))} /></Field></>}</section>
    <section className="receiveLines"><header><b>Products delivered</b><span>{chosen.length} of {lines.length} lines</span></header>{lines.map((line, index) => <article className={line.selected ? "selected" : ""} key={line.purchaseOrderItemId}><label className="receiveSelect"><input type="checkbox" checked={line.selected} onChange={(event) => update(index, "selected", event.target.checked)} /><span><b>{line.product}</b><small>Available to receive: {line.remaining}</small></span></label><Field label="Receive quantity"><input type="number" min="0.001" max={line.remaining} step="0.001" disabled={!line.selected} value={line.quantity} onChange={(event) => update(index, "quantity", numberValue(event.target.value))} /></Field><Field label="Actual unit cost"><input type="number" min="0" step="0.01" disabled={!line.selected} value={line.unitCost} onChange={(event) => update(index, "unitCost", numberValue(event.target.value))} /></Field><Field label="Retail price"><input type="number" min="0" step="0.01" disabled={!line.selected} value={line.sellingPrice} onChange={(event) => update(index, "sellingPrice", numberValue(event.target.value))} /></Field><Field label="Wholesale price"><input type="number" min="0" step="0.01" disabled={!line.selected} value={line.wholesalePrice} onChange={(event) => update(index, "wholesalePrice", numberValue(event.target.value))} /></Field><Field label={`Batch number${line.trackExpiry ? " *" : ""}`}><input required={line.selected && line.trackExpiry} disabled={!line.selected} value={line.batchNo} onChange={(event) => update(index, "batchNo", event.target.value)} /></Field><Field label="Manufactured"><input type="date" disabled={!line.selected} value={line.manufacturedDate} onChange={(event) => update(index, "manufacturedDate", event.target.value)} /></Field><Field label={`Expiry${line.trackExpiry ? " *" : ""}`}><input type="date" required={line.selected && line.trackExpiry} disabled={!line.selected} value={line.expiryDate} onChange={(event) => update(index, "expiryDate", event.target.value)} /></Field><strong>{money(line.quantity * line.unitCost)}</strong></article>)}</section>
    <footer className="receiveFooter"><div><small>Draft GRN total</small><b>{money(total)}</b><span>Reserved draft quantities are excluded; unreceived quantities remain open.</span></div><button type="button" className="quietButton" onClick={close}>Close</button><button className="new" disabled={busy || !chosen.length}>{busy ? "Creating…" : "Create draft GRN"}</button></footer>
  </form></div>;
}

function ProcurementDocument({data,type,close}:any){const items=data.items||[];return <div className="workflowModal documentModal" role="dialog" aria-modal="true"><section id="procurement-print" className="printDocument"><header><div><BusinessDocumentHeader/><h2>{type==="Purchase orders"?"PURCHASE ORDER":type==="GRN / Purchases"?"GOODS RECEIVED NOTE":"PURCHASE RETURN"}</h2></div><button className="noPrint" onClick={close}>×</button></header><div className="documentIdentity"><div><small>Reference</small><b>{data.number||data.invoiceNo||data.invoice}</b></div><div><small>Supplier</small><b>{data.supplier}</b></div><div><small>Date</small><b>{new Date(data.purchaseDate||data.date).toLocaleDateString("en-LK")}</b></div><div><small>Status</small><b>{String(data.status).replaceAll("_"," ")}</b></div></div>{items.length>0&&<table><thead><tr><th>Product</th><th>Batch / expiry</th><th>Qty</th><th>Unit cost</th><th>Total</th></tr></thead><tbody>{items.map((x:any)=><tr key={x.id}><td>{x.product}</td><td>{x.batchNo||"—"}<small>{x.expiryDate?new Date(x.expiryDate).toLocaleDateString("en-LK"):"No expiry"}</small></td><td>{x.quantity}</td><td>{money(x.unitCost)}</td><td>{money(Number(x.quantity)*Number(x.unitCost))}</td></tr>)}</tbody></table>}<div className="documentTotals"><span>Total <b>{money(data.total)}</b></span>{data.paidAmount!==undefined&&<><span>Paid <b>{money(data.paidAmount)}</b></span><span>Balance <b>{money(data.total-data.paidAmount)}</b></span></>}</div><footer className="noPrint"><button onClick={close}>Close</button><button className="new primary" onClick={()=>window.print()}>Print document</button></footer></section></div>}
function AuditDrawer({data,close}:any){return <div className="auditDrawer"><header><div><small>DOCUMENT HISTORY</small><h2>Audit trail</h2><p>{data.row.number||data.row.invoice}</p></div><button onClick={close}>×</button></header><section>{data.entries.length===0&&<div className="emptyState">No audit events recorded for this document.</div>}{data.entries.map((x:any)=><article key={x.id}><span/><div><b>{String(x.action).replaceAll("_"," ")}</b><small>{x.user} · {new Date(x.at).toLocaleString("en-LK")}</small>{Object.keys(x.new||{}).length>0&&<pre>{JSON.stringify(x.new,null,2)}</pre>}</div></article>)}</section></div>}
function RelatedDrawer({value,close}:any){const receipts=value.type==="receipts"?value.data:[],payments=value.type==="payments"?(value.data.payments||[]):[];return <div className="auditDrawer relatedDrawer"><header><div><small>{value.type==="receipts"?"PO RECEIVING":"GRN SETTLEMENT"}</small><h2>{value.type==="receipts"?"Receipt history":"Payment history"}</h2><p>{value.row.number||value.row.invoice}</p></div><button onClick={close}>×</button></header><section>{value.type==="payments"&&<div className="relatedTotals"><span>Total<b>{money(value.data.total)}</b></span><span>Paid<b>{money(value.data.paid)}</b></span><span>Due<b>{money(value.data.due)}</b></span></div>}{receipts.length===0&&payments.length===0&&<div className="emptyState">No related transactions yet.</div>}{receipts.map((x:any)=><article key={x.id}><span/><div><b>{x.invoice}</b><small>{new Date(x.date).toLocaleDateString("en-LK")} · {String(x.status).replaceAll("_"," ")}</small><p>{x.lineCount} lines · {x.receivedQuantity} units · {money(x.total)}</p><small>Paid {money(x.paid)} · Due {money(x.due)}</small></div></article>)}{payments.map((x:any)=><article key={x.id}><span/><div><b>{money(x.amount)} · {x.method}</b><small>{new Date(x.date).toLocaleDateString("en-LK")} · {x.account||"No account"}</small><p>{x.reference||"No reference"} · {x.user}</p><small className={x.status==="reversed"?"reversedText":""}>{x.status}{x.reason?` — ${x.reason}`:""}</small></div></article>)}</section></div>}
function GRNPaymentDialog({grn,accounts,close,saved,fail}:any){const[method,setMethod]=useState("cash"),[accountId,setAccountId]=useState(""),[amount,setAmount]=useState(Number(grn.dueAmount||0)),[busy,setBusy]=useState(false);const available=accounts.filter((x:any)=>x.type===method);const submit=async(e:React.FormEvent)=>{e.preventDefault();if(amount<=0||amount>Number(grn.dueAmount)){fail("Enter an amount within the GRN outstanding balance.");return}setBusy(true);try{await apiFetch("/supplier-payments",{method:"POST",body:JSON.stringify({partyId:grn.supplierId,purchaseId:grn.id,amount,method,accountId,date:new Date().toISOString().slice(0,10),reference:`Payment for ${grn.invoiceNo}`,notes:"Allocated from procurement",requestId:crypto.randomUUID()})});await saved()}catch(issue:any){fail(issue.message)}finally{setBusy(false)}};return <div className="workflowModal paymentModal" role="dialog" aria-modal="true"><form onSubmit={submit}><div className="formTitle"><div><small>SUPPLIER SETTLEMENT</small><h2>Pay GRN {grn.invoiceNo}</h2><p>{grn.supplier}</p></div><button type="button" onClick={close}>×</button></div><div className="paymentSummary"><span>Document total<b>{money(grn.total)}</b></span><span>Already paid<b>{money(grn.paidAmount)}</b></span><span>Outstanding<b>{money(grn.dueAmount)}</b></span></div><Field label="Payment method"><select value={method} onChange={e=>{setMethod(e.target.value);setAccountId("")}}><option value="cash">Cash</option><option value="bank">Bank</option></select></Field><Field label="Payment account"><select required value={accountId} onChange={e=>setAccountId(e.target.value)}><option value="">Select account</option>{available.map((x:any)=><option key={x.id} value={x.id}>{x.name} · Available {money(x.balance)}</option>)}</select></Field><Field label="Amount"><input required type="number" min="0.01" max={grn.dueAmount} step="0.01" value={amount} onChange={e=>setAmount(numberValue(e.target.value))}/></Field><footer><button type="button" className="quietButton" onClick={close}>Cancel</button><button className="new" disabled={busy||!accountId}>{busy?"Saving…":"Record & allocate payment"}</button></footer></form></div>}
function Field({ label, children }: any) { return <label className="formField"><span>{label}</span>{children}</label>; }

function QuickProduct({ supplierId, close, saved, fail }: any) {
  const [value, setValue] = useState<any>({ name: "", productCode: "", sku: "", barcode: "", purchasePrice: 0, sellingPrice: 0, wholesalePrice: 0, minimumStock: 0, openingStock: 0, taxRate: 0, trackExpiry: true, isActive: true });
  const set = (key: string, next: any) => setValue((current: any) => ({ ...current, [key]: next }));
  const submit = async (event: React.FormEvent) => { event.preventDefault(); try { await apiFetch("/products", { method: "POST", body: JSON.stringify(value) }); const all = await apiFetch("/products"); const product = all.find((item: any) => item.SKU === value.sku); if (!product) throw new Error("Product created but could not be assigned. Refresh and assign it manually."); await apiFetch(`/procurement/suppliers/${supplierId}/products`, { method: "POST", body: JSON.stringify({ productId: product.ID, defaultPurchasePrice: value.purchasePrice, recommendedSellingPrice: value.sellingPrice, wholesalePrice: value.wholesalePrice || 0, minimumOrderQty: 1, packSize: 1, isActive: true }) }); await saved(); } catch (issue: any) { fail(issue.message); } };
  return <div className="workflowModal" role="dialog" aria-modal="true"><form onSubmit={submit}><div className="formTitle"><div><h2>Quick add product</h2><small>Create and assign it to this supplier.</small></div><button type="button" className="quietButton" onClick={close}>Close</button></div><Field label="Product name"><input required value={value.name} onChange={(e) => set("name", e.target.value)} /></Field><Field label="Product code"><input required value={value.productCode} onChange={(e) => set("productCode", e.target.value)} /></Field><Field label="SKU"><input required value={value.sku} onChange={(e) => set("sku", e.target.value)} /></Field><Field label="Barcode"><input value={value.barcode} onChange={(e) => set("barcode", e.target.value)} /></Field><Field label="Purchase price"><input type="number" min="0" step="0.01" value={value.purchasePrice || ""} onChange={(e) => set("purchasePrice", numberValue(e.target.value))} /></Field><Field label="Selling price"><input required type="number" min="0" step="0.01" value={value.sellingPrice || ""} onChange={(e) => set("sellingPrice", numberValue(e.target.value))} /></Field><Field label="Wholesale price"><input type="number" min="0" step="0.01" value={value.wholesalePrice || ""} onChange={(e) => set("wholesalePrice", numberValue(e.target.value))} /></Field><label className="checkLabel"><input type="checkbox" checked={value.trackExpiry} onChange={(e) => set("trackExpiry", e.target.checked)} /> Track expiry for this product</label><button className="new">Create &amp; assign product</button></form></div>;
}

function ReturnForm({ done, fail }: any) {
  const [purchases, setPurchases] = useState<any[]>([]);
  const [search, setSearch] = useState("");
  const [purchaseId, setPurchaseId] = useState("");
  const [purchase, setPurchase] = useState<any>(null);
  const [selected, setSelected] = useState<Record<string, number>>({});
  const [reason, setReason] = useState("");
  const [notes, setNotes] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    apiFetch("/procurement/grns").then((rows: any[]) => setPurchases(rows.filter((row) => row.status === "finalized"))).catch((issue: any) => fail(issue.message)).finally(() => setLoading(false));
  }, []);

  const availablePurchases = useMemo(() => purchases.filter((row) => `${row.invoice} ${row.supplier} ${row.date}`.toLowerCase().includes(search.toLowerCase().trim())), [purchases, search]);
  const returnLines = Object.entries(selected).filter(([, quantity]) => quantity > 0).map(([id, quantity]) => ({ id, quantity }));
  const returnTotal = returnLines.reduce((sum, chosen) => {
    const item = purchase?.items?.find((line: any) => line.id === chosen.id);
    return sum + chosen.quantity * Number(item?.unitCost || 0);
  }, 0);

  const choosePurchase = async (id: string) => {
    setPurchaseId(id); setPurchase(null); setSelected({}); fail("");
    if (!id) return;
    setLoading(true);
    try { setPurchase(await apiFetch(`/procurement/grns/${id}/detail`)); }
    catch (issue: any) { fail(issue.message); setPurchaseId(""); }
    finally { setLoading(false); }
  };
  const toggleLine = (line: any) => setSelected((current) => current[line.id] > 0 ? Object.fromEntries(Object.entries(current).filter(([id]) => id !== line.id)) : { ...current, [line.id]: Math.min(1, Number(line.availableQuantity || 0)) });
  const setQuantity = (line: any, quantity: number) => setSelected((current) => ({ ...current, [line.id]: Math.max(0, Math.min(Number(line.availableQuantity || 0), quantity || 0)) }));

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!purchaseId) { fail("Choose a finalized purchase first."); return; }
    if (!returnLines.length) { fail("Select at least one item and enter the return quantity."); return; }
    if (!reason) { fail("Select a return reason."); return; }
    setBusy(true);
    try {
      const fullReason = notes.trim() ? `${reason}: ${notes.trim()}` : reason;
      await apiFetch("/purchase-returns", { method: "POST", body: JSON.stringify({ purchaseId, reason: fullReason, requestId: crypto.randomUUID(), items: returnLines }) });
      setPurchaseId(""); setPurchase(null); setSelected({}); setReason(""); setNotes("");
      await done();
    } catch (issue: any) { fail(issue.message); }
    finally { setBusy(false); }
  };

  return <form className="workflowForm returnForm returnWorkspace" onSubmit={submit}>
    <div className="formTitle"><div><h2>Create purchase return</h2><small>Find the supplier invoice, select received items and review the stock and payable impact.</small></div><span className="draftBadge">DRAFT</span></div>
    <ol className="purchaseSteps returnSteps" aria-label="Purchase return progress">{["Find purchase", "Select items", "Review & save"].map((label, index) => { const active = index === 0 || (index === 1 && !!purchase) || (index === 2 && returnLines.length > 0); return <li className={active ? "active" : ""} key={label}><span>{index + 1}</span><b>{label}</b></li>; })}</ol>

    <section className="returnSection">
      <div className="sectionHeading"><span>1</span><div><h3>Find the original purchase</h3><small>Search by invoice number, supplier or purchase date.</small></div></div>
      <div className="returnPurchasePicker"><Field label="Search finalized purchases"><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Invoice, supplier or date" /></Field><Field label="Original supplier invoice"><select value={purchaseId} onChange={(event) => choosePurchase(event.target.value)}><option value="">Select a finalized purchase</option>{availablePurchases.map((row) => <option value={row.id} key={row.id}>{row.invoice} · {row.supplier} · {row.date ? new Date(row.date).toLocaleDateString("en-LK") : "No date"}</option>)}</select></Field></div>
      {!loading && purchases.length === 0 && <div className="returnEmpty">No finalized purchases are available for return.</div>}
      {loading && <div className="returnEmpty">Loading purchase information…</div>}
      {purchase && <div className="purchaseSnapshot"><div><small>Supplier</small><b>{purchase.supplier}</b></div><div><small>Invoice</small><b>{purchase.invoiceNo}</b></div><div><small>Purchase date</small><b>{new Date(purchase.purchaseDate).toLocaleDateString("en-LK")}</b></div><div><small>Payment</small><b>{purchase.paymentMethod}</b></div><div><small>Original total</small><b>{money(purchase.total)}</b></div></div>}
    </section>

    <section className={`returnSection ${!purchase ? "isDisabled" : ""}`}>
      <div className="sectionHeading"><span>2</span><div><h3>Select products to return</h3><small>Quantities cannot exceed the amount still available from the original GRN line.</small></div></div>
      {!purchase ? <div className="returnEmpty">Choose a finalized purchase to see its received products.</div> : <div className="returnItems">{purchase.items.map((line: any) => { const quantity = selected[line.id] || 0; const unavailable = Number(line.availableQuantity) <= 0; return <article className={`${quantity > 0 ? "selected" : ""} ${unavailable ? "unavailable" : ""}`} key={line.id}>
        <label className="returnItemSelect"><input type="checkbox" checked={quantity > 0} disabled={unavailable} onChange={() => toggleLine(line)} /><span><b>{line.product}</b><small>{line.batchNo ? `Batch ${line.batchNo}` : "No batch"}{line.expiryDate ? ` · Expires ${new Date(line.expiryDate).toLocaleDateString("en-LK")}` : ""}</small></span></label>
        <div className="returnItemFacts"><span><small>Received</small><b>{line.quantity}</b></span><span><small>Returned</small><b>{line.returnedQuantity}</b></span><span><small>Available</small><b>{line.availableQuantity}</b></span><span><small>Unit cost</small><b>{money(line.unitCost)}</b></span></div>
        <Field label="Return quantity"><input type="number" min="0" max={line.availableQuantity} step="0.001" disabled={unavailable || quantity === 0} value={quantity || ""} onChange={(event) => setQuantity(line, numberValue(event.target.value))} /></Field>
        <strong className="returnLineTotal">{money(quantity * Number(line.unitCost || 0))}</strong>
      </article>; })}</div>}
    </section>

    <section className={`returnSection returnReview ${!returnLines.length ? "isDisabled" : ""}`}>
      <div className="sectionHeading"><span>3</span><div><h3>Reason &amp; impact review</h3><small>Saving creates a draft. Stock and supplier payable change only when the draft is finalized.</small></div></div>
      <div className="returnReviewGrid"><Field label="Return reason *"><select required value={reason} onChange={(event) => setReason(event.target.value)}><option value="">Choose reason</option><option>Damaged on arrival</option><option>Expired or short dated</option><option>Wrong item supplied</option><option>Excess quantity supplied</option><option>Quality issue</option><option>Supplier recall</option><option>Other</option></select></Field><Field label="Notes / supplier reference"><input value={notes} onChange={(event) => setNotes(event.target.value)} placeholder="Optional explanation or reference" /></Field></div>
      <div className="returnImpact"><div><span>Selected items</span><b>{returnLines.length}</b></div><div><span>Stock on finalization</span><b className="impactNegative">− {returnLines.reduce((sum, line) => sum + line.quantity, 0)}</b></div><div><span>Supplier payable</span><b className="impactNegative">− {money(returnTotal)}</b></div><div className="returnGrandTotal"><span>Return total</span><strong>{money(returnTotal)}</strong></div></div>
      <div className="returnActions"><button type="button" className="quietButton" onClick={() => { setSelected({}); setReason(""); setNotes(""); }} disabled={!returnLines.length}>Clear selection</button><button className="new" disabled={busy || !returnLines.length}>{busy ? "Saving…" : "Save return for review"}</button></div>
    </section>
  </form>;
}
