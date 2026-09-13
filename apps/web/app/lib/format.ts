export const CURRENCY = "LKR";

const LOCALE = "en-LK";

const toNumber = (value: any) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

const group = (value: number, decimals: number) =>
  value.toLocaleString(LOCALE, { minimumFractionDigits: decimals, maximumFractionDigits: decimals });

/** Exact figure, always two decimals, no currency prefix. */
export const amount = (value: any) => group(toNumber(value), 2);

/** Exact money. The default everywhere a figure must be readable to the cent —
 *  tables, invoices, receipts, forms, totals, ledgers. */
export const money = (value: any) => `${CURRENCY} ${amount(value)}`;

/** Plain integer counts — units, invoice counts, document numbers. */
export const count = (value: any) => group(toNumber(value), 0);

/**
 * Money shortened so it fits inside a narrow KPI card. Loses precision by design,
 * so always pair it with `title={money(value)}` — the exact figure stays one hover away.
 *
 *   under 1M   LKR 18,800.00     (cents still matter at this size)
 *   under 1B   LKR 13,732,450    (cents are noise, the card is not a ledger)
 *   1B and up  LKR 10.0B         (nothing else fits, and the magnitude is the point)
 *
 * Never use it on an invoice, a receipt, a journal line or anything a person
 * reconciles against — those need `money`.
 */
export const moneyCompact = (value: any) => {
  const parsed = toNumber(value);
  const size = Math.abs(parsed);
  if (size < 1e6) return `${CURRENCY} ${group(parsed, 2)}`;
  if (size < 1e9) return `${CURRENCY} ${group(parsed, 0)}`;
  const [divisor, suffix] = size < 1e12 ? [1e9, "B"] : [1e12, "T"];
  return `${CURRENCY} ${group(parsed / divisor, 1)}${suffix}`;
};

/**
 * Class that steps the KPI number down a size or two when the string is long,
 * so a wide figure shrinks to fit instead of being clipped mid-digit.
 * Lengths are tuned against the 240px minimum card width in liquid-glass.css.
 */
export const metricValueClass = (text: string) =>
  text.length > 14 ? "metricValue tiny" : text.length > 11 ? "metricValue small" : "metricValue";

export type MoneyFigure = { text: string; title: string; className: string };

/**
 * Everything a figure inside a narrow stat card needs: the shortened text, the
 * exact amount for the hover title, and the size-step class. Render it as
 * `<b className={f.className} title={f.title}>{f.text}</b>`.
 */
export const moneyFigure = (value: any): MoneyFigure => {
  const text = moneyCompact(value);
  return { text, title: money(value), className: metricValueClass(text) };
};
