INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'1010','Bank','asset' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'1200','Inventory','asset' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'6100','Operating Expenses','expense' FROM tenants ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION post_sale_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cash_id uuid; ar_id uuid; revenue_id uuid; journal uuid;
BEGIN
 SELECT id INTO cash_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN NEW.payment_method='bank' THEN '1010' ELSE '1000' END;
 SELECT id INTO ar_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1100';
 SELECT id INTO revenue_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='4000';
 IF cash_id IS NULL OR ar_id IS NULL OR revenue_id IS NULL THEN RETURN NEW; END IF;
 INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by) VALUES(NEW.tenant_id,'sale',NEW.id,'POS Sale '||NEW.invoice_no,NEW.cashier_id) RETURNING id INTO journal;
 IF NEW.paid_amount>0 THEN INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(journal,cash_id,NEW.paid_amount); END IF;
 IF NEW.total-NEW.paid_amount>0 THEN INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(journal,ar_id,NEW.total-NEW.paid_amount); END IF;
 INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(journal,revenue_id,NEW.total);
 RETURN NEW;
END $$;
