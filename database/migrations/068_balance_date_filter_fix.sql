-- 068: An account must not vanish from a balance report because its only
-- movements fall outside the reporting date.
--
-- accounting_trial_balance and accounting_control_reconciliation both kept
-- posted lines in range with
--
--   LEFT JOIN journal_entries j ON ... AND j.entry_date <= p_as_of
--   WHERE j.id IS NOT NULL OR l.id IS NULL
--
-- which reads as "keep rows that matched, and accounts with no lines at all".
-- The case it misses is an account that has lines, none of them in range: the
-- join produces rows with j.id NULL and l.id not null, the WHERE drops every
-- one of them, and the account disappears from the report instead of showing
-- zero. Accounts Receivable and Inventory both dropped out of the
-- reconciliation as at a date before their first sale, which in turn hid them
-- from the opening take-on that reads it.
--
-- Filtering inside the aggregate keeps every account and counts only the lines
-- that belong in the period.

CREATE OR REPLACE FUNCTION accounting_trial_balance(
  p_tenant uuid,
  p_as_of  date DEFAULT CURRENT_DATE,
  p_from   date DEFAULT NULL,
  p_branch uuid DEFAULT NULL
) RETURNS TABLE(
  account_id uuid, code text, name text, account_type text,
  is_group boolean, parent_id uuid, sort_order integer,
  debit numeric, credit numeric, balance numeric
) LANGUAGE sql STABLE AS $$
  SELECT a.id, a.code, a.name, a.account_type, a.is_group, a.parent_id, a.sort_order,
         COALESCE(sum(l.debit)  FILTER (WHERE j.id IS NOT NULL), 0) AS debit,
         COALESCE(sum(l.credit) FILTER (WHERE j.id IS NOT NULL), 0) AS credit,
         COALESCE(sum(account_signed_balance(a.account_type, l.debit, l.credit))
                  FILTER (WHERE j.id IS NOT NULL), 0) AS balance
    FROM chart_of_accounts a
    LEFT JOIN journal_lines l ON l.account_id = a.id
    LEFT JOIN journal_entries j
           ON j.id = l.journal_id
          AND j.status = 'posted'
          AND j.entry_date <= p_as_of
          AND (p_from IS NULL OR j.entry_date >= p_from)
          AND (p_branch IS NULL OR j.branch_id = p_branch)
   WHERE a.tenant_id = p_tenant
   GROUP BY a.id
   ORDER BY a.sort_order, a.code
$$;

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
           COALESCE(sum(account_signed_balance(c.account_type, l.debit, l.credit))
                    FILTER (WHERE j.id IS NOT NULL), 0) AS ledger
      FROM control c
      LEFT JOIN journal_lines l ON l.account_id = c.id
      LEFT JOIN journal_entries j
             ON j.id = l.journal_id AND j.status = 'posted' AND j.entry_date <= p_as_of
     GROUP BY c.id
  ), sub AS (
    SELECT c.id, COALESCE(sum(cu.balance), 0) AS amount, 'customer balances'::text AS note
      FROM control c
      LEFT JOIN customers cu ON cu.tenant_id = p_tenant
     WHERE c.control_type = 'customer' AND c.account_type = 'asset'
     GROUP BY c.id

    UNION ALL
    SELECT c.id, COALESCE(sum(s.balance), 0), 'supplier balances'
      FROM control c
      LEFT JOIN suppliers s ON s.tenant_id = p_tenant
     WHERE c.control_type = 'supplier' AND c.account_type = 'liability'
     GROUP BY c.id

    UNION ALL
    SELECT c.id, NULL, 'no subledger'
      FROM control c
     WHERE (c.control_type = 'customer' AND c.account_type = 'liability')
        OR (c.control_type = 'supplier' AND c.account_type = 'asset')

    UNION ALL
    SELECT c.id, COALESCE(sum(p.stock_quantity * p.purchase_price), 0), 'stock at cost'
      FROM control c
      LEFT JOIN products p ON p.tenant_id = p_tenant
     WHERE c.control_type = 'inventory' AND c.code <> '1210'
     GROUP BY c.id

    UNION ALL
    SELECT c.id, NULL, 'no subledger'
      FROM control c
     WHERE c.control_type = 'inventory' AND c.code = '1210'

    UNION ALL
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
    SELECT c.id, NULL, 'no subledger'
      FROM control c
     WHERE c.control_type IN ('cash','bank')
       AND NOT EXISTS (SELECT 1 FROM cash_accounts ca WHERE ca.ledger_account_id = c.id)

    UNION ALL
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
