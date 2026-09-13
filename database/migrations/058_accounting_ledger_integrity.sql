-- Financial ledger integrity: posted headers are immutable and every posted
-- journal must remain balanced at transaction commit.
CREATE OR REPLACE FUNCTION protect_posted_journal_header() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status='reversed' THEN
    RAISE EXCEPTION 'reversed journals are immutable';
  END IF;
  IF OLD.status='posted' AND NOT (
    NEW.status='reversed'
    AND NEW.id IS NOT DISTINCT FROM OLD.id
    AND NEW.tenant_id IS NOT DISTINCT FROM OLD.tenant_id
    AND NEW.entry_date IS NOT DISTINCT FROM OLD.entry_date
    AND NEW.reference_type IS NOT DISTINCT FROM OLD.reference_type
    AND NEW.reference_id IS NOT DISTINCT FROM OLD.reference_id
    AND NEW.description IS NOT DISTINCT FROM OLD.description
    AND NEW.created_by IS NOT DISTINCT FROM OLD.created_by
    AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at
    AND NEW.posted_at IS NOT DISTINCT FROM OLD.posted_at
    AND NEW.reversed_entry_id IS NOT DISTINCT FROM OLD.reversed_entry_id
    AND NEW.entry_no IS NOT DISTINCT FROM OLD.entry_no
    AND NEW.branch_id IS NOT DISTINCT FROM OLD.branch_id
    AND NEW.source IS NOT DISTINCT FROM OLD.source
    AND NEW.client_request_id IS NOT DISTINCT FROM OLD.client_request_id
    AND NEW.posted_by IS NOT DISTINCT FROM OLD.posted_by
  ) THEN
    RAISE EXCEPTION 'posted journal headers are immutable; reverse the journal';
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS journal_entries_posted_update_guard ON journal_entries;
CREATE TRIGGER journal_entries_posted_update_guard
BEFORE UPDATE ON journal_entries
FOR EACH ROW EXECUTE FUNCTION protect_posted_journal_header();

CREATE OR REPLACE FUNCTION enforce_balanced_posted_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target_id uuid; target_status text; debit_total numeric; credit_total numeric;
BEGIN
  target_id:=COALESCE(NEW.journal_id,OLD.journal_id);
  SELECT status INTO target_status FROM journal_entries WHERE id=target_id;
  IF target_status IN('posted','reversed') THEN
    SELECT COALESCE(sum(debit),0),COALESCE(sum(credit),0)
      INTO debit_total,credit_total FROM journal_lines WHERE journal_id=target_id;
    IF abs(debit_total-credit_total)>0.009 THEN
      RAISE EXCEPTION 'posted journal % is not balanced (debit %, credit %)',target_id,debit_total,credit_total;
    END IF;
  END IF;
  RETURN COALESCE(NEW,OLD);
END $$;

DROP TRIGGER IF EXISTS journal_lines_balance_guard ON journal_lines;
CREATE CONSTRAINT TRIGGER journal_lines_balance_guard
AFTER INSERT OR UPDATE OR DELETE ON journal_lines
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_balanced_posted_journal();
