package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func reconcileCashierSession(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){
	c:=claimsFrom(r);if !canManageCashierSession(c.Role){http.Error(w,"manager permission required",403);return}
	var x struct{Note string `json:"note"`;ApproveVariance bool `json:"approveVariance"`}
	if json.NewDecoder(r.Body).Decode(&x)!=nil{http.Error(w,"invalid reconciliation",400);return}
	tx,err:=db.Begin(r.Context());if err!=nil{http.Error(w,err.Error(),500);return};defer tx.Rollback(r.Context())
	var variance float64
	err=tx.QueryRow(r.Context(),`SELECT COALESCE(variance,0) FROM cashier_sessions WHERE id=$1 AND tenant_id=$2 AND closed_at IS NOT NULL AND reconciled_at IS NULL FOR UPDATE`,r.PathValue("id"),c.Tenant).Scan(&variance)
	if err==pgx.ErrNoRows{http.Error(w,"closed unreconciled session not found",404);return};if err!=nil{http.Error(w,err.Error(),500);return}
	if variance!=0 && (!x.ApproveVariance||strings.TrimSpace(x.Note)==""){http.Error(w,"approve the variance and enter a reconciliation note",400);return}
	_,err=tx.Exec(r.Context(),`UPDATE cashier_sessions SET reconciled_by=$2,reconciled_at=now(),reconciliation_note=NULLIF($3,''),reconciliation_variance_approved=$4 WHERE id=$1`,r.PathValue("id"),c.Sub,strings.TrimSpace(x.Note),x.ApproveVariance)
	if err!=nil{http.Error(w,err.Error(),500);return};if err=tx.Commit(r.Context());err!=nil{http.Error(w,err.Error(),500);return}
	auditUserAction(r,db,"CASHIER_SESSION_RECONCILED",r.PathValue("id"),map[string]any{"variance":variance,"note":x.Note,"approved":x.ApproveVariance})
	json.NewEncoder(w).Encode(map[string]any{"id":r.PathValue("id"),"status":"reconciled","variance":variance})
} }

func discountPolicies(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){
	c:=claimsFrom(r);if !canManageCashierSession(c.Role){http.Error(w,"manager permission required",403);return}
	rows,err:=db.Query(r.Context(),`SELECT r.id,r.key,r.name,COALESCE(d.max_item_percent,0),COALESCE(d.max_invoice_percent,0) FROM roles r LEFT JOIN role_discount_limits d ON d.role_id=r.id WHERE r.tenant_id=$1 AND r.is_active ORDER BY r.is_system DESC,r.name`,c.Tenant)
	if err!=nil{http.Error(w,err.Error(),500);return};defer rows.Close();out:=[]map[string]any{}
	for rows.Next(){var id,key,name string;var item,invoice float64;if err=rows.Scan(&id,&key,&name,&item,&invoice);err!=nil{http.Error(w,err.Error(),500);return};out=append(out,map[string]any{"roleId":id,"key":key,"name":name,"maxItemPercent":item,"maxInvoicePercent":invoice})}
	json.NewEncoder(w).Encode(out)
} }

func updateDiscountPolicy(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){
	c:=claimsFrom(r);if !canManageCashierSession(c.Role){http.Error(w,"manager permission required",403);return}
	var x struct{RoleID string `json:"roleId"`;MaxItemPercent float64 `json:"maxItemPercent"`;MaxInvoicePercent float64 `json:"maxInvoicePercent"`}
	if json.NewDecoder(r.Body).Decode(&x)!=nil||x.RoleID==""||x.MaxItemPercent<0||x.MaxItemPercent>100||x.MaxInvoicePercent<0||x.MaxInvoicePercent>100{http.Error(w,"valid role and discount percentages are required",400);return}
	result,err:=db.Exec(r.Context(),`INSERT INTO role_discount_limits(role_id,max_item_percent,max_invoice_percent,updated_at) SELECT id,$1,$2,now() FROM roles WHERE id=$3 AND tenant_id=$4 ON CONFLICT(role_id) DO UPDATE SET max_item_percent=EXCLUDED.max_item_percent,max_invoice_percent=EXCLUDED.max_invoice_percent,updated_at=now()`,x.MaxItemPercent,x.MaxInvoicePercent,x.RoleID,c.Tenant)
	if err!=nil{http.Error(w,err.Error(),500);return};if result.RowsAffected()==0{http.Error(w,"role not found",404);return}
	auditUserAction(r,db,"DISCOUNT_POLICY_UPDATED",x.RoleID,map[string]any{"maxItemPercent":x.MaxItemPercent,"maxInvoicePercent":x.MaxInvoicePercent})
	json.NewEncoder(w).Encode(map[string]any{"roleId":x.RoleID,"saved":true})
} }
