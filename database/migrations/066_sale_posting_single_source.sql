-- 066: One place decides what a sale does to the ledger.
--
-- Sale revenue was posted in two places that did not agree:
--
--   * post_sale_revenue, a trigger on sales, handled every payment method
--     except 'split', resolved accounts by hard-coded codes ('1000', '1010',
--     '1100', '4000'), recorded no branch, and credited the invoice total to
--     Sales Revenue. Because the total is already net of discount, the ledger
--     understated gross sales and the discount given never appeared at all.
--   * postSplitSaleJournal in Go handled split tenders, with the same defects.
--
-- Posting now happens in the Go engine (postJournalTx with the rules in
-- posting_rules.go), which credits gross revenue, debits the discount to its
-- own contra account, records the branch, and resolves accounts through
-- account_roles. This migration removes the trigger that competed with it and
-- replaces it with a guard: a sale that reaches commit without a posted
-- journal is rejected, so no code path can quietly skip the ledger.

DROP TRIGGER IF EXISTS sales_accounting_post ON sales;
DROP FUNCTION IF EXISTS post_sale_revenue();

CREATE OR REPLACE FUNCTION require_sale_journal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM journal_entries
     WHERE tenant_id = NEW.tenant_id
       AND reference_type = 'sale'
       AND reference_id = NEW.id
       AND status IN ('posted','reversed')
  ) THEN
    RAISE EXCEPTION 'sale % was committed without a revenue journal', NEW.invoice_no
      USING HINT = 'post through the accounting engine in the same transaction';
  END IF;
  RETURN NEW;
END $$;

-- DEFERRABLE INITIALLY DEFERRED so the check runs at commit, by which time the
-- handler has written the journal. An immediate trigger would fire while the
-- sale row was still the only thing inserted.
DROP TRIGGER IF EXISTS sales_require_journal ON sales;
CREATE CONSTRAINT TRIGGER sales_require_journal
AFTER INSERT ON sales
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION require_sale_journal();

COMMENT ON FUNCTION require_sale_journal() IS
  'Rejects a sale that commits without a revenue journal. The rule itself lives in the Go posting engine.';
