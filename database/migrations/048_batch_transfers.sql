ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS batch_id uuid REFERENCES stock_batches(id);
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS document_no text;
UPDATE stock_transfers SET document_no=COALESCE(NULLIF(reference,''),'ST-'||upper(substr(replace(id::text,'-',''),1,10))) WHERE document_no IS NULL;
CREATE INDEX IF NOT EXISTS stock_transfers_document_idx ON stock_transfers(tenant_id,document_no);
