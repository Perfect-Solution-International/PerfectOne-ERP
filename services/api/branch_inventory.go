package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func adjustBranchStock(ctx context.Context, tx pgx.Tx, tenant, branch, product string, delta float64) error {
	if branch == "" {
		return fmt.Errorf("branch is required for inventory movement")
	}
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM branches b JOIN products p ON p.tenant_id=b.tenant_id WHERE b.id=$1 AND p.id=$2 AND b.tenant_id=$3)`, branch, product, tenant).Scan(&valid); err != nil || !valid {
		return fmt.Errorf("product and branch inventory scope mismatch")
	}
	if delta >= 0 {
		_, err := tx.Exec(ctx, `INSERT INTO branch_stock(product_id,branch_id,quantity) VALUES($1,$2,$3) ON CONFLICT(product_id,branch_id) DO UPDATE SET quantity=branch_stock.quantity+EXCLUDED.quantity`, product, branch, delta)
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE branch_stock SET quantity=quantity+$3 WHERE product_id=$1 AND branch_id=$2 AND quantity+$3>=0`, product, branch, delta)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("insufficient stock in selected branch")
	}
	return nil
}
