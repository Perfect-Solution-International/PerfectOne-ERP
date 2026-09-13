ALTER TABLE sales ADD COLUMN IF NOT EXISTS branch_id uuid REFERENCES branches(id);
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS branch_id uuid REFERENCES branches(id);
ALTER TABLE stock_batches ADD COLUMN IF NOT EXISTS branch_id uuid REFERENCES branches(id);

UPDATE sales s SET branch_id=u.branch_id FROM users u WHERE u.id=s.cashier_id AND s.branch_id IS NULL;
UPDATE purchases p SET branch_id=u.branch_id FROM users u WHERE u.id=COALESCE(p.finalized_by,p.reversed_by) AND p.branch_id IS NULL;
UPDATE sales s SET branch_id=(SELECT id FROM branches WHERE tenant_id=s.tenant_id ORDER BY created_at LIMIT 1) WHERE s.branch_id IS NULL;
UPDATE purchases p SET branch_id=(SELECT id FROM branches WHERE tenant_id=p.tenant_id ORDER BY created_at LIMIT 1) WHERE p.branch_id IS NULL;
UPDATE stock_batches sb SET branch_id=(SELECT b.id FROM products p JOIN branches b ON b.tenant_id=p.tenant_id WHERE p.id=sb.product_id ORDER BY b.created_at LIMIT 1) WHERE sb.branch_id IS NULL;

CREATE OR REPLACE FUNCTION validate_document_branch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.branch_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM branches WHERE id=NEW.branch_id AND tenant_id=NEW.tenant_id) THEN
    RAISE EXCEPTION 'document branch must belong to the same tenant';
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS sales_branch_guard ON sales;
CREATE TRIGGER sales_branch_guard BEFORE INSERT OR UPDATE OF tenant_id,branch_id ON sales FOR EACH ROW EXECUTE FUNCTION validate_document_branch();
DROP TRIGGER IF EXISTS purchases_branch_guard ON purchases;
CREATE TRIGGER purchases_branch_guard BEFORE INSERT OR UPDATE OF tenant_id,branch_id ON purchases FOR EACH ROW EXECUTE FUNCTION validate_document_branch();

CREATE OR REPLACE FUNCTION validate_batch_branch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.branch_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM products p JOIN branches b ON b.tenant_id=p.tenant_id WHERE p.id=NEW.product_id AND b.id=NEW.branch_id) THEN
    RAISE EXCEPTION 'batch branch must belong to the product tenant';
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS stock_batch_branch_guard ON stock_batches;
CREATE TRIGGER stock_batch_branch_guard BEFORE INSERT OR UPDATE OF product_id,branch_id ON stock_batches FOR EACH ROW EXECUTE FUNCTION validate_batch_branch();

DO $$
DECLARE rec record; other_total numeric;
BEGIN
  FOR rec IN SELECT p.id product_id,p.stock_quantity,b.id branch_id FROM products p JOIN LATERAL(SELECT id FROM branches WHERE tenant_id=p.tenant_id ORDER BY created_at LIMIT 1)b ON true LOOP
    SELECT COALESCE(sum(quantity),0) INTO other_total FROM branch_stock WHERE product_id=rec.product_id AND branch_id<>rec.branch_id;
    IF other_total>rec.stock_quantity THEN
      RAISE EXCEPTION 'branch inventory exceeds global inventory for product %; reconcile before migration',rec.product_id;
    END IF;
    INSERT INTO branch_stock(product_id,branch_id,quantity) VALUES(rec.product_id,rec.branch_id,rec.stock_quantity-other_total)
    ON CONFLICT(product_id,branch_id) DO UPDATE SET quantity=EXCLUDED.quantity;
  END LOOP;
END $$;
