ALTER TABLE damaged_items ADD COLUMN IF NOT EXISTS branch_id uuid REFERENCES branches(id);
ALTER TABLE damaged_items ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE damaged_items ADD COLUMN IF NOT EXISTS finalized_by uuid REFERENCES users(id);
ALTER TABLE damaged_items ADD COLUMN IF NOT EXISTS finalized_at timestamptz;
ALTER TABLE damaged_items ADD COLUMN IF NOT EXISTS cancelled_by uuid REFERENCES users(id);
ALTER TABLE damaged_items ADD COLUMN IF NOT EXISTS cancellation_reason text;

CREATE UNIQUE INDEX IF NOT EXISTS damaged_item_request_unique
  ON damaged_items(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;

ALTER TABLE damaged_items DROP CONSTRAINT IF EXISTS damaged_items_quantity_check;
ALTER TABLE damaged_items ADD CONSTRAINT damaged_items_quantity_check CHECK(quantity>0);
ALTER TABLE damaged_items DROP CONSTRAINT IF EXISTS damaged_items_status_check;
ALTER TABLE damaged_items ADD CONSTRAINT damaged_items_status_check CHECK(status IN('draft','finalized','cancelled'));
ALTER TABLE stock_batches DROP CONSTRAINT IF EXISTS stock_batches_available_qty_check;
ALTER TABLE stock_batches ADD CONSTRAINT stock_batches_available_qty_check CHECK(available_qty>=0);

CREATE OR REPLACE FUNCTION validate_damaged_item_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS(SELECT 1 FROM products WHERE id=NEW.product_id AND tenant_id=NEW.tenant_id) THEN
    RAISE EXCEPTION 'damage product must belong to the same tenant';
  END IF;
  IF NEW.branch_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM branches WHERE id=NEW.branch_id AND tenant_id=NEW.tenant_id) THEN
    RAISE EXCEPTION 'damage branch must belong to the same tenant';
  END IF;
  IF NEW.batch_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM stock_batches WHERE id=NEW.batch_id AND product_id=NEW.product_id) THEN
    RAISE EXCEPTION 'damage batch must belong to the selected product';
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS damaged_item_scope_guard ON damaged_items;
CREATE TRIGGER damaged_item_scope_guard BEFORE INSERT OR UPDATE OF tenant_id,product_id,batch_id,branch_id
ON damaged_items FOR EACH ROW EXECUTE FUNCTION validate_damaged_item_scope();
