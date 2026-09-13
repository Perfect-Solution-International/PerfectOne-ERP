package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type purchaseReturnInput struct {
	PurchaseID string `json:"purchaseId"`
	Reason     string `json:"reason"`
	RequestID  string `json:"requestId"`
	Items      []struct {
		ID       string  `json:"id"`
		Quantity float64 `json:"quantity"`
	} `json:"items"`
}

func secureCreatePurchaseReturn(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input purchaseReturnInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.PurchaseID == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.RequestID) == "" || len(input.Items) == 0 {
			http.Error(w, "purchase, reason, request ID and items are required", 400)
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
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='purchase_return_create'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}
		var supplier string
		var purchaseTotal, purchaseLineTotal float64
		err = tx.QueryRow(r.Context(), `SELECT p.supplier_id,p.total,COALESCE((SELECT sum(i.total) FROM purchase_items i WHERE i.purchase_id=p.id),0) FROM purchases p WHERE p.id=$1 AND p.tenant_id=$2 AND p.status='finalized' FOR UPDATE`, input.PurchaseID, c.Tenant).Scan(&supplier, &purchaseTotal, &purchaseLineTotal)
		if err != nil {
			http.Error(w, "finalized purchase not found", 409)
			return
		}
		seen := map[string]bool{}
		type returnLine struct {
			purchaseItem, product     string
			quantity, unitCost, total float64
		}
		lines := []returnLine{}
		returnTotal := 0.0
		for _, requested := range input.Items {
			if requested.ID == "" || requested.Quantity <= 0 || seen[requested.ID] {
				http.Error(w, "return items must be unique with positive quantities", 400)
				return
			}
			seen[requested.ID] = true
			var line returnLine
			var purchased, lineTotal, alreadyReturned float64
			err = tx.QueryRow(r.Context(), `SELECT i.product_id,i.quantity+i.free_quantity,i.total,COALESCE((SELECT sum(ri.quantity) FROM purchase_return_items ri JOIN purchase_returns rr ON rr.id=ri.purchase_return_id WHERE ri.purchase_item_id=i.id AND rr.status<>'cancelled'),0) FROM purchase_items i WHERE i.id=$1 AND i.purchase_id=$2 FOR UPDATE`, requested.ID, input.PurchaseID).Scan(&line.product, &purchased, &lineTotal, &alreadyReturned)
			if err != nil || requested.Quantity > purchased-alreadyReturned {
				http.Error(w, "return quantity exceeds remaining received stock", 409)
				return
			}
			line.purchaseItem = requested.ID
			line.quantity = requested.Quantity
			line.unitCost = allocatedDocumentLineAmount(lineTotal, purchaseTotal, purchaseLineTotal) / purchased
			line.total = math.Round(line.quantity*line.unitCost*100) / 100
			returnTotal += line.total
			lines = append(lines, line)
		}
		returnTotal = math.Round(returnTotal*100) / 100
		var returnID, number string
		err = tx.QueryRow(r.Context(), `INSERT INTO purchase_returns(tenant_id,purchase_id,return_number,supplier_id,reason,total,created_by,client_request_id) VALUES($1,$2,next_purchase_return_no(),$3,$4,$5,NULLIF($6,'')::uuid,$7) RETURNING id,return_number`, c.Tenant, input.PurchaseID, supplier, strings.TrimSpace(input.Reason), returnTotal, c.Sub, input.RequestID).Scan(&returnID, &number)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		for _, line := range lines {
			if _, err = tx.Exec(r.Context(), `INSERT INTO purchase_return_items(purchase_return_id,purchase_item_id,product_id,quantity,unit_cost,total,entered_quantity,entered_unit_id,entered_unit_cost,conversion_factor) SELECT $1,$2,$3,$4,$5,$6,$4/NULLIF(COALESCE(i.conversion_factor,1),0),i.entered_unit_id,i.entered_unit_cost,COALESCE(i.conversion_factor,1) FROM purchase_items i WHERE i.id=$2`, returnID, line.purchaseItem, line.product, line.quantity, line.unitCost, line.total); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		response, _ := json.Marshal(map[string]any{"id": returnID, "number": number, "total": returnTotal, "status": "draft"})
		if _, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'purchase_return_create',$3,$4)`, c.Tenant, input.RequestID, returnID, response); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(201)
		w.Write(response)
	}
}

func securePurchaseReturnAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, action := r.PathValue("id"), r.PathValue("action")
		c := claimsFrom(r)
		if action == "cancel" {
			var input struct {
				Reason string `json:"reason"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			input.Reason = strings.TrimSpace(input.Reason)
			if input.Reason == "" {
				http.Error(w, "cancellation reason required", 400)
				return
			}
			tag, err := db.Exec(r.Context(), `UPDATE purchase_returns SET status='cancelled',cancelled_by=NULLIF($2,'')::uuid,cancelled_at=now(),cancellation_reason=$3 WHERE id=$1 AND tenant_id=$4 AND status='draft'`, id, c.Sub, input.Reason, c.Tenant)
			if err != nil || tag.RowsAffected() == 0 {
				http.Error(w, "draft return not found", 409)
				return
			}
			auditUserAction(r, db, "PURCHASE_RETURN_CANCELLED", id, map[string]any{"reason": input.Reason})
			w.WriteHeader(204)
			return
		}
		if action != "finalize" {
			http.Error(w, "unknown return action", 404)
			return
		}
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var supplier, branch string
		var total, supplierBalance float64
		err = tx.QueryRow(r.Context(), `SELECT r.supplier_id,r.total,s.balance,COALESCE(p.branch_id::text,'') FROM purchase_returns r JOIN suppliers s ON s.id=r.supplier_id AND s.tenant_id=r.tenant_id JOIN purchases p ON p.id=r.purchase_id WHERE r.id=$1 AND r.tenant_id=$2 AND r.status='draft' FOR UPDATE OF r,s,p`, id, c.Tenant).Scan(&supplier, &total, &supplierBalance, &branch)
		if err != nil {
			http.Error(w, "draft return not found", 409)
			return
		}
		if supplierBalance < total {
			http.Error(w, "supplier payable is lower than this return; reverse supplier payments first", 409)
			return
		}
		rows, err := tx.Query(r.Context(), `SELECT ri.product_id,ri.quantity,i.batch_id FROM purchase_return_items ri JOIN purchase_items i ON i.id=ri.purchase_item_id WHERE ri.purchase_return_id=$1 ORDER BY ri.id FOR UPDATE OF ri,i`, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		type line struct {
			product  string
			quantity float64
			batch    *string
		}
		lines := []line{}
		for rows.Next() {
			var item line
			if err = rows.Scan(&item.product, &item.quantity, &item.batch); err != nil {
				rows.Close()
				http.Error(w, err.Error(), 500)
				return
			}
			lines = append(lines, item)
		}
		rows.Close()
		if len(lines) == 0 {
			http.Error(w, "return has no items", 409)
			return
		}
		for _, item := range lines {
			if item.batch == nil {
				http.Error(w, "received batch link is missing; this return requires manual reconciliation", 409)
				return
			}
			var available float64
			err = tx.QueryRow(r.Context(), `SELECT available_qty FROM stock_batches WHERE id=$1 AND product_id=$2 FOR UPDATE`, *item.batch, item.product).Scan(&available)
			if err != nil || available < item.quantity {
				http.Error(w, "returned batch stock has already been consumed", 409)
				return
			}
			if _, err = tx.Exec(r.Context(), `UPDATE stock_batches SET available_qty=available_qty-$2,status=CASE WHEN available_qty-$2=0 THEN 'returned' ELSE status END WHERE id=$1`, *item.batch, item.quantity); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			var after float64
			err = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND tenant_id=$3 AND stock_quantity>=$2 RETURNING stock_quantity`, item.product, item.quantity, c.Tenant).Scan(&after)
			if err != nil {
				http.Error(w, "insufficient product stock for supplier return", 409)
				return
			}
			if err = adjustBranchStock(r.Context(), tx, c.Tenant, branch, item.product, -item.quantity); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			if _, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,created_by) VALUES($1,'purchase_return',$2,$3,NULLIF($4,'')::uuid,'purchase_return',$5,$6,NULLIF($7,'')::uuid)`, item.product, -item.quantity, c.Tenant, c.Branch, id, after, c.Sub); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE suppliers SET balance=balance-$2 WHERE id=$1 AND tenant_id=$3`, supplier, total, c.Tenant); err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO supplier_ledger(tenant_id,supplier_id,entry_type,reference_id,credit) VALUES($1,$2,'purchase_return',$3,$4)`, c.Tenant, supplier, id, total)
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE purchase_returns SET status='finalized',finalized_by=NULLIF($2,'')::uuid,finalized_at=now() WHERE id=$1 AND tenant_id=$3`, id, c.Sub, c.Tenant); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		auditUserAction(r, db, "PURCHASE_RETURN_FINALIZED", id, map[string]any{"total": total})
		w.WriteHeader(204)
	}
}
