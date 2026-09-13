package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"net/http"
	"strings"
)

type checkoutPayment struct {
	Method    string  `json:"method"`
	Amount    float64 `json:"amount"`
	Tendered  float64 `json:"tendered"`
	Reference string  `json:"reference"`
}

type secureCheckoutInput struct {
	CustomerID         string            `json:"customerId"`
	Lines              []CheckoutLine    `json:"lines"`
	Discount           float64           `json:"discount"`
	PaidAmount         float64           `json:"paidAmount"`
	PaymentMethod      string            `json:"paymentMethod"`
	Payments           []checkoutPayment `json:"payments"`
	RequestID          string            `json:"requestId"`
	DiscountApprovalID string            `json:"discountApprovalId"`
}

func secureCheckout(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x secureCheckoutInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || len(x.Lines) == 0 || x.Discount < 0 || x.PaidAmount < 0 || strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "sale lines, payment and request ID are required", 400)
			return
		}
		if len(x.Payments) == 0 {
			if x.PaymentMethod == "" {
				x.PaymentMethod = "cash"
			}
			x.Payments = []checkoutPayment{{Method: x.PaymentMethod, Amount: x.PaidAmount, Tendered: x.PaidAmount}}
		}
		for i := range x.Payments {
			p := &x.Payments[i]
			if p.Method != "cash" && p.Method != "bank" && p.Method != "card" || p.Amount < 0 {
				http.Error(w, "invalid payment allocation", 400)
				return
			}
			if p.Tendered == 0 {
				p.Tendered = p.Amount
			}
			if p.Method != "cash" && math.Abs(p.Tendered-p.Amount) > 0.009 || p.Tendered < p.Amount {
				http.Error(w, "only cash can include change", 400)
				return
			}
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var prior []byte
		e = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='pos_checkout'`, c.Tenant, x.RequestID).Scan(&prior)
		if e == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(prior)
			return
		}
		if e != pgx.ErrNoRows {
			http.Error(w, e.Error(), 500)
			return
		}
		var cashierSessionID string
		e = tx.QueryRow(r.Context(), `SELECT id FROM cashier_sessions WHERE tenant_id=$1 AND user_id=$2 AND branch_id=NULLIF($3,'')::uuid AND closed_at IS NULL ORDER BY opened_at DESC LIMIT 1 FOR UPDATE`, c.Tenant, c.Sub, c.Branch).Scan(&cashierSessionID)
		if e == pgx.ErrNoRows {
			http.Error(w, "open a cashier session before completing a sale", http.StatusConflict)
			return
		}
		if e != nil {
			http.Error(w, e.Error(), http.StatusInternalServerError)
			return
		}
		type pricedLine struct {
			product                                                               string
			promotionID                                                           string
			qty, price, discount, promotionDiscount, effectivePrice, fallbackCost float64
			enteredQty, enteredPrice, factor, tare                                float64
			unitID                                                                string
			trackExpiry                                                           bool
		}
		priced := []pricedLine{}
		gross, lineDiscountTotal, promotionDiscountTotal := 0.0, 0.0, 0.0
		var maxItemPercent, maxInvoicePercent float64
		e = tx.QueryRow(r.Context(), `SELECT COALESCE(dl.max_item_percent,0),COALESCE(dl.max_invoice_percent,0) FROM roles ro LEFT JOIN role_discount_limits dl ON dl.role_id=ro.id WHERE ro.tenant_id=$1 AND ro.key=$2`, c.Tenant, c.Role).Scan(&maxItemPercent, &maxInvoicePercent)
		if e != nil {
			http.Error(w, "discount policy is not configured", 409)
			return
		}
		requestedQty := map[string]float64{}
		for i := range x.Lines {
			l := &x.Lines[i]
			if l.EnteredQuantity <= 0 {
				l.EnteredQuantity = l.Quantity
			}
			factor := 1.0
			if l.UnitID != "" {
				if e = tx.QueryRow(r.Context(), `SELECT factor_to_base FROM product_units WHERE tenant_id=$1 AND product_id=$2 AND unit_id=$3 AND is_active AND usage IN('sale','both')`, c.Tenant, l.ProductID, l.UnitID).Scan(&factor); e != nil {
					http.Error(w, "sale unit is not allowed for this product", 400)
					return
				}
			}
			l.Quantity = math.Round((l.EnteredQuantity-l.TareQuantity)*factor*1e6) / 1e6
			if l.Quantity <= 0 {
				http.Error(w, "net measured quantity must be positive", 400)
				return
			}
			requestedQty[l.ProductID] += l.Quantity
		}
		for _, l := range x.Lines {
			var stock, price, purchasePrice float64
			var trackExpiry bool
			var minimum, step float64
			var precision int
			e = tx.QueryRow(r.Context(), `SELECT stock_quantity,selling_price,purchase_price,track_expiry,minimum_sale_quantity,quantity_step,decimal_precision FROM products WHERE id=$1 AND tenant_id=$2 AND is_active FOR UPDATE`, l.ProductID, c.Tenant).Scan(&stock, &price, &purchasePrice, &trackExpiry, &minimum, &step, &precision)
			if e != nil || l.Quantity <= 0 || stock < requestedQty[l.ProductID] {
				http.Error(w, "invalid product quantity or insufficient stock", 409)
				return
			}
			if l.Quantity+1e-6 < minimum || step > 0 && math.Abs(l.Quantity/step-math.Round(l.Quantity/step)) > 1e-5 {
				http.Error(w, "quantity does not match the product minimum or step", 400)
				return
			}
			factor := l.Quantity / (l.EnteredQuantity - l.TareQuantity)
			enteredPrice := price * factor
			if l.UnitID != "" {
				var override *float64
				if e = tx.QueryRow(r.Context(), `SELECT sale_price FROM product_units WHERE tenant_id=$1 AND product_id=$2 AND unit_id=$3`, c.Tenant, l.ProductID, l.UnitID).Scan(&override); e == nil && override != nil {
					enteredPrice = *override
					price = enteredPrice / factor
				}
			}
			_ = precision
			var branchStock float64
			e = tx.QueryRow(r.Context(), `SELECT quantity FROM branch_stock WHERE product_id=$1 AND branch_id=NULLIF($2,'')::uuid FOR UPDATE`, l.ProductID, c.Branch).Scan(&branchStock)
			if e != nil || branchStock < requestedQty[l.ProductID] {
				http.Error(w, "insufficient stock in cashier branch", 409)
				return
			}
			lineGross := l.Quantity * price
			if l.Discount < 0 || l.Discount > lineGross {
				http.Error(w, "invalid item discount", 400)
				return
			}
			var promotionID, kind string
			var promotionValue float64
			e = tx.QueryRow(r.Context(), `SELECT pr.id,pr.kind,pr.value FROM promotions pr JOIN products p ON p.id=$1 WHERE pr.tenant_id=$2 AND pr.is_active AND pr.archived_at IS NULL AND current_date BETWEEN pr.start_date AND pr.end_date AND (pr.product_id=p.id OR (pr.product_id IS NULL AND pr.category_id=p.category_id)) ORDER BY GREATEST(0,LEAST(p.selling_price,CASE pr.kind WHEN 'percentage' THEN p.selling_price*pr.value/100 WHEN 'fixed' THEN pr.value WHEN 'special_price' THEN p.selling_price-pr.value ELSE 0 END)) DESC,(pr.product_id IS NOT NULL) DESC,pr.created_at DESC LIMIT 1 FOR SHARE OF pr`, l.ProductID, c.Tenant).Scan(&promotionID, &kind, &promotionValue)
			if e != nil && e != pgx.ErrNoRows {
				http.Error(w, e.Error(), 500)
				return
			}
			unitPromo := 0.0
			if e == nil {
				unitPromo = promotionDiscount(kind, promotionValue, price)
			}
			promoTotal := math.Round(unitPromo*l.Quantity*100) / 100
			if l.Discount > lineGross-promoTotal {
				http.Error(w, "item discount exceeds the price after promotion", 400)
				return
			}
			gross += lineGross
			lineDiscountTotal += l.Discount
			promotionDiscountTotal += promoTotal
			priced = append(priced, pricedLine{product: l.ProductID, promotionID: promotionID, qty: l.Quantity, price: price, discount: l.Discount, promotionDiscount: promoTotal, effectivePrice: price - unitPromo, fallbackCost: purchasePrice, trackExpiry: trackExpiry, enteredQty: l.EnteredQuantity, enteredPrice: enteredPrice, factor: factor, tare: l.TareQuantity, unitID: l.UnitID})
		}
		netBeforeInvoiceDiscount := gross - lineDiscountTotal - promotionDiscountTotal
		total := netBeforeInvoiceDiscount - x.Discount
		if total < 0 {
			http.Error(w, "discount cannot exceed sale value", 400)
			return
		}
		overLimit := netBeforeInvoiceDiscount > 0 && x.Discount/netBeforeInvoiceDiscount*100 > maxInvoicePercent+0.0001
		for _, line := range priced {
			if line.qty*line.price > 0 && line.discount/(line.qty*line.price)*100 > maxItemPercent+0.0001 {
				overLimit = true
			}
		}
		if overLimit {
			if x.DiscountApprovalID != "" {
				e = tx.QueryRow(r.Context(), `SELECT id FROM pos_discount_approvals WHERE id=$1 AND tenant_id=$2 AND requested_by=$3 AND status='approved' AND abs(gross-$4)<0.01 AND abs(item_discount-$5)<0.01 AND abs(invoice_discount-$6)<0.01`, x.DiscountApprovalID, c.Tenant, c.Sub, gross, lineDiscountTotal, x.Discount).Scan(&x.DiscountApprovalID)
			} else {
				e = tx.QueryRow(r.Context(), `SELECT id FROM pos_discount_approvals WHERE tenant_id=$1 AND requested_by=$2 AND status='approved' AND abs(gross-$3)<0.01 AND abs(item_discount-$4)<0.01 AND abs(invoice_discount-$5)<0.01 ORDER BY reviewed_at DESC LIMIT 1`, c.Tenant, c.Sub, gross, lineDiscountTotal, x.Discount).Scan(&x.DiscountApprovalID)
			}
			if e != nil {
				http.Error(w, "discount exceeds your role limit; request manager approval", 403)
				return
			}
		}
		paid, tendered, cashPaid := 0.0, 0.0, 0.0
		for _, p := range x.Payments {
			paid += p.Amount
			tendered += p.Tendered
			if p.Method == "cash" {
				cashPaid += p.Amount
			}
		}
		paid = math.Round(paid*100) / 100
		if paid > total+0.009 {
			http.Error(w, "payment allocations exceed invoice total", 400)
			return
		}
		if x.CustomerID == "" && paid < total {
			http.Error(w, "customer required for credit sale", 400)
			return
		}
		balance := total - paid
		if x.CustomerID != "" {
			var available float64
			e = tx.QueryRow(r.Context(), `SELECT credit_limit-balance FROM customers WHERE id=$1 AND tenant_id=$2 AND is_active FOR UPDATE`, x.CustomerID, c.Tenant).Scan(&available)
			if e != nil {
				http.Error(w, "customer not found", 404)
				return
			}
			if balance > available {
				http.Error(w, "customer credit limit exceeded", 409)
				return
			}
		}
		var id, invoice string
		method := x.Payments[0].Method
		if len(x.Payments) > 1 {
			method = "split"
		}
		e = tx.QueryRow(r.Context(), `INSERT INTO sales(invoice_no,cashier_id,customer_id,total,paid_amount,balance,discount,payment_method,tenant_id,branch_id,client_request_id,cashier_session_id)VALUES(next_tenant_invoice_no($8),$1,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid,$10,$11)RETURNING id,invoice_no`, c.Sub, x.CustomerID, total, paid, balance, x.Discount, method, c.Tenant, c.Branch, x.RequestID, cashierSessionID).Scan(&id, &invoice)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		for _, p := range x.Payments {
			if p.Amount > 0 {
				_, e = tx.Exec(r.Context(), `INSERT INTO sale_payments(tenant_id,sale_id,cashier_session_id,method,amount,tendered,reference,created_by) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8)`, c.Tenant, id, cashierSessionID, p.Method, p.Amount, p.Tendered, strings.TrimSpace(p.Reference), c.Sub)
				if e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
			}
		}
		// Every sale posts its revenue here, whatever it was paid with. This
		// used to run only for split tenders, with a database trigger covering
		// the rest; the two disagreed about discounts, so the ledger showed
		// sales net of discount and the discount given was invisible.
		tenders := make([]tenderLeg, 0, len(x.Payments))
		for _, p := range x.Payments {
			if p.Amount > 0 {
				tenders = append(tenders, tenderLeg{Role: tenderRoleForMethod(p.Method), Amount: p.Amount, Memo: p.Method})
			}
		}
		revenueLines, e := saleRevenueLines(saleAmounts{
			Gross:     gross,
			Discount:  lineDiscountTotal + promotionDiscountTotal + x.Discount,
			Tenders:   tenders,
			OnAccount: balance,
		})
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if _, _, e = postJournalTx(r.Context(), tx, journalDraft{
			Tenant: c.Tenant, Branch: c.Branch, Actor: c.Sub,
			ReferenceType: "sale", ReferenceID: id,
			Description:     "Sale " + invoice,
			ClientRequestID: "sale:" + id,
			Lines:           revenueLines,
		}); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if cashPaid > 0 {
			_, e = tx.Exec(r.Context(), `INSERT INTO cashier_session_events(session_id,tenant_id,branch_id,event_type,reference_id,amount,created_by) VALUES($1,$2,NULLIF($3,'')::uuid,'sale_cash',$4,$5,$6)`, cashierSessionID, c.Tenant, c.Branch, id, cashPaid, c.Sub)
			if e != nil {
				http.Error(w, e.Error(), http.StatusInternalServerError)
				return
			}
		}
		for _, l := range priced {
			var saleItemID string
			allocations, unitCost, allocationErr := prepareFEFOAllocation(r.Context(), tx, l.product, c.Branch, l.qty, l.fallbackCost, l.trackExpiry)
			if allocationErr != nil {
				http.Error(w, allocationErr.Error(), http.StatusConflict)
				return
			}
			e = tx.QueryRow(r.Context(), `INSERT INTO sale_items(sale_id,product_id,quantity,unit_price,unit_cost,discount,promotion_id,promotion_discount,total,entered_quantity,entered_unit_id,entered_unit_price,conversion_factor,tare_quantity)VALUES($1,$2,$3,$4,$5,$6::numeric+$7::numeric,NULLIF($8,'')::uuid,$7,$3::numeric*$4::numeric-$6::numeric-$7::numeric,$9,NULLIF($10,'')::uuid,$11,$12,$13) RETURNING id`, id, l.product, l.qty, l.price, unitCost, l.discount, l.promotionDiscount, l.promotionID, l.enteredQty, l.unitID, l.enteredPrice, l.factor, l.tare).Scan(&saleItemID)
			if e == nil && l.promotionID != "" {
				_, e = tx.Exec(r.Context(), `INSERT INTO promotion_usage(tenant_id,promotion_id,sale_id,sale_item_id,quantity,original_unit_price,effective_unit_price,discount_amount) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, c.Tenant, l.promotionID, id, saleItemID, l.qty, l.price, l.effectivePrice, l.promotionDiscount)
			}
			if e == nil {
				e = applyFEFOAllocation(r.Context(), tx, saleItemID, allocations)
			}
			var after float64
			if e == nil {
				e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND tenant_id=$3 RETURNING stock_quantity`, l.product, l.qty, c.Tenant).Scan(&after)
			}
			if e == nil {
				e = adjustBranchStock(r.Context(), tx, c.Tenant, c.Branch, l.product, -l.qty)
			}
			if e == nil {
				_, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,created_by)VALUES($1,'sale',$2,$3,NULLIF($4,'')::uuid,'sale',$5,$6,$7)`, l.product, -l.qty, c.Tenant, c.Branch, id, after, c.Sub)
			}
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if x.CustomerID != "" && balance > 0 {
			_, e = tx.Exec(r.Context(), `UPDATE customers SET balance=balance+$2 WHERE id=$1 AND tenant_id=$3`, x.CustomerID, balance, c.Tenant)
			if e == nil {
				_, e = tx.Exec(r.Context(), `INSERT INTO customer_ledger(tenant_id,customer_id,entry_type,reference_id,debit)VALUES($1,$2,'sale',$3,$4)`, c.Tenant, x.CustomerID, id, balance)
			}
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if x.DiscountApprovalID != "" {
			tag, approvalErr := tx.Exec(r.Context(), `UPDATE pos_discount_approvals SET status='used',sale_id=$2 WHERE id=$1 AND tenant_id=$3 AND status='approved'`, x.DiscountApprovalID, id, c.Tenant)
			if approvalErr != nil || tag.RowsAffected() != 1 {
				http.Error(w, "discount approval is no longer available", 409)
				return
			}
		}
		response, _ := json.Marshal(map[string]any{"id": id, "invoice": invoice, "gross": gross, "itemDiscount": lineDiscountTotal, "promotionDiscount": promotionDiscountTotal, "invoiceDiscount": x.Discount, "total": total, "paid": paid, "change": math.Max(0, tendered-paid), "paymentMethod": method, "payments": x.Payments, "cashierSessionId": cashierSessionID})
		_, e = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response)VALUES($1,$2,'pos_checkout',$3,$4)`, c.Tenant, x.RequestID, id, response)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write(response)
	}
}
