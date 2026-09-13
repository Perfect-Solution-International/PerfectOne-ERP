package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type promotionInput struct {
	Name, Kind, ProductID, CategoryID, StartDate, EndDate string
	Value float64
	IsActive bool
}

func promotionDiscount(kind string, value, unitPrice float64) float64 {
	var discount float64
	switch kind { case "percentage": discount=unitPrice*value/100; case "fixed": discount=value; case "special_price": discount=unitPrice-value }
	return math.Round(math.Max(0,math.Min(unitPrice,discount))*100)/100
}

func validPromotion(x promotionInput) string {
	x.Name=strings.TrimSpace(x.Name)
	if x.Name=="" { return "promotion name is required" }
	if (x.ProductID=="")== (x.CategoryID=="") { return "select either one product or one category" }
	if x.Kind!="fixed"&&x.Kind!="percentage"&&x.Kind!="special_price" { return "invalid discount type" }
	if x.Value<=0 || (x.Kind=="percentage"&&x.Value>100) { return "enter a valid promotion value" }
	start,e1:=time.Parse("2006-01-02",x.StartDate); end,e2:=time.Parse("2006-01-02",x.EndDate)
	if e1!=nil||e2!=nil||end.Before(start) { return "enter a valid promotion date range" }
	return ""
}

func promotions(db *pgxpool.Pool) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) {
	includeInactive:=r.URL.Query().Get("includeInactive")=="true"
	rows,e:=db.Query(r.Context(),`SELECT pr.id,pr.name,pr.kind,pr.value,pr.start_date,pr.end_date,pr.is_active,pr.archived_at IS NOT NULL,
	 COALESCE(pr.product_id::text,''),COALESCE(p.name,''),COALESCE(pr.category_id::text,''),COALESCE(c.name,''),
	 CASE WHEN pr.is_active AND pr.archived_at IS NULL AND current_date BETWEEN pr.start_date AND pr.end_date THEN 'running' WHEN pr.archived_at IS NOT NULL THEN 'archived' WHEN NOT pr.is_active THEN 'inactive' WHEN current_date<pr.start_date THEN 'scheduled' ELSE 'expired' END,
	 COUNT(pu.id),COALESCE(SUM(pu.discount_amount),0)
	 FROM promotions pr LEFT JOIN products p ON p.id=pr.product_id LEFT JOIN categories c ON c.id=pr.category_id LEFT JOIN promotion_usage pu ON pu.promotion_id=pr.id
	 WHERE pr.tenant_id=$1 AND ($2 OR (pr.is_active AND pr.archived_at IS NULL)) GROUP BY pr.id,p.name,c.name ORDER BY pr.created_at DESC`,claimsFrom(r).Tenant,includeInactive)
	if e!=nil { http.Error(w,e.Error(),500);return }; defer rows.Close(); out:=[]map[string]any{}
	for rows.Next(){var id,name,kind,productID,product,categoryID,category,status string;var start,end time.Time;var value,total float64;var active,archived bool;var uses int64
		if e=rows.Scan(&id,&name,&kind,&value,&start,&end,&active,&archived,&productID,&product,&categoryID,&category,&status,&uses,&total);e!=nil{http.Error(w,e.Error(),500);return}
		out=append(out,map[string]any{"id":id,"name":name,"kind":kind,"value":value,"startDate":start.Format("2006-01-02"),"endDate":end.Format("2006-01-02"),"isActive":active,"archived":archived,"productId":productID,"product":product,"categoryId":categoryID,"category":category,"status":status,"usageCount":uses,"discountTotal":total}) }
	json.NewEncoder(w).Encode(out)
} }

func decodePromotion(r *http.Request)(promotionInput,error){var x promotionInput;e:=json.NewDecoder(r.Body).Decode(&x);return x,e}
func ensurePromotionTarget(r *http.Request,db *pgxpool.Pool,x promotionInput,exclude string) error {
	tenant:=claimsFrom(r).Tenant; var exists bool
	if x.ProductID!="" { if e:=db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM products WHERE id=$1 AND tenant_id=$2)`,x.ProductID,tenant).Scan(&exists);e!=nil||!exists{return pgx.ErrNoRows} } else { if e:=db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1 AND tenant_id=$2)`,x.CategoryID,tenant).Scan(&exists);e!=nil||!exists{return pgx.ErrNoRows} }
	if e:=db.QueryRow(r.Context(),`SELECT NOT EXISTS(SELECT 1 FROM promotions WHERE tenant_id=$1 AND id<>COALESCE(NULLIF($2,'')::uuid,'00000000-0000-0000-0000-000000000000') AND archived_at IS NULL AND is_active AND product_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND category_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND daterange(start_date,end_date,'[]')&&daterange($5::date,$6::date,'[]'))`,tenant,exclude,x.ProductID,x.CategoryID,x.StartDate,x.EndDate).Scan(&exists);e!=nil{return e};if !exists{return pgx.ErrNoRows};return nil
}
func writePromotionAudit(r *http.Request,db *pgxpool.Pool,action,id string,data any){b,_:=json.Marshal(data);c:=claimsFrom(r);db.Exec(r.Context(),`INSERT INTO audit_logs(tenant_id,user_id,action,entity_type,entity_id,new_data,ip_address) VALUES($1,$2,$3,'promotion',$4,NULLIF($5,'')::jsonb,NULLIF($6,'')::inet)`,c.Tenant,c.Sub,action,id,string(b),clientIP(r))}
func createPromotion(db *pgxpool.Pool) http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){x,e:=decodePromotion(r);if e!=nil{http.Error(w,"invalid promotion",400);return};if msg:=validPromotion(x);msg!=""{http.Error(w,msg,400);return};if e=ensurePromotionTarget(r,db,x,"");e!=nil{http.Error(w,"target not found or promotion dates overlap",409);return};c:=claimsFrom(r);var id string;e=db.QueryRow(r.Context(),`INSERT INTO promotions(tenant_id,name,product_id,category_id,kind,value,start_date,end_date,is_active,created_by,updated_by) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$10) RETURNING id`,c.Tenant,strings.TrimSpace(x.Name),x.ProductID,x.CategoryID,x.Kind,x.Value,x.StartDate,x.EndDate,x.IsActive,c.Sub).Scan(&id);if e!=nil{http.Error(w,e.Error(),409);return};writePromotionAudit(r,db,"create",id,x);w.WriteHeader(201);json.NewEncoder(w).Encode(map[string]string{"id":id})}}
func updatePromotion(db *pgxpool.Pool) http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){x,e:=decodePromotion(r);id:=r.PathValue("id");if e!=nil{http.Error(w,"invalid promotion",400);return};if msg:=validPromotion(x);msg!=""{http.Error(w,msg,400);return};if e=ensurePromotionTarget(r,db,x,id);e!=nil{http.Error(w,"target not found or promotion dates overlap",409);return};c:=claimsFrom(r);tag,e:=db.Exec(r.Context(),`UPDATE promotions SET name=$1,product_id=NULLIF($2,'')::uuid,category_id=NULLIF($3,'')::uuid,kind=$4,value=$5,start_date=$6,end_date=$7,is_active=$8,updated_by=$9,updated_at=now() WHERE id=$10 AND tenant_id=$11 AND archived_at IS NULL`,strings.TrimSpace(x.Name),x.ProductID,x.CategoryID,x.Kind,x.Value,x.StartDate,x.EndDate,x.IsActive,c.Sub,id,c.Tenant);if e!=nil{http.Error(w,e.Error(),409);return};if tag.RowsAffected()==0{http.Error(w,"promotion not found",404);return};writePromotionAudit(r,db,"update",id,x);json.NewEncoder(w).Encode(map[string]string{"id":id})}}
func deletePromotion(db *pgxpool.Pool) http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){c:=claimsFrom(r);id:=r.PathValue("id");var uses int;if e:=db.QueryRow(r.Context(),`SELECT COUNT(*) FROM promotion_usage pu JOIN promotions pr ON pr.id=pu.promotion_id WHERE pr.id=$1 AND pr.tenant_id=$2`,id,c.Tenant).Scan(&uses);e!=nil{http.Error(w,"promotion not found",404);return};result:="deleted";var e error;if uses==0{_,e=db.Exec(r.Context(),`DELETE FROM promotions WHERE id=$1 AND tenant_id=$2`,id,c.Tenant)}else{result="archived";_,e=db.Exec(r.Context(),`UPDATE promotions SET is_active=false,archived_at=now(),updated_by=$3,updated_at=now() WHERE id=$1 AND tenant_id=$2`,id,c.Tenant,c.Sub)};if e!=nil{http.Error(w,e.Error(),409);return};writePromotionAudit(r,db,result,id,map[string]int{"usageCount":uses});json.NewEncoder(w).Encode(map[string]string{"result":result})}}
func restorePromotion(db *pgxpool.Pool) http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){c:=claimsFrom(r);id:=r.PathValue("id");tag,e:=db.Exec(r.Context(),`UPDATE promotions SET archived_at=NULL,is_active=true,updated_by=$3,updated_at=now() WHERE id=$1 AND tenant_id=$2`,id,c.Tenant,c.Sub);if e!=nil||tag.RowsAffected()==0{http.Error(w,"promotion not found",404);return};writePromotionAudit(r,db,"restore",id,nil);json.NewEncoder(w).Encode(map[string]string{"id":id})}}
func promotionUsage(db *pgxpool.Pool) http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){rows,e:=db.Query(r.Context(),`SELECT pu.id,s.invoice_no,p.name,pu.quantity,pu.original_unit_price,pu.effective_unit_price,pu.discount_amount,pu.created_at FROM promotion_usage pu JOIN promotions pr ON pr.id=pu.promotion_id JOIN sales s ON s.id=pu.sale_id JOIN sale_items si ON si.id=pu.sale_item_id JOIN products p ON p.id=si.product_id WHERE pr.id=$1 AND pr.tenant_id=$2 ORDER BY pu.created_at DESC`,r.PathValue("id"),claimsFrom(r).Tenant);if e!=nil{http.Error(w,e.Error(),500);return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id,invoice,product string;var qty,original,effective,discount float64;var at time.Time;rows.Scan(&id,&invoice,&product,&qty,&original,&effective,&discount,&at);out=append(out,map[string]any{"id":id,"invoice":invoice,"product":product,"quantity":qty,"originalPrice":original,"effectivePrice":effective,"discount":discount,"createdAt":at})};json.NewEncoder(w).Encode(out)}}
