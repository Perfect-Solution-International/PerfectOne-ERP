package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

type supplierProductInput struct {
	ProductID               string  `json:"productId"`
	SupplierProductCode     string  `json:"supplierProductCode"`
	SupplierSKU             string  `json:"supplierSku"`
	DefaultPurchasePrice    float64 `json:"defaultPurchasePrice"`
	RecommendedSellingPrice float64 `json:"recommendedSellingPrice"`
	WholesalePrice          float64 `json:"wholesalePrice"`
	MinimumOrderQty         float64 `json:"minimumOrderQty"`
	PackSize                float64 `json:"packSize"`
	LeadTimeDays            int     `json:"leadTimeDays"`
	IsPreferred             bool    `json:"isPreferred"`
	IsActive                bool    `json:"isActive"`
}

func registerSupplierCatalogue(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /procurement/suppliers/{id}/products", jsonAPI(authenticated(db, stockRoles...)(listSupplierProducts(db))))
	m.HandleFunc("POST /procurement/suppliers/{id}/products", jsonAPI(authenticated(db, stockRoles...)(saveSupplierProduct(db))))
	m.HandleFunc("PUT /procurement/suppliers/{id}/products/{productId}", jsonAPI(authenticated(db, stockRoles...)(saveSupplierProduct(db))))
	m.HandleFunc("DELETE /procurement/suppliers/{id}/products/{productId}", jsonAPI(authenticated(db, stockRoles...)(deactivateSupplierProduct(db))))
	m.HandleFunc("GET /procurement/suppliers/{id}/products/{productId}/prices", jsonAPI(authenticated(db, stockRoles...)(supplierPriceHistory(db))))
	m.HandleFunc("POST /procurement/catalog-grns", jsonAPI(authenticated(db, stockRoles...)(secureSaveCatalogueGRN(db))))
	m.HandleFunc("PUT /procurement/catalog-grns/{id}", jsonAPI(authenticated(db, stockRoles...)(secureUpdateCatalogueGRN(db))))
	m.HandleFunc("POST /procurement/catalog-grns/{id}/finalize", jsonAPI(authenticated(db, stockRoles...)(secureFinalizeCatalogueGRN(db))))
	m.HandleFunc("POST /procurement/catalog-grns/{id}/reverse", jsonAPI(authenticated(db, stockRoles...)(secureReverseCatalogueGRN(db))))
	m.HandleFunc("POST /procurement/catalog-grns/{id}/cancel", jsonAPI(authenticated(db, stockRoles...)(secureCancelCatalogueGRN(db))))
	m.HandleFunc("GET /procurement/catalog-grns/{id}/payments", jsonAPI(authenticated(db, stockRoles...)(grnPaymentHistory(db))))
	m.HandleFunc("GET /procurement/suppliers/{id}/open-grns", jsonAPI(authenticated(db, stockRoles...)(supplierOpenGRNs(db))))
	m.HandleFunc("GET /purchase-orders/{id}/receipts", jsonAPI(authenticated(db, stockRoles...)(purchaseOrderReceipts(db))))
}

func listSupplierProducts(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		rows, e := db.Query(r.Context(), `SELECT sp.product_id,p.name,p.sku,COALESCE(p.barcode,''),p.stock_quantity,p.minimum_stock,p.track_expiry,p.purchase_price,p.selling_price,p.wholesale_price,COALESCE(sp.supplier_product_code,''),COALESCE(sp.supplier_sku,''),sp.default_purchase_price,sp.last_purchase_price,sp.recommended_selling_price,sp.wholesale_price,sp.minimum_order_qty,sp.pack_size,sp.lead_time_days,sp.is_preferred,sp.last_purchased_at FROM supplier_products sp JOIN products p ON p.id=sp.product_id WHERE sp.tenant_id=$1 AND sp.supplier_id=$2 AND sp.is_active AND p.is_active AND ($3='' OR p.name ILIKE '%'||$3||'%' OR p.sku ILIKE '%'||$3||'%' OR COALESCE(p.barcode,'') ILIKE '%'||$3||'%' OR COALESCE(sp.supplier_product_code,'') ILIKE '%'||$3||'%') ORDER BY sp.is_preferred DESC,p.name`, c.Tenant, r.PathValue("id"), q)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var productID, name, sku, barcode, code, supplierSKU string
			var stock, minStock, purchase, selling, wholesale, defaultPurchase, lastPurchase, recommended, supplierWholesale, moq, pack float64
			var track, preferred bool
			var lead int
			var last any
			if e = rows.Scan(&productID, &name, &sku, &barcode, &stock, &minStock, &track, &purchase, &selling, &wholesale, &code, &supplierSKU, &defaultPurchase, &lastPurchase, &recommended, &supplierWholesale, &moq, &pack, &lead, &preferred, &last); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			allowed, _ := loadProductUnits(r.Context(), db, productID, c.Tenant)
			var baseUnit string
			_ = db.QueryRow(r.Context(), `SELECT COALESCE(u.symbol,'unit') FROM products p LEFT JOIN units u ON u.id=p.unit_id WHERE p.id=$1 AND p.tenant_id=$2`, productID, c.Tenant).Scan(&baseUnit)
			out = append(out, map[string]any{"productId": productID, "name": name, "sku": sku, "barcode": barcode, "stock": stock, "minimumStock": minStock, "trackExpiry": track, "purchasePrice": purchase, "sellingPrice": selling, "productWholesalePrice": wholesale, "supplierProductCode": code, "supplierSku": supplierSKU, "defaultPurchasePrice": defaultPurchase, "lastPurchasePrice": lastPurchase, "recommendedSellingPrice": recommended, "wholesalePrice": supplierWholesale, "minimumOrderQty": moq, "packSize": pack, "leadTimeDays": lead, "isPreferred": preferred, "lastPurchasedAt": last, "baseUnit": baseUnit, "allowedUnits": allowed})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func saveSupplierProduct(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x supplierProductInput
		if json.NewDecoder(r.Body).Decode(&x) != nil {
			http.Error(w, "invalid supplier product", 400)
			return
		}
		if r.PathValue("productId") != "" {
			x.ProductID = r.PathValue("productId")
		}
		if x.ProductID == "" || x.DefaultPurchasePrice < 0 || x.RecommendedSellingPrice < 0 || x.WholesalePrice < 0 || x.MinimumOrderQty <= 0 || x.PackSize <= 0 || x.LeadTimeDays < 0 {
			http.Error(w, "valid product, prices, MOQ and pack size required", 400)
			return
		}
		c := claimsFrom(r)
		_, e := db.Exec(r.Context(), `INSERT INTO supplier_products(tenant_id,supplier_id,product_id,supplier_product_code,supplier_sku,default_purchase_price,recommended_selling_price,wholesale_price,minimum_order_qty,pack_size,lead_time_days,is_preferred,is_active) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,true) ON CONFLICT(tenant_id,supplier_id,product_id) DO UPDATE SET supplier_product_code=EXCLUDED.supplier_product_code,supplier_sku=EXCLUDED.supplier_sku,default_purchase_price=EXCLUDED.default_purchase_price,recommended_selling_price=EXCLUDED.recommended_selling_price,wholesale_price=EXCLUDED.wholesale_price,minimum_order_qty=EXCLUDED.minimum_order_qty,pack_size=EXCLUDED.pack_size,lead_time_days=EXCLUDED.lead_time_days,is_preferred=EXCLUDED.is_preferred,is_active=true,updated_at=now()`, c.Tenant, r.PathValue("id"), x.ProductID, x.SupplierProductCode, x.SupplierSKU, x.DefaultPurchasePrice, x.RecommendedSellingPrice, x.WholesalePrice, x.MinimumOrderQty, x.PackSize, x.LeadTimeDays, x.IsPreferred)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		w.WriteHeader(204)
	}
}

func deactivateSupplierProduct(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag, e := db.Exec(r.Context(), `UPDATE supplier_products SET is_active=false,updated_at=now() WHERE tenant_id=$1 AND supplier_id=$2 AND product_id=$3`, claimsFrom(r).Tenant, r.PathValue("id"), r.PathValue("productId"))
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "supplier product not found", 404)
			return
		}
		w.WriteHeader(204)
	}
}

func supplierPriceHistory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT h.old_purchase_price,h.new_purchase_price,h.selling_price,h.wholesale_price,COALESCE(u.name,'System'),h.changed_at FROM supplier_product_price_history h LEFT JOIN users u ON u.id=h.changed_by WHERE h.tenant_id=$1 AND h.supplier_id=$2 AND h.product_id=$3 ORDER BY h.changed_at DESC LIMIT 10`, claimsFrom(r).Tenant, r.PathValue("id"), r.PathValue("productId"))
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var oldPrice, newPrice, selling, wholesale any
			var user string
			var at any
			rows.Scan(&oldPrice, &newPrice, &selling, &wholesale, &user, &at)
			out = append(out, map[string]any{"oldPurchasePrice": oldPrice, "newPurchasePrice": newPrice, "sellingPrice": selling, "wholesalePrice": wholesale, "changedBy": user, "changedAt": at})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func saveCatalogueGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x grnInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.SupplierID == "" || strings.TrimSpace(x.InvoiceNo) == "" || len(x.Items) == 0 {
			http.Error(w, "invoice, supplier and products required", 400)
			return
		}
		seen := map[string]bool{}
		total := 0.0
		for _, l := range x.Items {
			if l.ProductID == "" || l.Quantity <= 0 || l.FreeQuantity < 0 || l.UnitCost < 0 || l.SellingPrice < 0 || l.WholesalePrice < 0 || l.Discount < 0 || l.Tax < 0 {
				http.Error(w, "quantities and prices must be valid", 400)
				return
			}
			key := l.ProductID + "|" + l.BatchNo
			if seen[key] {
				http.Error(w, "duplicate product and batch; merge the quantities into one line", 409)
				return
			}
			seen[key] = true
			if l.ExpiryDate != "" && l.ExpiryDate < x.PurchaseDate {
				http.Error(w, "expiry date cannot be before purchase date", 400)
				return
			}
			total += l.Quantity*l.UnitCost - l.Discount + l.Tax
		}
		total = total - x.Discount + x.Tax
		if total < 0 || x.PaidAmount < 0 || x.PaidAmount > total {
			http.Error(w, "paid amount cannot exceed purchase total", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var id string
		e = tx.QueryRow(r.Context(), `INSERT INTO purchases(invoice_no,supplier_id,total,paid_amount,tenant_id,purchase_date,discount,tax,payment_method,status,notes) VALUES($1,$2,$3,$4,$5,COALESCE(NULLIF($6,'')::date,current_date),$7,$8,COALESCE(NULLIF($9,''),'credit'),'draft',$10) RETURNING id`, strings.TrimSpace(x.InvoiceNo), x.SupplierID, total, x.PaidAmount, c.Tenant, x.PurchaseDate, x.Discount, x.Tax, x.PaymentMethod, x.Notes).Scan(&id)
		if e != nil {
			http.Error(w, "duplicate supplier invoice number or invalid purchase", 409)
			return
		}
		for _, l := range x.Items {
			var track bool
			e = tx.QueryRow(r.Context(), `SELECT track_expiry FROM products WHERE id=$1 AND tenant_id=$2 AND is_active`, l.ProductID, c.Tenant).Scan(&track)
			if e != nil {
				http.Error(w, "product not found", 404)
				return
			}
			if track && l.ExpiryDate == "" {
				http.Error(w, "expiry date is required for expiry-tracked products", 400)
				return
			}
			var assigned bool
			e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM supplier_products WHERE tenant_id=$1 AND supplier_id=$2 AND product_id=$3 AND is_active)`, c.Tenant, x.SupplierID, l.ProductID).Scan(&assigned)
			if e != nil || !assigned {
				http.Error(w, "all products must be assigned to the selected supplier", 409)
				return
			}
			_, e = tx.Exec(r.Context(), `INSERT INTO purchase_items(purchase_id,product_id,quantity,free_quantity,unit_cost,selling_price,wholesale_price,manufactured_date,expiry_date,total,discount,tax,batch_no,update_purchase_price,update_selling_price) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,NULLIF($9,'')::date,$3::numeric*$5::numeric-$10::numeric+$11::numeric,$10,$11,NULLIF($12,''),$13,$14)`, id, l.ProductID, l.Quantity, l.FreeQuantity, l.UnitCost, l.SellingPrice, l.WholesalePrice, l.ManufacturedDate, l.ExpiryDate, l.Discount, l.Tax, l.BatchNo, l.UpdatePurchasePrice, l.UpdateSellingPrice)
			if e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, "purchase could not be saved", 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}

func finalizeCatalogueGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := r.PathValue("id")
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var supplier, status string
		var total, paid float64
		e = tx.QueryRow(r.Context(), `SELECT supplier_id,status,total,paid_amount FROM purchases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, c.Tenant).Scan(&supplier, &status, &total, &paid)
		if e != nil || status != "draft" {
			http.Error(w, "draft GRN not found", 409)
			return
		}
		rows, e := tx.Query(r.Context(), `SELECT product_id,quantity,free_quantity,unit_cost,selling_price,wholesale_price,COALESCE(batch_no,''),COALESCE(expiry_date::text,''),update_purchase_price,update_selling_price FROM purchase_items WHERE purchase_id=$1`, id)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		type item struct {
			product, batch, expiry              string
			qty, free, cost, selling, wholesale float64
			updateCost, updateSale              bool
		}
		items := []item{}
		for rows.Next() {
			var l item
			if e = rows.Scan(&l.product, &l.qty, &l.free, &l.cost, &l.selling, &l.wholesale, &l.batch, &l.expiry, &l.updateCost, &l.updateSale); e != nil {
				rows.Close()
				http.Error(w, e.Error(), 500)
				return
			}
			items = append(items, l)
		}
		rows.Close()
		for _, l := range items {
			stockQty := l.qty + l.free
			var balance, oldCost float64
			e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2 WHERE id=$1 AND tenant_id=$3 RETURNING stock_quantity,purchase_price`, l.product, stockQty, c.Tenant).Scan(&balance, &oldCost)
			if e != nil {
				http.Error(w, "product stock update failed", 500)
				return
			}
			_, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'purchase',$2,$3,NULLIF($4,'')::uuid,'purchase',$5,$6,$7,$8)`, l.product, stockQty, c.Tenant, c.Branch, id, balance, "Purchased quantity including free items", c.Sub)
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			_, e = tx.Exec(r.Context(), `INSERT INTO stock_batches(product_id,batch_no,expiry_date,available_qty,unit_cost) VALUES($1,NULLIF($2,''),NULLIF($3,'')::date,$4,$5)`, l.product, l.batch, l.expiry, stockQty, l.cost)
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			_, e = tx.Exec(r.Context(), `INSERT INTO supplier_product_price_history(tenant_id,supplier_id,product_id,purchase_id,old_purchase_price,new_purchase_price,selling_price,wholesale_price,changed_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, c.Tenant, supplier, l.product, id, oldCost, l.cost, l.selling, l.wholesale, c.Sub)
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			_, e = tx.Exec(r.Context(), `UPDATE supplier_products SET last_purchase_price=$4,default_purchase_price=CASE WHEN $5 THEN $4 ELSE default_purchase_price END,recommended_selling_price=CASE WHEN $6 THEN $7 ELSE recommended_selling_price END,wholesale_price=CASE WHEN $6 THEN $8 ELSE wholesale_price END,last_purchased_at=now(),updated_at=now() WHERE tenant_id=$1 AND supplier_id=$2 AND product_id=$3`, c.Tenant, supplier, l.product, l.cost, l.updateCost, l.updateSale, l.selling, l.wholesale)
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			if l.updateCost || l.updateSale {
				_, e = tx.Exec(r.Context(), `UPDATE products SET purchase_price=CASE WHEN $3 THEN $4 ELSE purchase_price END,selling_price=CASE WHEN $5 THEN $6 ELSE selling_price END,wholesale_price=CASE WHEN $5 THEN $7 ELSE wholesale_price END,updated_at=now() WHERE id=$1 AND tenant_id=$2`, l.product, c.Tenant, l.updateCost, l.cost, l.updateSale, l.selling, l.wholesale)
				if e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
			}
		}
		payable := total - paid
		if _, e = tx.Exec(r.Context(), `UPDATE suppliers SET balance=balance+$2 WHERE id=$1 AND tenant_id=$3`, supplier, payable, c.Tenant); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO supplier_ledger(supplier_id,entry_type,reference_id,debit)VALUES($1,'purchase_finalize',$2,$3)`, supplier, id, payable); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if _, e = tx.Exec(r.Context(), `UPDATE purchases SET status='finalized',finalized_by=$2 WHERE id=$1`, id, c.Sub); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(204)
	}
}

func reverseCatalogueGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		if strings.TrimSpace(input.Reason) == "" {
			http.Error(w, "reversal reason required", 400)
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
		var supplier, status string
		var total, paid float64
		e = tx.QueryRow(r.Context(), `SELECT supplier_id,status,total,paid_amount FROM purchases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, c.Tenant).Scan(&supplier, &status, &total, &paid)
		if e != nil || status != "finalized" {
			http.Error(w, "only finalized GRNs can be reversed", 409)
			return
		}
		rows, e := tx.Query(r.Context(), `SELECT product_id,quantity+free_quantity,COALESCE(batch_no,'') FROM purchase_items WHERE purchase_id=$1`, id)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		type item struct {
			product, batch string
			qty            float64
		}
		items := []item{}
		for rows.Next() {
			var l item
			if e = rows.Scan(&l.product, &l.qty, &l.batch); e != nil {
				rows.Close()
				http.Error(w, e.Error(), 500)
				return
			}
			items = append(items, l)
		}
		rows.Close()
		for _, l := range items {
			var balance float64
			e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND tenant_id=$3 AND stock_quantity>=$2 RETURNING stock_quantity`, l.product, l.qty, c.Tenant).Scan(&balance)
			if e != nil {
				http.Error(w, "insufficient stock to reverse this GRN", 409)
				return
			}
			if _, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'purchase_reversal',$2,$3,NULLIF($4,'')::uuid,'purchase',$5,$6,$7,$8)`, l.product, -l.qty, c.Tenant, c.Branch, id, balance, input.Reason, c.Sub); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			if l.batch != "" {
				if _, e = tx.Exec(r.Context(), `UPDATE stock_batches SET available_qty=GREATEST(available_qty-$3,0) WHERE id=(SELECT id FROM stock_batches WHERE product_id=$1 AND batch_no=$2 ORDER BY id DESC LIMIT 1)`, l.product, l.batch, l.qty); e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
			}
		}
		payable := total - paid
		if _, e = tx.Exec(r.Context(), `UPDATE suppliers SET balance=GREATEST(balance-$2,0) WHERE id=$1 AND tenant_id=$3`, supplier, payable, c.Tenant); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO supplier_ledger(supplier_id,entry_type,reference_id,credit) VALUES($1,'purchase_reverse',$2,$3)`, supplier, id, payable); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if _, e = tx.Exec(r.Context(), `UPDATE purchases SET status='reversed',reversed_by=$2,reversed_at=now(),notes=concat_ws(E'\n',notes,'Reversed: '||$3) WHERE id=$1`, id, c.Sub, input.Reason); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(204)
	}
}
