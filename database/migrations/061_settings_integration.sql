ALTER TABLE tenant_settings ADD COLUMN IF NOT EXISTS business_registration_no text;
ALTER TABLE tenant_settings ADD COLUMN IF NOT EXISTS tax_registration_no text;
ALTER TABLE tenant_settings ADD COLUMN IF NOT EXISTS invoice_number_digits integer NOT NULL DEFAULT 6 CHECK(invoice_number_digits BETWEEN 4 AND 12);
ALTER TABLE tenant_settings ADD COLUMN IF NOT EXISTS receipt_title text NOT NULL DEFAULT 'SALES RECEIPT';
ALTER TABLE tenant_settings ADD COLUMN IF NOT EXISTS show_business_logo boolean NOT NULL DEFAULT true;
ALTER TABLE tenant_settings ADD COLUMN IF NOT EXISTS show_business_address boolean NOT NULL DEFAULT true;
ALTER TABLE tenant_settings ADD COLUMN IF NOT EXISTS show_business_contact boolean NOT NULL DEFAULT true;
ALTER TABLE tenant_settings ADD COLUMN IF NOT EXISTS receipt_copies integer NOT NULL DEFAULT 1 CHECK(receipt_copies BETWEEN 1 AND 3);

CREATE TABLE IF NOT EXISTS tenant_document_sequences(
 tenant_id uuid NOT NULL REFERENCES tenants(id), document_type text NOT NULL,
 next_value bigint NOT NULL DEFAULT 1 CHECK(next_value>0), PRIMARY KEY(tenant_id,document_type)
);

CREATE OR REPLACE FUNCTION next_tenant_invoice_no(p_tenant uuid) RETURNS text LANGUAGE plpgsql AS $$
DECLARE v_next bigint; v_prefix text; v_digits integer;
BEGIN
 INSERT INTO tenant_document_sequences(tenant_id,document_type,next_value) VALUES(p_tenant,'invoice',2)
 ON CONFLICT(tenant_id,document_type) DO UPDATE SET next_value=tenant_document_sequences.next_value+1
 RETURNING next_value-1 INTO v_next;
 SELECT invoice_prefix,invoice_number_digits INTO v_prefix,v_digits FROM tenant_settings WHERE tenant_id=p_tenant;
 RETURN COALESCE(NULLIF(v_prefix,''),'INV')||'-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(v_next::text,COALESCE(v_digits,6),'0');
END $$;

INSERT INTO role_permissions(role_id,permission)
SELECT r.id,p.permission FROM roles r CROSS JOIN LATERAL (VALUES('settings.view'),('settings.edit')) p(permission)
WHERE r.key IN('super_admin','admin') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id,permission)
SELECT r.id,'settings.view' FROM roles r WHERE r.key IN('manager','accountant','cashier','stock_manager','purchase_officer') ON CONFLICT DO NOTHING;
