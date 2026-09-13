"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { apiFetch } from "../lib/api";
const empty = {
  name: "",
  productCode: "",
  sku: "",
  barcode: "",
  categoryId: "",
  subcategoryId: "",
  brandId: "",
  unitId: "",
  purchasePrice: 0,
  sellingPrice: 0,
  wholesalePrice: 0,
  minimumStock: 0,
  openingStock: 0,
  taxRate: 0,
  trackExpiry: false,
  description: "",
  imageUrl: "",
  isActive: true,
  measurementType: "count",
  decimalPrecision: 0,
  minimumSaleQuantity: 1,
  quantityStep: 1,
  tareWeight: 0,
  pluCode: "",
  allowedUnits: [] as any[],
};
export default function ProductForm({
  product,
  onSave,
}: {
  product?: any;
  onSave: (value: any) => Promise<void>;
}) {
  const [form, setForm] = useState<any>(empty),
    [meta, setMeta] = useState<any>({
      categories: [],
      subcategories: [],
      brands: [],
      units: [],
    }),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  useEffect(() => {
    apiFetch("/product-metadata").then(setMeta);
  }, []);
  useEffect(() => {
    if (product)
      setForm({
        ...empty,
        name: product.Name || "",
        productCode: product.ProductCode || "",
        sku: product.SKU || "",
        barcode: product.Barcode || "",
        categoryId: product.CategoryID || "",
        subcategoryId: product.SubcategoryID || "",
        brandId: product.BrandID || "",
        unitId: product.UnitID || "",
        purchasePrice: product.Cost || 0,
        sellingPrice: product.Price || 0,
        wholesalePrice: product.WholesalePrice || 0,
        minimumStock: product.MinimumStock || 0,
        openingStock: product.Stock || 0,
        taxRate: product.TaxRate || 0,
        trackExpiry: !!product.TrackExpiry,
        description: product.Description || "",
        imageUrl: product.ImageURL || "",
        isActive: product.IsActive !== false,
        measurementType: product.MeasurementType || "count",
        decimalPrecision: product.DecimalPrecision || 0,
        minimumSaleQuantity: product.MinimumSaleQuantity || 1,
        quantityStep: product.QuantityStep || 1,
        tareWeight: product.TareWeight || 0,
        pluCode: product.PLUCode || "",
        allowedUnits: product.AllowedUnits || [],
      });
  }, [product]);
  const set = (key: string, value: any) =>
    setForm((x: any) => ({ ...x, [key]: value }));
  const image = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    if (file.size > 1_500_000) {
      setError("Image must be under 1.5 MB.");
      return;
    }
    const reader = new FileReader();
    reader.onload = () => set("imageUrl", reader.result);
    reader.readAsDataURL(file);
  };
  const generate = async () =>
    set("barcode", (await apiFetch("/barcodes/generate")).barcode);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await onSave(form);
    } catch (e: any) {
      setError(
        e.message.includes("duplicate")
          ? "This barcode, product code, SKU or PLU already exists."
          : e.message,
      );
    } finally {
      setBusy(false);
    }
  };
  const subs = meta.subcategories.filter(
    (x: any) => !form.categoryId || x.parentId === form.categoryId,
  );
  const addUnit = () =>
    set("allowedUnits", [
      ...form.allowedUnits,
      {
        unitId: "",
        factorToBase: 1,
        usage: "both",
        purchasePrice: null,
        salePrice: null,
        barcode: "",
      },
    ]);
  const unitSet = (i: number, k: string, v: any) =>
    set(
      "allowedUnits",
      form.allowedUnits.map((x: any, n: number) =>
        n === i ? { ...x, [k]: v } : x,
      ),
    );
  return (
    <form className="productForm" onSubmit={submit}>
      {error && (
        <button
          type="button"
          className="catalogMessage"
          onClick={() => setError("")}
        >
          {error}
          <span>×</span>
        </button>
      )}
      <FormSection
        title="Basic information"
        hint="Product identity and catalogue grouping."
      >
        <Field label="Product name" required>
          <input
            value={form.name}
            onChange={(e) => set("name", e.target.value)}
            required
          />
        </Field>
        <Field label="Product code" required>
          <input
            value={form.productCode}
            onChange={(e) => set("productCode", e.target.value)}
            required
          />
        </Field>
        <Field label="SKU" required>
          <input
            value={form.sku}
            onChange={(e) => set("sku", e.target.value)}
            required
          />
        </Field>
        <Field label="Barcode">
          <div className="barcodeInput">
            <input
              value={form.barcode}
              onChange={(e) => set("barcode", e.target.value)}
              placeholder="Scan or enter barcode"
            />
            <button type="button" onClick={generate}>
              Generate
            </button>
          </div>
        </Field>
        <Field label="Category">
          <Select
            value={form.categoryId}
            items={meta.categories}
            onChange={(v: string) => {
              set("categoryId", v);
              set("subcategoryId", "");
            }}
          />
        </Field>
        <Field label="Subcategory">
          <Select
            value={form.subcategoryId}
            items={subs}
            onChange={(v: string) => set("subcategoryId", v)}
          />
        </Field>
        <Field label="Brand">
          <Select
            value={form.brandId}
            items={meta.brands}
            onChange={(v: string) => set("brandId", v)}
          />
        </Field>
        <Field label="Base stock unit">
          <Select
            value={form.unitId}
            items={meta.units}
            onChange={(v: string) => set("unitId", v)}
            symbol
          />
        </Field>
        <div className="catalogueShortcut">
          <span>Need another category, brand or unit?</span>
          <Link href="/catalog-settings">Manage catalogue settings →</Link>
        </div>
      </FormSection>
      <FormSection
        title="Measurement & unit conversion"
        hint="Stock is held in the base unit; purchase and sale units convert automatically."
      >
        <Field label="Measurement type">
          <select
            value={form.measurementType}
            onChange={(e) => set("measurementType", e.target.value)}
          >
            <option value="count">Count</option>
            <option value="weight">Weight</option>
            <option value="volume">Volume</option>
            <option value="length">Length</option>
          </select>
        </Field>
        <Num
          label="Decimal precision"
          value={form.decimalPrecision}
          set={(v: number) => set("decimalPrecision", v)}
          step="1"
        />
        <Num
          label="Minimum sale quantity"
          value={form.minimumSaleQuantity}
          set={(v: number) => set("minimumSaleQuantity", v)}
          step="0.000001"
        />
        <Num
          label="Quantity step"
          value={form.quantityStep}
          set={(v: number) => set("quantityStep", v)}
          step="0.000001"
        />
        <Num
          label="Default tare"
          value={form.tareWeight}
          set={(v: number) => set("tareWeight", v)}
          step="0.000001"
        />
        <Field label="Scale / PLU code">
          <input
            value={form.pluCode}
            onChange={(e) => set("pluCode", e.target.value)}
            placeholder="Optional scale lookup code"
          />
        </Field>
        <div className="wide unitConversionEditor">
          <header>
            <div>
              <b>Allowed purchase & sale units</b>
              <small>Example: 25 kg bag = 25,000 g; 1 litre = 1,000 ml.</small>
            </div>
            <button type="button" onClick={addUnit}>
              + Add unit
            </button>
          </header>
          {form.allowedUnits.map((u: any, i: number) => (
            <div className="unitConversionRow" key={i}>
              <Select
                value={u.unitId}
                items={meta.units}
                onChange={(v: string) => unitSet(i, "unitId", v)}
                symbol
              />
              <input
                aria-label="Conversion factor"
                type="number"
                min="0.000001"
                step="0.000001"
                value={u.factorToBase}
                onChange={(e) =>
                  unitSet(i, "factorToBase", Number(e.target.value))
                }
                placeholder="Base factor"
              />
              <select
                value={u.usage}
                onChange={(e) => unitSet(i, "usage", e.target.value)}
              >
                <option value="both">Purchase & sale</option>
                <option value="purchase">Purchase only</option>
                <option value="sale">Sale only</option>
              </select>
              <input
                type="number"
                step="0.01"
                value={u.purchasePrice ?? ""}
                onChange={(e) =>
                  unitSet(
                    i,
                    "purchasePrice",
                    e.target.value === "" ? null : Number(e.target.value),
                  )
                }
                placeholder="Purchase price"
              />
              <input
                type="number"
                step="0.01"
                value={u.salePrice ?? ""}
                onChange={(e) =>
                  unitSet(
                    i,
                    "salePrice",
                    e.target.value === "" ? null : Number(e.target.value),
                  )
                }
                placeholder="Sale price"
              />
              <input
                value={u.barcode || ""}
                onChange={(e) => unitSet(i, "barcode", e.target.value)}
                placeholder="Unit barcode"
              />
              <button
                type="button"
                onClick={() =>
                  set(
                    "allowedUnits",
                    form.allowedUnits.filter((_: any, n: number) => n !== i),
                  )
                }
              >
                Remove
              </button>
            </div>
          ))}
        </div>
      </FormSection>
      <FormSection
        title="Pricing & stock"
        hint="Prices are per base unit unless a converted unit has its own price."
      >
        <Num
          label="Purchase price"
          value={form.purchasePrice}
          set={(v: number) => set("purchasePrice", v)}
        />
        <Num
          label="Selling price"
          value={form.sellingPrice}
          set={(v: number) => set("sellingPrice", v)}
          required
        />
        <Num
          label="Wholesale price"
          value={form.wholesalePrice}
          set={(v: number) => set("wholesalePrice", v)}
        />
        <Num
          label="Minimum stock level"
          value={form.minimumStock}
          set={(v: number) => set("minimumStock", v)}
        />
        {!product && (
          <Num
            label="Opening stock (base unit)"
            value={form.openingStock}
            set={(v: number) => set("openingStock", v)}
          />
        )}
        <Num
          label="Tax (%)"
          value={form.taxRate}
          set={(v: number) => set("taxRate", v)}
        />
        <label className="checkField">
          <input
            type="checkbox"
            checked={form.trackExpiry}
            onChange={(e) => set("trackExpiry", e.target.checked)}
          />
          <span>
            <b>Expiry tracking</b>
            <small>Track batches and expiry dates.</small>
          </span>
        </label>
        <label className="checkField">
          <input
            type="checkbox"
            checked={form.isActive}
            onChange={(e) => set("isActive", e.target.checked)}
          />
          <span>
            <b>Active product</b>
            <small>Available in inventory and POS.</small>
          </span>
        </label>
      </FormSection>
      <FormSection
        title="Media & notes"
        hint="Product image and internal description."
      >
        <Field label="Product image">
          <div className="imagePicker">
            {form.imageUrl ? (
              <img src={form.imageUrl} alt="Product preview" />
            ) : (
              <span>No image</span>
            )}
            <input type="file" accept="image/*" onChange={image} />
            {form.imageUrl && (
              <button type="button" onClick={() => set("imageUrl", "")}>
                Remove
              </button>
            )}
          </div>
        </Field>
        <Field label="Description" wide>
          <textarea
            rows={5}
            value={form.description}
            onChange={(e) => set("description", e.target.value)}
          />
        </Field>
      </FormSection>
      <footer>
        <Link href="/products">Cancel</Link>
        <button className="new" disabled={busy}>
          {busy ? "Saving…" : product ? "Save changes" : "Create product"}
        </button>
      </footer>
    </form>
  );
}
function FormSection({ title, hint, children }: any) {
  return (
    <section className="productFormSection">
      <div className="formSectionIntro">
        <b>{title}</b>
        <small>{hint}</small>
      </div>
      <div className="productFields">{children}</div>
    </section>
  );
}
function Field({ label, required, wide, children }: any) {
  return (
    <label className={wide ? "wide" : ""}>
      {label}
      {required && <em>*</em>}
      {children}
    </label>
  );
}
function Num({ label, value, set, required, step = "0.01" }: any) {
  return (
    <Field label={label} required={required}>
      <input
        type="number"
        min="0"
        step={step}
        value={value ?? ""}
        onChange={(e) => set(Number(e.target.value))}
        required={required}
      />
    </Field>
  );
}
function Select({ value, items, onChange, symbol }: any) {
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">Not selected</option>
      {items.map((x: any) => (
        <option key={x.id} value={x.id}>
          {x.name}
          {symbol && x.symbol ? ` (${x.symbol})` : ""}
        </option>
      ))}
    </select>
  );
}
