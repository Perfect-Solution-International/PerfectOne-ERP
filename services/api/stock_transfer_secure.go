package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type secureTransferInput struct {
	ProductID    string  `json:"productId"`
	BatchID      string  `json:"batchId"`
	FromBranchID string  `json:"fromBranchId"`
	ToBranchID   string  `json:"toBranchId"`
	Reference    string  `json:"reference"`
	Notes        string  `json:"notes"`
	RequestID    string  `json:"requestId"`
	Quantity     float64 `json:"quantity"`
}

func secureCreateTransfer(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input secureTransferInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.ProductID == "" || input.FromBranchID == "" || input.ToBranchID == "" || input.FromBranchID == input.ToBranchID || input.Quantity <= 0 || strings.TrimSpace(input.RequestID) == "" {
			http.Error(w, "valid source, destination, product, quantity and request ID are required", 400)
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
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='stock_transfer_create'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}
		var valid bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM products WHERE id=$1 AND tenant_id=$2 AND is_active) AND EXISTS(SELECT 1 FROM branches WHERE id=$3 AND tenant_id=$2) AND EXISTS(SELECT 1 FROM branches WHERE id=$4 AND tenant_id=$2) AND (NULLIF($5,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM stock_batches WHERE id=NULLIF($5,'')::uuid AND product_id=$1 AND branch_id=$3 AND available_qty>=$6))`, input.ProductID, c.Tenant, input.FromBranchID, input.ToBranchID,input.BatchID,input.Quantity).Scan(&valid)
		if err != nil || !valid {
			http.Error(w, "active product or branch not found", 409)
			return
		}
		var id string
		err = tx.QueryRow(r.Context(), `INSERT INTO stock_transfers(tenant_id,from_branch_id,to_branch_id,product_id,batch_id,quantity,status,reference,document_no,notes,transferred_by,client_request_id) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,'draft',NULLIF($7,''),COALESCE(NULLIF($7,''),'ST-'||upper(substr(replace(gen_random_uuid()::text,'-',''),1,10))),NULLIF($8,''),NULLIF($9,'')::uuid,$10) RETURNING id`, c.Tenant, input.FromBranchID, input.ToBranchID, input.ProductID,input.BatchID,input.Quantity, strings.TrimSpace(input.Reference), strings.TrimSpace(input.Notes), c.Sub, input.RequestID).Scan(&id)
		response, _ := json.Marshal(map[string]any{"id": id, "status": "draft"})
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'stock_transfer_create',$3,$4)`, c.Tenant, input.RequestID, id, response)
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "stock transfer could not be created", 409)
			return
		}
		auditUserAction(r, db, "STOCK_TRANSFER_CREATED", id, map[string]any{"quantity": input.Quantity})
		w.WriteHeader(201)
		w.Write(response)
	}
}

func secureTransferAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		input.Reason = strings.TrimSpace(input.Reason)
		id, action := r.PathValue("id"), r.PathValue("action")
		if action != "approve" && action != "dispatch" && action != "receive" && action != "cancel" {
			http.Error(w, "invalid transfer action", 404)
			return
		}
		if action == "cancel" && input.Reason == "" {
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
		var product, from, to, status, batchID string
		var quantity, globalBalance float64
		err = tx.QueryRow(r.Context(), `SELECT t.product_id,t.from_branch_id,t.to_branch_id,t.quantity,t.status,p.stock_quantity,COALESCE(t.batch_id::text,'') FROM stock_transfers t JOIN products p ON p.id=t.product_id AND p.tenant_id=t.tenant_id WHERE t.id=$1 AND t.tenant_id=$2 FOR UPDATE OF t,p`, id, c.Tenant).Scan(&product, &from, &to, &quantity, &status, &globalBalance, &batchID)
		if err != nil {
			http.Error(w, "stock transfer not found", 404)
			return
		}
		next := ""
		switch action {
		case "approve":
			if status != "draft" {
				http.Error(w, "only a draft transfer can be approved", 409)
				return
			}
			next = "approved"
		case "dispatch":
			if status != "approved" {
				http.Error(w, "only an approved transfer can be dispatched", 409)
				return
			}
			tag, updateErr := tx.Exec(r.Context(), `UPDATE branch_stock SET quantity=quantity-$3 WHERE product_id=$1 AND branch_id=$2 AND quantity>=$3`, product, from, quantity)
			if updateErr != nil || tag.RowsAffected() == 0 {
				http.Error(w, "insufficient source branch stock", 409)
				return
			}
			if batchID != "" {
				tag, updateErr = tx.Exec(r.Context(), `UPDATE stock_batches SET available_qty=available_qty-$2 WHERE id=$1 AND product_id=$3 AND branch_id=$4 AND available_qty>=$2`, batchID, quantity, product, from)
				if updateErr != nil || tag.RowsAffected() == 0 {
					http.Error(w, "insufficient source batch stock", 409)
					return
				}
			}
			_, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'transfer_out',$2,$3,$4,'stock_transfer',$5,$6,$7,NULLIF($8,'')::uuid)`, product, -quantity, c.Tenant, from, id, globalBalance, "Dispatched to branch "+to, c.Sub)
			next = "dispatched"
		case "receive":
			if status != "dispatched" {
				http.Error(w, "only a dispatched transfer can be received", 409)
				return
			}
			_, err = tx.Exec(r.Context(), `INSERT INTO branch_stock(product_id,branch_id,quantity) VALUES($1,$2,$3) ON CONFLICT(product_id,branch_id) DO UPDATE SET quantity=branch_stock.quantity+EXCLUDED.quantity`, product, to, quantity)
			if err == nil && batchID != "" {
				_, err = tx.Exec(r.Context(), `INSERT INTO stock_batches(product_id,branch_id,batch_no,manufactured_date,expiry_date,available_qty,unit_cost,status,notes) SELECT product_id,$2,batch_no,manufactured_date,expiry_date,$3,unit_cost,'available','Received through stock transfer '||$4 FROM stock_batches WHERE id=$1`, batchID, to, quantity, id)
			}
			if err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'transfer_in',$2,$3,$4,'stock_transfer',$5,$6,$7,NULLIF($8,'')::uuid)`, product, quantity, c.Tenant, to, id, globalBalance, "Received from branch "+from, c.Sub)
			}
			next = "received"
		case "cancel":
			if status != "draft" && status != "approved" {
				http.Error(w, "a dispatched or received transfer cannot be cancelled", 409)
				return
			}
			next = "cancelled"
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, err = tx.Exec(r.Context(), `UPDATE stock_transfers SET status=$2,approved_by=CASE WHEN $2='approved' THEN NULLIF($3,'')::uuid ELSE approved_by END,approved_at=CASE WHEN $2='approved' THEN now() ELSE approved_at END,dispatched_by=CASE WHEN $2='dispatched' THEN NULLIF($3,'')::uuid ELSE dispatched_by END,dispatched_at=CASE WHEN $2='dispatched' THEN now() ELSE dispatched_at END,received_by=CASE WHEN $2='received' THEN NULLIF($3,'')::uuid ELSE received_by END,received_at=CASE WHEN $2='received' THEN now() ELSE received_at END,cancelled_by=CASE WHEN $2='cancelled' THEN NULLIF($3,'')::uuid ELSE cancelled_by END,cancelled_at=CASE WHEN $2='cancelled' THEN now() ELSE cancelled_at END,cancellation_reason=CASE WHEN $2='cancelled' THEN $4 ELSE cancellation_reason END WHERE id=$1 AND tenant_id=$5`, id, next, c.Sub, input.Reason, c.Tenant)
		if err != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "stock transfer update failed", 500)
			return
		}
		auditUserAction(r, db, "STOCK_TRANSFER_"+strings.ToUpper(next), id, map[string]any{"reason": input.Reason})
		w.WriteHeader(204)
	}
}
