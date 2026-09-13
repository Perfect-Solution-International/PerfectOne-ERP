BEGIN;

DO $$
DECLARE
  test_product uuid;
  movement_id uuid;
  mutation_blocked boolean := false;
  negative_blocked boolean := false;
BEGIN
  IF EXISTS (
    SELECT 1 FROM products p
    WHERE p.stock_quantity <> 0
      AND NOT EXISTS (SELECT 1 FROM stock_movements m WHERE m.product_id = p.id)
  ) THEN
    RAISE EXCEPTION 'products with stock are missing their opening ledger entry';
  END IF;

  IF EXISTS (SELECT 1 FROM stock_movements WHERE tenant_id IS NULL) THEN
    RAISE EXCEPTION 'tenantless stock movement found';
  END IF;

  SELECT id INTO test_product FROM products ORDER BY created_at LIMIT 1;
  SELECT id INTO movement_id FROM stock_movements WHERE product_id = test_product ORDER BY created_at LIMIT 1;

  BEGIN
    UPDATE stock_movements SET quantity = quantity + 1 WHERE id = movement_id;
  EXCEPTION WHEN OTHERS THEN
    mutation_blocked := true;
  END;
  IF NOT mutation_blocked THEN
    RAISE EXCEPTION 'stock movement mutation was not blocked';
  END IF;

  BEGIN
    UPDATE products SET stock_quantity = -1 WHERE id = test_product;
  EXCEPTION WHEN OTHERS THEN
    negative_blocked := true;
  END;
  IF NOT negative_blocked THEN
    RAISE EXCEPTION 'negative product stock was not blocked';
  END IF;
END;
$$;

ROLLBACK;
