-- 067: One reconciliation between the general ledger and the subledgers.
--
-- Two endpoints reconciled control accounts, each with its own copy of the
-- query and each naming accounts by hard-coded code ('1100', '1200', '2000').
-- They also disagreed: one filtered stock valuation to active products, the
-- other did not, so the same inventory difference was reported two ways.
--
-- The rule now lives in one function driven by chart_of_accounts.control_type,
-- so adding a control account is a data change rather than a code change.
--
-- Sign convention: every figure is the account's natural balance, positive.
-- Receivables and inventory are debit balances; payables are credit balances,
-- reported positive so "GL says 2,000 owed, the supplier list says 2,000 owed"
-- reads the way an accountant would say it.

CREATE OR REPLACE FUNCTION accounting_control_reconciliation(
  p_tenant uuid,
  p_as_of  date DEFAULT CURRENT_DATE
) RETURNS TABLE(
  account_id   uuid,
  code         text,
  name         text,
  control_type text,
  ledger       numeric,
  subledger    numeric,
  difference   numeric,
  balanced     boolean,
  note         text
) LANGUAGE sql STABLE AS $$
  WITH control AS (
    SELECT a.id, a.code, a.name, a.control_type, a.account_type
      FROM chart_of_accounts a
     WHERE a.tenant_id = p_tenant
       AND a.control_type IS NOT NULL
       AND NOT a.is_group
  ), gl AS (
    SELECT c.id,
           COALESCE(sum(account_signed_balance(c.account_type, l.debit, l.credit)), 0) AS ledger
      FROM control c
      LEFT JOIN journal_lines l ON l.account_id = c.id
      LEFT JOIN journal_entries j
             ON j.id = l.journal_id AND j.status = 'posted' AND j.entry_date <= p_as_of
     WHERE j.id IS NOT NULL OR l.id IS NULL
     GROUP BY c.id
  ), sub AS (
    -- customers owe us; the customer list is the receivable subledger
    SELECT c.id, COALESCE(sum(cu.balance), 0) AS amount, 'customer balances'::text AS note
      FROM control c
      LEFT JOIN customers cu ON cu.tenant_id = p_tenant
     WHERE c.control_type = 'customer' AND c.account_type = 'asset'
     GROUP BY c.id

    UNION ALL
    -- we owe suppliers; a payable is a credit balance, reported positive
    SELECT c.id, COALESCE(sum(s.balance), 0), 'supplier balances'
      FROM control c
      LEFT JOIN suppliers s ON s.tenant_id = p_tenant
     WHERE c.control_type = 'supplier' AND c.account_type = 'liability'
     GROUP BY c.id

    UNION ALL
    -- customer advances and supplier advances have no subledger of their own
    SELECT c.id, NULL, 'no subledger'
      FROM control c
     WHERE (c.control_type = 'customer' AND c.account_type = 'liability')
        OR (c.control_type = 'supplier' AND c.account_type = 'asset')

    UNION ALL
    -- stock on hand valued at cost. Inactive products are included on purpose:
    -- deactivating a product does not remove its value from the balance sheet.
    SELECT c.id, COALESCE(sum(p.stock_quantity * p.purchase_price), 0), 'stock at cost'
      FROM control c
      LEFT JOIN products p ON p.tenant_id = p_tenant
     WHERE c.control_type = 'inventory' AND c.code <> '1210'
     GROUP BY c.id

    UNION ALL
    -- goods in transit is carried by the transfer documents, not the stock list
    SELECT c.id, NULL, 'no subledger'
      FROM control c
     WHERE c.control_type = 'inventory' AND c.code = '1210'

    UNION ALL
    -- each drawer or bank account reconciles to its own cashbook
    SELECT c.id,
           ca.opening_balance + COALESCE(sum(
             CASE WHEN x.transaction_type IN ('deposit','income','transfer_in')
                  THEN x.amount ELSE -x.amount END
           ) FILTER (WHERE x.status = 'finalized' AND x.created_at::date <= p_as_of), 0),
           'cashbook'
      FROM control c
      JOIN cash_accounts ca ON ca.ledger_account_id = c.id AND ca.tenant_id = p_tenant
      LEFT JOIN cash_transactions x ON x.account_id = ca.id
     WHERE c.control_type IN ('cash','bank')
     GROUP BY c.id, ca.opening_balance

    UNION ALL
    -- the generic cash and bank accounts are summaries, not a single drawer
    SELECT c.id, NULL, 'no subledger'
      FROM control c
     WHERE c.control_type IN ('cash','bank')
       AND NOT EXISTS (SELECT 1 FROM cash_accounts ca WHERE ca.ledger_account_id = c.id)

    UNION ALL
    -- tax control accounts reconcile to the tax return, not to a table here
    SELECT c.id, NULL, 'no subledger'
      FROM control c
     WHERE c.control_type IN ('tax_input','tax_output')
  )
  SELECT c.id, c.code, c.name, c.control_type,
         round(gl.ledger, 2),
         round(sub.amount, 2),
         round(gl.ledger - COALESCE(sub.amount, gl.ledger), 2),
         round(gl.ledger - COALESCE(sub.amount, gl.ledger), 2) = 0,
         sub.note
    FROM control c
    JOIN gl  ON gl.id  = c.id
    JOIN sub ON sub.id = c.id
   ORDER BY c.code
$$;

COMMENT ON FUNCTION accounting_control_reconciliation(uuid, date) IS
  'Compares each control account in the general ledger with the subledger that owns it.';

-- Accounts whose control balance disagrees with their subledger, for the
-- discrepancy alerts on the accounting dashboard.
CREATE OR REPLACE FUNCTION accounting_discrepancies(
  p_tenant uuid,
  p_as_of  date DEFAULT CURRENT_DATE
) RETURNS TABLE(kind text, reference text, detail text, amount numeric)
LANGUAGE sql STABLE AS $$
  SELECT 'control_account', code, name || ' differs from its ' || note, difference
    FROM accounting_control_reconciliation(p_tenant, p_as_of)
   WHERE NOT balanced

  UNION ALL
  SELECT 'unbalanced_journal', COALESCE(entry_no, id::text),
         'posted journal does not balance', difference
    FROM unbalanced_journals
   WHERE tenant_id = p_tenant

  UNION ALL
  SELECT 'accounting_equation', 'assets = liabilities + equity',
         'the ledger as a whole does not balance', difference
    FROM accounting_equation_check(p_tenant, p_as_of)
   WHERE NOT balanced

  UNION ALL
  SELECT 'unposted_sale', s.invoice_no, 'sale has no revenue journal', s.total
    FROM sales s
   WHERE s.tenant_id = p_tenant
     AND s.created_at::date <= p_as_of
     AND NOT EXISTS (
       SELECT 1 FROM journal_entries j
        WHERE j.tenant_id = s.tenant_id
          AND j.reference_type = 'sale'
          AND j.reference_id = s.id
     )
$$;

COMMENT ON FUNCTION accounting_discrepancies(uuid, date) IS
  'Every accounting inconsistency worth alerting on, in one list.';
