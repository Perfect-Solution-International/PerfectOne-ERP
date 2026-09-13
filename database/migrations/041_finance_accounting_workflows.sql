-- Finance and accounting control layer. Existing operational records are preserved.
ALTER TABLE chart_of_accounts ADD COLUMN IF NOT EXISTS parent_id uuid REFERENCES chart_of_accounts(id);
ALTER TABLE chart_of_accounts ADD COLUMN IF NOT EXISTS description text;
ALTER TABLE chart_of_accounts ADD COLUMN IF NOT EXISTS is_system boolean NOT NULL DEFAULT false;
ALTER TABLE chart_of_accounts ADD COLUMN IF NOT EXISTS allow_manual_entries boolean NOT NULL DEFAULT true;
ALTER TABLE chart_of_accounts ADD COLUMN IF NOT EXISTS normal_balance text CHECK(normal_balance IN('debit','credit'));
ALTER TABLE chart_of_accounts ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE chart_of_accounts ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
UPDATE chart_of_accounts
SET normal_balance=CASE WHEN account_type IN('asset','expense') THEN 'debit' ELSE 'credit' END
WHERE normal_balance IS NULL;
UPDATE chart_of_accounts SET is_system=true,allow_manual_entries=false
WHERE code IN('1000','1010','1100','1200','2000','3000','4000','5000');

INSERT INTO chart_of_accounts(tenant_id,code,name,account_type,is_system,allow_manual_entries,normal_balance)
SELECT id,'1300','Supplier Advances','asset',true,false,'debit' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type,is_system,allow_manual_entries,normal_balance)
SELECT id,'2100','Customer Advances','liability',true,false,'credit' FROM tenants ON CONFLICT DO NOTHING;

ALTER TABLE cash_accounts ADD COLUMN IF NOT EXISTS ledger_account_id uuid REFERENCES chart_of_accounts(id);
ALTER TABLE cash_accounts ADD COLUMN IF NOT EXISTS account_number text;
ALTER TABLE cash_accounts ADD COLUMN IF NOT EXISTS description text;
ALTER TABLE cash_accounts ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
UPDATE cash_accounts SET tenant_id=(SELECT id FROM tenants WHERE slug='default-grocery') WHERE tenant_id IS NULL;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type,description,is_system,allow_manual_entries,normal_balance)
SELECT a.tenant_id,'CA-'||replace(substr(a.id::text,1,13),'-',''),a.name,'asset','Operational '||a.account_type||' account',true,false,'debit'
FROM cash_accounts a
WHERE a.tenant_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM chart_of_accounts c WHERE c.tenant_id=a.tenant_id AND c.code='CA-'||replace(substr(a.id::text,1,13),'-',''))
ON CONFLICT DO NOTHING;
UPDATE cash_accounts a SET ledger_account_id=c.id
FROM chart_of_accounts c
WHERE a.ledger_account_id IS NULL AND c.tenant_id=a.tenant_id AND c.code='CA-'||replace(substr(a.id::text,1,13),'-','');
UPDATE chart_of_accounts c SET is_system=true,allow_manual_entries=false
WHERE EXISTS(SELECT 1 FROM cash_accounts a WHERE a.ledger_account_id=c.id);
CREATE UNIQUE INDEX IF NOT EXISTS cash_account_name_unique ON cash_accounts(tenant_id,lower(name));

ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS entry_no text;
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS branch_id uuid REFERENCES branches(id);
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'system';
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS posted_by uuid REFERENCES users(id);
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS reversed_by uuid REFERENCES users(id);
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
CREATE UNIQUE INDEX IF NOT EXISTS journal_entry_number_unique ON journal_entries(tenant_id,entry_no) WHERE entry_no IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS journal_request_unique ON journal_entries(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS journal_reference_idx ON journal_entries(tenant_id,reference_type,reference_id);

ALTER TABLE journal_lines ADD COLUMN IF NOT EXISTS memo text;
ALTER TABLE journal_lines DROP CONSTRAINT IF EXISTS journal_lines_check;
ALTER TABLE journal_lines ADD CONSTRAINT journal_line_positive_side CHECK(
  debit>=0 AND credit>=0 AND ((debit>0 AND credit=0) OR (credit>0 AND debit=0))
);

CREATE SEQUENCE IF NOT EXISTS journal_entry_number_seq;
CREATE OR REPLACE FUNCTION assign_journal_entry_no() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.entry_no IS NULL OR btrim(NEW.entry_no)='' THEN
  NEW.entry_no:='JE-'||to_char(COALESCE(NEW.entry_date,current_date),'YYYYMM')||'-'||lpad(nextval('journal_entry_number_seq')::text,7,'0');
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS journal_entry_number_assign ON journal_entries;
CREATE TRIGGER journal_entry_number_assign BEFORE INSERT ON journal_entries FOR EACH ROW EXECUTE FUNCTION assign_journal_entry_no();

CREATE OR REPLACE FUNCTION validate_journal_posting() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total_debit numeric; total_credit numeric; line_count integer; blocked boolean;
BEGIN
 IF NEW.status='posted' AND OLD.status='draft' THEN
  SELECT count(*),COALESCE(sum(debit),0),COALESCE(sum(credit),0)
    INTO line_count,total_debit,total_credit FROM journal_lines WHERE journal_id=NEW.id;
  IF line_count<2 OR total_debit<=0 OR total_debit<>total_credit THEN
   RAISE EXCEPTION 'journal must contain at least two balanced lines before posting';
  END IF;
  SELECT EXISTS(SELECT 1 FROM fiscal_periods p WHERE p.tenant_id=NEW.tenant_id AND NEW.entry_date BETWEEN p.start_date AND p.end_date AND p.status IN('locked','closed')) INTO blocked;
  IF blocked THEN RAISE EXCEPTION 'accounting period is locked or closed'; END IF;
  IF EXISTS(SELECT 1 FROM journal_lines l JOIN chart_of_accounts a ON a.id=l.account_id WHERE l.journal_id=NEW.id AND (a.tenant_id<>NEW.tenant_id OR NOT a.is_active)) THEN
   RAISE EXCEPTION 'journal contains an inactive or cross-workspace account';
  END IF;
  NEW.posted_at=COALESCE(NEW.posted_at,now());
  NEW.posted_by=COALESCE(NEW.posted_by,NEW.created_by);
 END IF;
 IF OLD.status IN('posted','reversed') AND NEW.status=OLD.status AND
    (NEW.entry_date,NEW.description,NEW.reference_type,NEW.reference_id,NEW.tenant_id) IS DISTINCT FROM
    (OLD.entry_date,OLD.description,OLD.reference_type,OLD.reference_id,OLD.tenant_id) THEN
  RAISE EXCEPTION 'posted journal metadata is immutable';
 END IF;
 IF OLD.status='reversed' AND NEW.status<>OLD.status THEN RAISE EXCEPTION 'reversed journal cannot be reopened'; END IF;
 IF OLD.status='posted' AND NEW.status NOT IN('posted','reversed') THEN RAISE EXCEPTION 'posted journal can only be reversed'; END IF;
 NEW.updated_at=now();
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS journal_posting_guard ON journal_entries;
CREATE TRIGGER journal_posting_guard BEFORE UPDATE ON journal_entries FOR EACH ROW EXECUTE FUNCTION validate_journal_posting();

CREATE OR REPLACE FUNCTION validate_posted_journal_line() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE journal_tenant uuid; account_tenant uuid; account_active boolean; period_blocked boolean; entry_day date;
BEGIN
 SELECT tenant_id,entry_date INTO journal_tenant,entry_day FROM journal_entries WHERE id=NEW.journal_id;
 SELECT tenant_id,is_active INTO account_tenant,account_active FROM chart_of_accounts WHERE id=NEW.account_id;
 IF journal_tenant IS DISTINCT FROM account_tenant OR NOT COALESCE(account_active,false) THEN RAISE EXCEPTION 'invalid journal account'; END IF;
 SELECT EXISTS(SELECT 1 FROM fiscal_periods p WHERE p.tenant_id=journal_tenant AND entry_day BETWEEN p.start_date AND p.end_date AND p.status IN('locked','closed')) INTO period_blocked;
 IF period_blocked THEN RAISE EXCEPTION 'accounting period is locked or closed'; END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS journal_line_scope_guard ON journal_lines;
CREATE TRIGGER journal_line_scope_guard BEFORE INSERT OR UPDATE ON journal_lines FOR EACH ROW EXECUTE FUNCTION validate_posted_journal_line();

CREATE TABLE IF NOT EXISTS expense_categories(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES tenants(id),name text NOT NULL,
 ledger_account_id uuid NOT NULL REFERENCES chart_of_accounts(id),is_active boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),UNIQUE(tenant_id,name)
);
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS category_id uuid REFERENCES expense_categories(id);
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS account_id uuid REFERENCES cash_accounts(id);
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS payment_method text CHECK(payment_method IN('cash','bank'));
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS reference text;
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS notes text;
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'draft' CHECK(status IN('draft','finalized','reversed','cancelled'));
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS created_by uuid REFERENCES users(id);
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS finalized_by uuid REFERENCES users(id);
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS finalized_at timestamptz;
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS reversed_by uuid REFERENCES users(id);
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS reversed_at timestamptz;
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS reversal_reason text;
ALTER TABLE expenses ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
CREATE UNIQUE INDEX IF NOT EXISTS expense_request_unique ON expenses(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;

ALTER TABLE cash_transactions ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES tenants(id);
UPDATE cash_transactions t SET tenant_id=a.tenant_id FROM cash_accounts a WHERE t.account_id=a.id AND t.tenant_id IS NULL;
ALTER TABLE cash_transactions ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'finalized' CHECK(status IN('finalized','reversed'));
ALTER TABLE cash_transactions ADD COLUMN IF NOT EXISTS client_request_id text;
ALTER TABLE cash_transactions ADD COLUMN IF NOT EXISTS reference_type text;
ALTER TABLE cash_transactions ADD COLUMN IF NOT EXISTS reference_id uuid;
ALTER TABLE cash_transactions ADD COLUMN IF NOT EXISTS reversed_transaction_id uuid REFERENCES cash_transactions(id);
ALTER TABLE cash_transactions ADD COLUMN IF NOT EXISTS journal_entry_id uuid REFERENCES journal_entries(id);
CREATE UNIQUE INDEX IF NOT EXISTS cash_transaction_request_unique ON cash_transactions(tenant_id,client_request_id) WHERE client_request_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS cash_transaction_tenant_date_idx ON cash_transactions(tenant_id,transaction_date DESC);

CREATE OR REPLACE FUNCTION protect_finalized_expense() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND OLD.status IN('finalized','reversed') THEN RAISE EXCEPTION 'finalized expense cannot be deleted; reverse it'; END IF;
 IF TG_OP='UPDATE' AND OLD.status IN('finalized','reversed') AND NEW.status=OLD.status AND
   (NEW.amount,NEW.expense_date,NEW.category_id,NEW.account_id,NEW.description) IS DISTINCT FROM
   (OLD.amount,OLD.expense_date,OLD.category_id,OLD.account_id,OLD.description) THEN
   RAISE EXCEPTION 'finalized expense is immutable';
 END IF;
 RETURN COALESCE(NEW,OLD);
END $$;
DROP TRIGGER IF EXISTS finalized_expense_guard ON expenses;
CREATE TRIGGER finalized_expense_guard BEFORE UPDATE OR DELETE ON expenses FOR EACH ROW EXECUTE FUNCTION protect_finalized_expense();

CREATE OR REPLACE FUNCTION protect_cash_transaction() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'cashbook entries are immutable; create a reversal'; END $$;
DROP TRIGGER IF EXISTS cash_transaction_immutable ON cash_transactions;
CREATE TRIGGER cash_transaction_immutable BEFORE UPDATE OR DELETE ON cash_transactions FOR EACH ROW EXECUTE FUNCTION protect_cash_transaction();

-- Post party payments to the exact operational cash/bank ledger account selected by the user.
CREATE OR REPLACE FUNCTION post_party_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cash_id uuid; control_id uuid; j uuid; party_kind text; selected_cash_account uuid;
BEGIN
 party_kind:=CASE WHEN TG_TABLE_NAME='customer_payments' THEN 'customer' ELSE 'supplier' END;
 selected_cash_account:=NULLIF(to_jsonb(NEW)->>'account_id','')::uuid;
 IF selected_cash_account IS NOT NULL THEN
  SELECT ledger_account_id INTO cash_id FROM cash_accounts WHERE id=selected_cash_account AND tenant_id=NEW.tenant_id;
 END IF;
 IF cash_id IS NULL THEN
  SELECT id INTO cash_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN NEW.method='bank' THEN '1010' ELSE '1000' END;
 END IF;
 SELECT id INTO control_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN party_kind='customer' THEN '1100' ELSE '2000' END;
 INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,source)
 VALUES(NEW.tenant_id,NEW.payment_date,party_kind||'_payment',NEW.id,initcap(party_kind)||' payment',COALESCE((to_jsonb(NEW)->>'received_by')::uuid,(to_jsonb(NEW)->>'paid_by')::uuid),'posted',now(),'payment') RETURNING id INTO j;
 IF party_kind='customer' THEN
  INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(j,cash_id,NEW.amount);
  INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(j,control_id,NEW.amount);
 ELSE
  INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(j,control_id,NEW.amount);
  INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(j,cash_id,NEW.amount);
 END IF;
 RETURN NEW;
END $$;

-- Purchase finalization also uses the exact payment account while retaining AP for credit.
CREATE OR REPLACE FUNCTION post_procurement_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE inventory_id uuid; payable_id uuid; cash_id uuid; new_journal_id uuid; original_id uuid;
BEGIN
 IF TG_TABLE_NAME='purchases' AND NEW.status='finalized' AND NEW.status IS DISTINCT FROM OLD.status THEN
  SELECT id INTO inventory_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1200';
  SELECT id INTO payable_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='2000';
  IF NEW.account_id IS NOT NULL THEN SELECT ledger_account_id INTO cash_id FROM cash_accounts WHERE id=NEW.account_id AND tenant_id=NEW.tenant_id; END IF;
  IF cash_id IS NULL THEN SELECT id INTO cash_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN NEW.payment_method='bank' THEN '1010' ELSE '1000' END; END IF;
  INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,source)
    VALUES(NEW.tenant_id,NEW.purchase_date,'purchase',NEW.id,'GRN finalized '||NEW.invoice_no,NEW.finalized_by,'posted',now(),'procurement') RETURNING id INTO new_journal_id;
  INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(new_journal_id,inventory_id,NEW.total);
  IF NEW.total-NEW.paid_amount>0 THEN INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(new_journal_id,payable_id,NEW.total-NEW.paid_amount); END IF;
  IF NEW.paid_amount>0 THEN INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(new_journal_id,cash_id,NEW.paid_amount); END IF;
 END IF;
 IF TG_TABLE_NAME='purchases' AND NEW.status='reversed' AND NEW.status IS DISTINCT FROM OLD.status THEN
  SELECT id INTO original_id FROM journal_entries WHERE tenant_id=NEW.tenant_id AND reference_type='purchase' AND reference_id=NEW.id AND status='posted' ORDER BY created_at LIMIT 1 FOR UPDATE;
  IF original_id IS NOT NULL THEN
   INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,reversed_entry_id,reversal_reason,source)
     VALUES(NEW.tenant_id,current_date,'purchase_reversal',NEW.id,'GRN reversed '||NEW.invoice_no,NEW.reversed_by,'posted',now(),original_id,NEW.reversal_reason,'procurement') RETURNING id INTO new_journal_id;
   INSERT INTO journal_lines(journal_id,account_id,debit,credit) SELECT new_journal_id,account_id,credit,debit FROM journal_lines WHERE journal_id=original_id;
   UPDATE journal_entries SET status='reversed',reversed_by=NEW.reversed_by,reversal_reason=NEW.reversal_reason WHERE id=original_id;
  END IF;
 END IF;
 IF TG_TABLE_NAME='purchase_returns' AND NEW.status='finalized' AND NEW.status IS DISTINCT FROM OLD.status THEN
  SELECT id INTO inventory_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1200';
  SELECT id INTO payable_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='2000';
  INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,source)
    VALUES(NEW.tenant_id,NEW.return_date,'purchase_return',NEW.id,'Supplier return '||NEW.return_number,NEW.finalized_by,'posted',now(),'procurement') RETURNING id INTO new_journal_id;
  INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(new_journal_id,payable_id,NEW.total);
  INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(new_journal_id,inventory_id,NEW.total);
 END IF;
 RETURN NEW;
END $$;
