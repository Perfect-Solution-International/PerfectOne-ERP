BEGIN;

DO $$
DECLARE
  tenant uuid;
  supplier uuid;
  product uuid;
  actor uuid;
  po uuid;
  po_line uuid;
  purchase uuid;
  remaining numeric;
  blocked boolean := false;
BEGIN
  SELECT id INTO tenant FROM tenants ORDER BY created_at LIMIT 1;
  SELECT id INTO supplier FROM suppliers WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO product FROM products WHERE tenant_id=tenant ORDER BY id LIMIT 1;
  SELECT id INTO actor FROM users WHERE tenant_id=tenant ORDER BY id LIMIT 1;

  INSERT INTO purchase_orders(tenant_id,po_number,supplier_id,total,status,created_by,client_request_id)
  VALUES(tenant,next_purchase_order_no(),supplier,500,'approved',actor,'po-runtime-create')
  RETURNING id INTO po;
  INSERT INTO purchase_order_items(purchase_order_id,product_id,quantity,unit_cost,total)
  VALUES(po,product,10,50,500)
  RETURNING id INTO po_line;

  INSERT INTO purchases(invoice_no,supplier_id,total,tenant_id,purchase_order_id,status,client_request_id)
  VALUES('PO-RUNTIME-GRN-1',supplier,200,tenant,po,'draft','po-runtime-grn-1')
  RETURNING id INTO purchase;
  INSERT INTO purchase_items(purchase_id,product_id,purchase_order_item_id,quantity,unit_cost,total)
  VALUES(purchase,product,po_line,4,50,200);

  SELECT oi.quantity-oi.received_quantity-COALESCE((SELECT sum(pi.quantity) FROM purchase_items pi JOIN purchases p ON p.id=pi.purchase_id WHERE pi.purchase_order_item_id=oi.id AND p.status='draft'),0)
  INTO remaining FROM purchase_order_items oi WHERE oi.id=po_line;
  IF remaining<>6 THEN
    RAISE EXCEPTION 'draft GRN reservation was not deducted from remaining PO quantity';
  END IF;

  UPDATE purchases SET status='finalized',finalized_by=actor,finalized_at=now() WHERE id=purchase;
  UPDATE purchase_order_items oi SET received_quantity=received_quantity+pi.quantity
  FROM purchase_items pi WHERE pi.purchase_id=purchase AND pi.purchase_order_item_id=oi.id;
  IF (SELECT received_quantity FROM purchase_order_items WHERE id=po_line)<>4 THEN
    RAISE EXCEPTION 'PO received quantity was not updated';
  END IF;

  BEGIN
    UPDATE purchase_order_items SET received_quantity=11 WHERE id=po_line;
  EXCEPTION WHEN check_violation THEN
    blocked := true;
  END;
  IF NOT blocked THEN
    RAISE EXCEPTION 'PO over-receipt constraint did not block invalid quantity';
  END IF;

  UPDATE purchase_order_items SET received_quantity=10 WHERE id=po_line;
  UPDATE purchase_orders p SET status=CASE WHEN NOT EXISTS(SELECT 1 FROM purchase_order_items i WHERE i.purchase_order_id=p.id AND i.received_quantity<i.quantity) THEN 'completed' ELSE 'partially_received' END WHERE p.id=po;
  IF (SELECT status FROM purchase_orders WHERE id=po)<>'completed' THEN
    RAISE EXCEPTION 'fully received purchase order was not completed';
  END IF;
END;
$$;

ROLLBACK;
