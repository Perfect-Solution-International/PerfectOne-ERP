package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net"
	"net/http"
	"strings"
	"time"
)

type LoginRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}
type LoginResponse struct {
	Token, Name, Role, UserID, TenantID, BranchID string
	Permissions                                   []string
}

func b64(v []byte) string { return base64.RawURLEncoding.EncodeToString(v) }
func token(id, role, tenant, branch string) string {
	head := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, _ := json.Marshal(map[string]any{"sub": id, "role": role, "tenant": tenant, "branch": branch, "exp": time.Now().Add(8 * time.Hour).Unix()})
	raw := head + "." + b64(body)
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(raw))
	return raw + "." + b64(mac.Sum(nil))
}
func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e == nil {
		return host
	}
	return strings.Trim(r.RemoteAddr, "[]")
}
func login(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in LoginRequest
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Password == "" {
			http.Error(w, "username/email and password required", 400)
			return
		}
		idn := strings.ToLower(strings.TrimSpace(in.Identifier))
		if idn == "" {
			idn = strings.ToLower(strings.TrimSpace(in.Email))
		}
		if idn == "" {
			http.Error(w, "username/email and password required", 400)
			return
		}
		var id, name, role, tenant, branch string
		e := db.QueryRow(r.Context(), `SELECT id,name,role,tenant_id,branch_id FROM users WHERE (lower(email)=$1 OR lower(username)=$1) AND is_active AND deleted_at IS NULL AND password_hash=crypt($2,password_hash)`, idn, in.Password).Scan(&id, &name, &role, &tenant, &branch)
		ip := clientIP(r)
		if e != nil {
			db.Exec(r.Context(), `INSERT INTO login_history(identifier,success,ip_address,user_agent) VALUES($1,false,NULLIF($2,'')::inet,$3)`, idn, ip, r.UserAgent())
			http.Error(w, "invalid username/email or password", 401)
			return
		}
		db.Exec(r.Context(), `UPDATE users SET last_login_at=now() WHERE id=$1`, id)
		db.Exec(r.Context(), `INSERT INTO login_history(tenant_id,user_id,identifier,success,ip_address,user_agent) VALUES($1,$2,$3,true,NULLIF($4,'')::inet,$5)`, tenant, id, idn, ip, r.UserAgent())
		permissions, e := effectivePermissions(r.Context(), db, id, role)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(LoginResponse{token(id, role, tenant, branch), name, role, id, tenant, branch, permissions})
	}
}
func logout(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		c := claimsFrom(r)
		db.Exec(r.Context(), `INSERT INTO revoked_tokens(token_hash,expires_at)VALUES($1,to_timestamp($2)) ON CONFLICT DO NOTHING`, tokenHash(raw), c.Exp)
		w.WriteHeader(http.StatusNoContent)
	}
}
