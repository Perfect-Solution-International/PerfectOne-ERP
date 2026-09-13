-- Complete the financial and document trail for variable-unit inventory.
ALTER TABLE unit_conversion_events
  ADD COLUMN IF NOT EXISTS source_cost numeric(14,2) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS target_unit_cost numeric(14,6),
  ADD COLUMN IF NOT EXISTS yield_percent numeric(9,4);

ALTER TABLE purchase_return_items
  ADD COLUMN IF NOT EXISTS entered_quantity numeric(14,6),
  ADD COLUMN IF NOT EXISTS entered_unit_id uuid REFERENCES units(id),
  ADD COLUMN IF NOT EXISTS entered_unit_cost numeric(14,2),
  ADD COLUMN IF NOT EXISTS conversion_factor numeric(18,6) NOT NULL DEFAULT 1;

UPDATE purchase_return_items r
SET entered_quantity = r.quantity / NULLIF(COALESCE(i.conversion_factor,1),0),
    entered_unit_id = i.entered_unit_id,
    entered_unit_cost = i.entered_unit_cost,
    conversion_factor = COALESCE(i.conversion_factor,1)
FROM purchase_items i
WHERE i.id=r.purchase_item_id AND r.entered_quantity IS NULL;

UPDATE purchase_return_items SET entered_quantity=quantity WHERE entered_quantity IS NULL;
ALTER TABLE purchase_return_items ALTER COLUMN entered_quantity SET NOT NULL;
