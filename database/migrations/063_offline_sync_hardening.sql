CREATE TABLE IF NOT EXISTS pos_sync_transactions(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 request_id text NOT NULL,user_id uuid REFERENCES users(id),branch_id uuid REFERENCES branches(id),terminal_id text NOT NULL,
 local_timestamp timestamptz,payload_hash text NOT NULL,status text NOT NULL DEFAULT 'syncing' CHECK(status IN('syncing','synced','failed','conflict','discarded')),
 attempts integer NOT NULL DEFAULT 1,last_error text,response_code integer,invoice_no text,
 resolution_reason text,resolved_by uuid REFERENCES users(id),resolved_at timestamptz,
 first_seen_at timestamptz NOT NULL DEFAULT now(),last_attempt_at timestamptz NOT NULL DEFAULT now(),synced_at timestamptz,
 UNIQUE(tenant_id,request_id)
);
CREATE INDEX IF NOT EXISTS pos_sync_transactions_queue_idx ON pos_sync_transactions(tenant_id,status,last_attempt_at DESC);
CREATE INDEX IF NOT EXISTS pos_sync_transactions_terminal_idx ON pos_sync_transactions(tenant_id,terminal_id,last_attempt_at DESC);
INSERT INTO role_permissions(role_id,permission) SELECT id,'sync.view' FROM roles WHERE key IN('super_admin','admin','manager','cashier') ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;
