package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerAccountingInsights(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /accounting/trial-balance", jsonAPI(authenticated(db, financeRoles...)(trialBalance(db))))
	m.HandleFunc("GET /accounting/discrepancies", jsonAPI(authenticated(db, financeRoles...)(accountingDiscrepancies(db))))
	m.HandleFunc("GET /accounting/reconciliation-v2", jsonAPI(authenticated(db, financeRoles...)(reconciliationSummary(db))))
	m.HandleFunc("GET /accounting/reconciliation/{code}", jsonAPI(authenticated(db, financeRoles...)(reconciliationDetail(db))))
	m.HandleFunc("GET /accounting/statements/{name}", jsonAPI(authenticated(db, financeRoles...)(fixedGroupedStatement(db))))
}

// trialBalance returns every account with its movement for the period and the
// proof that the ledger balances. The balanced flag is the answer to the
// question the report exists to ask, so the client does not have to add the
// columns up itself to find out.
func trialBalance(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		tenant := claimsFrom(r).Tenant
		rows, err := db.Query(r.Context(), `
			SELECT code, name, account_type, is_group, sort_order, debit, credit, balance
			  FROM accounting_trial_balance(
			         $1,
			         COALESCE(NULLIF($2,'')::date, current_date),
			         NULLIF($3,'')::date,
			         NULLIF($4,'')::uuid)`,
			tenant, q.Get("to"), q.Get("from"), q.Get("branchId"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		accounts := []map[string]any{}
		totalDebit, totalCredit := 0.0, 0.0
		for rows.Next() {
			var code, name, accountType string
			var isGroup bool
			var sortOrder int
			var debit, credit, balance float64
			if err := rows.Scan(&code, &name, &accountType, &isGroup, &sortOrder, &debit, &credit, &balance); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			// headings carry no postings of their own, so counting them would
			// double every subtotal
			if !isGroup {
				totalDebit += debit
				totalCredit += credit
			}
			accounts = append(accounts, map[string]any{
				"code": code, "name": name, "type": accountType, "isGroup": isGroup,
				"sortOrder": sortOrder, "debit": debit, "credit": credit, "balance": balance,
			})
		}
		difference := math.Round((totalDebit-totalCredit)*100) / 100
		json.NewEncoder(w).Encode(map[string]any{
			"accounts":    accounts,
			"totalDebit":  math.Round(totalDebit*100) / 100,
			"totalCredit": math.Round(totalCredit*100) / 100,
			"difference":  difference,
			"balanced":    difference == 0,
		})
	}
}

func reconciliationSummary(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The comparison itself lives in accounting_control_reconciliation, so
		// this endpoint and /accounting/reconciliation cannot drift apart the
		// way their two hand-written copies of the query did.
		rows, err := db.Query(r.Context(),
			`SELECT code, name, control_type, ledger, subledger, difference, balanced, note
			   FROM accounting_control_reconciliation($1, COALESCE(NULLIF($2,'')::date, current_date))`,
			claimsFrom(r).Tenant, r.URL.Query().Get("asOf"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var code, name, control, note string
			var ledger, difference float64
			var subledger *float64
			var balanced bool
			if err := rows.Scan(&code, &name, &control, &ledger, &subledger, &difference, &balanced, &note); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			row := map[string]any{
				"code": code, "name": name, "controlType": control,
				"ledger": ledger, "difference": difference, "balanced": balanced, "note": note,
			}
			// a null subledger means nothing in the system owns this balance,
			// so the client shows a dash rather than a misleading zero
			if subledger != nil {
				row["operational"] = *subledger
				row["subledger"] = *subledger
			}
			out = append(out, row)
		}
		json.NewEncoder(w).Encode(out)
	}
}

func accountingDiscrepancies(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(r.Context(),
			`SELECT kind, reference, detail, amount
			   FROM accounting_discrepancies($1, COALESCE(NULLIF($2,'')::date, current_date))
			  ORDER BY kind, reference`,
			claimsFrom(r).Tenant, r.URL.Query().Get("asOf"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var kind, reference, detail string
			var amount float64
			if err := rows.Scan(&kind, &reference, &detail, &amount); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			out = append(out, map[string]any{
				"kind": kind, "reference": reference, "detail": detail, "amount": amount,
			})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func reconciliationDetail(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){q:=r.URL.Query();rows,err:=db.Query(r.Context(),`SELECT j.id,j.entry_no,j.entry_date,COALESCE(j.description,''),COALESCE(j.reference_type,''),l.debit,l.credit,j.status FROM journal_lines l JOIN journal_entries j ON j.id=l.journal_id JOIN chart_of_accounts a ON a.id=l.account_id WHERE j.tenant_id=$1 AND a.code=$2 AND j.entry_date>=COALESCE(NULLIF($3,'')::date,'2000-01-01') AND j.entry_date<=COALESCE(NULLIF($4,'')::date,current_date) ORDER BY j.entry_date DESC,j.created_at DESC LIMIT $5 OFFSET $6`,claimsFrom(r).Tenant,r.PathValue("code"),q.Get("from"),q.Get("to"),queryLimit(q.Get("limit"),50),queryOffset(q.Get("offset")));if err!=nil{http.Error(w,err.Error(),500);return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id,no,desc,ref,status string;var date any;var debit,credit float64;rows.Scan(&id,&no,&date,&desc,&ref,&debit,&credit,&status);out=append(out,map[string]any{"id":id,"entryNo":no,"date":date,"description":desc,"reference":ref,"debit":debit,"credit":credit,"status":status})};json.NewEncoder(w).Encode(out)} }

func groupedStatement(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){name:=r.PathValue("name");q:=r.URL.Query();from,to:=q.Get("from"),q.Get("to");tenant:=claimsFrom(r).Tenant;query:="";if name=="profit-loss"{query=`SELECT a.account_type,a.code,a.name,CASE WHEN a.account_type='income' THEN sum(l.credit-l.debit) ELSE sum(l.debit-l.credit) END amount FROM journal_lines l JOIN journal_entries j ON j.id=l.journal_id JOIN chart_of_accounts a ON a.id=l.account_id WHERE j.tenant_id=$1 AND j.status='posted' AND j.entry_date BETWEEN COALESCE(NULLIF($2,'')::date,'2000-01-01') AND COALESCE(NULLIF($3,'')::date,current_date) AND a.account_type IN('income','expense') GROUP BY a.id ORDER BY a.account_type DESC,a.code`}else if name=="balance-sheet"{query=`SELECT a.account_type,a.code,a.name,CASE WHEN a.account_type='asset' THEN sum(l.debit-l.credit) ELSE sum(l.credit-l.debit) END amount FROM journal_lines l JOIN journal_entries j ON j.id=l.journal_id JOIN chart_of_accounts a ON a.id=l.account_id WHERE j.tenant_id=$1 AND j.status='posted' AND j.entry_date<=COALESCE(NULLIF($3,'')::date,current_date) AND a.account_type IN('asset','liability','equity') GROUP BY a.id ORDER BY a.account_type,a.code`}else{http.Error(w,"unknown statement",404);return};rows,err:=db.Query(r.Context(),query,tenant,from,to);if err!=nil{http.Error(w,err.Error(),500);return};defer rows.Close();groups:=map[string][]map[string]any{};totals:=map[string]float64{};for rows.Next(){var typ,code,label string;var amount float64;rows.Scan(&typ,&code,&label,&amount);groups[typ]=append(groups[typ],map[string]any{"code":code,"name":label,"amount":amount});totals[typ]+=amount};result:=map[string]any{"name":name,"from":from,"to":to,"groups":groups,"totals":totals};if name=="profit-loss"{result["netProfit"]=totals["income"]-totals["expense"]}else{result["difference"]=totals["asset"]-totals["liability"]-totals["equity"]};json.NewEncoder(w).Encode(result)} }
func queryOffset(raw string)int{n:=0;for _,c:=range strings.TrimSpace(raw){if c<'0'||c>'9'{return 0};n=n*10+int(c-'0');if n>100000{return 100000}};return n}
