ALTER TABLE purchases DROP CONSTRAINT IF EXISTS purchases_invoice_no_key;
CREATE UNIQUE INDEX IF NOT EXISTS purchases_supplier_invoice_unique
  ON purchases(tenant_id,supplier_id,invoice_no);
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS account_id uuid REFERENCES cash_accounts(id);
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS finalized_at timestamptz;
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS reversal_reason text;
CREATE UNIQUE INDEX IF NOT EXISTS purchase_request_unique
  ON purchases(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;

ALTER TABLE purchase_items ADD COLUMN IF NOT EXISTS batch_id uuid REFERENCES stock_batches(id);
ALTER TABLE purchase_returns ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE purchase_returns ADD COLUMN IF NOT EXISTS cancellation_reason text;
CREATE UNIQUE INDEX IF NOT EXISTS purchase_return_request_unique
  ON purchase_returns(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;

CREATE SEQUENCE IF NOT EXISTS purchase_return_number_seq;
CREATE OR REPLACE FUNCTION next_purchase_return_no() RETURNS text LANGUAGE sql AS $$
  SELECT 'PR-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('purchase_return_number_seq')::text,6,'0')
$$;

CREATE OR REPLACE FUNCTION post_procurement_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE inventory_id uuid; payable_id uuid; cash_id uuid; new_journal_id uuid; original_id uuid;
BEGIN
 IF TG_TABLE_NAME='purchases' AND NEW.status='finalized' AND NEW.status IS DISTINCT FROM OLD.status THEN
  SELECT id INTO inventory_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1200';
  SELECT id INTO payable_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='2000';
  SELECT id INTO cash_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN NEW.payment_method='bank' THEN '1010' ELSE '1000' END;
  INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at)
    VALUES(NEW.tenant_id,'purchase',NEW.id,'GRN finalized '||NEW.invoice_no,NEW.finalized_by,'posted',now()) RETURNING id INTO new_journal_id;
  INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(new_journal_id,inventory_id,NEW.total);
  IF NEW.total-NEW.paid_amount>0 THEN INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(new_journal_id,payable_id,NEW.total-NEW.paid_amount); END IF;
  IF NEW.paid_amount>0 THEN INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(new_journal_id,cash_id,NEW.paid_amount); END IF;
 END IF;
 IF TG_TABLE_NAME='purchases' AND NEW.status='reversed' AND NEW.status IS DISTINCT FROM OLD.status THEN
  SELECT id INTO original_id FROM journal_entries WHERE tenant_id=NEW.tenant_id AND reference_type='purchase' AND reference_id=NEW.id AND status='posted' ORDER BY created_at LIMIT 1 FOR UPDATE;
  IF original_id IS NOT NULL THEN
   INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at,reversed_entry_id,reversal_reason)
     VALUES(NEW.tenant_id,'purchase_reversal',NEW.id,'GRN reversed '||NEW.invoice_no,NEW.reversed_by,'posted',now(),original_id,NEW.reversal_reason) RETURNING id INTO new_journal_id;
   INSERT INTO journal_lines(journal_id,account_id,debit,credit) SELECT new_journal_id,account_id,credit,debit FROM journal_lines WHERE journal_id=original_id;
   UPDATE journal_entries SET status='reversed' WHERE id=original_id;
  END IF;
 END IF;
 IF TG_TABLE_NAME='purchase_returns' AND NEW.status='finalized' AND NEW.status IS DISTINCT FROM OLD.status THEN
  SELECT id INTO inventory_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1200';
  SELECT id INTO payable_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='2000';
  INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at)
    VALUES(NEW.tenant_id,'purchase_return',NEW.id,'Supplier return '||NEW.return_number,NEW.finalized_by,'posted',now()) RETURNING id INTO new_journal_id;
  INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(new_journal_id,payable_id,NEW.total);
  INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(new_journal_id,inventory_id,NEW.total);
 END IF;
 RETURN NEW;
END $$;
