-- Procurement receiving integrity and traceability.
ALTER TABLE stock_batches ADD COLUMN IF NOT EXISTS manufactured_date date;
ALTER TABLE stock_batches DROP CONSTRAINT IF EXISTS stock_batches_dates_check;
ALTER TABLE stock_batches ADD CONSTRAINT stock_batches_dates_check
  CHECK(manufactured_date IS NULL OR expiry_date IS NULL OR manufactured_date<=expiry_date);

ALTER TABLE purchases ADD COLUMN IF NOT EXISTS cancelled_by uuid REFERENCES users(id);
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS cancelled_at timestamptz;
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS cancellation_reason text;

CREATE INDEX IF NOT EXISTS purchase_items_purchase_order_line_status_idx
  ON purchase_items(purchase_order_item_id,purchase_id);
CREATE INDEX IF NOT EXISTS purchases_po_status_idx
  ON purchases(purchase_order_id,status) WHERE purchase_order_id IS NOT NULL;
