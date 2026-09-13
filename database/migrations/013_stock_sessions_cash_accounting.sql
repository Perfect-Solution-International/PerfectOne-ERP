ALTER TABLE stock_batches ADD COLUMN IF NOT EXISTS unit_cost numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE stock_batches ADD COLUMN IF NOT EXISTS received_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS expected_cash numeric(14,2);
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS variance numeric(14,2);
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS notes text;

CREATE TABLE IF NOT EXISTS stock_transfers(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),from_branch_id uuid REFERENCES branches(id),to_branch_id uuid REFERENCES branches(id),product_id uuid NOT NULL REFERENCES products(id),quantity numeric(14,3) NOT NULL CHECK(quantity>0),status text NOT NULL DEFAULT 'completed',reference text,transferred_by uuid REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS stock_adjustments(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),product_id uuid NOT NULL REFERENCES products(id),quantity numeric(14,3) NOT NULL CHECK(quantity<>0),reason text NOT NULL,adjusted_by uuid REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS cash_accounts(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid REFERENCES tenants(id),name text NOT NULL,account_type text NOT NULL CHECK(account_type IN('cash','bank')),opening_balance numeric(14,2) NOT NULL DEFAULT 0,is_active boolean NOT NULL DEFAULT true,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS cash_transactions(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),account_id uuid NOT NULL REFERENCES cash_accounts(id),transaction_type text NOT NULL CHECK(transaction_type IN('deposit','withdrawal','expense','income','transfer_in','transfer_out')),amount numeric(14,2) NOT NULL CHECK(amount>0),reference text,description text,transaction_date date NOT NULL DEFAULT current_date,created_by uuid REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS stock_batches_fefo_idx ON stock_batches(product_id,expiry_date,received_at);
CREATE INDEX IF NOT EXISTS cash_transactions_account_idx ON cash_transactions(account_id,transaction_date);
