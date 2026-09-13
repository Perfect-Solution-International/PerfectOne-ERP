package main

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

func registerPOS(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /invoices/{id}", jsonAPI(authenticated(db)(invoice(db))))
	m.HandleFunc("POST /sales/{id}/cancel", jsonAPI(authenticated(db, "super_admin", "manager")(secureCancelSale(db))))
}

func invoice(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		permissions, err := effectivePermissions(r.Context(), db, c.Sub, c.Role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		viewAll := c.Role == "super_admin" || c.Role == "admin" || hasAny(permissions, "sales", "sales.view")
		viewOwn := viewAll || hasAny(permissions, "own_sales", "own_sales.view")
		if !viewOwn {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}

		query := `SELECT s.id,s.invoice_no,COALESCE(cu.id::text,''),COALESCE(cu.name,'Walk-in customer'),COALESCE(cu.phone,''),
		 COALESCE(u.name,'Unknown user'),COALESCE(b.name,''),s.total,s.discount,s.paid_amount,s.balance,s.returned_total,
		 s.returned_paid,s.returned_receivable,s.payment_method,s.status,s.created_at,COALESCE(s.cancellation_reason,''),s.cancelled_at
		 FROM sales s LEFT JOIN customers cu ON cu.id=s.customer_id LEFT JOIN users u ON u.id=s.cashier_id LEFT JOIN branches b ON b.id=s.branch_id
		 WHERE s.id=$1 AND s.tenant_id=$2`
		args := []any{r.PathValue("id"), c.Tenant}
		if !viewAll {
			query += ` AND s.cashier_id=$3`
			args = append(args, c.Sub)
		}
		var id, number, customerID, customer, customerPhone, cashier, branch, method, status, cancellationReason string
		var total, discount, paid, balance, returned, returnedPaid, returnedReceivable float64
		var createdAt, cancelledAt any
		err = db.QueryRow(r.Context(), query, args...).Scan(&id, &number, &customerID, &customer, &customerPhone, &cashier, &branch, &total, &discount, &paid, &balance, &returned, &returnedPaid, &returnedReceivable, &method, &status, &createdAt, &cancellationReason, &cancelledAt)
		if err != nil {
			http.Error(w, "invoice not found", http.StatusNotFound)
			return
		}

		items := []map[string]any{}
		rows, err := db.Query(r.Context(), `SELECT si.id,si.product_id,p.name,COALESCE(p.sku,''),si.quantity,si.unit_price,si.discount,si.total,
		 COALESCE(si.entered_quantity,si.quantity),COALESCE(u.symbol,''),COALESCE(si.entered_unit_price,si.unit_price),COALESCE(si.tare_quantity,0),
		 COALESCE((SELECT sum(ri.quantity) FROM sale_return_items ri JOIN sale_returns sr ON sr.id=ri.return_id WHERE ri.sale_item_id=si.id AND sr.status='finalized'),0)
		 FROM sale_items si JOIN products p ON p.id=si.product_id LEFT JOIN units u ON u.id=si.entered_unit_id WHERE si.sale_id=$1 ORDER BY si.id`, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var itemID, productID, product, sku string
			var unitSymbol string
			var quantity, unitPrice, itemDiscount, lineTotal, enteredQuantity, enteredUnitPrice, tareQuantity, returnedQuantity float64
			if err = rows.Scan(&itemID, &productID, &product, &sku, &quantity, &unitPrice, &itemDiscount, &lineTotal, &enteredQuantity, &unitSymbol, &enteredUnitPrice, &tareQuantity, &returnedQuantity); err != nil {
				rows.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			items = append(items, map[string]any{"id": itemID, "productId": productID, "product": product, "sku": sku, "quantity": quantity, "unitPrice": unitPrice, "discount": itemDiscount, "total": lineTotal, "enteredQuantity": enteredQuantity, "unitSymbol": unitSymbol, "enteredUnitPrice": enteredUnitPrice, "tareQuantity": tareQuantity, "returnedQuantity": returnedQuantity, "returnableQuantity": quantity - returnedQuantity})
		}
		rows.Close()
		if rows.Err() != nil {
			http.Error(w, rows.Err().Error(), http.StatusInternalServerError)
			return
		}

		returns := []map[string]any{}
		returnRows, err := db.Query(r.Context(), `SELECT id,return_no,reason,total,refunded_amount,receivable_adjustment,status,created_at FROM sale_returns WHERE sale_id=$1 ORDER BY created_at DESC`, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for returnRows.Next() {
			var returnID, returnNo, reason, returnStatus string
			var returnTotal, refunded, receivable float64
			var returnDate any
			if err = returnRows.Scan(&returnID, &returnNo, &reason, &returnTotal, &refunded, &receivable, &returnStatus, &returnDate); err != nil {
				returnRows.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			returns = append(returns, map[string]any{"id": returnID, "returnNo": returnNo, "reason": reason, "total": returnTotal, "refunded": refunded, "receivableAdjustment": receivable, "status": returnStatus, "createdAt": returnDate})
		}
		returnRows.Close()
		payments := []map[string]any{}
		paymentRows, paymentErr := db.Query(r.Context(), `SELECT method,amount,tendered,COALESCE(reference,''),created_at FROM sale_payments WHERE sale_id=$1 ORDER BY created_at,id`, id)
		if paymentErr == nil {
			for paymentRows.Next() {
				var paymentMethod, reference string
				var amount, tendered float64
				var paymentDate any
				if paymentRows.Scan(&paymentMethod, &amount, &tendered, &reference, &paymentDate) == nil {
					payments = append(payments, map[string]any{"method": paymentMethod, "amount": amount, "tendered": tendered, "reference": reference, "createdAt": paymentDate})
				}
			}
			paymentRows.Close()
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id": id, "invoice": number, "customerId": customerID, "customer": customer, "customerPhone": customerPhone,
			"cashier": cashier, "branch": branch, "total": total, "discount": discount, "paid": paid, "balance": balance,
			"returned": returned, "returnedPaid": returnedPaid, "returnedReceivable": returnedReceivable,
			"netTotal": total - returned, "netPaid": paid - returnedPaid, "netBalance": balance - returnedReceivable,
			"paymentMethod": method, "status": status, "createdAt": createdAt, "cancellationReason": cancellationReason,
			"cancelledAt": cancelledAt, "items": items, "returns": returns, "payments": payments,
		})
	}
}
