package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type taxonomyInput struct {
	Name       string `json:"name"`
	Symbol     string `json:"symbol"`
	CategoryID string `json:"categoryId"`
}
type taxonomyConfig struct{ table, parent, column string }

func taxonomyKind(kind string) (taxonomyConfig, bool) {
	configs := map[string]taxonomyConfig{"categories": {"categories", "", "category_id"}, "subcategories": {"subcategories", "category_id", "subcategory_id"}, "brands": {"brands", "", "brand_id"}, "units": {"units", "", "unit_id"}}
	x, ok := configs[kind]
	return x, ok
}

func registerTaxonomy(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /catalog-settings/{kind}", jsonAPI(authenticated(db, stockRoles...)(listTaxonomy(db))))
	m.HandleFunc("POST /catalog-settings/{kind}", jsonAPI(authenticated(db, stockRoles...)(createTaxonomy(db))))
	m.HandleFunc("PUT /catalog-settings/{kind}/{id}", jsonAPI(authenticated(db, stockRoles...)(updateTaxonomy(db))))
	m.HandleFunc("DELETE /catalog-settings/{kind}/{id}", jsonAPI(authenticated(db, stockRoles...)(deleteTaxonomy(db))))
	m.HandleFunc("POST /catalog-settings/{kind}/{id}/status", jsonAPI(authenticated(db, stockRoles...)(taxonomyStatus(db))))
	m.HandleFunc("GET /barcodes/generate", jsonAPI(authenticated(db, stockRoles...)(generateBarcode(db))))
	m.HandleFunc("GET /barcodes/search", jsonAPI(authenticated(db, stockRoles...)(searchBarcodes(db))))
}

func listTaxonomy(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := taxonomyKind(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid catalogue type", 404)
			return
		}
		var query string
		switch c.table {
		case "subcategories":
			query = `SELECT x.id,x.name,x.is_active,COALESCE(x.category_id::text,''),COALESCE(c.name,''),(SELECT count(*) FROM products p WHERE p.subcategory_id=x.id) FROM subcategories x LEFT JOIN categories c ON c.id=x.category_id WHERE x.tenant_id=$1 ORDER BY x.name`
		case "units":
			query = `SELECT x.id,x.name,x.is_active,'',x.symbol,(SELECT count(*) FROM products p WHERE p.unit_id=x.id) FROM units x WHERE x.tenant_id=$1 ORDER BY x.name`
		default:
			query = fmt.Sprintf(`SELECT x.id,x.name,x.is_active,'','',(SELECT count(*) FROM products p WHERE p.%s=x.id) FROM %s x WHERE x.tenant_id=$1 ORDER BY x.name`, c.column, c.table)
		}
		rows, e := db.Query(r.Context(), query, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, parentID, detail string
			var active bool
			var count int
			if e = rows.Scan(&id, &name, &active, &parentID, &detail, &count); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			out = append(out, map[string]any{"id": id, "name": name, "isActive": active, "categoryId": parentID, "detail": detail, "productCount": count})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func createTaxonomy(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := taxonomyKind(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid catalogue type", 404)
			return
		}
		var x taxonomyInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" {
			http.Error(w, "name required", 400)
			return
		}
		var id string
		var e error
		switch c.table {
		case "subcategories":
			if x.CategoryID == "" {
				http.Error(w, "parent category required", 400)
				return
			}
			e = db.QueryRow(r.Context(), `INSERT INTO subcategories(tenant_id,category_id,name)VALUES($1,$2,$3)RETURNING id`, claimsFrom(r).Tenant, x.CategoryID, strings.TrimSpace(x.Name)).Scan(&id)
		case "units":
			if strings.TrimSpace(x.Symbol) == "" {
				http.Error(w, "unit symbol required", 400)
				return
			}
			e = db.QueryRow(r.Context(), `INSERT INTO units(tenant_id,name,symbol)VALUES($1,$2,$3)RETURNING id`, claimsFrom(r).Tenant, strings.TrimSpace(x.Name), strings.TrimSpace(x.Symbol)).Scan(&id)
		default:
			e = db.QueryRow(r.Context(), fmt.Sprintf(`INSERT INTO %s(tenant_id,name)VALUES($1,$2)RETURNING id`, c.table), claimsFrom(r).Tenant, strings.TrimSpace(x.Name)).Scan(&id)
		}
		if e != nil {
			http.Error(w, "name already exists or details are invalid", 409)
			return
		}
		auditUserAction(r, db, "CATALOGUE_ITEM_CREATED", id, map[string]any{"type": c.table, "name": x.Name})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}

func updateTaxonomy(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := taxonomyKind(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid catalogue type", 404)
			return
		}
		var x taxonomyInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" {
			http.Error(w, "name required", 400)
			return
		}
		var query string
		var args []any
		switch c.table {
		case "subcategories":
			if x.CategoryID == "" {
				http.Error(w, "parent category required", 400)
				return
			}
			query = `UPDATE subcategories SET name=$1,category_id=$2 WHERE id=$3 AND tenant_id=$4`
			args = []any{strings.TrimSpace(x.Name), x.CategoryID, r.PathValue("id"), claimsFrom(r).Tenant}
		case "units":
			if strings.TrimSpace(x.Symbol) == "" {
				http.Error(w, "unit symbol required", 400)
				return
			}
			query = `UPDATE units SET name=$1,symbol=$2 WHERE id=$3 AND tenant_id=$4`
			args = []any{strings.TrimSpace(x.Name), strings.TrimSpace(x.Symbol), r.PathValue("id"), claimsFrom(r).Tenant}
		default:
			query = fmt.Sprintf(`UPDATE %s SET name=$1 WHERE id=$2 AND tenant_id=$3`, c.table)
			args = []any{strings.TrimSpace(x.Name), r.PathValue("id"), claimsFrom(r).Tenant}
		}
		tag, e := db.Exec(r.Context(), query, args...)
		if e != nil {
			http.Error(w, "name already exists or details are invalid", 409)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "item not found", 404)
			return
		}
		auditUserAction(r, db, "CATALOGUE_ITEM_UPDATED", r.PathValue("id"), map[string]any{"type": c.table})
		w.WriteHeader(204)
	}
}

func deleteTaxonomy(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := taxonomyKind(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid catalogue type", 404)
			return
		}
		id := r.PathValue("id")
		var linked int
		e := db.QueryRow(r.Context(), fmt.Sprintf(`SELECT count(*) FROM products WHERE %s=$1`, c.column), id).Scan(&linked)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if linked > 0 {
			http.Error(w, "delete blocked: products are assigned to this item; deactivate it instead", 409)
			return
		}
		if c.table == "categories" {
			var children int
			db.QueryRow(r.Context(), `SELECT count(*) FROM subcategories WHERE category_id=$1`, id).Scan(&children)
			if children > 0 {
				http.Error(w, "delete blocked: subcategories are assigned to this category", 409)
				return
			}
		}
		tag, e := db.Exec(r.Context(), fmt.Sprintf(`DELETE FROM %s WHERE id=$1 AND tenant_id=$2`, c.table), id, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, "delete blocked because this item is in use", 409)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "item not found", 404)
			return
		}
		auditUserAction(r, db, "CATALOGUE_ITEM_DELETED", id, map[string]any{"type": c.table})
		w.WriteHeader(204)
	}
}

func taxonomyStatus(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := taxonomyKind(r.PathValue("kind"))
		if !ok {
			http.Error(w, "invalid catalogue type", 404)
			return
		}
		var x struct {
			IsActive bool `json:"isActive"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil {
			http.Error(w, "invalid status", 400)
			return
		}
		tag, e := db.Exec(r.Context(), fmt.Sprintf(`UPDATE %s SET is_active=$1 WHERE id=$2 AND tenant_id=$3`, c.table), x.IsActive, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "item not found", 404)
			return
		}
		w.WriteHeader(204)
	}
}

func generateBarcode(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 10; i++ {
			b := make([]byte, 6)
			rand.Read(b)
			code := fmt.Sprintf("29%010d", uint64(b[0])<<40|uint64(b[1])<<32|uint64(b[2])<<24|uint64(b[3])<<16|uint64(b[4])<<8|uint64(b[5]))
			var exists bool
			db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM products WHERE tenant_id=$1 AND barcode=$2)`, claimsFrom(r).Tenant, code).Scan(&exists)
			if !exists {
				json.NewEncoder(w).Encode(map[string]string{"barcode": code})
				return
			}
		}
		http.Error(w, "could not generate unique barcode", 500)
	}
}

func searchBarcodes(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		rows, e := db.Query(r.Context(), `SELECT id,name,COALESCE(product_code,''),sku,COALESCE(barcode,''),selling_price,is_active FROM products WHERE tenant_id=$1 AND (barcode ILIKE '%'||$2||'%' OR name ILIKE '%'||$2||'%' OR sku ILIKE '%'||$2||'%') ORDER BY name LIMIT 100`, claimsFrom(r).Tenant, q)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, code, sku, barcode string
			var price float64
			var active bool
			rows.Scan(&id, &name, &code, &sku, &barcode, &price, &active)
			out = append(out, map[string]any{"id": id, "name": name, "productCode": code, "sku": sku, "barcode": barcode, "price": price, "isActive": active})
		}
		json.NewEncoder(w).Encode(out)
	}
}
