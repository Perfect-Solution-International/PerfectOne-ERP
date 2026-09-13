-- 064: Accounting core structure.
--
-- The chart of accounts was a flat list of 14 accounts with no hierarchy, no
-- control-account marking, and no place to record tax, discounts or the
-- individual inventory-loss reasons. Go code resolved accounts by hard-coded
-- code literals ('1100', '4000', ...), so posting rules were spread across six
-- files with no single source of truth.
--
-- This migration adds:
--   * account grouping (parent headers that cannot themselves be posted to)
--   * contra accounts, so sales returns and discounts reduce revenue correctly
--   * control_type, marking the accounts that a subledger owns
--   * the accounts needed for tax, discounts, freight and loss reasons
--   * account_roles: a semantic name -> account map that the posting engine
--     resolves, replacing hard-coded codes
--   * trial-balance and accounting-equation validation functions
--
-- Existing account codes and ids are left untouched; every new account uses a
-- free code, so already-posted journals keep their meaning.

-- ---------------------------------------------------------------- structure

ALTER TABLE chart_of_accounts
  ADD COLUMN IF NOT EXISTS is_group    boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS is_contra   boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS control_type text,
  ADD COLUMN IF NOT EXISTS sort_order  integer NOT NULL DEFAULT 1000;

UPDATE chart_of_accounts
   SET normal_balance = CASE WHEN account_type IN ('asset','expense') THEN 'debit' ELSE 'credit' END
 WHERE normal_balance IS NULL;

ALTER TABLE chart_of_accounts ALTER COLUMN normal_balance SET NOT NULL;

ALTER TABLE chart_of_accounts DROP CONSTRAINT IF EXISTS chart_of_accounts_control_type_check;
ALTER TABLE chart_of_accounts ADD CONSTRAINT chart_of_accounts_control_type_check
  CHECK (control_type IS NULL OR control_type IN
        ('customer','supplier','inventory','cash','bank','tax_input','tax_output'));

-- A normal account carries its account type's natural balance; a contra
-- account carries the opposite, which is what makes "sales returns" reduce
-- income instead of adding to it.
ALTER TABLE chart_of_accounts DROP CONSTRAINT IF EXISTS chart_of_accounts_normal_balance_matches_type;
ALTER TABLE chart_of_accounts ADD CONSTRAINT chart_of_accounts_normal_balance_matches_type CHECK (
  normal_balance = CASE
    WHEN (account_type IN ('asset','expense')) <> is_contra THEN 'debit'
    ELSE 'credit'
  END
);

CREATE INDEX IF NOT EXISTS chart_of_accounts_parent_idx ON chart_of_accounts(tenant_id, parent_id);
CREATE INDEX IF NOT EXISTS chart_of_accounts_control_idx ON chart_of_accounts(tenant_id, control_type)
  WHERE control_type IS NOT NULL;

-- ------------------------------------------------- group accounts are headers

CREATE OR REPLACE FUNCTION reject_posting_to_group_account() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE grouped boolean;
BEGIN
  SELECT is_group INTO grouped FROM chart_of_accounts WHERE id = NEW.account_id;
  IF COALESCE(grouped, false) THEN
    RAISE EXCEPTION 'account is a heading and cannot be posted to; choose a detail account';
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS journal_lines_group_account_guard ON journal_lines;
CREATE TRIGGER journal_lines_group_account_guard
BEFORE INSERT OR UPDATE ON journal_lines
FOR EACH ROW EXECUTE FUNCTION reject_posting_to_group_account();

-- A heading may not sit under one of its own descendants.
CREATE OR REPLACE FUNCTION reject_account_cycle() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE walker uuid; hops integer := 0;
BEGIN
  IF NEW.parent_id IS NULL THEN RETURN NEW; END IF;
  IF NEW.parent_id = NEW.id THEN RAISE EXCEPTION 'an account cannot be its own parent'; END IF;
  walker := NEW.parent_id;
  WHILE walker IS NOT NULL AND hops < 64 LOOP
    IF walker = NEW.id THEN RAISE EXCEPTION 'account hierarchy cannot contain a cycle'; END IF;
    SELECT parent_id INTO walker FROM chart_of_accounts WHERE id = walker;
    hops := hops + 1;
  END LOOP;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS chart_of_accounts_cycle_guard ON chart_of_accounts;
CREATE TRIGGER chart_of_accounts_cycle_guard
BEFORE INSERT OR UPDATE OF parent_id ON chart_of_accounts
FOR EACH ROW EXECUTE FUNCTION reject_account_cycle();

-- ------------------------------------------------------------- posting roles

-- The posting engine asks for a role ("ar_control", "tax_output") rather than
-- an account code, so the chart can be renumbered or extended per tenant
-- without touching application code.
CREATE TABLE IF NOT EXISTS account_roles (
  tenant_id  uuid NOT NULL REFERENCES tenants(id),
  role       text NOT NULL,
  account_id uuid NOT NULL REFERENCES chart_of_accounts(id),
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid REFERENCES users(id),
  PRIMARY KEY (tenant_id, role)
);

COMMENT ON TABLE account_roles IS
  'Maps a semantic posting role to the account it posts to for one tenant.';

-- ------------------------------------------------------- seed the full chart

-- seed_accounts holds the target chart. A row whose code already exists is
-- left in place apart from its position in the tree, so the 14 accounts that
-- already carry posted history keep their ids.
CREATE TEMP TABLE seed_accounts(
  code text, name text, account_type text, normal_balance text,
  is_group boolean, is_contra boolean, control_type text,
  sort_order integer, parent_code text, is_system boolean, allow_manual boolean
);

INSERT INTO seed_accounts VALUES
  ('1'   ,'Assets'                     ,'asset'    ,'debit' ,true ,false,NULL        ,100,NULL ,true ,false),
  ('11'  ,'Current Assets'             ,'asset'    ,'debit' ,true ,false,NULL        ,110,'1'  ,true ,false),
  ('111' ,'Cash and Cash Equivalents'  ,'asset'    ,'debit' ,true ,false,NULL        ,111,'11' ,true ,false),
  ('1000','Cash'                       ,'asset'    ,'debit' ,false,false,'cash'      ,112,'111',true ,false),
  ('1120','Petty Cash'                 ,'asset'    ,'debit' ,false,false,'cash'      ,113,'111',true ,false),
  ('1010','Bank'                       ,'asset'    ,'debit' ,false,false,'bank'      ,114,'111',true ,false),
  ('1130','Cheques in Hand'            ,'asset'    ,'debit' ,false,false,NULL        ,115,'111',true ,true ),
  ('112' ,'Receivables'                ,'asset'    ,'debit' ,true ,false,NULL        ,120,'11' ,true ,false),
  ('1100','Accounts Receivable'        ,'asset'    ,'debit' ,false,false,'customer'  ,121,'112',true ,false),
  ('1300','Supplier Advances'          ,'asset'    ,'debit' ,false,false,'supplier'  ,122,'112',true ,false),
  ('113' ,'Inventory Assets'           ,'asset'    ,'debit' ,true ,false,NULL        ,130,'11' ,true ,false),
  ('1200','Inventory'                  ,'asset'    ,'debit' ,false,false,'inventory' ,131,'113',true ,false),
  ('1210','Goods in Transit'           ,'asset'    ,'debit' ,false,false,'inventory' ,132,'113',true ,false),
  ('114' ,'Tax Assets'                 ,'asset'    ,'debit' ,true ,false,NULL        ,140,'11' ,true ,false),
  ('1400','Input Tax Receivable'       ,'asset'    ,'debit' ,false,false,'tax_input' ,141,'114',true ,false),
  ('9'   ,'Suspense'                   ,'asset'    ,'debit' ,true ,false,NULL        ,190,'1'  ,true ,false),
  ('9000','Suspense Account'           ,'asset'    ,'debit' ,false,false,NULL        ,191,'9'  ,true ,true ),

  ('2'   ,'Liabilities'                ,'liability','credit',true ,false,NULL        ,200,NULL ,true ,false),
  ('21'  ,'Current Liabilities'        ,'liability','credit',true ,false,NULL        ,210,'2'  ,true ,false),
  ('211' ,'Trade Payables'             ,'liability','credit',true ,false,NULL        ,211,'21' ,true ,false),
  ('2000','Accounts Payable'           ,'liability','credit',false,false,'supplier'  ,212,'211',true ,false),
  ('2010','Goods Received Not Invoiced','liability','credit',false,false,NULL        ,213,'211',true ,false),
  ('212' ,'Other Current Liabilities'  ,'liability','credit',true ,false,NULL        ,220,'21' ,true ,false),
  ('2100','Customer Advances'          ,'liability','credit',false,false,'customer'  ,221,'212',true ,false),
  ('213' ,'Tax Liabilities'            ,'liability','credit',true ,false,NULL        ,230,'21' ,true ,false),
  ('2200','Output Tax Payable'         ,'liability','credit',false,false,'tax_output',231,'213',true ,false),

  ('3'   ,'Equity'                     ,'equity'   ,'credit',true ,false,NULL        ,300,NULL ,true ,false),
  ('3000','Owner Equity'               ,'equity'   ,'credit',false,false,NULL        ,310,'3'  ,true ,true ),
  ('3100','Retained Earnings'          ,'equity'   ,'credit',false,false,NULL        ,320,'3'  ,true ,false),
  ('3200','Opening Balance Equity'     ,'equity'   ,'credit',false,false,NULL        ,330,'3'  ,true ,false),

  ('4'   ,'Income'                     ,'income'   ,'credit',true ,false,NULL        ,400,NULL ,true ,false),
  ('41'  ,'Revenue'                    ,'income'   ,'credit',true ,false,NULL        ,410,'4'  ,true ,false),
  ('4000','Sales Revenue'              ,'income'   ,'credit',false,false,NULL        ,411,'41' ,true ,false),
  ('4010','Sales Returns'              ,'income'   ,'debit' ,false,true ,NULL        ,412,'41' ,true ,false),
  ('4020','Sales Discounts'            ,'income'   ,'debit' ,false,true ,NULL        ,413,'41' ,true ,false),
  ('42'  ,'Other Income'               ,'income'   ,'credit',true ,false,NULL        ,420,'4'  ,true ,false),
  ('4200','Inventory Gain'             ,'income'   ,'credit',false,false,NULL        ,421,'42' ,true ,false),
  ('4210','Cash Over'                  ,'income'   ,'credit',false,false,NULL        ,422,'42' ,true ,false),

  ('5'   ,'Cost of Sales'              ,'expense'  ,'debit' ,true ,false,NULL        ,500,NULL ,true ,false),
  ('5000','Cost of Goods Sold'         ,'expense'  ,'debit' ,false,false,NULL        ,510,'5'  ,true ,false),
  ('5100','Freight Inwards'            ,'expense'  ,'debit' ,false,false,NULL        ,520,'5'  ,true ,true ),
  ('5200','Purchase Discounts Received','expense'  ,'credit',false,true ,NULL        ,530,'5'  ,true ,false),

  ('6'   ,'Operating Expenses'         ,'expense'  ,'debit' ,true ,false,NULL        ,600,NULL ,true ,false),
  ('61'  ,'General Expenses'           ,'expense'  ,'debit' ,true ,false,NULL        ,610,'6'  ,true ,false),
  ('6100','Operating Expenses'         ,'expense'  ,'debit' ,false,false,NULL        ,611,'61' ,false,true ),
  ('6300','Bank Charges'               ,'expense'  ,'debit' ,false,false,NULL        ,612,'61' ,true ,true ),
  ('6400','Cash Short'                 ,'expense'  ,'debit' ,false,false,NULL        ,613,'61' ,true ,false),
  ('62'  ,'Inventory Losses'           ,'expense'  ,'debit' ,true ,false,NULL        ,620,'6'  ,true ,false),
  ('6200','Inventory Loss'             ,'expense'  ,'debit' ,false,false,NULL        ,621,'62' ,false,true ),
  ('6210','Damage Loss'                ,'expense'  ,'debit' ,false,false,NULL        ,622,'62' ,true ,false),
  ('6220','Expiry Loss'                ,'expense'  ,'debit' ,false,false,NULL        ,623,'62' ,true ,false),
  ('6230','Stock Adjustment Loss'      ,'expense'  ,'debit' ,false,false,NULL        ,624,'62' ,true ,false),
  ('6240','Transfer Shortage'          ,'expense'  ,'debit' ,false,false,NULL        ,625,'62' ,true ,false);

-- add the accounts a tenant does not have yet
INSERT INTO chart_of_accounts
  (tenant_id, code, name, account_type, normal_balance, is_group, is_contra,
   control_type, sort_order, is_system, allow_manual_entries, is_active)
SELECT t.id, s.code, s.name, s.account_type, s.normal_balance, s.is_group, s.is_contra,
       s.control_type, s.sort_order, s.is_system, s.allow_manual, true
  FROM tenants t CROSS JOIN seed_accounts s
 ON CONFLICT (tenant_id, code) DO NOTHING;

-- restate the accounts that already existed, then place everything in the tree
UPDATE chart_of_accounts a
   SET is_group             = s.is_group,
       is_contra            = s.is_contra,
       control_type         = s.control_type,
       sort_order           = s.sort_order,
       normal_balance       = s.normal_balance,
       is_system            = a.is_system OR s.is_system,
       allow_manual_entries = s.allow_manual,
       updated_at           = now()
  FROM seed_accounts s
 WHERE a.code = s.code;

UPDATE chart_of_accounts a
   SET parent_id = p.id, updated_at = now()
  FROM seed_accounts s
  JOIN chart_of_accounts p ON p.code = s.parent_code
 WHERE a.code = s.code
   AND s.parent_code IS NOT NULL
   AND p.tenant_id = a.tenant_id
   AND a.parent_id IS DISTINCT FROM p.id;

-- the per-drawer cash and bank ledger accounts sit under Cash and Equivalents
UPDATE chart_of_accounts a
   SET parent_id    = p.id,
       control_type = COALESCE(a.control_type, 'cash'),
       sort_order   = 119,
       updated_at   = now()
  FROM chart_of_accounts p
 WHERE p.tenant_id = a.tenant_id
   AND p.code = '111'
   AND a.code LIKE 'CA-%'
   AND a.parent_id IS NULL;

-- ------------------------------------------------------------- seed the roles

CREATE TEMP TABLE seed_roles(role text, code text);

INSERT INTO seed_roles VALUES
  ('cash_on_hand'          ,'1000'),
  ('petty_cash'            ,'1120'),
  ('bank'                  ,'1010'),
  ('cheques_in_hand'       ,'1130'),
  ('ar_control'            ,'1100'),
  ('supplier_advances'     ,'1300'),
  ('inventory_control'     ,'1200'),
  ('goods_in_transit'      ,'1210'),
  ('tax_input'             ,'1400'),
  ('suspense'              ,'9000'),
  ('ap_control'            ,'2000'),
  ('grni'                  ,'2010'),
  ('customer_advances'     ,'2100'),
  ('tax_output'            ,'2200'),
  ('owner_equity'          ,'3000'),
  ('retained_earnings'     ,'3100'),
  ('opening_balance_equity','3200'),
  ('sales_revenue'         ,'4000'),
  ('sales_returns'         ,'4010'),
  ('sales_discount'        ,'4020'),
  ('inventory_gain'        ,'4200'),
  ('cash_over'             ,'4210'),
  ('cogs'                  ,'5000'),
  ('freight_inwards'       ,'5100'),
  ('purchase_discount'     ,'5200'),
  ('operating_expense'     ,'6100'),
  ('bank_charges'          ,'6300'),
  ('cash_short'            ,'6400'),
  ('inventory_loss'        ,'6200'),
  ('damage_loss'           ,'6210'),
  ('expiry_loss'           ,'6220'),
  ('adjustment_loss'       ,'6230'),
  ('transfer_shortage'     ,'6240');

INSERT INTO account_roles(tenant_id, role, account_id)
SELECT a.tenant_id, r.role, a.id
  FROM seed_roles r
  JOIN chart_of_accounts a ON a.code = r.code
 ON CONFLICT (tenant_id, role) DO NOTHING;

DROP TABLE seed_roles;
DROP TABLE seed_accounts;

-- Every role in the seed must have resolved, or the posting engine would fall
-- back to a hard-coded code at runtime. Fail the migration instead.
DO $checkroles$
DECLARE missing integer;
BEGIN
  SELECT count(*) INTO missing
    FROM tenants t
   CROSS JOIN (VALUES
      ('cash_on_hand'),('petty_cash'),('bank'),('cheques_in_hand'),('ar_control'),
      ('supplier_advances'),('inventory_control'),('goods_in_transit'),('tax_input'),
      ('suspense'),('ap_control'),('grni'),('customer_advances'),('tax_output'),
      ('owner_equity'),('retained_earnings'),('opening_balance_equity'),
      ('sales_revenue'),('sales_returns'),('sales_discount'),('inventory_gain'),
      ('cash_over'),('cogs'),('freight_inwards'),('purchase_discount'),
      ('operating_expense'),('bank_charges'),('cash_short'),('inventory_loss'),
      ('damage_loss'),('expiry_loss'),('adjustment_loss'),('transfer_shortage')
   ) AS needed(role)
   WHERE NOT EXISTS (
     SELECT 1 FROM account_roles ar WHERE ar.tenant_id = t.id AND ar.role = needed.role
   );
  IF missing > 0 THEN
    RAISE EXCEPTION '% posting roles are unmapped after seeding', missing;
  END IF;
END $checkroles$;

-- --------------------------------------------------- validation and reporting

-- Signed movement of one journal line against its account's natural side.
-- Contra accounts are deliberately NOT flipped here: a contra income account
-- holding a debit balance must show as a negative income figure so that the
-- revenue group totals to net sales.
CREATE OR REPLACE FUNCTION account_signed_balance(
  p_account_type text, p_debit numeric, p_credit numeric
) RETURNS numeric LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN p_account_type IN ('asset','expense')
              THEN COALESCE(p_debit,0) - COALESCE(p_credit,0)
              ELSE COALESCE(p_credit,0) - COALESCE(p_debit,0) END
$$;

-- Trial balance for one tenant as at a date. Optionally restricted to one
-- branch; pass NULL for the whole company.
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
         COALESCE(sum(l.debit), 0)  AS debit,
         COALESCE(sum(l.credit), 0) AS credit,
         COALESCE(sum(account_signed_balance(a.account_type, l.debit, l.credit)), 0) AS balance
    FROM chart_of_accounts a
    LEFT JOIN journal_lines l ON l.account_id = a.id
    LEFT JOIN journal_entries j
           ON j.id = l.journal_id
          AND j.status = 'posted'
          AND j.entry_date <= p_as_of
          AND (p_from IS NULL OR j.entry_date >= p_from)
          AND (p_branch IS NULL OR j.branch_id = p_branch)
   WHERE a.tenant_id = p_tenant
     AND (j.id IS NOT NULL OR l.id IS NULL)
   GROUP BY a.id
   ORDER BY a.sort_order, a.code
$$;

-- Assets = Liabilities + Equity, with the period's income and expenses folded
-- into equity as current earnings. Any non-zero difference means journals have
-- been written that do not balance by account type, which should be impossible
-- while the balance triggers are in place.
CREATE OR REPLACE FUNCTION accounting_equation_check(
  p_tenant uuid,
  p_as_of  date DEFAULT CURRENT_DATE,
  p_branch uuid DEFAULT NULL
) RETURNS TABLE(
  assets numeric, liabilities numeric, equity numeric,
  income numeric, expense numeric, current_earnings numeric,
  difference numeric, balanced boolean,
  total_debit numeric, total_credit numeric, debits_equal_credits boolean
) LANGUAGE sql STABLE AS $$
  WITH tb AS (
    SELECT account_type, debit, credit, balance
      FROM accounting_trial_balance(p_tenant, p_as_of, NULL, p_branch)
     WHERE NOT is_group
  ), totals AS (
    SELECT
      COALESCE(sum(balance) FILTER (WHERE account_type = 'asset'), 0)     AS assets,
      COALESCE(sum(balance) FILTER (WHERE account_type = 'liability'), 0) AS liabilities,
      COALESCE(sum(balance) FILTER (WHERE account_type = 'equity'), 0)    AS equity,
      COALESCE(sum(balance) FILTER (WHERE account_type = 'income'), 0)    AS income,
      COALESCE(sum(balance) FILTER (WHERE account_type = 'expense'), 0)   AS expense,
      COALESCE(sum(debit), 0)  AS total_debit,
      COALESCE(sum(credit), 0) AS total_credit
    FROM tb
  )
  SELECT assets, liabilities, equity, income, expense,
         income - expense AS current_earnings,
         round(assets - (liabilities + equity + income - expense), 2) AS difference,
         round(assets - (liabilities + equity + income - expense), 2) = 0 AS balanced,
         total_debit, total_credit,
         round(total_debit - total_credit, 2) = 0 AS debits_equal_credits
    FROM totals
$$;

-- Journals whose own lines do not balance. The deferred triggers make this
-- unreachable for new entries; it exists so the alerting page can prove it.
CREATE OR REPLACE VIEW unbalanced_journals AS
  SELECT j.id, j.tenant_id, j.entry_no, j.entry_date, j.reference_type, j.status,
         sum(l.debit) AS debit, sum(l.credit) AS credit,
         round(sum(l.debit) - sum(l.credit), 2) AS difference
    FROM journal_entries j
    JOIN journal_lines l ON l.journal_id = j.id
   WHERE j.status = 'posted'
   GROUP BY j.id
  HAVING round(sum(l.debit) - sum(l.credit), 2) <> 0;

COMMENT ON VIEW unbalanced_journals IS
  'Posted journals whose debits and credits disagree. Should always be empty.';
