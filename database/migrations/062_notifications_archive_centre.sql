CREATE TABLE IF NOT EXISTS user_notification_states(
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  notification_key text NOT NULL,
  is_read boolean NOT NULL DEFAULT false,
  dismissed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,notification_key)
);
CREATE INDEX IF NOT EXISTS user_notification_states_tenant_idx ON user_notification_states(tenant_id,user_id,updated_at DESC);
INSERT INTO role_permissions(role_id,permission) SELECT id,'notifications.view' FROM roles WHERE key IN('super_admin','admin','manager','stock_manager','accountant') ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;
INSERT INTO role_permissions(role_id,permission) SELECT id,'archive.manage' FROM roles WHERE key IN('super_admin','admin') ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;
