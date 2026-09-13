package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var safeBackupID = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

func registerBackups(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /backups", jsonAPI(authenticated(db, "super_admin")(listBackups(db))))
	m.HandleFunc("POST /backups", jsonAPI(authenticated(db, "super_admin")(createBackup(db))))
	m.HandleFunc("GET /backups/{id}/download", authenticated(db, "super_admin")(downloadBackup(db)))
	m.HandleFunc("POST /backups/{id}/restore", jsonAPI(authenticated(db, "super_admin")(restoreBackup(db))))
	m.HandleFunc("GET /backups/schedule", jsonAPI(authenticated(db, "super_admin")(getBackupSchedule(db))))
	m.HandleFunc("PUT /backups/schedule", jsonAPI(authenticated(db, "super_admin")(updateBackupSchedule(db))))
}
func listBackups(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT id,file_name,status,size_bytes,COALESCE(checksum,''),COALESCE(error_message,''),created_at,completed_at,trigger_type FROM backup_history WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 50`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, status, sum, msg, trigger string
			var size int64
			var made, done any
			if rows.Scan(&id, &name, &status, &size, &sum, &msg, &made, &done, &trigger) == nil {
				out = append(out, map[string]any{"id": id, "fileName": name, "status": status, "sizeBytes": size, "checksum": sum, "error": msg, "createdAt": made, "completedAt": done, "triggerType": trigger})
			}
		}
		json.NewEncoder(w).Encode(out)
	}
}
func executeBackup(r *http.Request, db *pgxpool.Pool) (string, error) {
	c := claimsFrom(r)
	return executeBackupContext(r.Context(), db, c.Tenant, c.Sub, "manual")
}
func executeBackupContext(ctx context.Context, db *pgxpool.Pool, tenant, user, trigger string) (string, error) {
	var tenantCount int
	if e := db.QueryRow(ctx, `SELECT count(*) FROM tenants`).Scan(&tenantCount); e != nil || tenantCount != 1 {
		return "", fmt.Errorf("database backup requires a single-tenant deployment")
	}
	name := fmt.Sprintf("grocerly-%s.dump", time.Now().UTC().Format("20060102-150405.000"))
	var id string
	if e := db.QueryRow(ctx, `INSERT INTO backup_history(tenant_id,file_name,status,created_by,trigger_type)VALUES($1,$2,'creating',NULLIF($3,'')::uuid,$4)RETURNING id`, tenant, name, user, trigger).Scan(&id); e != nil {
		return "", e
	}
	path := filepath.Join("/backups", id+".dump")
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-owner", "--file", path, os.Getenv("DATABASE_URL"))
	if output, e := cmd.CombinedOutput(); e != nil {
		db.Exec(ctx, `UPDATE backup_history SET status='failed',error_message=$2,completed_at=now() WHERE id=$1`, id, string(output))
		return "", e
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	hash := sha256.New()
	size, _ := io.Copy(hash, f)
	sum := hex.EncodeToString(hash.Sum(nil))
	_, e = db.Exec(ctx, `UPDATE backup_history SET status='completed',size_bytes=$2,checksum=$3,completed_at=now() WHERE id=$1`, id, size, sum)
	return id, e
}
func createBackup(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, e := executeBackup(r, db)
		if e != nil {
			http.Error(w, "backup creation failed", 500)
			return
		}
		auditUserAction(r, db, "DATABASE_BACKUP_CREATED", id, map[string]any{})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "completed"})
	}
}
func backupPath(r *http.Request, db *pgxpool.Pool) (string, string, error) {
	id := r.PathValue("id")
	if !safeBackupID.MatchString(id) {
		return "", "", fmt.Errorf("invalid backup")
	}
	var name string
	e := db.QueryRow(r.Context(), `SELECT file_name FROM backup_history WHERE id=$1 AND tenant_id=$2 AND status IN('completed','restored')`, id, claimsFrom(r).Tenant).Scan(&name)
	return filepath.Join("/backups", id+".dump"), name, e
}
func downloadBackup(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path, name, e := backupPath(r, db)
		if e != nil {
			http.Error(w, "backup not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
		http.ServeFile(w, r, path)
	}
}
func restoreBackup(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Confirmation string `json:"confirmation"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.Confirmation != "RESTORE DATABASE" {
			http.Error(w, "type RESTORE DATABASE to confirm", 400)
			return
		}
		path, _, e := backupPath(r, db)
		if e != nil {
			http.Error(w, "backup not found", 404)
			return
		}
		claims := claimsFrom(r)
		safety, e := executeBackupContext(r.Context(), db, claims.Tenant, claims.Sub, "pre_restore")
		if e != nil {
			http.Error(w, "pre-restore safety backup failed", 409)
			return
		}
		cmd := exec.CommandContext(r.Context(), "pg_restore", "--clean", "--if-exists", "--no-owner", "--dbname", os.Getenv("DATABASE_URL"), path)
		if output, e := cmd.CombinedOutput(); e != nil {
			http.Error(w, "restore failed; safety backup "+safety+": "+string(output), 500)
			return
		}
		db.Exec(r.Context(), `UPDATE backup_history SET status='restored' WHERE id=$1`, r.PathValue("id"))
		auditUserAction(r, db, "DATABASE_RESTORED", r.PathValue("id"), map[string]any{"safetyBackup": safety})
		json.NewEncoder(w).Encode(map[string]string{"status": "restored", "safetyBackupId": safety})
	}
}

type backupSchedule struct {
	Enabled        bool       `json:"enabled"`
	Frequency      string     `json:"frequency"`
	RunAt          string     `json:"runAt"`
	Weekday        int        `json:"weekday"`
	Timezone       string     `json:"timezone"`
	RetentionCount int        `json:"retentionCount"`
	NextRunAt      *time.Time `json:"nextRunAt"`
	LastRunAt      *time.Time `json:"lastRunAt"`
	LastStatus     string     `json:"lastStatus"`
	LastError      string     `json:"lastError"`
}

func scanBackupSchedule(row interface{ Scan(...any) error }) (backupSchedule, error) {
	var x backupSchedule
	e := row.Scan(&x.Enabled, &x.Frequency, &x.RunAt, &x.Weekday, &x.Timezone, &x.RetentionCount, &x.NextRunAt, &x.LastRunAt, &x.LastStatus, &x.LastError)
	return x, e
}

func getBackupSchedule(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := claimsFrom(r).Tenant
		_, _ = db.Exec(r.Context(), `INSERT INTO backup_schedules(tenant_id) VALUES($1) ON CONFLICT DO NOTHING`, tenant)
		x, e := scanBackupSchedule(db.QueryRow(r.Context(), `SELECT enabled,frequency,to_char(run_at,'HH24:MI'),weekday,timezone,retention_count,next_run_at,last_run_at,COALESCE(last_status,''),COALESCE(last_error,'') FROM backup_schedules WHERE tenant_id=$1`, tenant))
		if e != nil { http.Error(w, e.Error(), 500); return }
		json.NewEncoder(w).Encode(x)
	}
}

func nextBackupRun(now time.Time, frequency, runAt, timezone string, weekday int) (time.Time, error) {
	location, e := time.LoadLocation(timezone)
	if e != nil { return time.Time{}, fmt.Errorf("invalid timezone") }
	clock, e := time.Parse("15:04", runAt)
	if e != nil { return time.Time{}, fmt.Errorf("run time must use HH:MM") }
	localNow := now.In(location)
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
	if frequency == "daily" {
		if !candidate.After(localNow) { candidate = candidate.AddDate(0, 0, 1) }
	} else {
		days := (weekday - int(localNow.Weekday()) + 7) % 7
		candidate = candidate.AddDate(0, 0, days)
		if !candidate.After(localNow) { candidate = candidate.AddDate(0, 0, 7) }
	}
	return candidate.UTC(), nil
}

func updateBackupSchedule(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x backupSchedule
		if json.NewDecoder(r.Body).Decode(&x) != nil { http.Error(w, "invalid schedule", 400); return }
		x.Frequency = strings.ToLower(strings.TrimSpace(x.Frequency)); x.Timezone = strings.TrimSpace(x.Timezone)
		if x.Frequency != "daily" && x.Frequency != "weekly" { http.Error(w, "frequency must be daily or weekly", 400); return }
		if x.Weekday < 0 || x.Weekday > 6 || x.RetentionCount < 1 || x.RetentionCount > 365 { http.Error(w, "invalid weekday or retention", 400); return }
		next, e := nextBackupRun(time.Now(), x.Frequency, x.RunAt, x.Timezone, x.Weekday)
		if e != nil { http.Error(w, e.Error(), 400); return }
		claims := claimsFrom(r)
		_, e = db.Exec(r.Context(), `INSERT INTO backup_schedules(tenant_id,enabled,frequency,run_at,weekday,timezone,retention_count,next_run_at,updated_by,updated_at) VALUES($1,$2,$3,$4::time,$5,$6,$7,$8,NULLIF($9,'')::uuid,now()) ON CONFLICT(tenant_id) DO UPDATE SET enabled=EXCLUDED.enabled,frequency=EXCLUDED.frequency,run_at=EXCLUDED.run_at,weekday=EXCLUDED.weekday,timezone=EXCLUDED.timezone,retention_count=EXCLUDED.retention_count,next_run_at=EXCLUDED.next_run_at,updated_by=EXCLUDED.updated_by,updated_at=now()`, claims.Tenant, x.Enabled, x.Frequency, x.RunAt, x.Weekday, x.Timezone, x.RetentionCount, next, claims.Sub)
		if e != nil { http.Error(w, e.Error(), 500); return }
		auditUserAction(r, db, "BACKUP_SCHEDULE_UPDATED", claims.Tenant, map[string]any{"enabled": x.Enabled, "frequency": x.Frequency, "nextRunAt": next})
		getBackupSchedule(db)(w, r)
	}
}

func startBackupScheduler(db *pgxpool.Pool) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for { runDueBackups(context.Background(), db); <-ticker.C }
	}()
}

func runDueBackups(ctx context.Context, db *pgxpool.Pool) {
	rows, e := db.Query(ctx, `SELECT tenant_id::text,frequency,to_char(run_at,'HH24:MI'),weekday,timezone,retention_count,next_run_at FROM backup_schedules WHERE enabled AND next_run_at<=now()`)
	if e != nil { return }
	type due struct { tenant, frequency, runAt string; weekday, retention int; timezone string; expected time.Time }
	jobs := []due{}
	for rows.Next() { var x due; if rows.Scan(&x.tenant,&x.frequency,&x.runAt,&x.weekday,&x.timezone,&x.retention,&x.expected)==nil { jobs=append(jobs,x) } }
	rows.Close()
	for _, job := range jobs {
		next, e := nextBackupRun(time.Now(), job.frequency, job.runAt, job.timezone, job.weekday); if e != nil { continue }
		result, e := db.Exec(ctx, `UPDATE backup_schedules SET next_run_at=$3,last_run_at=now(),last_status='running',last_error=NULL WHERE tenant_id=$1 AND next_run_at=$2`, job.tenant, job.expected, next)
		if e != nil || result.RowsAffected()!=1 { continue }
		_, e = executeBackupContext(ctx, db, job.tenant, "", "scheduled")
		status, failure := "completed", ""; if e != nil { status="failed"; failure=e.Error(); log.Printf("scheduled backup failed for tenant %s: %v", job.tenant, e) }
		_, _ = db.Exec(ctx, `UPDATE backup_schedules SET last_status=$2,last_error=NULLIF($3,'') WHERE tenant_id=$1`, job.tenant, status, failure)
		if e == nil { pruneScheduledBackups(ctx, db, job.tenant, job.retention) }
	}
}

func pruneScheduledBackups(ctx context.Context, db *pgxpool.Pool, tenant string, retain int) {
	rows, e := db.Query(ctx, `SELECT id::text FROM backup_history WHERE tenant_id=$1 AND trigger_type='scheduled' AND status='completed' ORDER BY created_at DESC OFFSET $2`, tenant, retain)
	if e != nil { return }
	ids := []string{}; for rows.Next() { var id string; if rows.Scan(&id)==nil { ids=append(ids,id) } }; rows.Close()
	for _, id := range ids { if safeBackupID.MatchString(id) { _ = os.Remove(filepath.Join("/backups", id+".dump")); _, _ = db.Exec(ctx, `DELETE FROM backup_history WHERE id=$1 AND tenant_id=$2 AND trigger_type='scheduled'`, id, tenant) } }
}
