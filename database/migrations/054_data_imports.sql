CREATE TABLE IF NOT EXISTS data_import_jobs(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES tenants(id),kind text NOT NULL CHECK(kind IN('products','customers','suppliers','opening_stock')),
 status text NOT NULL CHECK(status IN('validated','completed','failed')),file_name text,row_count integer NOT NULL DEFAULT 0,success_count integer NOT NULL DEFAULT 0,error_count integer NOT NULL DEFAULT 0,
 errors jsonb NOT NULL DEFAULT '[]',created_by uuid REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now(),completed_at timestamptz
);
CREATE INDEX IF NOT EXISTS data_import_jobs_tenant_idx ON data_import_jobs(tenant_id,created_at DESC);
