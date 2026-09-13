-- Allow a posted journal to receive only its immutable reversal link while
-- remaining included in ledger reports. No monetary or descriptive field may
-- change, and an already-linked journal remains immutable.
CREATE OR REPLACE FUNCTION protect_posted_journal_header() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status='reversed' THEN
    RAISE EXCEPTION 'reversed journals are immutable';
  END IF;
  IF OLD.status='posted' AND NOT (
    OLD.reversed_entry_id IS NULL
    AND NEW.status IN ('posted','reversed')
    AND NEW.reversed_entry_id IS NOT NULL
    AND NEW.id IS NOT DISTINCT FROM OLD.id
    AND NEW.tenant_id IS NOT DISTINCT FROM OLD.tenant_id
    AND NEW.entry_date IS NOT DISTINCT FROM OLD.entry_date
    AND NEW.reference_type IS NOT DISTINCT FROM OLD.reference_type
    AND NEW.reference_id IS NOT DISTINCT FROM OLD.reference_id
    AND NEW.description IS NOT DISTINCT FROM OLD.description
    AND NEW.created_by IS NOT DISTINCT FROM OLD.created_by
    AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at
    AND NEW.posted_at IS NOT DISTINCT FROM OLD.posted_at
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
