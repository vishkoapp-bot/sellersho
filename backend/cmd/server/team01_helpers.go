package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
)

// Team 01 (Phase 2, live parity) shared helpers.
//
// Naming: every helper in this file is prefixed t01 to avoid collisions
// with the existing handler package. No helper here invents live
// behaviour: anything whose live contract is unverified is surfaced by
// callers through t01Stub with its U number, never as a guessed value.

// t01WriteJSON writes v as JSON with the given status code.
func t01WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// t01Stub registers a confirmed live route whose body/response is
// unverified. It never guesses: it answers 501 with the U number so an
// authenticated probe (or the merged tree) can later replace it.
func t01Stub(w http.ResponseWriter, r *http.Request, todo, detail string) {
	t01WriteJSON(w, http.StatusNotImplemented, map[string]interface{}{
		"error":  "not_implemented",
		"todo":   todo,
		"route":  r.URL.Path,
		"detail": detail,
	})
}

// t01ClaimsUserID returns the authenticated caller's user id, or "".
func t01ClaimsUserID(r *http.Request) string {
	if claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		return claims.UserID
	}
	return ""
}

// t01DBNil reports (and answers) a missing database handle.
func t01DBNil(w http.ResponseWriter, db *sql.DB) bool {
	if db == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return true
	}
	return false
}

// t01TableExists reports whether a relation exists in the public schema.
func t01TableExists(ctx context.Context, db *sql.DB, table string) bool {
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists)
	return err == nil && exists
}

// t01ColumnExists reports whether a column exists on a table.
func t01ColumnExists(ctx context.Context, db *sql.DB, table, column string) bool {
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2)`,
		table, column).Scan(&exists)
	return err == nil && exists
}

// t01ListRows returns up to limit rows of a table as generic JSON maps.
// It is used only for admin list endpoints whose exact live response
// shape is unverified (marked TODO at each call site): the rows are
// real table rows, never invented fields. orderBy must be a constant
// ORDER BY clause supplied by the caller (never user input). The second
// return value is false when the table does not exist.
func t01ListRows(ctx context.Context, db *sql.DB, table, orderBy string, limit int) ([]map[string]interface{}, bool, error) {
	if !t01TableExists(ctx, db, table) {
		return []map[string]interface{}{}, false, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := "SELECT * FROM " + table + " "
	if orderBy != "" {
		q += orderBy + " "
	}
	q += "LIMIT " + strconv.Itoa(limit)
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, true, err
	}
	out := []map[string]interface{}{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, true, err
		}
		row := map[string]interface{}{}
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				row[c] = string(v)
			case time.Time:
				row[c] = v.Format(time.RFC3339)
			default:
				row[c] = v
			}
		}
		out = append(out, row)
	}
	return out, true, rows.Err()
}

// t01Limit parses ?limit= (default 100, clamped to 1..200).
func t01Limit(r *http.Request) int {
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n >= 1 && n <= 200 {
		return n
	}
	return 100
}

// t01PlatformInt reads one integer column of the platform_settings
// singleton (id = 1), returning fallback when the column or row is
// missing. column must be a caller constant, never user input.
func t01PlatformInt(ctx context.Context, db *sql.DB, column string, fallback int64) int64 {
	if !t01ColumnExists(ctx, db, "platform_settings", column) {
		return fallback
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx,
		"SELECT "+column+" FROM platform_settings WHERE id = 1").Scan(&v); err != nil || !v.Valid {
		return fallback
	}
	return v.Int64
}

// t01PlatformFloat reads one float column of the platform_settings singleton.
func t01PlatformFloat(ctx context.Context, db *sql.DB, column string, fallback float64) float64 {
	if !t01ColumnExists(ctx, db, "platform_settings", column) {
		return fallback
	}
	var v sql.NullFloat64
	if err := db.QueryRowContext(ctx,
		"SELECT "+column+" FROM platform_settings WHERE id = 1").Scan(&v); err != nil || !v.Valid {
		return fallback
	}
	return v.Float64
}

// t01PlatformText reads one text column of the platform_settings singleton.
func t01PlatformText(ctx context.Context, db *sql.DB, column string, fallback string) string {
	if !t01ColumnExists(ctx, db, "platform_settings", column) {
		return fallback
	}
	var v sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT "+column+" FROM platform_settings WHERE id = 1").Scan(&v); err != nil || !v.Valid {
		return fallback
	}
	return v.String
}

// t01PlatformJSON reads one JSONB column of the platform_settings singleton.
func t01PlatformJSON(ctx context.Context, db *sql.DB, column string) json.RawMessage {
	if !t01ColumnExists(ctx, db, "platform_settings", column) {
		return json.RawMessage("null")
	}
	var raw []byte
	if err := db.QueryRowContext(ctx,
		"SELECT COALESCE("+column+"::text, 'null') FROM platform_settings WHERE id = 1").Scan(&raw); err != nil {
		return json.RawMessage("null")
	}
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(raw)
}

// t01SetPlatformColumn updates one platform_settings column (singleton
// id = 1) when the column exists. column must be a caller constant.
func t01SetPlatformColumn(ctx context.Context, db *sql.DB, column string, value interface{}) error {
	if !t01ColumnExists(ctx, db, "platform_settings", column) {
		return sql.ErrNoRows
	}
	_, err := db.ExecContext(ctx,
		"UPDATE platform_settings SET "+column+" = $1 WHERE id = 1", value)
	return err
}

// t01SellerStoreIDs returns the ids of stores owned by a user.
func t01SellerStoreIDs(ctx context.Context, db *sql.DB, userID string) []string {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM stores WHERE owner_user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return []string{}
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// t01SellerStoreSlugs returns the slugs of stores owned by a user.
func t01SellerStoreSlugs(ctx context.Context, db *sql.DB, userID string) []string {
	rows, err := db.QueryContext(ctx,
		`SELECT slug FROM stores WHERE owner_user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return []string{}
	}
	defer rows.Close()
	slugs := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			slugs = append(slugs, s)
		}
	}
	return slugs
}

// t01TehranDay returns the current calendar day in Asia/Tehran (the
// day basis of the view/search rollups); it falls back to UTC when the
// zone data is unavailable.
func t01TehranDay(now time.Time) time.Time {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		return now.UTC().Truncate(24 * time.Hour)
	}
	t := now.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// t01HashToken returns the SHA-256 hex of a client daily token. Only the
// hash is ever stored (migration 202610230002 privacy rule).
func t01HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// t01NormalizeMobile strips spaces and converts +98/98 prefixes to 0.
func t01NormalizeMobile(m string) string {
	m = strings.ReplaceAll(strings.TrimSpace(m), " ", "")
	m = strings.ReplaceAll(m, "-", "")
	if strings.HasPrefix(m, "+98") {
		return "0" + m[3:]
	}
	if strings.HasPrefix(m, "98") && len(m) > 10 {
		return "0" + m[2:]
	}
	return m
}

// t01DecodeJSON decodes a request body into v, reporting false on error.
func t01DecodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return false
	}
	return true
}

// writeMethodNotAllowed answers 405 with the live JSON body
// {"error":"Method not allowed"} (live probe: 405 is JSON, not the
// Go default plain text). Used by t01MethodOnly and every handler
// that used to answer plain-text 405.
func writeMethodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_, _ = w.Write([]byte(`{"error":"Method not allowed"}`))
}

// t01MethodOnly answers 405 unless the request uses one of the methods.
func t01MethodOnly(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	writeMethodNotAllowed(w)
	return false
}

// t01PlatformBool reads one boolean column of the platform_settings
// singleton (id = 1), returning fallback when the column or row is
// missing. column must be a caller constant, never user input.
func t01PlatformBool(ctx context.Context, db *sql.DB, column string, fallback bool) bool {
	if !t01ColumnExists(ctx, db, "platform_settings", column) {
		return fallback
	}
	var v sql.NullBool
	if err := db.QueryRowContext(ctx,
		"SELECT "+column+" FROM platform_settings WHERE id = 1").Scan(&v); err != nil || !v.Valid {
		return fallback
	}
	return v.Bool
}
