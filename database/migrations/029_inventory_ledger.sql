ALTER TABLE stock_adjustments ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES tenants(id);
ALTER TABLE stock_adjustments ADD COLUMN IF NOT EXISTS branch_id uuid REFERENCES branches(id);
ALTER TABLE stock_adjustments ADD COLUMN IF NOT EXISTS system_quantity numeric(14,3);
ALTER TABLE stock_adjustments ADD COLUMN IF NOT EXISTS physical_quantity numeric(14,3);
ALTER TABLE stock_adjustments ADD COLUMN IF NOT EXISTS notes text;
ALTER TABLE stock_adjustments ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'finalized'
  CHECK (status IN ('finalized', 'reversed'));
ALTER TABLE stock_adjustments ADD COLUMN IF NOT EXISTS client_request_id text;

UPDATE stock_adjustments a
SET tenant_id = p.tenant_id
FROM products p
WHERE p.id = a.product_id AND a.tenant_id IS NULL;

ALTER TABLE stock_adjustments ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE stock_movements ALTER COLUMN tenant_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS stock_adjustment_request_unique
  ON stock_adjustments(tenant_id, client_request_id)
  WHERE client_request_id IS NOT NULL;

-- Existing stock predates the immutable ledger. Record one auditable opening
-- entry per product without changing the current on-hand quantity.
INSERT INTO stock_movements(product_id, kind, quantity, tenant_id, reference_type, balance_after, notes)
SELECT p.id, 'opening', p.stock_quantity, p.tenant_id, 'product', p.stock_quantity,
       'Opening balance migration'
FROM products p
WHERE p.stock_quantity <> 0
  AND p.tenant_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM stock_movements m WHERE m.product_id = p.id
  );

CREATE OR REPLACE FUNCTION prevent_negative_product_stock()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.stock_quantity < 0 THEN
    RAISE EXCEPTION 'product stock cannot be negative';
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS products_non_negative_stock ON products;
CREATE TRIGGER products_non_negative_stock
BEFORE INSERT OR UPDATE OF stock_quantity ON products
FOR EACH ROW EXECUTE FUNCTION prevent_negative_product_stock();
