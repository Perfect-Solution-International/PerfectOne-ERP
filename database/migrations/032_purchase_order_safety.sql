ALTER TABLE purchase_orders ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE purchase_orders ADD COLUMN IF NOT EXISTS cancellation_reason text;
CREATE UNIQUE INDEX IF NOT EXISTS purchase_order_request_unique
  ON purchase_orders(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;

ALTER TABLE purchase_items ADD COLUMN IF NOT EXISTS purchase_order_item_id uuid REFERENCES purchase_order_items(id);
CREATE INDEX IF NOT EXISTS purchase_items_po_line_idx ON purchase_items(purchase_order_item_id);

CREATE SEQUENCE IF NOT EXISTS purchase_order_number_seq;
CREATE OR REPLACE FUNCTION next_purchase_order_no() RETURNS text LANGUAGE sql AS $$
  SELECT 'PO-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('purchase_order_number_seq')::text,6,'0')
$$;

ALTER TABLE purchase_order_items DROP CONSTRAINT IF EXISTS purchase_order_received_quantity_check;
ALTER TABLE purchase_order_items ADD CONSTRAINT purchase_order_received_quantity_check
  CHECK(received_quantity>=0 AND received_quantity<=quantity);
