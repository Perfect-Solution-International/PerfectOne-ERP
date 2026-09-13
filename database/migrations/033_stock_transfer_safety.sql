ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS approved_at timestamptz;
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS dispatched_by uuid REFERENCES users(id);
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS received_by uuid REFERENCES users(id);
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS cancelled_by uuid REFERENCES users(id);
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS cancelled_at timestamptz;
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS cancellation_reason text;

CREATE UNIQUE INDEX IF NOT EXISTS stock_transfer_request_unique
  ON stock_transfers(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;

ALTER TABLE branch_stock DROP CONSTRAINT IF EXISTS branch_stock_quantity_check;
ALTER TABLE branch_stock ADD CONSTRAINT branch_stock_quantity_check CHECK(quantity>=0);

CREATE OR REPLACE FUNCTION validate_stock_transfer_tenant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS(SELECT 1 FROM branches WHERE id=NEW.from_branch_id AND tenant_id=NEW.tenant_id)
     OR NOT EXISTS(SELECT 1 FROM branches WHERE id=NEW.to_branch_id AND tenant_id=NEW.tenant_id)
     OR NOT EXISTS(SELECT 1 FROM products WHERE id=NEW.product_id AND tenant_id=NEW.tenant_id) THEN
    RAISE EXCEPTION 'transfer branches and product must belong to the same tenant';
  END IF;
  IF NEW.from_branch_id=NEW.to_branch_id THEN
    RAISE EXCEPTION 'source and destination branches must differ';
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS stock_transfer_tenant_guard ON stock_transfers;
CREATE TRIGGER stock_transfer_tenant_guard BEFORE INSERT OR UPDATE OF tenant_id,from_branch_id,to_branch_id,product_id
ON stock_transfers FOR EACH ROW EXECUTE FUNCTION validate_stock_transfer_tenant();
