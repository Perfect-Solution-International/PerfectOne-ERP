package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type securePOInput struct {
	SupplierID   string            `json:"supplierId"`
	ExpectedDate string            `json:"expectedDate"`
	Notes        string            `json:"notes"`
	RequestID    string            `json:"requestId"`
	Discount     float64           `json:"discount"`
	Tax          float64           `json:"tax"`
	Items        []procurementLine `json:"items"`
}

func validatePOInput(input securePOInput, requireRequest bool) (float64, string) {
	if input.SupplierID == "" || len(input.Items) == 0 || (requireRequest && strings.TrimSpace(input.RequestID) == "") {
		return 0, "supplier, products and request ID are required"
	}
	if input.Discount < 0 || input.Tax < 0 {
		return 0, "discount and tax cannot be negative"
	}
	seen := map[string]bool{}
	total := 0.0
	for _, line := range input.Items {
		if line.ProductID == "" || line.Quantity <= 0 || line.UnitCost < 0 || line.Discount < 0 || line.Tax < 0 || line.Discount > line.Quantity*line.UnitCost+line.Tax {
			return 0, "invalid order quantity, price, discount or tax"
		}
		if line.ManufacturedDate != "" && line.ExpiryDate != "" && line.ManufacturedDate > line.ExpiryDate {
			return 0, "manufactured date cannot be after expiry date"
		}
		key := line.ProductID + "|" + strings.ToLower(strings.TrimSpace(line.BatchNo)) + "|" + line.ExpiryDate
		if seen[key] {
			return 0, "duplicate product, batch and expiry; merge it into one order line"
		}
		seen[key] = true
		total += line.Quantity*line.UnitCost - line.Discount + line.Tax
	}
	total = math.Round((total-input.Discount+input.Tax)*100) / 100
	if total < 0 {
		return 0, "order total cannot be negative"
	}
	return total, ""
}

func validatePOReferences(r *http.Request, tx pgx.Tx, input securePOInput) error {
	c := claimsFrom(r)
	var ok bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM suppliers WHERE id=$1 AND tenant_id=$2 AND is_active)`, input.SupplierID, c.Tenant).Scan(&ok); err != nil || !ok {
		return pgx.ErrNoRows
	}
	for _, line := range input.Items {
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM products p JOIN supplier_products sp ON sp.product_id=p.id AND sp.tenant_id=p.tenant_id WHERE p.id=$1 AND p.tenant_id=$2 AND p.is_active AND sp.supplier_id=$3 AND sp.is_active)`, line.ProductID, c.Tenant, input.SupplierID).Scan(&ok); err != nil || !ok {
			return pgx.ErrNoRows
		}
	}
	return nil
}

func insertPOLines(r *http.Request, tx pgx.Tx, id string, lines []procurementLine) error {
	for _, line := range lines {
		if _, err := tx.Exec(r.Context(), `INSERT INTO purchase_order_items(purchase_order_id,product_id,quantity,unit_cost,discount,tax,total,batch_no,manufactured_date,expiry_date) VALUES($1,$2,$3,$4,$5,$6,$3::numeric*$4::numeric-$5::numeric+$6::numeric,NULLIF($7,''),NULLIF($8,'')::date,NULLIF($9,'')::date)`, id, line.ProductID, line.Quantity, line.UnitCost, line.Discount, line.Tax, strings.TrimSpace(line.BatchNo), line.ManufacturedDate, line.ExpiryDate); err != nil {
			return err
		}
	}
	return nil
}

func secureCreatePO(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input securePOInput
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "invalid order", 400)
			return
		}
		total, message := validatePOInput(input, true)
		if message != "" {
			http.Error(w, message, 400)
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
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='po_create'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}
		if validatePOReferences(r, tx, input) != nil {
			http.Error(w, "active supplier product assignment not found", 409)
			return
		}
		var id, number string
		err = tx.QueryRow(r.Context(), `INSERT INTO purchase_orders(tenant_id,branch_id,po_number,supplier_id,expected_date,discount,tax,total,notes,created_by,client_request_id) VALUES($1,NULLIF($2,'')::uuid,next_purchase_order_no(),$3,NULLIF($4,'')::date,$5,$6,$7,NULLIF($8,''),NULLIF($9,'')::uuid,$10) RETURNING id,po_number`, c.Tenant, c.Branch, input.SupplierID, input.ExpectedDate, input.Discount, input.Tax, total, strings.TrimSpace(input.Notes), c.Sub, input.RequestID).Scan(&id, &number)
		if err == nil {
			err = insertPOLines(r, tx, id, input.Items)
		}
		response, _ := json.Marshal(map[string]any{"id": id, "number": number, "status": "draft", "total": total})
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'po_create',$3,$4)`, c.Tenant, input.RequestID, id, response)
		}
		if err != nil {
			http.Error(w, "purchase order could not be created: "+err.Error(), 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, "purchase order could not be committed: "+err.Error(), 409)
			return
		}
		auditUserAction(r, db, "PURCHASE_ORDER_CREATED", id, map[string]any{"number": number, "total": total})
		w.WriteHeader(201)
		w.Write(response)
	}
}

func secureUpdatePO(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input securePOInput
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "invalid order", 400)
			return
		}
		total, message := validatePOInput(input, false)
		if message != "" {
			http.Error(w, message, 400)
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
		if validatePOReferences(r, tx, input) != nil {
			http.Error(w, "active supplier product assignment not found", 409)
			return
		}
		tag, err := tx.Exec(r.Context(), `UPDATE purchase_orders SET supplier_id=$1,expected_date=NULLIF($2,'')::date,discount=$3,tax=$4,total=$5,notes=NULLIF($6,''),updated_at=now() WHERE id=$7 AND tenant_id=$8 AND status='draft'`, input.SupplierID, input.ExpectedDate, input.Discount, input.Tax, total, strings.TrimSpace(input.Notes), id, c.Tenant)
		if err != nil || tag.RowsAffected() == 0 {
			http.Error(w, "only a draft purchase order can be edited", 409)
			return
		}
		if _, err = tx.Exec(r.Context(), `DELETE FROM purchase_order_items WHERE purchase_order_id=$1`, id); err == nil {
			err = insertPOLines(r, tx, id, input.Items)
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "purchase order could not be updated", 409)
			return
		}
		auditUserAction(r, db, "PURCHASE_ORDER_UPDATED", id, map[string]any{"total": total})
		w.WriteHeader(204)
	}
}

func securePOAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Reason    string `json:"reason"`
			RequestID string `json:"requestId"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		c := claimsFrom(r)
		id, action := r.PathValue("id"), r.PathValue("action")
		if action == "approve" {
			tag, err := db.Exec(r.Context(), `UPDATE purchase_orders SET status='approved',approved_by=NULLIF($2,'')::uuid,approved_at=now(),updated_at=now() WHERE id=$1 AND tenant_id=$3 AND status='draft'`, id, c.Sub, c.Tenant)
			if err != nil || tag.RowsAffected() == 0 {
				http.Error(w, "draft purchase order not found", 409)
				return
			}
			auditUserAction(r, db, "PURCHASE_ORDER_APPROVED", id, map[string]any{})
			w.WriteHeader(204)
			return
		}
		if action == "cancel" {
			input.Reason = strings.TrimSpace(input.Reason)
			if input.Reason == "" {
				http.Error(w, "cancellation reason required", 400)
				return
			}
			tag, err := db.Exec(r.Context(), `UPDATE purchase_orders SET status='cancelled',cancelled_by=NULLIF($2,'')::uuid,cancelled_at=now(),cancellation_reason=$3,notes=concat_ws(E'\n',notes,'Cancelled: '||$3),updated_at=now() WHERE id=$1 AND tenant_id=$4 AND status NOT IN('completed','cancelled') AND NOT EXISTS(SELECT 1 FROM purchases WHERE purchase_order_id=$1 AND status='finalized')`, id, c.Sub, input.Reason, c.Tenant)
			if err != nil || tag.RowsAffected() == 0 {
				http.Error(w, "received or completed purchase order cannot be cancelled", 409)
				return
			}
			auditUserAction(r, db, "PURCHASE_ORDER_CANCELLED", id, map[string]any{"reason": input.Reason})
			w.WriteHeader(204)
			return
		}
		if action != "duplicate" {
			http.Error(w, "unknown action", 404)
			return
		}
		if strings.TrimSpace(input.RequestID) == "" {
			http.Error(w, "request ID is required", 400)
			return
		}
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='po_duplicate'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		var newID, number string
		err = tx.QueryRow(r.Context(), `INSERT INTO purchase_orders(tenant_id,branch_id,po_number,supplier_id,expected_date,discount,tax,total,notes,created_by,client_request_id) SELECT tenant_id,branch_id,next_purchase_order_no(),supplier_id,expected_date,discount,tax,total,concat_ws(E'\n',notes,'Duplicated from '||po_number),NULLIF($2,'')::uuid,$3 FROM purchase_orders WHERE id=$1 AND tenant_id=$4 RETURNING id,po_number`, id, c.Sub, input.RequestID, c.Tenant).Scan(&newID, &number)
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO purchase_order_items(purchase_order_id,product_id,quantity,unit_cost,discount,tax,total,batch_no,manufactured_date,expiry_date) SELECT $1,product_id,quantity,unit_cost,discount,tax,total,batch_no,manufactured_date,expiry_date FROM purchase_order_items WHERE purchase_order_id=$2`, newID, id)
		}
		response, _ := json.Marshal(map[string]any{"id": newID, "number": number, "status": "draft"})
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'po_duplicate',$3,$4)`, c.Tenant, input.RequestID, newID, response)
		}
		if err != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "purchase order could not be duplicated", 409)
			return
		}
		auditUserAction(r, db, "PURCHASE_ORDER_DUPLICATED", newID, map[string]any{"source": id})
		w.WriteHeader(201)
		w.Write(response)
	}
}

func secureConvertPO(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			InvoiceNo     string  `json:"invoiceNo"`
			PaymentMethod string  `json:"paymentMethod"`
			AccountID     string  `json:"accountId"`
			RequestID     string  `json:"requestId"`
			PaidAmount    float64 `json:"paidAmount"`
			Items         []struct {
				PurchaseOrderItemID string  `json:"purchaseOrderItemId"`
				Quantity            float64 `json:"quantity"`
				UnitCost            float64 `json:"unitCost"`
				SellingPrice        float64 `json:"sellingPrice"`
				WholesalePrice      float64 `json:"wholesalePrice"`
				BatchNo             string  `json:"batchNo"`
				ManufacturedDate    string  `json:"manufacturedDate"`
				ExpiryDate          string  `json:"expiryDate"`
			} `json:"items"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.InvoiceNo) == "" || strings.TrimSpace(input.RequestID) == "" {
			http.Error(w, "supplier invoice number and request ID are required", 400)
			return
		}
		if input.PaymentMethod == "" {
			input.PaymentMethod = "credit"
		}
		if input.PaymentMethod != "credit" && input.PaymentMethod != "cash" && input.PaymentMethod != "bank" {
			http.Error(w, "invalid payment method", 400)
			return
		}
		c := claimsFrom(r)
		poID := r.PathValue("id")
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='po_convert_grn'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		var supplier, status, branch string
		var headerDiscount, headerTax, orderedBase float64
		err = tx.QueryRow(r.Context(), `SELECT supplier_id,status,COALESCE(branch_id::text,''),discount,tax,COALESCE((SELECT sum(quantity*unit_cost) FROM purchase_order_items WHERE purchase_order_id=purchase_orders.id),0) FROM purchase_orders WHERE id=$1 AND tenant_id=$2 AND status IN('approved','ordered','partially_received') FOR UPDATE`, poID, c.Tenant).Scan(&supplier, &status, &branch, &headerDiscount, &headerTax, &orderedBase)
		if err != nil {
			http.Error(w, "approved purchase order not found", 409)
			return
		}
		type remainingLine struct {
			id, product, batch, manufactured, expiry                 string
			qty, cost, discount, tax, selling, wholesale, baseWeight float64
			trackExpiry                                              bool
		}
		rows, err := tx.Query(r.Context(), `SELECT i.id,i.product_id,COALESCE(i.batch_no,''),COALESCE(i.manufactured_date::text,''),COALESCE(i.expiry_date::text,''),i.quantity-i.received_quantity-COALESCE((SELECT sum(pi.quantity) FROM purchase_items pi JOIN purchases p ON p.id=pi.purchase_id WHERE pi.purchase_order_item_id=i.id AND p.status='draft'),0),i.unit_cost,CASE WHEN i.quantity=0 THEN 0 ELSE i.discount*(i.quantity-i.received_quantity-COALESCE((SELECT sum(pi.quantity) FROM purchase_items pi JOIN purchases p ON p.id=pi.purchase_id WHERE pi.purchase_order_item_id=i.id AND p.status='draft'),0))/i.quantity END,CASE WHEN i.quantity=0 THEN 0 ELSE i.tax*(i.quantity-i.received_quantity-COALESCE((SELECT sum(pi.quantity) FROM purchase_items pi JOIN purchases p ON p.id=pi.purchase_id WHERE pi.purchase_order_item_id=i.id AND p.status='draft'),0))/i.quantity END,p.selling_price,p.wholesale_price,p.track_expiry FROM purchase_order_items i JOIN products p ON p.id=i.product_id AND p.tenant_id=$2 WHERE i.purchase_order_id=$1 ORDER BY i.id FOR UPDATE OF i`, poID, c.Tenant)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		lines := []remainingLine{}
		requested := map[string]struct {
			quantity, cost, selling, wholesale float64
			batch, manufactured, expiry        string
		}{}
		for _, selected := range input.Items {
			if selected.PurchaseOrderItemID == "" || selected.Quantity <= 0 {
				rows.Close()
				http.Error(w, "each selected PO line needs one positive receiving quantity", 400)
				return
			}
			if _, exists := requested[selected.PurchaseOrderItemID]; exists {
				rows.Close()
				http.Error(w, "a PO line can only be selected once", 400)
				return
			}
			if selected.ManufacturedDate != "" && selected.ExpiryDate != "" && selected.ManufacturedDate > selected.ExpiryDate {
				rows.Close()
				http.Error(w, "manufactured date cannot be after expiry date", 400)
				return
			}
			requested[selected.PurchaseOrderItemID] = struct {
				quantity, cost, selling, wholesale float64
				batch, manufactured, expiry        string
			}{selected.Quantity, selected.UnitCost, selected.SellingPrice, selected.WholesalePrice, strings.TrimSpace(selected.BatchNo), selected.ManufacturedDate, selected.ExpiryDate}
		}
		for rows.Next() {
			var line remainingLine
			if err = rows.Scan(&line.id, &line.product, &line.batch, &line.manufactured, &line.expiry, &line.qty, &line.cost, &line.discount, &line.tax, &line.selling, &line.wholesale, &line.trackExpiry); err != nil {
				rows.Close()
				http.Error(w, err.Error(), 500)
				return
			}
			remaining := line.qty
			line.baseWeight = line.qty * line.cost
			if len(requested) > 0 {
				selection, selected := requested[line.id]
				if !selected {
					continue
				}
				if selection.quantity > remaining {
					rows.Close()
					http.Error(w, "receiving quantity exceeds the remaining PO quantity", 409)
					return
				}
				line.qty = selection.quantity
				line.baseWeight = selection.quantity * line.cost
				if remaining > 0 {
					line.discount = line.discount * selection.quantity / remaining
					line.tax = line.tax * selection.quantity / remaining
				}
				if selection.cost > 0 {
					line.cost = selection.cost
				}
				if selection.selling > 0 {
					line.selling = selection.selling
				}
				if selection.wholesale > 0 {
					line.wholesale = selection.wholesale
				}
				if selection.batch != "" {
					line.batch = selection.batch
				}
				if selection.manufactured != "" {
					line.manufactured = selection.manufactured
				}
				if selection.expiry != "" {
					line.expiry = selection.expiry
				}
			}
			if line.trackExpiry && (line.batch == "" || line.expiry == "") {
				rows.Close()
				http.Error(w, "batch number and expiry date are required for expiry-tracked products", 400)
				return
			}
			if line.expiry != "" && line.expiry < time.Now().Format("2006-01-02") {
				rows.Close()
				http.Error(w, "expired products cannot be received into available stock", 400)
				return
			}
			if line.qty > 0 {
				lines = append(lines, line)
			}
		}
		rows.Close()
		if len(requested) > 0 && len(lines) != len(requested) {
			http.Error(w, "one or more selected PO lines are no longer available", 409)
			return
		}
		if len(lines) == 0 {
			http.Error(w, "all order quantities are received or already reserved by draft GRNs", 409)
			return
		}
		total, receivedWeight := 0.0, 0.0
		for _, line := range lines {
			total += line.qty*line.cost - line.discount + line.tax
			receivedWeight += line.baseWeight
		}
		allocationRatio := 1.0
		if orderedBase > 0 {
			allocationRatio = math.Min(1, receivedWeight/orderedBase)
		}
		allocatedDiscount := math.Round(headerDiscount*allocationRatio*100) / 100
		allocatedTax := math.Round(headerTax*allocationRatio*100) / 100
		total = math.Round((total-allocatedDiscount+allocatedTax)*100) / 100
		if input.PaidAmount < 0 || input.PaidAmount > total || (input.PaidAmount > 0 && (input.PaymentMethod == "credit" || input.AccountID == "")) {
			http.Error(w, "invalid payment amount or account", 400)
			return
		}
		if input.PaidAmount > 0 {
			var kind string
			if err = tx.QueryRow(r.Context(), `SELECT account_type FROM cash_accounts WHERE id=$1 AND tenant_id=$2 AND is_active`, input.AccountID, c.Tenant).Scan(&kind); err != nil || kind != input.PaymentMethod {
				http.Error(w, "selected account does not match payment method", 409)
				return
			}
		}
		var purchaseID string
		err = tx.QueryRow(r.Context(), `INSERT INTO purchases(invoice_no,supplier_id,total,paid_amount,tenant_id,branch_id,purchase_order_id,purchase_date,discount,tax,payment_method,account_id,status,notes,client_request_id) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7,current_date,$8,$9,$10,NULLIF($11,'')::uuid,'draft','Converted from purchase order',$12) RETURNING id`, strings.TrimSpace(input.InvoiceNo), supplier, total, input.PaidAmount, c.Tenant, branch, poID, allocatedDiscount, allocatedTax, input.PaymentMethod, input.AccountID, input.RequestID).Scan(&purchaseID)
		for _, line := range lines {
			if err != nil {
				break
			}
			_, err = tx.Exec(r.Context(), `INSERT INTO purchase_items(purchase_id,product_id,purchase_order_item_id,quantity,unit_cost,selling_price,wholesale_price,total,discount,tax,batch_no,manufactured_date,expiry_date,update_purchase_price,update_selling_price) VALUES($1,$2,$3,$4,$5,$6,$7,$4::numeric*$5::numeric-$8::numeric+$9::numeric,$8,$9,NULLIF($10,''),NULLIF($11,'')::date,NULLIF($12,'')::date,true,true)`, purchaseID, line.product, line.id, line.qty, line.cost, line.selling, line.wholesale, line.discount, line.tax, line.batch, line.manufactured, line.expiry)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE purchase_orders SET status='ordered',updated_at=now() WHERE id=$1 AND tenant_id=$2 AND status='approved'`, poID, c.Tenant)
		}
		response, _ := json.Marshal(map[string]any{"id": purchaseID, "status": "draft", "total": total})
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'po_convert_grn',$3,$4)`, c.Tenant, input.RequestID, purchaseID, response)
		}
		if err != nil {
			http.Error(w, "purchase order conversion failed: "+err.Error(), 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, "purchase order conversion could not be committed: "+err.Error(), 409)
			return
		}
		auditUserAction(r, db, "PURCHASE_ORDER_CONVERTED", poID, map[string]any{"purchaseId": purchaseID})
		w.WriteHeader(201)
		w.Write(response)
	}
}
