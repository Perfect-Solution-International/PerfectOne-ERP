DO $$
DECLARE
  tenant uuid;
  source_branch uuid;
  destination_branch uuid;
  product uuid;
  transfer_document uuid;
  transfer_line uuid;
  damage_event uuid;
  debit_total numeric;
  credit_total numeric;
  rejected boolean:=false;
BEGIN
  SELECT t.id,b.id INTO tenant,source_branch
    FROM tenants t JOIN branches b ON b.tenant_id=t.id ORDER BY t.created_at LIMIT 1;
  SELECT id INTO product FROM products WHERE tenant_id=tenant ORDER BY created_at LIMIT 1;
  INSERT INTO branches(tenant_id,name,code)
    VALUES(tenant,'Transfer integrity destination','TEST-DEST') RETURNING id INTO destination_branch;
  INSERT INTO stock_transfer_documents(tenant_id,document_no,from_branch_id,to_branch_id,status,client_request_id)
    VALUES(tenant,'ST-INTEGRITY-TEST',source_branch,destination_branch,'partially_received','st-integrity-test')
    RETURNING id INTO transfer_document;
  INSERT INTO stock_transfers(tenant_id,document_id,from_branch_id,to_branch_id,product_id,quantity,status,reference)
    VALUES(tenant,transfer_document,source_branch,destination_branch,product,2,'dispatched','integrity test')
    RETURNING id INTO transfer_line;
  INSERT INTO transfer_damage_events(tenant_id,transfer_document_id,transfer_line_id,product_id,quantity,unit_cost,reason)
    VALUES(tenant,transfer_document,transfer_line,product,2,125.50,'Damaged in isolated migration test')
    RETURNING id INTO damage_event;

  SELECT sum(l.debit),sum(l.credit) INTO debit_total,credit_total
    FROM journal_entries j JOIN journal_lines l ON l.journal_id=j.id
    WHERE j.reference_type='transfer_damage' AND j.reference_id=damage_event;
  ASSERT debit_total=251.00 AND credit_total=251.00,
    'transit damage journal must debit and credit the full inventory cost';

  BEGIN
    UPDATE transfer_damage_events SET reason='forbidden' WHERE id=damage_event;
  EXCEPTION WHEN OTHERS THEN
    rejected:=true;
  END;
  ASSERT rejected,'transfer damage event mutation was not rejected';
END $$;
