BEGIN;

DO $$
DECLARE
  tenant uuid;
  other_tenant uuid;
  product uuid;
  actor uuid;
  source_branch uuid;
  destination_branch uuid;
  foreign_branch uuid;
  transfer uuid;
  global_before numeric;
  invalid_tenant_blocked boolean := false;
  negative_blocked boolean := false;
BEGIN
  SELECT id INTO tenant FROM tenants ORDER BY created_at LIMIT 1;
  SELECT id INTO product FROM products WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO actor FROM users WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO source_branch FROM branches WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  INSERT INTO branches(tenant_id,name,code) VALUES(tenant,'Transfer runtime destination','TRANSFER-RUNTIME') RETURNING id INTO destination_branch;
  INSERT INTO branch_stock(product_id,branch_id,quantity) VALUES(product,source_branch,10)
  ON CONFLICT(product_id,branch_id) DO UPDATE SET quantity=10;
  SELECT stock_quantity INTO global_before FROM products WHERE id=product;

  INSERT INTO stock_transfers(tenant_id,from_branch_id,to_branch_id,product_id,quantity,status,transferred_by,client_request_id)
  VALUES(tenant,source_branch,destination_branch,product,4,'approved',actor,'transfer-runtime-1') RETURNING id INTO transfer;
  UPDATE branch_stock SET quantity=quantity-4 WHERE product_id=product AND branch_id=source_branch;
  UPDATE stock_transfers SET status='dispatched',dispatched_by=actor,dispatched_at=now() WHERE id=transfer;
  INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,created_by)
  VALUES(product,'transfer_out',-4,tenant,source_branch,'stock_transfer',transfer,global_before,actor);
  INSERT INTO branch_stock(product_id,branch_id,quantity) VALUES(product,destination_branch,4)
  ON CONFLICT(product_id,branch_id) DO UPDATE SET quantity=branch_stock.quantity+4;
  UPDATE stock_transfers SET status='received',received_by=actor,received_at=now() WHERE id=transfer;
  INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,created_by)
  VALUES(product,'transfer_in',4,tenant,destination_branch,'stock_transfer',transfer,global_before,actor);

  IF (SELECT quantity FROM branch_stock WHERE product_id=product AND branch_id=source_branch)<>6
     OR (SELECT quantity FROM branch_stock WHERE product_id=product AND branch_id=destination_branch)<>4 THEN
    RAISE EXCEPTION 'branch quantities do not reconcile after transfer';
  END IF;
  IF (SELECT stock_quantity FROM products WHERE id=product)<>global_before THEN
    RAISE EXCEPTION 'internal transfer changed global product stock';
  END IF;
  IF (SELECT count(*) FROM stock_movements WHERE reference_type='stock_transfer' AND reference_id=transfer)<>2 THEN
    RAISE EXCEPTION 'transfer movement ledger is incomplete';
  END IF;

  BEGIN
    UPDATE branch_stock SET quantity=-1 WHERE product_id=product AND branch_id=source_branch;
  EXCEPTION WHEN check_violation THEN
    negative_blocked := true;
  END;
  IF NOT negative_blocked THEN
    RAISE EXCEPTION 'negative branch stock was not blocked';
  END IF;

  INSERT INTO tenants(name,slug) VALUES('Foreign runtime tenant','foreign-runtime-tenant') RETURNING id INTO other_tenant;
  INSERT INTO branches(tenant_id,name,code) VALUES(other_tenant,'Foreign branch','FOREIGN-RUNTIME') RETURNING id INTO foreign_branch;
  BEGIN
    INSERT INTO stock_transfers(tenant_id,from_branch_id,to_branch_id,product_id,quantity,status)
    VALUES(tenant,source_branch,foreign_branch,product,1,'draft');
  EXCEPTION WHEN OTHERS THEN
    invalid_tenant_blocked := true;
  END;
  IF NOT invalid_tenant_blocked THEN
    RAISE EXCEPTION 'cross-tenant stock transfer was not blocked';
  END IF;
END;
$$;

ROLLBACK;
