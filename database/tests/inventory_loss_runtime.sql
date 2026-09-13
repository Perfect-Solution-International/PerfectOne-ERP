BEGIN;

DO $$
DECLARE
  tenant uuid;
  product uuid;
  other_product uuid;
  actor uuid;
  batch uuid;
  damage uuid;
  before_stock numeric;
  invalid_batch_blocked boolean := false;
  negative_batch_blocked boolean := false;
BEGIN
  SELECT id INTO tenant FROM tenants ORDER BY created_at LIMIT 1;
  SELECT id INTO product FROM products WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO actor FROM users WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  INSERT INTO products(tenant_id,name,product_code,sku,selling_price,stock_quantity)
  VALUES(tenant,'Loss test other product','LOSS-OTHER','LOSS-OTHER',1,0) RETURNING id INTO other_product;
  UPDATE products SET stock_quantity=stock_quantity+5 WHERE id=product RETURNING stock_quantity-5 INTO before_stock;
  INSERT INTO stock_batches(product_id,batch_no,expiry_date,available_qty,unit_cost,status)
  VALUES(product,'LOSS-RUNTIME',current_date-1,5,1,'available') RETURNING id INTO batch;

  BEGIN
    INSERT INTO damaged_items(tenant_id,product_id,batch_id,quantity,reason,status,reported_by)
    VALUES(tenant,other_product,batch,1,'invalid batch scope','draft',actor);
  EXCEPTION WHEN OTHERS THEN
    invalid_batch_blocked := true;
  END;
  IF NOT invalid_batch_blocked THEN
    RAISE EXCEPTION 'damage record accepted a batch from another product';
  END IF;

  INSERT INTO damaged_items(tenant_id,product_id,batch_id,quantity,reason,status,reported_by,client_request_id)
  VALUES(tenant,product,batch,2,'runtime damage','draft',actor,'damage-runtime-1') RETURNING id INTO damage;
  UPDATE stock_batches SET available_qty=available_qty-2 WHERE id=batch;
  UPDATE products SET stock_quantity=stock_quantity-2 WHERE id=product;
  UPDATE damaged_items SET status='finalized',finalized_by=actor,finalized_at=now() WHERE id=damage;
  INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,reference_type,reference_id,balance_after,created_by)
  SELECT product,'damage',-2,tenant,'damage',damage,stock_quantity,actor FROM products WHERE id=product;
  IF (SELECT available_qty FROM stock_batches WHERE id=batch)<>3 OR (SELECT stock_quantity FROM products WHERE id=product)<>before_stock+3 THEN
    RAISE EXCEPTION 'damage batch and product quantities do not reconcile';
  END IF;

  BEGIN
    UPDATE stock_batches SET available_qty=-1 WHERE id=batch;
  EXCEPTION WHEN check_violation THEN
    negative_batch_blocked := true;
  END;
  IF NOT negative_batch_blocked THEN
    RAISE EXCEPTION 'negative batch availability was not blocked';
  END IF;
END;
$$;

ROLLBACK;
