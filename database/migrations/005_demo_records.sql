DO $$
DECLARE admin_id uuid; customer_id uuid; supplier_id uuid; rice_id uuid; milk_id uuid; oil_id uuid; purchase_id uuid; sale_id uuid;
BEGIN
 SELECT id INTO admin_id FROM users WHERE email='admin@grocerly.local';
 SELECT id INTO customer_id FROM customers WHERE phone='0771234567';
 SELECT id INTO supplier_id FROM suppliers WHERE name='Ceylon Distributors';
 SELECT id INTO rice_id FROM products WHERE sku='RICE-001'; SELECT id INTO milk_id FROM products WHERE sku='MILK-400'; SELECT id INTO oil_id FROM products WHERE sku='OIL-001';
 IF NOT EXISTS(SELECT 1 FROM purchases WHERE invoice_no='PUR-DEMO-001') THEN
  INSERT INTO purchases(id,invoice_no,supplier_id,total,paid_amount) VALUES(gen_random_uuid(),'PUR-DEMO-001',supplier_id,18800,18800) RETURNING id INTO purchase_id;
  INSERT INTO purchase_items(purchase_id,product_id,quantity,unit_cost,total) VALUES(purchase_id,rice_id,40,205,8200),(purchase_id,milk_id,5,1120,5600),(purchase_id,oil_id,6,750,4500);
 END IF;
 IF NOT EXISTS(SELECT 1 FROM sales WHERE invoice_no='INV-DEMO-001') THEN
  INSERT INTO sales(id,invoice_no,cashier_id,customer_id,total,paid_amount,balance) VALUES(gen_random_uuid(),'INV-DEMO-001',admin_id,customer_id,2660,2660,0) RETURNING id INTO sale_id;
  INSERT INTO sale_items(sale_id,product_id,quantity,unit_price,total) VALUES(sale_id,rice_id,4,245,980),(sale_id,milk_id,1,1280,1280),(sale_id,oil_id,1,400,400);
 END IF;
 IF NOT EXISTS(SELECT 1 FROM promotions WHERE product_id=oil_id) THEN INSERT INTO promotions(product_id,kind,value,start_date,end_date) VALUES(oil_id,'percentage',10,current_date-1,current_date+14); END IF;
 IF NOT EXISTS(SELECT 1 FROM damaged_items WHERE product_id=milk_id) THEN INSERT INTO damaged_items(product_id,quantity,reason,reported_by) VALUES(milk_id,1,'Damaged packaging',admin_id); END IF;
END $$;
