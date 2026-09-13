BEGIN;
DO $$
DECLARE t uuid; u uuid; c uuid; p uuid; sale_id uuid; payment_id uuid; unbalanced integer; payment_journals integer;
BEGIN
 SELECT id INTO t FROM tenants ORDER BY created_at LIMIT 1;
 SELECT id INTO u FROM users WHERE tenant_id=t ORDER BY created_at LIMIT 1;
 SELECT id INTO c FROM customers WHERE tenant_id=t ORDER BY name LIMIT 1;
 SELECT id INTO p FROM products WHERE tenant_id=t ORDER BY name LIMIT 1;
 UPDATE customers SET balance=100,credit_limit=1000 WHERE id=c;
 INSERT INTO sales(invoice_no,cashier_id,customer_id,total,paid_amount,balance,discount,payment_method,tenant_id,client_request_id)
 VALUES(next_safe_invoice_no(),u,c,100,60,40,0,'cash',t,'accounting-test-sale') RETURNING id INTO sale_id;
 INSERT INTO sale_items(sale_id,product_id,quantity,unit_price,total) VALUES(sale_id,p,1,100,100);
 INSERT INTO customer_payments(tenant_id,customer_id,amount,method,received_by,client_request_id)
 VALUES(t,c,20,'cash',u,'accounting-test-payment') RETURNING id INTO payment_id;
 UPDATE customer_payments SET status='reversed',reversed_by=u,reversed_at=now(),reversal_reason='Automated test' WHERE id=payment_id;
 SELECT count(*) INTO unbalanced FROM journal_entries j JOIN LATERAL(SELECT COALESCE(sum(debit),0)d,COALESCE(sum(credit),0)c FROM journal_lines WHERE journal_id=j.id)x ON true WHERE j.status='posted' AND x.d<>x.c;
 IF unbalanced<>0 THEN RAISE EXCEPTION '% unbalanced posted journals found',unbalanced; END IF;
 SELECT count(*) INTO payment_journals FROM journal_entries WHERE reference_id=payment_id;
 IF payment_journals<>2 THEN RAISE EXCEPTION 'expected original and reversal payment journals, found %',payment_journals;END IF;
 IF NOT EXISTS(SELECT 1 FROM journal_entries WHERE reference_type='sale' AND reference_id=sale_id) THEN RAISE EXCEPTION 'sale revenue journal missing';END IF;
 IF NOT EXISTS(SELECT 1 FROM journal_entries WHERE reference_type='sale_cogs' AND reference_id=sale_id) THEN RAISE EXCEPTION 'sale COGS journal missing';END IF;
END $$;
ROLLBACK;
