package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

func registerLedgers(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("POST /customer-payments", jsonAPI(authenticated(db, financeRoles...)(secureCustomerPayment(db))))
	m.HandleFunc("POST /supplier-payments", jsonAPI(authenticated(db, financeRoles...)(secureSupplierPayment(db))))
	m.HandleFunc("POST /customer-payments/{id}/reverse", jsonAPI(authenticated(db, financeRoles...)(reverseCustomerPayment(db))))
	m.HandleFunc("POST /supplier-payments/{id}/reverse", jsonAPI(authenticated(db, financeRoles...)(reverseSupplierPayment(db))))
	m.HandleFunc("POST /finance/payments/{kind}/{id}/cheque/{action}", jsonAPI(authenticated(db, financeRoles...)(chequeAction(db))))
	m.HandleFunc("GET /customers/{id}/ledger", jsonAPI(authenticated(db, financeRoles...)(customerLedger(db))))
	m.HandleFunc("GET /suppliers/{id}/ledger", jsonAPI(authenticated(db, financeRoles...)(supplierLedger(db))))
}
func customerPayment(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			CustomerID string  `json:"customerId"`
			Amount     float64 `json:"amount"`
			Method     string  `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&p) != nil || p.CustomerID == "" || p.Amount <= 0 {
			http.Error(w, "customer and amount required", 400)
			return
		}
		tenant := claimsFrom(r).Tenant
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		tag, e := tx.Exec(r.Context(), `UPDATE customers SET balance=GREATEST(balance-$2,0) WHERE id=$1 AND tenant_id=$3`, p.CustomerID, p.Amount, tenant)
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "customer not found", 404)
			return
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO customer_payments(customer_id,amount,method)VALUES($1,$2,$3)`, p.CustomerID, p.Amount, p.Method)
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO customer_ledger(customer_id,entry_type,credit)VALUES($1,'payment',$2)`, p.CustomerID, p.Amount)
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
	}
}
func supplierPayment(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			SupplierID string  `json:"supplierId"`
			Amount     float64 `json:"amount"`
			Method     string  `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&p) != nil || p.SupplierID == "" || p.Amount <= 0 {
			http.Error(w, "supplier and amount required", 400)
			return
		}
		tenant := claimsFrom(r).Tenant
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		tag, e := tx.Exec(r.Context(), `UPDATE suppliers SET balance=GREATEST(balance-$2,0) WHERE id=$1 AND tenant_id=$3`, p.SupplierID, p.Amount, tenant)
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "supplier not found", 404)
			return
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO supplier_payments(supplier_id,amount,method)VALUES($1,$2,$3)`, p.SupplierID, p.Amount, p.Method)
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO supplier_ledger(supplier_id,entry_type,credit)VALUES($1,'payment',$2)`, p.SupplierID, p.Amount)
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
	}
}
func customerLedger(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT entry_type,debit,credit,created_at FROM customer_ledger l JOIN customers c ON c.id=l.customer_id WHERE l.customer_id=$1 AND c.tenant_id=$2 ORDER BY created_at`, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		var running float64
		for rows.Next() {
			var t string
			var d, c float64
			var at any
			if e = rows.Scan(&t, &d, &c, &at); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			running += d - c
			out = append(out, map[string]any{"type": t, "debit": d, "credit": c, "balance": running, "date": at})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func supplierLedger(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT entry_type,debit,credit,created_at FROM supplier_ledger l JOIN suppliers s ON s.id=l.supplier_id WHERE l.supplier_id=$1 AND s.tenant_id=$2 ORDER BY created_at`, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		var running float64
		for rows.Next() {
			var t string
			var d, c float64
			var at any
			if e = rows.Scan(&t, &d, &c, &at); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			running += d - c
			out = append(out, map[string]any{"type": t, "debit": d, "credit": c, "balance": running, "date": at})
		}
		json.NewEncoder(w).Encode(out)
	}
}
