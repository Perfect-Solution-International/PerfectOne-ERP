ALTER TABLE customer_payments ADD COLUMN IF NOT EXISTS payment_date date NOT NULL DEFAULT current_date;
ALTER TABLE customer_payments ADD COLUMN IF NOT EXISTS notes text;
ALTER TABLE customer_payments ADD COLUMN IF NOT EXISTS account_id uuid REFERENCES cash_accounts(id);
ALTER TABLE customer_payments ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'finalized' CHECK(status IN('finalized','reversed'));
ALTER TABLE customer_payments ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE customer_payments ADD COLUMN IF NOT EXISTS reversed_by uuid REFERENCES users(id);
ALTER TABLE customer_payments ADD COLUMN IF NOT EXISTS reversed_at timestamptz;
ALTER TABLE customer_payments ADD COLUMN IF NOT EXISTS reversal_reason text;
CREATE UNIQUE INDEX IF NOT EXISTS customer_payment_request_unique ON customer_payments(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;

ALTER TABLE supplier_payments ADD COLUMN IF NOT EXISTS payment_date date NOT NULL DEFAULT current_date;
ALTER TABLE supplier_payments ADD COLUMN IF NOT EXISTS notes text;
ALTER TABLE supplier_payments ADD COLUMN IF NOT EXISTS account_id uuid REFERENCES cash_accounts(id);
ALTER TABLE supplier_payments ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'finalized' CHECK(status IN('finalized','reversed'));
ALTER TABLE supplier_payments ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE supplier_payments ADD COLUMN IF NOT EXISTS reversed_by uuid REFERENCES users(id);
ALTER TABLE supplier_payments ADD COLUMN IF NOT EXISTS reversed_at timestamptz;
ALTER TABLE supplier_payments ADD COLUMN IF NOT EXISTS reversal_reason text;
CREATE UNIQUE INDEX IF NOT EXISTS supplier_payment_request_unique ON supplier_payments(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;
