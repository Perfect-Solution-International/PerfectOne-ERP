CREATE TABLE IF NOT EXISTS supplier_products(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 supplier_id uuid NOT NULL REFERENCES suppliers(id), product_id uuid NOT NULL REFERENCES products(id),
 supplier_product_code text, supplier_sku text, default_purchase_price numeric(14,2) NOT NULL DEFAULT 0,
 last_purchase_price numeric(14,2) NOT NULL DEFAULT 0, recommended_selling_price numeric(14,2) NOT NULL DEFAULT 0,
 wholesale_price numeric(14,2) NOT NULL DEFAULT 0, minimum_order_qty numeric(14,3) NOT NULL DEFAULT 1,
 pack_size numeric(14,3) NOT NULL DEFAULT 1, lead_time_days integer NOT NULL DEFAULT 0,
 is_preferred boolean NOT NULL DEFAULT false, is_active boolean NOT NULL DEFAULT true,
 last_purchased_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,supplier_id,product_id)
);
CREATE INDEX IF NOT EXISTS supplier_products_lookup_idx ON supplier_products(tenant_id,supplier_id,is_active,product_id);

ALTER TABLE purchase_items ADD COLUMN IF NOT EXISTS selling_price numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE purchase_items ADD COLUMN IF NOT EXISTS wholesale_price numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE purchase_items ADD COLUMN IF NOT EXISTS free_quantity numeric(14,3) NOT NULL DEFAULT 0;
ALTER TABLE purchase_items ADD COLUMN IF NOT EXISTS manufactured_date date;
ALTER TABLE purchase_items ADD COLUMN IF NOT EXISTS update_purchase_price boolean NOT NULL DEFAULT false;
ALTER TABLE purchase_items ADD COLUMN IF NOT EXISTS update_selling_price boolean NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS supplier_product_price_history(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 supplier_id uuid NOT NULL REFERENCES suppliers(id), product_id uuid NOT NULL REFERENCES products(id),
 purchase_id uuid REFERENCES purchases(id), old_purchase_price numeric(14,2), new_purchase_price numeric(14,2) NOT NULL,
 selling_price numeric(14,2), wholesale_price numeric(14,2), changed_by uuid REFERENCES users(id), changed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS supplier_product_price_history_idx ON supplier_product_price_history(tenant_id,supplier_id,product_id,changed_at DESC);

INSERT INTO supplier_products(tenant_id,supplier_id,product_id,default_purchase_price,last_purchase_price,recommended_selling_price,wholesale_price,last_purchased_at)
SELECT DISTINCT p.tenant_id,p.supplier_id,i.product_id,i.unit_cost,i.unit_cost,pr.selling_price,pr.wholesale_price,p.created_at
FROM purchases p JOIN purchase_items i ON i.purchase_id=p.id JOIN products pr ON pr.id=i.product_id
WHERE p.tenant_id IS NOT NULL
ON CONFLICT(tenant_id,supplier_id,product_id) DO UPDATE SET last_purchase_price=EXCLUDED.last_purchase_price,last_purchased_at=EXCLUDED.last_purchased_at;
