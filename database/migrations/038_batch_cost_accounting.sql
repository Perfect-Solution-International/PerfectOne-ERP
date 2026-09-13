CREATE OR REPLACE FUNCTION post_inventory_loss_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE inventory_id uuid; loss_id uuid; journal_id uuid; amount numeric; unit_cost numeric;
BEGIN
  IF OLD.status<>'finalized' AND NEW.status='finalized' THEN
    SELECT COALESCE(
      CASE WHEN NEW.batch_id IS NOT NULL THEN (SELECT b.unit_cost FROM stock_batches b WHERE b.id=NEW.batch_id) END,
      (SELECT purchase_price FROM products WHERE id=NEW.product_id),0
    ) INTO unit_cost;
    amount:=round(NEW.quantity*unit_cost,2);
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

CREATE OR REPLACE FUNCTION post_stock_movement_adjustment_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE inventory_id uuid; offset_id uuid; journal_id uuid; amount numeric; ref_type text; unit_cost numeric;
BEGIN
  IF NEW.kind NOT IN('expired','adjustment') OR NEW.quantity=0 OR NEW.reference_id IS NULL THEN RETURN NEW; END IF;
  ref_type:=CASE WHEN NEW.kind='expired' THEN 'expiry' ELSE 'stock_adjustment' END;
  IF EXISTS(SELECT 1 FROM journal_entries WHERE tenant_id=NEW.tenant_id AND reference_type=ref_type AND reference_id=NEW.reference_id) THEN RETURN NEW; END IF;
  SELECT COALESCE(
    CASE WHEN NEW.kind='expired' AND NEW.reference_type='batch' THEN (SELECT b.unit_cost FROM stock_batches b WHERE b.id=NEW.reference_id) END,
    (SELECT purchase_price FROM products WHERE id=NEW.product_id),0
  ) INTO unit_cost;
  amount:=round(abs(NEW.quantity)*unit_cost,2);
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
