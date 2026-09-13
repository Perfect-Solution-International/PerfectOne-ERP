package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
	"time"
)

type UserRecord struct {
	ID, Name, Email, Username, Role string
	IsActive                        bool       `json:"isActive"`
	IsDeleted                       bool       `json:"isDeleted"`
	LastLoginAt                     *time.Time `json:"lastLoginAt"`
	Permissions                     []string   `json:"permissions"`
}

func registerUsers(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /users", jsonAPI(authenticated(db, "super_admin", "admin")(listUsers(db))))
	m.HandleFunc("POST /users", jsonAPI(authenticated(db, "super_admin", "admin")(createUser(db))))
	m.HandleFunc("PUT /users/{id}", jsonAPI(authenticated(db, "super_admin", "admin")(updateUser(db))))
	m.HandleFunc("DELETE /users/{id}", jsonAPI(authenticated(db, "super_admin", "admin")(deleteUser(db))))
	m.HandleFunc("POST /users/{id}/restore", jsonAPI(authenticated(db, "super_admin", "admin")(restoreUser(db))))
	m.HandleFunc("POST /users/{id}/reset-password", jsonAPI(authenticated(db, "super_admin", "admin")(resetPassword(db))))
	m.HandleFunc("GET /users/{id}/login-history", jsonAPI(authenticated(db, "super_admin", "admin")(userLoginHistory(db))))
	m.HandleFunc("GET /users/{id}/activity", jsonAPI(authenticated(db, "super_admin", "admin")(userActivity(db))))
	m.HandleFunc("PUT /users/{id}/permissions", jsonAPI(authenticated(db, "super_admin", "admin")(updatePermissions(db))))
}
func auditUserAction(r *http.Request, db *pgxpool.Pool, action, target string, details any) {
	c := claimsFrom(r)
	payload, _ := json.Marshal(details)
	db.Exec(r.Context(), `INSERT INTO audit_logs(tenant_id,user_id,action,entity_type,entity_id,new_data,ip_address) VALUES($1,$2,$3,'user',$4,NULLIF($5,'')::jsonb,NULLIF($6,'')::inet)`, c.Tenant, c.Sub, action, target, string(payload), clientIP(r))
}
func listUsers(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT id,name,email,COALESCE(username,''),role,is_active,deleted_at IS NOT NULL,last_login_at FROM users WHERE tenant_id=$1 ORDER BY deleted_at NULLS FIRST,name`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []UserRecord{}
		for rows.Next() {
			var u UserRecord
			if e = rows.Scan(&u.ID, &u.Name, &u.Email, &u.Username, &u.Role, &u.IsActive, &u.IsDeleted, &u.LastLoginAt); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			u.Permissions, e = effectivePermissions(r.Context(), db, u.ID, u.Role)
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			out = append(out, u)
		}
		json.NewEncoder(w).Encode(out)
	}
}
func createUser(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct{ Name, Email, Username, Password, Role string }
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Name) == "" || strings.TrimSpace(x.Email) == "" || strings.TrimSpace(x.Username) == "" || len(x.Password) < 8 || x.Role == "" {
			http.Error(w, "name, email, username, role and an 8+ character password required", 400)
			return
		}
		c := claimsFrom(r)
		var id string
		e := db.QueryRow(r.Context(), `INSERT INTO users(name,email,username,password_hash,role,tenant_id,branch_id) VALUES($1,lower($2),lower($3),crypt($4,gen_salt('bf')),$5,$6,$7) RETURNING id`, strings.TrimSpace(x.Name), strings.TrimSpace(x.Email), strings.TrimSpace(x.Username), x.Password, x.Role, c.Tenant, c.Branch).Scan(&id)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		auditUserAction(r, db, "USER_CREATED", id, map[string]any{"name": x.Name, "role": x.Role})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}
func updateUser(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Name, Email, Username, Role string
			IsActive                    bool `json:"isActive"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.Name == "" || x.Email == "" || x.Username == "" || x.Role == "" {
			http.Error(w, "name, email, username and role required", 400)
			return
		}
		c := claimsFrom(r)
		id := r.PathValue("id")
		if id == c.Sub && !x.IsActive {
			http.Error(w, "you cannot deactivate your own account", 400)
			return
		}
		tag, e := db.Exec(r.Context(), `UPDATE users SET name=$1,email=lower($2),username=lower($3),role=$4,is_active=$5 WHERE id=$6 AND tenant_id=$7 AND deleted_at IS NULL`, strings.TrimSpace(x.Name), strings.TrimSpace(x.Email), strings.TrimSpace(x.Username), x.Role, x.IsActive, id, c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "active user not found", 404)
			return
		}
		auditUserAction(r, db, "USER_UPDATED", id, map[string]any{"name": x.Name, "role": x.Role, "active": x.IsActive})
		w.WriteHeader(204)
	}
}
func resetPassword(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Password string `json:"password"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || len(x.Password) < 8 {
			http.Error(w, "password must have at least 8 characters", 400)
			return
		}
		c := claimsFrom(r)
		id := r.PathValue("id")
		tag, e := db.Exec(r.Context(), `UPDATE users SET password_hash=crypt($1,gen_salt('bf')) WHERE id=$2 AND tenant_id=$3 AND deleted_at IS NULL`, x.Password, id, c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "user not found", 404)
			return
		}
		auditUserAction(r, db, "PASSWORD_RESET", id, map[string]any{})
		w.WriteHeader(204)
	}
}
func deleteUser(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := r.PathValue("id")
		if id == c.Sub {
			http.Error(w, "you cannot delete your own account", 400)
			return
		}
		tag, e := db.Exec(r.Context(), `UPDATE users SET deleted_at=now(),deleted_by=$1,is_active=false WHERE id=$2 AND tenant_id=$3 AND deleted_at IS NULL`, c.Sub, id, c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "user not found or already deleted", 404)
			return
		}
		auditUserAction(r, db, "USER_DELETED", id, map[string]any{"softDelete": true})
		w.WriteHeader(204)
	}
}
func restoreUser(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		id := r.PathValue("id")
		tag, e := db.Exec(r.Context(), `UPDATE users SET deleted_at=NULL,deleted_by=NULL,is_active=true WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NOT NULL`, id, c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "deleted user not found", 404)
			return
		}
		auditUserAction(r, db, "USER_RESTORED", id, map[string]any{})
		w.WriteHeader(204)
	}
}
func userLoginHistory(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		rows, e := db.Query(r.Context(), `SELECT h.id,h.identifier,h.success,COALESCE(h.ip_address::text,''),COALESCE(h.user_agent,''),h.created_at FROM login_history h JOIN users u ON u.id=h.user_id WHERE h.user_id=$1 AND u.tenant_id=$2 ORDER BY h.created_at DESC LIMIT 100`, r.PathValue("id"), c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, identifier, ip, agent string
			var success bool
			var at time.Time
			if e = rows.Scan(&id, &identifier, &success, &ip, &agent, &at); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			out = append(out, map[string]any{"id": id, "identifier": identifier, "success": success, "ipAddress": ip, "userAgent": agent, "createdAt": at})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func userActivity(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		rows, e := db.Query(r.Context(), `SELECT id,action,entity_type,COALESCE(entity_id::text,''),COALESCE(new_data,'{}'),created_at FROM audit_logs WHERE user_id=$1 AND tenant_id=$2 ORDER BY created_at DESC LIMIT 200`, r.PathValue("id"), c.Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, action, entity, entityID string
			var details any
			var at time.Time
			if e = rows.Scan(&id, &action, &entity, &entityID, &details, &at); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			out = append(out, map[string]any{"id": id, "action": action, "entityType": entity, "entityId": entityID, "details": details, "createdAt": at})
		}
		json.NewEncoder(w).Encode(out)
	}
}
func updatePermissions(db *pgxpool.Pool) http.HandlerFunc {
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
		var role string
		if e := db.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, id, c.Tenant).Scan(&role); e != nil {
			http.Error(w, "user not found", 404)
			return
		}
		if role == "super_admin" {
			http.Error(w, "super admin permissions cannot be restricted", 400)
			return
		}
		selected := map[string]bool{}
		for _, p := range x.Permissions {
			selected[p] = true
		}
		tx, e := db.Begin(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())
		for _, p := range permissionKeys {
			if p == "catalog_read" {
				continue
			}
			_, e = tx.Exec(r.Context(), `INSERT INTO user_permissions(user_id,permission,enabled,updated_at) VALUES($1,$2,$3,now()) ON CONFLICT(user_id,permission) DO UPDATE SET enabled=excluded.enabled,updated_at=now()`, id, p, selected[p])
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		auditUserAction(r, db, "PERMISSIONS_UPDATED", id, map[string]any{"permissions": x.Permissions})
		w.WriteHeader(204)
	}
}
