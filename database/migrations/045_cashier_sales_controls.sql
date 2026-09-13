ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS close_denominations jsonb;
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS reconciliation_note text;
ALTER TABLE cashier_sessions ADD COLUMN IF NOT EXISTS reconciliation_variance_approved boolean NOT NULL DEFAULT false;

INSERT INTO role_permissions(role_id,permission)
SELECT id,p FROM roles CROSS JOIN LATERAL (VALUES('cashier_session.view'),('cashier_session.reconcile'),('pos.discount_policy')) x(p)
WHERE key IN('super_admin','admin','manager') ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;

INSERT INTO role_permissions(role_id,permission)
SELECT id,'cashier_session.view' FROM roles WHERE key='cashier'
ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;
