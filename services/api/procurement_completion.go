package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// secureUpdateCatalogueGRN edits only a direct, unposted GRN. PO receipts are
// deliberately cancelled and recreated because their line reservations must
// remain tied to an exact purchase_order_item_id.
func secureUpdateCatalogueGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input secureGRNInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.SupplierID == "" || strings.TrimSpace(input.InvoiceNo) == "" || len(input.Items) == 0 {
			http.Error(w, "invoice, supplier and product lines are required", http.StatusBadRequest)
			return
		}
		if input.PaymentMethod == "" {
			input.PaymentMethod = "credit"
		}
		if input.PaymentMethod != "credit" && input.PaymentMethod != "cash" && input.PaymentMethod != "bank" {
			http.Error(w, "invalid payment method", http.StatusBadRequest)
			return
		}
		seen, total := map[string]bool{}, 0.0
		for _, line := range input.Items {
			if line.ProductID == "" || line.Quantity <= 0 || line.FreeQuantity < 0 || line.UnitCost < 0 || line.SellingPrice < 0 || line.WholesalePrice < 0 || line.Discount < 0 || line.Tax < 0 || line.Discount > line.Quantity*line.UnitCost+line.Tax {
				http.Error(w, "invalid purchase line", http.StatusBadRequest)
				return
			}
			key := line.ProductID + "|" + strings.ToLower(strings.TrimSpace(line.BatchNo)) + "|" + line.ExpiryDate
			if seen[key] {
				http.Error(w, "duplicate product and batch; use a different batch/expiry or merge the line", http.StatusConflict)
				return
			}
			seen[key] = true
			if line.ManufacturedDate != "" && line.ExpiryDate != "" && line.ManufacturedDate > line.ExpiryDate {
				http.Error(w, "manufactured date cannot be after expiry date", http.StatusBadRequest)
				return
			}
			if input.PurchaseDate != "" && line.ExpiryDate != "" && line.ExpiryDate < input.PurchaseDate {
				http.Error(w, "expiry date cannot be before purchase date", http.StatusBadRequest)
				return
			}
			total += line.Quantity*line.UnitCost - line.Discount + line.Tax
		}
		total = math.Round((total-input.Discount+input.Tax)*100) / 100
		if total < 0 || input.PaidAmount < 0 || input.PaidAmount > total || (input.PaidAmount > 0 && (input.PaymentMethod == "credit" || input.AccountID == "")) {
			http.Error(w, "invalid total or payment", http.StatusBadRequest)
			return
		}

		c, id := claimsFrom(r), r.PathValue("id")
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())
		var status string
		var poID *string
		if err = tx.QueryRow(r.Context(), `SELECT status,purchase_order_id::text FROM purchases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, c.Tenant).Scan(&status, &poID); err != nil || status != "draft" {
			http.Error(w, "only a draft GRN can be edited", http.StatusConflict)
			return
		}
		if poID != nil {
			http.Error(w, "cancel and recreate a PO receipt to preserve order-line reservations", http.StatusConflict)
			return
		}
		var supplierOK bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM suppliers WHERE id=$1 AND tenant_id=$2 AND is_active)`, input.SupplierID, c.Tenant).Scan(&supplierOK); err != nil || !supplierOK {
			http.Error(w, "active supplier not found", http.StatusNotFound)
			return
		}
		if input.PaidAmount > 0 {
			var kind string
			if err = tx.QueryRow(r.Context(), `SELECT account_type FROM cash_accounts WHERE id=$1 AND tenant_id=$2 AND is_active`, input.AccountID, c.Tenant).Scan(&kind); err != nil || kind != input.PaymentMethod {
				http.Error(w, "selected account does not match payment method", http.StatusConflict)
				return
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE purchases SET invoice_no=$1,supplier_id=$2,total=$3,paid_amount=$4,purchase_date=COALESCE(NULLIF($5,'')::date,current_date),discount=$6,tax=$7,payment_method=$8,account_id=NULLIF($9,'')::uuid,notes=NULLIF($10,''),updated_at=now() WHERE id=$11 AND tenant_id=$12 AND status='draft'`, strings.TrimSpace(input.InvoiceNo), input.SupplierID, total, input.PaidAmount, input.PurchaseDate, input.Discount, input.Tax, input.PaymentMethod, input.AccountID, strings.TrimSpace(input.Notes), id, c.Tenant); err != nil {
			http.Error(w, "supplier invoice already exists or GRN is invalid", http.StatusConflict)
			return
		}
		if _, err = tx.Exec(r.Context(), `DELETE FROM purchase_items WHERE purchase_id=$1`, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, line := range input.Items {
			var track, assigned bool
			err = tx.QueryRow(r.Context(), `SELECT p.track_expiry,EXISTS(SELECT 1 FROM supplier_products sp WHERE sp.tenant_id=$2 AND sp.supplier_id=$3 AND sp.product_id=p.id AND sp.is_active) FROM products p WHERE p.id=$1 AND p.tenant_id=$2 AND p.is_active`, line.ProductID, c.Tenant, input.SupplierID).Scan(&track, &assigned)
			if err != nil || !assigned {
				http.Error(w, "active supplier product assignment not found", http.StatusConflict)
				return
			}
			if track && (strings.TrimSpace(line.BatchNo) == "" || line.ExpiryDate == "") {
				http.Error(w, "batch number and expiry date are required for expiry-tracked products", http.StatusBadRequest)
				return
			}
			entered := line.EnteredQuantity
			if entered <= 0 {
				entered = line.Quantity
			}
			enteredCost := line.EnteredUnitCost
			if enteredCost <= 0 {
				enteredCost = line.UnitCost
			}
			factor := line.ConversionFactor
			if factor <= 0 {
				factor = 1
			}
			_, err = tx.Exec(r.Context(), `INSERT INTO purchase_items(purchase_id,product_id,quantity,free_quantity,unit_cost,selling_price,wholesale_price,manufactured_date,expiry_date,total,discount,tax,batch_no,update_purchase_price,update_selling_price,entered_quantity,entered_unit_id,entered_unit_cost,conversion_factor) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,NULLIF($9,'')::date,$3::numeric*$5::numeric-$10::numeric+$11::numeric,$10,$11,NULLIF($12,''),$13,$14,$15,NULLIF($16,'')::uuid,$17,$18)`, id, line.ProductID, line.Quantity, line.FreeQuantity, line.UnitCost, line.SellingPrice, line.WholesalePrice, line.ManufacturedDate, line.ExpiryDate, line.Discount, line.Tax, strings.TrimSpace(line.BatchNo), line.UpdatePurchasePrice, line.UpdateSellingPrice, entered, line.UnitID, enteredCost, factor)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		auditUserAction(r, db, "GRN_DRAFT_UPDATED", id, map[string]any{"total": total, "lineCount": len(input.Items)})
		json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "draft", "total": total})
	}
}

func purchaseOrderReceipts(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, poID := claimsFrom(r), r.PathValue("id")
		var exists bool
		if err := db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM purchase_orders WHERE id=$1 AND tenant_id=$2)`, poID, c.Tenant).Scan(&exists); err != nil || !exists {
			http.Error(w, "purchase order not found", http.StatusNotFound)
			return
		}
		rows, err := db.Query(r.Context(), `SELECT p.id,p.invoice_no,p.purchase_date::text,p.status,p.total,p.paid_amount,COALESCE((SELECT sum(a.amount) FROM purchase_payment_allocations a JOIN supplier_payments sp ON sp.id=a.supplier_payment_id WHERE a.purchase_id=p.id AND sp.status='finalized'),0),COALESCE(sum(pi.quantity),0),count(pi.id),p.created_at FROM purchases p LEFT JOIN purchase_items pi ON pi.purchase_id=p.id WHERE p.tenant_id=$1 AND p.purchase_order_id=$2 GROUP BY p.id ORDER BY p.created_at`, c.Tenant, poID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, invoice, date, status string
			var total, initialPaid, allocated, quantity float64
			var lineCount int
			var created any
			if err = rows.Scan(&id, &invoice, &date, &status, &total, &initialPaid, &allocated, &quantity, &lineCount, &created); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			out = append(out, map[string]any{"id": id, "invoice": invoice, "date": date, "status": status, "total": total, "paid": initialPaid + allocated, "due": math.Max(0, total-initialPaid-allocated), "receivedQuantity": quantity, "lineCount": lineCount, "createdAt": created})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func grnPaymentHistory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, purchaseID := claimsFrom(r), r.PathValue("id")
		var base map[string]any
		var id, invoice, method, status string
		var account *string
		var total, initialPaid float64
		var purchaseDate any
		if err := db.QueryRow(r.Context(), `SELECT id,invoice_no,payment_method,account_id::text,status,total,paid_amount,purchase_date FROM purchases WHERE id=$1 AND tenant_id=$2`, purchaseID, c.Tenant).Scan(&id, &invoice, &method, &account, &status, &total, &initialPaid, &purchaseDate); err != nil {
			http.Error(w, "GRN not found", http.StatusNotFound)
			return
		}
		base = map[string]any{"id": id, "invoice": invoice, "status": status, "total": total, "initialPaid": initialPaid, "method": method, "accountId": account, "date": purchaseDate}
		rows, err := db.Query(r.Context(), `SELECT sp.id,a.amount,sp.method,COALESCE(ca.name,''),sp.payment_date::text,COALESCE(sp.reference,''),sp.status,COALESCE(sp.reversal_reason,''),COALESCE(u.name,'System') FROM purchase_payment_allocations a JOIN supplier_payments sp ON sp.id=a.supplier_payment_id LEFT JOIN cash_accounts ca ON ca.id=sp.account_id LEFT JOIN users u ON u.id=sp.paid_by WHERE a.tenant_id=$1 AND a.purchase_id=$2 ORDER BY a.created_at`, c.Tenant, purchaseID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		payments, activeAllocated := []map[string]any{}, 0.0
		for rows.Next() {
			var paymentID, paymentMethod, accountName, date, reference, paymentStatus, reason, user string
			var amount float64
			if err = rows.Scan(&paymentID, &amount, &paymentMethod, &accountName, &date, &reference, &paymentStatus, &reason, &user); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if paymentStatus == "finalized" {
				activeAllocated += amount
			}
			payments = append(payments, map[string]any{"id": paymentID, "amount": amount, "method": paymentMethod, "account": accountName, "date": date, "reference": reference, "status": paymentStatus, "reason": reason, "user": user})
		}
		base["allocatedPaid"], base["paid"], base["due"], base["payments"] = activeAllocated, initialPaid+activeAllocated, math.Max(0, total-initialPaid-activeAllocated), payments
		json.NewEncoder(w).Encode(base)
	}
}

func supplierOpenGRNs(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(r.Context(), `SELECT p.id,p.invoice_no,p.purchase_date::text,p.total,p.paid_amount+COALESCE(sum(CASE WHEN sp.status='finalized' THEN a.amount ELSE 0 END),0),p.total-p.paid_amount-COALESCE(sum(CASE WHEN sp.status='finalized' THEN a.amount ELSE 0 END),0) FROM purchases p LEFT JOIN purchase_payment_allocations a ON a.purchase_id=p.id AND a.tenant_id=p.tenant_id LEFT JOIN supplier_payments sp ON sp.id=a.supplier_payment_id WHERE p.tenant_id=$1 AND p.supplier_id=$2 AND p.status='finalized' GROUP BY p.id HAVING p.total-p.paid_amount-COALESCE(sum(CASE WHEN sp.status='finalized' THEN a.amount ELSE 0 END),0)>0 ORDER BY p.purchase_date,p.created_at`, claimsFrom(r).Tenant, r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, invoice, date string
			var total, paid, due float64
			if rows.Scan(&id, &invoice, &date, &total, &paid, &due) == nil {
				out = append(out, map[string]any{"id": id, "invoice": invoice, "date": date, "total": total, "paid": paid, "due": due})
			}
		}
		json.NewEncoder(w).Encode(out)
	}
}
