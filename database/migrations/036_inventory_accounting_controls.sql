INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'4200','Inventory Gain','income' FROM tenants ON CONFLICT DO NOTHING;
INSERT INTO chart_of_accounts(tenant_id,code,name,account_type) SELECT id,'6200','Inventory Loss','expense' FROM tenants ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION post_inventory_loss_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE inventory_id uuid; loss_id uuid; journal_id uuid; amount numeric;
BEGIN
  IF OLD.status<>'finalized' AND NEW.status='finalized' THEN
    amount:=round(NEW.quantity*COALESCE((SELECT purchase_price FROM products WHERE id=NEW.product_id),0),2);
    IF amount>0 AND NOT EXISTS(SELECT 1 FROM journal_entries WHERE tenant_id=NEW.tenant_id AND reference_type='damage' AND reference_id=NEW.id) THEN
      SELECT id INTO inventory_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1200';
      SELECT id INTO loss_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='6200';
      INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at)
      VALUES(NEW.tenant_id,'damage',NEW.id,'Damaged inventory',NEW.finalized_by,'posted',now()) RETURNING id INTO journal_id;
      INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(journal_id,loss_id,amount);
      INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(journal_id,inventory_id,amount);
    END IF;
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS damaged_items_accounting ON damaged_items;
CREATE TRIGGER damaged_items_accounting AFTER UPDATE OF status ON damaged_items FOR EACH ROW EXECUTE FUNCTION post_inventory_loss_journal();

CREATE OR REPLACE FUNCTION post_stock_movement_adjustment_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE inventory_id uuid; offset_id uuid; journal_id uuid; amount numeric; ref_type text;
BEGIN
  IF NEW.kind NOT IN('expired','adjustment') OR NEW.quantity=0 OR NEW.reference_id IS NULL THEN RETURN NEW; END IF;
  ref_type:=CASE WHEN NEW.kind='expired' THEN 'expiry' ELSE 'stock_adjustment' END;
  IF EXISTS(SELECT 1 FROM journal_entries WHERE tenant_id=NEW.tenant_id AND reference_type=ref_type AND reference_id=NEW.reference_id) THEN RETURN NEW; END IF;
  amount:=round(abs(NEW.quantity)*COALESCE((SELECT purchase_price FROM products WHERE id=NEW.product_id),0),2);
  IF amount<=0 THEN RETURN NEW; END IF;
  SELECT id INTO inventory_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1200';
  SELECT id INTO offset_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code=CASE WHEN NEW.quantity>0 THEN '4200' ELSE '6200' END;
  INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at)
  VALUES(NEW.tenant_id,ref_type,NEW.reference_id,CASE WHEN NEW.kind='expired' THEN 'Expired inventory' ELSE 'Stock adjustment' END,NEW.created_by,'posted',now()) RETURNING id INTO journal_id;
  IF NEW.quantity>0 THEN
    INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(journal_id,inventory_id,amount);
    INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(journal_id,offset_id,amount);
  ELSE
    INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(journal_id,offset_id,amount);
    INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(journal_id,inventory_id,amount);
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS inventory_movement_accounting ON stock_movements;
CREATE TRIGGER inventory_movement_accounting AFTER INSERT ON stock_movements FOR EACH ROW EXECUTE FUNCTION post_stock_movement_adjustment_journal();

CREATE OR REPLACE FUNCTION block_finalized_inventory_record_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status='finalized' THEN RAISE EXCEPTION 'finalized inventory records are immutable; create a reversal adjustment'; END IF;
  RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
DROP TRIGGER IF EXISTS stock_adjustments_finalized_immutable ON stock_adjustments;
CREATE TRIGGER stock_adjustments_finalized_immutable BEFORE UPDATE OR DELETE ON stock_adjustments FOR EACH ROW EXECUTE FUNCTION block_finalized_inventory_record_mutation();
DROP TRIGGER IF EXISTS damaged_items_finalized_immutable ON damaged_items;
CREATE TRIGGER damaged_items_finalized_immutable BEFORE UPDATE OR DELETE ON damaged_items FOR EACH ROW EXECUTE FUNCTION block_finalized_inventory_record_mutation();

CREATE OR REPLACE FUNCTION block_posted_journal_line_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS(SELECT 1 FROM journal_entries WHERE id=OLD.journal_id AND status IN('posted','reversed')) THEN
    RAISE EXCEPTION 'posted journal lines are immutable; reverse the journal';
  END IF;
  RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
DROP TRIGGER IF EXISTS journal_lines_posted_immutable ON journal_lines;
CREATE TRIGGER journal_lines_posted_immutable BEFORE UPDATE OR DELETE ON journal_lines FOR EACH ROW EXECUTE FUNCTION block_posted_journal_line_mutation();

CREATE OR REPLACE FUNCTION block_posted_journal_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN('posted','reversed') THEN RAISE EXCEPTION 'posted journals are immutable; create a reversal'; END IF;
  RETURN OLD;
END $$;
DROP TRIGGER IF EXISTS journal_entries_posted_delete_guard ON journal_entries;
CREATE TRIGGER journal_entries_posted_delete_guard BEFORE DELETE ON journal_entries FOR EACH ROW EXECUTE FUNCTION block_posted_journal_delete();
