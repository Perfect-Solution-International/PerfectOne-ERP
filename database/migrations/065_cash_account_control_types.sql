-- 065: A cash account's ledger account inherits the drawer's own kind.
--
-- Migration 064 parented every CA-* ledger account under Cash and Cash
-- Equivalents and defaulted them all to control_type 'cash'. That mislabels the
-- bank accounts among them, which would put bank balances into the cash figure
-- on any report that groups by control type. Take the kind from cash_accounts,
-- which already records 'cash' or 'bank' per account.

UPDATE chart_of_accounts a
   SET control_type = c.account_type,
       updated_at   = now()
  FROM cash_accounts c
 WHERE c.ledger_account_id = a.id
   AND c.account_type IN ('cash','bank')
   AND a.control_type IS DISTINCT FROM c.account_type;

-- Keep the two in step from now on: creating or re-pointing a cash account
-- stamps the kind onto its ledger account.
CREATE OR REPLACE FUNCTION sync_cash_account_control_type() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.ledger_account_id IS NOT NULL THEN
    UPDATE chart_of_accounts
       SET control_type = NEW.account_type, updated_at = now()
     WHERE id = NEW.ledger_account_id
       AND control_type IS DISTINCT FROM NEW.account_type;
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS cash_accounts_control_type_sync ON cash_accounts;
CREATE TRIGGER cash_accounts_control_type_sync
AFTER INSERT OR UPDATE OF ledger_account_id, account_type ON cash_accounts
FOR EACH ROW EXECUTE FUNCTION sync_cash_account_control_type();
