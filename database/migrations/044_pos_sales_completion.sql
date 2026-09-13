-- POS completion: resumable carts, granular discounts and immutable tender ledger.
CREATE TABLE IF NOT EXISTS role_discount_limits(
  role_id uuid PRIMARY KEY REFERENCES roles(id) ON DELETE CASCADE,
  max_item_percent numeric(5,2) NOT NULL DEFAULT 0 CHECK(max_item_percent BETWEEN 0 AND 100),
  max_invoice_percent numeric(5,2) NOT NULL DEFAULT 0 CHECK(max_invoice_percent BETWEEN 0 AND 100),
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO role_discount_limits(role_id,max_item_percent,max_invoice_percent)
SELECT id,
 CASE WHEN key IN('super_admin','admin') THEN 100 WHEN key='manager' THEN 20 WHEN key='cashier' THEN 5 ELSE 0 END,
 CASE WHEN key IN('super_admin','admin') THEN 100 WHEN key='manager' THEN 20 WHEN key='cashier' THEN 5 ELSE 0 END
FROM roles ON CONFLICT(role_id) DO NOTHING;

ALTER TABLE sale_items ADD COLUMN IF NOT EXISTS discount numeric(14,2) NOT NULL DEFAULT 0 CHECK(discount>=0);

CREATE TABLE IF NOT EXISTS held_sales(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  branch_id uuid NOT NULL REFERENCES branches(id),
  cashier_id uuid NOT NULL REFERENCES users(id),
  cashier_session_id uuid NOT NULL REFERENCES cashier_sessions(id),
  customer_id uuid REFERENCES customers(id),
  reference text NOT NULL,
  cart jsonb NOT NULL CHECK(jsonb_typeof(cart)='array'),
  invoice_discount numeric(14,2) NOT NULL DEFAULT 0 CHECK(invoice_discount>=0),
  status text NOT NULL DEFAULT 'held' CHECK(status IN('held','resumed','cancelled')),
  client_request_id text NOT NULL,
  held_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz,
  UNIQUE(tenant_id,client_request_id),
  UNIQUE(tenant_id,reference)
);
CREATE INDEX IF NOT EXISTS held_sales_open_idx ON held_sales(tenant_id,branch_id,cashier_id,held_at DESC) WHERE status='held';

CREATE TABLE IF NOT EXISTS sale_payments(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  sale_id uuid NOT NULL REFERENCES sales(id),
  cashier_session_id uuid NOT NULL REFERENCES cashier_sessions(id),
  method text NOT NULL CHECK(method IN('cash','bank','card')),
  amount numeric(14,2) NOT NULL CHECK(amount>0),
  tendered numeric(14,2) NOT NULL CHECK(tendered>=amount),
  reference text,
  created_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sale_payments_sale_idx ON sale_payments(sale_id,created_at);

CREATE TABLE IF NOT EXISTS sale_payment_refunds(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  sale_payment_id uuid NOT NULL REFERENCES sale_payments(id),
  reference_type text NOT NULL CHECK(reference_type IN('sale_return','sale_cancel')),
  reference_id uuid NOT NULL,
  amount numeric(14,2) NOT NULL CHECK(amount>0),
  created_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(sale_payment_id,reference_type,reference_id)
);

CREATE OR REPLACE FUNCTION protect_pos_financial_ledger() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'POS payment records are immutable; use return or cancellation'; END $$;
DROP TRIGGER IF EXISTS sale_payments_immutable ON sale_payments;
CREATE TRIGGER sale_payments_immutable BEFORE UPDATE OR DELETE ON sale_payments FOR EACH ROW EXECUTE FUNCTION protect_pos_financial_ledger();
DROP TRIGGER IF EXISTS sale_payment_refunds_immutable ON sale_payment_refunds;
CREATE TRIGGER sale_payment_refunds_immutable BEFORE UPDATE OR DELETE ON sale_payment_refunds FOR EACH ROW EXECUTE FUNCTION protect_pos_financial_ledger();

-- Split sales are posted after allocations are inserted by the API. Existing single-tender
-- sales retain the established automatic posting path.
CREATE OR REPLACE FUNCTION post_sale_revenue() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cash_id uuid; ar_id uuid; revenue_id uuid; j uuid;
BEGIN
 IF NEW.payment_method='split' THEN RETURN NEW; END IF;
 SELECT id INTO cash_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN NEW.payment_method IN('bank','card') THEN '1010' ELSE '1000' END;
 SELECT id INTO ar_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1100';
 SELECT id INTO revenue_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='4000';
 INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at)
 VALUES(NEW.tenant_id,'sale',NEW.id,'Sale '||NEW.invoice_no,NEW.cashier_id,'posted',now()) RETURNING id INTO j;
 IF NEW.paid_amount>0 THEN INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(j,cash_id,NEW.paid_amount); END IF;
 IF NEW.balance>0 THEN INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(j,ar_id,NEW.balance); END IF;
 INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(j,revenue_id,NEW.total);
 RETURN NEW;
END $$;

INSERT INTO role_permissions(role_id,permission)
SELECT id,p FROM roles CROSS JOIN LATERAL (VALUES('pos.hold'),('pos.resume'),('pos.split_payment')) x(p)
WHERE key IN('super_admin','admin','manager','cashier') ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;
