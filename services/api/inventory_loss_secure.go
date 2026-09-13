package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func secureExpireBatch(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct{ Reason, RequestID string }
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.RequestID) == "" {
			http.Error(w, "reason and request ID are required", 400)
			return
		}
		c, id := claimsFrom(r), r.PathValue("id")
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='batch_expire'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}
		var product, branch string
		var quantity, balance float64
		err = tx.QueryRow(r.Context(), `SELECT b.product_id,COALESCE(b.branch_id::text,''),b.available_qty,p.stock_quantity FROM stock_batches b JOIN products p ON p.id=b.product_id WHERE b.id=$1 AND p.tenant_id=$2 AND b.status='available' AND b.available_qty>0 AND b.expiry_date<current_date FOR UPDATE OF b,p`, id, c.Tenant).Scan(&product, &branch, &quantity, &balance)
		if err != nil {
			http.Error(w, "expired available batch not found", 409)
			return
		}
		if balance < quantity {
			http.Error(w, "batch quantity exceeds product stock; reconcile inventory first", 409)
			return
		}
		balance -= quantity
		_, err = tx.Exec(r.Context(), `UPDATE products SET stock_quantity=$2 WHERE id=$1 AND tenant_id=$3`, product, balance, c.Tenant)
		if err == nil {
			err = adjustBranchStock(r.Context(), tx, c.Tenant, branch, product, -quantity)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE stock_batches SET available_qty=0,status='expired',notes=concat_ws(E'\n',notes,$2::text) WHERE id=$1`, id, strings.TrimSpace(input.Reason))
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'expired',$2,$3,NULLIF($4,'')::uuid,'batch',$5,$6,$7,NULLIF($8,'')::uuid)`, product, -quantity, c.Tenant, branch, id, balance, strings.TrimSpace(input.Reason), c.Sub)
		}
		response, _ := json.Marshal(map[string]any{"id": id, "status": "expired", "quantity": quantity})
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'batch_expire',$3,$4)`, c.Tenant, input.RequestID, id, response)
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "expiry stock adjustment failed", 500)
			return
		}
		auditUserAction(r, db, "BATCH_EXPIRED", id, map[string]any{"quantity": quantity, "reason": input.Reason})
		w.Write(response)
	}
}

type damageInput struct {
	ProductID string  `json:"productId"`
	BatchID   string  `json:"batchId"`
	Reason    string  `json:"reason"`
	Notes     string  `json:"notes"`
	RequestID string  `json:"requestId"`
	Quantity  float64 `json:"quantity"`
}

func secureCreateDamage(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input damageInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.ProductID == "" || input.Quantity <= 0 || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.RequestID) == "" {
			http.Error(w, "product, positive quantity, reason and request ID are required", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='damage_create'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}
		var valid bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM products p WHERE p.id=$1 AND p.tenant_id=$2 AND p.is_active AND (NULLIF($3,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM stock_batches b WHERE b.id=NULLIF($3,'')::uuid AND b.product_id=p.id AND b.branch_id=NULLIF($4,'')::uuid AND b.status='available')))`, input.ProductID, c.Tenant, input.BatchID, c.Branch).Scan(&valid)
		if err != nil || !valid {
			http.Error(w, "active product or matching available batch not found", 409)
			return
		}
		var id string
		err = tx.QueryRow(r.Context(), `INSERT INTO damaged_items(tenant_id,branch_id,product_id,batch_id,quantity,reason,notes,status,reported_by,client_request_id) VALUES($1,NULLIF($2,'')::uuid,$3,NULLIF($4,'')::uuid,$5,$6,NULLIF($7,''),'draft',NULLIF($8,'')::uuid,$9) RETURNING id`, c.Tenant, c.Branch, input.ProductID, input.BatchID, input.Quantity, strings.TrimSpace(input.Reason), strings.TrimSpace(input.Notes), c.Sub, input.RequestID).Scan(&id)
		response, _ := json.Marshal(map[string]any{"id": id, "status": "draft"})
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'damage_create',$3,$4)`, c.Tenant, input.RequestID, id, response)
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "damage draft could not be created", 409)
			return
		}
		auditUserAction(r, db, "DAMAGE_DRAFT_CREATED", id, map[string]any{"quantity": input.Quantity})
		w.WriteHeader(201)
		w.Write(response)
	}
}

func secureDamageAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct{ Reason string }
		json.NewDecoder(r.Body).Decode(&input)
		id, action := r.PathValue("id"), r.PathValue("action")
		if action != "finalize" && action != "cancel" {
			http.Error(w, "unknown action", 404)
			return
		}
		if action == "cancel" && strings.TrimSpace(input.Reason) == "" {
			http.Error(w, "cancellation reason required", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var product, reason, branch string
		var batch *string
		var quantity float64
		err = tx.QueryRow(r.Context(), `SELECT product_id,batch_id::text,quantity,reason,COALESCE(branch_id::text,'') FROM damaged_items WHERE id=$1 AND tenant_id=$2 AND status='draft' FOR UPDATE`, id, c.Tenant).Scan(&product, &batch, &quantity, &reason, &branch)
		if err != nil {
			http.Error(w, "draft damage record not found", 409)
			return
		}
		if action == "cancel" {
			_, err = tx.Exec(r.Context(), `UPDATE damaged_items SET status='cancelled',cancelled_at=now(),cancelled_by=NULLIF($2,'')::uuid,cancellation_reason=$3 WHERE id=$1`, id, c.Sub, strings.TrimSpace(input.Reason))
		} else {
			if batch != nil {
				tag, e := tx.Exec(r.Context(), `UPDATE stock_batches SET available_qty=available_qty-$2,status=CASE WHEN available_qty-$2=0 THEN 'depleted' ELSE status END WHERE id=$1 AND product_id=$3 AND status='available' AND available_qty>=$2`, *batch, quantity, product)
				if e != nil || tag.RowsAffected() == 0 {
					http.Error(w, "insufficient stock in selected batch", 409)
					return
				}
			}
			var balance float64
			err = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND tenant_id=$3 AND stock_quantity>=$2 RETURNING stock_quantity`, product, quantity, c.Tenant).Scan(&balance)
			if err != nil {
				http.Error(w, "insufficient product stock", 409)
				return
			}
			if err = adjustBranchStock(r.Context(), tx, c.Tenant, branch, product, -quantity); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			_, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'damage',$2,$3,NULLIF($4,'')::uuid,'damage',$5,$6,$7,NULLIF($8,'')::uuid)`, product, -quantity, c.Tenant, branch, id, balance, reason, c.Sub)
			if err == nil {
				_, err = tx.Exec(r.Context(), `UPDATE damaged_items SET status='finalized',finalized_at=now(),finalized_by=NULLIF($2,'')::uuid WHERE id=$1`, id, c.Sub)
			}
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "damage workflow update failed", 500)
			return
		}
		auditUserAction(r, db, "DAMAGE_"+strings.ToUpper(action)+"D", id, map[string]any{"quantity": quantity, "reason": input.Reason})
		w.WriteHeader(204)
	}
}
