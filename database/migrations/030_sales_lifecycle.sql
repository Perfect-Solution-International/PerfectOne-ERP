ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_status_check;
ALTER TABLE sales ADD CONSTRAINT sales_status_check
  CHECK (status IN ('completed','partial_returned','returned','cancelled'));
ALTER TABLE sales ADD COLUMN IF NOT EXISTS returned_total numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS returned_receivable numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS returned_paid numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS cancellation_reason text;

ALTER TABLE sale_returns ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES tenants(id);
ALTER TABLE sale_returns ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'finalized'
  CHECK (status IN ('finalized','reversed'));
ALTER TABLE sale_returns ADD COLUMN IF NOT EXISTS refunded_amount numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE sale_returns ADD COLUMN IF NOT EXISTS receivable_adjustment numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE sale_returns ADD COLUMN IF NOT EXISTS client_request_id text;
UPDATE sale_returns r SET tenant_id=s.tenant_id FROM sales s WHERE s.id=r.sale_id AND r.tenant_id IS NULL;
ALTER TABLE sale_returns ALTER COLUMN tenant_id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS sale_return_request_unique
  ON sale_returns(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;

CREATE SEQUENCE IF NOT EXISTS sale_return_number_seq;
CREATE OR REPLACE FUNCTION next_sale_return_no() RETURNS text LANGUAGE sql AS $$
  SELECT 'RET-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('sale_return_number_seq')::text,6,'0')
$$;

CREATE TABLE IF NOT EXISTS sale_item_batches(
  sale_item_id uuid NOT NULL REFERENCES sale_items(id),
  batch_id uuid NOT NULL REFERENCES stock_batches(id),
  quantity numeric(14,3) NOT NULL CHECK(quantity>0),
  returned_quantity numeric(14,3) NOT NULL DEFAULT 0 CHECK(returned_quantity>=0 AND returned_quantity<=quantity),
  PRIMARY KEY(sale_item_id,batch_id)
);
