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

type measurementQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func registerMeasurements(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /products/{id}/units", jsonAPI(authenticated(db, allStaff...)(productUnitsHandler(db))))
	m.HandleFunc("GET /products/{id}/conversion-history", jsonAPI(authenticated(db, stockRoles...)(conversionHistory(db))))
	m.HandleFunc("POST /inventory/unit-conversions", jsonAPI(authenticated(db, stockRoles...)(createUnitConversion(db))))
}

func loadProductUnits(ctx context.Context, q measurementQuerier, product, tenant string) ([]map[string]any, error) {
	rows, err := q.Query(ctx, `SELECT pu.id,pu.unit_id,u.name,u.symbol,pu.factor_to_base,pu.usage,pu.purchase_price,pu.sale_price,COALESCE(pu.barcode,''),pu.is_default_purchase,pu.is_default_sale FROM product_units pu JOIN units u ON u.id=pu.unit_id WHERE pu.tenant_id=$1 AND pu.product_id=$2 AND pu.is_active ORDER BY pu.is_default_sale DESC,pu.factor_to_base`, tenant, product)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uid, name, symbol, usage, barcode string
		var factor float64
		var purchase, sale *float64
		var dp, ds bool
		if err = rows.Scan(&id, &uid, &name, &symbol, &factor, &usage, &purchase, &sale, &barcode, &dp, &ds); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "unitId": uid, "name": name, "symbol": symbol, "factorToBase": factor, "usage": usage, "purchasePrice": purchase, "salePrice": sale, "barcode": barcode, "isDefaultPurchase": dp, "isDefaultSale": ds})
	}
	return out, rows.Err()
}

func saveProductUnits(ctx context.Context, tx pgx.Tx, tenant, product string, units []ProductUnitInput) error {
	if _, err := tx.Exec(ctx, `UPDATE product_units SET is_active=false,updated_at=now() WHERE tenant_id=$1 AND product_id=$2`, tenant, product); err != nil {
		return err
	}
	for _, u := range units {
		if u.UnitID == "" || u.Factor <= 0 {
			return fmt.Errorf("unit and positive conversion factor required")
		}
		usage := u.Usage
		if usage == "" {
			usage = "both"
		}
		_, err := tx.Exec(ctx, `INSERT INTO product_units(tenant_id,product_id,unit_id,factor_to_base,usage,purchase_price,sale_price,barcode,is_default_purchase,is_default_sale,is_active) SELECT $1,$2,x.id,$4,$5,$6,$7,NULLIF($8,''),$9,$10,true FROM units x WHERE x.id=$3 AND x.tenant_id=$1 AND x.is_active ON CONFLICT(tenant_id,product_id,unit_id) DO UPDATE SET factor_to_base=EXCLUDED.factor_to_base,usage=EXCLUDED.usage,purchase_price=EXCLUDED.purchase_price,sale_price=EXCLUDED.sale_price,barcode=EXCLUDED.barcode,is_default_purchase=EXCLUDED.is_default_purchase,is_default_sale=EXCLUDED.is_default_sale,is_active=true,updated_at=now()`, tenant, product, u.UnitID, u.Factor, usage, u.PurchasePrice, u.SalePrice, strings.TrimSpace(u.Barcode), u.DefaultPurchase, u.DefaultSale)
		if err != nil {
			return err
		}
	}
	return nil
}

func productUnitsHandler(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := loadProductUnits(r.Context(), db, r.PathValue("id"), claimsFrom(r).Tenant)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(out)
	}
}

func conversionHistory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(r.Context(), `SELECT e.id,e.event_type,e.source_quantity,COALESCE(su.symbol,''),e.source_base_quantity,e.target_quantity,COALESCE(tu.symbol,''),e.target_base_quantity,e.reason,COALESCE(e.reference,''),COALESCE(u.name,'System'),e.created_at FROM unit_conversion_events e LEFT JOIN units su ON su.id=e.source_unit_id LEFT JOIN units tu ON tu.id=e.target_unit_id LEFT JOIN users u ON u.id=e.created_by WHERE e.tenant_id=$1 AND (e.source_product_id=$2 OR e.target_product_id=$2) ORDER BY e.created_at DESC`, claimsFrom(r).Tenant, r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, kind, su, tu, reason, ref, user string
			var sq, sb float64
			var tq, tb *float64
			var at any
			if rows.Scan(&id, &kind, &sq, &su, &sb, &tq, &tu, &tb, &reason, &ref, &user, &at) == nil {
				out = append(out, map[string]any{"id": id, "type": kind, "sourceQuantity": sq, "sourceUnit": su, "sourceBaseQuantity": sb, "targetQuantity": tq, "targetUnit": tu, "targetBaseQuantity": tb, "reason": reason, "reference": ref, "user": user, "createdAt": at})
			}
		}
		json.NewEncoder(w).Encode(out)
	}
}

func createUnitConversion(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			EventType, SourceProductID, SourceUnitID, TargetProductID, TargetUnitID, Reason, Reference, RequestID string
			SourceQuantity, TargetQuantity                                                                        float64
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.SourceProductID == "" || x.SourceQuantity <= 0 || strings.TrimSpace(x.Reason) == "" {
			http.Error(w, "product, positive quantity and reason required", 400)
			return
		}
		if x.EventType != "repack" && x.EventType != "unpack" && x.EventType != "wastage" && x.EventType != "spillage" && x.EventType != "correction" {
			http.Error(w, "invalid conversion event", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		factor := 1.0
		if x.SourceUnitID != "" {
			err = tx.QueryRow(r.Context(), `SELECT factor_to_base FROM product_units WHERE tenant_id=$1 AND product_id=$2 AND unit_id=$3 AND is_active FOR SHARE`, c.Tenant, x.SourceProductID, x.SourceUnitID).Scan(&factor)
		}
		base := math.Round(x.SourceQuantity*factor*1e6) / 1e6
		if err == nil {
			var available float64
			err = tx.QueryRow(r.Context(), `SELECT quantity FROM branch_stock WHERE branch_id=NULLIF($1,'')::uuid AND product_id=$2 FOR UPDATE`, c.Branch, x.SourceProductID).Scan(&available)
			if err == nil && available+1e-6 < base {
				err = fmt.Errorf("insufficient branch stock")
			}
		}
		var sourceCostPrice float64
		if err == nil {
			err = tx.QueryRow(r.Context(), `SELECT purchase_price FROM products WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, c.Tenant, x.SourceProductID).Scan(&sourceCostPrice)
		}
		sourceCost := money(base * sourceCostPrice)
		var targetBase *float64
		var targetUnitCost *float64
		var yieldPercent *float64
		if err == nil && x.TargetProductID != "" {
			tf := 1.0
			if x.TargetUnitID != "" {
				err = tx.QueryRow(r.Context(), `SELECT factor_to_base FROM product_units WHERE tenant_id=$1 AND product_id=$2 AND unit_id=$3 AND is_active FOR SHARE`, c.Tenant, x.TargetProductID, x.TargetUnitID).Scan(&tf)
			}
			v := math.Round(x.TargetQuantity*tf*1e6) / 1e6
			targetBase = &v
			if v <= 0 {
				err = fmt.Errorf("positive result quantity required")
			} else {
				cost := sourceCost / v
				targetUnitCost = &cost
				yield := v / base * 100
				yieldPercent = &yield
			}
		}
		var id string
		if err == nil {
			err = tx.QueryRow(r.Context(), `INSERT INTO unit_conversion_events(tenant_id,branch_id,event_type,source_product_id,target_product_id,source_quantity,source_unit_id,source_base_quantity,target_quantity,target_unit_id,target_base_quantity,reason,reference,created_by,source_cost,target_unit_cost,yield_percent)VALUES($1,NULLIF($2,'')::uuid,$3,$4,NULLIF($5,'')::uuid,$6,NULLIF($7,'')::uuid,$8,NULLIF($9,0),NULLIF($10,'')::uuid,$11,$12,NULLIF($13,''),NULLIF($14,'')::uuid,$15,$16,$17)RETURNING id`, c.Tenant, c.Branch, x.EventType, x.SourceProductID, x.TargetProductID, x.SourceQuantity, x.SourceUnitID, base, x.TargetQuantity, x.TargetUnitID, targetBase, strings.TrimSpace(x.Reason), strings.TrimSpace(x.Reference), c.Sub, sourceCost, targetUnitCost, yieldPercent).Scan(&id)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE products SET stock_quantity=stock_quantity-$2 WHERE id=$1 AND tenant_id=$3`, x.SourceProductID, base, c.Tenant)
		}
		if err == nil {
			err = adjustBranchStock(r.Context(), tx, c.Tenant, c.Branch, x.SourceProductID, -base)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) SELECT id,$2,-$3,tenant_id,NULLIF($4,'')::uuid,'unit_conversion',$5,stock_quantity,$6,NULLIF($7,'')::uuid FROM products WHERE id=$1`, x.SourceProductID, x.EventType, base, c.Branch, id, x.Reason, c.Sub)
		}
		if err == nil && targetBase != nil {
			var oldQty, oldCost float64
			err = tx.QueryRow(r.Context(), `SELECT stock_quantity,purchase_price FROM products WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, x.TargetProductID, c.Tenant).Scan(&oldQty, &oldCost)
			if err == nil {
				newCost := oldCost
				if oldQty+*targetBase > 0 {
					newCost = (oldQty*oldCost + sourceCost) / (oldQty + *targetBase)
				}
				_, err = tx.Exec(r.Context(), `UPDATE products SET stock_quantity=stock_quantity+$2,purchase_price=$3 WHERE id=$1 AND tenant_id=$4`, x.TargetProductID, *targetBase, money(newCost), c.Tenant)
			}
			if err == nil {
				err = adjustBranchStock(r.Context(), tx, c.Tenant, c.Branch, x.TargetProductID, *targetBase)
			}
			if err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO stock_movements(product_id,kind,quantity,tenant_id,branch_id,reference_type,reference_id,balance_after,notes,created_by) SELECT id,$2,$3,tenant_id,NULLIF($4,'')::uuid,'unit_conversion',$5,stock_quantity,$6,NULLIF($7,'')::uuid FROM products WHERE id=$1`, x.TargetProductID, x.EventType, *targetBase, c.Branch, id, x.Reason, c.Sub)
			}
		}
		if err == nil && targetBase == nil && sourceCost > 0 {
			_, _, err = postJournalTx(r.Context(), tx, journalDraft{Tenant: c.Tenant, Branch: c.Branch, Actor: c.Sub, ReferenceType: "unit_conversion", ReferenceID: id, Description: strings.Title(x.EventType) + " inventory loss", Source: "system", ClientRequestID: "unit-conversion:" + id, Lines: []postingLine{debitRole(roleInventoryLoss, sourceCost, x.Reason), creditRole(roleInventoryControl, sourceCost, x.Reason)}})
		}
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		auditUserAction(r, db, "UNIT_CONVERSION_CREATED", id, map[string]any{"type": x.EventType, "baseQuantity": base, "reason": x.Reason})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "baseQuantity": base, "targetBaseQuantity": targetBase})
	}
}
