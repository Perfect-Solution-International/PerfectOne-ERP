ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'posted' CHECK(status IN('draft','posted','reversed'));
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS posted_at timestamptz;
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS reversed_entry_id uuid REFERENCES journal_entries(id);
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS reversal_reason text;
ALTER TABLE sale_items ADD COLUMN IF NOT EXISTS unit_cost numeric(14,2) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS fiscal_periods(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES tenants(id),name text NOT NULL,start_date date NOT NULL,end_date date NOT NULL,status text NOT NULL DEFAULT 'open' CHECK(status IN('open','locked','closed')),locked_by uuid REFERENCES users(id),locked_at timestamptz,CHECK(end_date>=start_date),UNIQUE(tenant_id,name));

INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'1000','Cash','asset' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'1010','Bank','asset' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'1100','Accounts Receivable','asset' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'1200','Inventory','asset' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'2000','Accounts Payable','liability' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'3000','Owner Equity','equity' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'4000','Sales Revenue','income' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'5000','Cost of Goods Sold','expense' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'6100','Operating Expenses','expense' FROM tenants ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION assert_balanced_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE jid uuid; d numeric; c numeric; state text;
BEGIN jid:=COALESCE(NEW.journal_id,OLD.journal_id);SELECT COALESCE(sum(debit),0),COALESCE(sum(credit),0) INTO d,c FROM journal_lines WHERE journal_id=jid;SELECT status INTO state FROM journal_entries WHERE id=jid;IF state='posted' AND d<>c THEN RAISE EXCEPTION 'posted journal % is not balanced: debit %, credit %',jid,d,c;END IF;RETURN NULL;END $$;
DROP TRIGGER IF EXISTS journal_balance_guard ON journal_lines;
CREATE CONSTRAINT TRIGGER journal_balance_guard AFTER INSERT OR UPDATE OR DELETE ON journal_lines DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION assert_balanced_journal();

CREATE OR REPLACE FUNCTION post_sale_revenue() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cash_id uuid; ar_id uuid; revenue_id uuid; j uuid;
BEGIN
 SELECT id INTO cash_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN NEW.payment_method IN('bank','card') THEN '1010' ELSE '1000' END;
 SELECT id INTO ar_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1100';SELECT id INTO revenue_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='4000';
 INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at)VALUES(NEW.tenant_id,'sale',NEW.id,'Sale '||NEW.invoice_no,NEW.cashier_id,'posted',now())RETURNING id INTO j;
 IF NEW.paid_amount>0 THEN INSERT INTO journal_lines(journal_id,account_id,debit)VALUES(j,cash_id,NEW.paid_amount);END IF;IF NEW.balance>0 THEN INSERT INTO journal_lines(journal_id,account_id,debit)VALUES(j,ar_id,NEW.balance);END IF;INSERT INTO journal_lines(journal_id,account_id,credit)VALUES(j,revenue_id,NEW.total);RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS sales_accounting_post ON sales;
CREATE TRIGGER sales_accounting_post AFTER INSERT ON sales FOR EACH ROW EXECUTE FUNCTION post_sale_revenue();

CREATE OR REPLACE FUNCTION post_sale_item_cogs() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tenant uuid; user_id uuid; inventory_id uuid; cogs_id uuid; j uuid; cost numeric;
BEGIN SELECT s.tenant_id,s.cashier_id INTO tenant,user_id FROM sales s WHERE s.id=NEW.sale_id;SELECT id INTO inventory_id FROM chart_of_accounts WHERE tenant_id=tenant AND code='1200';SELECT id INTO cogs_id FROM chart_of_accounts WHERE tenant_id=tenant AND code='5000';cost:=NEW.quantity*COALESCE(NULLIF(NEW.unit_cost,0),(SELECT purchase_price FROM products WHERE id=NEW.product_id),0);UPDATE sale_items SET unit_cost=CASE WHEN NEW.unit_cost=0 THEN cost/NULLIF(NEW.quantity,0) ELSE NEW.unit_cost END WHERE id=NEW.id;IF cost>0 THEN INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at)VALUES(tenant,'sale_cogs',NEW.sale_id,'Cost of goods sold',user_id,'posted',now())RETURNING id INTO j;INSERT INTO journal_lines(journal_id,account_id,debit)VALUES(j,cogs_id,cost);INSERT INTO journal_lines(journal_id,account_id,credit)VALUES(j,inventory_id,cost);END IF;RETURN NEW;END $$;
DROP TRIGGER IF EXISTS sale_items_cogs_post ON sale_items;
CREATE TRIGGER sale_items_cogs_post AFTER INSERT ON sale_items FOR EACH ROW EXECUTE FUNCTION post_sale_item_cogs();

CREATE OR REPLACE FUNCTION post_party_payment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cash_id uuid; control_id uuid; j uuid; party_kind text;
BEGIN party_kind:=CASE WHEN TG_TABLE_NAME='customer_payments' THEN 'customer' ELSE 'supplier' END;SELECT id INTO cash_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN NEW.method='bank' THEN '1010' ELSE '1000' END;SELECT id INTO control_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN party_kind='customer' THEN '1100' ELSE '2000' END;INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at)VALUES(NEW.tenant_id,party_kind||'_payment',NEW.id,initcap(party_kind)||' payment',COALESCE((to_jsonb(NEW)->>'received_by')::uuid,(to_jsonb(NEW)->>'paid_by')::uuid),'posted',now())RETURNING id INTO j;IF party_kind='customer' THEN INSERT INTO journal_lines(journal_id,account_id,debit)VALUES(j,cash_id,NEW.amount);INSERT INTO journal_lines(journal_id,account_id,credit)VALUES(j,control_id,NEW.amount);ELSE INSERT INTO journal_lines(journal_id,account_id,debit)VALUES(j,control_id,NEW.amount);INSERT INTO journal_lines(journal_id,account_id,credit)VALUES(j,cash_id,NEW.amount);END IF;RETURN NEW;END $$;
DROP TRIGGER IF EXISTS customer_payment_accounting ON customer_payments;
CREATE TRIGGER customer_payment_accounting AFTER INSERT ON customer_payments FOR EACH ROW EXECUTE FUNCTION post_party_payment();
DROP TRIGGER IF EXISTS supplier_payment_accounting ON supplier_payments;
CREATE TRIGGER supplier_payment_accounting AFTER INSERT ON supplier_payments FOR EACH ROW EXECUTE FUNCTION post_party_payment();

CREATE OR REPLACE FUNCTION reverse_payment_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE original uuid; reversal uuid; actor uuid;
BEGIN IF OLD.status='finalized' AND NEW.status='reversed' THEN SELECT id INTO original FROM journal_entries WHERE tenant_id=NEW.tenant_id AND reference_type=CASE WHEN TG_TABLE_NAME='customer_payments' THEN 'customer_payment' ELSE 'supplier_payment' END AND reference_id=NEW.id AND status='posted' ORDER BY created_at LIMIT 1;IF original IS NOT NULL THEN actor:=NEW.reversed_by;INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at,reversed_entry_id,reversal_reason)VALUES(NEW.tenant_id,'reversal',NEW.id,'Payment reversal',actor,'posted',now(),original,NEW.reversal_reason)RETURNING id INTO reversal;INSERT INTO journal_lines(journal_id,account_id,debit,credit)SELECT reversal,account_id,credit,debit FROM journal_lines WHERE journal_id=original;UPDATE journal_entries SET status='reversed' WHERE id=original;END IF;END IF;RETURN NEW;END $$;
DROP TRIGGER IF EXISTS customer_payment_reversal_accounting ON customer_payments;
CREATE TRIGGER customer_payment_reversal_accounting AFTER UPDATE OF status ON customer_payments FOR EACH ROW EXECUTE FUNCTION reverse_payment_journal();
DROP TRIGGER IF EXISTS supplier_payment_reversal_accounting ON supplier_payments;
CREATE TRIGGER supplier_payment_reversal_accounting AFTER UPDATE OF status ON supplier_payments FOR EACH ROW EXECUTE FUNCTION reverse_payment_journal();
