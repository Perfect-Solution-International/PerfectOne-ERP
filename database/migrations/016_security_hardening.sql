CREATE TABLE IF NOT EXISTS revoked_tokens(token_hash text PRIMARY KEY,expires_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS revoked_tokens_expiry_idx ON revoked_tokens(expires_at);
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES tenants(id);
UPDATE expenses SET tenant_id=(SELECT id FROM tenants WHERE slug='default-grocery') WHERE tenant_id IS NULL;
CREATE INDEX IF NOT EXISTS expenses_tenant_idx ON expenses(tenant_id);
