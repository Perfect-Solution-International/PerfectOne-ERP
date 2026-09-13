DO $$
DECLARE
  tenant uuid;
  account uuid;
  balanced_journal uuid;
  rejected boolean:=false;
BEGIN
  SELECT tenant_id,id INTO tenant,account FROM chart_of_accounts ORDER BY code LIMIT 1;

  INSERT INTO journal_entries(tenant_id,description,status)
    VALUES(tenant,'ledger integrity test','posted') RETURNING id INTO balanced_journal;
  INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(balanced_journal,account,10);
  INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(balanced_journal,account,10);

  BEGIN
    UPDATE journal_entries SET description='forbidden mutation' WHERE id=balanced_journal;
  EXCEPTION WHEN OTHERS THEN
    rejected:=true;
  END;
  ASSERT rejected,'posted journal header mutation was not rejected';

  UPDATE journal_entries
    SET status='reversed',reversal_reason='integrity test',reversed_by=NULL
    WHERE id=balanced_journal;

  rejected:=false;
  BEGIN
    WITH journal AS (
      INSERT INTO journal_entries(tenant_id,description,status)
        VALUES(tenant,'unbalanced integrity test','posted') RETURNING id
    )
    INSERT INTO journal_lines(journal_id,account_id,debit)
      SELECT id,account,25 FROM journal;
    SET CONSTRAINTS journal_lines_balance_guard IMMEDIATE;
  EXCEPTION WHEN OTHERS THEN
    rejected:=true;
  END;
  ASSERT rejected,'unbalanced posted journal was not rejected';
END $$;
