CREATE TABLE IF NOT EXISTS stock_transfer_documents(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 document_no text NOT NULL, from_branch_id uuid NOT NULL REFERENCES branches(id), to_branch_id uuid NOT NULL REFERENCES branches(id),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN('draft','approved','dispatched','received','cancelled')),
 reference text, notes text, created_by uuid REFERENCES users(id), approved_by uuid REFERENCES users(id), dispatched_by uuid REFERENCES users(id), received_by uuid REFERENCES users(id), cancelled_by uuid REFERENCES users(id),
 approved_at timestamptz, dispatched_at timestamptz, received_at timestamptz, cancelled_at timestamptz, cancellation_reason text,
 client_request_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,document_no), UNIQUE(tenant_id,client_request_id), CHECK(from_branch_id<>to_branch_id)
);
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS document_id uuid REFERENCES stock_transfer_documents(id);
CREATE INDEX IF NOT EXISTS stock_transfer_document_lines_idx ON stock_transfers(document_id,created_at);
