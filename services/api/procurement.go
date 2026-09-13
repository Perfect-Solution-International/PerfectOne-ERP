package main

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"net/http"
	"strings"
)

type procurementLine struct {
	ID                  string  `json:"id"`
	ProductID           string  `json:"productId"`
	Quantity            float64 `json:"quantity"`
	EnteredQuantity     float64 `json:"enteredQuantity"`
	EnteredUnitCost     float64 `json:"enteredUnitCost"`
	UnitID              string  `json:"unitId"`
	ConversionFactor    float64 `json:"conversionFactor"`
	FreeQuantity        float64 `json:"freeQuantity"`
	UnitCost            float64 `json:"unitCost"`
	SellingPrice        float64 `json:"sellingPrice"`
	WholesalePrice      float64 `json:"wholesalePrice"`
	Discount            float64 `json:"discount"`
	Tax                 float64 `json:"tax"`
	BatchNo             string  `json:"batchNo"`
	ManufacturedDate    string  `json:"manufacturedDate"`
	ExpiryDate          string  `json:"expiryDate"`
	UpdatePurchasePrice bool    `json:"updatePurchasePrice"`
	UpdateSellingPrice  bool    `json:"updateSellingPrice"`
}
type orderInput struct {
	SupplierID, ExpectedDate, Notes string
	Discount, Tax                   float64
	Items                           []procurementLine
}
type grnInput struct {
	SupplierID, InvoiceNo, PurchaseDate, PaymentMethod, Notes, PurchaseOrderID string
	PaidAmount, Discount, Tax                                                  float64
	Items                                                                      []procurementLine
}

func registerProcurement(m *http.ServeMux, db *pgxpool.Pool) {
	registerSupplierCatalogue(m, db)
	m.HandleFunc("GET /purchase-orders", jsonAPI(authenticated(db, stockRoles...)(listPO(db))))
	m.HandleFunc("POST /purchase-orders", jsonAPI(authenticated(db, stockRoles...)(secureCreatePO(db))))
	m.HandleFunc("PUT /purchase-orders/{id}", jsonAPI(authenticated(db, stockRoles...)(secureUpdatePO(db))))
	m.HandleFunc("DELETE /purchase-orders/{id}", jsonAPI(authenticated(db, stockRoles...)(deletePO(db))))
	m.HandleFunc("POST /purchase-orders/{id}/convert-grn", jsonAPI(authenticated(db, stockRoles...)(secureConvertPO(db))))
	m.HandleFunc("POST /purchase-orders/{id}/{action}", jsonAPI(authenticated(db, stockRoles...)(securePOAction(db))))
	m.HandleFunc("GET /procurement/grns", jsonAPI(authenticated(db, stockRoles...)(listGRN(db))))
	m.HandleFunc("GET /procurement/grns/{id}/detail", jsonAPI(authenticated(db, stockRoles...)(grnDetail(db))))
	m.HandleFunc("GET /purchase-returns", jsonAPI(authenticated(db, stockRoles...)(listReturns(db))))
	m.HandleFunc("POST /purchase-returns", jsonAPI(authenticated(db, stockRoles...)(secureCreatePurchaseReturn(db))))
	m.HandleFunc("PUT /purchase-returns/{id}", jsonAPI(authenticated(db, stockRoles...)(updateReturn(db))))
	m.HandleFunc("DELETE /purchase-returns/{id}", jsonAPI(authenticated(db, stockRoles...)(deleteReturn(db))))
	m.HandleFunc("POST /purchase-returns/{id}/{action}", jsonAPI(authenticated(db, stockRoles...)(securePurchaseReturnAction(db))))
	m.HandleFunc("GET /inventory/movements", jsonAPI(authenticated(db, stockRoles...)(movementHistory(db))))
	m.HandleFunc("GET /inventory/branches", jsonAPI(authenticated(db, stockRoles...)(listBranches(db))))
	m.HandleFunc("POST /inventory/branches", jsonAPI(authenticated(db, management...)(createBranch(db))))
	m.HandleFunc("PUT /inventory/branches/{id}", jsonAPI(authenticated(db, management...)(updateBranch(db))))
	m.HandleFunc("POST /inventory/branches/{id}/status", jsonAPI(authenticated(db, management...)(branchStatus(db))))
	m.HandleFunc("GET /inventory/branches/{id}/stock", jsonAPI(authenticated(db, stockRoles...)(branchStock(db))))
	m.HandleFunc("GET /inventory/transfer-documents", jsonAPI(authenticated(db, stockRoles...)(listTransferDocuments(db))))
	m.HandleFunc("POST /inventory/transfer-documents", jsonAPI(authenticated(db, stockRoles...)(createTransferDocument(db))))
	m.HandleFunc("POST /inventory/transfer-documents/{id}/{action}", jsonAPI(authenticated(db, stockRoles...)(transferDocumentAction(db))))
	m.HandleFunc("POST /inventory/transfer-documents/{id}/partial", jsonAPI(authenticated(db, stockRoles...)(partialTransferDocument(db))))
	m.HandleFunc("GET /inventory/transfer-documents/{id}", jsonAPI(authenticated(db, stockRoles...)(transferDocumentDetail(db))))
	m.HandleFunc("GET /inventory/transfers", jsonAPI(authenticated(db, stockRoles...)(listTransfers(db))))
	m.HandleFunc("POST /inventory/transfers", jsonAPI(authenticated(db, stockRoles...)(secureCreateTransfer(db))))
	m.HandleFunc("POST /inventory/transfers/{id}/{action}", jsonAPI(authenticated(db, stockRoles...)(secureTransferAction(db))))
	m.HandleFunc("PUT /inventory/batches/{id}", jsonAPI(authenticated(db, stockRoles...)(updateBatch(db))))
	m.HandleFunc("POST /inventory/batches/{id}/expire", jsonAPI(authenticated(db, stockRoles...)(secureExpireBatch(db))))
	m.HandleFunc("POST /inventory/damages", jsonAPI(authenticated(db, stockRoles...)(secureCreateDamage(db))))
	m.HandleFunc("POST /inventory/damages/{id}/{action}", jsonAPI(authenticated(db, stockRoles...)(secureDamageAction(db))))
	m.HandleFunc("GET /procurement/audit", jsonAPI(authenticated(db, stockRoles...)(procurementAudit(db))))
}

func procurementAudit(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityID := r.URL.Query().Get("entityId")
		if entityID == "" {
			http.Error(w, "entity ID is required", 400)
			return
		}
		rows, err := db.Query(r.Context(), `SELECT a.id,a.action,COALESCE(u.name,'System'),a.created_at,COALESCE(a.old_data,'{}'::jsonb),COALESCE(a.new_data,'{}'::jsonb) FROM audit_logs a LEFT JOIN users u ON u.id=a.user_id WHERE a.tenant_id=$1 AND a.entity_id=NULLIF($2,'')::uuid ORDER BY a.created_at DESC`, claimsFrom(r).Tenant, entityID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, action, user string
			var at, oldData, newData any
			if rows.Scan(&id, &action, &user, &at, &oldData, &newData) != nil {
				continue
			}
			out = append(out, map[string]any{"id": id, "action": action, "user": user, "at": at, "old": oldData, "new": newData})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func orderTotal(x orderInput) float64 {
	n := 0.0
	for _, l := range x.Items {
		n += l.Quantity*l.UnitCost - l.Discount + l.Tax
	}
	return n - x.Discount + x.Tax
}
func listPO(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT o.id,o.po_number,s.name,o.order_date,o.expected_date,o.total,o.discount,o.tax,o.status,COALESCE(o.notes,''),COALESCE((SELECT json_agg(json_build_object('id',i.id,'productId',i.product_id,'product',p.name,'quantity',i.quantity,'received',i.received_quantity,'reserved',COALESCE((SELECT sum(pi.quantity) FROM purchase_items pi JOIN purchases pr ON pr.id=pi.purchase_id WHERE pi.purchase_order_item_id=i.id AND pr.status='draft'),0),'unitCost',i.unit_cost,'sellingPrice',p.selling_price,'wholesalePrice',p.wholesale_price,'trackExpiry',p.track_expiry,'discount',i.discount,'tax',i.tax,'batchNo',COALESCE(i.batch_no,''),'manufacturedDate',i.manufactured_date,'expiryDate',i.expiry_date) ORDER BY i.id) FROM purchase_order_items i JOIN products p ON p.id=i.product_id WHERE i.purchase_order_id=o.id),'[]') FROM purchase_orders o JOIN suppliers s ON s.id=o.supplier_id WHERE o.tenant_id=$1 ORDER BY o.created_at DESC`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, no, supplier, status, notes string
			var date, expected, items any
			var total, discount, tax float64
			rows.Scan(&id, &no, &supplier, &date, &expected, &total, &discount, &tax, &status, &notes, &items)
			out = append(out, map[string]any{"id": id, "number": no, "supplier": supplier, "date": date, "expectedDate": expected, "total": total, "discount": discount, "tax": tax, "status": status, "notes": notes, "items": items})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func writePO(r *http.Request, db *pgxpool.Pool, id string, x orderInput) error {
	if x.SupplierID == "" || len(x.Items) == 0 {
		return fmt.Errorf("supplier and items required")
	}
	total := orderTotal(x)
	tx, e := db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if id == "" {
		e = tx.QueryRow(r.Context(), `INSERT INTO purchase_orders(tenant_id,branch_id,po_number,supplier_id,expected_date,discount,tax,total,notes,created_by)VALUES($1,NULLIF($2,'')::uuid,'PO-'||to_char(clock_timestamp(),'YYYYMMDDHH24MISSMS'),$3,NULLIF($4,'')::date,$5,$6,$7,$8,$9)RETURNING id`, claimsFrom(r).Tenant, claimsFrom(r).Branch, x.SupplierID, x.ExpectedDate, x.Discount, x.Tax, total, x.Notes, claimsFrom(r).Sub).Scan(&id)
	} else {
		tag, er := tx.Exec(r.Context(), `UPDATE purchase_orders SET supplier_id=$1,expected_date=NULLIF($2,'')::date,discount=$3,tax=$4,total=$5,notes=$6,updated_at=now() WHERE id=$7 AND tenant_id=$8 AND status='draft'`, x.SupplierID, x.ExpectedDate, x.Discount, x.Tax, total, x.Notes, id, claimsFrom(r).Tenant)
		e = er
		if e == nil && tag.RowsAffected() == 0 {
			return fmt.Errorf("only draft orders can be edited")
		}
		if e == nil {
			_, e = tx.Exec(r.Context(), `DELETE FROM purchase_order_items WHERE purchase_order_id=$1`, id)
		}
	}
	if e != nil {
		return e
	}
	for _, l := range x.Items {
		if l.ProductID == "" || l.Quantity <= 0 || l.UnitCost < 0 {
			return fmt.Errorf("invalid order item")
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO purchase_order_items(purchase_order_id,product_id,quantity,unit_cost,discount,tax,total)VALUES($1,$2,$3,$4,$5,$6,$3::numeric*$4::numeric-$5::numeric+$6::numeric)`, id, l.ProductID, l.Quantity, l.UnitCost, l.Discount, l.Tax)
		if e != nil {
			return e
		}
	}
	return tx.Commit(r.Context())
}
func createPO(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x orderInput
		if json.NewDecoder(r.Body).Decode(&x) != nil {
			http.Error(w, "invalid order", 400)
			return
		}
		if e := writePO(r, db, "", x); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		w.WriteHeader(201)
	}
}
func updatePO(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x orderInput
		if json.NewDecoder(r.Body).Decode(&x) != nil {
			http.Error(w, "invalid order", 400)
			return
		}
		if e := writePO(r, db, r.PathValue("id"), x); e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		w.WriteHeader(204)
	}
}
func deletePO(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag, e := db.Exec(r.Context(), `DELETE FROM purchase_orders WHERE id=$1 AND tenant_id=$2 AND status='draft'`, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "only draft orders can be deleted", 409)
			return
		}
		w.WriteHeader(204)
	}
}
func poAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, a := r.PathValue("id"), r.PathValue("action")
		c := claimsFrom(r)
		switch a {
		case "approve":
			tag, e := db.Exec(r.Context(), `UPDATE purchase_orders SET status='approved',approved_by=$2,approved_at=now(),updated_at=now() WHERE id=$1 AND tenant_id=$3 AND status='draft'`, id, c.Sub, c.Tenant)
			if e != nil || tag.RowsAffected() == 0 {
				http.Error(w, "draft order not found", 409)
				return
			}
		case "cancel":
			var x struct {
				Reason string `json:"reason"`
			}
			json.NewDecoder(r.Body).Decode(&x)
			if strings.TrimSpace(x.Reason) == "" {
				http.Error(w, "cancellation reason required", 400)
				return
			}
			tag, e := db.Exec(r.Context(), `UPDATE purchase_orders SET status='cancelled',cancelled_by=$2,cancelled_at=now(),notes=concat_ws(E'\n',notes,'Cancelled: '||$3),updated_at=now() WHERE id=$1 AND tenant_id=$4 AND status NOT IN('completed','cancelled')`, id, c.Sub, x.Reason, c.Tenant)
			if e != nil || tag.RowsAffected() == 0 {
				http.Error(w, "order cannot be cancelled", 409)
				return
			}
		case "duplicate":
			_, e := db.Exec(r.Context(), `WITH n AS(INSERT INTO purchase_orders(tenant_id,branch_id,po_number,supplier_id,expected_date,discount,tax,total,notes,created_by) SELECT tenant_id,branch_id,'PO-'||to_char(clock_timestamp(),'YYYYMMDDHH24MISSMS'),supplier_id,expected_date,discount,tax,total,notes,$2 FROM purchase_orders WHERE id=$1 AND tenant_id=$3 RETURNING id),x AS(SELECT id FROM n) INSERT INTO purchase_order_items(purchase_order_id,product_id,quantity,unit_cost,discount,tax,total)SELECT x.id,i.product_id,i.quantity,i.unit_cost,i.discount,i.tax,i.total FROM purchase_order_items i,x WHERE i.purchase_order_id=$1`, id, c.Sub, c.Tenant)
			if e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
		default:
			http.Error(w, "unknown action", 404)
			return
		}
		w.WriteHeader(204)
	}
}
func convertPO(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct{ InvoiceNo, PaymentMethod string }
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.InvoiceNo == "" {
			http.Error(w, "supplier invoice number required", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var supplier string
		var discount, tax, total float64
		e = tx.QueryRow(r.Context(), `SELECT supplier_id,discount,tax,total FROM purchase_orders WHERE id=$1 AND tenant_id=$2 AND status IN('approved','ordered','partially_received') FOR UPDATE`, r.PathValue("id"), c.Tenant).Scan(&supplier, &discount, &tax, &total)
		if e != nil {
			http.Error(w, "approved order not found", 409)
			return
		}
		var grn string
		e = tx.QueryRow(r.Context(), `INSERT INTO purchases(invoice_no,supplier_id,total,tenant_id,purchase_order_id,discount,tax,payment_method,status,notes)VALUES($1,$2,$3,$4,$5,$6,$7,COALESCE(NULLIF($8,''),'credit'),'draft','Converted from purchase order')RETURNING id`, x.InvoiceNo, supplier, total, c.Tenant, r.PathValue("id"), discount, tax, x.PaymentMethod).Scan(&grn)
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO purchase_items(purchase_id,product_id,quantity,unit_cost,total,discount,tax)SELECT $1,product_id,quantity-received_quantity,unit_cost,(quantity-received_quantity)*unit_cost-discount+tax,discount,tax FROM purchase_order_items WHERE purchase_order_id=$2 AND quantity>received_quantity`, grn, r.PathValue("id"))
		}
		if e == nil {
			_, e = tx.Exec(r.Context(), `UPDATE purchase_orders SET status='ordered',updated_at=now() WHERE id=$1`, r.PathValue("id"))
		}
		if e != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "PO conversion failed", 400)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": grn})
	}
}
func listGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT p.id,p.invoice_no,s.name,p.purchase_date,p.total,p.paid_amount+COALESCE((SELECT sum(a.amount) FROM purchase_payment_allocations a JOIN supplier_payments sp ON sp.id=a.supplier_payment_id WHERE a.purchase_id=p.id AND sp.status='finalized'),0),p.payment_method,p.status,COALESCE(p.notes,'') FROM purchases p JOIN suppliers s ON s.id=p.supplier_id WHERE p.tenant_id=$1 ORDER BY p.created_at DESC`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, no, supplier, method, status, notes string
			var date any
			var total, paid float64
			rows.Scan(&id, &no, &supplier, &date, &total, &paid, &method, &status, &notes)
			out = append(out, map[string]any{"id": id, "invoice": no, "supplier": supplier, "date": date, "total": total, "paid": paid, "method": method, "status": status, "notes": notes})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func saveGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x grnInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.SupplierID == "" || x.InvoiceNo == "" || len(x.Items) == 0 {
			http.Error(w, "invoice, supplier and items required", 400)
			return
		}
		total := 0.0
		for _, l := range x.Items {
			if l.ProductID == "" || l.Quantity <= 0 || l.UnitCost < 0 {
				http.Error(w, "invalid purchase item", 400)
				return
			}
			total += l.Quantity*l.UnitCost - l.Discount + l.Tax
		}
		total = total - x.Discount + x.Tax
		if total < 0 || x.PaidAmount < 0 || x.PaidAmount > total {
			http.Error(w, "invalid totals or paid amount", 400)
			return
		}
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var id string
		e = tx.QueryRow(r.Context(), `INSERT INTO purchases(invoice_no,supplier_id,total,paid_amount,tenant_id,purchase_order_id,purchase_date,discount,tax,payment_method,status,notes)VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,COALESCE(NULLIF($7,'')::date,current_date),$8,$9,COALESCE(NULLIF($10,''),'credit'),'draft',$11)RETURNING id`, x.InvoiceNo, x.SupplierID, total, x.PaidAmount, claimsFrom(r).Tenant, x.PurchaseOrderID, x.PurchaseDate, x.Discount, x.Tax, x.PaymentMethod, x.Notes).Scan(&id)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		for _, l := range x.Items {
			_, e = tx.Exec(r.Context(), `INSERT INTO purchase_items(purchase_id,product_id,quantity,unit_cost,expiry_date,total,discount,tax,batch_no)VALUES($1,$2,$3,$4,NULLIF($5,'')::date,$3::numeric*$4::numeric-$6::numeric+$7::numeric,$6,$7,NULLIF($8,''))`, id, l.ProductID, l.Quantity, l.UnitCost, l.ExpiryDate, l.Discount, l.Tax, l.BatchNo)
			if e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, "GRN save failed", 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func editGRN(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x grnInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.SupplierID == "" || x.InvoiceNo == "" || len(x.Items) == 0 {
			http.Error(w, "invoice, supplier and items required", 400)
			return
		}
		total := 0.0
		for _, l := range x.Items {
			total += l.Quantity*l.UnitCost - l.Discount + l.Tax
		}
		total = total - x.Discount + x.Tax
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		tag, e := tx.Exec(r.Context(), `UPDATE purchases SET invoice_no=$1,supplier_id=$2,total=$3,paid_amount=$4,purchase_date=COALESCE(NULLIF($5,'')::date,current_date),discount=$6,tax=$7,payment_method=$8,notes=$9 WHERE id=$10 AND tenant_id=$11 AND status='draft'`, x.InvoiceNo, x.SupplierID, total, x.PaidAmount, x.PurchaseDate, x.Discount, x.Tax, x.PaymentMethod, x.Notes, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "only draft GRNs can be edited", 409)
			return
		}
		tx.Exec(r.Context(), `DELETE FROM purchase_items WHERE purchase_id=$1`, r.PathValue("id"))
		for _, l := range x.Items {
			_, e = tx.Exec(r.Context(), `INSERT INTO purchase_items(purchase_id,product_id,quantity,unit_cost,expiry_date,total,discount,tax,batch_no)VALUES($1,$2,$3,$4,NULLIF($5,'')::date,$3::numeric*$4::numeric-$6::numeric+$7::numeric,$6,$7,NULLIF($8,''))`, r.PathValue("id"), l.ProductID, l.Quantity, l.UnitCost, l.ExpiryDate, l.Discount, l.Tax, l.BatchNo)
			if e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
		}
		if tx.Commit(r.Context()) != nil {
			http.Error(w, "GRN update failed", 500)
			return
		}
		w.WriteHeader(204)
	}
}
func grnAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, a := r.PathValue("id"), r.PathValue("action")
		if a != "finalize" && a != "reverse" {
			http.Error(w, "unknown action", 404)
			return
		}
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		c := claimsFrom(r)
		var supplier, status string
		var total, paid float64
		e = tx.QueryRow(r.Context(), `SELECT supplier_id,status,total,paid_amount FROM purchases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, c.Tenant).Scan(&supplier, &status, &total, &paid)
		if e != nil {
			http.Error(w, "GRN not found", 404)
			return
		}
		sign := 1.0
		next := "finalized"
		if a == "finalize" && status != "draft" {
			http.Error(w, "only draft GRNs can be finalized", 409)
			return
		}
		if a == "reverse" {
			if status != "finalized" {
				http.Error(w, "only finalized GRNs can be reversed", 409)
				return
			}
			sign = -1
			next = "reversed"
		}
		rows, e := tx.Query(r.Context(), `SELECT product_id,quantity,unit_cost,COALESCE(batch_no,''),COALESCE(expiry_date::text,'') FROM purchase_items WHERE purchase_id=$1`, id)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		type line struct {
			p, b, expiry string
			q, cost      float64
		}
		lines := []line{}
		for rows.Next() {
			var l line
			rows.Scan(&l.p, &l.q, &l.cost, &l.b, &l.expiry)
			lines = append(lines, l)
		}
		rows.Close()
		for _, l := range lines {
			var bal float64
			e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2,purchase_price=CASE WHEN $2>0 THEN $3 ELSE purchase_price END WHERE id=$1 AND tenant_id=$4 AND stock_quantity+$2>=0 RETURNING stock_quantity`, l.p, l.q*sign, l.cost, c.Tenant).Scan(&bal)
			if e != nil {
				http.Error(w, "insufficient stock to reverse GRN", 409)
				return
			}
			kind := "purchase"
			if sign < 0 {
				kind = "purchase_reversal"
			}
			_, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,created_by)VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,'purchase',$6,$7,$8)`, l.p, kind, l.q*sign, c.Tenant, c.Branch, id, bal, c.Sub)
			if e == nil && sign > 0 {
				_, e = tx.Exec(r.Context(), `INSERT INTO stock_batches(product_id,batch_no,expiry_date,available_qty,unit_cost)VALUES($1,NULLIF($2,''),NULLIF($3,'')::date,$4,$5)`, l.p, l.b, l.expiry, l.q, l.cost)
			}
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		balanceChange := (total - paid) * sign
		_, e = tx.Exec(r.Context(), `UPDATE suppliers SET balance=GREATEST(balance+$2,0) WHERE id=$1`, supplier, balanceChange)
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO supplier_ledger(supplier_id,entry_type,reference_id,debit,credit)VALUES($1,$2,$3,$4,$5)`, supplier, fmt.Sprintf("purchase_%s", a), id, max(balanceChange, 0), max(-balanceChange, 0))
		}
		if e == nil {
			_, e = tx.Exec(r.Context(), `UPDATE purchases SET status=$2,finalized_by=CASE WHEN $2='finalized' THEN $3 ELSE finalized_by END,reversed_by=CASE WHEN $2='reversed' THEN $3 ELSE NULL END,reversed_at=CASE WHEN $2='reversed' THEN now() ELSE NULL END WHERE id=$1`, id, next, c.Sub)
		}
		if e != nil {
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
func listReturns(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT r.id,r.return_number,p.invoice_no,s.name,r.return_date,r.total,r.status,r.reason FROM purchase_returns r JOIN purchases p ON p.id=r.purchase_id JOIN suppliers s ON s.id=r.supplier_id WHERE r.tenant_id=$1 ORDER BY r.created_at DESC`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, no, invoice, supplier, status, reason string
			var date any
			var total float64
			rows.Scan(&id, &no, &invoice, &supplier, &date, &total, &status, &reason)
			out = append(out, map[string]any{"id": id, "number": no, "invoice": invoice, "supplier": supplier, "date": date, "total": total, "status": status, "reason": reason})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func createReturn(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			PurchaseID, Reason string
			Items              []procurementLine
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.PurchaseID == "" || x.Reason == "" || len(x.Items) == 0 {
			http.Error(w, "purchase, reason and items required", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var supplier, id string
		e = tx.QueryRow(r.Context(), `SELECT supplier_id FROM purchases WHERE id=$1 AND tenant_id=$2 AND status='finalized'`, x.PurchaseID, c.Tenant).Scan(&supplier)
		if e == nil {
			e = tx.QueryRow(r.Context(), `INSERT INTO purchase_returns(tenant_id,purchase_id,return_number,supplier_id,reason,created_by)VALUES($1,$2,'PR-'||to_char(clock_timestamp(),'YYYYMMDDHH24MISSMS'),$3,$4,$5)RETURNING id`, c.Tenant, x.PurchaseID, supplier, x.Reason, c.Sub).Scan(&id)
		}
		total := 0.0
		for _, l := range x.Items {
			var product string
			var cost, purchased, returned float64
			e = tx.QueryRow(r.Context(), `SELECT i.product_id,i.unit_cost,i.quantity,COALESCE((SELECT sum(ri.quantity) FROM purchase_return_items ri JOIN purchase_returns rr ON rr.id=ri.purchase_return_id WHERE ri.purchase_item_id=i.id AND rr.status='finalized'),0) FROM purchase_items i WHERE i.id=$1 AND i.purchase_id=$2`, l.ID, x.PurchaseID).Scan(&product, &cost, &purchased, &returned)
			if e != nil || l.Quantity <= 0 || l.Quantity > purchased-returned {
				http.Error(w, "invalid return quantity", 409)
				return
			}
			lineTotal := l.Quantity * cost
			total += lineTotal
			_, e = tx.Exec(r.Context(), `INSERT INTO purchase_return_items(purchase_return_id,purchase_item_id,product_id,quantity,unit_cost,total,entered_quantity,entered_unit_id,entered_unit_cost,conversion_factor) SELECT $1,$2,$3,$4,$5,$6,$4/NULLIF(COALESCE(i.conversion_factor,1),0),i.entered_unit_id,i.entered_unit_cost,COALESCE(i.conversion_factor,1) FROM purchase_items i WHERE i.id=$2`, id, l.ID, product, l.Quantity, cost, lineTotal)
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		_, e = tx.Exec(r.Context(), `UPDATE purchase_returns SET total=$2 WHERE id=$1`, id, total)
		if e != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "return save failed", 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func returnAction(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, a := r.PathValue("id"), r.PathValue("action")
		if a == "cancel" {
			tag, _ := db.Exec(r.Context(), `UPDATE purchase_returns SET status='cancelled',cancelled_by=$2,cancelled_at=now() WHERE id=$1 AND tenant_id=$3 AND status='draft'`, id, claimsFrom(r).Sub, claimsFrom(r).Tenant)
			if tag.RowsAffected() == 0 {
				http.Error(w, "draft return not found", 409)
				return
			}
			w.WriteHeader(204)
			return
		}
		if a != "finalize" {
			http.Error(w, "unknown action", 404)
			return
		}
		c := claimsFrom(r)
		tx, _ := db.Begin(r.Context())
		defer tx.Rollback(r.Context())
		var supplier string
		var total float64
		e := tx.QueryRow(r.Context(), `SELECT supplier_id,total FROM purchase_returns WHERE id=$1 AND tenant_id=$2 AND status='draft' FOR UPDATE`, id, c.Tenant).Scan(&supplier, &total)
		if e != nil {
			http.Error(w, "draft return not found", 409)
			return
		}
		rows, _ := tx.Query(r.Context(), `SELECT product_id,quantity FROM purchase_return_items WHERE purchase_return_id=$1`, id)
		type l struct {
			p string
			q float64
		}
		lines := []l{}
		for rows.Next() {
			var x l
			rows.Scan(&x.p, &x.q)
			lines = append(lines, x)
		}
		rows.Close()
		for _, x := range lines {
			var bal float64
			e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND stock_quantity>=$2 RETURNING stock_quantity`, x.p, x.q).Scan(&bal)
			if e != nil {
				http.Error(w, "insufficient stock for supplier return", 409)
				return
			}
			tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,created_by)VALUES($1,'purchase_return',$2,$3,NULLIF($4,'')::uuid,'purchase_return',$5,$6,$7)`, x.p, -x.q, c.Tenant, c.Branch, id, bal, c.Sub)
		}
		tx.Exec(r.Context(), `UPDATE suppliers SET balance=GREATEST(balance-$2,0) WHERE id=$1`, supplier, total)
		tx.Exec(r.Context(), `INSERT INTO supplier_ledger(supplier_id,entry_type,reference_id,credit)VALUES($1,'purchase_return',$2,$3)`, supplier, id, total)
		tx.Exec(r.Context(), `UPDATE purchase_returns SET status='finalized',finalized_by=$2,finalized_at=now() WHERE id=$1`, id, c.Sub)
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(204)
	}
}

func grnDetail(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		var id, invoice, supplierID, supplier, date, method, accountID, status, notes, poID string
		var total, paid, discount, tax float64
		var initialPaid float64
		e := db.QueryRow(r.Context(), `SELECT p.id,p.invoice_no,p.supplier_id,s.name,p.purchase_date::text,p.payment_method,COALESCE(p.account_id::text,''),p.status,COALESCE(p.notes,''),COALESCE(p.purchase_order_id::text,''),p.total,p.paid_amount,p.paid_amount+COALESCE((SELECT sum(a.amount) FROM purchase_payment_allocations a JOIN supplier_payments sp ON sp.id=a.supplier_payment_id WHERE a.purchase_id=p.id AND sp.status='finalized'),0),p.discount,p.tax FROM purchases p JOIN suppliers s ON s.id=p.supplier_id WHERE p.id=$1 AND p.tenant_id=$2`, r.PathValue("id"), c.Tenant).Scan(&id, &invoice, &supplierID, &supplier, &date, &method, &accountID, &status, &notes, &poID, &total, &initialPaid, &paid, &discount, &tax)
		if e != nil {
			http.Error(w, "GRN not found", 404)
			return
		}
		rows, e := db.Query(r.Context(), `SELECT i.id,i.product_id,p.name,i.quantity+i.free_quantity,i.quantity,i.free_quantity,i.unit_cost,i.selling_price,i.wholesale_price,i.discount,i.tax,COALESCE(i.batch_no,''),COALESCE(i.manufactured_date::text,''),COALESCE(i.expiry_date::text,''),i.update_purchase_price,i.update_selling_price,COALESCE((SELECT sum(ri.quantity) FROM purchase_return_items ri JOIN purchase_returns rr ON rr.id=ri.purchase_return_id WHERE ri.purchase_item_id=i.id AND rr.status<>'cancelled'),0),COALESCE(i.entered_quantity,i.quantity),COALESCE(u.symbol,''),COALESCE(i.entered_unit_cost,i.unit_cost),COALESCE(i.conversion_factor,1) FROM purchase_items i JOIN products p ON p.id=i.product_id LEFT JOIN units u ON u.id=i.entered_unit_id WHERE i.purchase_id=$1 ORDER BY i.id`, id)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var lineID, productID, name, batch, manufactured, expiry, unitSymbol string
			var qty, purchaseQty, freeQty, cost, selling, wholesale, lineDiscount, lineTax, returned, enteredQuantity, enteredUnitCost, conversionFactor float64
			var updateCost, updateSale bool
			if rows.Scan(&lineID, &productID, &name, &qty, &purchaseQty, &freeQty, &cost, &selling, &wholesale, &lineDiscount, &lineTax, &batch, &manufactured, &expiry, &updateCost, &updateSale, &returned, &enteredQuantity, &unitSymbol, &enteredUnitCost, &conversionFactor) != nil {
				http.Error(w, "could not read GRN items", 500)
				return
			}
			items = append(items, map[string]any{"id": lineID, "productId": productID, "product": name, "quantity": qty, "purchaseQuantity": purchaseQty, "freeQuantity": freeQty, "returnedQuantity": returned, "availableQuantity": qty - returned, "unitCost": cost, "enteredQuantity": enteredQuantity, "unitSymbol": unitSymbol, "enteredUnitCost": enteredUnitCost, "conversionFactor": conversionFactor, "sellingPrice": selling, "wholesalePrice": wholesale, "discount": lineDiscount, "tax": lineTax, "batchNo": batch, "manufacturedDate": manufactured, "expiryDate": expiry, "updatePurchasePrice": updateCost, "updateSellingPrice": updateSale})
		}
		json.NewEncoder(w).Encode(map[string]any{"id": id, "invoiceNo": invoice, "supplierId": supplierID, "supplier": supplier, "purchaseDate": date, "paymentMethod": method, "accountId": accountID, "status": status, "notes": notes, "purchaseOrderId": poID, "total": total, "initialPaidAmount": initialPaid, "paidAmount": paid, "dueAmount": math.Max(0, total-paid), "discount": discount, "tax": tax, "items": items})
	}
}

func updateReturn(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			PurchaseID, Reason string
			Items              []procurementLine
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Reason) == "" || len(x.Items) == 0 {
			http.Error(w, "purchase, reason and items required", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var purchaseID string
		e = tx.QueryRow(r.Context(), `SELECT purchase_id FROM purchase_returns WHERE id=$1 AND tenant_id=$2 AND status='draft' FOR UPDATE`, r.PathValue("id"), c.Tenant).Scan(&purchaseID)
		if e != nil {
			http.Error(w, "only draft returns can be edited", 409)
			return
		}
		if x.PurchaseID != "" && x.PurchaseID != purchaseID {
			http.Error(w, "purchase cannot be changed", 409)
			return
		}
		if _, e = tx.Exec(r.Context(), `DELETE FROM purchase_return_items WHERE purchase_return_id=$1`, r.PathValue("id")); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		total := 0.0
		for _, l := range x.Items {
			var product string
			var cost, purchased, returned float64
			e = tx.QueryRow(r.Context(), `SELECT i.product_id,i.unit_cost,i.quantity,COALESCE((SELECT sum(ri.quantity) FROM purchase_return_items ri JOIN purchase_returns rr ON rr.id=ri.purchase_return_id WHERE ri.purchase_item_id=i.id AND rr.status='finalized'),0) FROM purchase_items i WHERE i.id=$1 AND i.purchase_id=$2`, l.ID, purchaseID).Scan(&product, &cost, &purchased, &returned)
			if e != nil || l.Quantity <= 0 || l.Quantity > purchased-returned {
				http.Error(w, "invalid return quantity", 409)
				return
			}
			lineTotal := l.Quantity * cost
			total += lineTotal
			if _, e = tx.Exec(r.Context(), `INSERT INTO purchase_return_items(purchase_return_id,purchase_item_id,product_id,quantity,unit_cost,total,entered_quantity,entered_unit_id,entered_unit_cost,conversion_factor) SELECT $1,$2,$3,$4,$5,$6,$4/NULLIF(COALESCE(i.conversion_factor,1),0),i.entered_unit_id,i.entered_unit_cost,COALESCE(i.conversion_factor,1) FROM purchase_items i WHERE i.id=$2`, r.PathValue("id"), l.ID, product, l.Quantity, cost, lineTotal); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if _, e = tx.Exec(r.Context(), `UPDATE purchase_returns SET reason=$2,total=$3 WHERE id=$1`, r.PathValue("id"), x.Reason, total); e != nil {
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

func deleteReturn(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag, e := db.Exec(r.Context(), `DELETE FROM purchase_returns WHERE id=$1 AND tenant_id=$2 AND status='draft'`, r.PathValue("id"), claimsFrom(r).Tenant)
		if e != nil || tag.RowsAffected() == 0 {
			http.Error(w, "only draft returns can be deleted", 409)
			return
		}
		w.WriteHeader(204)
	}
}
