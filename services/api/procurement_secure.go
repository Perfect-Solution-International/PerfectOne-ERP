package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type secureGRNInput struct {
	SupplierID      string            `json:"supplierId"`
	InvoiceNo       string            `json:"invoiceNo"`
	PurchaseDate    string            `json:"purchaseDate"`
	PaymentMethod   string            `json:"paymentMethod"`
	AccountID       string            `json:"accountId"`
	Notes           string            `json:"notes"`
	PurchaseOrderID string            `json:"purchaseOrderId"`
	RequestID       string            `json:"requestId"`
	PaidAmount      float64           `json:"paidAmount"`
	Discount        float64           `json:"discount"`
	Tax             float64           `json:"tax"`
	Items           []procurementLine `json:"items"`
}

func secureSaveCatalogueGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input secureGRNInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.SupplierID == "" || strings.TrimSpace(input.InvoiceNo) == "" || strings.TrimSpace(input.RequestID) == "" || len(input.Items) == 0 {
			http.Error(w, "invoice, supplier, products and request ID are required", 400)
			return
		}
		if input.PaymentMethod == "" {
			input.PaymentMethod = "credit"
		}
		if input.PaymentMethod != "credit" && input.PaymentMethod != "cash" && input.PaymentMethod != "bank" {
			http.Error(w, "invalid payment method", 400)
			return
		}
		seen := map[string]bool{}
		total := 0.0
		for _, line := range input.Items {
			if line.ProductID == "" || line.Quantity <= 0 || line.FreeQuantity < 0 || line.UnitCost < 0 || line.SellingPrice < 0 || line.WholesalePrice < 0 || line.Discount < 0 || line.Tax < 0 || line.Discount > line.Quantity*line.UnitCost+line.Tax {
				http.Error(w, "invalid purchase quantities, prices, discount or tax", 400)
				return
			}
			key := line.ProductID + "|" + strings.ToLower(strings.TrimSpace(line.BatchNo)) + "|" + line.ExpiryDate
			if seen[key] {
				http.Error(w, "duplicate product and batch; merge it into one line", 409)
				return
			}
			seen[key] = true
			if line.ManufacturedDate != "" && line.ExpiryDate != "" && line.ManufacturedDate > line.ExpiryDate {
				http.Error(w, "manufactured date cannot be after expiry date", 400)
				return
			}
			if input.PurchaseDate != "" && line.ExpiryDate != "" && line.ExpiryDate < input.PurchaseDate {
				http.Error(w, "expiry date cannot be before purchase date", 400)
				return
			}
			total += line.Quantity*line.UnitCost - line.Discount + line.Tax
		}
		total = math.Round((total-input.Discount+input.Tax)*100) / 100
		if total < 0 || input.PaidAmount < 0 || input.PaidAmount > total {
			http.Error(w, "paid amount cannot exceed purchase total", 400)
			return
		}
		if input.PaidAmount > 0 && (input.PaymentMethod == "credit" || input.AccountID == "") {
			http.Error(w, "select cash/bank and its account for a paid purchase", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='grn_create'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}
		var supplierExists bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM suppliers WHERE id=$1 AND tenant_id=$2 AND is_active)`, input.SupplierID, c.Tenant).Scan(&supplierExists); err != nil || !supplierExists {
			http.Error(w, "active supplier not found", 404)
			return
		}
		if input.PaidAmount > 0 {
			var accountType string
			err = tx.QueryRow(r.Context(), `SELECT account_type FROM cash_accounts WHERE id=$1 AND tenant_id=$2 AND is_active`, input.AccountID, c.Tenant).Scan(&accountType)
			if err != nil || accountType != input.PaymentMethod {
				http.Error(w, "selected account does not match payment method", 409)
				return
			}
		}
		var id string
		err = tx.QueryRow(r.Context(), `INSERT INTO purchases(invoice_no,supplier_id,total,paid_amount,tenant_id,branch_id,purchase_order_id,purchase_date,discount,tax,payment_method,account_id,status,notes,client_request_id) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid,COALESCE(NULLIF($8,'')::date,current_date),$9,$10,$11,NULLIF($12,'')::uuid,'draft',NULLIF($13,''),$14) RETURNING id`, strings.TrimSpace(input.InvoiceNo), input.SupplierID, total, input.PaidAmount, c.Tenant, c.Branch, input.PurchaseOrderID, input.PurchaseDate, input.Discount, input.Tax, input.PaymentMethod, input.AccountID, strings.TrimSpace(input.Notes), input.RequestID).Scan(&id)
		if err != nil {
			http.Error(w, "supplier invoice already exists or purchase is invalid", 409)
			return
		}
		for _, line := range input.Items {
			var track, assigned bool
			err = tx.QueryRow(r.Context(), `SELECT p.track_expiry,EXISTS(SELECT 1 FROM supplier_products sp WHERE sp.tenant_id=$2 AND sp.supplier_id=$3 AND sp.product_id=p.id AND sp.is_active) FROM products p WHERE p.id=$1 AND p.tenant_id=$2 AND p.is_active`, line.ProductID, c.Tenant, input.SupplierID).Scan(&track, &assigned)
			if err != nil {
				http.Error(w, "active product not found", 404)
				return
			}
			if !assigned {
				http.Error(w, "every product must be assigned to this supplier", 409)
				return
			}
			if track && (strings.TrimSpace(line.BatchNo) == "" || line.ExpiryDate == "") {
				http.Error(w, "batch number and expiry date are required for expiry-tracked products", 400)
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
				http.Error(w, err.Error(), 400)
				return
			}
		}
		response, _ := json.Marshal(map[string]any{"id": id, "status": "draft", "total": total})
		if _, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'grn_create',$3,$4)`, c.Tenant, input.RequestID, id, response); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(201)
		w.Write(response)
	}
}

type securedPurchaseLine struct {
	id, product, batch, manufactured, expiry          string
	quantity, cost, inventoryCost, selling, wholesale float64
	updateCost, updateSale                            bool
}

func allocatedDocumentLineAmount(lineAmount, documentTotal, lineAmountTotal float64) float64 {
	if lineAmount <= 0 || documentTotal <= 0 || lineAmountTotal <= 0 {
		return 0
	}
	return lineAmount * documentTotal / lineAmountTotal
}

func secureFinalizeCatalogueGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := r.PathValue("id")
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var supplier, status, method, branch string
		var account *string
		var total, paid float64
		err = tx.QueryRow(r.Context(), `SELECT supplier_id,status,total,paid_amount,payment_method,account_id::text,COALESCE(branch_id::text,'') FROM purchases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, c.Tenant).Scan(&supplier, &status, &total, &paid, &method, &account, &branch)
		if err != nil || status != "draft" {
			http.Error(w, "draft GRN not found", 409)
			return
		}
		if paid > 0 {
			if account == nil {
				http.Error(w, "payment account is required before finalizing", 409)
				return
			}
			var available float64
			err = tx.QueryRow(r.Context(), `SELECT opening_balance+COALESCE(sum(CASE WHEN t.transaction_type IN('deposit','income','transfer_in') THEN t.amount ELSE -t.amount END),0) FROM cash_accounts a LEFT JOIN cash_transactions t ON t.account_id=a.id AND t.status='finalized' WHERE a.id=$1 AND a.tenant_id=$2 AND a.account_type=$3 AND a.is_active GROUP BY a.id`, *account, c.Tenant, method).Scan(&available)
			if err != nil || available < paid {
				http.Error(w, "insufficient payment account balance", 409)
				return
			}
		}
		lines, err := loadSecuredPurchaseLines(r, tx, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if len(lines) == 0 {
			http.Error(w, "GRN has no product lines", 409)
			return
		}
		for _, line := range lines {
			var balance, oldCost float64
			err = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2 WHERE id=$1 AND tenant_id=$3 AND is_active RETURNING stock_quantity,purchase_price`, line.product, line.quantity, c.Tenant).Scan(&balance, &oldCost)
			if err != nil {
				http.Error(w, "active product stock update failed", 409)
				return
			}
			if err = adjustBranchStock(r.Context(), tx, c.Tenant, branch, line.product, line.quantity); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			if _, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'purchase',$2,$3,NULLIF($4,'')::uuid,'purchase',$5,$6,'Purchased quantity including free items',NULLIF($7,'')::uuid)`, line.product, line.quantity, c.Tenant, c.Branch, id, balance, c.Sub); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			var batchID string
			err = tx.QueryRow(r.Context(), `INSERT INTO stock_batches(product_id,branch_id,batch_no,manufactured_date,expiry_date,available_qty,unit_cost) VALUES($1,NULLIF($2,'')::uuid,NULLIF($3,''),NULLIF($4,'')::date,NULLIF($5,'')::date,$6,$7) RETURNING id`, line.product, branch, line.batch, line.manufactured, line.expiry, line.quantity, line.inventoryCost).Scan(&batchID)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if _, err = tx.Exec(r.Context(), `UPDATE purchase_items SET batch_id=$2 WHERE id=$1`, line.id, batchID); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if _, err = tx.Exec(r.Context(), `INSERT INTO supplier_product_price_history(tenant_id,supplier_id,product_id,purchase_id,old_purchase_price,new_purchase_price,selling_price,wholesale_price,changed_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid)`, c.Tenant, supplier, line.product, id, oldCost, line.cost, line.selling, line.wholesale, c.Sub); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if _, err = tx.Exec(r.Context(), `UPDATE supplier_products SET last_purchase_price=$4,default_purchase_price=CASE WHEN $5 THEN $4 ELSE default_purchase_price END,recommended_selling_price=CASE WHEN $6 THEN $7 ELSE recommended_selling_price END,wholesale_price=CASE WHEN $6 THEN $8 ELSE wholesale_price END,last_purchased_at=now(),updated_at=now() WHERE tenant_id=$1 AND supplier_id=$2 AND product_id=$3`, c.Tenant, supplier, line.product, line.cost, line.updateCost, line.updateSale, line.selling, line.wholesale); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if line.updateCost || line.updateSale {
				if _, err = tx.Exec(r.Context(), `UPDATE products SET purchase_price=CASE WHEN $3 THEN $4 ELSE purchase_price END,selling_price=CASE WHEN $5 THEN $6 ELSE selling_price END,wholesale_price=CASE WHEN $5 THEN $7 ELSE wholesale_price END,updated_at=now() WHERE id=$1 AND tenant_id=$2`, line.product, c.Tenant, line.updateCost, line.cost, line.updateSale, line.selling, line.wholesale); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			}
		}
		payable := total - paid
		if payable > 0 {
			if _, err = tx.Exec(r.Context(), `UPDATE suppliers SET balance=balance+$2 WHERE id=$1 AND tenant_id=$3`, supplier, payable, c.Tenant); err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO supplier_ledger(tenant_id,supplier_id,entry_type,reference_id,debit) VALUES($1,$2,'purchase_finalize',$3,$4)`, c.Tenant, supplier, id, payable)
			}
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE purchases SET status='finalized',finalized_by=NULLIF($2,'')::uuid,finalized_at=now() WHERE id=$1 AND tenant_id=$3`, id, c.Sub, c.Tenant); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if paid > 0 {
			var journalID string
			if err = tx.QueryRow(r.Context(), `SELECT id FROM journal_entries WHERE tenant_id=$1 AND reference_type='purchase' AND reference_id=$2 AND status='posted' ORDER BY created_at DESC LIMIT 1`, c.Tenant, id).Scan(&journalID); err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,transaction_date,created_by,tenant_id,client_request_id,reference_type,reference_id,journal_entry_id) SELECT account_id,'withdrawal',$2,invoice_no,'GRN payment',purchase_date,NULLIF($3,'')::uuid,tenant_id,'grn-payment-'||id::text,'purchase',id,$4 FROM purchases WHERE id=$1`, id, paid, c.Sub, journalID)
			}
			if err != nil {
				http.Error(w, "GRN payment cashbook entry failed: "+err.Error(), 500)
				return
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE purchase_order_items oi SET received_quantity=received_quantity+pi.quantity FROM purchase_items pi WHERE pi.purchase_id=$1 AND pi.purchase_order_item_id=oi.id`, id); err != nil {
			http.Error(w, "purchase order receipt update failed", 409)
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE purchase_orders po SET status=CASE WHEN NOT EXISTS(SELECT 1 FROM purchase_order_items oi WHERE oi.purchase_order_id=po.id AND oi.received_quantity<oi.quantity) THEN 'completed' ELSE 'partially_received' END,updated_at=now() WHERE po.id=(SELECT purchase_order_id FROM purchases WHERE id=$1)`, id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		auditUserAction(r, db, "GRN_FINALIZED", id, map[string]any{"total": total, "paid": paid})
		w.WriteHeader(204)
	}
}

func loadSecuredPurchaseLines(r *http.Request, tx pgx.Tx, purchaseID string) ([]securedPurchaseLine, error) {
	rows, err := tx.Query(r.Context(), `SELECT i.id,i.product_id,i.quantity+i.free_quantity,i.unit_cost,i.selling_price,i.wholesale_price,COALESCE(i.batch_no,''),COALESCE(i.manufactured_date::text,''),COALESCE(i.expiry_date::text,''),i.update_purchase_price,i.update_selling_price,i.total,p.total,COALESCE((SELECT sum(i2.total) FROM purchase_items i2 WHERE i2.purchase_id=i.purchase_id),0) FROM purchase_items i JOIN purchases p ON p.id=i.purchase_id WHERE i.purchase_id=$1 ORDER BY i.id FOR UPDATE OF i`, purchaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []securedPurchaseLine{}
	for rows.Next() {
		var line securedPurchaseLine
		var lineAmount, documentTotal, lineAmountTotal float64
		if err = rows.Scan(&line.id, &line.product, &line.quantity, &line.cost, &line.selling, &line.wholesale, &line.batch, &line.manufactured, &line.expiry, &line.updateCost, &line.updateSale, &lineAmount, &documentTotal, &lineAmountTotal); err != nil {
			return nil, err
		}
		if line.quantity > 0 {
			line.inventoryCost = math.Round(allocatedDocumentLineAmount(lineAmount, documentTotal, lineAmountTotal)/line.quantity*10000) / 10000
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

func secureReverseCatalogueGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		input.Reason = strings.TrimSpace(input.Reason)
		if input.Reason == "" {
			http.Error(w, "reversal reason required", 400)
			return
		}
		c := claimsFrom(r)
		id := r.PathValue("id")
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var supplier, status, branch string
		var account *string
		var total, paid, balance float64
		err = tx.QueryRow(r.Context(), `SELECT p.supplier_id,p.status,p.total,p.paid_amount,p.account_id::text,s.balance,COALESCE(p.branch_id::text,'') FROM purchases p JOIN suppliers s ON s.id=p.supplier_id AND s.tenant_id=p.tenant_id WHERE p.id=$1 AND p.tenant_id=$2 FOR UPDATE OF p,s`, id, c.Tenant).Scan(&supplier, &status, &total, &paid, &account, &balance, &branch)
		if err != nil || status != "finalized" {
			http.Error(w, "only a finalized GRN can be reversed", 409)
			return
		}
		var allocatedPaymentExists bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM purchase_payment_allocations a JOIN supplier_payments sp ON sp.id=a.supplier_payment_id WHERE a.tenant_id=$1 AND a.purchase_id=$2 AND sp.status='finalized')`, c.Tenant, id).Scan(&allocatedPaymentExists); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if allocatedPaymentExists {
			http.Error(w, "allocated supplier payments must be reversed before reversing this GRN", 409)
			return
		}
		payable := total - paid
		if balance < payable {
			http.Error(w, "supplier payments have been applied; reverse those payments first", 409)
			return
		}
		lines, err := loadSecuredPurchaseLines(r, tx, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		for _, line := range lines {
			var batchID string
			var available float64
			err = tx.QueryRow(r.Context(), `SELECT b.id,b.available_qty FROM stock_batches b WHERE b.id=(SELECT pi.batch_id FROM purchase_items pi WHERE pi.id=$1) FOR UPDATE`, line.id).Scan(&batchID, &available)
			if err != nil || available < line.quantity {
				http.Error(w, "stock from this GRN was already consumed and cannot be reversed", 409)
				return
			}
			if _, err = tx.Exec(r.Context(), `UPDATE stock_batches SET available_qty=available_qty-$2,status=CASE WHEN available_qty-$2=0 THEN 'reversed' ELSE status END WHERE id=$1`, batchID, line.quantity); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			var after float64
			err = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND tenant_id=$3 AND stock_quantity>=$2 RETURNING stock_quantity`, line.product, line.quantity, c.Tenant).Scan(&after)
			if err != nil {
				http.Error(w, "insufficient product stock to reverse GRN", 409)
				return
			}
			if err = adjustBranchStock(r.Context(), tx, c.Tenant, branch, line.product, -line.quantity); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			if _, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'purchase_reversal',$2,$3,NULLIF($4,'')::uuid,'purchase',$5,$6,$7,NULLIF($8,'')::uuid)`, line.product, -line.quantity, c.Tenant, c.Branch, id, after, input.Reason, c.Sub); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if payable > 0 {
			if _, err = tx.Exec(r.Context(), `UPDATE suppliers SET balance=balance-$2 WHERE id=$1 AND tenant_id=$3`, supplier, payable, c.Tenant); err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO supplier_ledger(tenant_id,supplier_id,entry_type,reference_id,credit) VALUES($1,$2,'purchase_reverse',$3,$4)`, c.Tenant, supplier, id, payable)
			}
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE purchases SET status='reversed',reversed_by=NULLIF($2,'')::uuid,reversed_at=now(),reversal_reason=$3,notes=concat_ws(E'\n',notes,'Reversed: '||$3) WHERE id=$1 AND tenant_id=$4`, id, c.Sub, input.Reason, c.Tenant); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if paid > 0 {
			if account == nil {
				http.Error(w, "original payment account is missing", 409)
				return
			}
			var reversalJournal string
			if err = tx.QueryRow(r.Context(), `SELECT id FROM journal_entries WHERE tenant_id=$1 AND reference_type='purchase_reversal' AND reference_id=$2 AND status='posted' ORDER BY created_at DESC LIMIT 1`, c.Tenant, id).Scan(&reversalJournal); err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO cash_transactions(account_id,transaction_type,amount,reference,description,created_by,tenant_id,client_request_id,reference_type,reference_id,journal_entry_id) VALUES($1,'income',$2,$3::text,$4,NULLIF($5,'')::uuid,$6,'grn-reversal-payment-'||$3::text,'purchase_reversal',$8::uuid,$7)`, *account, paid, id, "GRN reversal: "+input.Reason, c.Sub, c.Tenant, reversalJournal, id)
			}
			if err != nil {
				http.Error(w, "GRN payment reversal cashbook entry failed: "+err.Error(), 500)
				return
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE purchase_order_items oi SET received_quantity=received_quantity-pi.quantity FROM purchase_items pi WHERE pi.purchase_id=$1 AND pi.purchase_order_item_id=oi.id`, id); err != nil {
			http.Error(w, "purchase order receipt reversal failed", 409)
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE purchase_orders po SET status=CASE WHEN EXISTS(SELECT 1 FROM purchase_order_items oi WHERE oi.purchase_order_id=po.id AND oi.received_quantity>0) THEN 'partially_received' WHEN EXISTS(SELECT 1 FROM purchases p WHERE p.purchase_order_id=po.id AND p.status='draft') THEN 'ordered' ELSE 'approved' END,updated_at=now() WHERE po.id=(SELECT purchase_order_id FROM purchases WHERE id=$1)`, id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		auditUserAction(r, db, "GRN_REVERSED", id, map[string]any{"reason": input.Reason})
		w.WriteHeader(204)
	}
}

func secureCancelCatalogueGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		input.Reason = strings.TrimSpace(input.Reason)
		if input.Reason == "" {
			http.Error(w, "cancellation reason required", http.StatusBadRequest)
			return
		}
		c := claimsFrom(r)
		id := r.PathValue("id")
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())
		var poID *string
		if err = tx.QueryRow(r.Context(), `SELECT purchase_order_id::text FROM purchases WHERE id=$1 AND tenant_id=$2 AND status='draft' FOR UPDATE`, id, c.Tenant).Scan(&poID); err != nil {
			http.Error(w, "only a draft GRN can be cancelled", http.StatusConflict)
			return
		}
		tag, err := tx.Exec(r.Context(), `UPDATE purchases SET status='cancelled',cancelled_by=NULLIF($2,'')::uuid,cancelled_at=now(),cancellation_reason=$3,notes=concat_ws(E'\n',notes,'Cancelled: '||$3),updated_at=now() WHERE id=$1 AND tenant_id=$4 AND status='draft'`, id, c.Sub, input.Reason, c.Tenant)
		if err != nil || tag.RowsAffected() != 1 {
			http.Error(w, "only a draft GRN can be cancelled", http.StatusConflict)
			return
		}
		if poID != nil {
			_, err = tx.Exec(r.Context(), `UPDATE purchase_orders po SET status=CASE WHEN EXISTS(SELECT 1 FROM purchase_order_items oi WHERE oi.purchase_order_id=po.id AND oi.received_quantity>0) THEN 'partially_received' WHEN EXISTS(SELECT 1 FROM purchases p WHERE p.purchase_order_id=po.id AND p.status='draft') THEN 'ordered' ELSE 'approved' END,updated_at=now() WHERE po.id=$1 AND po.tenant_id=$2 AND po.status NOT IN('completed','cancelled')`, *poID, c.Tenant)
			if err != nil {
				http.Error(w, "purchase order reservation release failed", http.StatusInternalServerError)
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		auditUserAction(r, db, "GRN_CANCELLED", id, map[string]any{"reason": input.Reason})
		w.WriteHeader(http.StatusNoContent)
	}
}
