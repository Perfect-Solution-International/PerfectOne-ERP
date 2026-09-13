-- Links later supplier payments to the GRNs they settle without mutating the
-- original purchase journal. Payment reversals remain visible through the
-- supplier_payments status and therefore automatically reopen the GRN balance.
CREATE TABLE IF NOT EXISTS purchase_payment_allocations(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  purchase_id uuid NOT NULL REFERENCES purchases(id),
  supplier_payment_id uuid NOT NULL REFERENCES supplier_payments(id),
  amount numeric(14,2) NOT NULL CHECK(amount>0),
  created_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(supplier_payment_id)
);

CREATE INDEX IF NOT EXISTS purchase_payment_allocations_purchase_idx
  ON purchase_payment_allocations(tenant_id,purchase_id,created_at DESC);

CREATE OR REPLACE FUNCTION protect_purchase_payment_allocation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'purchase payment allocations are immutable; reverse the supplier payment';
END $$;
DROP TRIGGER IF EXISTS purchase_payment_allocation_immutable ON purchase_payment_allocations;
CREATE TRIGGER purchase_payment_allocation_immutable
  BEFORE UPDATE OR DELETE ON purchase_payment_allocations
  FOR EACH ROW EXECUTE FUNCTION protect_purchase_payment_allocation();

CREATE INDEX IF NOT EXISTS purchases_supplier_status_date_idx
  ON purchases(tenant_id,supplier_id,status,purchase_date DESC);

ALTER TABLE purchases ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
