package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func expiryBatches(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		branchID := strings.TrimSpace(r.URL.Query().Get("branchId"))
		from := strings.TrimSpace(r.URL.Query().Get("from"))
		to := strings.TrimSpace(r.URL.Query().Get("to"))
		days, err := strconv.Atoi(r.URL.Query().Get("days"))
		if err != nil || days < 1 || days > 365 {
			days = 30
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page < 1 {
			page = 1
		}
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			limit = 25
		}

		const stateSQL = `CASE
			WHEN b.status='expired' THEN 'removed'
			WHEN b.available_qty<=0 OR b.status IN('depleted','returned') THEN 'depleted'
			WHEN b.expiry_date IS NULL THEN 'no_expiry'
			WHEN b.expiry_date<current_date THEN 'expired'
			WHEN b.expiry_date<=current_date+$6::int THEN 'near_expiry'
			ELSE 'valid' END`
		base := ` FROM stock_batches b
			JOIN products p ON p.id=b.product_id
			LEFT JOIN branches br ON br.id=b.branch_id
			LEFT JOIN purchase_items pi ON pi.batch_id=b.id
			LEFT JOIN purchases pu ON pu.id=pi.purchase_id
			LEFT JOIN suppliers s ON s.id=pu.supplier_id
			WHERE p.tenant_id=$1
			AND ($2='' OR p.name ILIKE '%'||$2||'%' OR p.sku ILIKE '%'||$2||'%' OR COALESCE(p.barcode,'') ILIKE '%'||$2||'%' OR COALESCE(b.batch_no,'') ILIKE '%'||$2||'%')
			AND ($3='' OR b.branch_id=NULLIF($3,'')::uuid)
			AND ($4='' OR b.expiry_date>=NULLIF($4,'')::date)
			AND ($5='' OR b.expiry_date<=NULLIF($5,'')::date)`
		args := []any{c.Tenant, query, branchID, from, to, days, status, limit, (page - 1) * limit}

		var total int
		countSQL := `SELECT count(*) FROM (SELECT b.id,` + stateSQL + ` AS expiry_state` + base + `) x WHERE $7='' OR $7='all' OR expiry_state=$7`
		if err = db.QueryRow(r.Context(), countSQL, args[:7]...).Scan(&total); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		rowsSQL := `SELECT * FROM (SELECT b.id,b.product_id,p.name,p.sku,COALESCE(p.barcode,''),COALESCE(b.batch_no,''),COALESCE(b.expiry_date::text,'') AS expiry_date,b.available_qty,b.unit_cost,b.status,COALESCE(br.name,''),COALESCE(pu.invoice_no,''),COALESCE(s.name,''),b.received_at,COALESCE(b.notes,''),` + stateSQL + ` AS expiry_state,
			NOT EXISTS(SELECT 1 FROM sale_item_batches sib WHERE sib.batch_id=b.id) AND NOT EXISTS(SELECT 1 FROM purchase_return_items pri JOIN purchase_returns pr ON pr.id=pri.purchase_return_id WHERE pri.purchase_item_id=pi.id AND pr.status='finalized') AND b.status='available' AS can_edit` + base + `) x
			WHERE $7='' OR $7='all' OR expiry_state=$7
			ORDER BY CASE expiry_state WHEN 'expired' THEN 0 WHEN 'near_expiry' THEN 1 WHEN 'valid' THEN 2 WHEN 'no_expiry' THEN 3 ELSE 4 END, NULLIF(expiry_date,'')::date NULLS LAST,received_at DESC
			LIMIT $8 OFFSET $9`
		rows, err := db.Query(r.Context(), rowsSQL, args...)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, productID, product, sku, barcode, batchNo, expiry, batchStatus, branch, invoice, supplier, notes, expiryState string
			var available, unitCost float64
			var received any
			var canEdit bool
			if err = rows.Scan(&id, &productID, &product, &sku, &barcode, &batchNo, &expiry, &available, &unitCost, &batchStatus, &branch, &invoice, &supplier, &received, &notes, &expiryState, &canEdit); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			items = append(items, map[string]any{"id": id, "productId": productID, "product": product, "sku": sku, "barcode": barcode, "batchNo": batchNo, "expiry": expiry, "available": available, "unitCost": unitCost, "stockValue": available * unitCost, "batchStatus": batchStatus, "expiryState": expiryState, "branch": branch, "invoice": invoice, "supplier": supplier, "receivedAt": received, "notes": notes, "canEdit": canEdit})
		}
		if err = rows.Err(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		var valid, near, expired, noExpiry, removed int
		var atRiskQty, atRiskValue float64
		summaryStateSQL := strings.ReplaceAll(stateSQL, "$6", "$3")
		summarySQL := `SELECT count(*) FILTER(WHERE state='valid'),count(*) FILTER(WHERE state='near_expiry'),count(*) FILTER(WHERE state='expired'),count(*) FILTER(WHERE state='no_expiry'),count(*) FILTER(WHERE state IN('removed','depleted')),COALESCE(sum(available_qty) FILTER(WHERE state IN('near_expiry','expired')),0),COALESCE(sum(available_qty*unit_cost) FILTER(WHERE state IN('near_expiry','expired')),0) FROM (SELECT b.available_qty,b.unit_cost,` + summaryStateSQL + ` state FROM stock_batches b JOIN products p ON p.id=b.product_id WHERE p.tenant_id=$1 AND ($2='' OR b.branch_id=NULLIF($2,'')::uuid)) x`
		if err = db.QueryRow(r.Context(), summarySQL, c.Tenant, branchID, days).Scan(&valid, &near, &expired, &noExpiry, &removed, &atRiskQty, &atRiskValue); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"rows": items, "total": total, "page": page, "limit": limit, "days": days, "summary": map[string]any{"valid": valid, "nearExpiry": near, "expired": expired, "noExpiry": noExpiry, "removed": removed, "atRiskQuantity": atRiskQty, "atRiskValue": atRiskValue}})
	}
}

func expiryHistory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		rows, err := db.Query(r.Context(), `SELECT m.id,p.name,p.sku,COALESCE(b.batch_no,''),m.quantity,COALESCE(m.notes,''),COALESCE(u.name,'System'),m.created_at
			FROM stock_movements m JOIN products p ON p.id=m.product_id LEFT JOIN stock_batches b ON b.id=m.reference_id AND m.reference_type='batch' LEFT JOIN users u ON u.id=m.created_by
			WHERE m.tenant_id=$1 AND m.kind='expired' AND ($2='' OR p.name ILIKE '%'||$2||'%' OR p.sku ILIKE '%'||$2||'%' OR COALESCE(b.batch_no,'') ILIKE '%'||$2||'%') ORDER BY m.created_at DESC LIMIT 500`, claimsFrom(r).Tenant, q)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, product, sku, batch, reason, user string
			var quantity float64
			var created any
			if err = rows.Scan(&id, &product, &sku, &batch, &quantity, &reason, &user, &created); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			out = append(out, map[string]any{"id": id, "product": product, "sku": sku, "batchNo": batch, "quantity": quantity, "reason": reason, "user": user, "createdAt": created})
		}
		json.NewEncoder(w).Encode(out)
	}
}
