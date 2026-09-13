package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

func registerOperations(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /purchases", jsonAPI(authenticated(db, allStaff...)(purchases(db))))
	m.HandleFunc("POST /purchases", jsonAPI(authenticated(db, stockRoles...)(purchase(db))))
	m.HandleFunc("GET /damages", jsonAPI(authenticated(db, allStaff...)(damages(db))))
	m.HandleFunc("GET /promotions", jsonAPI(authenticated(db, allStaff...)(promotions(db))))
	m.HandleFunc("POST /promotions", jsonAPI(authenticated(db)(createPromotion(db))))
	m.HandleFunc("PUT /promotions/{id}", jsonAPI(authenticated(db)(updatePromotion(db))))
	m.HandleFunc("DELETE /promotions/{id}", jsonAPI(authenticated(db)(deletePromotion(db))))
	m.HandleFunc("POST /promotions/{id}/restore", jsonAPI(authenticated(db)(restorePromotion(db))))
	m.HandleFunc("GET /promotions/{id}/usage", jsonAPI(authenticated(db)(promotionUsage(db))))
}
func damages(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT d.id,p.name,p.sku,d.quantity,d.reason,COALESCE(d.notes,''),d.status,d.reported_at,COALESCE(b.batch_no,''),COALESCE(u.name,'System'),COALESCE(d.cancellation_reason,'') FROM damaged_items d JOIN products p ON p.id=d.product_id LEFT JOIN stock_batches b ON b.id=d.batch_id LEFT JOIN users u ON u.id=d.reported_by WHERE d.tenant_id=$1 ORDER BY d.reported_at DESC`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, sku, reason, notes, status, batch, user, cancellation string
			var qty float64
			var reported any
			rows.Scan(&id, &name, &sku, &qty, &reason, &notes, &status, &reported, &batch, &user, &cancellation)
			out = append(out, map[string]any{"id": id, "product": name, "sku": sku, "quantity": qty, "reason": reason, "notes": notes, "status": status, "reportedAt": reported, "batchNo": batch, "user": user, "cancellationReason": cancellation})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func purchases(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT p.id,p.invoice_no,s.name,p.total,p.created_at FROM purchases p JOIN suppliers s ON s.id=p.supplier_id WHERE p.tenant_id=$1 ORDER BY p.created_at DESC`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, invoice, supplier string
			var total float64
			var created any
			rows.Scan(&id, &invoice, &supplier, &total, &created)
			out = append(out, map[string]any{"id": id, "invoice": invoice, "supplier": supplier, "total": total, "createdAt": created})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func purchase(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			InvoiceNo  string `json:"invoiceNo"`
			SupplierID string `json:"supplierId"`
			Items      []struct {
				ProductID  string  `json:"productId"`
				Quantity   float64 `json:"quantity"`
				UnitCost   float64 `json:"unitCost"`
				BatchNo    string  `json:"batchNo"`
				ExpiryDate *string `json:"expiryDate"`
			} `json:"items"`
		}
		if json.NewDecoder(r.Body).Decode(&p) != nil || p.InvoiceNo == "" || p.SupplierID == "" || len(p.Items) == 0 {
			http.Error(w, "invoice, supplier and items required", 400)
			return
		}
		tenant := claimsFrom(r).Tenant
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var total float64
		for _, x := range p.Items {
			if x.ProductID == "" || x.Quantity <= 0 || x.UnitCost < 0 {
				http.Error(w, "valid purchase items required", 400)
				return
			}
			total += x.Quantity * x.UnitCost
		}
		var id string
		e = tx.QueryRow(r.Context(), `INSERT INTO purchases(invoice_no,supplier_id,total,tenant_id)VALUES($1,$2,$3,$4)RETURNING id`, p.InvoiceNo, p.SupplierID, total, tenant).Scan(&id)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		for _, x := range p.Items {
			_, e = tx.Exec(r.Context(), `INSERT INTO purchase_items(purchase_id,product_id,quantity,unit_cost,expiry_date,total)VALUES($1,$2,$3,$4,NULLIF($5,'')::date,$6)`, id, x.ProductID, x.Quantity, x.UnitCost, x.ExpiryDate, x.Quantity*x.UnitCost)
			if e == nil {
				_, e = tx.Exec(r.Context(), `INSERT INTO stock_batches(product_id,batch_no,expiry_date,available_qty,unit_cost)VALUES($1,NULLIF($2,''),NULLIF($3,'')::date,$4,$5)`, x.ProductID, x.BatchNo, x.ExpiryDate, x.Quantity, x.UnitCost)
			}
			if e == nil {
				_, e = tx.Exec(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2,purchase_price=$3 WHERE id=$1 AND tenant_id=$4`, x.ProductID, x.Quantity, x.UnitCost, tenant)
			}
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		_, e = tx.Exec(r.Context(), `UPDATE suppliers SET balance=balance+$2 WHERE id=$1 AND tenant_id=$3`, p.SupplierID, total, tenant)
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO supplier_ledger(supplier_id,entry_type,reference_id,debit)VALUES($1,'purchase',$2,$3)`, p.SupplierID, id, total)
		}
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "total": total})
	}
}
func damage(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			ProductID, Reason string
			Quantity          float64
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.ProductID == "" || x.Quantity <= 0 {
			http.Error(w, "product and quantity required", 400)
			return
		}
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		tag, e := tx.Exec(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND tenant_id=$3 AND stock_quantity>=$2`, x.ProductID, x.Quantity, claimsFrom(r).Tenant)
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "insufficient stock", 409)
			return
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO damaged_items(product_id,quantity,reason)VALUES($1,$2,$3)`, x.ProductID, x.Quantity, x.Reason)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		tx.Commit(r.Context())
		w.WriteHeader(201)
	}
}
