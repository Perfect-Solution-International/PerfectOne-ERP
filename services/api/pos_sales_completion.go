package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type holdSaleInput struct {
	CustomerID string `json:"customerId"`
	Discount float64 `json:"discount"`
	RequestID string `json:"requestId"`
	Cart []struct { ProductID string `json:"productId"`; Quantity float64 `json:"quantity"`; Name string `json:"name"`; Price float64 `json:"price"`; Discount float64 `json:"discount"` } `json:"cart"`
}

func holdSale(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){
	var x holdSaleInput
	if json.NewDecoder(r.Body).Decode(&x)!=nil||len(x.Cart)==0||x.Discount<0||strings.TrimSpace(x.RequestID)==""{http.Error(w,"cart and request ID are required",400);return}
	for _,line:=range x.Cart{if line.ProductID==""||line.Quantity<=0{http.Error(w,"invalid held sale line",400);return}}
	c:=claimsFrom(r);tx,err:=db.Begin(r.Context());if err!=nil{http.Error(w,err.Error(),500);return};defer tx.Rollback(r.Context())
	var session string;if err=tx.QueryRow(r.Context(),`SELECT id FROM cashier_sessions WHERE tenant_id=$1 AND user_id=$2 AND branch_id=NULLIF($3,'')::uuid AND closed_at IS NULL FOR UPDATE`,c.Tenant,c.Sub,c.Branch).Scan(&session);err!=nil{http.Error(w,"open a cashier session before holding a sale",409);return}
	data,_:=json.Marshal(x.Cart);var id,ref string
	err=tx.QueryRow(r.Context(),`INSERT INTO held_sales(tenant_id,branch_id,cashier_id,cashier_session_id,customer_id,reference,cart,invoice_discount,client_request_id) VALUES($1,NULLIF($2,'')::uuid,$3,$4,NULLIF($5,'')::uuid,'HOLD-'||upper(substr(replace(gen_random_uuid()::text,'-',''),1,8)),$6,$7,$8) ON CONFLICT(tenant_id,client_request_id) DO UPDATE SET client_request_id=EXCLUDED.client_request_id RETURNING id,reference`,c.Tenant,c.Branch,c.Sub,session,x.CustomerID,data,x.Discount,x.RequestID).Scan(&id,&ref)
	if err!=nil{http.Error(w,err.Error(),409);return};if err=tx.Commit(r.Context());err!=nil{http.Error(w,err.Error(),500);return}
	w.WriteHeader(201);json.NewEncoder(w).Encode(map[string]any{"id":id,"reference":ref,"status":"held"})
} }

func heldSales(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){
	c:=claimsFrom(r);query:=`SELECT h.id,h.reference,COALESCE(cu.name,'Walk-in customer'),h.customer_id,h.cart,h.invoice_discount,h.held_at,COALESCE(u.name,'Unknown cashier'),h.cashier_id::text FROM held_sales h LEFT JOIN customers cu ON cu.id=h.customer_id LEFT JOIN users u ON u.id=h.cashier_id WHERE h.tenant_id=$1 AND h.branch_id=NULLIF($2,'')::uuid AND h.status='held'`;args:=[]any{c.Tenant,c.Branch};if !canManageCashierSession(c.Role){query+=` AND h.cashier_id=$3`;args=append(args,c.Sub)};query+=` ORDER BY h.held_at DESC`
	rows,err:=db.Query(r.Context(),query,args...)
	if err!=nil{http.Error(w,err.Error(),500);return};defer rows.Close();out:=[]map[string]any{}
	for rows.Next(){var id,ref,customer,cashier,cashierID string;var customerID,heldAt any;var cart []byte;var discount float64;if err=rows.Scan(&id,&ref,&customer,&customerID,&cart,&discount,&heldAt,&cashier,&cashierID);err!=nil{http.Error(w,err.Error(),500);return};var lines any;json.Unmarshal(cart,&lines);out=append(out,map[string]any{"id":id,"reference":ref,"customer":customer,"customerId":customerID,"cart":lines,"discount":discount,"heldAt":heldAt,"cashier":cashier,"owned":cashierID==c.Sub})}
	json.NewEncoder(w).Encode(out)
} }

func resolveHeldSale(db *pgxpool.Pool,status string) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){
	c:=claimsFrom(r);tx,err:=db.Begin(r.Context());if err!=nil{http.Error(w,err.Error(),500);return};defer tx.Rollback(r.Context())
	var id,ref,customer string;var cart []byte;var discount float64
	query:=`UPDATE held_sales SET status=$4,resolved_at=now() WHERE id=$1 AND tenant_id=$2 AND status='held' AND ($5 OR cashier_id=$3) RETURNING id,reference,COALESCE(customer_id::text,''),cart,invoice_discount`
	managerCancel:=status=="cancelled"&&canManageCashierSession(c.Role)
	err=tx.QueryRow(r.Context(),query,r.PathValue("id"),c.Tenant,c.Sub,status,managerCancel).Scan(&id,&ref,&customer,&cart,&discount)
	if err==pgx.ErrNoRows{http.Error(w,"held sale not found",404);return};if err!=nil{http.Error(w,err.Error(),500);return};if err=tx.Commit(r.Context());err!=nil{http.Error(w,err.Error(),500);return}
	var lines any;json.Unmarshal(cart,&lines);json.NewEncoder(w).Encode(map[string]any{"id":id,"reference":ref,"customerId":customer,"cart":lines,"discount":discount,"status":status})
} }
