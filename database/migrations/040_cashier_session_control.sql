ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES tenants(id);
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS branch_id uuid REFERENCES branches(id);
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS opened_by uuid REFERENCES users(id);
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS closed_by uuid REFERENCES users(id);
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS close_notes text;
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS reconciled_by uuid REFERENCES users(id);
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS reconciled_at timestamptz;

UPDATE cashier_sessions s
SET tenant_id=u.tenant_id,
    branch_id=u.branch_id,
    opened_by=COALESCE(s.opened_by,s.user_id)
FROM users u
WHERE u.id=s.user_id
  AND (s.tenant_id IS NULL OR s.branch_id IS NULL OR s.opened_by IS NULL);

ALTER TABLE sales ADD COLUMN IF NOT EXISTS cashier_session_id uuid REFERENCES cashier_sessions(id);
ALTER TABLE sales ADD COLUMN IF NOT EXISTS cancellation_session_id uuid REFERENCES cashier_sessions(id);
ALTER TABLE sale_returns ADD COLUMN IF NOT EXISTS cashier_session_id uuid REFERENCES cashier_sessions(id);

UPDATE sales s
SET cashier_session_id=(
  SELECT cs.id
  FROM cashier_sessions cs
  WHERE cs.user_id=s.cashier_id
    AND cs.tenant_id=s.tenant_id
    AND (cs.branch_id IS NULL OR cs.branch_id=s.branch_id)
    AND s.created_at>=cs.opened_at
    AND (cs.closed_at IS NULL OR s.created_at<=cs.closed_at)
  ORDER BY cs.opened_at DESC
  LIMIT 1
)
WHERE s.cashier_session_id IS NULL;

UPDATE sale_returns r SET cashier_session_id=s.cashier_session_id
FROM sales s WHERE s.id=r.sale_id AND r.cashier_session_id IS NULL;
UPDATE sales SET cancellation_session_id=cashier_session_id
WHERE status='cancelled' AND cancellation_session_id IS NULL;

CREATE TABLE IF NOT EXISTS cashier_session_events(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES cashier_sessions(id),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  branch_id uuid REFERENCES branches(id),
  event_type text NOT NULL CHECK(event_type IN('sale_cash','sale_return_cash','sale_cancel_cash','customer_payment_cash','cash_adjustment')),
  reference_id uuid NOT NULL,
  amount numeric(14,2) NOT NULL CHECK(amount<>0),
  created_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(session_id,event_type,reference_id)
);

INSERT INTO cashier_session_events(session_id,tenant_id,branch_id,event_type,reference_id,amount,created_by,created_at)
SELECT s.cashier_session_id,s.tenant_id,s.branch_id,'sale_cash',s.id,s.paid_amount,s.cashier_id,s.created_at
FROM sales s WHERE s.cashier_session_id IS NOT NULL AND s.payment_method='cash' AND s.paid_amount>0
ON CONFLICT(session_id,event_type,reference_id) DO NOTHING;

INSERT INTO cashier_session_events(session_id,tenant_id,branch_id,event_type,reference_id,amount,created_by,created_at)
SELECT r.cashier_session_id,r.tenant_id,s.branch_id,'sale_return_cash',r.id,-r.refunded_amount,r.returned_by,r.created_at
FROM sale_returns r JOIN sales s ON s.id=r.sale_id
WHERE r.cashier_session_id IS NOT NULL AND s.payment_method='cash' AND r.refunded_amount>0
ON CONFLICT(session_id,event_type,reference_id) DO NOTHING;

INSERT INTO cashier_session_events(session_id,tenant_id,branch_id,event_type,reference_id,amount,created_by,created_at)
SELECT s.cancellation_session_id,s.tenant_id,s.branch_id,'sale_cancel_cash',s.id,-s.paid_amount,s.cancelled_by,s.cancelled_at
FROM sales s
WHERE s.cancellation_session_id IS NOT NULL AND s.payment_method='cash' AND s.status='cancelled' AND s.paid_amount>0
ON CONFLICT(session_id,event_type,reference_id) DO NOTHING;

UPDATE cashier_sessions cs
SET expected_cash=cs.opening_cash+COALESCE((SELECT sum(e.amount) FROM cashier_session_events e WHERE e.session_id=cs.id),0),
    variance=cs.closing_cash-(cs.opening_cash+COALESCE((SELECT sum(e.amount) FROM cashier_session_events e WHERE e.session_id=cs.id),0))
WHERE cs.closed_at IS NOT NULL AND cs.closing_cash IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS cashier_sessions_one_open_per_user
  ON cashier_sessions(user_id)
  WHERE closed_at IS NULL;
CREATE INDEX IF NOT EXISTS cashier_sessions_tenant_branch_opened_idx
  ON cashier_sessions(tenant_id,branch_id,opened_at DESC);
CREATE INDEX IF NOT EXISTS sales_cashier_session_idx
  ON sales(cashier_session_id,created_at);
CREATE INDEX IF NOT EXISTS cashier_session_events_session_idx
  ON cashier_session_events(session_id,created_at);

INSERT INTO role_permissions(role_id,permission)
SELECT id,'cashier_session' FROM roles WHERE key='manager'
ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;

DO $$
BEGIN
  IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='cashier_sessions_cash_nonnegative') THEN
    ALTER TABLE cashier_sessions ADD CONSTRAINT cashier_sessions_cash_nonnegative
      CHECK(opening_cash>=0 AND (closing_cash IS NULL OR closing_cash>=0)) NOT VALID;
  END IF;
END $$;

CREATE OR REPLACE FUNCTION validate_cashier_session_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.cashier_session_id IS NOT NULL AND NOT EXISTS(
    SELECT 1
    FROM cashier_sessions cs
    WHERE cs.id=NEW.cashier_session_id
      AND cs.user_id=NEW.cashier_id
      AND cs.tenant_id=NEW.tenant_id
      AND (cs.branch_id IS NULL OR cs.branch_id=NEW.branch_id)
      AND NEW.created_at>=cs.opened_at
      AND (cs.closed_at IS NULL OR NEW.created_at<=cs.closed_at)
  ) THEN
    RAISE EXCEPTION 'sale cashier session does not match cashier, tenant, branch or session period';
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS sales_cashier_session_guard ON sales;
CREATE TRIGGER sales_cashier_session_guard
BEFORE INSERT OR UPDATE OF cashier_session_id,cashier_id,tenant_id,branch_id,created_at ON sales
FOR EACH ROW EXECUTE FUNCTION validate_cashier_session_scope();

CREATE OR REPLACE FUNCTION validate_cashier_session_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS(
    SELECT 1 FROM cashier_sessions cs
    WHERE cs.id=NEW.session_id
      AND cs.tenant_id=NEW.tenant_id
      AND (cs.branch_id IS NULL OR cs.branch_id=NEW.branch_id)
      AND NEW.created_at>=cs.opened_at
      AND (cs.closed_at IS NULL OR NEW.created_at<=cs.closed_at)
  ) THEN
    RAISE EXCEPTION 'cashier session event is outside the session scope or period';
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS cashier_session_event_scope_guard ON cashier_session_events;
CREATE TRIGGER cashier_session_event_scope_guard
BEFORE INSERT ON cashier_session_events
FOR EACH ROW EXECUTE FUNCTION validate_cashier_session_event();

CREATE OR REPLACE FUNCTION prevent_cashier_session_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'cashier session events are immutable; create a correcting cash adjustment instead';
END $$;
DROP TRIGGER IF EXISTS cashier_session_events_immutable ON cashier_session_events;
CREATE TRIGGER cashier_session_events_immutable
BEFORE UPDATE OR DELETE ON cashier_session_events
FOR EACH ROW EXECUTE FUNCTION prevent_cashier_session_event_mutation();
