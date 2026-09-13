package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

type paymentInput struct {
	PartyID    string  `json:"partyId"`
	PurchaseID string  `json:"purchaseId"`
	Amount     float64 `json:"amount"`
	Method     string  `json:"method"`
	AccountID  string  `json:"accountId"`
	Date       string  `json:"date"`
	Reference  string  `json:"reference"`
	Notes      string  `json:"notes"`
	ChequeNumber string `json:"chequeNumber"`
	ChequeDate string `json:"chequeDate"`
	RequestID  string  `json:"requestId"`
}

func secureCustomerPayment(db *pgxpool.Pool) http.HandlerFunc { return securePayment(db, "customer") }
func secureSupplierPayment(db *pgxpool.Pool) http.HandlerFunc { return securePayment(db, "supplier") }
func securePayment(db *pgxpool.Pool, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x paymentInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.PartyID == "" || x.Amount <= 0 || strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "party, positive amount and request ID required", 400)
			return
		}
		if x.Method != "cash" && x.Method != "bank" {
			http.Error(w, "payment method must be cash or bank", 400)
			return
		}
		if strings.TrimSpace(x.AccountID) == "" {
			http.Error(w, "cash or bank account is required", 400)
			return
		}
		if strings.TrimSpace(x.ChequeNumber) != "" && (x.Method != "bank" || strings.TrimSpace(x.ChequeDate) == "") {
			http.Error(w, "cheques require a bank account and cheque date", 400)
			return
		}
		if strings.TrimSpace(x.ChequeNumber) == "" && strings.TrimSpace(x.ChequeDate) != "" {
			http.Error(w, "cheque number is required with cheque date", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		operation := kind + "_payment"
		var prior []byte
		e = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation=$3`, c.Tenant, x.RequestID, operation).Scan(&prior)
		if e == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(prior)
			return
		}
		if e != pgx.ErrNoRows {
			http.Error(w, e.Error(), 500)
			return
		}
		if x.AccountID != "" {
			var accountType string
			e = tx.QueryRow(r.Context(), `SELECT account_type FROM cash_accounts WHERE id=$1 AND tenant_id=$2 AND is_active FOR UPDATE`, x.AccountID, c.Tenant).Scan(&accountType)
			if e != nil || accountType != x.Method {
				http.Error(w, "selected account does not match payment method", 409)
				return
			}
			if kind == "supplier" {
				var available float64
				e = tx.QueryRow(r.Context(), `SELECT a.opening_balance+COALESCE(sum(CASE WHEN t.transaction_type IN('deposit','income','transfer_in') THEN t.amount ELSE -t.amount END),0) FROM cash_accounts a LEFT JOIN cash_transactions t ON t.account_id=a.id AND t.status='finalized' WHERE a.id=$1 AND a.tenant_id=$2 GROUP BY a.id`, x.AccountID, c.Tenant).Scan(&available)
				if e != nil || available < x.Amount {
					http.Error(w, "payment account balance is insufficient", 409)
					return
				}
			}
		}
		table, partyColumn, payments, ledger := "customers", "customer_id", "customer_payments", "customer_ledger"
		if kind == "supplier" {
			table, partyColumn, payments, ledger = "suppliers", "supplier_id", "supplier_payments", "supplier_ledger"
		}
		var outstanding float64
		e = tx.QueryRow(r.Context(), `SELECT balance FROM `+table+` WHERE id=$1 AND tenant_id=$2 AND is_active FOR UPDATE`, x.PartyID, c.Tenant).Scan(&outstanding)
		if e != nil {
			http.Error(w, kind+" not found", 404)
			return
		}
		if x.Amount > outstanding {
			http.Error(w, "payment exceeds outstanding balance", 409)
			return
		}
		purchaseOutstanding := 0.0
		if kind == "supplier" && strings.TrimSpace(x.PurchaseID) != "" {
			var purchaseSupplier, purchaseStatus string
			var total, paid, allocated float64
			e = tx.QueryRow(r.Context(), `SELECT p.supplier_id,p.status,p.total,p.paid_amount,COALESCE((SELECT sum(a.amount) FROM purchase_payment_allocations a JOIN supplier_payments sp ON sp.id=a.supplier_payment_id WHERE a.purchase_id=p.id AND a.tenant_id=p.tenant_id AND sp.status='finalized'),0) FROM purchases p WHERE p.id=$1 AND p.tenant_id=$2 FOR UPDATE`, x.PurchaseID, c.Tenant).Scan(&purchaseSupplier, &purchaseStatus, &total, &paid, &allocated)
			if e != nil || purchaseSupplier != x.PartyID || purchaseStatus != "finalized" {
				http.Error(w, "finalized GRN does not belong to this supplier", 409)
				return
			}
			purchaseOutstanding = total - paid - allocated
			if purchaseOutstanding <= 0 || x.Amount > purchaseOutstanding+0.0001 {
				http.Error(w, "payment exceeds this GRN outstanding balance", 409)
				return
			}
		}
		var id string
		query := `INSERT INTO ` + payments + `(tenant_id,` + partyColumn + `,amount,method,account_id,payment_date,reference,notes,client_request_id,cheque_number,cheque_date,clearance_status,` + map[string]string{"customer": "received_by", "supplier": "paid_by"}[kind] + `)VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,COALESCE(NULLIF($6,'')::date,current_date),NULLIF($7,''),NULLIF($8,''),$9,NULLIF($10,''),NULLIF($11,'')::date,CASE WHEN NULLIF($10,'') IS NULL THEN 'not_applicable' ELSE 'pending' END,$12)RETURNING id`
		e = tx.QueryRow(r.Context(), query, c.Tenant, x.PartyID, x.Amount, x.Method, x.AccountID, x.Date, x.Reference, x.Notes, x.RequestID, strings.TrimSpace(x.ChequeNumber), x.ChequeDate, c.Sub).Scan(&id)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		if kind == "supplier" && strings.TrimSpace(x.PurchaseID) != "" {
			if _, e = tx.Exec(r.Context(), `INSERT INTO purchase_payment_allocations(tenant_id,purchase_id,supplier_payment_id,amount,created_by) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid)`, c.Tenant, x.PurchaseID, id, x.Amount, c.Sub); e != nil {
				http.Error(w, "purchase payment allocation failed: "+e.Error(), 409)
				return
			}
			purchaseOutstanding -= x.Amount
		}
		if _, e = tx.Exec(r.Context(), `UPDATE `+table+` SET balance=balance-$2 WHERE id=$1 AND tenant_id=$3`, x.PartyID, x.Amount, c.Tenant); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		entry := "payment"
		if _, e = tx.Exec(r.Context(), `INSERT INTO `+ledger+`(tenant_id,`+partyColumn+`,entry_type,reference_id,credit)VALUES($1,$2,$3,$4,$5)`, c.Tenant, x.PartyID, entry, id, x.Amount); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if x.AccountID != "" {
			cashType := "income"
			if kind == "supplier" {
				cashType = "withdrawal"
			}
			var journalID string
			e = tx.QueryRow(r.Context(), `SELECT id FROM journal_entries WHERE tenant_id=$1 AND reference_type=$2 AND reference_id=$3 AND status='posted' ORDER BY created_at DESC LIMIT 1`, c.Tenant, operation, id).Scan(&journalID)
			if e != nil {
				http.Error(w, "payment journal was not created", 500)
				return
			}
			if _, e = tx.Exec(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,transaction_date,created_by,tenant_id,client_request_id,reference_type,reference_id,journal_entry_id)VALUES($1,$2,$3,NULLIF($4,''),$5,COALESCE(NULLIF($6,'')::date,current_date),$7,$8,$9,$10,$11,$12)`, x.AccountID, cashType, x.Amount, x.Reference, kind+" payment", x.Date, c.Sub, c.Tenant, x.RequestID+"-cash", operation, id, journalID); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		response, _ := json.Marshal(map[string]any{"id": id, "amount": x.Amount, "outstanding": outstanding - x.Amount, "purchaseId": x.PurchaseID, "purchaseOutstanding": purchaseOutstanding})
		if _, e = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response)VALUES($1,$2,$3,$4,$5)`, c.Tenant, x.RequestID, operation, id, response); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write(response)
		if kind == "supplier" && strings.TrimSpace(x.PurchaseID) != "" {
			auditUserAction(r, db, "GRN_PAYMENT_ALLOCATED", x.PurchaseID, map[string]any{"paymentId": id, "amount": x.Amount})
		}
	}
}

func reverseCustomerPayment(db *pgxpool.Pool) http.HandlerFunc { return reversePayment(db, "customer") }
func reverseSupplierPayment(db *pgxpool.Pool) http.HandlerFunc { return reversePayment(db, "supplier") }
func reversePayment(db *pgxpool.Pool, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Reason    string `json:"reason"`
			RequestID string `json:"requestId"`
		}
		json.NewDecoder(r.Body).Decode(&x)
		if strings.TrimSpace(x.Reason) == "" || strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "reversal reason and request ID required", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		operation := kind + "_payment"
		reversalOperation := operation + "_reversal"
		var prior []byte
		e = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation=$3`, c.Tenant, x.RequestID, reversalOperation).Scan(&prior)
		if e == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(prior)
			return
		}
		if e != pgx.ErrNoRows {
			http.Error(w, e.Error(), 500)
			return
		}
		table, partyColumn, payments, ledger := "customers", "customer_id", "customer_payments", "customer_ledger"
		if kind == "supplier" {
			table, partyColumn, payments, ledger = "suppliers", "supplier_id", "supplier_payments", "supplier_ledger"
		}
		var party string
		var amount float64
		var account *string
		query := `SELECT ` + partyColumn + `,amount,account_id::text FROM ` + payments + ` WHERE id=$1 AND tenant_id=$2 AND status='finalized' FOR UPDATE`
		e = tx.QueryRow(r.Context(), query, r.PathValue("id"), c.Tenant).Scan(&party, &amount, &account)
		if e != nil {
			http.Error(w, "finalized payment not found", 409)
			return
		}
		// Reversing a customer receipt withdraws money from the selected account.
		// A supplier-payment reversal is an inflow and does not require this check.
		if kind == "customer" && account != nil {
			var available float64
			e = tx.QueryRow(r.Context(), `SELECT opening_balance+COALESCE(sum(CASE WHEN transaction_type IN('deposit','income','transfer_in') THEN amount ELSE -amount END),0) FROM cash_accounts a LEFT JOIN cash_transactions t ON t.account_id=a.id AND t.status='finalized' WHERE a.id=$1 AND a.tenant_id=$2 GROUP BY a.id`, *account, c.Tenant).Scan(&available)
			if e != nil || available < amount {
				http.Error(w, "insufficient account balance to reverse customer payment", 409)
				return
			}
		}
		var originalJournal, reversalJournal string
		e = tx.QueryRow(r.Context(), `SELECT id FROM journal_entries WHERE tenant_id=$1 AND reference_type=$2 AND reference_id=$3 AND status='posted' ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, c.Tenant, operation, r.PathValue("id")).Scan(&originalJournal)
		if e != nil {
			http.Error(w, "posted payment journal not found", 409)
			return
		}
		e = tx.QueryRow(r.Context(), `INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,reversed_entry_id,reversal_reason,source) VALUES($1,current_date,$2,$3,$4,$5,'posted',now(),$6,$7,'payment') RETURNING id`, c.Tenant, operation+"_reversal", r.PathValue("id"), kind+" payment reversal", c.Sub, originalJournal, strings.TrimSpace(x.Reason)).Scan(&reversalJournal)
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) SELECT $1,account_id,credit,debit,$2 FROM journal_lines WHERE journal_id=$3`, reversalJournal, strings.TrimSpace(x.Reason), originalJournal)
		}
		if e == nil {
			_, e = tx.Exec(r.Context(), `UPDATE journal_entries SET status='reversed',reversed_by=$2,reversal_reason=$3 WHERE id=$1`, originalJournal, c.Sub, strings.TrimSpace(x.Reason))
		}
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		if _, e = tx.Exec(r.Context(), `UPDATE `+table+` SET balance=balance+$2 WHERE id=$1 AND tenant_id=$3`, party, amount, c.Tenant); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO `+ledger+`(tenant_id,`+partyColumn+`,entry_type,reference_id,debit)VALUES($1,$2,'payment_reversal',$3,$4)`, c.Tenant, party, r.PathValue("id"), amount); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if account != nil {
			cashType := "withdrawal"
			if kind == "supplier" {
				cashType = "income"
			}
			if _, e = tx.Exec(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,created_by,tenant_id,reference_type,reference_id,journal_entry_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, *account, cashType, amount, r.PathValue("id"), kind+" payment reversal: "+x.Reason, c.Sub, c.Tenant, operation+"_reversal", r.PathValue("id"), reversalJournal); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		query = `UPDATE ` + payments + ` SET status='reversed',reversed_by=$2,reversed_at=now(),reversal_reason=$3,clearance_status=CASE WHEN clearance_status='pending' THEN 'bounced' ELSE clearance_status END WHERE id=$1`
		if _, e = tx.Exec(r.Context(), query, r.PathValue("id"), c.Sub, x.Reason); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		response, _ := json.Marshal(map[string]any{"id": r.PathValue("id"), "status": "reversed"})
		if _, e = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,$3,$4,$5)`, c.Tenant, x.RequestID, reversalOperation, r.PathValue("id"), response); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	}
}
