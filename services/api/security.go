package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
	"time"
)

type Claims struct {
	Sub, Role, Tenant, Branch string
	Exp                       int64
}
type claimsCtxKeyType struct{}
type auditResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(b)
}

var claimsCtxKey = claimsCtxKeyType{}
var jwtSecret []byte
var allStaff = []string{"super_admin", "admin", "manager", "accountant", "cashier", "stock_manager"}
var management = []string{"super_admin", "manager"}
var stockRoles = []string{"super_admin", "manager", "stock_manager"}
var financeRoles = []string{"super_admin", "admin", "manager", "accountant"}
var salesRoles = []string{"super_admin", "admin", "manager", "cashier"}
var permissionDefaults = map[string][]string{
	"super_admin":   {"*"},
	"admin":         {"*"},
	"manager":       {"dashboard", "pos", "sales", "sales_returns", "purchases", "inventory", "reports", "contacts", "catalog_read"},
	"accountant":    {"accounting", "cash_bank", "financial_reports", "contacts", "catalog_read"},
	"cashier":       {"pos", "sales_returns", "own_sales", "cashier_session", "catalog_read"},
	"stock_manager": {"products", "purchases", "inventory", "expiry", "expiry.view", "expiry.edit", "expiry.print", "expiry.export", "damage", "catalog_read"},
}
var permissionKeys = []string{"dashboard", "pos", "sales", "sales_returns", "own_sales", "cashier_session", "products", "purchases", "inventory", "expiry", "expiry.view", "expiry.edit", "expiry.print", "expiry.export", "damage", "reports", "contacts", "accounting", "cash_bank", "financial_reports", "users", "roles", "catalog_read", "promotions.view", "promotions.add", "promotions.edit", "promotions.delete", "settings.view", "settings.edit", "notifications.view", "archive.manage", "sync.view"}

func roleHas(role, permission string) bool {
	for _, p := range permissionDefaults[role] {
		if p == "*" || p == permission {
			return true
		}
	}
	return false
}
func effectivePermissions(ctx context.Context, db *pgxpool.Pool, userID, role string) ([]string, error) {
	enabled := map[string]bool{}
	rows, e := db.Query(ctx, `SELECT rp.permission FROM roles r JOIN role_permissions rp ON rp.role_id=r.id AND rp.enabled JOIN users u ON u.tenant_id=r.tenant_id AND u.role=r.key WHERE u.id=$1 AND r.is_active`, userID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var p string
		if e = rows.Scan(&p); e != nil {
			rows.Close()
			return nil, e
		}
		enabled[p] = true
	}
	rows.Close()
	if len(enabled) == 0 {
		for _, p := range permissionKeys {
			if roleHas(role, p) {
				enabled[p] = true
			}
		}
	}
	overrides, e := db.Query(ctx, `SELECT permission,enabled FROM user_permissions WHERE user_id=$1`, userID)
	if e != nil {
		return nil, e
	}
	defer overrides.Close()
	for overrides.Next() {
		var p string
		var on bool
		if e = overrides.Scan(&p, &on); e != nil {
			return nil, e
		}
		if role != "super_admin" {
			enabled[p] = on
		}
	}
	out := []string{}
	for p, on := range enabled {
		if on {
			out = append(out, p)
		}
	}
	return out, overrides.Err()
}
func actionFor(method string) string {
	switch method {
	case "GET":
		return "view"
	case "POST":
		return "add"
	case "PUT", "PATCH":
		return "edit"
	case "DELETE":
		return "delete"
	}
	return "view"
}
func requestPermission(r *http.Request) string {
	p := r.URL.Path
	m := r.Method
	a := actionFor(m)
	if strings.HasPrefix(p, "/notifications") { return "notifications.view" }
	if strings.HasPrefix(p, "/archive") { return "archive.manage" }
	if strings.HasPrefix(p, "/sync-transactions") { return "sync.view" }
	if strings.HasPrefix(p, "/settings") { if m=="GET" { return "settings.view" }; return "settings.edit" }
	if strings.HasPrefix(p, "/promotions") {
		if strings.Contains(p, "/usage") { return "promotions.view" }
		if strings.Contains(p, "/restore") { return "promotions.edit" }
		return "promotions." + a
	}
	if strings.HasPrefix(p, "/expiry/") {
		if m == "GET" {
			return "expiry.view"
		}
		return "expiry.edit"
	}
	if strings.HasPrefix(p, "/roles") {
		return "roles." + a
	}
	if strings.HasPrefix(p, "/users") {
		return "users." + a
	}
	if strings.HasPrefix(p, "/purchase-orders") || strings.HasPrefix(p, "/purchase-returns") || strings.HasPrefix(p, "/procurement/") {
		if strings.Contains(p, "/approve") {
			return "purchases.approve"
		}
		if strings.Contains(p, "/cancel") || strings.Contains(p, "/reverse") {
			return "purchases.cancel"
		}
		if strings.Contains(p, "/convert") || strings.Contains(p, "/finalize") {
			return "purchases.approve"
		}
		return "purchases." + a
	}
	if strings.HasPrefix(p, "/inventory/") {
		if strings.Contains(p, "/approve") || strings.Contains(p, "/dispatch") || strings.Contains(p, "/receive") || strings.Contains(p, "/finalize") {
			return "inventory.approve"
		}
		if strings.Contains(p, "/cancel") {
			return "inventory.cancel"
		}
		return "inventory." + a
	}
	if p == "/sales" && m == "GET" {
		return ""
	}
	if strings.HasPrefix(p, "/invoices/") && m == "GET" {
		return ""
	}
	if strings.Contains(p, "/cancel") {
		return "sales.cancel"
	}
	if strings.Contains(p, "/return") {
		return "sales_returns.add"
	}
	if strings.HasPrefix(p, "/pos/") {
		return "sales.add"
	}
	if strings.HasPrefix(p, "/cashier-sessions") {
		return "cashier_session"
	}
	if strings.HasPrefix(p, "/cash-") {
		return "cash_bank." + a
	}
	if strings.HasPrefix(p, "/finance/") || strings.HasPrefix(p, "/expenses") || strings.HasPrefix(p, "/expense-categories") {
		return "cash_bank." + a
	}
	if strings.HasPrefix(p, "/accounting/") {
		return "accounting." + a
	}
	if strings.HasPrefix(p, "/reports/accounting") {
		return "accounting.view"
	}
	if strings.HasPrefix(p, "/reports/") {
		if r.URL.Query().Get("format") != "" {
			return "reports.export"
		}
		return "reports.view"
	}
	if strings.HasPrefix(p, "/purchases") {
		if strings.Contains(p, "/cancel") || strings.Contains(p, "/reverse") {
			return "purchases.cancel"
		}
		if strings.Contains(p, "/finalize") {
			return "purchases.approve"
		}
		return "purchases." + a
	}
	if strings.HasPrefix(p, "/stock/expiry") {
		return "expiry.view"
	}
	if strings.HasPrefix(p, "/stock/") || strings.HasPrefix(p, "/low-stock") {
		return "inventory." + a
	}
	if strings.HasPrefix(p, "/damages") {
		return "damage"
	}
	if strings.HasPrefix(p, "/catalog-settings") {
		if strings.Contains(p, "/status") {
			return "products.edit"
		}
		return "products." + a
	}
	if strings.HasPrefix(p, "/barcodes") {
		if strings.Contains(p, "/generate") {
			return "products.add"
		}
		return "products.view"
	}
	if strings.HasPrefix(p, "/product-metadata") {
		return "catalog_read"
	}
	if strings.HasPrefix(p, "/products") {
		if strings.Contains(p, "/price-history") {
			return "products.view"
		}
		if strings.Contains(p, "/restore") {
			return "products.edit"
		}
		if m == "GET" {
			return "catalog_read"
		}
		return "products." + a
	}
	if strings.HasPrefix(p, "/customers") {
		if m == "GET" {
			return "catalog_read"
		}
		return "customers." + a
	}
	if strings.HasPrefix(p, "/suppliers") {
		if m == "GET" {
			return "catalog_read"
		}
		return "suppliers." + a
	}
	if strings.Contains(p, "payments") {
		return "accounting.add"
	}
	if strings.HasPrefix(p, "/sales") {
		return "sales." + a
	}
	if strings.HasPrefix(p, "/dashboard") {
		return "dashboard"
	}
	return ""
}
func authenticated(db *pgxpool.Pool, roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			parts := strings.Split(raw, ".")
			if len(parts) != 3 {
				http.Error(w, "authentication required", 401)
				return
			}
			mac := hmac.New(sha256.New, jwtSecret)
			mac.Write([]byte(parts[0] + "." + parts[1]))
			if !hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), []byte(parts[2])) {
				http.Error(w, "invalid token", 401)
				return
			}
			var c Claims
			if json.Unmarshal(must64(parts[1]), &c) != nil || c.Exp < time.Now().Unix() {
				http.Error(w, "expired token", 401)
				return
			}
			var exists int
			e := db.QueryRow(r.Context(), `SELECT 1 FROM revoked_tokens WHERE token_hash=$1 UNION ALL SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM users WHERE id=$2 AND is_active AND deleted_at IS NULL) LIMIT 1`, tokenHash(raw), c.Sub).Scan(&exists)
			if e == nil {
				http.Error(w, "token revoked or account inactive", 401)
				return
			}
			if e != pgx.ErrNoRows {
				http.Error(w, e.Error(), 500)
				return
			}
			permission := requestPermission(r)
			if permission != "" && c.Role != "super_admin" {
				permissions, er := effectivePermissions(r.Context(), db, c.Sub, c.Role)
				if er != nil {
					http.Error(w, er.Error(), 500)
					return
				}
				ok := false
				for _, p := range permissions {
					if p == permission {
						ok = true
						break
					}
				}
				if !ok {
					http.Error(w, "permission denied", 403)
					return
				}
			} else if len(roles) > 0 {
				ok := false
				for _, role := range roles {
					if c.Role == role {
						ok = true
					}
				}
				if !ok {
					http.Error(w, "permission denied", 403)
					return
				}
			}
			ctxR := r.WithContext(context.WithValue(r.Context(), claimsCtxKey, c))
			aw := &auditResponseWriter{ResponseWriter: w}
			next(aw, ctxR)
			if r.Method != "GET" && r.Method != "OPTIONS" && aw.status < 400 {
				details, _ := json.Marshal(map[string]string{"method": r.Method, "path": r.URL.Path})
				db.Exec(r.Context(), `INSERT INTO audit_logs(tenant_id,user_id,action,entity_type,new_data,ip_address) VALUES($1,$2,$3,'api',NULLIF($4,'')::jsonb,NULLIF($5,'')::inet)`, c.Tenant, c.Sub, "API_"+r.Method, string(details), clientIP(r))
			}
		}
	}
}
func claimsFrom(r *http.Request) Claims { c, _ := r.Context().Value(claimsCtxKey).(Claims); return c }
func must64(s string) []byte            { b, _ := base64.RawURLEncoding.DecodeString(s); return b }
func tokenHash(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
