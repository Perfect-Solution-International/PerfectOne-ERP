package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"regexp"
	"strings"
)

type RoleRecord struct {
	ID, Key, Name, Description string
	IsSystem                   bool     `json:"isSystem"`
	IsActive                   bool     `json:"isActive"`
	UserCount                  int      `json:"userCount"`
	Permissions                []string `json:"permissions"`
}

var roleKeyCleaner = regexp.MustCompile(`[^a-z0-9]+`)

func registerRoles(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /roles", jsonAPI(authenticated(db, "super_admin", "admin")(listRoles(db))))
	m.HandleFunc("POST /roles", jsonAPI(authenticated(db, "super_admin", "admin")(createRole(db))))
	m.HandleFunc("PUT /roles/{id}", jsonAPI(authenticated(db, "super_admin", "admin")(updateRole(db))))
	m.HandleFunc("PUT /roles/{id}/permissions", jsonAPI(authenticated(db, "super_admin", "admin")(updateRolePermissions(db))))
	m.HandleFunc("DELETE /roles/{id}", jsonAPI(authenticated(db, "super_admin", "admin")(deleteRole(db))))
}
func rolePermissions(r *http.Request, db *pgxpool.Pool, id string) ([]string, error) {
	rows, e := db.Query(r.Context(), `SELECT permission FROM role_permissions WHERE role_id=$1 AND enabled ORDER BY permission`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if e = rows.Scan(&p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func listRoles(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		rows, e := db.Query(r.Context(), `SELECT r.id,r.key,r.name,COALESCE(r.description,''),r.is_system,r.is_active,count(u.id) FROM roles r LEFT JOIN users u ON u.role=r.key AND u.tenant_id=r.tenant_id AND u.deleted_at IS NULL WHERE r.tenant_id=$1 GROUP BY r.id ORDER BY r.is_system DESC,r.name`, c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []RoleRecord{}
		for rows.Next() {
			var x RoleRecord
			if e = rows.Scan(&x.ID, &x.Key, &x.Name, &x.Description, &x.IsSystem, &x.IsActive, &x.UserCount); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			x.Permissions, e = rolePermissions(r, db, x.ID)
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			out = append(out, x)
		}
		json.NewEncoder(w).Encode(out)
	}
}
func createRole(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct{ Name, Description string }
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" {
			http.Error(w, "role name required", 400)
			return
		}
		key := strings.Trim(roleKeyCleaner.ReplaceAllString(strings.ToLower(x.Name), "_"), "_")
		if key == "" {
			http.Error(w, "invalid role name", 400)
			return
		}
		c := claimsFrom(r)
		var id string
		e := db.QueryRow(r.Context(), `INSERT INTO roles(tenant_id,key,name,description) VALUES($1,$2,$3,$4) RETURNING id`, c.Tenant, key, strings.TrimSpace(x.Name), strings.TrimSpace(x.Description)).Scan(&id)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		auditUserAction(r, db, "ROLE_CREATED", id, map[string]any{"key": key, "name": x.Name})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id, "key": key})
	}
}
func updateRole(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Name, Description string
			IsActive          bool `json:"isActive"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" {
			http.Error(w, "role name required", 400)
			return
		}
		c := claimsFrom(r)
		id := r.PathValue("id")
		var key string
		if e := db.QueryRow(r.Context(), `SELECT key FROM roles WHERE id=$1 AND tenant_id=$2`, id, c.Tenant).Scan(&key); e != nil {
			http.Error(w, "role not found", 404)
			return
		}
		if key == "super_admin" && !x.IsActive {
			http.Error(w, "Super Admin role cannot be disabled", 400)
			return
		}
		_, e := db.Exec(r.Context(), `UPDATE roles SET name=$1,description=$2,is_active=$3,updated_at=now() WHERE id=$4 AND tenant_id=$5`, strings.TrimSpace(x.Name), strings.TrimSpace(x.Description), x.IsActive, id, c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		auditUserAction(r, db, "ROLE_UPDATED", id, map[string]any{"name": x.Name, "active": x.IsActive})
		w.WriteHeader(204)
	}
}
func updateRolePermissions(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Permissions []string `json:"permissions"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil {
			http.Error(w, "permissions required", 400)
			return
		}
		c := claimsFrom(r)
		id := r.PathValue("id")
		var key string
		if e := db.QueryRow(r.Context(), `SELECT key FROM roles WHERE id=$1 AND tenant_id=$2`, id, c.Tenant).Scan(&key); e != nil {
			http.Error(w, "role not found", 404)
			return
		}
		if key == "super_admin" {
			http.Error(w, "Super Admin always has full access", 400)
			return
		}
		selected := map[string]bool{}
		for _, p := range x.Permissions {
			p = strings.TrimSpace(p)
			if p != "" {
				selected[p] = true
				if i := strings.IndexByte(p, '.'); i > 0 {
					selected[p[:i]] = true
				}
			}
		}
		selected["catalog_read"] = true
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		if _, e = tx.Exec(r.Context(), `DELETE FROM role_permissions WHERE role_id=$1`, id); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		for p := range selected {
			if _, e = tx.Exec(r.Context(), `INSERT INTO role_permissions(role_id,permission) VALUES($1,$2)`, id, p); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		auditUserAction(r, db, "ROLE_PERMISSIONS_UPDATED", id, map[string]any{"permissions": x.Permissions})
		w.WriteHeader(204)
	}
}
func deleteRole(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := r.PathValue("id")
		var key string
		var system bool
		if e := db.QueryRow(r.Context(), `SELECT key,is_system FROM roles WHERE id=$1 AND tenant_id=$2`, id, c.Tenant).Scan(&key, &system); e != nil {
			http.Error(w, "role not found", 404)
			return
		}
		if system {
			http.Error(w, "system roles cannot be deleted", 400)
			return
		}
		var users int
		db.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE tenant_id=$1 AND role=$2 AND deleted_at IS NULL`, c.Tenant, key).Scan(&users)
		if users > 0 {
			http.Error(w, "reassign users before deleting this role", 409)
			return
		}
		if _, e := db.Exec(r.Context(), `DELETE FROM roles WHERE id=$1`, id); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		auditUserAction(r, db, "ROLE_DELETED", id, map[string]any{"key": key})
		w.WriteHeader(204)
	}
}
