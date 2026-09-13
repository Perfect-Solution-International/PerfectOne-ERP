ALTER TABLE users ALTER COLUMN role DROP DEFAULT;
ALTER TABLE users ALTER COLUMN role TYPE text USING role::text;
ALTER TABLE users ALTER COLUMN role SET DEFAULT 'cashier';

CREATE TABLE IF NOT EXISTS roles(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  key text NOT NULL,
  name text NOT NULL,
  description text,
  is_system boolean NOT NULL DEFAULT false,
  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(tenant_id,key),
  UNIQUE(tenant_id,name)
);
CREATE TABLE IF NOT EXISTS role_permissions(
  role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  PRIMARY KEY(role_id,permission)
);

INSERT INTO roles(tenant_id,key,name,is_system)
SELECT t.id,r.key,r.name,true FROM tenants t CROSS JOIN (VALUES
 ('super_admin','Super Admin'),('admin','Admin'),('manager','Manager'),
 ('accountant','Accountant'),('cashier','Cashier'),('stock_manager','Stock Manager'),
 ('purchase_officer','Purchase Officer')
) r(key,name) ON CONFLICT(tenant_id,key) DO NOTHING;

INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('dashboard'),('pos'),('sales'),('sales_returns'),('own_sales'),('cashier_session'),
 ('products'),('purchases'),('inventory'),('expiry'),('damage'),('reports'),('contacts'),
 ('accounting'),('cash_bank'),('financial_reports'),('users'),('roles'),('catalog_read')
) p(permission) WHERE r.key IN('super_admin','admin') ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('dashboard'),('pos'),('sales'),('sales_returns'),('purchases'),('inventory'),('reports'),('contacts'),('catalog_read')
) p(permission) WHERE r.key='manager' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('accounting'),('cash_bank'),('financial_reports'),('contacts'),('catalog_read')
) p(permission) WHERE r.key='accountant' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('pos'),('sales_returns'),('own_sales'),('cashier_session'),('catalog_read')
) p(permission) WHERE r.key='cashier' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('products'),('purchases'),('inventory'),('expiry'),('damage'),('catalog_read')
) p(permission) WHERE r.key='stock_manager' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('purchases'),('suppliers.view'),('products.view'),('purchases.view'),('purchases.add'),('purchases.edit'),('purchases.print'),('catalog_read')
) p(permission) WHERE r.key='purchase_officer' ON CONFLICT DO NOTHING;

-- Seed granular actions for full-access roles.
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,m.module||'.'||a.action FROM roles r
CROSS JOIN (VALUES('pos'),('sales'),('purchases'),('products'),('inventory'),('customers'),('suppliers'),('reports'),('accounting'),('cash_bank'),('users'),('roles')) m(module)
CROSS JOIN (VALUES('view'),('add'),('edit'),('delete'),('approve'),('cancel'),('print'),('export')) a(action)
WHERE r.key IN('super_admin','admin') ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('pos.view'),('sales.add'),('sales_returns.add'),('sales.print'),('own_sales.view')
) p(permission) WHERE r.key='cashier' ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('pos.view'),('sales.view'),('sales.add'),('sales.edit'),('sales.cancel'),('sales.print'),('sales.export'),
 ('sales_returns.view'),('sales_returns.add'),('purchases.view'),('purchases.add'),('purchases.edit'),('purchases.approve'),('purchases.print'),
 ('products.view'),('products.add'),('products.edit'),('inventory.view'),('customers.view'),('customers.add'),('customers.edit'),
 ('suppliers.view'),('suppliers.add'),('suppliers.edit'),('reports.view'),('reports.print'),('reports.export')
) p(permission) WHERE r.key='manager' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('accounting.view'),('accounting.add'),('accounting.edit'),('accounting.approve'),('accounting.print'),('accounting.export'),
 ('cash_bank.view'),('cash_bank.add'),('cash_bank.edit'),('reports.view'),('reports.print'),('reports.export'),
 ('customers.view'),('customers.edit'),('suppliers.view'),('suppliers.edit')
) p(permission) WHERE r.key='accountant' ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES
 ('products.view'),('products.add'),('products.edit'),('products.delete'),('inventory.view'),('inventory.add'),('inventory.edit'),
 ('purchases.view'),('purchases.add'),('purchases.edit'),('purchases.approve'),('expiry.view'),('damage.view'),('damage.add')
) p(permission) WHERE r.key='stock_manager' ON CONFLICT DO NOTHING;
