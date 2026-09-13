CREATE OR REPLACE FUNCTION post_sale_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE cash_id uuid; revenue_id uuid; journal uuid;
BEGIN
 SELECT id INTO cash_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1000';
 SELECT id INTO revenue_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='4000';
 IF cash_id IS NULL OR revenue_id IS NULL THEN RETURN NEW; END IF;
 INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by) VALUES(NEW.tenant_id,'sale',NEW.id,'POS Sale '||NEW.invoice_no,NEW.cashier_id) RETURNING id INTO journal;
 INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(journal,cash_id,NEW.paid_amount);
 INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(journal,revenue_id,NEW.total);
 INSERT INTO sync_outbox(tenant_id,entity_type,payload) VALUES(NEW.tenant_id,'sale',jsonb_build_object('id',NEW.id,'invoice',NEW.invoice_no));
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS sales_accounting_post ON sales;
CREATE TRIGGER sales_accounting_post AFTER INSERT ON sales FOR EACH ROW EXECUTE FUNCTION post_sale_journal();
CREATE OR REPLACE FUNCTION audit_product_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF TG_OP='DELETE' THEN INSERT INTO audit_logs(tenant_id,action,entity_type,entity_id,old_data) VALUES(OLD.tenant_id,TG_OP,'product',OLD.id,to_jsonb(OLD)); RETURN OLD; END IF; INSERT INTO audit_logs(tenant_id,action,entity_type,entity_id,new_data) VALUES(NEW.tenant_id,TG_OP,'product',NEW.id,to_jsonb(NEW)); RETURN NEW; END $$;
DROP TRIGGER IF EXISTS products_audit ON products;
CREATE TRIGGER products_audit AFTER INSERT OR UPDATE OR DELETE ON products FOR EACH ROW EXECUTE FUNCTION audit_product_change();
