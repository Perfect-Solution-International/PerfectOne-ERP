package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

type CheckoutLine struct {
	ProductID string  `json:"productId"`
	Quantity  float64 `json:"quantity"`
	EnteredQuantity float64 `json:"enteredQuantity"`
	UnitID string `json:"unitId"`
	TareQuantity float64 `json:"tareQuantity"`
	UnitPrice float64 `json:"unitPrice"`
	Discount  float64 `json:"discount"`
}
type Checkout struct {
	CashierID     string         `json:"cashierId"`
	CustomerID    string         `json:"customerId"`
	Lines         []CheckoutLine `json:"lines"`
	Discount      float64        `json:"discount"`
	PaidAmount    float64        `json:"paidAmount"`
	PaymentMethod string         `json:"paymentMethod"`
}

func registerPOSWorkflow(m *http.ServeMux, db *pgxpool.Pool) {
	registerPOSDiscountApprovals(m, db)
	m.HandleFunc("POST /pos/checkout", jsonAPI(authenticated(db, salesRoles...)(trackPOSSync(db,secureCheckout(db)))))
	m.HandleFunc("GET /pos/holds", jsonAPI(authenticated(db, salesRoles...)(heldSales(db))))
	m.HandleFunc("POST /pos/holds", jsonAPI(authenticated(db, salesRoles...)(holdSale(db))))
	m.HandleFunc("POST /pos/holds/{id}/resume", jsonAPI(authenticated(db, salesRoles...)(resolveHeldSale(db,"resumed"))))
	m.HandleFunc("POST /pos/holds/{id}/cancel", jsonAPI(authenticated(db, salesRoles...)(resolveHeldSale(db,"cancelled"))))
	m.HandleFunc("POST /sales/{id}/return", jsonAPI(authenticated(db, salesRoles...)(secureReturnSale(db))))
}
func checkout(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x Checkout
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.CashierID == "" || len(x.Lines) == 0 || x.Discount < 0 || x.PaidAmount < 0 {
			http.Error(w, "cashier, lines and valid payment required", 400)
			return
		}
		if x.PaymentMethod == "" {
			x.PaymentMethod = "cash"
		}
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		gross := 0.0
		for _, l := range x.Lines {
			var stock float64
			e = tx.QueryRow(r.Context(), `SELECT stock_quantity FROM products WHERE id=$1 FOR UPDATE`, l.ProductID).Scan(&stock)
			if e != nil || l.Quantity <= 0 || l.UnitPrice < 0 || stock < l.Quantity {
				http.Error(w, "invalid line or insufficient stock", 409)
				return
			}
			gross += l.Quantity * l.UnitPrice
		}
		total := gross - x.Discount
		if total < 0 || (x.PaymentMethod != "cash" && x.PaidAmount > total) {
			http.Error(w, "invalid discount or payment", 400)
			return
		}
		if x.CustomerID == "" && x.PaidAmount < total {
			http.Error(w, "customer required for credit sale", 400)
			return
		}
		appliedPaid := x.PaidAmount
		if appliedPaid > total {
			appliedPaid = total
		}
		if x.CustomerID != "" && appliedPaid < total {
			var available float64
			e = tx.QueryRow(r.Context(), `SELECT credit_limit-balance FROM customers WHERE id=$1 FOR UPDATE`, x.CustomerID).Scan(&available)
			if e != nil || available < total-appliedPaid {
				http.Error(w, "customer credit limit exceeded", 409)
				return
			}
		}
		balance := total - appliedPaid
		var id, invoice string
		e = tx.QueryRow(r.Context(), `INSERT INTO sales(invoice_no,cashier_id,customer_id,total,paid_amount,balance,discount,payment_method,tenant_id) SELECT next_invoice_no(),$1,NULLIF($2,'')::uuid,$3::numeric,$4::numeric,$5::numeric,$6::numeric,$7::text,tenant_id FROM users WHERE id=$1 RETURNING id,invoice_no`, x.CashierID, x.CustomerID, total, appliedPaid, balance, x.Discount, x.PaymentMethod).Scan(&id, &invoice)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		for _, l := range x.Lines {
			_, e = tx.Exec(r.Context(), `INSERT INTO sale_items(sale_id,product_id,quantity,unit_price,total)VALUES($1,$2,$3,$4,$5)`, id, l.ProductID, l.Quantity, l.UnitPrice, l.Quantity*l.UnitPrice)
			if e == nil {
				e = consumeFEFO(r.Context(), tx, l.ProductID, l.Quantity)
			}
			if e == nil {
				_, e = tx.Exec(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1`, l.ProductID, l.Quantity)
			}
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if x.CustomerID != "" && balance > 0 {
			_, e = tx.Exec(r.Context(), `UPDATE customers SET balance=balance+$2 WHERE id=$1`, x.CustomerID, balance)
			if e == nil {
				_, e = tx.Exec(r.Context(), `INSERT INTO customer_ledger(customer_id,entry_type,reference_id,debit)VALUES($1,'sale',$3,$2)`, x.CustomerID, balance, id)
			}
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		change := 0.0
		if x.PaymentMethod == "cash" && x.PaidAmount > total {
			change = x.PaidAmount - total
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "invoice": invoice, "total": total, "change": change})
	}
}
func returnSale(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Reason string `json:"reason"`
			Items  []struct {
				SaleItemID string  `json:"saleItemId"`
				Quantity   float64 `json:"quantity"`
			} `json:"items"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.Reason == "" || len(x.Items) == 0 {
			http.Error(w, "reason and return items required", 400)
			return
		}
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		saleID := r.PathValue("id")
		var owns int
		if e = tx.QueryRow(r.Context(), `SELECT 1 FROM sales WHERE id=$1 AND tenant_id=$2`, saleID, claimsFrom(r).Tenant).Scan(&owns); e != nil {
			http.Error(w, "sale not found", 404)
			return
		}
		var total float64
		for _, i := range x.Items {
			var qty, price float64
			e = tx.QueryRow(r.Context(), `SELECT quantity,unit_price FROM sale_items WHERE id=$1 AND sale_id=$2`, i.SaleItemID, saleID).Scan(&qty, &price)
			if e != nil || i.Quantity <= 0 || i.Quantity > qty {
				http.Error(w, "invalid return quantity", 400)
				return
			}
			total += i.Quantity * price
		}
		var ret string
		e = tx.QueryRow(r.Context(), `INSERT INTO sale_returns(sale_id,return_no,reason,total)VALUES($1,'RET-'||to_char(now(),'YYYYMMDDHH24MISS'),$2,$3)RETURNING id`, saleID, x.Reason, total).Scan(&ret)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		for _, i := range x.Items {
			var product, code string
			var price float64
			tx.QueryRow(r.Context(), `SELECT product_id,unit_price FROM sale_items WHERE id=$1`, i.SaleItemID).Scan(&product, &price)
			_, e = tx.Exec(r.Context(), `INSERT INTO sale_return_items(return_id,sale_item_id,quantity,total)VALUES($1,$2,$3,$4)`, ret, i.SaleItemID, i.Quantity, i.Quantity*price)
			if e == nil {
				_, e = tx.Exec(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2 WHERE id=$1`, product, i.Quantity)
			}
			_ = code
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "returned", "total": total})
	}
}
