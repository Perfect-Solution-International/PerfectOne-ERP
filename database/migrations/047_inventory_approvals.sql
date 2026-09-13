CREATE TABLE IF NOT EXISTS stock_adjustment_requests(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), branch_id uuid NOT NULL REFERENCES branches(id),
 product_id uuid NOT NULL REFERENCES products(id), system_quantity numeric(14,3) NOT NULL, physical_quantity numeric(14,3) NOT NULL,
 difference numeric(14,3) NOT NULL CHECK(difference<>0), reason text NOT NULL, notes text, status text NOT NULL DEFAULT 'draft' CHECK(status IN('draft','approved','finalized','rejected','cancelled')),
 requested_by uuid REFERENCES users(id), approved_by uuid REFERENCES users(id), finalized_by uuid REFERENCES users(id), decision_reason text,
 client_request_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), approved_at timestamptz, finalized_at timestamptz,
 UNIQUE(tenant_id,client_request_id)
);
CREATE INDEX IF NOT EXISTS stock_adjustment_requests_queue_idx ON stock_adjustment_requests(tenant_id,status,created_at DESC);
INSERT INTO role_permissions(role_id,permission,enabled) SELECT r.id,'inventory.approve',true FROM roles r WHERE r.key IN('super_admin','admin','manager','stock_manager') ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;
