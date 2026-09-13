CREATE TABLE IF NOT EXISTS accounting_year_closures(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 fiscal_period_id uuid NOT NULL REFERENCES fiscal_periods(id), closing_journal_id uuid NOT NULL REFERENCES journal_entries(id),
 retained_earnings numeric(14,2) NOT NULL, closed_by uuid REFERENCES users(id), closed_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,fiscal_period_id)
);
CREATE TABLE IF NOT EXISTS accounting_opening_register(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), account_id uuid NOT NULL REFERENCES chart_of_accounts(id),
 opening_date date NOT NULL, journal_id uuid NOT NULL REFERENCES journal_entries(id), created_by uuid REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,account_id,opening_date)
);
CREATE INDEX IF NOT EXISTS accounting_year_closures_tenant_idx ON accounting_year_closures(tenant_id,closed_at DESC);
CREATE OR REPLACE FUNCTION prevent_overlapping_fiscal_periods() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM fiscal_periods p WHERE p.tenant_id=NEW.tenant_id AND p.id<>NEW.id AND daterange(p.start_date,p.end_date,'[]') && daterange(NEW.start_date,NEW.end_date,'[]')) THEN RAISE EXCEPTION 'fiscal period overlaps an existing period'; END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS fiscal_period_overlap_guard ON fiscal_periods;
CREATE TRIGGER fiscal_period_overlap_guard BEFORE INSERT OR UPDATE OF start_date,end_date,tenant_id ON fiscal_periods FOR EACH ROW EXECUTE FUNCTION prevent_overlapping_fiscal_periods();
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type,description,is_system,allow_manual_entries,normal_balance)
SELECT id,'3100','Retained Earnings','equity','Accumulated retained profit and loss',true,false,'credit' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission,enabled)
SELECT r.id,p.permission,true FROM roles r CROSS JOIN LATERAL(VALUES
 ('accounting.post'),('accounting.reverse'),('accounting.lock_period'),('accounting.close_year')) p(permission)
WHERE r.key IN('super_admin','admin','accountant')
ON CONFLICT(role_id,permission) DO UPDATE SET enabled=true;
