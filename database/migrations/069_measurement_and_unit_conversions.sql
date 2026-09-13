-- Variable-measure products and auditable unit conversions.
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS measurement_type text NOT NULL DEFAULT 'count',
  ADD COLUMN IF NOT EXISTS decimal_precision smallint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS minimum_sale_quantity numeric(14,6) NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS quantity_step numeric(14,6) NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS tare_weight numeric(14,6) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS plu_code text;

ALTER TABLE products DROP CONSTRAINT IF EXISTS products_measurement_type_check;
ALTER TABLE products ADD CONSTRAINT products_measurement_type_check
  CHECK (measurement_type IN ('weight','volume','count','length'));
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_measurement_rules_check;
ALTER TABLE products ADD CONSTRAINT products_measurement_rules_check
  CHECK (decimal_precision BETWEEN 0 AND 6 AND minimum_sale_quantity > 0 AND quantity_step > 0 AND tare_weight >= 0);
CREATE UNIQUE INDEX IF NOT EXISTS products_tenant_plu_unique ON products(tenant_id,plu_code)
  WHERE plu_code IS NOT NULL AND btrim(plu_code) <> '';

CREATE TABLE IF NOT EXISTS product_units(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  product_id uuid NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  unit_id uuid NOT NULL REFERENCES units(id),
  factor_to_base numeric(18,6) NOT NULL CHECK(factor_to_base > 0),
  usage text NOT NULL DEFAULT 'both' CHECK(usage IN ('purchase','sale','both')),
  purchase_price numeric(14,2),
  sale_price numeric(14,2),
  barcode text,
  is_default_purchase boolean NOT NULL DEFAULT false,
  is_default_sale boolean NOT NULL DEFAULT false,
  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(tenant_id,product_id,unit_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS product_units_tenant_barcode_unique ON product_units(tenant_id,barcode)
  WHERE barcode IS NOT NULL AND btrim(barcode) <> '';
CREATE INDEX IF NOT EXISTS product_units_product_idx ON product_units(tenant_id,product_id,is_active);

ALTER TABLE sale_items
  ADD COLUMN IF NOT EXISTS entered_quantity numeric(14,6),
  ADD COLUMN IF NOT EXISTS entered_unit_id uuid REFERENCES units(id),
  ADD COLUMN IF NOT EXISTS entered_unit_price numeric(14,2),
  ADD COLUMN IF NOT EXISTS conversion_factor numeric(18,6) NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS tare_quantity numeric(14,6) NOT NULL DEFAULT 0;
UPDATE sale_items SET entered_quantity=quantity WHERE entered_quantity IS NULL;
UPDATE sale_items SET entered_unit_price=unit_price WHERE entered_unit_price IS NULL;
ALTER TABLE sale_items ALTER COLUMN entered_quantity SET NOT NULL;

ALTER TABLE sale_return_items
  ADD COLUMN IF NOT EXISTS entered_quantity numeric(14,6),
  ADD COLUMN IF NOT EXISTS entered_unit_id uuid REFERENCES units(id),
  ADD COLUMN IF NOT EXISTS conversion_factor numeric(18,6) NOT NULL DEFAULT 1;

ALTER TABLE purchase_items
  ADD COLUMN IF NOT EXISTS entered_quantity numeric(14,6),
  ADD COLUMN IF NOT EXISTS entered_unit_id uuid REFERENCES units(id),
  ADD COLUMN IF NOT EXISTS entered_unit_cost numeric(14,2),
  ADD COLUMN IF NOT EXISTS conversion_factor numeric(18,6) NOT NULL DEFAULT 1;
UPDATE purchase_items SET entered_quantity=quantity WHERE entered_quantity IS NULL;

CREATE TABLE IF NOT EXISTS unit_conversion_events(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  branch_id uuid REFERENCES branches(id),
  event_type text NOT NULL CHECK(event_type IN ('repack','unpack','wastage','spillage','correction')),
  source_product_id uuid NOT NULL REFERENCES products(id),
  target_product_id uuid REFERENCES products(id),
  source_quantity numeric(14,6) NOT NULL CHECK(source_quantity > 0),
  source_unit_id uuid REFERENCES units(id),
  source_base_quantity numeric(14,6) NOT NULL CHECK(source_base_quantity > 0),
  target_quantity numeric(14,6),
  target_unit_id uuid REFERENCES units(id),
  target_base_quantity numeric(14,6),
  reason text NOT NULL,
  reference text,
  created_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS unit_conversion_events_history_idx
  ON unit_conversion_events(tenant_id,source_product_id,created_at DESC);

CREATE OR REPLACE FUNCTION block_unit_conversion_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'unit conversion history is immutable; create a correcting event'; END $$;
DROP TRIGGER IF EXISTS unit_conversion_events_immutable ON unit_conversion_events;
CREATE TRIGGER unit_conversion_events_immutable BEFORE UPDATE OR DELETE ON unit_conversion_events
FOR EACH ROW EXECUTE FUNCTION block_unit_conversion_event_mutation();
