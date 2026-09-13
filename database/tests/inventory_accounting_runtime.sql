BEGIN;
DO $$
DECLARE tenant uuid; product uuid; actor uuid; branch uuid; damage uuid; adjustment uuid; journal uuid; debit_total numeric; credit_total numeric; blocked boolean:=false;
BEGIN
 SELECT id INTO tenant FROM tenants ORDER BY created_at LIMIT 1;
 SELECT id INTO product FROM products WHERE tenant_id=tenant AND purchase_price>0 ORDER BY id LIMIT 1;
 SELECT id INTO actor FROM users WHERE tenant_id=tenant ORDER BY id LIMIT 1;
 SELECT id INTO branch FROM branches WHERE tenant_id=tenant ORDER BY id LIMIT 1;
 INSERT INTO damaged_items(tenant_id,branch_id,product_id,quantity,reason,status,reported_by) VALUES(tenant,branch,product,2,'accounting test','draft',actor) RETURNING id INTO damage;
 UPDATE damaged_items SET status='finalized',finalized_by=actor,finalized_at=now() WHERE id=damage;
 SELECT id INTO journal FROM journal_entries WHERE reference_type='damage' AND reference_id=damage;
 SELECT sum(debit),sum(credit) INTO debit_total,credit_total FROM journal_lines WHERE journal_id=journal;
 IF journal IS NULL OR debit_total<=0 OR debit_total<>credit_total THEN RAISE EXCEPTION 'damage journal missing or unbalanced'; END IF;
 BEGIN UPDATE journal_lines SET debit=debit+1 WHERE journal_id=journal AND debit>0; EXCEPTION WHEN OTHERS THEN blocked:=true; END;
 IF NOT blocked THEN RAISE EXCEPTION 'posted journal line mutation was not blocked'; END IF;
 INSERT INTO stock_adjustments(tenant_id,branch_id,product_id,system_quantity,physical_quantity,quantity,reason,status,adjusted_by) VALUES(tenant,branch,product,10,9,-1,'accounting test','finalized',actor) RETURNING id INTO adjustment;
 INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,created_by) VALUES(product,'adjustment',-1,tenant,branch,'stock_adjustment',adjustment,9,actor);
 SELECT id INTO journal FROM journal_entries WHERE reference_type='stock_adjustment' AND reference_id=adjustment;
 SELECT sum(debit),sum(credit) INTO debit_total,credit_total FROM journal_lines WHERE journal_id=journal;
 IF journal IS NULL OR debit_total<=0 OR debit_total<>credit_total THEN RAISE EXCEPTION 'stock adjustment journal missing or unbalanced'; END IF;
 blocked:=false;
 BEGIN DELETE FROM stock_adjustments WHERE id=adjustment; EXCEPTION WHEN OTHERS THEN blocked:=true; END;
 IF NOT blocked THEN RAISE EXCEPTION 'finalized stock adjustment deletion was not blocked'; END IF;
END $$;
ROLLBACK;
