"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import ActionDialog, {
  type ActionDialogRequest,
} from "../components/ActionDialog";
import Sidebar from "../components/Sidebar";
import ConfiguredReceipt from "../components/ConfiguredReceipt";
import {
  apiFetch,
  cachePOSData,
  cachedPOSData,
  getSession,
  type Session,
} from "../lib/api";
import { useRequireSession } from "../lib/useRequireSession";
import { money } from "../lib/format";

type Register = {
  id: string;
  cashier: string;
  branch: string;
  opening: number;
  cashSales: number;
  cashReturns: number;
  expected: number;
  openedAt: string;
};
type CartLine = {
  id: string;
  name: string;
  price: number;
  effectivePrice: number;
  promotionName?: string;
  promotionDiscount: number;
  qty: number;
  stock: number;
  discount: number;
  enteredQuantity?: number;
  unitId?: string;
  unitSymbol?: string;
  conversionFactor?: number;
  tareQuantity?: number;
  enteredUnitPrice?: number;
};
type HeldSale = {
  id: string;
  reference: string;
  customer: string;
  customerId?: string;
  cart: Array<{
    productId: string;
    quantity: number;
    name: string;
    price: number;
    discount: number;
  }>;
  discount: number;
  heldAt: string;
  cashier?: string;
  owned?: boolean;
};


export default function POS() {
  useRequireSession();
  const [user, setUser] = useState<Session | null>(null);
  const [register, setRegister] = useState<Register | null>(null);
  const [registerReady, setRegisterReady] = useState(false);
  const [products, setProducts] = useState<any[]>([]),
    [customers, setCustomers] = useState<any[]>([]),
    [cart, setCart] = useState<CartLine[]>([]);
  const [customer, setCustomer] = useState(""),
    [discount, setDiscount] = useState(0),
    [paid, setPaid] = useState(0),
    [method, setMethod] = useState("cash");
  const [splitPayment, setSplitPayment] = useState(false),
    [paymentParts, setPaymentParts] = useState({ cash: 0, bank: 0, card: 0 }),
    [holds, setHolds] = useState<HeldSale[]>([]),
    [showHolds, setShowHolds] = useState(false),
    [holding, setHolding] = useState(false);
  const [paymentRefs, setPaymentRefs] = useState({ bank: "", card: "" });
  const [discountApprovalId, setDiscountApprovalId] = useState("");
  const [discountApprovals, setDiscountApprovals] = useState<any[]>([]),
    [showApprovals, setShowApprovals] = useState(false);
  const [message, setMessage] = useState(""),
    [receipt, setReceipt] = useState<any>(null),
    [search, setSearch] = useState("");
  const [checkingOut, setCheckingOut] = useState(false),
    [openingCash, setOpeningCash] = useState(0),
    [openingNotes, setOpeningNotes] = useState(""),
    [opening, setOpening] = useState(false);
  const [showClose, setShowClose] = useState(false),
    [closingCash, setClosingCash] = useState(0),
    [closingNotes, setClosingNotes] = useState(""),
    [closing, setClosing] = useState(false);
  const [actionDialog, setActionDialog] = useState<ActionDialogRequest | null>(
    null,
  );
  const [density, setDensity] = useState<"compact" | "comfortable">("compact");
  const [showCheckout, setShowCheckout] = useState(false);
  const [showShortcutHelp, setShowShortcutHelp] = useState(false);
  const [measure, setMeasure] = useState<any>(null),
    [measureQty, setMeasureQty] = useState(1),
    [measureUnit, setMeasureUnit] = useState<any>(null),
    [measureTare, setMeasureTare] = useState(0);
  const checkoutRequest = useRef("");
  const searchInput = useRef<HTMLInputElement>(null);

  const [online, setOnline] = useState(true);
  useEffect(() => {
    setUser(getSession());
  }, []);
  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    update();
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }, []);
  useEffect(() => {
    apiFetch("/settings")
      .then((value) => {
        if (["cash", "bank", "card"].includes(value.defaultPaymentMethod))
          setMethod(value.defaultPaymentMethod);
      })
      .catch(() => {});
  }, []);
  const loadCatalogue = async () => {
    try {
      const [productRows, customerRows] = await Promise.all([
        apiFetch("/products"),
        apiFetch("/customers"),
      ]);
      setProducts(productRows);
      setCustomers(customerRows);
      cachePOSData({ products: productRows, customers: customerRows });
    } catch (issue) {
      const cached = cachedPOSData();
      if (!cached?.products?.length) throw issue;
      setProducts(cached.products);
      setCustomers(cached.customers || []);
      setMessage(
        `Offline catalogue loaded from ${new Date(cached.cachedAt).toLocaleString()}. Customer and stock values may be stale.`,
      );
    }
  };
  const loadRegister = async () => {
    try {
      const result = await apiFetch("/cashier-sessions/current");
      const current = result.session || null;
      setRegister(current);
      cachePOSData({ register: current });
    } catch (issue) {
      const cached = cachedPOSData();
      if (cached?.register) {
        setRegister(cached.register);
        setMessage(
          "Offline register snapshot loaded. Sales will queue until the connection returns.",
        );
      } else throw issue;
    } finally {
      setRegisterReady(true);
    }
  };
  const loadHolds = async () => {
    try {
      setHolds(await apiFetch("/pos/holds"));
    } catch {
      /* available after migration 044 */
    }
  };
  const loadDiscountApprovals = async () => {
    try {
      setDiscountApprovals(
        await apiFetch("/pos/discount-approvals?status=pending"),
      );
    } catch {
      /* available after migration 049 */
    }
  };
  useEffect(() => {
    Promise.all([
      loadCatalogue(),
      loadRegister(),
      loadHolds(),
      loadDiscountApprovals(),
    ]).catch((error: Error) => setMessage(error.message));
  }, []);

  const subtotal = cart.reduce((sum, line) => sum + line.price * line.qty, 0),
    promotionDiscount = cart.reduce(
      (sum, line) => sum + line.promotionDiscount * line.qty,
      0,
    ),
    itemDiscount = cart.reduce((sum, line) => sum + line.discount, 0),
    gross = Math.max(0, subtotal - promotionDiscount - itemDiscount),
    total = Math.max(0, gross - discount);
  useEffect(() => {
    if (method !== "cash") setPaid(total);
  }, [method, total]);
  useEffect(() => {
    if (splitPayment)
      setPaymentParts((parts) => ({
        ...parts,
        cash: Math.max(0, total - parts.bank - parts.card),
      }));
  }, [splitPayment, total]);
  const filtered = products.filter((product: any) => {
    const needle = search.trim().toLowerCase();
    return (
      !needle ||
      product.Name.toLowerCase().includes(needle) ||
      (product.SKU || "").toLowerCase().includes(needle) ||
      (product.Barcode || "").includes(needle) ||
      (product.PLUCode || "").toLowerCase().includes(needle) ||
      product.AllowedUnits?.some((unit: any) =>
        (unit.barcode || "").toLowerCase().includes(needle),
      )
    );
  });
  const add = (product: any) => {
    if (
      (product.MeasurementType && product.MeasurementType !== "count") ||
      product.AllowedUnits?.length
    ) {
      const scannedUnit = product.AllowedUnits?.find(
        (u: any) => u.barcode && u.barcode === search.trim(),
      );
      const preferred = scannedUnit || product.AllowedUnits?.find(
        (u: any) => u.isDefaultSale,
      ) ||
        product.AllowedUnits?.find((u: any) =>
          ["sale", "both"].includes(u.usage),
        ) || {
          unitId: "",
          symbol: product.Unit,
          factorToBase: 1,
          salePrice: product.Price,
        };
      setMeasure(product);
      setMeasureUnit(preferred);
      setMeasureQty(product.MinimumSaleQuantity || 1);
      setMeasureTare(product.TareWeight || 0);
      return;
    }
    setCart((current) => {
      const existing = current.find((line) => line.id === product.ID);
      return existing
        ? current.map((line) =>
            line.id === product.ID
              ? { ...line, qty: Math.min(line.stock, line.qty + 1) }
              : line,
          )
        : [
            ...current,
            {
              id: product.ID,
              name: product.Name,
              price: product.Price,
              effectivePrice: product.EffectivePrice ?? product.Price,
              promotionName: product.PromotionName,
              promotionDiscount: product.PromotionDiscount || 0,
              qty: 1,
              stock: product.Stock,
              discount: 0,
            },
          ];
    });
  };
  const addMeasured = () => {
    if (!measure || !measureUnit) return;
    const factor = Number(measureUnit.factorToBase || 1),
      baseQty = (Number(measureQty) - Number(measureTare || 0)) * factor;
    if (baseQty <= 0 || baseQty > measure.Stock) {
      setMessage("Enter a valid net quantity within available stock.");
      return;
    }
    const enteredPrice = Number(
        measureUnit.salePrice ?? measure.Price * factor,
      ),
      basePrice = enteredPrice / factor;
    setCart((current) => [
      ...current,
      {
        id: measure.ID,
        name: measure.Name,
        price: basePrice,
        effectivePrice: basePrice,
        promotionDiscount: 0,
        qty: baseQty,
        stock: measure.Stock,
        discount: 0,
        enteredQuantity: Number(measureQty),
        unitId: measureUnit.unitId || "",
        unitSymbol: measureUnit.symbol || measure.Unit,
        conversionFactor: factor,
        tareQuantity: Number(measureTare || 0),
        enteredUnitPrice: enteredPrice,
      },
    ]);
    setMeasure(null);
  };
  const setLineDiscount = (id: string, value: number) =>
    setCart((current) =>
      current.map((line) =>
        line.id === id
          ? {
              ...line,
              discount: Math.max(
                0,
                Math.min(value, line.effectivePrice * line.qty),
              ),
            }
          : line,
      ),
    );
  const increment = (id: string) =>
    setCart((current) =>
      current.map((line) =>
        line.id === id
          ? { ...line, qty: Math.min(line.stock, line.qty + 1) }
          : line,
      ),
    );
  const decrement = (id: string) =>
    setCart((current) =>
      current
        .map((line) =>
          line.id === id
            ? {
                ...line,
                qty: line.qty - 1,
                discount: Math.min(
                  line.discount,
                  line.effectivePrice * (line.qty - 1),
                ),
              }
            : line,
        )
        .filter((line) => line.qty > 0),
    );
  const removeLine = (id: string) =>
    setCart((current) => current.filter((line) => line.id !== id));
  const quickAmounts = Array.from(
    new Set(
      [
        total,
        Math.ceil(total / 100) * 100,
        Math.ceil(total / 500) * 500,
        Math.ceil(total / 1000) * 1000,
      ].filter((value) => value > 0),
    ),
  )
    .sort((a, b) => a - b)
    .slice(0, 4);

  const openRegister = async (event: React.FormEvent) => {
    event.preventDefault();
    if (opening) return;
    setOpening(true);
    setMessage("");
    try {
      await apiFetch("/cashier-sessions/open", {
        method: "POST",
        body: JSON.stringify({ openingCash, notes: openingNotes }),
      });
      await loadRegister();
      setOpeningNotes("");
    } catch (error: any) {
      setMessage(error.message);
    } finally {
      setOpening(false);
    }
  };
  const closeRegister = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!register || closing) return;
    setClosing(true);
    setMessage("");
    try {
      const result = await apiFetch(`/cashier-sessions/${register.id}/close`, {
        method: "POST",
        body: JSON.stringify({ closingCash, notes: closingNotes }),
      });
      setMessage(
        `Session closed. ${result.variance === 0 ? "Cash balanced exactly." : `Cash variance: ${money(result.variance)}.`}`,
      );
      setRegister(null);
      cachePOSData({ register: null });
      setShowClose(false);
      setClosingCash(0);
      setClosingNotes("");
      setReceipt(null);
    } catch (error: any) {
      setMessage(error.message);
    } finally {
      setClosing(false);
    }
  };
  const effectivePaid = splitPayment
    ? paymentParts.cash + paymentParts.bank + paymentParts.card
    : Math.min(paid, total);
  const tenderedPaid = splitPayment ? effectivePaid : paid;
  const holdCurrent = async () => {
    if (!register || !cart.length || holding) return;
    setHolding(true);
    setMessage("");
    try {
      const result = await apiFetch("/pos/holds", {
        method: "POST",
        body: JSON.stringify({
          customerId: customer,
          discount,
          requestId: crypto.randomUUID(),
          cart: cart.map((line) => ({
            productId: line.id,
            quantity: line.qty,
            name: line.name,
            price: line.price,
            discount: line.discount,
          })),
        }),
      });
      setCart([]);
      setCustomer("");
      setDiscount(0);
      setPaid(0);
      setPaymentParts({ cash: 0, bank: 0, card: 0 });
      setMessage(`${result.reference} held successfully.`);
      await loadHolds();
    } catch (error: any) {
      setMessage(error.message);
    } finally {
      setHolding(false);
    }
  };
  const resumeHold = async (hold: HeldSale) => {
    try {
      const result = await apiFetch(`/pos/holds/${hold.id}/resume`, {
        method: "POST",
      });
      const catalogue = new Map(products.map((p: any) => [p.ID, p]));
      setCart(
        result.cart.map((line: any) => {
          const live: any = catalogue.get(line.productId);
          const price = live?.Price ?? line.price;
          return {
            id: line.productId,
            name: live?.Name || line.name,
            price,
            effectivePrice: live?.EffectivePrice ?? price,
            promotionName: live?.PromotionName,
            promotionDiscount: live?.PromotionDiscount || 0,
            qty: line.quantity,
            stock: live?.Stock ?? 0,
            discount: line.discount || 0,
          };
        }),
      );
      setCustomer(result.customerId || "");
      setDiscount(result.discount || 0);
      setShowHolds(false);
      await loadHolds();
      setMessage(
        `${result.reference} resumed. Prices, promotions and stock were refreshed.`,
      );
    } catch (error: any) {
      setMessage(error.message);
    }
  };
  const cancelHold = (hold: HeldSale) =>
    setActionDialog({
      title: "Cancel held sale",
      message: `Cancel ${hold.reference}? The held cart will no longer be available to resume.`,
      confirmLabel: "Cancel held sale",
      tone: "danger",
      onConfirm: async () => {
        await apiFetch(`/pos/holds/${hold.id}/cancel`, { method: "POST" });
        setMessage(`${hold.reference} cancelled.`);
        await loadHolds();
      },
    });
  const checkout = async () => {
    if (checkingOut || !register) return;
    setCheckingOut(true);
    setMessage("");
    if (!checkoutRequest.current) checkoutRequest.current = crypto.randomUUID();
    const requestId = checkoutRequest.current;
    const saleItems = [...cart];
    const payments = splitPayment
      ? Object.entries(paymentParts)
          .filter(([, amount]) => amount > 0)
          .map(([paymentMethod, amount]) => ({
            method: paymentMethod,
            amount,
            tendered: amount,
            reference:
              paymentMethod === "cash"
                ? ""
                : paymentRefs[paymentMethod as "bank" | "card"],
          }))
      : [
          {
            method,
            amount: Math.min(paid, total),
            tendered: paid,
            reference:
              method === "cash" ? "" : paymentRefs[method as "bank" | "card"],
          },
        ];
    try {
      const result = await apiFetch("/pos/checkout", {
        method: "POST",
        body: JSON.stringify({
          customerId: customer,
          lines: cart.map((line) => ({
            productId: line.id,
            quantity: line.qty,
            enteredQuantity: line.enteredQuantity || line.qty,
            unitId: line.unitId || "",
            tareQuantity: line.tareQuantity || 0,
            discount: line.discount,
          })),
          discount,
          paidAmount: effectivePaid,
          paymentMethod: method,
          payments,
          requestId,
        }),
      });
      setReceipt({
        invoice: result.invoice,
        date: new Date().toISOString(),
        cashier: user?.Name || "",
        customer:
          customers.find((item) => item.id === customer)?.name ||
          "Walk-in customer",
        items: cart,
        discount,
        itemDiscount: result.itemDiscount,
        promotionDiscount: result.promotionDiscount,
        gross: result.gross,
        total: result.total,
        paid: result.paid,
        change: result.change,
        method: result.paymentMethod,
        payments: result.payments,
      });
      checkoutRequest.current = "";
      setCart([]);
      setDiscount(0);
      setPaid(0);
      setPaymentParts({ cash: 0, bank: 0, card: 0 });
      setPaymentRefs({ bank: "", card: "" });
      setSplitPayment(false);
      setShowCheckout(false);
      await Promise.all([loadCatalogue(), loadRegister(), loadHolds()]);
    } catch (error: any) {
      if (error.offlineQueued) {
        const nextProducts = products.map((product: any) => {
          const line = saleItems.find((item) => item.id === product.ID);
          return line
            ? {
                ...product,
                Stock: Math.max(0, Number(product.Stock) - line.qty),
              }
            : product;
        });
        const cashReceived = payments
          .filter((part) => part.method === "cash")
          .reduce((sum, part) => sum + Number(part.amount), 0);
        const nextRegister = {
          ...register,
          expected: Number(register.expected) + cashReceived,
        };
        setProducts(nextProducts);
        setRegister(nextRegister);
        cachePOSData({ products: nextProducts, register: nextRegister });
        setReceipt({
          invoice: `OFFLINE-${requestId.slice(0, 8).toUpperCase()}`,
          offline: true,
          date: new Date().toISOString(),
          cashier: user?.Name || "",
          customer:
            customers.find((item) => item.id === customer)?.name ||
            "Walk-in customer",
          items: saleItems,
          discount,
          itemDiscount,
          promotionDiscount,
          gross: subtotal,
          total,
          paid: effectivePaid,
          change: Math.max(0, tenderedPaid - total),
          method,
          payments,
        });
        checkoutRequest.current = "";
        setCart([]);
        setDiscount(0);
        setPaid(0);
        setPaymentParts({ cash: 0, bank: 0, card: 0 });
        setPaymentRefs({ bank: "", card: "" });
        setSplitPayment(false);
        setShowCheckout(false);
        setMessage(
          "Sale saved offline. It will synchronize automatically when the connection returns.",
        );
      } else setMessage(error.message);
    } finally {
      setCheckingOut(false);
    }
  };
  const requestDiscountApproval = () => {
    if (!cart.length || itemDiscount + discount <= 0) return;
    setActionDialog({
      title: "Request discount approval",
      message: `Request manager approval for a ${money(itemDiscount + discount)} discount on this sale.`,
      confirmLabel: "Send request",
      reasonRequired: true,
      reasonLabel: "Business reason",
      onConfirm: async (reason) => {
        const result = await apiFetch("/pos/discount-approvals", {
          method: "POST",
          body: JSON.stringify({
            gross: subtotal,
            itemDiscount,
            invoiceDiscount: discount,
            reason,
          }),
        });
        setDiscountApprovalId(result.id);
        setMessage(
          "Discount approval requested. Ask a manager to approve it before checkout.",
        );
      },
    });
  };
  const reviewDiscount = async (id: string, action: "approve" | "reject") => {
    try {
      await apiFetch(`/pos/discount-approvals/${id}/${action}`, {
        method: "POST",
        body: JSON.stringify({ note: "Reviewed at POS" }),
      });
      await loadDiscountApprovals();
      setMessage(`Discount ${action}d.`);
    } catch (error: any) {
      setMessage(error.message);
    }
  };
  const toggleFullscreen = () =>
    document.fullscreenElement
      ? document.exitFullscreen()
      : document.documentElement.requestFullscreen();
  const canCheckout =
    !!register &&
    !!cart.length &&
    discount <= gross &&
    effectivePaid <= total + 0.01 &&
    (!customer ? Math.abs(effectivePaid - total) < 0.01 : true) &&
    !checkingOut;

  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      const editing = ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
      if (event.key === "Escape") {
        setShowShortcutHelp(false);
        setShowCheckout(false);
        return;
      }
      if (event.ctrlKey && event.key.toLowerCase() === "h") {
        event.preventDefault();
        setShowShortcutHelp((value) => !value);
        return;
      }
      if (event.altKey && event.key.toLowerCase() === "p") {
        event.preventDefault();
        if (receipt) window.print();
        return;
      }
      if (event.ctrlKey && event.key === "Enter") {
        event.preventDefault();
        if (showCheckout && canCheckout) void checkout();
        return;
      }
      if (editing && !event.key.startsWith("F")) return;
      const openPayment = (paymentMethod?: string) => {
        if (!cart.length || !register) return;
        if (paymentMethod) {
          setSplitPayment(false);
          setMethod(paymentMethod);
        }
        setShowCheckout(true);
      };
      switch (event.key) {
        case "F2": event.preventDefault(); searchInput.current?.focus(); searchInput.current?.select(); break;
        case "F3": event.preventDefault(); document.querySelector<HTMLSelectElement>(".posCustomerPicker select")?.focus(); break;
        case "F4": event.preventDefault(); event.shiftKey ? setShowHolds(true) : void holdCurrent(); break;
        case "F6": event.preventDefault(); openPayment(); setTimeout(() => document.querySelector<HTMLInputElement>("#checkout-discount")?.focus(), 0); break;
        case "F7": event.preventDefault(); openPayment(); break;
        case "F8": event.preventDefault(); openPayment("cash"); break;
        case "F9": event.preventDefault(); openPayment("bank"); break;
        case "F10": event.preventDefault(); openPayment("card"); break;
        case "F11": event.preventDefault(); openPayment(); setSplitPayment((value) => !value); break;
        case "Delete": if (!editing && cart.length) { event.preventDefault(); removeLine(cart[cart.length - 1].id); } break;
        case "+": if (!editing && cart.length) { event.preventDefault(); increment(cart[cart.length - 1].id); } break;
        case "-": if (!editing && cart.length) { event.preventDefault(); decrement(cart[cart.length - 1].id); } break;
      }
    };
    window.addEventListener("keydown", keyboard);
    return () => window.removeEventListener("keydown", keyboard);
  }, [cart, register, receipt, showCheckout, canCheckout, checkingOut]);

  return (
    <main className="shell">
      <Sidebar active="New sale" />
      <section className="posTerminal">
        <div className="posSaleActions posTopActions">
          <button
            type="button"
            disabled={!cart.length || holding}
            onClick={holdCurrent}
          >
            {holding ? "Holding…" : "Hold sale"}
          </button>
          <button type="button" onClick={() => setShowHolds(!showHolds)}>
            Held sales <b>{holds.length}</b>
          </button>
          <button
            type="button"
            disabled={!cart.length || itemDiscount + discount <= 0}
            onClick={requestDiscountApproval}
          >
            {discountApprovalId
              ? "Approval requested"
              : "Request discount approval"}
          </button>
          <button
            type="button"
            onClick={() => {
              setShowApprovals(!showApprovals);
              void loadDiscountApprovals();
            }}
          >
            Manager approvals <b>{discountApprovals.length}</b>
          </button>
        </div>
        {showApprovals && (
          <div className="heldSaleList">
            {discountApprovals.length ? (
              discountApprovals.map((item: any) => (
                <article key={item.id}>
                  <div>
                    <b>{item.user}</b>
                    <span>
                      {money(item.itemDiscount + item.invoiceDiscount)} discount
                      · sale {money(item.gross)}
                    </span>
                    <small>{item.reason}</small>
                  </div>
                  <button
                    type="button"
                    onClick={() => reviewDiscount(item.id, "approve")}
                  >
                    Approve
                  </button>
                  <button
                    className="cancelHeld"
                    type="button"
                    onClick={() => reviewDiscount(item.id, "reject")}
                  >
                    Reject
                  </button>
                </article>
              ))
            ) : (
              <small>No pending discount approvals.</small>
            )}
          </div>
        )}
        <header className="posHeader">
          <div className="posBrandBlock">
            <img
              className="posLogo"
              src="/perfectone-mark.png"
              alt="PerfectOne ERP"
            />
            <div>
              <b>PerfectOne POS</b>
            </div>
          </div>
          <div className="posOnlineStatus">
            <span className={`posOnlineDot ${online ? "" : "offline"}`} />
            <span>{online ? "Online · Synced" : "Offline · Queuing"}</span>
          </div>
          <div className="posHeaderRight">
            {register && (
              <button
                className="registerStatus"
                onClick={() => {
                  if (cart.length) {
                    setMessage(
                      "Complete or clear the current sale before closing the session.",
                    );
                    return;
                  }
                  setClosingCash(register.expected);
                  setShowClose(true);
                }}
              >
                <span>Register open</span>
                <b>{money(register.expected)}</b>
              </button>
            )}
            <div className="posCashierBadge">
              <b>{user?.Name}</b>
              <small>
                {register?.branch || user?.Role?.replaceAll("_", " ")}
              </small>
            </div>
            <button
              className="posIconBtn"
              onClick={toggleFullscreen}
              title="Fullscreen"
              aria-label="Toggle fullscreen"
            >
              ⛶
            </button>
          </div>
        </header>
        {message && (
          <button
            className="posNotice"
            type="button"
            onClick={() => setMessage("")}
          >
            <span>{message}</span>
            <b>×</b>
          </button>
        )}
        <section className="posCheckout">
          <article>
            <div className="posScanRow">
              <label className="posCustomerPicker">
                <span>Customer</span>
                <select
                  value={customer}
                  onChange={(event) => setCustomer(event.target.value)}
                >
                  <option value="">Walk-in customer</option>
                  {customers.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>
              <div className="posSearchBar">
                <span>⌕</span>
                <input
                  ref={searchInput}
                  placeholder="Scan barcode or search products"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  autoFocus
                />
              </div>
            </div>
            <div className="productGrid">
              {filtered.map((product) => (
                <button
                  key={product.ID}
                  className="posProductCard"
                  disabled={product.Stock <= 0 || !register}
                  onClick={() => add(product)}
                >
                  {product.PromotionID && <em className="promoBadge">OFFER</em>}
                  <span className="posProductAvatar">{product.Name[0]}</span>
                  <div className="posProductInfo">
                    <b>{product.Name}</b>
                    <small className="posProductSku">
                      {product.SKU ? `#${product.SKU}` : "—"}
                    </small>
                    <small
                      className={
                        product.Stock <= product.MinimumStock
                          ? "stockBadge low"
                          : "stockBadge"
                      }
                    >
                      {product.Stock <= 0
                        ? "Out of stock"
                        : `${product.Stock} in stock`}
                    </small>
                  </div>
                  <span className="posProductPrice">
                    {product.PromotionID ? (
                      <span className="promoPrice">
                        <s>{money(product.Price)}</s>
                        <strong>{money(product.EffectivePrice)}</strong>
                      </span>
                    ) : (
                      <strong>{money(product.Price)}</strong>
                    )}
                    <span className="posProductAdd" aria-hidden="true">
                      +
                    </span>
                  </span>
                </button>
              ))}
              {!filtered.length && (
                <div className="empty">
                  <span className="emptyIcon">
                    <Icon name="box" />
                  </span>
                  <b>No products match</b>
                  <small>Try a different name, SKU or barcode.</small>
                </div>
              )}
            </div>
          </article>
          <article className="checkoutCart">
            {receipt ? (
              <ConfiguredReceipt
                data={receipt}
                newSale={() => {
                  setReceipt(null);
                  setMessage("");
                }}
              />
            ) : (
              <>
                <h2>
                  Current sale{" "}
                  <span className="cartCountBadge">
                    {cart.length} item{cart.length === 1 ? "" : "s"}
                  </span>
                  <div className="cartDensity">
                    <button
                      type="button"
                      className={density === "compact" ? "active" : ""}
                      onClick={() => setDensity("compact")}
                    >
                      Compact
                    </button>
                    <button
                      type="button"
                      className={density === "comfortable" ? "active" : ""}
                      onClick={() => setDensity("comfortable")}
                    >
                      Comfort
                    </button>
                  </div>
                </h2>
                {cart.length ? (
                  <div className={`cartLines density-${density}`}>
                    {cart.map((line, lineIndex) => (
                      <div className="cartLine" key={`${line.id}-${lineIndex}`}>
                        <div className="cartLineInfo">
                          <b>{line.name}</b>
                          <small>
                            {line.enteredQuantity
                              ? `${line.enteredQuantity} ${line.unitSymbol || ""} × ${money(line.enteredUnitPrice || line.price)}`
                              : `${money(line.effectivePrice)} each`}
                            {line.tareQuantity
                              ? ` · tare ${line.tareQuantity} ${line.unitSymbol || ""}`
                              : ""}
                          </small>
                          {line.promotionName && (
                            <em className="cartPromo">
                              {line.promotionName} · save{" "}
                              {money(line.promotionDiscount * line.qty)}
                            </em>
                          )}
                          <label className="lineDiscount">
                            Extra item discount
                            <input
                              aria-label={`Discount for ${line.name}`}
                              type="number"
                              min="0"
                              max={line.effectivePrice * line.qty}
                              step="0.01"
                              value={line.discount || ""}
                              onChange={(event) =>
                                setLineDiscount(
                                  line.id,
                                  Number(event.target.value),
                                )
                              }
                            />
                          </label>
                        </div>
                        {!line.enteredQuantity && <div className="qtyStepper">
                          <button
                            type="button"
                            onClick={() => decrement(line.id)}
                          >
                            −
                          </button>
                          <span>{line.qty}</span>
                          <button
                            type="button"
                            disabled={line.qty >= line.stock}
                            onClick={() => increment(line.id)}
                          >
                            +
                          </button>
                        </div>}
                        <b className="cartLineTotal">
                          {money(
                            line.qty * line.effectivePrice - line.discount,
                          )}
                        </b>
                        <button
                          type="button"
                          className="removeLine"
                          onClick={() => removeLine(line.id)}
                        >
                          ×
                        </button>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="empty">
                    <span className="emptyIcon">
                      <Icon name="cart" />
                    </span>
                    <b>Cart is empty</b>
                    <small>
                      {register
                        ? "Select a product to begin."
                        : "Open the register to begin selling."}
                    </small>
                  </div>
                )}
                <label>
                  Invoice discount <small>Limited by your role</small>
                  <input
                    type="number"
                    min="0"
                    max={gross}
                    value={discount || ""}
                    onChange={(event) =>
                      setDiscount(Number(event.target.value))
                    }
                  />
                </label>
                <div className="posSaleActions cartHoldActions">
                  <button
                    type="button"
                    disabled={!cart.length || holding}
                    onClick={holdCurrent}
                  >
                    {holding ? "Holding…" : "Hold sale"}
                  </button>
                  <button
                    type="button"
                    onClick={() => setShowHolds(!showHolds)}
                  >
                    Held sales <b>{holds.length}</b>
                  </button>
                </div>
                {showHolds && (
                  <div className="heldSaleList">
                    {holds.length ? (
                      holds.map((hold) => (
                        <article key={hold.id}>
                          <button
                            type="button"
                            disabled={!hold.owned}
                            onClick={() => resumeHold(hold)}
                          >
                            <b>{hold.reference}</b>
                            <span>
                              {hold.customer} · {hold.cart.length} lines
                            </span>
                            <small>
                              {hold.cashier || "Current cashier"} ·{" "}
                              {new Date(hold.heldAt).toLocaleString()}
                            </small>
                          </button>
                          <button
                            className="cancelHeld"
                            type="button"
                            onClick={() => cancelHold(hold)}
                          >
                            Cancel
                          </button>
                        </article>
                      ))
                    ) : (
                      <small>No held sales in this register.</small>
                    )}
                  </div>
                )}
                <label className="splitToggle">
                  <input
                    type="checkbox"
                    checked={splitPayment}
                    onChange={(event) => setSplitPayment(event.target.checked)}
                  />{" "}
                  Split payment
                </label>
                {splitPayment ? (
                  <div className="splitPayments">
                    {(["cash", "bank", "card"] as const).map((key) => (
                      <label key={key}>
                        {key[0].toUpperCase() + key.slice(1)}
                        <input
                          type="number"
                          min="0"
                          step="0.01"
                          value={paymentParts[key] || ""}
                          onChange={(event) =>
                            setPaymentParts({
                              ...paymentParts,
                              [key]: Number(event.target.value),
                            })
                          }
                        />
                        {key !== "cash" && paymentParts[key] > 0 && (
                          <input
                            className="paymentReference"
                            value={paymentRefs[key]}
                            onChange={(event) =>
                              setPaymentRefs({
                                ...paymentRefs,
                                [key]: event.target.value,
                              })
                            }
                            placeholder={`${key} reference`}
                          />
                        )}
                      </label>
                    ))}
                  </div>
                ) : (
                  <>
                    <label>
                      Payment method
                      <div className="paymentMethods">
                        {[
                          ["cash", "Cash"],
                          ["bank", "Bank"],
                          ["card", "Card"],
                        ].map(([value, label]) => (
                          <button
                            type="button"
                            key={value}
                            className={method === value ? "active" : ""}
                            onClick={() => setMethod(value)}
                          >
                            {label}
                          </button>
                        ))}
                      </div>
                    </label>
                    <label>
                      Paid amount
                      <input
                        type="number"
                        min="0"
                        value={paid || ""}
                        readOnly={method !== "cash"}
                        onChange={(event) =>
                          setPaid(Number(event.target.value))
                        }
                      />
                    </label>
                    {method !== "cash" && (
                      <label>
                        Payment reference <small>Optional</small>
                        <input
                          value={paymentRefs[method as "bank" | "card"]}
                          onChange={(event) =>
                            setPaymentRefs({
                              ...paymentRefs,
                              [method]: event.target.value,
                            })
                          }
                          placeholder="Transaction or approval reference"
                        />
                      </label>
                    )}
                    {method === "cash" && quickAmounts.length > 0 && (
                      <div className="quickCash">
                        {quickAmounts.map((amount) => (
                          <button
                            type="button"
                            key={amount}
                            onClick={() => setPaid(amount)}
                          >
                            {money(amount)}
                          </button>
                        ))}
                      </div>
                    )}
                  </>
                )}
                <div className="posFooter">
                  <div className="totalRow">
                    <span>Total</span>
                    <b>{money(total)}</b>
                  </div>
                  <div
                    className={`changeDisplay ${effectivePaid >= total ? "ok" : "warn"}`}
                  >
                    <span>
                      {effectivePaid >= total ? "Change due" : "Amount short"}
                    </span>
                    <b>
                      {money(
                        effectivePaid >= total
                          ? Math.max(0, tenderedPaid - total)
                          : total - effectivePaid,
                      )}
                    </b>
                  </div>
                  <button
                    className="payButton"
                    onClick={() => { setSplitPayment(false); setShowCheckout(true); }}
                    disabled={!register || !cart.length}
                  >
                    {checkingOut
                      ? "Processing…"
                      : register
                        ? "Checkout  F7"
                        : "Open register to sell"}
                  </button>
                </div>
              </>
            )}
          </article>
        </section>
        {showCheckout && !receipt && (
          <div className="transactionDialog posPaymentDialog" role="dialog" aria-modal="true" aria-labelledby="checkout-title">
            <form onSubmit={(event) => { event.preventDefault(); if (canCheckout) void checkout(); }}>
              <header><div><small>SALE CHECKOUT</small><h2 id="checkout-title">Review and take payment</h2></div><button type="button" onClick={() => setShowCheckout(false)} aria-label="Close checkout">×</button></header>
              <div className="posCheckoutLayout">
                <aside className="checkoutOrderReview"><div className="checkoutReviewTitle"><span>ORDER SUMMARY</span><b>{cart.length} line{cart.length===1?"":"s"}</b></div><div className="checkoutReviewLines">{cart.map((line,index)=><article key={`${line.id}-${index}`}><div><b>{line.name}</b><small>{line.enteredQuantity?`${line.enteredQuantity} ${line.unitSymbol||""}`:`${line.qty} × ${money(line.effectivePrice)}`}</small></div><strong>{money(line.qty*line.effectivePrice-line.discount)}</strong></article>)}</div><div className="checkoutReviewTotal"><span>Cart total</span><b>{money(total)}</b></div></aside>
                <section className="checkoutPaymentPane">
              <div className="checkoutModalSummary"><span>{cart.length} cart lines</span><b>{money(total)}</b></div>
              <label>Invoice discount <small>F6 · Limited by your role</small><input id="checkout-discount" type="number" min="0" max={gross} value={discount || ""} onChange={(event) => setDiscount(Number(event.target.value))}/></label>
              <label className={`splitToggle checkoutSplitToggle ${splitPayment ? "active" : ""}`}>
                <input type="checkbox" checked={splitPayment} onChange={(event) => setSplitPayment(event.target.checked)}/>
                <span className="checkoutCheck" aria-hidden="true">{splitPayment ? "✓" : ""}</span>
                <span>Split payment</span>
                <small>F11</small>
              </label>
              {splitPayment ? <div className="splitPayments">{(["cash","bank","card"] as const).map((key) => <label key={key}>{key[0].toUpperCase()+key.slice(1)}<input type="number" min="0" step="0.01" value={paymentParts[key] || ""} onChange={(event) => setPaymentParts({...paymentParts,[key]:Number(event.target.value)})}/>{key!=="cash"&&paymentParts[key]>0&&<input className="paymentReference" value={paymentRefs[key]} onChange={(event)=>setPaymentRefs({...paymentRefs,[key]:event.target.value})} placeholder={`${key} reference`}/>}</label>)}</div> : <>
                <label>Payment method <small>F8 Cash · F9 Bank · F10 Card</small><div className="paymentMethods">{[["cash","Cash"],["bank","Bank"],["card","Card"]].map(([value,label])=><button type="button" key={value} className={method===value?"active":""} onClick={()=>setMethod(value)}>{label}</button>)}</div></label>
                <label>Paid amount<input autoFocus type="number" min="0" value={paid || ""} readOnly={method!=="cash"} onChange={(event)=>setPaid(Number(event.target.value))}/></label>
                {method!=="cash"&&<label>Payment reference <small>Optional</small><input value={paymentRefs[method as "bank"|"card"]} onChange={(event)=>setPaymentRefs({...paymentRefs,[method]:event.target.value})} placeholder="Transaction or approval reference"/></label>}
                {method==="cash"&&quickAmounts.length>0&&<div className="quickCash">{quickAmounts.map((amount)=><button type="button" key={amount} onClick={()=>setPaid(amount)}>{money(amount)}</button>)}</div>}
              </>}
              <div className={`changeDisplay ${effectivePaid>=total?"ok":"warn"}`}><span>{effectivePaid>=total?"Change due":"Amount short"}</span><b>{money(effectivePaid>=total?Math.max(0,tenderedPaid-total):total-effectivePaid)}</b></div>
                </section>
              </div>
              <footer><button type="button" onClick={()=>setShowCheckout(false)}>Back to cart</button><button className="new" disabled={!canCheckout}>{checkingOut?"Processing…":"Confirm payment  Ctrl+Enter"}</button></footer>
            </form>
          </div>
        )}
        {showShortcutHelp && <div className="transactionDialog shortcutDialog" role="dialog" aria-modal="true"><section><header><div><small>KEYBOARD HELP</small><h2>POS shortcuts</h2></div><button type="button" onClick={()=>setShowShortcutHelp(false)}>×</button></header><div className="shortcutGrid">{[["F2","Product search"],["F3","Customer"],["F4","Hold sale"],["Shift + F4","Held sales"],["F6","Invoice discount"],["F7","Checkout"],["F8 / F9 / F10","Cash / Bank / Card"],["F11","Split payment"],["Ctrl + Enter","Confirm payment"],["Esc","Close dialog"],["Alt + P","Print receipt"],["Ctrl + H","Shortcut help"]].map(([key,action])=><div key={key}><kbd>{key}</kbd><span>{action}</span></div>)}</div></section></div>}
        {registerReady && !register && !receipt && (
          <div
            className="registerGate"
            role="dialog"
            aria-modal="true"
            aria-labelledby="open-register-title"
          >
            <form onSubmit={openRegister}>
              <span className="registerGateIcon">$</span>
              <p>START YOUR SHIFT</p>
              <h1 id="open-register-title">Open cashier register</h1>
              <small>
                Count the cash currently in your drawer. This becomes the
                starting balance for an accurate end-of-shift reconciliation.
              </small>
              <label>
                Opening cash
                <input
                  type="number"
                  min="0"
                  step="0.01"
                  value={openingCash || ""}
                  onChange={(event) =>
                    setOpeningCash(Number(event.target.value))
                  }
                  autoFocus
                />
              </label>
              <label>
                Opening note <em>Optional</em>
                <textarea
                  rows={3}
                  value={openingNotes}
                  onChange={(event) => setOpeningNotes(event.target.value)}
                  placeholder="For example: Float received from manager"
                />
              </label>
              <button disabled={opening}>
                {opening
                  ? "Opening register…"
                  : "Open register and start selling"}
              </button>
            </form>
          </div>
        )}
        {showClose && register && (
          <div
            className="registerGate"
            role="dialog"
            aria-modal="true"
            aria-labelledby="close-register-title"
          >
            <form onSubmit={closeRegister}>
              <button
                className="dialogClose"
                type="button"
                onClick={() => setShowClose(false)}
                aria-label="Close"
              >
                ×
              </button>
              <p>END YOUR SHIFT</p>
              <h1 id="close-register-title">Close cashier register</h1>
              <div className="registerSummary">
                <span>
                  <small>Opening cash</small>
                  <b>{money(register.opening)}</b>
                </span>
                <span>
                  <small>Cash sales</small>
                  <b>{money(register.cashSales)}</b>
                </span>
                <span>
                  <small>Cash returns</small>
                  <b>-{money(register.cashReturns)}</b>
                </span>
                <span className="expected">
                  <small>Expected drawer cash</small>
                  <b>{money(register.expected)}</b>
                </span>
              </div>
              <label>
                Counted closing cash
                <input
                  type="number"
                  min="0"
                  step="0.01"
                  value={closingCash || ""}
                  onChange={(event) =>
                    setClosingCash(Number(event.target.value))
                  }
                  autoFocus
                />
              </label>
              <div
                className={`variancePreview ${closingCash - register.expected === 0 ? "balanced" : "different"}`}
              >
                <span>Difference</span>
                <b>{money(closingCash - register.expected)}</b>
              </div>
              <label>
                Closing note <em>Optional</em>
                <textarea
                  rows={3}
                  value={closingNotes}
                  onChange={(event) => setClosingNotes(event.target.value)}
                  placeholder="Explain any cash difference"
                />
              </label>
              <button disabled={closing}>
                {closing ? "Closing session…" : "Confirm and close session"}
              </button>
            </form>
          </div>
        )}
        {measure && (
          <div
            className="registerGate measureGate"
            role="dialog"
            aria-modal="true"
          >
            <form
              onSubmit={(e) => {
                e.preventDefault();
                addMeasured();
              }}
            >
              <button
                className="dialogClose"
                type="button"
                onClick={() => setMeasure(null)}
              >
                ×
              </button>
              <p>VARIABLE QUANTITY</p>
              <h1>{measure.Name}</h1>
              <small>
                Available: {measure.Stock} {measure.Unit} · stock is deducted in
                the base unit.
              </small>
              <label>
                Sell as
                <select
                  value={measureUnit?.unitId || ""}
                  onChange={(e) => {
                    const u = measure.AllowedUnits?.find(
                      (x: any) => x.unitId === e.target.value,
                    ) || {
                      unitId: "",
                      symbol: measure.Unit,
                      factorToBase: 1,
                      salePrice: measure.Price,
                    };
                    setMeasureUnit(u);
                  }}
                >
                  <option value="">Base unit ({measure.Unit})</option>
                  {measure.AllowedUnits?.filter((u: any) =>
                    ["sale", "both"].includes(u.usage),
                  ).map((u: any) => (
                    <option key={u.id || u.unitId} value={u.unitId}>
                      {u.name} ({u.symbol})
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Gross quantity
                <input
                  type="number"
                  min={measure.MinimumSaleQuantity || 0.000001}
                  step={measure.QuantityStep || 0.001}
                  value={measureQty}
                  onChange={(e) => setMeasureQty(Number(e.target.value))}
                  autoFocus
                />
              </label>
              <label>
                Tare / container weight
                <input
                  type="number"
                  min="0"
                  step={measure.QuantityStep || 0.001}
                  value={measureTare}
                  onChange={(e) => setMeasureTare(Number(e.target.value))}
                />
              </label>
              <div className="measureQuick">
                {[100, 250, 500, 1000].map((q) => (
                  <button
                    type="button"
                    key={q}
                    onClick={() => setMeasureQty(q)}
                  >
                    {q}
                    {measureUnit?.symbol || measure.Unit}
                  </button>
                ))}
              </div>
              <div className="variancePreview balanced">
                <span>Net base quantity</span>
                <b>
                  {Math.max(
                    0,
                    (measureQty - measureTare) *
                      Number(measureUnit?.factorToBase || 1),
                  ).toFixed(measure.DecimalPrecision || 3)}{" "}
                  {measure.Unit}
                </b>
              </div>
              <button>Add measured item</button>
            </form>
          </div>
        )}
        <ActionDialog
          request={actionDialog}
          close={() => setActionDialog(null)}
        />
      </section>
    </main>
  );
}

function Icon({ name }: { name: "box" | "cart" }) {
  const paths = {
    box: (
      <>
        <path d="m4 7 8-4 8 4-8 4-8-4Z" />
        <path d="M4 7v10l8 4 8-4V7M12 11v10" />
      </>
    ),
    cart: (
      <>
        <path d="M3 4h2l2.3 10.2a2 2 0 0 0 2 1.6h7.9a2 2 0 0 0 2-1.6L21 7H6" />
        <circle cx="10" cy="20" r="1" />
        <circle cx="18" cy="20" r="1" />
      </>
    ),
  };
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.7}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {paths[name]}
    </svg>
  );
}

function Receipt({ data, newSale }: { data: any; newSale: () => void }) {
  return (
    <div id="receipt" className="receiptPaper">
      {data.offline && (
        <div className="offlineReceiptWarning">
          <b>OFFLINE SALE</b>
          <span>Pending synchronization — this is a temporary reference.</span>
        </div>
      )}
      <div className="receiptHeader">
        <b>PERFECTONE ERP</b>
        <small>Complete Business Control</small>
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
        {data.items.map((line: CartLine) => (
          <div className="receiptItem" key={line.id}>
            <div>
              <b>{line.name}</b>
              <small>
                {line.qty} × {money(line.price)}
                {line.promotionDiscount > 0
                  ? ` · offer -${money(line.promotionDiscount * line.qty)}`
                  : ""}
                {line.discount > 0
                  ? ` · extra discount ${money(line.discount)}`
                  : ""}
              </small>
            </div>
            <b>
              {money(
                line.qty * (line.effectivePrice ?? line.price) - line.discount,
              )}
            </b>
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
      {data.promotionDiscount > 0 && (
        <div className="receiptRow">
          <span>Promotions</span>
          <b>-{money(data.promotionDiscount)}</b>
        </div>
      )}
      {data.itemDiscount > 0 && (
        <div className="receiptRow">
          <span>Item discounts</span>
          <b>-{money(data.itemDiscount)}</b>
        </div>
      )}
      <div className="receiptRow">
        <span>Invoice discount</span>
        <b>-{money(data.discount)}</b>
      </div>
      <div className="receiptRow total">
        <span>TOTAL</span>
        <b>{money(data.total)}</b>
      </div>
      <div className="receiptPayments">
        {data.payments?.map((payment: any, index: number) => (
          <div className="receiptRow" key={`${payment.method}-${index}`}>
            <span>
              {payment.method.toUpperCase()}
              {payment.reference ? ` · ${payment.reference}` : ""}
            </span>
            <b>{money(payment.amount)}</b>
          </div>
        ))}
      </div>
      <div className="receiptRow">
        <span>Change</span>
        <b>{money(data.change)}</b>
      </div>
      <div className="receiptDivider" />
      <p className="receiptThanks">Thank you for shopping with us!</p>
      <div className="receiptActions">
        <button onClick={() => window.print()}>Print receipt</button>
        <button onClick={newSale}>New sale</button>
      </div>
    </div>
  );
}
