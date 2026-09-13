-- One-time correction for the known seed invoice created before GL posting existed.
-- Deliberately targets only INV-DEMO-001 and is idempotent.
DO $$
DECLARE s record; jid uuid;
BEGIN
  SELECT * INTO s FROM sales
   WHERE invoice_no='INV-DEMO-001' AND status<>'cancelled'
     AND NOT EXISTS (SELECT 1 FROM journal_entries WHERE reference_type='sale' AND reference_id=sales.id AND status='posted');
  IF FOUND THEN
    INSERT INTO journal_entries(tenant_id,branch_id,entry_date,reference_type,reference_id,description,created_by,posted_by,posted_at,status,source,client_request_id)
    VALUES(s.tenant_id,s.branch_id,s.created_at::date,'sale',s.id,'Legacy seed invoice reconciliation: INV-DEMO-001',s.cashier_id,s.cashier_id,s.created_at,'posted','system','legacy-seed-sale:INV-DEMO-001')
    RETURNING id INTO jid;

    INSERT INTO journal_lines(journal_id,account_id,debit,credit)
    SELECT jid,account_id,s.paid_amount,0 FROM account_roles
     WHERE tenant_id=s.tenant_id AND role=CASE WHEN s.payment_method IN('bank','card') THEN 'bank' ELSE 'cash_on_hand' END
       AND s.paid_amount>0;
    INSERT INTO journal_lines(journal_id,account_id,debit,credit)
    SELECT jid,account_id,s.balance,0 FROM account_roles
     WHERE tenant_id=s.tenant_id AND role='ar_control' AND s.balance>0;
    INSERT INTO journal_lines(journal_id,account_id,debit,credit)
    SELECT jid,account_id,0,s.total FROM account_roles
     WHERE tenant_id=s.tenant_id AND role='sales_revenue';
  END IF;
END $$;
