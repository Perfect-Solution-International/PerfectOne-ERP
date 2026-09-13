package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
)

type importInput struct {
	Kind, FileName, CSV, ContentBase64 string
	Mapping map[string]string
	Commit              bool
}

func registerDataImports(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /data-imports", jsonAPI(authenticated(db, "super_admin", "admin")(listImports(db))))
	m.HandleFunc("POST /data-imports", jsonAPI(authenticated(db, "super_admin", "admin")(runImport(db))))
	m.HandleFunc("POST /data-imports/inspect", jsonAPI(authenticated(db, "super_admin", "admin")(inspectImport())))
}
func inspectImport() http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){var x importInput;if json.NewDecoder(http.MaxBytesReader(w,r.Body,20<<20)).Decode(&x)!=nil{http.Error(w,"invalid upload or file is larger than 15 MB",400);return};records,e:=spreadsheetRecords(x.FileName,x.CSV,x.ContentBase64);if e!=nil{http.Error(w,e.Error(),400);return};preview:=records[1:];if len(preview)>8{preview=preview[:8]};json.NewEncoder(w).Encode(map[string]any{"headers":records[0],"preview":preview,"rowCount":len(records)-1})}}
func listImports(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT id,kind,status,COALESCE(file_name,''),row_count,success_count,error_count,errors,created_at FROM data_import_jobs WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 50`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, kind, status, file string
			var total, ok, bad int
			var errors, at any
			if rows.Scan(&id, &kind, &status, &file, &total, &ok, &bad, &errors, &at) == nil {
				out = append(out, map[string]any{"id": id, "kind": kind, "status": status, "fileName": file, "rowCount": total, "successCount": ok, "errorCount": bad, "errors": errors, "createdAt": at})
			}
		}
		json.NewEncoder(w).Encode(out)
	}
}
func runImport(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x importInput
		if json.NewDecoder(http.MaxBytesReader(w,r.Body,20<<20)).Decode(&x) != nil || !map[string]bool{"products": true, "customers": true, "suppliers": true, "opening_stock": true}[x.Kind] || (strings.TrimSpace(x.CSV) == ""&&x.ContentBase64=="") {
			http.Error(w, "kind and spreadsheet data required", 400)
			return
		}
		records, e := spreadsheetRecords(x.FileName,x.CSV,x.ContentBase64)
		if e != nil || len(records) < 2 {
			http.Error(w, "CSV must contain a header and at least one row", 400)
			return
		}
		headers := mappedHeaders(records[0],x.Mapping)
		get := func(row []string, key string) string {
			i, ok := headers[key]
			if !ok || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		c := claimsFrom(r)
		errs := []map[string]any{}
		valid := [][]string{}
		required := map[string][]string{"products": {"name", "sku", "selling_price"}, "customers": {"name"}, "suppliers": {"name"}, "opening_stock": {"sku", "quantity"}}[x.Kind]
		seenSKU,seenBarcode:=map[string]bool{},map[string]bool{}
		for n, row := range records[1:] {
			missing := ""
			for _, key := range required {
				if get(row, key) == "" {
					missing = key
					break
				}
			}
			if missing != "" {
				errs = append(errs, map[string]any{"row": n + 2, "error": "missing " + missing})
				continue
			}
			numeric:=[]string{};if x.Kind=="products"{numeric=[]string{"selling_price","purchase_price","wholesale_price","minimum_stock"}}else if x.Kind=="opening_stock"{numeric=[]string{"quantity"}}else if x.Kind=="customers"{numeric=[]string{"credit_limit"}}
			invalid:="";for _,key:=range numeric{raw:=get(row,key);if raw==""&&key!="selling_price"&&key!="quantity"{continue};v,err:=strconv.ParseFloat(raw,64);if err!=nil||v<0||(key=="quantity"&&v<=0){invalid=key;break}};if invalid!=""{errs=append(errs,map[string]any{"row":n+2,"error":"invalid "+invalid});continue}
			if email:=get(row,"email");email!=""&&(!strings.Contains(email,"@")||strings.ContainsAny(email," \t")){errs=append(errs,map[string]any{"row":n+2,"error":"invalid email"});continue}
			if x.Kind=="products"{sku:=strings.ToLower(get(row,"sku"));barcode:=get(row,"barcode");if seenSKU[sku]{errs=append(errs,map[string]any{"row":n+2,"error":"duplicate SKU in file"});continue};seenSKU[sku]=true;if barcode!=""{if seenBarcode[barcode]{errs=append(errs,map[string]any{"row":n+2,"error":"duplicate barcode in file"});continue};seenBarcode[barcode]=true}}
			var exists bool
			if x.Kind=="products"{_ = db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM products WHERE tenant_id=$1 AND (lower(sku)=lower($2) OR ($3<>'' AND barcode=$3)))`,c.Tenant,get(row,"sku"),get(row,"barcode")).Scan(&exists);if exists{errs=append(errs,map[string]any{"row":n+2,"error":"SKU or barcode already exists"});continue}}
			if x.Kind=="opening_stock"{_ = db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM products WHERE tenant_id=$1 AND lower(sku)=lower($2) AND is_active)`,c.Tenant,get(row,"sku")).Scan(&exists);if !exists{errs=append(errs,map[string]any{"row":n+2,"error":"active product SKU not found"});continue}}
			if x.Kind=="customers"&&get(row,"phone")!=""{_ = db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM customers WHERE tenant_id=$1 AND phone=$2)`,c.Tenant,get(row,"phone")).Scan(&exists);if exists{errs=append(errs,map[string]any{"row":n+2,"error":"customer phone already exists"});continue}}
			valid = append(valid, row)
		}
		status := "validated"
		success := 0
		if x.Commit && len(errs) == 0 {
			tx, err := db.Begin(r.Context())
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer tx.Rollback(r.Context())
			for _, row := range valid {
				switch x.Kind {
				case "products":
					price, _ := strconv.ParseFloat(get(row, "selling_price"), 64)
					cost, _ := strconv.ParseFloat(get(row, "purchase_price"), 64)
					wholesale,_:=strconv.ParseFloat(get(row,"wholesale_price"),64);minimum,_:=strconv.ParseFloat(get(row,"minimum_stock"),64)
					_, e = tx.Exec(r.Context(), `INSERT INTO products(tenant_id,name,sku,barcode,purchase_price,selling_price,wholesale_price,minimum_stock,is_active)VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,true)`, c.Tenant, get(row, "name"), get(row, "sku"), get(row, "barcode"), cost, price,wholesale,minimum)
				case "customers":
					limit,_:=strconv.ParseFloat(get(row,"credit_limit"),64);_,e=tx.Exec(r.Context(),`INSERT INTO customers(tenant_id,name,phone,email,address,credit_limit,is_active)VALUES($1,$2,NULLIF($3,''),NULLIF(lower($4),''),NULLIF($5,''),$6,true)`,c.Tenant,get(row,"name"),get(row,"phone"),get(row,"email"),get(row,"address"),limit)
				case "suppliers":
					_,e=tx.Exec(r.Context(),`INSERT INTO suppliers(tenant_id,name,company,phone,email,address,is_active)VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF(lower($5),''),NULLIF($6,''),true)`,c.Tenant,get(row,"name"),get(row,"company"),get(row,"phone"),get(row,"email"),get(row,"address"))
				case "opening_stock":
					qty, _ := strconv.ParseFloat(get(row, "quantity"), 64)
					var product string
					var balance float64
					e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$1 WHERE tenant_id=$2 AND sku=$3 AND is_active RETURNING id,stock_quantity`, qty, c.Tenant, get(row, "sku")).Scan(&product, &balance)
					if e == nil {
						_, e = tx.Exec(r.Context(), `INSERT INTO branch_stock(product_id,branch_id,quantity)VALUES($1,$2,$3)ON CONFLICT(product_id,branch_id)DO UPDATE SET quantity=branch_stock.quantity+EXCLUDED.quantity`, product, c.Branch, qty)
					}
					if e == nil {
						_, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,balance_after,notes,created_by)VALUES($1,'opening_import',$2,$3,$4,'data_import',$5,$6,$7)`, product, qty, c.Tenant, c.Branch, balance, "Opening stock CSV import: "+x.FileName, c.Sub)
					}
				}
				if e != nil {
					errs = append(errs, map[string]any{"row": success + 2, "error": e.Error()})
					break
				}
				success++
			}
			if len(errs) == 0 && tx.Commit(r.Context()) == nil {
				status = "completed"
			} else {
				success = 0
				status = "failed"
			}
		}
		payload, _ := json.Marshal(errs)
		var id string
		_ = db.QueryRow(r.Context(), `INSERT INTO data_import_jobs(tenant_id,kind,status,file_name,row_count,success_count,error_count,errors,created_by,completed_at)VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,NULLIF($9,'')::uuid,CASE WHEN $3='completed' THEN now() END)RETURNING id`, c.Tenant, x.Kind, status, x.FileName, len(records)-1, success, len(errs), payload, c.Sub).Scan(&id)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "status": status, "rowCount": len(records) - 1, "validCount": len(valid), "errors": errs})
	}
}
