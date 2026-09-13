package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type saleBatchAllocation struct {
	id       string
	quantity float64
	unitCost float64
}

func weightedInventoryCost(quantity, fallbackCost float64, allocations []saleBatchAllocation) float64 {
	if quantity <= 0 {
		return 0
	}
	allocated, totalCost := 0.0, 0.0
	for _, item := range allocations {
		allocated += item.quantity
		totalCost += item.quantity * item.unitCost
	}
	totalCost += math.Max(0, quantity-allocated) * fallbackCost
	return math.Round(totalCost/quantity*10000) / 10000
}

// prepareFEFOAllocation locks the exact batches that will fund a sale and derives
// the weighted historical cost before sale_items is inserted. This lets the
// sale_items COGS trigger post the actual batch cost instead of today's product
// default purchase price.
func prepareFEFOAllocation(ctx context.Context, tx pgx.Tx, productID, branchID string, quantity, fallbackCost float64, required bool) ([]saleBatchAllocation, float64, error) {
	rows, err := tx.Query(ctx, `SELECT id,available_qty,unit_cost FROM stock_batches WHERE product_id=$1 AND branch_id=NULLIF($2,'')::uuid AND status='available' AND available_qty>0 AND (expiry_date IS NULL OR expiry_date>=current_date) ORDER BY expiry_date NULLS LAST,received_at,id FOR UPDATE`, productID, branchID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	remaining := quantity
	allocations := []saleBatchAllocation{}
	for rows.Next() && remaining > 0 {
		var id string
		var available, unitCost float64
		if err = rows.Scan(&id, &available, &unitCost); err != nil {
			return nil, 0, err
		}
		used := math.Min(available, remaining)
		allocations = append(allocations, saleBatchAllocation{id: id, quantity: used, unitCost: unitCost})
		remaining -= used
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	if required && remaining > 0.000001 {
		return nil, 0, fmt.Errorf("insufficient unexpired batch stock in selected branch")
	}
	// Any non-batch remainder retains the product's current inventory cost.
	return allocations, weightedInventoryCost(quantity, fallbackCost, allocations), nil
}

func applyFEFOAllocation(ctx context.Context, tx pgx.Tx, saleItemID string, allocations []saleBatchAllocation) error {
	for _, item := range allocations {
		if _, err := tx.Exec(ctx, `UPDATE stock_batches SET available_qty=available_qty-$2 WHERE id=$1`, item.id, item.quantity); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sale_item_batches(sale_item_id,batch_id,quantity) VALUES($1,$2,$3)`, saleItemID, item.id, item.quantity); err != nil {
			return err
		}
	}
	return nil
}

func lockOpenCashierSession(ctx context.Context, tx pgx.Tx, claims Claims) (string, error) {
	var sessionID string
	err := tx.QueryRow(ctx, `SELECT id FROM cashier_sessions WHERE tenant_id=$1 AND user_id=$2 AND branch_id=NULLIF($3,'')::uuid AND closed_at IS NULL ORDER BY opened_at DESC LIMIT 1 FOR UPDATE`, claims.Tenant, claims.Sub, claims.Branch).Scan(&sessionID)
	return sessionID, err
}

type saleReturnInput struct {
	Reason    string `json:"reason"`
	RequestID string `json:"requestId"`
	Items     []struct {
		SaleItemID string  `json:"saleItemId"`
		Quantity   float64 `json:"quantity"`
	} `json:"items"`
}

func proratedSaleReturnAmount(returnQty, soldQty, lineNet, invoiceTotal, invoiceLineNetTotal float64) float64 {
	if returnQty <= 0 || soldQty <= 0 || lineNet <= 0 || invoiceLineNetTotal <= 0 {
		return 0
	}
	return returnQty / soldQty * lineNet * invoiceTotal / invoiceLineNetTotal
}

func secureReturnSale(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input saleReturnInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.RequestID) == "" || len(input.Items) == 0 {
			http.Error(w, "reason, request ID and return items are required", http.StatusBadRequest)
			return
		}
		c := claimsFrom(r)
		saleID := r.PathValue("id")
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='sale_return'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}

		var customerID, method, status, branch string
		var saleTotal, saleBalance, alreadyReturned float64
		err = tx.QueryRow(r.Context(), `SELECT COALESCE(customer_id::text,''),payment_method,status,total,balance,returned_total,COALESCE(branch_id::text,'') FROM sales WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, saleID, c.Tenant).Scan(&customerID, &method, &status, &saleTotal, &saleBalance, &alreadyReturned, &branch)
		if err != nil || (status != "completed" && status != "partial_returned") {
			http.Error(w, "returnable sale not found", http.StatusConflict)
			return
		}
		if branch == "" || branch != c.Branch {
			http.Error(w, "sale returns must be processed in the original sale branch", http.StatusConflict)
			return
		}
		cashierSessionID, sessionErr := lockOpenCashierSession(r.Context(), tx, c)
		if sessionErr == pgx.ErrNoRows {
			http.Error(w, "open a cashier session before processing a sales return", http.StatusConflict)
			return
		}
		if sessionErr != nil {
			http.Error(w, sessionErr.Error(), http.StatusInternalServerError)
			return
		}
		var gross float64
		if err = tx.QueryRow(r.Context(), `SELECT COALESCE(sum(total),0) FROM sale_items WHERE sale_id=$1`, saleID).Scan(&gross); err != nil || gross <= 0 {
			http.Error(w, "sale has no returnable items", 409)
			return
		}

		type line struct {
			saleItem, product                      string
			quantity, price, cost, lineNet, amount float64
		}
		lines := []line{}
		returnTotal := 0.0
		returnCost := 0.0
		for _, requested := range input.Items {
			var item line
			var sold, returned float64
			err = tx.QueryRow(r.Context(), `SELECT i.product_id,i.quantity,i.unit_price,i.unit_cost,i.total,COALESCE((SELECT sum(ri.quantity) FROM sale_return_items ri JOIN sale_returns rr ON rr.id=ri.return_id WHERE ri.sale_item_id=i.id AND rr.status='finalized'),0) FROM sale_items i WHERE i.id=$1 AND i.sale_id=$2`, requested.SaleItemID, saleID).Scan(&item.product, &sold, &item.price, &item.cost, &item.lineNet, &returned)
			if err != nil || requested.Quantity <= 0 || requested.Quantity > sold-returned {
				http.Error(w, "return quantity exceeds the remaining sold quantity", 409)
				return
			}
			item.saleItem, item.quantity = requested.SaleItemID, requested.Quantity
			// Preserve both the original line discount and the invoice-level
			// discount allocation when calculating a partial return.
			item.amount = proratedSaleReturnAmount(requested.Quantity, sold, item.lineNet, saleTotal, gross)
			returnTotal += item.amount
			returnCost += requested.Quantity * item.cost
			lines = append(lines, item)
		}
		returnTotal = math.Round(returnTotal*100) / 100
		if returnTotal <= 0 || alreadyReturned+returnTotal > saleTotal+0.01 {
			http.Error(w, "return exceeds remaining invoice value", 409)
			return
		}

		receivableAdjustment := 0.0
		if customerID != "" && saleBalance > 0 {
			var customerBalance float64
			if err = tx.QueryRow(r.Context(), `SELECT balance FROM customers WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, customerID, c.Tenant).Scan(&customerBalance); err != nil {
				http.Error(w, "customer not found", 409)
				return
			}
			var priorReceivable float64
			if err = tx.QueryRow(r.Context(), `SELECT returned_receivable FROM sales WHERE id=$1`, saleID).Scan(&priorReceivable); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			receivableAdjustment = math.Min(returnTotal, math.Min(math.Max(0, saleBalance-priorReceivable), customerBalance))
		}
		refund := returnTotal - receivableAdjustment
		var returnID, returnNo string
		err = tx.QueryRow(r.Context(), `INSERT INTO sale_returns(tenant_id,sale_id,return_no,reason,returned_by,total,refunded_amount,receivable_adjustment,client_request_id,cashier_session_id) VALUES($1,$2,next_sale_return_no(),$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9) RETURNING id,return_no`, c.Tenant, saleID, strings.TrimSpace(input.Reason), c.Sub, returnTotal, refund, receivableAdjustment, input.RequestID, cashierSessionID).Scan(&returnID, &returnNo)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		for _, item := range lines {
			if _, err = tx.Exec(r.Context(), `INSERT INTO sale_return_items(return_id,sale_item_id,quantity,total,entered_quantity,entered_unit_id,conversion_factor) SELECT $1,i.id,$3,$4,$3/NULLIF(i.conversion_factor,0),i.entered_unit_id,i.conversion_factor FROM sale_items i WHERE i.id=$2`, returnID, item.saleItem, item.quantity, item.amount); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			var balanceAfter float64
			if err = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2 WHERE id=$1 AND tenant_id=$3 RETURNING stock_quantity`, item.product, item.quantity, c.Tenant).Scan(&balanceAfter); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if err = adjustBranchStock(r.Context(), tx, c.Tenant, branch, item.product, item.quantity); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			if _, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'sale_return',$2,$3,NULLIF($4,'')::uuid,'sale_return',$5,$6,$7,NULLIF($8,'')::uuid)`, item.product, item.quantity, c.Tenant, c.Branch, returnID, balanceAfter, input.Reason, c.Sub); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if err = restoreTrackedBatches(r.Context(), tx, item.saleItem, item.quantity); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if customerID != "" && receivableAdjustment > 0 {
			if _, err = tx.Exec(r.Context(), `UPDATE customers SET balance=balance-$2 WHERE id=$1 AND tenant_id=$3`, customerID, receivableAdjustment, c.Tenant); err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO customer_ledger(tenant_id,customer_id,entry_type,reference_id,credit) VALUES($1,$2,'sale_return',$3,$4)`, c.Tenant, customerID, returnID, receivableAdjustment)
			}
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		cashRefund, bankRefund, allocationErr := allocateSaleRefund(r.Context(), tx, c.Tenant, c.Sub, saleID, "sale_return", returnID, refund, method)
		if allocationErr != nil {
			http.Error(w, allocationErr.Error(), 500)
			return
		}
		if err = postSaleReturnJournal(r.Context(), tx, c.Tenant, c.Sub, returnID, returnTotal, cashRefund, bankRefund, receivableAdjustment, returnCost); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if cashRefund > 0 {
			if _, err = tx.Exec(r.Context(), `INSERT INTO cashier_session_events(session_id,tenant_id,branch_id,event_type,reference_id,amount,created_by) VALUES($1,$2,NULLIF($3,'')::uuid,'sale_return_cash',$4,$5,$6)`, cashierSessionID, c.Tenant, c.Branch, returnID, -cashRefund, c.Sub); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		newReturned := alreadyReturned + returnTotal
		nextStatus := "partial_returned"
		if newReturned >= saleTotal-0.01 {
			nextStatus = "returned"
		}
		if _, err = tx.Exec(r.Context(), `UPDATE sales SET returned_total=$2,returned_receivable=returned_receivable+$3,returned_paid=returned_paid+$4,status=$5 WHERE id=$1 AND tenant_id=$6`, saleID, newReturned, receivableAdjustment, refund, nextStatus, c.Tenant); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		response, _ := json.Marshal(map[string]any{"id": returnID, "returnNo": returnNo, "total": returnTotal, "refund": refund, "receivableAdjustment": receivableAdjustment, "status": nextStatus})
		if _, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'sale_return',$3,$4)`, c.Tenant, input.RequestID, returnID, response); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write(response)
	}
}

func restoreTrackedBatches(ctx context.Context, tx pgx.Tx, saleItemID string, quantity float64) error {
	rows, err := tx.Query(ctx, `SELECT batch_id,quantity-returned_quantity FROM sale_item_batches WHERE sale_item_id=$1 AND quantity>returned_quantity ORDER BY batch_id FOR UPDATE`, saleItemID)
	if err != nil {
		return err
	}
	type allocation struct {
		batch     string
		available float64
	}
	items := []allocation{}
	for rows.Next() {
		var item allocation
		if err = rows.Scan(&item.batch, &item.available); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	remaining := quantity
	for _, item := range items {
		if remaining <= 0 {
			break
		}
		restored := math.Min(remaining, item.available)
		if _, err = tx.Exec(ctx, `UPDATE stock_batches SET available_qty=available_qty+$2 WHERE id=$1`, item.batch, restored); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE sale_item_batches SET returned_quantity=returned_quantity+$3 WHERE sale_item_id=$1 AND batch_id=$2`, saleItemID, item.batch, restored); err != nil {
			return err
		}
		remaining -= restored
	}
	return nil
}

// allocateSaleRefund records exactly which original tenders fund a return/cancellation.
// It preserves immutable payment history and keeps cashier reconciliation cash-only.
func allocateSaleRefund(ctx context.Context, tx pgx.Tx, tenant, actor, saleID, referenceType, referenceID string, amount float64, legacyMethod string) (float64, float64, error) {
	if amount <= 0 {
		return 0, 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT p.id,p.method,p.amount-COALESCE((SELECT sum(r.amount) FROM sale_payment_refunds r WHERE r.sale_payment_id=p.id),0) FROM sale_payments p WHERE p.sale_id=$1 AND p.tenant_id=$2 ORDER BY p.created_at,p.id FOR UPDATE`, saleID, tenant)
	if err != nil {
		return 0, 0, err
	}
	type tender struct {
		id, method string
		available  float64
	}
	tenders := []tender{}
	for rows.Next() {
		var p tender
		if err = rows.Scan(&p.id, &p.method, &p.available); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if p.available > 0 {
			tenders = append(tenders, p)
		}
	}
	rows.Close()
	if len(tenders) == 0 {
		if legacyMethod == "bank" || legacyMethod == "card" {
			return 0, amount, nil
		}
		return amount, 0, nil
	}
	remaining := amount
	cashAmount, bankAmount := 0.0, 0.0
	for _, p := range tenders {
		if remaining <= 0.009 {
			break
		}
		used := math.Min(remaining, p.available)
		used = math.Round(used*100) / 100
		if used <= 0 {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO sale_payment_refunds(tenant_id,sale_payment_id,reference_type,reference_id,amount,created_by) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid)`, tenant, p.id, referenceType, referenceID, used, actor); err != nil {
			return 0, 0, err
		}
		if p.method == "cash" {
			cashAmount += used
		} else {
			bankAmount += used
		}
		remaining -= used
	}
	if remaining > 0.01 {
		return 0, 0, fmt.Errorf("refund exceeds remaining paid allocations")
	}
	return math.Round(cashAmount*100) / 100, math.Round(bankAmount*100) / 100, nil
}

func postSaleReturnJournal(ctx context.Context, tx pgx.Tx, tenant, actor, returnID string, total, cashRefund, bankRefund, receivable, cost float64) error {
	accounts := map[string]string{}
	rows, err := tx.Query(ctx, `SELECT code,id FROM chart_of_accounts WHERE tenant_id=$1 AND code=ANY($2)`, tenant, []string{"1000", "1010", "1100", "1200", "4000", "5000"})
	if err != nil {
		return err
	}
	for rows.Next() {
		var code, id string
		if err = rows.Scan(&code, &id); err != nil {
			rows.Close()
			return err
		}
		accounts[code] = id
	}
	rows.Close()
	if len(accounts) < 6 {
		return pgx.ErrNoRows
	}
	var journal string
	if err = tx.QueryRow(ctx, `INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at) VALUES($1,'sale_return',$2,'Sales return',NULLIF($3,'')::uuid,'posted',now()) RETURNING id`, tenant, returnID, actor).Scan(&journal); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO journal_lines(journal_id,account_id,debit) VALUES($1,$2,$3)`, journal, accounts["4000"], total); err != nil {
		return err
	}
	if cashRefund > 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO journal_lines(journal_id,account_id,credit) VALUES($1,$2,$3)`, journal, accounts["1000"], cashRefund); err != nil {
			return err
		}
	}
	if bankRefund > 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO journal_lines(journal_id,account_id,credit) VALUES($1,$2,$3)`, journal, accounts["1010"], bankRefund); err != nil {
			return err
		}
	}
	if receivable > 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO journal_lines(journal_id,account_id,credit) VALUES($1,$2,$3)`, journal, accounts["1100"], receivable); err != nil {
			return err
		}
	}
	if cost > 0 {
		var costJournal string
		if err = tx.QueryRow(ctx, `INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at) VALUES($1,'sale_return_cogs',$2,'Sales return inventory',NULLIF($3,'')::uuid,'posted',now()) RETURNING id`, tenant, returnID, actor).Scan(&costJournal); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO journal_lines(journal_id,account_id,debit) VALUES($1,$2,$3)`, costJournal, accounts["1200"], cost); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO journal_lines(journal_id,account_id,credit) VALUES($1,$2,$3)`, costJournal, accounts["5000"], cost); err != nil {
			return err
		}
	}
	return nil
}

type saleCancelInput struct {
	Reason    string `json:"reason"`
	RequestID string `json:"requestId"`
}

func secureCancelSale(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input saleCancelInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.RequestID) == "" {
			http.Error(w, "reason and request ID are required", 400)
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
		var prior []byte
		err = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation='sale_cancel'`, c.Tenant, input.RequestID).Scan(&prior)
		if err == nil {
			w.Write(prior)
			return
		}
		if err != pgx.ErrNoRows {
			http.Error(w, err.Error(), 500)
			return
		}
		var customer, branch, method string
		var balance, paidAmount float64
		err = tx.QueryRow(r.Context(), `SELECT COALESCE(customer_id::text,''),balance,COALESCE(branch_id::text,''),payment_method,paid_amount FROM sales WHERE id=$1 AND tenant_id=$2 AND status='completed' AND returned_total=0 FOR UPDATE`, id, c.Tenant).Scan(&customer, &balance, &branch, &method, &paidAmount)
		if err != nil {
			http.Error(w, "only an unreturned completed sale can be cancelled", 409)
			return
		}
		if branch == "" || branch != c.Branch {
			http.Error(w, "sale cancellation must be processed in the original sale branch", http.StatusConflict)
			return
		}
		cashierSessionID := ""
		if paidAmount > 0 {
			cashierSessionID, err = lockOpenCashierSession(r.Context(), tx, c)
			if err == pgx.ErrNoRows {
				http.Error(w, "open a cashier session before cancelling a cash sale", http.StatusConflict)
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if customer != "" && balance > 0 {
			var current float64
			if err = tx.QueryRow(r.Context(), `SELECT balance FROM customers WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, customer, c.Tenant).Scan(&current); err != nil || current < balance {
				http.Error(w, "customer payments were applied; use the controlled return workflow", 409)
				return
			}
		}
		rows, err := tx.Query(r.Context(), `SELECT id,product_id,quantity FROM sale_items WHERE sale_id=$1`, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		type soldLine struct {
			item, product string
			quantity      float64
		}
		lines := []soldLine{}
		for rows.Next() {
			var line soldLine
			if err = rows.Scan(&line.item, &line.product, &line.quantity); err != nil {
				rows.Close()
				http.Error(w, err.Error(), 500)
				return
			}
			lines = append(lines, line)
		}
		rows.Close()
		for _, line := range lines {
			var after float64
			if err = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2 WHERE id=$1 AND tenant_id=$3 RETURNING stock_quantity`, line.product, line.quantity, c.Tenant).Scan(&after); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if err = adjustBranchStock(r.Context(), tx, c.Tenant, branch, line.product, line.quantity); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			if _, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) VALUES($1,'sale_cancel',$2,$3,NULLIF($4,'')::uuid,'sale',$5,$6,$7,NULLIF($8,'')::uuid)`, line.product, line.quantity, c.Tenant, c.Branch, id, after, input.Reason, c.Sub); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if err = restoreTrackedBatches(r.Context(), tx, line.item, line.quantity); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if customer != "" && balance > 0 {
			if _, err = tx.Exec(r.Context(), `UPDATE customers SET balance=balance-$2 WHERE id=$1 AND tenant_id=$3`, customer, balance, c.Tenant); err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO customer_ledger(tenant_id,customer_id,entry_type,reference_id,credit) VALUES($1,$2,'sale_cancel',$3,$4)`, c.Tenant, customer, id, balance)
			}
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if err = reverseSaleJournals(r.Context(), tx, c.Tenant, c.Sub, id, input.Reason); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		cashRefund, _, allocationErr := allocateSaleRefund(r.Context(), tx, c.Tenant, c.Sub, id, "sale_cancel", id, paidAmount, method)
		if allocationErr != nil {
			http.Error(w, allocationErr.Error(), 500)
			return
		}
		if cashRefund > 0 {
			if _, err = tx.Exec(r.Context(), `INSERT INTO cashier_session_events(session_id,tenant_id,branch_id,event_type,reference_id,amount,created_by) VALUES($1,$2,NULLIF($3,'')::uuid,'sale_cancel_cash',$4,$5,$6)`, cashierSessionID, c.Tenant, c.Branch, id, -cashRefund, c.Sub); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE sales SET status='cancelled',cancelled_at=now(),cancelled_by=NULLIF($2,'')::uuid,cancellation_reason=$3,cancellation_session_id=NULLIF($5,'')::uuid WHERE id=$1 AND tenant_id=$4`, id, c.Sub, input.Reason, c.Tenant, cashierSessionID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		response, _ := json.Marshal(map[string]any{"id": id, "status": "cancelled"})
		if _, err = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,'sale_cancel',$3,$4)`, c.Tenant, input.RequestID, id, response); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(response)
	}
}

func reverseSaleJournals(ctx context.Context, tx pgx.Tx, tenant, actor, saleID, reason string) error {
	rows, err := tx.Query(ctx, `SELECT id FROM journal_entries WHERE tenant_id=$1 AND reference_id=$2 AND reference_type IN('sale','sale_cogs') AND status='posted' AND reversed_entry_id IS NULL FOR UPDATE`, tenant, saleID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, original := range ids {
		var reversal string
		if err = tx.QueryRow(ctx, `INSERT INTO journal_entries(tenant_id,reference_type,reference_id,description,created_by,status,posted_at,reversed_entry_id,reversal_reason) VALUES($1,'sale_cancel',$2,'Sale cancellation',NULLIF($3,'')::uuid,'posted',now(),$4,$5) RETURNING id`, tenant, saleID, actor, original, reason).Scan(&reversal); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO journal_lines(journal_id,account_id,debit,credit) SELECT $1,account_id,credit,debit FROM journal_lines WHERE journal_id=$2`, reversal, original); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE journal_entries SET reversed_entry_id=$2,reversal_reason=$3,reversed_by=NULLIF($4,'')::uuid WHERE id=$1`, original, reversal, reason, actor); err != nil {
			return err
		}
	}
	return nil
}
