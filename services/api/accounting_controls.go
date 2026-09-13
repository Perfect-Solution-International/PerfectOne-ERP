package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func registerAccountingControls(m *http.ServeMux, db *pgxpool.Pool) {
	registerAccountingInsights(m, db)
	m.HandleFunc("GET /accounting/periods", jsonAPI(authenticated(db, financeRoles...)(listFiscalPeriods(db))))
	m.HandleFunc("POST /accounting/periods", jsonAPI(authenticated(db, financeRoles...)(saveFiscalPeriod(db))))
	m.HandleFunc("POST /accounting/opening-balances/sync", jsonAPI(authenticated(db, financeRoles...)(syncOpeningBalances(db))))
	m.HandleFunc("POST /accounting/opening-balances", jsonAPI(authenticated(db, financeRoles...)(createOpeningBalance(db))))
	m.HandleFunc("GET /accounting/reconciliation", jsonAPI(authenticated(db, financeRoles...)(accountingReconciliation(db))))
	m.HandleFunc("GET /accounting/year-closures", jsonAPI(authenticated(db, financeRoles...)(listYearClosures(db))))
	m.HandleFunc("POST /accounting/periods/{id}/close-year", jsonAPI(authenticated(db, financeRoles...)(closeFiscalYear(db))))
	m.HandleFunc("PUT /accounting/journals/{id}", jsonAPI(authenticated(db, financeRoles...)(updateDraftJournal(db))))
	m.HandleFunc("DELETE /accounting/journals/{id}", jsonAPI(authenticated(db, financeRoles...)(deleteDraftJournal(db))))
}

func listFiscalPeriods(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(r.Context(), `SELECT p.id,p.name,p.start_date,p.end_date,p.status,COALESCE(u.name,''),p.locked_at FROM fiscal_periods p LEFT JOIN users u ON u.id=p.locked_by WHERE p.tenant_id=$1 ORDER BY p.start_date DESC`, claimsFrom(r).Tenant)
	if err != nil { http.Error(w, err.Error(), 500); return }; defer rows.Close(); out:=[]map[string]any{}
	for rows.Next(){ var id,name,status,user string; var start,end,locked any; if rows.Scan(&id,&name,&start,&end,&status,&user,&locked)==nil { out=append(out,map[string]any{"id":id,"name":name,"start":start,"end":end,"status":status,"user":user,"lockedAt":locked}) } }; json.NewEncoder(w).Encode(out)
} }

func saveFiscalPeriod(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) {
	var x struct{Name,Start,End,Status string}; if json.NewDecoder(r.Body).Decode(&x)!=nil || strings.TrimSpace(x.Name)=="" || x.Start=="" || x.End=="" || (x.Status!="open"&&x.Status!="locked"&&x.Status!="closed") { http.Error(w,"name, dates and open/locked/closed status are required",400); return }
	c:=claimsFrom(r); var id string; err:=db.QueryRow(r.Context(),`INSERT INTO fiscal_periods(tenant_id,name,start_date,end_date,status,locked_by,locked_at) VALUES($1,$2,$3::date,$4::date,$5,CASE WHEN $5='open' THEN NULL ELSE NULLIF($6,'')::uuid END,CASE WHEN $5='open' THEN NULL ELSE now() END) ON CONFLICT(tenant_id,name) DO UPDATE SET start_date=EXCLUDED.start_date,end_date=EXCLUDED.end_date,status=EXCLUDED.status,locked_by=EXCLUDED.locked_by,locked_at=EXCLUDED.locked_at RETURNING id`,c.Tenant,strings.TrimSpace(x.Name),x.Start,x.End,x.Status,c.Sub).Scan(&id)
	if err!=nil { http.Error(w,"period overlaps, dates are invalid, or period could not be saved",409); return }; auditUserAction(r,db,"FISCAL_PERIOD_"+strings.ToUpper(x.Status),id,x); w.WriteHeader(201); json.NewEncoder(w).Encode(map[string]string{"id":id})
} }

func validateJournalInput(x journalInput) error { debit,credit:=0.0,0.0; if strings.TrimSpace(x.Description)==""||len(x.Lines)<2{return fmt.Errorf("description and at least two lines are required")}; for _,l:=range x.Lines{if l.AccountID==""||l.Debit<0||l.Credit<0||(l.Debit>0)==(l.Credit>0){return fmt.Errorf("each line must contain one positive debit or credit")};debit+=l.Debit;credit+=l.Credit};if fmt.Sprintf("%.2f",debit)!=fmt.Sprintf("%.2f",credit)||debit<=0{return fmt.Errorf("journal debits and credits must balance")};return nil }

func updateDraftJournal(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){ var x journalInput;if json.NewDecoder(r.Body).Decode(&x)!=nil{http.Error(w,"invalid journal",400);return};if err:=validateJournalInput(x);err!=nil{http.Error(w,err.Error(),400);return};c:=claimsFrom(r);tx,err:=db.Begin(r.Context());if err!=nil{http.Error(w,err.Error(),500);return};defer tx.Rollback(r.Context());tag,err:=tx.Exec(r.Context(),`UPDATE journal_entries SET entry_date=COALESCE(NULLIF($3,'')::date,entry_date),description=concat_ws(' / ',$4::text,NULLIF($5::text,'')),updated_at=now() WHERE id=$1 AND tenant_id=$2 AND status='draft' AND source='manual'`,r.PathValue("id"),c.Tenant,x.Date,strings.TrimSpace(x.Description),strings.TrimSpace(x.Reference));if err==nil&&tag.RowsAffected()==1{_,err=tx.Exec(r.Context(),`DELETE FROM journal_lines WHERE journal_id=$1`,r.PathValue("id"))};for _,l:=range x.Lines{if err==nil{var n int;err=tx.QueryRow(r.Context(),`WITH i AS(INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) SELECT $1,a.id,$3,$4,NULLIF($5,'') FROM chart_of_accounts a WHERE a.id=$2 AND a.tenant_id=$6 AND a.is_active AND a.allow_manual_entries RETURNING 1)SELECT count(*) FROM i`,r.PathValue("id"),l.AccountID,l.Debit,l.Credit,strings.TrimSpace(l.Memo),c.Tenant).Scan(&n);if err==nil&&n!=1{err=fmt.Errorf("invalid manual account")}}};if err!=nil||tag.RowsAffected()!=1||tx.Commit(r.Context())!=nil{http.Error(w,"only an unlocked manual draft can be edited",409);return};auditUserAction(r,db,"JOURNAL_DRAFT_UPDATED",r.PathValue("id"),map[string]any{"description":x.Description});w.WriteHeader(204) } }
func deleteDraftJournal(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){tag,err:=db.Exec(r.Context(),`DELETE FROM journal_entries WHERE id=$1 AND tenant_id=$2 AND status='draft' AND source='manual'`,r.PathValue("id"),claimsFrom(r).Tenant);if err!=nil||tag.RowsAffected()!=1{http.Error(w,"only a manual draft journal can be deleted",409);return};auditUserAction(r,db,"JOURNAL_DRAFT_DELETED",r.PathValue("id"),nil);w.WriteHeader(204)} }

// accountingReconciliation is the original reconciliation endpoint, kept at its
// old URL for existing clients. It now reads the same function as
// /accounting/reconciliation-v2 instead of its own copy of the query, which
// reported a different inventory figure because it filtered on active products.
func accountingReconciliation(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(r.Context(),
			`SELECT code, name, ledger, subledger, difference, balanced, note
			   FROM accounting_control_reconciliation($1, current_date)
			  WHERE control_type IN ('customer','supplier')
			    AND subledger IS NOT NULL`,
			claimsFrom(r).Tenant)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var code, name, note string
			var ledger, subledger, difference float64
			var balanced bool
			if err := rows.Scan(&code, &name, &ledger, &subledger, &difference, &balanced, &note); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			out = append(out, map[string]any{
				"code": code, "name": name, "ledger": ledger,
				"operational": subledger, "subledger": subledger,
				"difference": difference, "balanced": balanced, "note": note,
			})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func listYearClosures(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){rows,err:=db.Query(r.Context(),`SELECT y.id,p.name,y.retained_earnings,y.closed_at,COALESCE(u.name,'System'),j.entry_no FROM accounting_year_closures y JOIN fiscal_periods p ON p.id=y.fiscal_period_id JOIN journal_entries j ON j.id=y.closing_journal_id LEFT JOIN users u ON u.id=y.closed_by WHERE y.tenant_id=$1 ORDER BY y.closed_at DESC`,claimsFrom(r).Tenant);if err!=nil{http.Error(w,err.Error(),500);return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id,name,user,no string;var amount float64;var at any;rows.Scan(&id,&name,&amount,&at,&user,&no);out=append(out,map[string]any{"id":id,"period":name,"amount":amount,"closedAt":at,"user":user,"journalNo":no})};json.NewEncoder(w).Encode(out)} }

func closeFiscalYear(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){var x struct{Reason,RequestID string};if json.NewDecoder(r.Body).Decode(&x)!=nil||strings.TrimSpace(x.Reason)==""||x.RequestID==""{http.Error(w,"reason and request ID are required",400);return};c:=claimsFrom(r);tx,err:=db.Begin(r.Context());if err!=nil{http.Error(w,err.Error(),500);return};defer tx.Rollback(r.Context());var start,end any;var name string;err=tx.QueryRow(r.Context(),`SELECT name,start_date,end_date FROM fiscal_periods WHERE id=$1 AND tenant_id=$2 AND status='open' AND NOT EXISTS(SELECT 1 FROM accounting_year_closures WHERE tenant_id=$2 AND fiscal_period_id=$1) FOR UPDATE`,r.PathValue("id"),c.Tenant).Scan(&name,&start,&end);if err!=nil{http.Error(w,"open, unclosed fiscal period not found",409);return};var drafts int;err=tx.QueryRow(r.Context(),`SELECT count(*) FROM journal_entries WHERE tenant_id=$1 AND status='draft' AND entry_date BETWEEN $2::date AND $3::date`,c.Tenant,start,end).Scan(&drafts);if err!=nil||drafts>0{http.Error(w,"post or delete all draft journals in this period before closing",409);return};var journal,retained string;err=tx.QueryRow(r.Context(),`SELECT id FROM chart_of_accounts WHERE tenant_id=$1 AND code='3100' AND is_active`,c.Tenant).Scan(&retained);if err==nil{err=tx.QueryRow(r.Context(),`INSERT INTO journal_entries(tenant_id,entry_date,reference_type,description,created_by,status,posted_at,source,client_request_id)VALUES($1,$2::date,'year_close',$3,$4,'posted',now(),'year_close',$5)RETURNING id`,c.Tenant,end,"Year-end close: "+name+" — "+strings.TrimSpace(x.Reason),c.Sub,x.RequestID).Scan(&journal)};if err==nil{_,err=tx.Exec(r.Context(),`WITH balances AS(SELECT a.id,a.account_type,COALESCE(sum(l.debit-l.credit),0) bal FROM chart_of_accounts a JOIN journal_lines l ON l.account_id=a.id JOIN journal_entries j ON j.id=l.journal_id WHERE a.tenant_id=$1 AND a.account_type IN('income','expense') AND j.status='posted' AND j.entry_date BETWEEN $2::date AND $3::date AND j.id<>$4 GROUP BY a.id,a.account_type) INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo) SELECT $4,id,CASE WHEN bal<0 THEN -bal ELSE 0 END,CASE WHEN bal>0 THEN bal ELSE 0 END,'Year-end close' FROM balances WHERE bal<>0`,c.Tenant,start,end,journal)};var net float64;if err==nil{err=tx.QueryRow(r.Context(),`SELECT COALESCE(sum(credit-debit),0) FROM journal_lines WHERE journal_id=$1`,journal).Scan(&net)};if err==nil&&net!=0{if net>0{_,err=tx.Exec(r.Context(),`INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo)VALUES($1,$2,$3,0,'Transfer to retained earnings')`,journal,retained,net)}else{_,err=tx.Exec(r.Context(),`INSERT INTO journal_lines(journal_id,account_id,debit,credit,memo)VALUES($1,$2,0,$3,'Transfer to retained earnings')`,journal,retained,-net)}};if err==nil{_,err=tx.Exec(r.Context(),`INSERT INTO accounting_year_closures(tenant_id,fiscal_period_id,closing_journal_id,retained_earnings,closed_by)VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid)`,c.Tenant,r.PathValue("id"),journal,net,c.Sub)};if err==nil{_,err=tx.Exec(r.Context(),`UPDATE fiscal_periods SET status='closed',locked_by=NULLIF($2,'')::uuid,locked_at=now() WHERE id=$1`,r.PathValue("id"),c.Sub)};if err!=nil||tx.Commit(r.Context())!=nil{http.Error(w,"year-end closing failed",409);return};auditUserAction(r,db,"FISCAL_YEAR_CLOSED",r.PathValue("id"),map[string]any{"journalId":journal,"retainedEarnings":net,"reason":x.Reason});w.WriteHeader(201);json.NewEncoder(w).Encode(map[string]any{"journalId":journal,"retainedEarnings":net})} }
