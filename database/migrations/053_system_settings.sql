CREATE TABLE IF NOT EXISTS tenant_settings (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
  business_name text NOT NULL DEFAULT 'Grocerly',
  logo_url text,
  address text,
  phone text,
  email text,
  currency_code text NOT NULL DEFAULT 'LKR',
  currency_symbol text NOT NULL DEFAULT 'LKR',
  default_tax_rate numeric(8,4) NOT NULL DEFAULT 0 CHECK(default_tax_rate >= 0),
  invoice_prefix text NOT NULL DEFAULT 'INV',
  receipt_header text,
  receipt_footer text,
  receipt_size text NOT NULL DEFAULT '80mm' CHECK(receipt_size IN ('58mm','80mm','A4')),
  show_tax boolean NOT NULL DEFAULT true,
  show_discount boolean NOT NULL DEFAULT true,
  low_stock_enabled boolean NOT NULL DEFAULT true,
  expiry_alert_days integer NOT NULL DEFAULT 30 CHECK(expiry_alert_days BETWEEN 0 AND 3650),
  default_payment_method text NOT NULL DEFAULT 'cash' CHECK(default_payment_method IN ('cash','bank','card','credit','cheque')),
  updated_by uuid REFERENCES users(id),
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO tenant_settings(tenant_id,business_name)
SELECT id,name FROM tenants
ON CONFLICT(tenant_id) DO NOTHING;
