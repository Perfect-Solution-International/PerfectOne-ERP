"use client";
import { useEffect, useState } from "react";
import { apiFetch } from "../lib/api";
import { CURRENCY, amount } from "../lib/format";
const fallback = {
  businessName: "PerfectOne ERP",
  logoUrl: "",
  address: "",
  phone: "",
  email: "",
  businessRegistrationNo: "",
  taxRegistrationNo: "",
  currencySymbol: "LKR",
  receiptTitle: "SALES RECEIPT",
  receiptHeader: "",
  receiptFooter: "Thank you for your business!",
  receiptSize: "80mm",
  receiptCopies: 1,
  showBusinessLogo: true,
  showBusinessAddress: true,
  showBusinessContact: true,
  showTax: true,
  showDiscount: true,
};
export default function ConfiguredReceipt({
  data,
  newSale,
}: {
  data: any;
  newSale: () => void;
}) {
  const [settings, setSettings] = useState<any>(fallback);
  useEffect(() => {
    apiFetch("/settings")
      .then((x) => setSettings({ ...fallback, ...x }))
      .catch(() => {});
  }, []);
  const money = (v: number) => `${settings.currencySymbol || CURRENCY} ${amount(v)}`;
  const qty = (l: any) =>
    l.enteredQuantity
      ? `${l.enteredQuantity}${l.unitSymbol ? ` ${l.unitSymbol}` : ""} × ${money(l.enteredUnitPrice ?? l.price)}`
      : `${l.qty} × ${money(l.price)}`;
  return (
    <div
      id="receipt"
      className={`receiptPaper receipt-${String(settings.receiptSize).toLowerCase()}`}
      data-copies={settings.receiptCopies}
    >
      {data.offline && (
        <div className="offlineReceiptWarning">
          <b>OFFLINE SALE</b>
          <span>Pending synchronization — temporary reference.</span>
        </div>
      )}
      <div className="receiptHeader">
        {settings.showBusinessLogo && settings.logoUrl && (
          <img src={settings.logoUrl} alt="Business logo" />
        )}
        <b>{settings.businessName}</b>
        {settings.businessRegistrationNo && (
          <small>Reg: {settings.businessRegistrationNo}</small>
        )}
        {settings.taxRegistrationNo && (
          <small>Tax: {settings.taxRegistrationNo}</small>
        )}
        {settings.showBusinessAddress && settings.address && (
          <small>{settings.address}</small>
        )}
        {settings.showBusinessContact && (settings.phone || settings.email) && (
          <small>
            {[settings.phone, settings.email].filter(Boolean).join(" · ")}
          </small>
        )}
        {settings.receiptHeader && <p>{settings.receiptHeader}</p>}
        <strong>{settings.receiptTitle}</strong>
        <small>
          {data.offline ? "Temporary reference" : "Invoice"} {data.invoice}
        </small>
        <small>{new Date(data.date).toLocaleString()}</small>
      </div>
      <div className="receiptMeta">
        <span>Cashier: {data.cashier}</span>
        <span>Customer: {data.customer}</span>
      </div>
      <div className="receiptDivider" />
      <div className="receiptItems">
        {data.items.map((l: any, i: number) => (
          <div className="receiptItem" key={`${l.id}-${i}`}>
            <div>
              <b>{l.name}</b>
              <small>
                {qty(l)}
                {l.tareQuantity
                  ? ` · tare ${l.tareQuantity}${l.unitSymbol || ""}`
                  : ""}
                {settings.showDiscount && l.discount > 0
                  ? ` · discount ${money(l.discount)}`
                  : ""}
              </small>
            </div>
            <b>{money(l.qty * (l.effectivePrice ?? l.price) - l.discount)}</b>
          </div>
        ))}
      </div>
      <div className="receiptDivider" />
      <div className="receiptRow">
        <span>Subtotal</span>
        <b>
          {money(
            data.gross ||
              data.total +
                data.discount +
                (data.itemDiscount || 0) +
                (data.promotionDiscount || 0),
          )}
        </b>
      </div>
      {settings.showDiscount && data.promotionDiscount > 0 && (
        <div className="receiptRow">
          <span>Promotions</span>
          <b>-{money(data.promotionDiscount)}</b>
        </div>
      )}
      {settings.showDiscount && data.itemDiscount > 0 && (
        <div className="receiptRow">
          <span>Item discounts</span>
          <b>-{money(data.itemDiscount)}</b>
        </div>
      )}
      {settings.showDiscount && data.discount > 0 && (
        <div className="receiptRow">
          <span>Invoice discount</span>
          <b>-{money(data.discount)}</b>
        </div>
      )}
      {settings.showTax && (
        <div className="receiptRow">
          <span>Tax</span>
          <b>{money(data.tax || 0)}</b>
        </div>
      )}
      <div className="receiptRow total">
        <span>TOTAL</span>
        <b>{money(data.total)}</b>
      </div>
      <div className="receiptPayments">
        {data.payments?.map((p: any, i: number) => (
          <div className="receiptRow" key={`${p.method}-${i}`}>
            <span>
              {p.method.toUpperCase()}
              {p.reference ? ` · ${p.reference}` : ""}
            </span>
            <b>{money(p.amount)}</b>
          </div>
        ))}
      </div>
      <div className="receiptRow">
        <span>Change</span>
        <b>{money(data.change)}</b>
      </div>
      <div className="receiptDivider" />
      <p className="receiptThanks">{settings.receiptFooter}</p>
      <small className="receiptPowered">
        Powered by PerfectOne ERP · v3.0.0
      </small>
      <div className="receiptActions">
        <button onClick={() => window.print()}>
          Print{" "}
          {settings.receiptCopies > 1
            ? `${settings.receiptCopies} copies`
            : "receipt"}
        </button>
        <button onClick={newSale}>New sale</button>
      </div>
    </div>
  );
}
