CREATE TABLE IF NOT EXISTS backup_schedules (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
  enabled boolean NOT NULL DEFAULT false,
  frequency text NOT NULL DEFAULT 'daily' CHECK (frequency IN ('daily','weekly')),
  run_at time NOT NULL DEFAULT '02:00',
  weekday smallint NOT NULL DEFAULT 0 CHECK (weekday BETWEEN 0 AND 6),
  timezone text NOT NULL DEFAULT 'Asia/Colombo',
  retention_count integer NOT NULL DEFAULT 14 CHECK (retention_count BETWEEN 1 AND 365),
  next_run_at timestamptz,
  last_run_at timestamptz,
  last_status text,
  last_error text,
  updated_by uuid REFERENCES users(id),
  updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE backup_history ADD COLUMN IF NOT EXISTS trigger_type text NOT NULL DEFAULT 'manual';
ALTER TABLE backup_history DROP CONSTRAINT IF EXISTS backup_history_trigger_type_check;
ALTER TABLE backup_history ADD CONSTRAINT backup_history_trigger_type_check CHECK (trigger_type IN ('manual','scheduled','pre_restore'));
CREATE INDEX IF NOT EXISTS backup_schedule_due_idx ON backup_schedules(next_run_at) WHERE enabled;
