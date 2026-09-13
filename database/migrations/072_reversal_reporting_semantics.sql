-- The acceptance test exposed the legacy reversal convention (original marked
-- reversed and therefore excluded by reports). Preserve the immutable pair and
-- post one explicit correction for that known QA document. New API reversals
-- keep the original posted, so future documents need no correction.
DO $$
DECLARE s record; jid uuid;
BEGIN
  SELECT * INTO s FROM sales WHERE client_request_id='qa-pos-20260913-001' AND status='cancelled';
  IF FOUND AND NOT EXISTS(SELECT 1 FROM journal_entries WHERE client_request_id='qa-reversal-correction:20260913-001') THEN
    INSERT INTO journal_entries(tenant_id,branch_id,entry_date,reference_type,reference_id,description,created_by,posted_by,posted_at,status,source,client_request_id)
    VALUES(s.tenant_id,s.branch_id,current_date,'qa_reversal_correction',s.id,'Acceptance-test reversal ledger correction',s.cancelled_by,s.cancelled_by,now(),'posted','system','qa-reversal-correction:20260913-001')
    RETURNING id INTO jid;
    INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo)
      SELECT jid,account_id,s.paid_amount,0,'Restore excluded original receipt' FROM account_roles WHERE tenant_id=s.tenant_id AND role='cash_on_hand';
    INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo)
      SELECT jid,account_id,0,s.total,'Restore excluded original revenue' FROM account_roles WHERE tenant_id=s.tenant_id AND role='sales_revenue';
    INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo)
      SELECT jid,ar.account_id,sum(si.quantity*si.unit_cost),0,'Restore excluded original COGS' FROM sale_items si JOIN account_roles ar ON ar.tenant_id=s.tenant_id AND ar.role='cogs' WHERE si.sale_id=s.id GROUP BY ar.account_id;
    INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo)
      SELECT jid,ar.account_id,0,sum(si.quantity*si.unit_cost),'Restore excluded original inventory relief' FROM sale_items si JOIN account_roles ar ON ar.tenant_id=s.tenant_id AND ar.role='inventory_control' WHERE si.sale_id=s.id GROUP BY ar.account_id;
  END IF;
END $$;
