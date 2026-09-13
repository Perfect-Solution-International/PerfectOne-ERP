INSERT INTO cash_accounts(name,account_type,opening_balance)
SELECT 'Main Cash Drawer','cash',0
WHERE NOT EXISTS(SELECT 1 FROM cash_accounts WHERE name='Main Cash Drawer');
INSERT INTO cash_accounts(name,account_type,opening_balance)
SELECT 'Business Bank Account','bank',0
WHERE NOT EXISTS(SELECT 1 FROM cash_accounts WHERE name='Business Bank Account');
