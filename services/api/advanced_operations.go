package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

func registerAdvancedOperations(m *http.ServeMux, db *pgxpool.Pool) {
	registerStockApprovals(m, db)
	m.HandleFunc("GET /stock/batches", jsonAPI(authenticated(db, allStaff...)(batches(db))))
	m.HandleFunc("GET /stock/expiry-alerts", jsonAPI(authenticated(db, allStaff...)(expiryAlerts(db))))
	m.HandleFunc("GET /expiry/batches", jsonAPI(authenticated(db, stockRoles...)(expiryBatches(db))))
	m.HandleFunc("PUT /expiry/batches/{id}", jsonAPI(authenticated(db, stockRoles...)(updateBatch(db))))
	m.HandleFunc("POST /expiry/batches/{id}/remove", jsonAPI(authenticated(db, stockRoles...)(secureExpireBatch(db))))
	m.HandleFunc("GET /expiry/history", jsonAPI(authenticated(db, stockRoles...)(expiryHistory(db))))
	m.HandleFunc("POST /stock/adjustments", jsonAPI(authenticated(db, stockRoles...)(secureAdjustStock(db))))
	m.HandleFunc("GET /stock/adjustments", jsonAPI(authenticated(db, stockRoles...)(stockAdjustments(db))))
	m.HandleFunc("POST /stock/transfers", jsonAPI(authenticated(db, stockRoles...)(transferStock(db))))
	m.HandleFunc("GET /cashier-sessions", jsonAPI(authenticated(db, salesRoles...)(sessions(db))))
	m.HandleFunc("GET /cashier-sessions/current", jsonAPI(authenticated(db, salesRoles...)(currentSession(db))))
	m.HandleFunc("POST /cashier-sessions/open", jsonAPI(authenticated(db, salesRoles...)(openSession(db))))
	m.HandleFunc("POST /cashier-sessions/{id}/close", jsonAPI(authenticated(db, salesRoles...)(closeSession(db))))
	m.HandleFunc("POST /cashier-sessions/{id}/reconcile", jsonAPI(authenticated(db, salesRoles...)(reconcileCashierSession(db))))
	m.HandleFunc("GET /pos/discount-policies", jsonAPI(authenticated(db, salesRoles...)(discountPolicies(db))))
	m.HandleFunc("PUT /pos/discount-policies", jsonAPI(authenticated(db, salesRoles...)(updateDiscountPolicy(db))))
	m.HandleFunc("GET /cash-accounts", jsonAPI(authenticated(db, financeRoles...)(cashAccounts(db))))
	m.HandleFunc("POST /cash-accounts", jsonAPI(authenticated(db, financeRoles...)(createCashAccount(db))))
	m.HandleFunc("PUT /cash-accounts/{id}", jsonAPI(authenticated(db, financeRoles...)(updateCashAccount(db))))
	m.HandleFunc("POST /cash-accounts/{id}/status", jsonAPI(authenticated(db, financeRoles...)(cashAccountStatus(db))))
	m.HandleFunc("POST /cash-transactions", jsonAPI(authenticated(db, financeRoles...)(cashTransaction(db))))
	m.HandleFunc("GET /reports/accounting", jsonAPI(authenticated(db, financeRoles...)(accountingReport(db))))
}

func stockAdjustments(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(r.Context(), `SELECT a.id,p.name,p.sku,a.system_quantity,a.physical_quantity,a.quantity,a.reason,COALESCE(a.notes,''),a.status,a.created_at,COALESCE(u.name,'System') FROM stock_adjustments a JOIN products p ON p.id=a.product_id LEFT JOIN users u ON u.id=a.adjusted_by WHERE a.tenant_id=$1 ORDER BY a.created_at DESC LIMIT 100`, claimsFrom(r).Tenant)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, product, sku, reason, notes, status, user string
			var system, physical, quantity float64
			var created any
			if rows.Scan(&id, &product, &sku, &system, &physical, &quantity, &reason, &notes, &status, &created, &user) != nil {
				continue
			}
			out = append(out, map[string]any{"id": id, "product": product, "sku": sku, "systemQuantity": system, "physicalQuantity": physical, "quantity": quantity, "reason": reason, "notes": notes, "status": status, "createdAt": created, "user": user})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func batches(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT b.id,p.id,p.name,COALESCE(b.batch_no,''),b.expiry_date,b.available_qty,b.unit_cost,COALESCE(b.branch_id::text,'') FROM stock_batches b JOIN products p ON p.id=b.product_id WHERE b.available_qty>0 AND p.tenant_id=$1 ORDER BY b.expiry_date NULLS LAST,b.received_at`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, productID, name, no, branchID string
			var expiry any
			var qty, cost float64
			rows.Scan(&id,&productID, &name, &no, &expiry, &qty, &cost,&branchID)
			out = append(out, map[string]any{"id": id,"productId":productID, "product": name, "batchNo": no, "expiry": expiry, "available": qty, "unitCost": cost,"branchId":branchID})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func expiryAlerts(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT p.name,COALESCE(b.batch_no,''),b.expiry_date,b.available_qty FROM stock_batches b JOIN products p ON p.id=b.product_id WHERE b.available_qty>0 AND b.expiry_date IS NOT NULL AND b.expiry_date<=current_date+30 AND p.tenant_id=$1 ORDER BY b.expiry_date`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var product, no string
			var expiry any
			var qty float64
			rows.Scan(&product, &no, &expiry, &qty)
			out = append(out, map[string]any{"product": product, "batchNo": no, "expiry": expiry, "available": qty})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func adjustStock(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			ProductID string  `json:"productId"`
			Quantity  float64 `json:"quantity"`
			Reason    string  `json:"reason"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.ProductID == "" || x.Quantity == 0 || x.Reason == "" {
			http.Error(w, "product, non-zero quantity and reason required", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		tag, e := tx.Exec(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2 WHERE id=$1 AND tenant_id=$3 AND stock_quantity+$2>=0`, x.ProductID, x.Quantity, c.Tenant)
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "insufficient stock or invalid product", 409)
			return
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO stock_adjustments(product_id,quantity,reason,adjusted_by)VALUES($1,$2,$3,NULLIF($4,'')::uuid)`, x.ProductID, x.Quantity, x.Reason, c.Sub)
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity)VALUES($1,'adjustment',$2)`, x.ProductID, x.Quantity)
		}
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		tx.Commit(r.Context())
		w.WriteHeader(201)
	}
}
func transferStock(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			ProductID, FromBranchID, ToBranchID, Reference string
			Quantity                                       float64
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.ProductID == "" || x.Quantity <= 0 || x.FromBranchID == "" || x.ToBranchID == "" || x.FromBranchID == x.ToBranchID {
			http.Error(w, "valid branches, product and quantity required", 400)
			return
		}
		_, e := db.Exec(r.Context(), `INSERT INTO stock_transfers(from_branch_id,to_branch_id,product_id,quantity,reference,transferred_by)VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid)`, x.FromBranchID, x.ToBranchID, x.ProductID, x.Quantity, x.Reference, claimsFrom(r).Sub)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		w.WriteHeader(201)
	}
}

type cashierSessionSummary struct {
	ID          string  `json:"id"`
	UserID      string  `json:"userId"`
	Cashier     string  `json:"cashier"`
	Branch      string  `json:"branch"`
	Opening     float64 `json:"opening"`
	CashSales   float64 `json:"cashSales"`
	CashReturns float64 `json:"cashReturns"`
	Expected    float64 `json:"expected"`
	Closing     any     `json:"closing"`
	Variance    any     `json:"variance"`
	OpenedAt    any     `json:"openedAt"`
	ClosedAt    any     `json:"closedAt"`
	Status      string  `json:"status"`
	ReconciliationNote string `json:"reconciliationNote"`
	VarianceApproved bool `json:"varianceApproved"`
}

const sessionSummarySQL = `SELECT cs.id,cs.user_id,u.name,COALESCE(b.name,''),cs.opening_cash,
 COALESCE(sum(CASE WHEN e.event_type='sale_cash' THEN e.amount ELSE 0 END),0),
 COALESCE(sum(CASE WHEN e.event_type IN('sale_return_cash','sale_cancel_cash') THEN -e.amount ELSE 0 END),0),
 COALESCE(sum(e.amount),0),
 cs.closing_cash,cs.expected_cash,cs.variance,cs.opened_at,cs.closed_at,cs.reconciled_at,COALESCE(cs.reconciliation_note,''),cs.reconciliation_variance_approved
 FROM cashier_sessions cs
 JOIN users u ON u.id=cs.user_id
 LEFT JOIN branches b ON b.id=cs.branch_id
 LEFT JOIN cashier_session_events e ON e.session_id=cs.id
 WHERE cs.tenant_id=$1`

func scanSession(row pgx.Row) (cashierSessionSummary, error) {
	var result cashierSessionSummary
	var storedExpected, reconciled any
	var netMovement float64
	err := row.Scan(&result.ID, &result.UserID, &result.Cashier, &result.Branch, &result.Opening, &result.CashSales, &result.CashReturns, &netMovement, &result.Closing, &storedExpected, &result.Variance, &result.OpenedAt, &result.ClosedAt, &reconciled,&result.ReconciliationNote,&result.VarianceApproved)
	result.Expected = result.Opening + netMovement
	result.Status = "open"
	if result.ClosedAt != nil {
		result.Status = "closed"
	}
	if reconciled != nil {
		result.Status = "reconciled"
	}
	return result, err
}

func sessions(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		query:=sessionSummarySQL;args:=[]any{c.Tenant};if !canManageCashierSession(c.Role){query+=` AND cs.user_id=$2`;args=append(args,c.Sub)};query+=` GROUP BY cs.id,u.name,b.name ORDER BY cs.opened_at DESC LIMIT 100`
		rows, err := db.Query(r.Context(), query,args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		out := []cashierSessionSummary{}
		for rows.Next() {
			entry, scanErr := scanSession(rows)
			if scanErr != nil {
				http.Error(w, scanErr.Error(), http.StatusInternalServerError)
				return
			}
			out = append(out, entry)
		}
		if rows.Err() != nil {
			http.Error(w, rows.Err().Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(out)
	}
}

func currentSession(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		row := db.QueryRow(r.Context(), sessionSummarySQL+` AND cs.user_id=$2 AND cs.branch_id=NULLIF($3,'')::uuid AND cs.closed_at IS NULL GROUP BY cs.id,u.name,b.name ORDER BY cs.opened_at DESC LIMIT 1`, c.Tenant, c.Sub, c.Branch)
		entry, err := scanSession(row)
		if err == pgx.ErrNoRows {
			json.NewEncoder(w).Encode(map[string]any{"session": nil})
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"session": entry})
	}
}

func canManageCashierSession(role string) bool {
	return role == "super_admin" || role == "admin" || role == "manager"
}

func openSession(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			UserID      string  `json:"userId"`
			OpeningCash float64 `json:"openingCash"`
			Notes       string  `json:"notes"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.OpeningCash < 0 {
			http.Error(w, "a non-negative opening cash amount is required", http.StatusBadRequest)
			return
		}
		c := claimsFrom(r)
		if x.UserID == "" {
			x.UserID = c.Sub
		}
		if x.UserID != c.Sub && !canManageCashierSession(c.Role) {
			http.Error(w, "you can only open your own cashier session", http.StatusForbidden)
			return
		}
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())
		var branchID string
		err = tx.QueryRow(r.Context(), `SELECT branch_id::text FROM users WHERE id=$1 AND tenant_id=$2 AND is_active AND deleted_at IS NULL FOR UPDATE`, x.UserID, c.Tenant).Scan(&branchID)
		if err != nil {
			http.Error(w, "active cashier not found in this workspace", http.StatusNotFound)
			return
		}
		if !canManageCashierSession(c.Role) && branchID != c.Branch {
			http.Error(w, "cashier branch does not match the signed-in branch", http.StatusForbidden)
			return
		}
		var id string
		err = tx.QueryRow(r.Context(), `INSERT INTO cashier_sessions(user_id,tenant_id,branch_id,opening_cash,notes,opened_by) VALUES($1,$2,$3,$4,NULLIF($5,''),$6) RETURNING id`, x.UserID, c.Tenant, branchID, x.OpeningCash, x.Notes, c.Sub).Scan(&id)
		if err != nil {
			http.Error(w, "cashier already has an open session", http.StatusConflict)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		auditUserAction(r, db, "CASHIER_SESSION_OPENED", id, map[string]any{"cashierId": x.UserID, "openingCash": x.OpeningCash})
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}

func closeSession(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			ClosingCash float64 `json:"closingCash"`
			Notes       string  `json:"notes"`
			Denominations map[string]float64 `json:"denominations"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.ClosingCash < 0 {
			http.Error(w, "a non-negative closing cash amount is required", http.StatusBadRequest)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())
		var userID string
		var openingCash float64
		err = tx.QueryRow(r.Context(), `SELECT user_id,opening_cash FROM cashier_sessions WHERE id=$1 AND tenant_id=$2 AND closed_at IS NULL FOR UPDATE`, r.PathValue("id"), c.Tenant).Scan(&userID, &openingCash)
		if err == pgx.ErrNoRows {
			http.Error(w, "open cashier session not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if userID != c.Sub && !canManageCashierSession(c.Role) {
			http.Error(w, "you can only close your own cashier session", http.StatusForbidden)
			return
		}
		var cashSales, cashReturns, netMovement float64
		err = tx.QueryRow(r.Context(), `SELECT
		 COALESCE(sum(CASE WHEN event_type='sale_cash' THEN amount ELSE 0 END),0),
		 COALESCE(sum(CASE WHEN event_type IN('sale_return_cash','sale_cancel_cash') THEN -amount ELSE 0 END),0),
		 COALESCE(sum(amount),0)
		 FROM cashier_session_events WHERE session_id=$1`, r.PathValue("id")).Scan(&cashSales, &cashReturns, &netMovement)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		expected := openingCash + netMovement
		variance := x.ClosingCash - expected
		denominations,_:=json.Marshal(x.Denominations)
		_, err = tx.Exec(r.Context(), `UPDATE cashier_sessions SET closing_cash=$2,expected_cash=$3,variance=$4,close_notes=NULLIF($5,''),closed_by=$6,closed_at=now(),close_denominations=NULLIF($7,'{}')::jsonb WHERE id=$1`, r.PathValue("id"), x.ClosingCash, expected, variance, x.Notes, c.Sub,string(denominations))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		auditUserAction(r, db, "CASHIER_SESSION_CLOSED", r.PathValue("id"), map[string]any{"closingCash": x.ClosingCash, "expectedCash": expected, "variance": variance})
		json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "closing": x.ClosingCash, "expected": expected, "variance": variance, "cashSales": cashSales, "cashReturns": cashReturns})
	}
}
func cashAccounts(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includeInactive := r.URL.Query().Get("includeInactive") == "true"
		rows, e := db.Query(r.Context(), `SELECT a.id,a.name,a.account_type,a.opening_balance+COALESCE(sum(CASE WHEN t.transaction_type IN('deposit','income','transfer_in') THEN t.amount ELSE -t.amount END),0),COALESCE(a.account_number,''),COALESCE(a.description,''),COALESCE(a.ledger_account_id::text,''),COALESCE(c.code,''),a.is_active FROM cash_accounts a LEFT JOIN cash_transactions t ON t.account_id=a.id AND t.status='finalized' LEFT JOIN chart_of_accounts c ON c.id=a.ledger_account_id WHERE ($2 OR a.is_active) AND a.tenant_id=$1 GROUP BY a.id,c.code ORDER BY a.is_active DESC,a.account_type,a.name`, claimsFrom(r).Tenant, includeInactive)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, kind, number, description, ledgerID, ledgerCode string
			var bal float64
			var active bool
			rows.Scan(&id, &name, &kind, &bal, &number, &description, &ledgerID, &ledgerCode, &active)
			out = append(out, map[string]any{"id": id, "name": name, "type": kind, "balance": bal, "accountNumber": number, "description": description, "ledgerAccountId": ledgerID, "ledgerCode": ledgerCode, "active": active})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func updateCashAccount(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct{ Name, AccountNumber, Description string }
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" { http.Error(w, "account name is required", 400); return }
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context()); if err != nil { http.Error(w, err.Error(), 500); return }; defer tx.Rollback(r.Context())
		var ledgerID string
		err = tx.QueryRow(r.Context(), `UPDATE cash_accounts SET name=$3,account_number=NULLIF($4,''),description=NULLIF($5,'') WHERE id=$1 AND tenant_id=$2 RETURNING ledger_account_id`, r.PathValue("id"), c.Tenant, strings.TrimSpace(x.Name), strings.TrimSpace(x.AccountNumber), strings.TrimSpace(x.Description)).Scan(&ledgerID)
		if err == nil { _, err = tx.Exec(r.Context(), `UPDATE chart_of_accounts SET name=$2,description=NULLIF($3,''),updated_at=now() WHERE id=$1 AND tenant_id=$4`, ledgerID, strings.TrimSpace(x.Name), strings.TrimSpace(x.Description), c.Tenant) }
		if err != nil || tx.Commit(r.Context()) != nil { http.Error(w, "account could not be updated", 409); return }
		auditUserAction(r, db, "CASH_ACCOUNT_UPDATED", r.PathValue("id"), map[string]any{"name": x.Name}); w.WriteHeader(204)
	}
}
func cashAccountStatus(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct{ Active bool `json:"active"` }; if json.NewDecoder(r.Body).Decode(&x) != nil { http.Error(w, "active status is required", 400); return }
		c := claimsFrom(r); tx, err := db.Begin(r.Context()); if err != nil { http.Error(w, err.Error(), 500); return }; defer tx.Rollback(r.Context())
		var ledgerID string
		err = tx.QueryRow(r.Context(), `UPDATE cash_accounts SET is_active=$3 WHERE id=$1 AND tenant_id=$2 RETURNING ledger_account_id`, r.PathValue("id"), c.Tenant, x.Active).Scan(&ledgerID)
		if err == nil { _, err = tx.Exec(r.Context(), `UPDATE chart_of_accounts SET is_active=$2,updated_at=now() WHERE id=$1 AND tenant_id=$3`, ledgerID, x.Active, c.Tenant) }
		if err != nil || tx.Commit(r.Context()) != nil { http.Error(w, "account status could not be changed", 409); return }
		auditUserAction(r, db, "CASH_ACCOUNT_STATUS_CHANGED", r.PathValue("id"), map[string]any{"active": x.Active}); w.WriteHeader(204)
	}
}
func createCashAccount(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Name, Type, AccountNumber, Description string
			OpeningBalance                         float64 `json:"openingBalance"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.Name == "" || (x.Type != "cash" && x.Type != "bank") {
			http.Error(w, "name and cash/bank type required", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var id, ledgerID string
		e = tx.QueryRow(r.Context(), `INSERT INTO chart_of_accounts(tenant_id,code,name,account_type,description,is_system,allow_manual_entries,normal_balance) VALUES($1,'CA-'||upper(substr(replace(gen_random_uuid()::text,'-',''),1,8)),$2,'asset',NULLIF($3,''),true,false,'debit') RETURNING id`, c.Tenant, strings.TrimSpace(x.Name), strings.TrimSpace(x.Description)).Scan(&ledgerID)
		if e == nil {
			e = tx.QueryRow(r.Context(), `INSERT INTO cash_accounts(name,account_type,opening_balance,tenant_id,account_number,description,ledger_account_id)VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7)RETURNING id`, strings.TrimSpace(x.Name), x.Type, x.OpeningBalance, c.Tenant, strings.TrimSpace(x.AccountNumber), strings.TrimSpace(x.Description), ledgerID).Scan(&id)
		}
		if e == nil && x.OpeningBalance != 0 {
			var equityID, journalID string
			e = tx.QueryRow(r.Context(), `SELECT id FROM chart_of_accounts WHERE tenant_id=$1 AND code='3000'`, c.Tenant).Scan(&equityID)
			if e == nil {
				e = tx.QueryRow(r.Context(), `INSERT INTO journal_entries(tenant_id,entry_date,reference_type,reference_id,description,created_by,status,posted_at,source) VALUES($1,current_date,'cash_opening',$2,$3,$4,'posted',now(),'cashbook') RETURNING id`, c.Tenant, id, "Opening balance - "+strings.TrimSpace(x.Name), c.Sub).Scan(&journalID)
			}
			if e == nil {
				_, e = tx.Exec(r.Context(), `INSERT INTO journal_lines(journal_id,account_id,debit,credit) VALUES($1,$2,$3,0),($1,$4,0,$3)`, journalID, ledgerID, x.OpeningBalance, equityID)
			}
		}
		if e != nil {
			http.Error(w, "account name already exists or opening balance is invalid", 409)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func cashTransaction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			AccountID, Type, Reference, Description string
			Amount                                  float64
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.AccountID == "" || x.Amount <= 0 {
			http.Error(w, "account and amount required", 400)
			return
		}
		tag, e := db.Exec(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description) SELECT $1,$2,$3,$4,$5 WHERE EXISTS(SELECT 1 FROM cash_accounts WHERE id=$1 AND tenant_id=$6)`, x.AccountID, x.Type, x.Amount, x.Reference, x.Description, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "account not found", 404)
			return
		}
		w.WriteHeader(201)
	}
}
func accountingReport(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := claimsFrom(r).Tenant
		var cash, bank, receivable, payable, income, expense float64
		db.QueryRow(r.Context(), `SELECT COALESCE((SELECT sum(opening_balance+COALESCE((SELECT sum(CASE WHEN transaction_type IN('deposit','income','transfer_in') THEN amount ELSE -amount END) FROM cash_transactions WHERE account_id=a.id),0)) FROM cash_accounts a WHERE account_type='cash' AND a.tenant_id=$1),0),COALESCE((SELECT sum(opening_balance+COALESCE((SELECT sum(CASE WHEN transaction_type IN('deposit','income','transfer_in') THEN amount ELSE -amount END) FROM cash_transactions WHERE account_id=a.id),0)) FROM cash_accounts a WHERE account_type='bank' AND a.tenant_id=$1),0),COALESCE((SELECT sum(balance) FROM customers WHERE tenant_id=$1),0),COALESCE((SELECT sum(balance) FROM suppliers WHERE tenant_id=$1),0),COALESCE((SELECT sum(total) FROM sales WHERE tenant_id=$1),0),COALESCE((SELECT sum(amount) FROM expenses WHERE tenant_id=$1),0)`, tenant).Scan(&cash, &bank, &receivable, &payable, &income, &expense)
		json.NewEncoder(w).Encode(map[string]any{"cash": cash, "bank": bank, "receivables": receivable, "payables": payable, "salesIncome": income, "expenses": expense, "netIncome": income - expense})
	}
}
