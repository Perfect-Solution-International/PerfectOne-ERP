CREATE SEQUENCE IF NOT EXISTS invoice_number_sequence;
CREATE OR REPLACE FUNCTION next_safe_invoice_no() RETURNS text LANGUAGE sql AS $$ SELECT 'INV-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('invoice_number_sequence')::text,8,'0') $$;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS client_request_id text;
CREATE UNIQUE INDEX IF NOT EXISTS sales_request_id_unique ON sales(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS transaction_requests(tenant_id uuid NOT NULL REFERENCES tenants(id),request_id text NOT NULL,operation text NOT NULL,entity_id uuid,response jsonb,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,request_id,operation));
