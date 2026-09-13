BEGIN;

DO $$
DECLARE
  tenant uuid;
  product uuid;
  branch uuid;
  expired_count integer;
  near_count integer;
  valid_count integer;
  expiry_batch uuid;
  expiry_journal uuid;
  journal_debit numeric;
  journal_credit numeric;
BEGIN
  SELECT id INTO tenant FROM tenants ORDER BY created_at LIMIT 1;
  SELECT id INTO product FROM products WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO branch FROM branches WHERE tenant_id=tenant ORDER BY created_at LIMIT 1;

  INSERT INTO stock_batches(product_id,branch_id,batch_no,expiry_date,available_qty,unit_cost,status)
  VALUES
    (product,branch,'EXP-RUNTIME-OLD',current_date-1,2,10,'available'),
    (product,branch,'EXP-RUNTIME-NEAR',current_date+7,3,10,'available'),
    (product,branch,'EXP-RUNTIME-VALID',current_date+90,4,10,'available');

  SELECT
    count(*) FILTER(WHERE expiry_date<current_date),
    count(*) FILTER(WHERE expiry_date BETWEEN current_date AND current_date+30),
    count(*) FILTER(WHERE expiry_date>current_date+30)
  INTO expired_count,near_count,valid_count
  FROM stock_batches
  WHERE batch_no LIKE 'EXP-RUNTIME-%' AND product_id=product AND branch_id=branch AND available_qty>0;

  IF expired_count<>1 OR near_count<>1 OR valid_count<>1 THEN
    RAISE EXCEPTION 'expiry classification failed: expired %, near %, valid %',expired_count,near_count,valid_count;
  END IF;

  IF NOT EXISTS(
    SELECT 1 FROM roles r JOIN role_permissions rp ON rp.role_id=r.id
    WHERE r.tenant_id=tenant AND r.key='stock_manager' AND rp.permission='expiry.edit' AND rp.enabled
  ) THEN
    RAISE EXCEPTION 'stock manager expiry edit permission was not seeded';
  END IF;

  SELECT id INTO expiry_batch FROM stock_batches WHERE product_id=product AND batch_no='EXP-RUNTIME-OLD';
  INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes)
  VALUES(product,'expired',-2,tenant,branch,'batch',expiry_batch,0,'expiry accounting runtime test');
  SELECT id INTO expiry_journal FROM journal_entries WHERE tenant_id=tenant AND reference_type='expiry' AND reference_id=expiry_batch;
  SELECT sum(debit),sum(credit) INTO journal_debit,journal_credit FROM journal_lines WHERE journal_id=expiry_journal;
  IF expiry_journal IS NULL OR journal_debit<>20 OR journal_credit<>20 THEN
    RAISE EXCEPTION 'expiry journal did not use batch cost: debit %, credit %',journal_debit,journal_credit;
  END IF;
END;
$$;

ROLLBACK;
