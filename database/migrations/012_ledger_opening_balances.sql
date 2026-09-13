-- Preserve pre-existing contact balances as auditable opening entries.
INSERT INTO customer_ledger(customer_id,entry_type,debit,credit,balance,created_at)
SELECT c.id,'opening_balance',GREATEST(c.balance-COALESCE(l.net,0),0),GREATEST(COALESCE(l.net,0)-c.balance,0),c.balance,'2000-01-01T00:00:00Z'
FROM customers c
LEFT JOIN (SELECT customer_id,SUM(debit-credit) net FROM customer_ledger GROUP BY customer_id) l ON l.customer_id=c.id
WHERE COALESCE(l.net,0)<>c.balance;

INSERT INTO supplier_ledger(supplier_id,entry_type,debit,credit,balance,created_at)
SELECT s.id,'opening_balance',GREATEST(s.balance-COALESCE(l.net,0),0),GREATEST(COALESCE(l.net,0)-s.balance,0),s.balance,'2000-01-01T00:00:00Z'
FROM suppliers s
LEFT JOIN (SELECT supplier_id,SUM(debit-credit) net FROM supplier_ledger GROUP BY supplier_id) l ON l.supplier_id=s.id
WHERE COALESCE(l.net,0)<>s.balance;
