package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

func listBranches(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT id,name,code,COALESCE(address,''),COALESCE(phone,''),COALESCE(email,''),is_active FROM branches WHERE tenant_id=$1 AND ($2 OR is_active) ORDER BY is_active DESC,name`, claimsFrom(r).Tenant, r.URL.Query().Get("includeInactive") == "true")
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, code, address, phone, email string; var active bool
			rows.Scan(&id, &name, &code, &address, &phone, &email, &active)
			out = append(out, map[string]any{"id": id, "name": name, "code":code, "address":address, "phone":phone, "email":email, "active":active})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func branchStock(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		branchID := r.PathValue("id")
		var exists bool
		if err := db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM branches WHERE id=$1 AND tenant_id=$2)`, branchID, c.Tenant).Scan(&exists); err != nil || !exists {
			http.Error(w, "inventory branch not found", http.StatusNotFound)
			return
		}
		rows, err := db.Query(r.Context(), `SELECT p.id,p.name,p.sku,COALESCE(bs.quantity,0),p.unit
			FROM products p LEFT JOIN branch_stock bs ON bs.product_id=p.id AND bs.branch_id=$1
			WHERE p.tenant_id=$2 AND p.is_active ORDER BY p.name`, branchID, c.Tenant)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, sku, unit string
			var quantity float64
			if err := rows.Scan(&id, &name, &sku, &quantity, &unit); err != nil { continue }
			out = append(out, map[string]any{"id": id, "name": name, "sku": sku, "quantity": quantity, "unit": unit})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func movementHistory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := "%" + r.URL.Query().Get("q") + "%"
		rows, e := db.Query(r.Context(), `SELECT m.id,p.name,p.sku,m.kind,m.quantity,m.balance_after,COALESCE(m.reference_type,''),COALESCE(m.notes,''),COALESCE(u.name,'System'),m.created_at FROM stock_movements m JOIN products p ON p.id=m.product_id LEFT JOIN users u ON u.id=m.created_by WHERE m.tenant_id=$1 AND (p.name ILIKE $2 OR p.sku ILIKE $2 OR m.kind ILIKE $2) ORDER BY m.created_at DESC LIMIT 500`, claimsFrom(r).Tenant, q)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, sku, kind, ref, notes, user string
			var qty float64
			var balance, at any
			rows.Scan(&id, &name, &sku, &kind, &qty, &balance, &ref, &notes, &user, &at)
			out = append(out, map[string]any{"id": id, "product": name, "sku": sku, "kind": kind, "quantity": qty, "balance": balance, "reference": ref, "notes": notes, "user": user, "createdAt": at})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func listTransfers(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT t.id,fb.name,tb.name,p.name,t.quantity,t.status,COALESCE(t.reference,''),COALESCE(t.notes,''),t.created_at,COALESCE(t.document_no,''),COALESCE(b.batch_no,'') FROM stock_transfers t JOIN branches fb ON fb.id=t.from_branch_id JOIN branches tb ON tb.id=t.to_branch_id JOIN products p ON p.id=t.product_id LEFT JOIN stock_batches b ON b.id=t.batch_id WHERE t.tenant_id=$1 ORDER BY t.created_at DESC`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, from, to, product, status, ref, notes, documentNo, batchNo string
			var qty float64
			var at any
			rows.Scan(&id, &from, &to, &product, &qty, &status, &ref, &notes, &at, &documentNo, &batchNo)
			out = append(out, map[string]any{"id": id, "from": from, "to": to, "product": product, "quantity": qty, "status": status, "reference": ref, "documentNo": documentNo, "batchNo": batchNo, "notes": notes, "createdAt": at})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func createTransfer(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			ProductID, FromBranchID, ToBranchID, Reference, Notes string
			Quantity                                              float64
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.ProductID == "" || x.FromBranchID == "" || x.ToBranchID == "" || x.FromBranchID == x.ToBranchID || x.Quantity <= 0 {
			http.Error(w, "valid source, destination, product and quantity required", 400)
			return
		}
		var id string
		e := db.QueryRow(r.Context(), `INSERT INTO stock_transfers(tenant_id,from_branch_id,to_branch_id,product_id,quantity,status,reference,notes,transferred_by)VALUES($1,$2,$3,$4,$5,'draft',$6,$7,$8)RETURNING id`, claimsFrom(r).Tenant, x.FromBranchID, x.ToBranchID, x.ProductID, x.Quantity, x.Reference, x.Notes, claimsFrom(r).Sub).Scan(&id)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func transferAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, a := r.PathValue("id"), r.PathValue("action")
		c := claimsFrom(r)
		allowed := map[string][2]string{"approve": {"draft", "approved"}, "dispatch": {"approved", "dispatched"}, "receive": {"dispatched", "received"}, "cancel": {"draft", "cancelled"}}
		flow, ok := allowed[a]
		if !ok {
			http.Error(w, "invalid transfer action", 404)
			return
		}
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var product, from, to string
		var qty float64
		e = tx.QueryRow(r.Context(), `SELECT product_id,from_branch_id,to_branch_id,quantity FROM stock_transfers WHERE id=$1 AND tenant_id=$2 AND status=$3 FOR UPDATE`, id, c.Tenant, flow[0]).Scan(&product, &from, &to, &qty)
		if e != nil {
			http.Error(w, "transfer is not in the required state", 409)
			return
		}
		if a == "dispatch" {
			tag, e := tx.Exec(r.Context(), `UPDATE branch_stock SET quantity=quantity-$3 WHERE product_id=$1 AND branch_id=$2 AND quantity>=$3`, product, from, qty)
			if e != nil || tag.RowsAffected() == 0 {
				http.Error(w, "insufficient source branch stock", 409)
				return
			}
		}
		if a == "receive" {
			_, e = tx.Exec(r.Context(), `INSERT INTO branch_stock(product_id,branch_id,quantity)VALUES($1,$2,$3)ON CONFLICT(product_id,branch_id)DO UPDATE SET quantity=branch_stock.quantity+EXCLUDED.quantity`, product, to, qty)
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		_, e = tx.Exec(r.Context(), `UPDATE stock_transfers SET status=$2,approved_by=CASE WHEN $2='approved' THEN $3 ELSE approved_by END,dispatched_at=CASE WHEN $2='dispatched' THEN now() ELSE dispatched_at END,received_at=CASE WHEN $2='received' THEN now() ELSE received_at END WHERE id=$1`, id, flow[1], c.Sub)
		if e != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "transfer update failed", 500)
			return
		}
		w.WriteHeader(204)
	}
}
func updateBatch(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct{ BatchNo, ExpiryDate, Notes, Reason string }
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.BatchNo) == "" || strings.TrimSpace(x.ExpiryDate) == "" || strings.TrimSpace(x.Reason) == "" {
			http.Error(w, "batch number, expiry date and correction reason are required", 400)
			return
		}
		c := claimsFrom(r)
		id := r.PathValue("id")
		tag, e := db.Exec(r.Context(), `UPDATE stock_batches b SET batch_no=$1,expiry_date=$2::date,notes=concat_ws(E'\n',NULLIF($3,''),'Correction: '||$4)
			WHERE b.id=$5 AND b.status='available' AND b.available_qty>0
			AND NOT EXISTS(SELECT 1 FROM sale_item_batches sib WHERE sib.batch_id=b.id)
			AND NOT EXISTS(SELECT 1 FROM purchase_items pi JOIN purchase_return_items pri ON pri.purchase_item_id=pi.id JOIN purchase_returns pr ON pr.id=pri.purchase_return_id WHERE pi.batch_id=b.id AND pr.status='finalized')
			AND EXISTS(SELECT 1 FROM products p WHERE p.id=b.product_id AND p.tenant_id=$6)`, strings.TrimSpace(x.BatchNo), x.ExpiryDate, strings.TrimSpace(x.Notes), strings.TrimSpace(x.Reason), id, c.Tenant)
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "batch cannot be edited after sale, return, depletion or removal", 409)
			return
		}
		auditUserAction(r, db, "BATCH_EXPIRY_CORRECTED", id, map[string]any{"batchNo": x.BatchNo, "expiryDate": x.ExpiryDate, "reason": x.Reason})
		w.WriteHeader(204)
	}
}
func expireBatch(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&x)
		if strings.TrimSpace(x.Reason) == "" {
			http.Error(w, "reason required", 400)
			return
		}
		c := claimsFrom(r)
		tx, _ := db.Begin(r.Context())
		defer tx.Rollback(r.Context())
		var product string
		var qty float64
		e := tx.QueryRow(r.Context(), `SELECT b.product_id,b.available_qty FROM stock_batches b JOIN products p ON p.id=b.product_id WHERE b.id=$1 AND p.tenant_id=$2 AND b.status='available' FOR UPDATE`, r.PathValue("id"), c.Tenant).Scan(&product, &qty)
		if e != nil {
			http.Error(w, "available batch not found", 404)
			return
		}
		var bal float64
		e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND stock_quantity>=$2 RETURNING stock_quantity`, product, qty).Scan(&bal)
		if e != nil {
			http.Error(w, "stock mismatch", 409)
			return
		}
		tx.Exec(r.Context(), `UPDATE stock_batches SET available_qty=0,status='expired',notes=concat_ws(E'\n',notes,$2) WHERE id=$1`, r.PathValue("id"), x.Reason)
		tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by)VALUES($1,'expired',$2,$3,NULLIF($4,'')::uuid,'batch',$5,$6,$7,$8)`, product, -qty, c.Tenant, c.Branch, r.PathValue("id"), bal, x.Reason, c.Sub)
		if tx.Commit(r.Context()) != nil {
			http.Error(w, "expiry adjustment failed", 500)
			return
		}
		w.WriteHeader(204)
	}
}
func createDamageDraft(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			ProductID, BatchID, Reason, Notes string
			Quantity                          float64
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.ProductID == "" || x.Quantity <= 0 || x.Reason == "" {
			http.Error(w, "product, quantity and reason required", 400)
			return
		}
		var id string
		e := db.QueryRow(r.Context(), `INSERT INTO damaged_items(tenant_id,product_id,batch_id,quantity,reason,notes,status,reported_by)VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,'draft',$7)RETURNING id`, claimsFrom(r).Tenant, x.ProductID, x.BatchID, x.Quantity, x.Reason, x.Notes, claimsFrom(r).Sub).Scan(&id)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func damageAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, a := r.PathValue("id"), r.PathValue("action")
		if a == "cancel" {
			tag, _ := db.Exec(r.Context(), `UPDATE damaged_items SET status='cancelled',cancelled_at=now() WHERE id=$1 AND tenant_id=$2 AND status='draft'`, id, claimsFrom(r).Tenant)
			if tag.RowsAffected() == 0 {
				http.Error(w, "draft damage not found", 409)
				return
			}
			w.WriteHeader(204)
			return
		}
		if a != "finalize" {
			http.Error(w, "unknown action", 404)
			return
		}
		c := claimsFrom(r)
		tx, _ := db.Begin(r.Context())
		defer tx.Rollback(r.Context())
		var product string
		var qty float64
		e := tx.QueryRow(r.Context(), `SELECT product_id,quantity FROM damaged_items WHERE id=$1 AND tenant_id=$2 AND status='draft' FOR UPDATE`, id, c.Tenant).Scan(&product, &qty)
		if e != nil {
			http.Error(w, "draft damage not found", 409)
			return
		}
		var bal float64
		e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND stock_quantity>=$2 RETURNING stock_quantity`, product, qty).Scan(&bal)
		if e != nil {
			http.Error(w, "insufficient stock", 409)
			return
		}
		tx.Exec(r.Context(), `UPDATE damaged_items SET status='finalized' WHERE id=$1`, id)
		tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,created_by)VALUES($1,'damage',$2,$3,NULLIF($4,'')::uuid,'damage',$5,$6,$7)`, product, -qty, c.Tenant, c.Branch, id, bal, c.Sub)
		if tx.Commit(r.Context()) != nil {
			http.Error(w, "damage finalize failed", 500)
			return
		}
		w.WriteHeader(204)
	}
}
