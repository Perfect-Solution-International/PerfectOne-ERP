-- Immutable accounting evidence for inventory lost while stock is in transit.
CREATE TABLE IF NOT EXISTS transfer_damage_events(
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  transfer_document_id uuid NOT NULL REFERENCES stock_transfer_documents(id),
  transfer_line_id uuid NOT NULL REFERENCES stock_transfers(id),
  product_id uuid NOT NULL REFERENCES products(id),
  batch_id uuid REFERENCES stock_batches(id),
  quantity numeric(14,3) NOT NULL CHECK(quantity>0),
  unit_cost numeric(14,4) NOT NULL CHECK(unit_cost>=0),
  reason text NOT NULL CHECK(length(trim(reason))>0),
  reported_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS transfer_damage_events_document_idx
  ON transfer_damage_events(tenant_id,transfer_document_id,created_at);

CREATE OR REPLACE FUNCTION protect_transfer_damage_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'transfer damage events are immutable'; END $$;
DROP TRIGGER IF EXISTS transfer_damage_events_immutable ON transfer_damage_events;
CREATE TRIGGER transfer_damage_events_immutable
BEFORE UPDATE OR DELETE ON transfer_damage_events
FOR EACH ROW EXECUTE FUNCTION protect_transfer_damage_event();

CREATE OR REPLACE FUNCTION post_transfer_damage_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE inventory_id uuid; loss_id uuid; journal_id uuid; amount numeric;
BEGIN
  amount:=round(NEW.quantity*NEW.unit_cost,2);
  IF amount<=0 THEN RETURN NEW; END IF;
  SELECT id INTO inventory_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='1200';
  SELECT id INTO loss_id FROM chart_of_accounts WHERE tenant_id=NEW.tenant_id AND code='6200';
  IF inventory_id IS NULL OR loss_id IS NULL THEN
    RAISE EXCEPTION 'inventory and inventory loss accounts must be configured';
  END IF;
  INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at,source)
    VALUES(NEW.tenant_id,'transfer_damage',NEW.id,'Inventory damaged in transit',NEW.reported_by,'posted',now(),'inventory')
    RETURNING id INTO journal_id;
  INSERT INTO journal_lines(journal_id,account_id,debit) VALUES(journal_id,loss_id,amount);
  INSERT INTO journal_lines(journal_id,account_id,credit) VALUES(journal_id,inventory_id,amount);
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS transfer_damage_events_accounting ON transfer_damage_events;
CREATE TRIGGER transfer_damage_events_accounting
AFTER INSERT ON transfer_damage_events
FOR EACH ROW EXECUTE FUNCTION post_transfer_damage_journal();
