package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerFinanceAccounting(m *http.ServeMux, db *pgxpool.Pool) {
	registerAccountingControls(m, db)
	m.HandleFunc("GET /finance/summary", jsonAPI(authenticated(db, financeRoles...)(financeSummary(db))))
	m.HandleFunc("GET /finance/transactions", jsonAPI(authenticated(db, financeRoles...)(financeTransactions(db))))
	m.HandleFunc("POST /finance/transfers", jsonAPI(authenticated(db, financeRoles...)(createFinanceTransfer(db))))
	m.HandleFunc("POST /finance/transfers/{id}/reverse", jsonAPI(authenticated(db, financeRoles...)(reverseFinanceTransfer(db))))
	m.HandleFunc("GET /finance/payments/{kind}", jsonAPI(authenticated(db, financeRoles...)(paymentHistory(db))))
	m.HandleFunc("GET /expense-categories", jsonAPI(authenticated(db, financeRoles...)(expenseCategories(db))))
	m.HandleFunc("POST /expense-categories", jsonAPI(authenticated(db, financeRoles...)(createExpenseCategory(db))))
	m.HandleFunc("PUT /expense-categories/{id}", jsonAPI(authenticated(db, financeRoles...)(updateExpenseCategory(db))))
	m.HandleFunc("POST /expense-categories/{id}/status", jsonAPI(authenticated(db, financeRoles...)(expenseCategoryStatus(db))))
	m.HandleFunc("GET /expenses", jsonAPI(authenticated(db, financeRoles...)(listExpenses(db))))
	m.HandleFunc("POST /expenses", jsonAPI(authenticated(db, financeRoles...)(createExpense(db))))
	m.HandleFunc("PUT /expenses/{id}", jsonAPI(authenticated(db, financeRoles...)(updateExpense(db))))
	m.HandleFunc("POST /expenses/{id}/{action}", jsonAPI(authenticated(db, financeRoles...)(expenseAction(db))))
	m.HandleFunc("GET /accounting/accounts", jsonAPI(authenticated(db, financeRoles...)(accountingAccounts(db))))
	m.HandleFunc("POST /accounting/accounts", jsonAPI(authenticated(db, financeRoles...)(createAccountingAccount(db))))
	m.HandleFunc("PUT /accounting/accounts/{id}", jsonAPI(authenticated(db, financeRoles...)(updateAccountingAccount(db))))
	m.HandleFunc("POST /accounting/accounts/{id}/status", jsonAPI(authenticated(db, financeRoles...)(accountingAccountStatus(db))))
	m.HandleFunc("GET /accounting/journals", jsonAPI(authenticated(db, financeRoles...)(listJournals(db))))
	m.HandleFunc("POST /accounting/journals", jsonAPI(authenticated(db, financeRoles...)(createJournal(db))))
	m.HandleFunc("POST /accounting/journals/{id}/{action}", jsonAPI(authenticated(db, financeRoles...)(journalAction(db))))
}

func financeSummary(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		var cash, bank, receivable, payable, expenses float64
		err := db.QueryRow(r.Context(), `SELECT
		 COALESCE(sum(CASE WHEN a.account_type='cash' THEN a.opening_balance+COALESCE(x.net,0) ELSE 0 END),0),
		 COALESCE(sum(CASE WHEN a.account_type='bank' THEN a.opening_balance+COALESCE(x.net,0) ELSE 0 END),0)
		 FROM cash_accounts a LEFT JOIN (
		  SELECT account_id,sum(CASE WHEN transaction_type IN('deposit','income','transfer_in') THEN amount ELSE -amount END) net
		  FROM cash_transactions WHERE status='finalized' GROUP BY account_id
		 ) x ON x.account_id=a.id WHERE a.tenant_id=$1 AND a.is_active`, c.Tenant).Scan(&cash, &bank)
		if err == nil {
			err = db.QueryRow(r.Context(), `SELECT COALESCE(sum(balance),0) FROM customers WHERE tenant_id=$1 AND is_active`, c.Tenant).Scan(&receivable)
		}
		if err == nil {
			err = db.QueryRow(r.Context(), `SELECT COALESCE(sum(balance),0) FROM suppliers WHERE tenant_id=$1 AND is_active`, c.Tenant).Scan(&payable)
		}
		if err == nil {
			err = db.QueryRow(r.Context(), `SELECT COALESCE(sum(amount),0) FROM expenses WHERE tenant_id=$1 AND status='finalized' AND date_trunc('month',expense_date)=date_trunc('month',current_date)`, c.Tenant).Scan(&expenses)
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"cashBalance": cash, "bankBalance": bank, "customerReceivables": receivable, "supplierPayables": payable, "monthExpenses": expenses})
	}
}

func financeTransactions(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, q := claimsFrom(r), r.URL.Query()
		limit := queryLimit(q.Get("limit"), 100)
		rows, err := db.Query(r.Context(), `SELECT t.id,a.name,a.account_type,t.transaction_type,t.amount,COALESCE(t.reference,''),COALESCE(t.description,''),t.transaction_date,COALESCE(j.status,t.status),COALESCE(u.name,'System'),COALESCE(t.journal_entry_id::text,'')
		 FROM cash_transactions t JOIN cash_accounts a ON a.id=t.account_id LEFT JOIN users u ON u.id=t.created_by LEFT JOIN journal_entries j ON j.id=t.journal_entry_id
		 WHERE a.tenant_id=$1 AND ($2='' OR t.account_id::text=$2) AND ($3='' OR t.transaction_type=$3)
		 AND ($4='' OR t.reference_type=$4) AND t.transaction_date>=COALESCE(NULLIF($5,'')::date,'2000-01-01') AND t.transaction_date<=COALESCE(NULLIF($6,'')::date,current_date)
		 ORDER BY t.transaction_date DESC,t.created_at DESC LIMIT $7`, c.Tenant, q.Get("accountId"), q.Get("type"), q.Get("referenceType"), q.Get("from"), q.Get("to"), limit)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, kind, typ, ref, description, status, user, journalID string
			var amount float64
			var date any
			if rows.Scan(&id, &name, &kind, &typ, &amount, &ref, &description, &date, &status, &user, &journalID) != nil {
				continue
			}
			out = append(out, map[string]any{"id": id, "account": name, "accountType": kind, "type": typ, "amount": amount, "reference": ref, "description": description, "date": date, "status": status, "user": user, "journalEntryId": journalID})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func reverseFinanceTransfer(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		if strings.TrimSpace(input.Reason) == "" {
			http.Error(w, "reversal reason is required", http.StatusBadRequest)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())
		var description, sourceAccount, destinationAccount, sourceLedger, destinationLedger string
		var amount, destinationBalance float64
		err = tx.QueryRow(r.Context(), `SELECT COALESCE(j.description,''),o.account_id,i.account_id,o.amount,sa.ledger_account_id,da.ledger_account_id
			FROM journal_entries j
			JOIN cash_transactions o ON o.journal_entry_id=j.id AND o.transaction_type='transfer_out'
			JOIN cash_transactions i ON i.journal_entry_id=j.id AND i.transaction_type='transfer_in'
			JOIN cash_accounts sa ON sa.id=o.account_id AND sa.tenant_id=j.tenant_id
			JOIN cash_accounts da ON da.id=i.account_id AND da.tenant_id=j.tenant_id
			WHERE j.id=$1 AND j.tenant_id=$2 AND j.reference_type='cash_transfer' AND j.status='posted'
			FOR UPDATE OF j,sa,da`, r.PathValue("id"), c.Tenant).Scan(&description, &sourceAccount, &destinationAccount, &amount, &sourceLedger, &destinationLedger)
		if err == nil {
			err = tx.QueryRow(r.Context(), `SELECT a.opening_balance+COALESCE(sum(CASE WHEN ct.transaction_type IN('deposit','income','transfer_in') THEN ct.amount ELSE -ct.amount END),0) FROM cash_accounts a LEFT JOIN cash_transactions ct ON ct.account_id=a.id AND ct.status='finalized' WHERE a.id=$1 AND a.tenant_id=$2 GROUP BY a.id`, destinationAccount, c.Tenant).Scan(&destinationBalance)
		}
		if err != nil || destinationBalance < amount {
			http.Error(w, "transfer is already reversed, missing, or destination balance is insufficient", http.StatusConflict)
			return
		}
		var reversal string
		err = tx.QueryRow(r.Context(), `INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,reversed_entry_id,reversal_reason,source) VALUES($1,current_date,'cash_transfer_reversal',$2,$3,$4,'posted',now(),$2,$5,'cashbook') RETURNING id`, c.Tenant, r.PathValue("id"), "Reversal: "+description, c.Sub, strings.TrimSpace(input.Reason)).Scan(&reversal)
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) VALUES($1,$2,$3,0,$4),($1,$5,0,$3,$4)`, reversal, sourceLedger, amount, input.Reason, destinationLedger)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,created_by,tenant_id,reference_type,reference_id,journal_entry_id) VALUES($1,'transfer_in',$2,$3,$4,$5,$6,'cash_transfer_reversal',$3::uuid,$7),($8,'transfer_out',$2,$3,$4,$5,$6,'cash_transfer_reversal',$3::uuid,$7)`, sourceAccount, amount, r.PathValue("id"), "Transfer reversal: "+input.Reason, c.Sub, c.Tenant, reversal, destinationAccount)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE journal_entries SET status='reversed',reversed_by=$2,reversal_reason=$3 WHERE id=$1`, r.PathValue("id"), c.Sub, strings.TrimSpace(input.Reason))
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		auditUserAction(r, db, "CASH_TRANSFER_REVERSED", r.PathValue("id"), map[string]any{"reason": input.Reason})
		w.WriteHeader(http.StatusNoContent)
	}
}

func createFinanceTransfer(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			FromAccountID                     string  `json:"fromAccountId"`
			ToAccountID                       string  `json:"toAccountId"`
			Amount                            float64 `json:"amount"`
			Date, Reference, Notes, RequestID string
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.FromAccountID == "" || x.ToAccountID == "" || x.FromAccountID == x.ToAccountID || x.Amount <= 0 || strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "source, destination, positive amount and request ID are required", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var fromLedger, toLedger string
		var available float64
		err = tx.QueryRow(r.Context(), `SELECT a.ledger_account_id,b.ledger_account_id,a.opening_balance+COALESCE(sum(CASE WHEN t.transaction_type IN('deposit','income','transfer_in') THEN t.amount ELSE -t.amount END),0)
		 FROM cash_accounts a JOIN cash_accounts b ON b.id=$2 AND b.tenant_id=a.tenant_id LEFT JOIN cash_transactions t ON t.account_id=a.id AND t.status='finalized'
		 WHERE a.id=$1 AND a.tenant_id=$3 AND a.is_active AND b.is_active AND a.ledger_account_id IS NOT NULL AND b.ledger_account_id IS NOT NULL GROUP BY a.id,b.ledger_account_id`, x.FromAccountID, x.ToAccountID, c.Tenant).Scan(&fromLedger, &toLedger, &available)
		if err != nil || available < x.Amount {
			http.Error(w, "accounts not found or source balance is insufficient", 409)
			return
		}
		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='cash_transfer'`, c.Tenant, x.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}
		var journal string
		err = tx.QueryRow(r.Context(), `INSERT INTO journal_entries(tenant_id,entry_date,reference_type,description,created_by,status,posted_at,source,client_request_id) VALUES($1,COALESCE(NULLIF($2,'')::date,current_date),'cash_transfer',$3,$4,'posted',now(),'cashbook',$5) RETURNING id`, c.Tenant, x.Date, "Account transfer: "+x.Notes, c.Sub, x.RequestID).Scan(&journal)
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) VALUES($1,$2,$3,0,$4),($1,$5,0,$3,$4)`, journal, toLedger, x.Amount, x.Notes, fromLedger)
		}
		var outID, inID string
		if err == nil {
			err = tx.QueryRow(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,transaction_date,created_by,tenant_id,client_request_id,reference_type,journal_entry_id) VALUES($1,'transfer_out',$2,NULLIF($3,''),NULLIF($4,''),COALESCE(NULLIF($5,'')::date,current_date),$6,$7,$8||'-out','cash_transfer',$9) RETURNING id`, x.FromAccountID, x.Amount, x.Reference, x.Notes, x.Date, c.Sub, c.Tenant, x.RequestID, journal).Scan(&outID)
		}
		if err == nil {
			err = tx.QueryRow(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,transaction_date,created_by,tenant_id,client_request_id,reference_type,journal_entry_id) VALUES($1,'transfer_in',$2,NULLIF($3,''),NULLIF($4,''),COALESCE(NULLIF($5,'')::date,current_date),$6,$7,$8||'-in','cash_transfer',$9) RETURNING id`, x.ToAccountID, x.Amount, x.Reference, x.Notes, x.Date, c.Sub, c.Tenant, x.RequestID, journal).Scan(&inID)
		}
		response, _ := json.Marshal(map[string]any{"id": journal, "status": "finalized", "amount": x.Amount})
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'cash_transfer',$3,$4)`, c.Tenant, x.RequestID, journal, response)
		}
		if err != nil {
			http.Error(w, err.Error(), 409)
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

func paymentHistory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("kind")
		if kind != "customers" && kind != "suppliers" {
			http.Error(w, "unknown payment type", 404)
			return
		}
		table, partyTable, partyColumn, actorColumn := "customer_payments", "customers", "customer_id", "received_by"
		if kind == "suppliers" {
			table, partyTable, partyColumn, actorColumn = "supplier_payments", "suppliers", "supplier_id", "paid_by"
		}
		q := `SELECT p.id,c.name,p.amount,p.method,COALESCE(a.name,''),p.payment_date,COALESCE(p.reference,''),COALESCE(p.notes,''),p.status,COALESCE(u.name,'System'),COALESCE(p.reversal_reason,''),COALESCE(p.cheque_number,''),p.cheque_date,p.clearance_status FROM ` + table + ` p JOIN ` + partyTable + ` c ON c.id=p.` + partyColumn + ` LEFT JOIN cash_accounts a ON a.id=p.account_id LEFT JOIN users u ON u.id=p.` + actorColumn + ` WHERE p.tenant_id=$1 ORDER BY p.payment_date DESC,p.paid_at DESC LIMIT $2`
		if kind == "customers" {
			q = strings.Replace(q, "p.paid_at", "p.received_at", 1)
		}
		rows, err := db.Query(r.Context(), q, claimsFrom(r).Tenant, queryLimit(r.URL.Query().Get("limit"), 100))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, party, method, account, ref, notes, status, user, reason, chequeNumber, clearanceStatus string
			var amount float64
			var date, chequeDate any
			if rows.Scan(&id, &party, &amount, &method, &account, &date, &ref, &notes, &status, &user, &reason, &chequeNumber, &chequeDate, &clearanceStatus) != nil {
				continue
			}
			out = append(out, map[string]any{"id": id, "party": party, "amount": amount, "method": method, "account": account, "date": date, "reference": ref, "notes": notes, "status": status, "user": user, "reversalReason": reason, "chequeNumber": chequeNumber, "chequeDate": chequeDate, "clearanceStatus": clearanceStatus})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func expenseCategories(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(r.Context(), `SELECT c.id,c.name,c.is_active,a.id,a.code,a.name FROM expense_categories c JOIN chart_of_accounts a ON a.id=c.ledger_account_id WHERE c.tenant_id=$1 ORDER BY c.is_active DESC,c.name`, claimsFrom(r).Tenant)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, aid, code, aname string
			var active bool
			rows.Scan(&id, &name, &active, &aid, &code, &aname)
			out = append(out, map[string]any{"id": id, "name": name, "active": active, "accountId": aid, "accountCode": code, "accountName": aname})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func createExpenseCategory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" {
			http.Error(w, "category name is required", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var aid, id string
		err = tx.QueryRow(r.Context(), `INSERT INTO chart_of_accounts(tenant_id,code,name,account_type,normal_balance) VALUES($1,'EX-'||upper(substr(replace(gen_random_uuid()::text,'-',''),1,8)),$2||' Expense','expense','debit') RETURNING id`, c.Tenant, strings.TrimSpace(x.Name)).Scan(&aid)
		if err == nil {
			err = tx.QueryRow(r.Context(), `INSERT INTO expense_categories(tenant_id,name,ledger_account_id) VALUES($1,$2,$3) RETURNING id`, c.Tenant, strings.TrimSpace(x.Name), aid).Scan(&id)
		}
		if err != nil {
			http.Error(w, "category already exists", 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}

func updateExpenseCategory(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){var x struct{Name string `json:"name"`};if json.NewDecoder(r.Body).Decode(&x)!=nil||strings.TrimSpace(x.Name)==""{http.Error(w,"category name is required",400);return};c:=claimsFrom(r);tx,err:=db.Begin(r.Context());if err!=nil{http.Error(w,err.Error(),500);return};defer tx.Rollback(r.Context());var account string;err=tx.QueryRow(r.Context(),`UPDATE expense_categories SET name=$3 WHERE id=$1 AND tenant_id=$2 RETURNING ledger_account_id`,r.PathValue("id"),c.Tenant,strings.TrimSpace(x.Name)).Scan(&account);if err==nil{_,err=tx.Exec(r.Context(),`UPDATE chart_of_accounts SET name=$2,updated_at=now() WHERE id=$1 AND tenant_id=$3`,account,strings.TrimSpace(x.Name),c.Tenant)};if err!=nil||tx.Commit(r.Context())!=nil{http.Error(w,"category could not be updated",409);return};auditUserAction(r,db,"EXPENSE_CATEGORY_UPDATED",r.PathValue("id"),x);w.WriteHeader(204)} }
func expenseCategoryStatus(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){var x struct{Active bool `json:"active"`};if json.NewDecoder(r.Body).Decode(&x)!=nil{http.Error(w,"active status is required",400);return};tag,err:=db.Exec(r.Context(),`UPDATE expense_categories SET is_active=$3 WHERE id=$1 AND tenant_id=$2`,r.PathValue("id"),claimsFrom(r).Tenant,x.Active);if err!=nil||tag.RowsAffected()!=1{http.Error(w,"category not found",404);return};auditUserAction(r,db,"EXPENSE_CATEGORY_STATUS_CHANGED",r.PathValue("id"),x);w.WriteHeader(204)} }

type expenseInput struct {
	CategoryID    string  `json:"categoryId"`
	AccountID     string  `json:"accountId"`
	Description   string  `json:"description"`
	Amount        float64 `json:"amount"`
	Date          string  `json:"date"`
	PaymentMethod string  `json:"paymentMethod"`
	Reference     string  `json:"reference"`
	Notes         string  `json:"notes"`
	RequestID     string  `json:"requestId"`
}

func listExpenses(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rows, err := db.Query(r.Context(), `SELECT e.id,COALESCE(c.name,'Uncategorised'),e.description,e.amount,e.expense_date,COALESCE(a.name,''),COALESCE(e.payment_method,''),e.status,COALESCE(e.reference,''),COALESCE(e.reversal_reason,'') FROM expenses e LEFT JOIN expense_categories c ON c.id=e.category_id LEFT JOIN cash_accounts a ON a.id=e.account_id WHERE e.tenant_id=$1 AND ($2='' OR e.status=$2) AND e.expense_date>=COALESCE(NULLIF($3,'')::date,'2000-01-01') AND e.expense_date<=COALESCE(NULLIF($4,'')::date,current_date) ORDER BY e.expense_date DESC,e.created_at DESC LIMIT $5`, claimsFrom(r).Tenant, q.Get("status"), q.Get("from"), q.Get("to"), queryLimit(q.Get("limit"), 100))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, cat, desc, account, method, status, ref, reason string
			var amount float64
			var date any
			rows.Scan(&id, &cat, &desc, &amount, &date, &account, &method, &status, &ref, &reason)
			out = append(out, map[string]any{"id": id, "category": cat, "description": desc, "amount": amount, "date": date, "account": account, "method": method, "status": status, "reference": ref, "reversalReason": reason})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func createExpense(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x expenseInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.CategoryID == "" || strings.TrimSpace(x.Description) == "" || x.Amount <= 0 || strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "category, description, positive amount and request ID are required", 400)
			return
		}
		c := claimsFrom(r)
		var id string
		err := db.QueryRow(r.Context(), `INSERT INTO expenses(tenant_id,category_id,account_id,description,amount,expense_date,payment_method,reference,notes,status,client_request_id,created_by) SELECT $1,ec.id,NULLIF($3,'')::uuid,$4,$5,COALESCE(NULLIF($6,'')::date,current_date),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),'draft',$10,$11 FROM expense_categories ec WHERE ec.id=$2 AND ec.tenant_id=$1 AND ec.is_active RETURNING id`, c.Tenant, x.CategoryID, x.AccountID, strings.TrimSpace(x.Description), x.Amount, x.Date, x.PaymentMethod, x.Reference, x.Notes, x.RequestID, c.Sub).Scan(&id)
		if err != nil {
			http.Error(w, "expense category is invalid or request was already saved", 409)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "draft"})
	}
}
func updateExpense(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x expenseInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.CategoryID == "" || strings.TrimSpace(x.Description) == "" || x.Amount <= 0 {
			http.Error(w, "valid category, description and amount are required", 400)
			return
		}
		tag, err := db.Exec(r.Context(), `UPDATE expenses e SET category_id=ec.id,account_id=NULLIF($3,'')::uuid,description=$4,amount=$5,expense_date=COALESCE(NULLIF($6,'')::date,current_date),payment_method=NULLIF($7,''),reference=NULLIF($8,''),notes=NULLIF($9,''),updated_at=now() FROM expense_categories ec WHERE e.id=$1 AND e.tenant_id=$2 AND e.status='draft' AND ec.id=$10 AND ec.tenant_id=$2 AND ec.is_active`, r.PathValue("id"), claimsFrom(r).Tenant, x.AccountID, strings.TrimSpace(x.Description), x.Amount, x.Date, x.PaymentMethod, x.Reference, x.Notes, x.CategoryID)
		if err != nil || tag.RowsAffected() == 0 {
			http.Error(w, "draft expense not found or category is invalid", 409)
			return
		}
		w.WriteHeader(204)
	}
}

func expenseAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		action := r.PathValue("action")
		if action != "finalize" && action != "cancel" && action != "reverse" {
			http.Error(w, "unknown expense action", 404)
			return
		}
		var x struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&x)
		if (action == "cancel" || action == "reverse") && strings.TrimSpace(x.Reason) == "" {
			http.Error(w, "reason is required", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var status, desc, account, ledger string
		var date any
		var amount, available float64
		err = tx.QueryRow(r.Context(), `SELECT e.status,e.description,e.amount,e.expense_date,e.account_id::text,ec.ledger_account_id FROM expenses e JOIN expense_categories ec ON ec.id=e.category_id WHERE e.id=$1 AND e.tenant_id=$2 FOR UPDATE`, r.PathValue("id"), c.Tenant).Scan(&status, &desc, &amount, &date, &account, &ledger)
		if err != nil {
			http.Error(w, "expense not found", 404)
			return
		}
		if action == "cancel" {
			if status != "draft" {
				http.Error(w, "only a draft expense can be cancelled", 409)
				return
			}
			_, err = tx.Exec(r.Context(), `UPDATE expenses SET status='cancelled',reversal_reason=$2,updated_at=now() WHERE id=$1`, r.PathValue("id"), strings.TrimSpace(x.Reason))
		} else if action == "finalize" {
			if status != "draft" || account == "" {
				http.Error(w, "draft expense and payment account are required", 409)
				return
			}
			var cashLedger string
			err = tx.QueryRow(r.Context(), `SELECT ledger_account_id,opening_balance+COALESCE(sum(CASE WHEN t.transaction_type IN('deposit','income','transfer_in') THEN t.amount ELSE -t.amount END),0) FROM cash_accounts a LEFT JOIN cash_transactions t ON t.account_id=a.id AND t.status='finalized' WHERE a.id=$1 AND a.tenant_id=$2 AND a.is_active GROUP BY a.id`, account, c.Tenant).Scan(&cashLedger, &available)
			if err != nil || available < amount {
				http.Error(w, "payment account balance is insufficient", 409)
				return
			}
			var jid string
			err = tx.QueryRow(r.Context(), `INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,source) VALUES($1,$2,'expense',$3,$4,$5,'posted',now(),'expense') RETURNING id`, c.Tenant, date, r.PathValue("id"), desc, c.Sub).Scan(&jid)
			if err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) VALUES($1,$2,$3,0,$4),($1,$5,0,$3,$4)`, jid, ledger, amount, desc, cashLedger)
			}
			if err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,transaction_date,created_by,tenant_id,reference_type,reference_id,journal_entry_id) VALUES($1,'expense',$2,$3::text,$4,$5,$6,$7,'expense',$3::uuid,$8)`, account, amount, r.PathValue("id"), desc, date, c.Sub, c.Tenant, jid)
			}
			if err == nil {
				_, err = tx.Exec(r.Context(), `UPDATE expenses SET status='finalized',finalized_by=$2,finalized_at=now(),updated_at=now() WHERE id=$1`, r.PathValue("id"), c.Sub)
			}
		} else {
			if status != "finalized" {
				http.Error(w, "only a finalized expense can be reversed", 409)
				return
			}
			var original, cashLedger, reversal string
			err = tx.QueryRow(r.Context(), `SELECT id FROM journal_entries WHERE tenant_id=$1 AND reference_type='expense' AND reference_id=$2 AND status='posted' FOR UPDATE`, c.Tenant, r.PathValue("id")).Scan(&original)
			if err == nil {
				err = tx.QueryRow(r.Context(), `SELECT ledger_account_id FROM cash_accounts WHERE id=$1 AND tenant_id=$2`, account, c.Tenant).Scan(&cashLedger)
			}
			if err == nil {
				err = tx.QueryRow(r.Context(), `INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,reversed_entry_id,reversal_reason,source) VALUES($1,current_date,'expense_reversal',$2,$3,$4,'posted',now(),$5,$6,'expense') RETURNING id`, c.Tenant, r.PathValue("id"), "Expense reversal: "+desc, c.Sub, original, strings.TrimSpace(x.Reason)).Scan(&reversal)
			}
			if err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) SELECT $1,account_id,credit,debit,$2 FROM journal_lines WHERE journal_id=$3`, reversal, strings.TrimSpace(x.Reason), original)
			}
			if err == nil {
				_, err = tx.Exec(r.Context(), `UPDATE journal_entries SET status='reversed',reversed_by=$2,reversal_reason=$3 WHERE id=$1`, original, c.Sub, strings.TrimSpace(x.Reason))
			}
			if err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,created_by,tenant_id,reference_type,reference_id,journal_entry_id) VALUES($1,'income',$2,$3::text,$4,$5,$6,'expense_reversal',$3::uuid,$7)`, account, amount, r.PathValue("id"), "Expense reversal: "+x.Reason, c.Sub, c.Tenant, reversal)
			}
			if err == nil {
				_, err = tx.Exec(r.Context(), `UPDATE expenses SET status='reversed',reversed_by=$2,reversed_at=now(),reversal_reason=$3,updated_at=now() WHERE id=$1`, r.PathValue("id"), c.Sub, strings.TrimSpace(x.Reason))
			}
		}
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		auditUserAction(r, db, "EXPENSE_"+strings.ToUpper(action), r.PathValue("id"), map[string]any{"reason": x.Reason})
		w.WriteHeader(204)
	}
}

func accountingAccounts(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// depth and sort_order come from the tree so the client can render the
		// chart as a hierarchy instead of a code-sorted flat list, which put
		// "11 Current Assets" between "1000 Cash" and "1010 Bank".
		rows, err := db.Query(r.Context(), `
			WITH RECURSIVE tree AS (
			  SELECT id, 0 AS depth FROM chart_of_accounts WHERE tenant_id=$1 AND parent_id IS NULL
			  UNION ALL
			  SELECT c.id, t.depth+1 FROM chart_of_accounts c JOIN tree t ON c.parent_id=t.id
			)
			SELECT a.id,a.code,a.name,a.account_type,a.normal_balance,a.is_active,a.is_system,
			       a.allow_manual_entries,COALESCE(a.description,''),COALESCE(p.name,''),
			       COALESCE(a.parent_id::text,''),a.is_group,a.is_contra,
			       COALESCE(a.control_type,''),a.sort_order,COALESCE(t.depth,0),
			       COALESCE(ar.role,'')
			  FROM chart_of_accounts a
			  LEFT JOIN chart_of_accounts p ON p.id=a.parent_id
			  LEFT JOIN tree t ON t.id=a.id
			  LEFT JOIN account_roles ar ON ar.account_id=a.id AND ar.tenant_id=a.tenant_id
			 WHERE a.tenant_id=$1
			 ORDER BY a.sort_order, a.code`, claimsFrom(r).Tenant)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, code, name, typ, normal, description, parent, parentID, control, role string
			var active, system, manual, group, contra bool
			var sortOrder, depth int
			if err := rows.Scan(&id, &code, &name, &typ, &normal, &active, &system, &manual,
				&description, &parent, &parentID, &group, &contra, &control, &sortOrder, &depth, &role); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			out = append(out, map[string]any{
				"id": id, "code": code, "name": name, "type": typ, "normalBalance": normal,
				"active": active, "system": system, "allowManual": manual,
				"description": description, "parent": parent, "parentId": parentID,
				"isGroup": group, "isContra": contra, "controlType": control,
				"sortOrder": sortOrder, "depth": depth, "role": role,
			})
		}
		json.NewEncoder(w).Encode(out)
	}
}

type accountInput struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	ParentID    string `json:"parentId"`
	Description string `json:"description"`
	AllowManual bool   `json:"allowManual"`
}

func createAccountingAccount(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x accountInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Code) == "" || strings.TrimSpace(x.Name) == "" || !validAccountType(x.Type) {
			http.Error(w, "code, name and valid account type are required", 400)
			return
		}
		var id string
		err := db.QueryRow(r.Context(), `INSERT INTO chart_of_accounts(tenant_id,code,name,account_type,parent_id,description,allow_manual_entries,normal_balance) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,''),$7,CASE WHEN $4 IN('asset','expense') THEN 'debit' ELSE 'credit' END) RETURNING id`, claimsFrom(r).Tenant, strings.TrimSpace(x.Code), strings.TrimSpace(x.Name), x.Type, x.ParentID, strings.TrimSpace(x.Description), x.AllowManual).Scan(&id)
		if err != nil {
			http.Error(w, "account code already exists or parent is invalid", 409)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func updateAccountingAccount(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x accountInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" {
			http.Error(w, "account name is required", 400)
			return
		}
		tag, err := db.Exec(r.Context(), `UPDATE chart_of_accounts SET name=$3,parent_id=NULLIF($4,'')::uuid,description=NULLIF($5,''),allow_manual_entries=CASE WHEN is_system THEN false ELSE $6 END,updated_at=now() WHERE id=$1 AND tenant_id=$2`, r.PathValue("id"), claimsFrom(r).Tenant, strings.TrimSpace(x.Name), x.ParentID, strings.TrimSpace(x.Description), x.AllowManual)
		if err != nil || tag.RowsAffected() == 0 {
			http.Error(w, "account not found or parent is invalid", 409)
			return
		}
		w.WriteHeader(204)
	}
}
func accountingAccountStatus(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Active bool `json:"active"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil {
			http.Error(w, "active status is required", 400)
			return
		}
		tag, err := db.Exec(r.Context(), `UPDATE chart_of_accounts SET is_active=$3,updated_at=now() WHERE id=$1 AND tenant_id=$2 AND NOT is_system AND ($3 OR NOT EXISTS(SELECT 1 FROM journal_lines WHERE account_id=$1))`, r.PathValue("id"), claimsFrom(r).Tenant, x.Active)
		if err != nil || tag.RowsAffected() == 0 {
			http.Error(w, "system or used account cannot be deactivated", 409)
			return
		}
		w.WriteHeader(204)
	}
}
func validAccountType(v string) bool {
	return v == "asset" || v == "liability" || v == "equity" || v == "income" || v == "expense"
}

type journalLineInput struct {
	AccountID string  `json:"accountId"`
	Debit     float64 `json:"debit"`
	Credit    float64 `json:"credit"`
	Memo      string  `json:"memo"`
}
type journalInput struct {
	Date        string             `json:"date"`
	Description string             `json:"description"`
	Reference   string             `json:"reference"`
	RequestID   string             `json:"requestId"`
	Lines       []journalLineInput `json:"lines"`
}

func createJournal(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x journalInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Description) == "" || strings.TrimSpace(x.RequestID) == "" || len(x.Lines) < 2 {
			http.Error(w, "description, request ID and at least two lines are required", 400)
			return
		}
		debit, credit := 0.0, 0.0
		for _, l := range x.Lines {
			if l.AccountID == "" || l.Debit < 0 || l.Credit < 0 || (l.Debit > 0) == (l.Credit > 0) {
				http.Error(w, "each line must contain one positive debit or credit", 400)
				return
			}
			debit += l.Debit
			credit += l.Credit
		}
		if fmt.Sprintf("%.2f", debit) != fmt.Sprintf("%.2f", credit) || debit <= 0 {
			http.Error(w, "journal debits and credits must balance", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var id, no string
		err = tx.QueryRow(r.Context(), `INSERT INTO journal_entries(tenant_id,entry_date,reference_type,description,created_by,status,source,client_request_id) VALUES($1,COALESCE(NULLIF($2,'')::date,current_date),'manual',concat_ws(' / ',$3::text,NULLIF($4::text,'')),$5,'draft','manual',$6) RETURNING id,entry_no`, c.Tenant, x.Date, strings.TrimSpace(x.Description), strings.TrimSpace(x.Reference), c.Sub, x.RequestID).Scan(&id, &no)
		for _, l := range x.Lines {
			if err == nil {
				var tag pgconn.CommandTag
				tag, err = tx.Exec(r.Context(), `INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) SELECT $1,a.id,$3,$4,NULLIF($5,'') FROM chart_of_accounts a WHERE a.id=$2 AND a.tenant_id=$6 AND a.is_active AND a.allow_manual_entries`, id, l.AccountID, l.Debit, l.Credit, strings.TrimSpace(l.Memo), c.Tenant)
				if err == nil && tag.RowsAffected() != 1 {
					err = fmt.Errorf("account %s is inactive or does not allow manual entries", l.AccountID)
				}
			}
		}
		if err != nil {
			http.Error(w, "journal could not be saved: "+err.Error(), 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "entryNo": no, "status": "draft"})
	}
}

func listJournals(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rows, err := db.Query(r.Context(), `SELECT je.id,je.entry_no,je.entry_date,COALESCE(je.description,''),COALESCE(je.reference_type,''),je.status,COALESCE(u.name,'System'),COALESCE(sum(jl.debit),0),COALESCE(je.reversal_reason,'') FROM journal_entries je LEFT JOIN users u ON u.id=je.created_by LEFT JOIN journal_lines jl ON jl.journal_id=je.id WHERE je.tenant_id=$1 AND ($2='' OR je.status=$2) AND je.entry_date>=COALESCE(NULLIF($3,'')::date,'2000-01-01') AND je.entry_date<=COALESCE(NULLIF($4,'')::date,current_date) GROUP BY je.id,u.name ORDER BY je.entry_date DESC,je.created_at DESC LIMIT $5`, claimsFrom(r).Tenant, q.Get("status"), q.Get("from"), q.Get("to"), queryLimit(q.Get("limit"), 100))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, no, desc, ref, status, user, reason string
			var date any
			var total float64
			rows.Scan(&id, &no, &date, &desc, &ref, &status, &user, &total, &reason)
			lineRows, e := db.Query(r.Context(), `SELECT a.id,a.code,a.name,jl.debit,jl.credit,COALESCE(jl.memo,'') FROM journal_lines jl JOIN chart_of_accounts a ON a.id=jl.account_id WHERE jl.journal_id=$1 ORDER BY jl.id`, id)
			lines := []map[string]any{}
			if e == nil {
				for lineRows.Next() {
					var accountID, code, name, memo string
					var debit, credit float64
					lineRows.Scan(&accountID, &code, &name, &debit, &credit, &memo)
					lines = append(lines, map[string]any{"accountId": accountID, "code": code, "account": name, "debit": debit, "credit": credit, "memo": memo})
				}
				lineRows.Close()
			}
			out = append(out, map[string]any{"id": id, "entryNo": no, "date": date, "description": desc, "referenceType": ref, "status": status, "user": user, "total": total, "reversalReason": reason, "lines": lines})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func journalAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		action := r.PathValue("action")
		if action != "post" && action != "reverse" {
			http.Error(w, "unknown journal action", 404)
			return
		}
		var x struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&x)
		c := claimsFrom(r)
		if action == "post" {
			tag, err := db.Exec(r.Context(), `UPDATE journal_entries SET status='posted',posted_by=$3 WHERE id=$1 AND tenant_id=$2 AND status='draft'`, r.PathValue("id"), c.Tenant, c.Sub)
			if err != nil || tag.RowsAffected() == 0 {
				http.Error(w, "draft journal is invalid, unbalanced, or period is locked", 409)
				return
			}
			w.WriteHeader(204)
			return
		}
		if strings.TrimSpace(x.Reason) == "" {
			http.Error(w, "reversal reason is required", 400)
			return
		}
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var original, reference string
		err = tx.QueryRow(r.Context(), `SELECT id,COALESCE(description,'') FROM journal_entries WHERE id=$1 AND tenant_id=$2 AND status='posted' FOR UPDATE`, r.PathValue("id"), c.Tenant).Scan(&original, &reference)
		var reversal string
		if err == nil {
			err = tx.QueryRow(r.Context(), `INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,reversed_entry_id,reversal_reason,source) VALUES($1,current_date,'manual_reversal',$2,$3,$4,'posted',now(),$2,$5,'manual') RETURNING id`, c.Tenant, original, "Reversal: "+reference, c.Sub, strings.TrimSpace(x.Reason)).Scan(&reversal)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) SELECT $1,account_id,credit,debit,$2 FROM journal_lines WHERE journal_id=$3`, reversal, strings.TrimSpace(x.Reason), original)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE journal_entries SET status='reversed',reversed_by=$2,reversal_reason=$3 WHERE id=$1`, original, c.Sub, strings.TrimSpace(x.Reason))
		}
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	}
}

func queryLimit(raw string, fallback int) int {
	n, _ := strconv.Atoi(raw)
	if n < 1 {
		return fallback
	}
	if n > 500 {
		return 500
	}
	return n
}
