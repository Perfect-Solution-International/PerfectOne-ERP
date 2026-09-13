CREATE TABLE IF NOT EXISTS pos_discount_approvals(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
  requested_by uuid NOT NULL REFERENCES users(id), gross numeric(14,2) NOT NULL CHECK(gross>0),
  item_discount numeric(14,2) NOT NULL DEFAULT 0 CHECK(item_discount>=0), invoice_discount numeric(14,2) NOT NULL DEFAULT 0 CHECK(invoice_discount>=0),
  reason text NOT NULL, status text NOT NULL DEFAULT 'pending' CHECK(status IN('pending','approved','rejected','used')),
  reviewed_by uuid REFERENCES users(id), reviewed_at timestamptz, review_note text, sale_id uuid REFERENCES sales(id), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS pos_discount_approvals_queue_idx ON pos_discount_approvals(tenant_id,status,created_at DESC);
INSERT INTO role_permissions(role_id,permission) SELECT id,'pos.discount_approve' FROM roles WHERE key IN('super_admin','admin','manager') ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;
