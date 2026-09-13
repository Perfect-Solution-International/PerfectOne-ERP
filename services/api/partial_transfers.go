package main

import (
	"encoding/json"
	"math"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

type partialTransferLine struct {
	ID       string  `json:"id"`
	Quantity float64 `json:"quantity"`
	Damaged  float64 `json:"damaged"`
	Reason   string  `json:"reason"`
}

func transferDocumentDetail(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		var out = map[string]any{}
		var id, no, from, to, status, reference, notes string
		var created any
		e := db.QueryRow(r.Context(), `SELECT d.id,d.document_no,fb.name,tb.name,d.status,COALESCE(d.reference,''),COALESCE(d.notes,''),d.created_at FROM stock_transfer_documents d JOIN branches fb ON fb.id=d.from_branch_id JOIN branches tb ON tb.id=d.to_branch_id WHERE d.id=$1 AND d.tenant_id=$2`, r.PathValue("id"), c.Tenant).Scan(&id, &no, &from, &to, &status, &reference, &notes, &created)
		if e != nil {
			http.Error(w, "transfer document not found", 404)
			return
		}
		rows, e := db.Query(r.Context(), `SELECT t.id,p.name,p.sku,COALESCE(b.batch_no,''),t.quantity,t.dispatched_quantity,t.received_quantity,t.damaged_in_transit_quantity,COALESCE(t.transit_damage_reason,'') FROM stock_transfers t JOIN products p ON p.id=t.product_id LEFT JOIN stock_batches b ON b.id=t.batch_id WHERE t.document_id=$1 ORDER BY t.created_at`, id)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		lines := []map[string]any{}
		for rows.Next() {
			var lid, name, sku, batch, reason string
			var qty, dispatched, received, damaged float64
			if rows.Scan(&lid, &name, &sku, &batch, &qty, &dispatched, &received, &damaged, &reason) == nil {
				lines = append(lines, map[string]any{"id": lid, "product": name, "sku": sku, "batchNo": batch, "quantity": qty, "dispatched": dispatched, "received": received, "damaged": damaged, "damageReason": reason})
			}
		}
		out["id"], out["documentNo"], out["from"], out["to"], out["status"], out["reference"], out["notes"], out["createdAt"], out["lines"] = id, no, from, to, status, reference, notes, created, lines
		json.NewEncoder(w).Encode(out)
	}
}
func partialTransferDocument(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Action string                `json:"action"`
			Lines  []partialTransferLine `json:"lines"`
			RequestID string             `json:"requestId"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || (x.Action != "dispatch" && x.Action != "receive") || len(x.Lines) == 0 || strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "dispatch or receive lines and request ID are required", 400)
			return
		}
		c := claimsFrom(r)
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		operation := "stock_transfer_partial_" + x.Action
		var prior []byte
		e = tx.QueryRow(r.Context(), `SELECT response FROM transaction_requests WHERE tenant_id=$1 AND request_id=$2 AND operation=$3`, c.Tenant, x.RequestID, operation).Scan(&prior)
		if e == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(prior)
			return
		}
		if e != pgx.ErrNoRows {
			http.Error(w, e.Error(), 500)
			return
		}
		var from, to, status string
		e = tx.QueryRow(r.Context(), `SELECT from_branch_id,to_branch_id,status FROM stock_transfer_documents WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, r.PathValue("id"), c.Tenant).Scan(&from, &to, &status)
		allowed := x.Action == "dispatch" && (status == "approved" || status == "partially_dispatched") || x.Action == "receive" && (status == "partially_dispatched" || status == "dispatched" || status == "partially_received")
		if e != nil || !allowed {
			http.Error(w, "action is not allowed in current status", 409)
			return
		}
		seen := map[string]bool{}
		for _, in := range x.Lines {
			if in.ID == "" || in.Quantity < 0 || in.Damaged < 0 || in.Quantity+in.Damaged <= 0 {
				http.Error(w, "positive line quantities required", 400)
				return
			}
			if seen[in.ID] {
				http.Error(w, "each transfer line can only appear once per request", 400)
				return
			}
			seen[in.ID] = true
			var product, batch string
			var requested, dispatched, received, damaged float64
			e = tx.QueryRow(r.Context(), `SELECT product_id,COALESCE(batch_id::text,''),quantity,dispatched_quantity,received_quantity,damaged_in_transit_quantity FROM stock_transfers WHERE id=$1 AND document_id=$2 FOR UPDATE`, in.ID, r.PathValue("id")).Scan(&product, &batch, &requested, &dispatched, &received, &damaged)
			if e != nil {
				http.Error(w, "transfer line not found", 404)
				return
			}
			var globalBalance, fallbackCost float64
			e = tx.QueryRow(r.Context(), `SELECT stock_quantity,purchase_price FROM products WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, product, c.Tenant).Scan(&globalBalance, &fallbackCost)
			if e != nil {
				http.Error(w, "transfer product not found", 409)
				return
			}
			if x.Action == "dispatch" {
				if in.Damaged > 0 || in.Quantity > requested-dispatched {
					http.Error(w, "dispatch exceeds remaining requested quantity", 409)
					return
				}
				var branchBalance float64
				e = tx.QueryRow(r.Context(), `UPDATE branch_stock SET quantity=quantity-$3 WHERE product_id=$1 AND branch_id=$2 AND quantity>=$3 RETURNING quantity`, product, from, in.Quantity).Scan(&branchBalance)
				if e == nil && batch != "" {
					var left float64
					e = tx.QueryRow(r.Context(), `UPDATE stock_batches SET available_qty=available_qty-$2 WHERE id=$1 AND branch_id=$3 AND available_qty>=$2 RETURNING available_qty`, batch, in.Quantity, from).Scan(&left)
				}
				if e == nil {
					_, e = tx.Exec(r.Context(), `UPDATE stock_transfers SET dispatched_quantity=dispatched_quantity+$2 WHERE id=$1`, in.ID, in.Quantity)
				}
				if e == nil {
					_, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by)VALUES($1,'transfer_out',$2,$3,$4,'stock_transfer_document',$5,$6,$7,$8)`, product, -in.Quantity, c.Tenant, from, r.PathValue("id"), globalBalance, "Partial dispatch to "+to, c.Sub)
				}
			} else {
				if in.Quantity+in.Damaged > dispatched-received-damaged || in.Damaged > 0 && strings.TrimSpace(in.Reason) == "" {
					http.Error(w, "receipt exceeds dispatched quantity or damage reason is missing", 409)
					return
				}
				var destBalance float64
				if in.Quantity > 0 {
					e = tx.QueryRow(r.Context(), `INSERT INTO branch_stock(product_id,branch_id,quantity)VALUES($1,$2,$3)ON CONFLICT(product_id,branch_id)DO UPDATE SET quantity=branch_stock.quantity+EXCLUDED.quantity RETURNING quantity`, product, to, in.Quantity).Scan(&destBalance)
					if e == nil && batch != "" {
						var batchNo string
						var made, expiry any
						var cost float64
						e = tx.QueryRow(r.Context(), `SELECT COALESCE(batch_no,''),manufactured_date,expiry_date,unit_cost FROM stock_batches WHERE id=$1`, batch).Scan(&batchNo, &made, &expiry, &cost)
						if e == nil {
							tag, err := tx.Exec(r.Context(), `UPDATE stock_batches SET available_qty=available_qty+$7 WHERE product_id=$1 AND branch_id=$2 AND batch_no IS NOT DISTINCT FROM NULLIF($3,'') AND manufactured_date IS NOT DISTINCT FROM $4::date AND expiry_date IS NOT DISTINCT FROM $5::date AND unit_cost=$6`, product, to, batchNo, made, expiry, cost, in.Quantity)
							e = err
							if e == nil && tag.RowsAffected() == 0 {
								_, e = tx.Exec(r.Context(), `INSERT INTO stock_batches(product_id,branch_id,batch_no,manufactured_date,expiry_date,available_qty,unit_cost,status,notes)VALUES($1,$2,$3,$4,$5,$6,$7,'available',$8)`, product, to, batchNo, made, expiry, in.Quantity, cost, "Transfer "+r.PathValue("id"))
							}
						}
					}
					if e == nil {
						_, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by)VALUES($1,'transfer_in',$2,$3,$4,'stock_transfer_document',$5,$6,$7,$8)`, product, in.Quantity, c.Tenant, to, r.PathValue("id"), globalBalance, "Partial receipt from "+from, c.Sub)
					}
				}
				if e == nil && in.Damaged > 0 {
					unitCost := fallbackCost
					if batch != "" {
						e = tx.QueryRow(r.Context(), `SELECT unit_cost FROM stock_batches WHERE id=$1 AND product_id=$2`, batch, product).Scan(&unitCost)
					}
					var global float64
					if e == nil {
						e = tx.QueryRow(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND tenant_id=$3 AND stock_quantity>=$2 RETURNING stock_quantity`, product, in.Damaged, c.Tenant).Scan(&global)
					}
					if e == nil {
						var damageID string
						e = tx.QueryRow(r.Context(), `INSERT INTO transfer_damage_events(tenant_id,transfer_document_id,transfer_line_id,product_id,batch_id,quantity,unit_cost,reason,reported_by) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8,NULLIF($9,'')::uuid) RETURNING id`, c.Tenant, r.PathValue("id"), in.ID, product, batch, in.Damaged, math.Round(unitCost*10000)/10000, strings.TrimSpace(in.Reason), c.Sub).Scan(&damageID)
						if e == nil {
							_, e = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by)VALUES($1,'transfer_damage',$2,$3,$4,'transfer_damage',$5,$6,$7,$8)`, product, -in.Damaged, c.Tenant, to, damageID, global, strings.TrimSpace(in.Reason), c.Sub)
						}
					}
				}
				if e == nil {
					_, e = tx.Exec(r.Context(), `UPDATE stock_transfers SET received_quantity=received_quantity+$2,damaged_in_transit_quantity=damaged_in_transit_quantity+$3,transit_damage_reason=CASE WHEN $3>0 THEN $4 ELSE transit_damage_reason END WHERE id=$1`, in.ID, in.Quantity, in.Damaged, strings.TrimSpace(in.Reason))
				}
			}
			if e != nil {
				http.Error(w, "stock quantity or batch is insufficient", 409)
				return
			}
		}
		var requested, dispatched, processed float64
		e = tx.QueryRow(r.Context(), `SELECT COALESCE(sum(quantity),0),COALESCE(sum(dispatched_quantity),0),COALESCE(sum(received_quantity+damaged_in_transit_quantity),0) FROM stock_transfers WHERE document_id=$1`, r.PathValue("id")).Scan(&requested, &dispatched, &processed)
		next := "partially_dispatched"
		if math.Abs(processed-requested) < 0.0005 {
			next = "received"
		} else if processed > 0 {
			next = "partially_received"
		} else if math.Abs(dispatched-requested) < 0.0005 {
			next = "dispatched"
		}
		_, e = tx.Exec(r.Context(), `UPDATE stock_transfer_documents SET status=$2,dispatched_by=CASE WHEN $3='dispatch' THEN NULLIF($4,'')::uuid ELSE dispatched_by END,dispatched_at=CASE WHEN $3='dispatch' THEN now() ELSE dispatched_at END,received_by=CASE WHEN $3='receive' THEN NULLIF($4,'')::uuid ELSE received_by END,received_at=CASE WHEN $2='received' THEN now() ELSE received_at END,updated_at=now() WHERE id=$1`, r.PathValue("id"), next, x.Action, c.Sub)
		response, _ := json.Marshal(map[string]string{"status": next})
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO transaction_requests(tenant_id,request_id,operation,entity_id,response) VALUES($1,$2,$3,$4,$5)`, c.Tenant, x.RequestID, operation, r.PathValue("id"), response)
		}
		if e != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "partial transfer failed", 409)
			return
		}
		auditUserAction(r, db, "STOCK_TRANSFER_PARTIAL_"+strings.ToUpper(x.Action), r.PathValue("id"), x)
		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	}
}
