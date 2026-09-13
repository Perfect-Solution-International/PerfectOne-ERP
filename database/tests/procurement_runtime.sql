BEGIN;

DO $$
DECLARE
  tenant uuid;
  supplier uuid;
  product uuid;
  actor uuid;
  account uuid;
  purchase uuid;
  original_journal uuid;
  reversal_journal uuid;
  debit_total numeric;
  credit_total numeric;
  duplicate_blocked boolean := false;
BEGIN
  SELECT id INTO tenant FROM tenants ORDER BY created_at LIMIT 1;
  SELECT id INTO supplier FROM suppliers WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO product FROM products WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO actor FROM users WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO account FROM cash_accounts WHERE tenant_id=tenant AND account_type='cash' ORDER BY id LIMIT 1;

  INSERT INTO purchases(invoice_no,supplier_id,total,paid_amount,tenant_id,purchase_date,payment_method,account_id,status,client_request_id)
  VALUES('PROCUREMENT-RUNTIME-001',supplier,100,20,tenant,current_date,'cash',account,'draft','procurement-runtime-001')
  RETURNING id INTO purchase;
  INSERT INTO purchase_items(purchase_id,product_id,quantity,unit_cost,total)
  VALUES(purchase,product,2,50,100);

  UPDATE purchases SET status='finalized',finalized_by=actor,finalized_at=now() WHERE id=purchase;
  SELECT id INTO original_journal FROM journal_entries WHERE reference_type='purchase' AND reference_id=purchase;
  SELECT sum(debit),sum(credit) INTO debit_total,credit_total FROM journal_lines WHERE journal_id=original_journal;
  IF original_journal IS NULL OR debit_total<>100 OR credit_total<>100 THEN
    RAISE EXCEPTION 'finalized GRN journal is missing or unbalanced';
  END IF;

  UPDATE purchases SET status='reversed',reversed_by=actor,reversed_at=now(),reversal_reason='runtime verification' WHERE id=purchase;
  SELECT id INTO reversal_journal FROM journal_entries WHERE reference_type='purchase_reversal' AND reference_id=purchase;
  SELECT sum(debit),sum(credit) INTO debit_total,credit_total FROM journal_lines WHERE journal_id=reversal_journal;
  IF reversal_journal IS NULL OR debit_total<>100 OR credit_total<>100 THEN
    RAISE EXCEPTION 'GRN reversal journal is missing or unbalanced';
  END IF;
  IF NOT EXISTS(SELECT 1 FROM journal_entries WHERE id=original_journal AND status='reversed') THEN
    RAISE EXCEPTION 'original GRN journal was not marked reversed';
  END IF;
  IF NOT EXISTS(SELECT 1 FROM journal_entries WHERE id=reversal_journal AND reversed_entry_id=original_journal) THEN
    RAISE EXCEPTION 'GRN reversal is not linked to its original journal';
  END IF;

  BEGIN
    INSERT INTO purchases(invoice_no,supplier_id,total,paid_amount,tenant_id,status)
    VALUES('PROCUREMENT-RUNTIME-001',supplier,1,0,tenant,'draft');
  EXCEPTION WHEN unique_violation THEN
    duplicate_blocked := true;
  END;
  IF NOT duplicate_blocked THEN
    RAISE EXCEPTION 'duplicate supplier invoice was not blocked';
  END IF;
END;
$$;

ROLLBACK;
