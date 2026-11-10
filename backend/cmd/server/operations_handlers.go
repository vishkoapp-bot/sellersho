package main

import (
	"database/sql"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

func handleOperationsOverview(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	ctx := r.Context()
	out := map[string]interface{}{}
	queries := map[string]string{
		"jobs":        `SELECT job,affected,error,started_at,finished_at FROM operations_job_runs ORDER BY id DESC LIMIT 30`,
		"overdueQC":   `SELECT q.id,q.supplier_id,q.product_id,q.reason,q.response_due_at,q.escalated_at FROM qc_discrepancies q WHERE q.status='open' AND q.responded_at IS NULL AND q.response_due_at<=NOW() ORDER BY q.response_due_at LIMIT 100`,
		"supplierSLA": `SELECT m.supplier_id,s.name,m.samples,m.on_time,m.score,m.eligible,m.assessed_at FROM supplier_sla_metrics m JOIN suppliers s ON s.id=m.supplier_id ORDER BY m.score DESC,m.supplier_id LIMIT 100`,
	}
	for key, query := range queries {
		rows, e := db.QueryContext(ctx, query)
		if e != nil {
			writeInternalError(w, e)
			return
		}
		cols, e := rows.Columns()
		if e != nil {
			rows.Close()
			writeInternalError(w, e)
			return
		}
		list := []map[string]interface{}{}
		for rows.Next() {
			values := make([]interface{}, len(cols))
			ptr := make([]interface{}, len(cols))
			for i := range ptr {
				ptr[i] = &values[i]
			}
			if e = rows.Scan(ptr...); e != nil {
				rows.Close()
				writeInternalError(w, e)
				return
			}
			v := map[string]interface{}{}
			for i, c := range cols {
				if b, ok := values[i].([]byte); ok {
					v[c] = string(b)
				} else {
					v[c] = values[i]
				}
			}
			list = append(list, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			writeInternalError(w, e)
			return
		}
		out[key] = list
	}
	t01WriteJSON(w, 200, out)
}
func handleResolveOverdueQC(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/operations/qc/"), "/resolve")
	var p struct {
		Resolution string `json:"resolution"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&p) != nil || strings.TrimSpace(p.Resolution) == "" {
		http.Error(w, "نتیجه بررسی الزامی است", 400)
		return
	}
	actor := r.Context().Value(userClaimsKey).(*auth.UserClaims).UserID
	tx, e := db.BeginTx(r.Context(), nil)
	if e != nil {
		writeInternalError(w, e)
		return
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(r.Context(), `UPDATE qc_discrepancies SET status='resolved',resolved_at=NOW(),admin_resolution=$2,reviewed_by=$3,escalated_at=COALESCE(escalated_at,NOW()) WHERE id=$1 AND status='open' AND responded_at IS NULL AND response_due_at<=NOW()`, id, p.Resolution, actor)
	if e != nil {
		writeInternalError(w, e)
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		http.Error(w, "پروندهٔ منقضی باز پیدا نشد", 409)
		return
	}
	if _, e = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(id,operator,action,category,details) VALUES($1,$2,'qc_deadline_review','warehouse',$3)`, "qc-"+uuid.NewString(), actor, id+": "+p.Resolution); e != nil {
		writeInternalError(w, e)
		return
	}
	if e = tx.Commit(); e != nil {
		writeInternalError(w, e)
		return
	}
	t01WriteJSON(w, 200, map[string]bool{"success": true})
}
