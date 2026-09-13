package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

func fixedGroupedStatement(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
		tenant := claimsFrom(r).Tenant
		query := ""
		args := []any{}
		if name == "profit-loss" {
			query = `SELECT a.account_type,a.code,a.name,CASE WHEN a.account_type='income' THEN sum(l.credit-l.debit) ELSE sum(l.debit-l.credit) END amount FROM journal_lines l JOIN journal_entries j ON j.id=l.journal_id JOIN chart_of_accounts a ON a.id=l.account_id WHERE j.tenant_id=$1 AND j.status='posted' AND j.entry_date BETWEEN COALESCE(NULLIF($2,'')::date,'2000-01-01') AND COALESCE(NULLIF($3,'')::date,current_date) AND a.account_type IN('income','expense') GROUP BY a.id ORDER BY a.account_type DESC,a.code`
			args = []any{tenant, from, to}
		} else if name == "balance-sheet" {
			query = `SELECT account_type,code,name,amount FROM (SELECT a.account_type,a.code,a.name,CASE WHEN a.account_type='asset' THEN sum(l.debit-l.credit) ELSE sum(l.credit-l.debit) END amount FROM journal_lines l JOIN journal_entries j ON j.id=l.journal_id JOIN chart_of_accounts a ON a.id=l.account_id WHERE j.tenant_id=$1 AND j.status='posted' AND j.entry_date<=COALESCE(NULLIF($2,'')::date,current_date) AND a.account_type IN('asset','liability','equity') GROUP BY a.id UNION ALL SELECT 'equity','3999','Current period earnings',COALESCE(sum(CASE WHEN a.account_type='income' THEN l.credit-l.debit ELSE l.credit-l.debit END),0) FROM journal_lines l JOIN journal_entries j ON j.id=l.journal_id JOIN chart_of_accounts a ON a.id=l.account_id WHERE j.tenant_id=$1 AND j.status='posted' AND j.entry_date<=COALESCE(NULLIF($2,'')::date,current_date) AND a.account_type IN('income','expense')) x WHERE amount<>0 ORDER BY account_type,code`
			args = []any{tenant, to}
		} else {
			http.Error(w, "unknown statement", 404)
			return
		}
		rows, err := db.Query(r.Context(), query, args...)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		groups := map[string][]map[string]any{}
		totals := map[string]float64{}
		for rows.Next() {
			var typ, code, label string
			var amount float64
			if rows.Scan(&typ, &code, &label, &amount) == nil {
				groups[typ] = append(groups[typ], map[string]any{"code": code, "name": label, "amount": amount})
				totals[typ] += amount
			}
		}
		result := map[string]any{"name": name, "from": from, "to": to, "groups": groups, "totals": totals}
		if name == "profit-loss" {
			result["netProfit"] = totals["income"] - totals["expense"]
		} else {
			result["difference"] = totals["asset"] - totals["liability"] - totals["equity"]
		}
		json.NewEncoder(w).Encode(result)
	}
}
