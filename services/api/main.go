package main

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net/http"
	"os"
	"time"
)

type Product struct {
	ID, Name, ProductCode, SKU, Barcode, CategoryID, Category, SubcategoryID, Subcategory, BrandID, Brand, UnitID, Unit, Description, ImageURL string
	Price, Cost, WholesalePrice, Stock, MinimumStock, TaxRate                                                                                  float64
	TrackExpiry, IsActive                                                                                                                      bool
	PromotionID, PromotionName, PromotionKind                                                                                                  string
	PromotionValue, PromotionDiscount, EffectivePrice                                                                                          float64
	MeasurementType, PLUCode                                                                                                                   string
	DecimalPrecision                                                                                                                           int
	MinimumSaleQuantity, QuantityStep, TareWeight                                                                                              float64
	AllowedUnits                                                                                                                               []map[string]any
}
type SaleLine struct {
	ProductID           string `json:"productId"`
	Quantity, UnitPrice float64
}
type Sale struct {
	CashierID string     `json:"cashierId"`
	Lines     []SaleLine `json:"lines"`
}

func main() {
	key := os.Getenv("JWT_SECRET")
	if key == "" {
		log.Fatal("JWT_SECRET environment variable is required")
	}
	jwtSecret = []byte(key)
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://grocerly:grocerly@localhost:5432/grocerly?sslmode=disable"
	}
	db, e := pgxpool.New(context.Background(), url)
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", jsonAPI(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	mux.HandleFunc("POST /auth/login", jsonAPI(login(db)))
	mux.HandleFunc("POST /auth/logout", jsonAPI(authenticated(db)(logout(db))))
	mux.HandleFunc("GET /products", jsonAPI(authenticated(db, allStaff...)(products(db))))
	mux.HandleFunc("POST /sales", jsonAPI(authenticated(db, salesRoles...)(sale(db))))
	registerCatalog(mux, db)
	registerContacts(mux, db)
	registerTaxonomy(mux, db)
	registerOperations(mux, db)
	registerProcurement(mux, db)
	registerReports(mux, db)
	registerPOS(mux, db)
	registerPOSWorkflow(mux, db)
	registerLedgers(mux, db)
	registerAdvancedOperations(mux, db)
	registerFinanceAccounting(mux, db)
	registerReportExports(mux, db)
	registerUsers(mux, db)
	registerRoles(mux, db)
	registerSettings(mux, db)
	registerNotifications(mux, db)
	registerSync(mux, db)
	registerDataImports(mux, db)
	registerBackups(mux, db)
	startBackupScheduler(db)
	log.Println("api listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", logRequests(mux)))
}

type logWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *logWriter) WriteHeader(status int) { w.status = status; w.ResponseWriter.WriteHeader(status) }
func (w *logWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, e := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, e
}

// logRequests records one line per request (method, path, status, duration) and
// flags 5xx responses so server-side failures are visible in `docker compose logs`.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &logWriter{ResponseWriter: w}
		next.ServeHTTP(lw, r)
		if lw.status == 0 {
			lw.status = 200
		}
		line := "%s %s -> %d (%s, %dB)"
		args := []any{r.Method, r.URL.Path, lw.status, time.Since(start).Round(time.Millisecond), lw.bytes}
		if lw.status >= 500 {
			log.Printf("ERROR "+line, args...)
		} else if r.Method != http.MethodOptions {
			log.Printf(line, args...)
		}
	})
}
func jsonAPI(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", os.Getenv("WEB_ORIGIN"))
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}
func products(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includeArchived := r.URL.Query().Get("includeArchived") == "true"
		rows, e := db.Query(r.Context(), `SELECT p.id,p.name,COALESCE(p.product_code,''),p.sku,COALESCE(p.barcode,''),COALESCE(p.category_id::text,''),COALESCE(c.name,''),COALESCE(p.subcategory_id::text,''),COALESCE(sc.name,''),COALESCE(p.brand_id::text,''),COALESCE(b.name,''),COALESCE(p.unit_id::text,''),COALESCE(u.symbol,''),COALESCE(p.description,''),COALESCE(p.image_url,''),p.selling_price,p.purchase_price,p.wholesale_price,p.stock_quantity,p.minimum_stock,p.tax_rate,p.track_expiry,p.is_active,COALESCE(promo.id::text,''),COALESCE(promo.name,''),COALESCE(promo.kind,''),COALESCE(promo.value,0),COALESCE(promo.discount,0),p.selling_price-COALESCE(promo.discount,0),p.measurement_type,p.decimal_precision,p.minimum_sale_quantity,p.quantity_step,p.tare_weight,COALESCE(p.plu_code,'')
		FROM products p LEFT JOIN categories c ON c.id=p.category_id LEFT JOIN subcategories sc ON sc.id=p.subcategory_id LEFT JOIN brands b ON b.id=p.brand_id LEFT JOIN units u ON u.id=p.unit_id
		LEFT JOIN LATERAL (SELECT pr.id,pr.name,pr.kind,pr.value,GREATEST(0,LEAST(p.selling_price,CASE pr.kind WHEN 'percentage' THEN p.selling_price*pr.value/100 WHEN 'fixed' THEN pr.value WHEN 'special_price' THEN p.selling_price-pr.value ELSE 0 END)) discount FROM promotions pr WHERE pr.tenant_id=p.tenant_id AND pr.is_active AND pr.archived_at IS NULL AND current_date BETWEEN pr.start_date AND pr.end_date AND (pr.product_id=p.id OR (pr.product_id IS NULL AND pr.category_id=p.category_id)) ORDER BY discount DESC,(pr.product_id IS NOT NULL) DESC,pr.created_at DESC LIMIT 1) promo ON true
		WHERE p.tenant_id=$1 AND (p.is_active OR $2) ORDER BY p.is_active DESC,p.name`, claimsFrom(r).Tenant, includeArchived)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []Product{}
		for rows.Next() {
			var p Product
			if e = rows.Scan(&p.ID, &p.Name, &p.ProductCode, &p.SKU, &p.Barcode, &p.CategoryID, &p.Category, &p.SubcategoryID, &p.Subcategory, &p.BrandID, &p.Brand, &p.UnitID, &p.Unit, &p.Description, &p.ImageURL, &p.Price, &p.Cost, &p.WholesalePrice, &p.Stock, &p.MinimumStock, &p.TaxRate, &p.TrackExpiry, &p.IsActive, &p.PromotionID, &p.PromotionName, &p.PromotionKind, &p.PromotionValue, &p.PromotionDiscount, &p.EffectivePrice, &p.MeasurementType, &p.DecimalPrecision, &p.MinimumSaleQuantity, &p.QuantityStep, &p.TareWeight, &p.PLUCode); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			p.AllowedUnits, _ = loadProductUnits(r.Context(), db, p.ID, claimsFrom(r).Tenant)
			out = append(out, p)
		}
		json.NewEncoder(w).Encode(out)
	}
}
func consumeFEFO(ctx context.Context, tx pgx.Tx, productID string, qty float64) error {
	rows, e := tx.Query(ctx, `SELECT id,available_qty FROM stock_batches WHERE product_id=$1 AND available_qty>0 ORDER BY expiry_date NULLS LAST,received_at FOR UPDATE`, productID)
	if e != nil {
		return e
	}
	defer rows.Close()
	remaining := qty
	for rows.Next() && remaining > 0 {
		var id string
		var available float64
		if e = rows.Scan(&id, &available); e != nil {
			return e
		}
		used := available
		if used > remaining {
			used = remaining
		}
		if _, e = tx.Exec(ctx, `UPDATE stock_batches SET available_qty=available_qty-$2 WHERE id=$1`, id, used); e != nil {
			return e
		}
		remaining -= used
	}
	return rows.Err()
}
func sale(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var s Sale
		if e := json.NewDecoder(r.Body).Decode(&s); e != nil || len(s.Lines) == 0 || s.CashierID == "" {
			http.Error(w, "cashier and lines required", 400)
			return
		}
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var total float64
		for _, x := range s.Lines {
			var stock float64
			e = tx.QueryRow(r.Context(), `SELECT stock_quantity FROM products WHERE id=$1 FOR UPDATE`, x.ProductID).Scan(&stock)
			if e != nil || x.Quantity <= 0 || stock < x.Quantity {
				http.Error(w, "insufficient stock", 409)
				return
			}
			total += x.Quantity * x.UnitPrice
		}
		var id, invoice string
		e = tx.QueryRow(r.Context(), `INSERT INTO sales(invoice_no,cashier_id,total,paid_amount,tenant_id) VALUES(next_invoice_no(),$1,$2,$2,$3) RETURNING id,invoice_no`, s.CashierID, total, claimsFrom(r).Tenant).Scan(&id, &invoice)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		for _, x := range s.Lines {
			_, e = tx.Exec(r.Context(), `INSERT INTO sale_items(sale_id,product_id,quantity,unit_price,total) VALUES($1,$2,$3,$4,$5)`, id, x.ProductID, x.Quantity, x.UnitPrice, x.Quantity*x.UnitPrice)
			if e == nil {
				e = consumeFEFO(r.Context(), tx, x.ProductID, x.Quantity)
			}
			if e == nil {
				_, e = tx.Exec(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1`, x.ProductID, x.Quantity)
			}
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		// This legacy endpoint takes the full amount in cash. It posts through
		// the same engine as the till so that no sale reaches the ledger by a
		// route of its own.
		c := claimsFrom(r)
		revenueLines, e2 := saleRevenueLines(saleAmounts{
			Gross:   total,
			Tenders: []tenderLeg{{Role: roleCashOnHand, Amount: total, Memo: "cash"}},
		})
		if e2 != nil {
			http.Error(w, e2.Error(), 500)
			return
		}
		if _, _, e2 = postJournalTx(r.Context(), tx, journalDraft{
			Tenant: c.Tenant, Branch: c.Branch, Actor: c.Sub,
			ReferenceType: "sale", ReferenceID: id,
			Description:     "Sale " + invoice,
			ClientRequestID: "sale:" + id,
			Lines:           revenueLines,
		}); e2 != nil {
			http.Error(w, e2.Error(), 500)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "total": total})
	}
}
