ALTER TABLE promotions ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES tenants(id);
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS name text;
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS category_id uuid REFERENCES categories(id);
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS created_by uuid REFERENCES users(id);
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS updated_by uuid REFERENCES users(id);
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE promotions ADD COLUMN IF NOT EXISTS archived_at timestamptz;

UPDATE promotions pr SET tenant_id=p.tenant_id, name=COALESCE(NULLIF(pr.name,''),p.name||' promotion')
FROM products p WHERE p.id=pr.product_id AND (pr.tenant_id IS NULL OR pr.name IS NULL);
ALTER TABLE promotions ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE promotions ALTER COLUMN name SET NOT NULL;
ALTER TABLE promotions ALTER COLUMN product_id DROP NOT NULL;
ALTER TABLE promotions DROP CONSTRAINT IF EXISTS promotions_target_check;
ALTER TABLE promotions ADD CONSTRAINT promotions_target_check CHECK (num_nonnulls(product_id,category_id)=1);
ALTER TABLE promotions DROP CONSTRAINT IF EXISTS promotions_value_check;
ALTER TABLE promotions ADD CONSTRAINT promotions_value_check CHECK (value>0 AND (kind<>'percentage' OR value<=100));
ALTER TABLE promotions DROP CONSTRAINT IF EXISTS promotions_dates_check;
ALTER TABLE promotions ADD CONSTRAINT promotions_dates_check CHECK (end_date>=start_date);
CREATE INDEX IF NOT EXISTS promotions_active_lookup_idx ON promotions(tenant_id,start_date,end_date) WHERE is_active AND archived_at IS NULL;

ALTER TABLE sale_items ADD COLUMN IF NOT EXISTS promotion_id uuid REFERENCES promotions(id);
ALTER TABLE sale_items ADD COLUMN IF NOT EXISTS promotion_discount numeric(14,2) NOT NULL DEFAULT 0 CHECK(promotion_discount>=0);

CREATE TABLE IF NOT EXISTS promotion_usage(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 promotion_id uuid NOT NULL REFERENCES promotions(id), sale_id uuid NOT NULL REFERENCES sales(id),
 sale_item_id uuid NOT NULL UNIQUE REFERENCES sale_items(id), quantity numeric(14,3) NOT NULL CHECK(quantity>0),
 original_unit_price numeric(14,2) NOT NULL CHECK(original_unit_price>=0),
 effective_unit_price numeric(14,2) NOT NULL CHECK(effective_unit_price>=0),
 discount_amount numeric(14,2) NOT NULL CHECK(discount_amount>=0), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS promotion_usage_history_idx ON promotion_usage(tenant_id,promotion_id,created_at DESC);

CREATE OR REPLACE FUNCTION block_promotion_usage_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'promotion usage history is immutable'; END $$;
DROP TRIGGER IF EXISTS promotion_usage_immutable ON promotion_usage;
CREATE TRIGGER promotion_usage_immutable BEFORE UPDATE OR DELETE ON promotion_usage FOR EACH ROW EXECUTE FUNCTION block_promotion_usage_mutation();

INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('promotions.view'),('promotions.add'),('promotions.edit'),('promotions.delete')
) p(permission) WHERE r.key IN('super_admin','admin') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,'promotions.view' FROM roles r WHERE r.key IN('manager','cashier') ON CONFLICT DO NOTHING;
