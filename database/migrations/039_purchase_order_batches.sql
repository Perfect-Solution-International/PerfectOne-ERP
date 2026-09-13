ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS batch_no text;
ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS manufactured_date date;
ALTER TABLE purchase_order_items ADD COLUMN IF NOT EXISTS expiry_date date;

ALTER TABLE purchase_order_items DROP CONSTRAINT IF EXISTS purchase_order_item_dates_check;
ALTER TABLE purchase_order_items ADD CONSTRAINT purchase_order_item_dates_check
  CHECK(manufactured_date IS NULL OR expiry_date IS NULL OR manufactured_date <= expiry_date);

CREATE INDEX IF NOT EXISTS purchase_order_items_batch_idx
  ON purchase_order_items(product_id,batch_no,expiry_date);
