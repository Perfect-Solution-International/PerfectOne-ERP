CREATE TABLE IF NOT EXISTS user_permissions(
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  permission text NOT NULL,
  enabled boolean NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id, permission)
);
CREATE INDEX IF NOT EXISTS user_permissions_user_idx ON user_permissions(user_id);
