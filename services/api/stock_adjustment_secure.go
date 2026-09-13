package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stockAdjustmentInput struct {
	ProductID        string   `json:"productId"`
	PhysicalQuantity *float64 `json:"physicalQuantity"`
	Quantity         *float64 `json:"quantity"`
	Reason           string   `json:"reason"`
	Notes            string   `json:"notes"`
	RequestID        string   `json:"requestId"`
}

func secureAdjustStock(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x stockAdjustmentInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.ProductID == "" || strings.TrimSpace(x.Reason) == "" || strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "product, reason and request ID are required", http.StatusBadRequest)
			return
		}
		if (x.PhysicalQuantity == nil) == (x.Quantity == nil) {
			http.Error(w, "provide either physical quantity or adjustment quantity", http.StatusBadRequest)
			return
		}
		if x.PhysicalQuantity != nil && *x.PhysicalQuantity < 0 {
			http.Error(w, "physical quantity cannot be negative", http.StatusBadRequest)
			return
		}

		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())

		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='stock_adjustment'`, c.Tenant, x.RequestID).Scan(&prior)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var globalQuantity float64
		err = tx.QueryRow(r.Context(), `SELECT stock_quantity FROM products WHERE id=$1 AND tenant_id=$2 AND is_active FOR UPDATE`, x.ProductID, c.Tenant).Scan(&globalQuantity)
		if err != nil {
			http.Error(w, "active product not found", http.StatusNotFound)
			return
		}
		if c.Branch == "" {
			http.Error(w, "user branch is required for a physical stock count", http.StatusConflict)
			return
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO branch_stock(product_id,branch_id,quantity) SELECT $1,$2,0 WHERE EXISTS(SELECT 1 FROM branches WHERE id=$2 AND tenant_id=$3) ON CONFLICT(product_id,branch_id) DO NOTHING`, x.ProductID, c.Branch, c.Tenant); err != nil {
			http.Error(w, "invalid inventory branch", http.StatusConflict)
			return
		}
		var systemQuantity float64
		if err = tx.QueryRow(r.Context(), `SELECT quantity FROM branch_stock WHERE product_id=$1 AND branch_id=$2 FOR UPDATE`, x.ProductID, c.Branch).Scan(&systemQuantity); err != nil {
			http.Error(w, "branch inventory is unavailable", http.StatusConflict)
			return
		}
		physicalQuantity := systemQuantity
		if x.PhysicalQuantity != nil {
			physicalQuantity = *x.PhysicalQuantity
		} else {
			physicalQuantity += *x.Quantity
		}
		difference := physicalQuantity - systemQuantity
		if difference == 0 {
			http.Error(w, "physical quantity matches system quantity", http.StatusBadRequest)
			return
		}
		if physicalQuantity < 0 || globalQuantity+difference < 0 {
			http.Error(w, "adjustment would make stock negative", http.StatusConflict)
			return
		}

		var adjustmentID string
		err = tx.QueryRow(r.Context(), `INSERT INTO stock_adjustments(tenant_id,branch_id,product_id,system_quantity,physical_quantity,quantity,reason,notes,adjusted_by,client_request_id)
			VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,'')::uuid,$10) RETURNING id`,
			c.Tenant, c.Branch, x.ProductID, systemQuantity, physicalQuantity, difference, strings.TrimSpace(x.Reason), strings.TrimSpace(x.Notes), c.Sub, x.RequestID).Scan(&adjustmentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		var balanceAfter float64
		err = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$1 WHERE id=$2 AND tenant_id=$3 RETURNING stock_quantity`, difference, x.ProductID, c.Tenant).Scan(&balanceAfter)
		if err == nil {
			err = adjustBranchStock(r.Context(), tx, c.Tenant, c.Branch, x.ProductID, difference)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by)
				VALUES($1,'adjustment',$2,$3,NULLIF($4,'')::uuid,'stock_adjustment',$5,$6,$7,NULLIF($8,'')::uuid)`,
				x.ProductID, difference, c.Tenant, c.Branch, adjustmentID, balanceAfter, strings.TrimSpace(x.Reason)+": "+strings.TrimSpace(x.Notes), c.Sub)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response, _ := json.Marshal(map[string]any{
			"id": adjustmentID, "systemQuantity": systemQuantity, "physicalQuantity": physicalQuantity,
			"adjustment": difference, "balanceAfter": balanceAfter, "status": "finalized",
		})
		_, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'stock_adjustment',$3,$4)`, c.Tenant, x.RequestID, adjustmentID, response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(response)
	}
}
