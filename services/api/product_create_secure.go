package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func secureCreateProduct(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p ProductInput
		if json.NewDecoder(r.Body).Decode(&p) != nil {
			http.Error(w, "invalid product", http.StatusBadRequest)
			return
		}
		if message := validateProduct(p); message != "" {
			http.Error(w, message, http.StatusBadRequest)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())

		// Tenant-bound lookups stop IDs from another tenant being assigned.
		checks := []struct{ table, id string }{{"categories", p.CategoryID}, {"subcategories", p.SubcategoryID}, {"brands", p.BrandID}, {"units", p.UnitID}}
		for _, check := range checks {
			if check.id == "" {
				continue
			}
			var exists bool
			if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM `+check.table+` WHERE id=$1 AND tenant_id=$2 AND is_active)`, check.id, c.Tenant).Scan(&exists); err != nil || !exists {
				http.Error(w, "invalid product classification", http.StatusBadRequest)
				return
			}
		}

		var id string
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
		err = tx.QueryRow(r.Context(), `INSERT INTO products(name,product_code,sku,barcode,category_id,subcategory_id,brand_id,unit_id,purchase_price,selling_price,wholesale_price,stock_quantity,minimum_stock,tax_rate,track_expiry,description,image_url,is_active,tenant_id,measurement_type,decimal_precision,minimum_sale_quantity,quantity_step,tare_weight,plu_code)
			VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,$9,$10,$11,$12,$13,$14,$15,$16,NULLIF($17,''),$18,$19,$20,$21,$22,$23,$24,NULLIF($25,'')) RETURNING id`,
			strings.TrimSpace(p.Name), strings.TrimSpace(p.ProductCode), strings.TrimSpace(p.SKU), strings.TrimSpace(p.Barcode), p.CategoryID, p.SubcategoryID, p.BrandID, p.UnitID,
			p.PurchasePrice, p.SellingPrice, p.WholesalePrice, p.OpeningStock, p.MinimumStock, p.TaxRate, p.TrackExpiry, p.Description, p.ImageURL, p.IsActive, c.Tenant, measurement, p.DecimalPrecision, minimum, step, p.TareWeight, strings.TrimSpace(p.PLUCode)).Scan(&id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if p.OpeningStock > 0 {
			if err = adjustBranchStock(r.Context(), tx, c.Tenant, c.Branch, id, p.OpeningStock); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			_, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by)
				VALUES($1,'opening',$2,$3,NULLIF($4,'')::uuid,'product',$1,$2,'Opening stock',NULLIF($5,'')::uuid)`, id, p.OpeningStock, c.Tenant, c.Branch, c.Sub)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if err = saveProductUnits(r.Context(), tx, c.Tenant, id, p.AllowedUnits); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		auditUserAction(r, db, "PRODUCT_CREATED", id, map[string]any{"name": p.Name, "sku": p.SKU, "openingStock": p.OpeningStock})
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
