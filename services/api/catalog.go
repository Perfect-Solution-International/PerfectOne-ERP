package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

type Contact struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Phone   string  `json:"phone"`
	Email   string  `json:"email"`
	Balance float64 `json:"balance"`
}
type ProductInput struct {
	Name                string             `json:"name"`
	ProductCode         string             `json:"productCode"`
	SKU                 string             `json:"sku"`
	Barcode             string             `json:"barcode"`
	CategoryID          string             `json:"categoryId"`
	SubcategoryID       string             `json:"subcategoryId"`
	BrandID             string             `json:"brandId"`
	UnitID              string             `json:"unitId"`
	PurchasePrice       float64            `json:"purchasePrice"`
	SellingPrice        float64            `json:"sellingPrice"`
	WholesalePrice      float64            `json:"wholesalePrice"`
	MinimumStock        float64            `json:"minimumStock"`
	OpeningStock        float64            `json:"openingStock"`
	TaxRate             float64            `json:"taxRate"`
	TrackExpiry         bool               `json:"trackExpiry"`
	Description         string             `json:"description"`
	ImageURL            string             `json:"imageUrl"`
	IsActive            bool               `json:"isActive"`
	MeasurementType     string             `json:"measurementType"`
	DecimalPrecision    int                `json:"decimalPrecision"`
	MinimumSaleQuantity float64            `json:"minimumSaleQuantity"`
	QuantityStep        float64            `json:"quantityStep"`
	TareWeight          float64            `json:"tareWeight"`
	PLUCode             string             `json:"pluCode"`
	AllowedUnits        []ProductUnitInput `json:"allowedUnits"`
}

type ProductUnitInput struct {
	UnitID          string   `json:"unitId"`
	Factor          float64  `json:"factorToBase"`
	Usage           string   `json:"usage"`
	PurchasePrice   *float64 `json:"purchasePrice"`
	SalePrice       *float64 `json:"salePrice"`
	Barcode         string   `json:"barcode"`
	DefaultPurchase bool     `json:"isDefaultPurchase"`
	DefaultSale     bool     `json:"isDefaultSale"`
}

func registerCatalog(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /customers", jsonAPI(authenticated(db, allStaff...)(listContacts(db, "customers"))))
	m.HandleFunc("POST /customers", jsonAPI(authenticated(db, financeRoles...)(createContact(db, "customers"))))
	m.HandleFunc("PUT /customers/{id}", jsonAPI(authenticated(db, financeRoles...)(updateContact(db, "customers"))))
	m.HandleFunc("DELETE /customers/{id}", jsonAPI(authenticated(db, financeRoles...)(deactivateContact(db, "customers"))))
	m.HandleFunc("GET /suppliers", jsonAPI(authenticated(db, allStaff...)(listContacts(db, "suppliers"))))
	m.HandleFunc("POST /suppliers", jsonAPI(authenticated(db, financeRoles...)(createContact(db, "suppliers"))))
	m.HandleFunc("PUT /suppliers/{id}", jsonAPI(authenticated(db, financeRoles...)(updateContact(db, "suppliers"))))
	m.HandleFunc("DELETE /suppliers/{id}", jsonAPI(authenticated(db, financeRoles...)(deactivateContact(db, "suppliers"))))
	m.HandleFunc("GET /product-metadata", jsonAPI(authenticated(db)(productMetadata(db))))
	m.HandleFunc("POST /products", jsonAPI(authenticated(db, stockRoles...)(secureCreateProduct(db))))
	m.HandleFunc("PUT /products/{id}", jsonAPI(authenticated(db, stockRoles...)(updateProduct(db))))
	m.HandleFunc("DELETE /products/{id}", jsonAPI(authenticated(db, stockRoles...)(deleteOrArchiveProduct(db))))
	m.HandleFunc("POST /products/{id}/restore", jsonAPI(authenticated(db, stockRoles...)(restoreProduct(db))))
	m.HandleFunc("GET /products/{id}/price-history", jsonAPI(authenticated(db, stockRoles...)(priceHistory(db))))
	registerMeasurements(m, db)
}
func listContacts(db *pgxpool.Pool, table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := "SELECT id,name,COALESCE(phone,''),COALESCE(email,''),balance FROM " + table + " WHERE tenant_id=$1 AND is_active ORDER BY name"
		rows, e := db.Query(r.Context(), q, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []Contact{}
		for rows.Next() {
			var x Contact
			rows.Scan(&x.ID, &x.Name, &x.Phone, &x.Email, &x.Balance)
			out = append(out, x)
		}
		json.NewEncoder(w).Encode(out)
	}
}
func createContact(db *pgxpool.Pool, table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x Contact
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.Name == "" {
			http.Error(w, "name required", 400)
			return
		}
		q := "INSERT INTO " + table + "(name,phone,email,balance,tenant_id) VALUES($1,$2,$3,$4,$5) RETURNING id"
		db.QueryRow(r.Context(), q, x.Name, x.Phone, x.Email, x.Balance, claimsFrom(r).Tenant).Scan(&x.ID)
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(x)
	}
}
func updateContact(db *pgxpool.Pool, table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct{ Name, Phone, Email string }
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.Name == "" {
			http.Error(w, "name required", 400)
			return
		}
		q := "UPDATE " + table + " SET name=$1,phone=$2,email=$3 WHERE id=$4 AND tenant_id=$5"
		tag, e := db.Exec(r.Context(), q, x.Name, x.Phone, x.Email, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "not found", 404)
			return
		}
		w.WriteHeader(204)
	}
}
func deactivateContact(db *pgxpool.Pool, table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := "UPDATE " + table + " SET is_active=false WHERE id=$1 AND tenant_id=$2"
		tag, e := db.Exec(r.Context(), q, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "not found", 404)
			return
		}
		w.WriteHeader(204)
	}
}
func productMetadata(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := claimsFrom(r).Tenant
		load := func(query string) []map[string]any {
			rows, e := db.Query(r.Context(), query, tenant)
			if e != nil {
				return []map[string]any{}
			}
			defer rows.Close()
			out := []map[string]any{}
			for rows.Next() {
				values, _ := rows.Values()
				x := map[string]any{"id": values[0], "name": values[1]}
				if len(values) > 2 {
					x["parentId"] = values[2]
				}
				if len(values) > 3 {
					x["symbol"] = values[3]
				}
				out = append(out, x)
			}
			return out
		}
		json.NewEncoder(w).Encode(map[string]any{"categories": load(`SELECT id,name FROM categories WHERE tenant_id=$1 AND is_active ORDER BY name`), "subcategories": load(`SELECT id,name,category_id FROM subcategories WHERE tenant_id=$1 AND is_active ORDER BY name`), "brands": load(`SELECT id,name FROM brands WHERE tenant_id=$1 AND is_active ORDER BY name`), "units": load(`SELECT id,name,NULL::uuid,symbol FROM units WHERE tenant_id=$1 AND is_active ORDER BY name`)})
	}
}
func validateProduct(p ProductInput) string {
	if strings.TrimSpace(p.Name) == "" {
		return "product name required"
	}
	if strings.TrimSpace(p.ProductCode) == "" {
		return "product code required"
	}
	if strings.TrimSpace(p.SKU) == "" {
		return "SKU required"
	}
	if p.SellingPrice < 0 || p.PurchasePrice < 0 || p.WholesalePrice < 0 || p.MinimumStock < 0 || p.OpeningStock < 0 || p.TaxRate < 0 {
		return "prices, stock and tax cannot be negative"
	}
	if p.MeasurementType == "" {
		p.MeasurementType = "count"
	}
	if p.MeasurementType != "weight" && p.MeasurementType != "volume" && p.MeasurementType != "count" && p.MeasurementType != "length" {
		return "invalid measurement type"
	}
	if p.DecimalPrecision < 0 || p.DecimalPrecision > 6 || p.MinimumSaleQuantity < 0 || p.QuantityStep < 0 || p.TareWeight < 0 {
		return "invalid measurement rules"
	}
	for _, u := range p.AllowedUnits {
		if u.UnitID == "" || u.Factor <= 0 || (u.Usage != "purchase" && u.Usage != "sale" && u.Usage != "both") {
			return "invalid allowed unit conversion"
		}
	}
	return ""
}
func createProduct(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p ProductInput
		if json.NewDecoder(r.Body).Decode(&p) != nil {
			http.Error(w, "invalid product", 400)
			return
		}
		if message := validateProduct(p); message != "" {
			http.Error(w, message, 400)
			return
		}
		c := claimsFrom(r)
		var id string
		e := db.QueryRow(r.Context(), `INSERT INTO products(name,product_code,sku,barcode,category_id,subcategory_id,brand_id,unit_id,purchase_price,selling_price,wholesale_price,stock_quantity,minimum_stock,tax_rate,track_expiry,description,image_url,is_active,tenant_id)VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,$9,$10,$11,$12,$13,$14,$15,$16,NULLIF($17,''),$18,$19)RETURNING id`, strings.TrimSpace(p.Name), strings.TrimSpace(p.ProductCode), strings.TrimSpace(p.SKU), strings.TrimSpace(p.Barcode), p.CategoryID, p.SubcategoryID, p.BrandID, p.UnitID, p.PurchasePrice, p.SellingPrice, p.WholesalePrice, p.OpeningStock, p.MinimumStock, p.TaxRate, p.TrackExpiry, p.Description, p.ImageURL, p.IsActive, c.Tenant).Scan(&id)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		auditUserAction(r, db, "PRODUCT_CREATED", id, map[string]any{"name": p.Name, "sku": p.SKU})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func updateProduct(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p ProductInput
		if json.NewDecoder(r.Body).Decode(&p) != nil {
			http.Error(w, "invalid product", 400)
			return
		}
		if message := validateProduct(p); message != "" {
			http.Error(w, message, 400)
			return
		}
		c := claimsFrom(r)
		id := r.PathValue("id")
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var oldPurchase, oldSelling, oldWholesale float64
		e = tx.QueryRow(r.Context(), `SELECT purchase_price,selling_price,wholesale_price FROM products WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, c.Tenant).Scan(&oldPurchase, &oldSelling, &oldWholesale)
		if e != nil {
			http.Error(w, "product not found", 404)
			return
		}
		measurement := p.MeasurementType
		if measurement == "" {
			measurement = "count"
		}
		minimum := p.MinimumSaleQuantity
		if minimum <= 0 {
			minimum = 1
		}
		step := p.QuantityStep
		if step <= 0 {
			step = 1
		}
		_, e = tx.Exec(r.Context(), `UPDATE products SET name=$1,product_code=$2,sku=$3,barcode=NULLIF($4,''),category_id=NULLIF($5,'')::uuid,subcategory_id=NULLIF($6,'')::uuid,brand_id=NULLIF($7,'')::uuid,unit_id=NULLIF($8,'')::uuid,purchase_price=$9,selling_price=$10,wholesale_price=$11,minimum_stock=$12,tax_rate=$13,track_expiry=$14,description=$15,image_url=NULLIF($16,''),is_active=$17,archived_at=CASE WHEN $17 THEN NULL ELSE COALESCE(archived_at,now()) END,updated_at=now(),measurement_type=$20,decimal_precision=$21,minimum_sale_quantity=$22,quantity_step=$23,tare_weight=$24,plu_code=NULLIF($25,'') WHERE id=$18 AND tenant_id=$19`, p.Name, p.ProductCode, p.SKU, p.Barcode, p.CategoryID, p.SubcategoryID, p.BrandID, p.UnitID, p.PurchasePrice, p.SellingPrice, p.WholesalePrice, p.MinimumStock, p.TaxRate, p.TrackExpiry, p.Description, p.ImageURL, p.IsActive, id, c.Tenant, measurement, p.DecimalPrecision, minimum, step, p.TareWeight, strings.TrimSpace(p.PLUCode))
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if e = saveProductUnits(r.Context(), tx, c.Tenant, id, p.AllowedUnits); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		prices := []struct {
			kind     string
			old, new float64
		}{{"purchase", oldPurchase, p.PurchasePrice}, {"selling", oldSelling, p.SellingPrice}, {"wholesale", oldWholesale, p.WholesalePrice}}
		for _, x := range prices {
			if x.old != x.new {
				_, e = tx.Exec(r.Context(), `INSERT INTO product_price_history(tenant_id,product_id,price_type,old_price,new_price,changed_by)VALUES($1,$2,$3,$4,$5,$6)`, c.Tenant, id, x.kind, x.old, x.new, c.Sub)
				if e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		auditUserAction(r, db, "PRODUCT_UPDATED", id, map[string]any{"name": p.Name})
		w.WriteHeader(204)
	}
}
func deleteOrArchiveProduct(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := r.PathValue("id")
		var used bool
		e := db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM sale_items WHERE product_id=$1 UNION ALL SELECT 1 FROM purchase_items WHERE product_id=$1 UNION ALL SELECT 1 FROM stock_movements WHERE product_id=$1 UNION ALL SELECT 1 FROM damaged_items WHERE product_id=$1 UNION ALL SELECT 1 FROM promotions WHERE product_id=$1)`, id).Scan(&used)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if used {
			tag, e := db.Exec(r.Context(), `UPDATE products SET is_active=false,archived_at=now(),updated_at=now() WHERE id=$1 AND tenant_id=$2`, id, c.Tenant)
			if e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
			if tag.RowsAffected() == 0 {
				http.Error(w, "product not found", 404)
				return
			}
			auditUserAction(r, db, "PRODUCT_ARCHIVED", id, map[string]any{})
			json.NewEncoder(w).Encode(map[string]string{"result": "archived"})
			return
		}
		tag, e := db.Exec(r.Context(), `DELETE FROM products WHERE id=$1 AND tenant_id=$2`, id, c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "product not found", 404)
			return
		}
		auditUserAction(r, db, "PRODUCT_DELETED", id, map[string]any{"permanent": true})
		json.NewEncoder(w).Encode(map[string]string{"result": "deleted"})
	}
}
func restoreProduct(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := r.PathValue("id")
		tag, e := db.Exec(r.Context(), `UPDATE products SET is_active=true,archived_at=NULL,updated_at=now() WHERE id=$1 AND tenant_id=$2 AND archived_at IS NOT NULL`, id, c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "archived product not found", 404)
			return
		}
		auditUserAction(r, db, "PRODUCT_RESTORED", id, map[string]any{})
		w.WriteHeader(204)
	}
}
func priceHistory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		rows, e := db.Query(r.Context(), `SELECT h.id,h.price_type,h.old_price,h.new_price,COALESCE(u.name,'System'),h.changed_at FROM product_price_history h LEFT JOIN users u ON u.id=h.changed_by WHERE h.product_id=$1 AND h.tenant_id=$2 ORDER BY h.changed_at DESC`, r.PathValue("id"), c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, kind, user string
			var oldPrice, newPrice float64
			var at any
			rows.Scan(&id, &kind, &oldPrice, &newPrice, &user, &at)
			out = append(out, map[string]any{"id": id, "type": kind, "oldPrice": oldPrice, "newPrice": newPrice, "changedBy": user, "changedAt": at})
		}
		json.NewEncoder(w).Encode(out)
	}
}
