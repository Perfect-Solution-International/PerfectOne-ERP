ALTER TABLE stock_transfer_documents DROP CONSTRAINT IF EXISTS stock_transfer_documents_status_check;
ALTER TABLE stock_transfer_documents ADD CONSTRAINT stock_transfer_documents_status_check CHECK(status IN('draft','approved','partially_dispatched','dispatched','partially_received','received','cancelled'));
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS dispatched_quantity numeric(14,3) NOT NULL DEFAULT 0 CHECK(dispatched_quantity>=0 AND dispatched_quantity<=quantity);
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS received_quantity numeric(14,3) NOT NULL DEFAULT 0 CHECK(received_quantity>=0);
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS damaged_in_transit_quantity numeric(14,3) NOT NULL DEFAULT 0 CHECK(damaged_in_transit_quantity>=0);
ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS transit_damage_reason text;
ALTER TABLE stock_transfers DROP CONSTRAINT IF EXISTS stock_transfer_received_limit;
ALTER TABLE stock_transfers ADD CONSTRAINT stock_transfer_received_limit CHECK(received_quantity+damaged_in_transit_quantity<=dispatched_quantity);
UPDATE stock_transfers SET dispatched_quantity=quantity,received_quantity=CASE WHEN status='received' THEN quantity ELSE 0 END WHERE status IN('dispatched','received');
