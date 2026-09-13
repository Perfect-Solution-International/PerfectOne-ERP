package main

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

type directoryInput struct {
	Name           string  `json:"name"`
	Company        string  `json:"company"`
	Phone          string  `json:"phone"`
	Email          string  `json:"email"`
	Address        string  `json:"address"`
	TaxInformation string  `json:"taxInformation"`
	OpeningBalance float64 `json:"openingBalance"`
	CreditLimit    float64 `json:"creditLimit"`
	PaymentTerms   string  `json:"paymentTerms"`
	CustomerType   string  `json:"customerType"`
	Notes          string  `json:"notes"`
	IsActive       bool    `json:"isActive"`
}

func registerContacts(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /directory/{kind}", jsonAPI(authenticated(db, financeRoles...)(directoryList(db))))
	m.HandleFunc("POST /directory/{kind}", jsonAPI(authenticated(db, financeRoles...)(directoryCreate(db))))
	m.HandleFunc("GET /directory/{kind}/{id}", jsonAPI(authenticated(db, financeRoles...)(directoryProfile(db))))
	m.HandleFunc("PUT /directory/{kind}/{id}", jsonAPI(authenticated(db, financeRoles...)(directoryUpdate(db))))
	m.HandleFunc("DELETE /directory/{kind}/{id}", jsonAPI(authenticated(db, financeRoles...)(directoryDelete(db))))
	m.HandleFunc("POST /directory/{kind}/{id}/status", jsonAPI(authenticated(db, financeRoles...)(directoryStatus(db))))
}
func directoryTable(kind string) (string, bool) {
	if kind == "suppliers" || kind == "customers" {
		return kind, true
	}
	return "", false
}
func directoryList(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table, ok := directoryTable(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid directory", 404)
			return
		}
		var q string
		if table == "suppliers" {
			q = `SELECT id,name,COALESCE(company,''),COALESCE(phone,''),COALESCE(email,''),COALESCE(address,''),COALESCE(tax_information,''),balance,credit_limit,COALESCE(payment_terms,''),COALESCE(notes,''),is_active FROM suppliers WHERE tenant_id=$1 ORDER BY is_active DESC,name`
		} else {
			q = `SELECT id,name,'',COALESCE(phone,''),COALESCE(email,''),COALESCE(address,''),'',balance,credit_limit,COALESCE(customer_type,'retail'),COALESCE(notes,''),is_active FROM customers WHERE tenant_id=$1 ORDER BY is_active DESC,name`
		}
		rows, e := db.Query(r.Context(), q, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, company, phone, email, address, tax, terms, notes string
			var balance, limit float64
			var active bool
			rows.Scan(&id, &name, &company, &phone, &email, &address, &tax, &balance, &limit, &terms, &notes, &active)
			out = append(out, map[string]any{"id": id, "name": name, "company": company, "phone": phone, "email": email, "address": address, "taxInformation": tax, "balance": balance, "creditLimit": limit, "detail": terms, "notes": notes, "isActive": active})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func directoryCreate(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table, ok := directoryTable(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid directory", 404)
			return
		}
		var x directoryInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" || x.OpeningBalance < 0 || x.CreditLimit < 0 {
			http.Error(w, "valid name and balances required", 400)
			return
		}
		c := claimsFrom(r)
		var id string
		var e error
		if table == "suppliers" {
			e = db.QueryRow(r.Context(), `INSERT INTO suppliers(tenant_id,name,company,phone,email,address,tax_information,balance,credit_limit,payment_terms,notes,is_active)VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,NULLIF($10,''),NULLIF($11,''),true)RETURNING id`, c.Tenant, x.Name, x.Company, x.Phone, x.Email, x.Address, x.TaxInformation, x.OpeningBalance, x.CreditLimit, x.PaymentTerms, x.Notes).Scan(&id)
		} else {
			if x.CustomerType == "" {
				x.CustomerType = "retail"
			}
			e = db.QueryRow(r.Context(), `INSERT INTO customers(tenant_id,name,phone,email,address,balance,credit_limit,customer_type,notes,is_active)VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),$6,$7,$8,NULLIF($9,''),true)RETURNING id`, c.Tenant, x.Name, x.Phone, x.Email, x.Address, x.OpeningBalance, x.CreditLimit, x.CustomerType, x.Notes).Scan(&id)
		}
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		if x.OpeningBalance > 0 {
			ledger := strings.TrimSuffix(table, "s") + "_ledger"
			column := strings.TrimSuffix(table, "s") + "_id"
			db.Exec(r.Context(), fmt.Sprintf(`INSERT INTO %s(%s,entry_type,debit)VALUES($1,'opening_balance',$2)`, ledger, column), id, x.OpeningBalance)
		}
		auditUserAction(r, db, "CONTACT_CREATED", id, map[string]any{"type": table, "name": x.Name})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func directoryUpdate(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table, ok := directoryTable(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid directory", 404)
			return
		}
		var x directoryInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" || x.CreditLimit < 0 {
			http.Error(w, "valid details required", 400)
			return
		}
		var q string
		var args []any
		if table == "suppliers" {
			q = `UPDATE suppliers SET name=$1,company=NULLIF($2,''),phone=NULLIF($3,''),email=NULLIF($4,''),address=NULLIF($5,''),tax_information=NULLIF($6,''),credit_limit=$7,payment_terms=NULLIF($8,''),notes=NULLIF($9,''),updated_at=now() WHERE id=$10 AND tenant_id=$11`
			args = []any{x.Name, x.Company, x.Phone, x.Email, x.Address, x.TaxInformation, x.CreditLimit, x.PaymentTerms, x.Notes, r.PathValue("id"), claimsFrom(r).Tenant}
		} else {
			if x.CustomerType == "" {
				x.CustomerType = "retail"
			}
			q = `UPDATE customers SET name=$1,phone=NULLIF($2,''),email=NULLIF($3,''),address=NULLIF($4,''),credit_limit=$5,customer_type=$6,notes=NULLIF($7,''),updated_at=now() WHERE id=$8 AND tenant_id=$9`
			args = []any{x.Name, x.Phone, x.Email, x.Address, x.CreditLimit, x.CustomerType, x.Notes, r.PathValue("id"), claimsFrom(r).Tenant}
		}
		tag, e := db.Exec(r.Context(), q, args...)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "not found", 404)
			return
		}
		auditUserAction(r, db, "CONTACT_UPDATED", r.PathValue("id"), map[string]any{"type": table})
		w.WriteHeader(204)
	}
}
func directoryDelete(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table, ok := directoryTable(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid directory", 404)
			return
		}
		id := r.PathValue("id")
		reference := "supplier_id"
		transaction := "purchases"
		if table == "customers" {
			reference = "customer_id"
			transaction = "sales"
		}
		var used bool
		db.QueryRow(r.Context(), fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE %s=$1)`, transaction, reference), id).Scan(&used)
		if used {
			tag, e := db.Exec(r.Context(), fmt.Sprintf(`UPDATE %s SET is_active=false,archived_at=now(),updated_at=now() WHERE id=$1 AND tenant_id=$2`, table), id, claimsFrom(r).Tenant)
			if e != nil || tag.RowsAffected() == 0 {
				http.Error(w, "not found", 404)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"result": "archived"})
			return
		}
		tag, e := db.Exec(r.Context(), fmt.Sprintf(`DELETE FROM %s WHERE id=$1 AND tenant_id=$2`, table), id, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, "delete blocked because financial records exist", 409)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "not found", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"result": "deleted"})
	}
}
func directoryStatus(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table, ok := directoryTable(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid directory", 404)
			return
		}
		var x struct {
			IsActive bool `json:"isActive"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil {
			http.Error(w, "invalid status", 400)
			return
		}
		_, e := db.Exec(r.Context(), fmt.Sprintf(`UPDATE %s SET is_active=$1,archived_at=CASE WHEN $1 THEN NULL ELSE now() END,updated_at=now() WHERE id=$2 AND tenant_id=$3`, table), x.IsActive, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		w.WriteHeader(204)
	}
}
func directoryProfile(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table, ok := directoryTable(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid directory", 404)
			return
		}
		id := r.PathValue("id")
		transaction, reference, ledger := "purchases", "supplier_id", "supplier_ledger"
		if table == "customers" {
			transaction, reference, ledger = "sales", "customer_id", "customer_ledger"
		}
		var total, paid, outstanding float64
		db.QueryRow(r.Context(), fmt.Sprintf(`SELECT COALESCE(sum(total),0),COALESCE(sum(paid_amount),0) FROM %s WHERE %s=$1`, transaction, reference), id).Scan(&total, &paid)
		db.QueryRow(r.Context(), fmt.Sprintf(`SELECT COALESCE(balance,0) FROM %s WHERE id=$1 AND tenant_id=$2`, table), id, claimsFrom(r).Tenant).Scan(&outstanding)
		rows, e := db.Query(r.Context(), fmt.Sprintf(`SELECT id,entry_type,COALESCE(debit,0),COALESCE(credit,0),balance,created_at FROM %s WHERE %s=$1 ORDER BY created_at DESC LIMIT 100`, ledger, reference), id)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		entries := []map[string]any{}
		for rows.Next() {
			var eid, kind string
			var debit, credit, balance float64
			var at any
			rows.Scan(&eid, &kind, &debit, &credit, &balance, &at)
			entries = append(entries, map[string]any{"id": eid, "type": kind, "debit": debit, "credit": credit, "balance": balance, "createdAt": at})
		}
		json.NewEncoder(w).Encode(map[string]any{"total": total, "paid": paid, "outstanding": outstanding, "returns": 0, "ledger": entries})
	}
}
