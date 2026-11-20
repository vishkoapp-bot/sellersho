// Package migrations is the single fail-closed migration runner for the server,
// CLI, test harness and release. Filenames are immutable ordered versions.
package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Historical checksums are pinned to the reviewed committed baseline, never
// derived from an operator's possibly modified deployment directory.
//
//go:embed historical-checksums.json
var historicalJSON []byte

// legacyAliases records duplicate rows written by the production server's
// pre-canonical migrator. The alias and its canonical target must both remain
// present; aliases never stand in for a missing canonical migration.
//
//go:embed legacy-aliases.json
var legacyAliasesJSON []byte

// Global to this PostgreSQL database, shared by every schema/replica/entrypoint.
const advisoryLockID int64 = 0x53454c4c45524d

type Options struct {
	Dir              string
	LockTimeout      time.Duration
	MigrationTimeout time.Duration // Deprecated: use StatementTimeout. When set and StatementTimeout is zero, it is used as the statement timeout.
	// StatementTimeout bounds how long one migration's statements may run.
	// Default 10 minutes (MIG-02 D-5). Previously this was the same knob
	// as the lock timeout (2 minutes for both); they are now separate.
	StatementTimeout time.Duration
	// MigrationLockTimeout bounds how long one migration's transaction
	// waits for DDL/data locks (lock_timeout inside the transaction).
	// Default 30 seconds (MIG-02 D-5).
	MigrationLockTimeout time.Duration
	// TimeoutOverrides carries per-migration budgets from the release
	// manifest (MIG-02 step 5.1 / D-5): keyed by migration filename.
	// An entry for a file that is not in the directory fails the run
	// (fail-closed). A zero field in an entry falls back to the default.
	TimeoutOverrides map[string]MigrationTimeouts
	AdoptLegacy      bool
	Logf             func(string, ...any)
}

// MigrationTimeouts is one migration's lock/statement budget pair.
type MigrationTimeouts struct {
	LockTimeout      time.Duration
	StatementTimeout time.Duration
}

// resolveTimeouts applies the D-5 defaults: singleton lock wait 30s,
// per-migration statement 10min, per-migration lock wait 30s. The
// deprecated MigrationTimeout field still acts as a statement timeout
// when StatementTimeout is unset, so existing callers keep working.
func resolveTimeouts(options Options) (lock, statement, txnLock time.Duration) {
	lock = options.LockTimeout
	if lock <= 0 {
		lock = 30 * time.Second
	}
	statement = options.StatementTimeout
	if statement <= 0 {
		statement = options.MigrationTimeout
	}
	if statement <= 0 {
		statement = 10 * time.Minute
	}
	txnLock = options.MigrationLockTimeout
	if txnLock <= 0 {
		txnLock = 30 * time.Second
	}
	return lock, statement, txnLock
}

// LoadTimeoutOverrides parses the "timeouts" object of a release
// manifest (release.json: migrations.timeouts) into per-migration
// budgets. The JSON is strict: unknown fields and unparseable or
// non-positive durations are rejected, so a typo in the manifest can
// never silently widen or narrow a migration's budget. A nil/empty
// input means "no overrides".
func LoadTimeoutOverrides(data []byte) (map[string]MigrationTimeouts, error) {
	if len(data) == 0 {
		return nil, nil
	}
	// Decode in layers so strictness applies only to the timeout
	// entries themselves: the manifest carries many other fields
	// (files, count, lastCompatibleRevision, ...) that must not be
	// rejected here.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("release manifest timeouts: %w", err)
	}
	rawMig, ok := top["migrations"]
	if !ok {
		return nil, nil
	}
	var mig map[string]json.RawMessage
	if err := json.Unmarshal(rawMig, &mig); err != nil {
		return nil, fmt.Errorf("release manifest timeouts: %w", err)
	}
	rawTimeouts, ok := mig["timeouts"]
	if !ok {
		return nil, nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(rawTimeouts, &entries); err != nil {
		return nil, fmt.Errorf("release manifest timeouts: %w", err)
	}
	out := make(map[string]MigrationTimeouts, len(entries))
	for name, rawEntry := range entries {
		var entry struct {
			LockTimeout      string `json:"lock_timeout"`
			StatementTimeout string `json:"statement_timeout"`
		}
		dec := json.NewDecoder(strings.NewReader(string(rawEntry)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&entry); err != nil {
			return nil, fmt.Errorf("release manifest timeouts: migration %s: %w", name, err)
		}
		var to MigrationTimeouts
		if entry.LockTimeout != "" {
			d, err := time.ParseDuration(entry.LockTimeout)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("release manifest timeouts: migration %s has invalid lock_timeout %q", name, entry.LockTimeout)
			}
			to.LockTimeout = d
		}
		if entry.StatementTimeout != "" {
			d, err := time.ParseDuration(entry.StatementTimeout)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("release manifest timeouts: migration %s has invalid statement_timeout %q", name, entry.StatementTimeout)
			}
			to.StatementTimeout = d
		}
		out[name] = to
	}
	return out, nil
}

type Result struct {
	Version  string
	Duration time.Duration
}

type Report struct{ Applied []Result }

type migration struct{ name, checksum, body string }

var filenamePattern = regexp.MustCompile(`^[0-9]+_[A-Za-z0-9_]+\.up\.sql$`)

func load(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("required migrations directory: %w", err)
	}
	var files []migration
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !filenamePattern.MatchString(entry.Name()) {
			return nil, fmt.Errorf("invalid migration file %q", entry.Name())
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		// Git autocrlf must not create false drift between Windows and Linux.
		body := strings.ReplaceAll(string(content), "\r\n", "\n")
		if strings.TrimSpace(body) == "" {
			return nil, fmt.Errorf("empty migration %s", entry.Name())
		}
		if strings.Contains(body, "-- migrate:transaction off") {
			return nil, fmt.Errorf("migration %s requests unsupported nontransactional execution; see internal/migrations/README.md", entry.Name())
		}
		if err := validateSQL(body); err != nil {
			return nil, fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256([]byte(body))
		files = append(files, migration{entry.Name(), hex.EncodeToString(sum[:]), body})
	}
	if len(files) == 0 {
		return nil, errors.New("required migrations directory contains no up migrations")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

// Run holds a session advisory lock on one dedicated connection, including
// history validation and every DDL transaction. A failure stops immediately.
// The caller should also supply an overall context deadline.
func Run(ctx context.Context, db *sql.DB, options Options) (report Report, err error) {
	files, err := load(options.Dir)
	if err != nil {
		return report, err
	}
	if db == nil {
		return report, errors.New("migration database is required")
	}
	lockTimeout, statementTimeout, txnLockTimeout := resolveTimeouts(options)
	conn, err := db.Conn(ctx)
	if err != nil {
		return report, fmt.Errorf("migration connection: %w", err)
	}
	defer conn.Close()
	lockCtx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	for {
		var locked bool
		if err := conn.QueryRowContext(lockCtx, `SELECT pg_try_advisory_lock($1)`, advisoryLockID).Scan(&locked); err != nil {
			return report, fmt.Errorf("migration singleton lock: %w", err)
		}
		if locked {
			break
		}
		select {
		case <-lockCtx.Done():
			return report, fmt.Errorf("migration singleton lock timeout: %w", lockCtx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		unlockErr := conn.QueryRowContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, advisoryLockID).Scan(&unlocked)
		if unlockErr != nil || !unlocked {
			// Never return a possibly still locked session to the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if err == nil {
				err = errors.New("migration singleton lock release failed")
			}
		}
	}()
	applied, err := prepareHistory(ctx, conn, files, options, statementTimeout, txnLockTimeout)
	if err != nil {
		return report, err
	}
	// A manifest override for a file that is not in the directory is a
	// fail-closed error: it means the manifest and the migration set
	// disagree, and silently ignoring it could run a migration on the
	// wrong budget.
	known := make(map[string]bool, len(files))
	for _, file := range files {
		known[file.name] = true
	}
	for name := range options.TimeoutOverrides {
		if !known[name] {
			return report, fmt.Errorf("migration timeout override for unknown migration %s", name)
		}
	}
	for _, file := range files {
		if applied[file.name] {
			continue
		}
		started := time.Now()
		if options.Logf != nil {
			options.Logf("migration %s starting", file.name)
		}
		fileLock, fileStatement := txnLockTimeout, statementTimeout
		if override, ok := options.TimeoutOverrides[file.name]; ok {
			if override.LockTimeout > 0 {
				fileLock = override.LockTimeout
			}
			if override.StatementTimeout > 0 {
				fileStatement = override.StatementTimeout
			}
		}
		if err := apply(ctx, conn, file, fileLock, fileStatement); err != nil {
			if options.Logf != nil {
				options.Logf("migration %s failed after %s", file.name, time.Since(started))
			}
			return report, fmt.Errorf("migration %s failed: %w", file.name, err)
		}
		result := Result{Version: file.name, Duration: time.Since(started)}
		report.Applied = append(report.Applied, result)
		if options.Logf != nil {
			options.Logf("migration %s applied in %s", file.name, result.Duration)
		}
	}
	return report, nil
}

var dollarQuote = regexp.MustCompile(`^\$(?:[A-Za-z_][A-Za-z0-9_]*)?\$`)

// validateSQL forbids transaction control outside SQL strings/comments/DO
// bodies. Otherwise a file could COMMIT our transaction before its ledger row.
func validateSQL(body string) error {
	start := true
	for i := 0; i < len(body); {
		switch {
		case strings.HasPrefix(body[i:], "--"):
			end := strings.IndexByte(body[i:], '\n')
			if end < 0 {
				return nil
			}
			i += end + 1
		case strings.HasPrefix(body[i:], "/*"):
			i += 2
			depth := 1
			for i < len(body) && depth > 0 {
				if strings.HasPrefix(body[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(body[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
		case body[i] == '\'' || body[i] == '"':
			quote := body[i]
			// Standard PostgreSQL strings and quoted identifiers use doubled
			// quotes. Only E'...' strings recognize backslash quote escapes.
			escapes := quote == '\'' && i > 0 && (body[i-1] == 'E' || body[i-1] == 'e') &&
				(i < 2 || !((body[i-2] >= 'A' && body[i-2] <= 'Z') || (body[i-2] >= 'a' && body[i-2] <= 'z') || (body[i-2] >= '0' && body[i-2] <= '9') || body[i-2] == '_'))
			i++
			for i < len(body) {
				if escapes && body[i] == '\\' {
					i += 2
					continue
				}
				if body[i] == quote {
					i++
					if i < len(body) && body[i] == quote {
						i++
						continue
					}
					break
				}
				i++
			}
			start = false
		case body[i] == '$':
			marker := dollarQuote.FindString(body[i:])
			if marker == "" {
				i++
				continue
			}
			i += len(marker)
			end := strings.Index(body[i:], marker)
			if end < 0 {
				return errors.New("unterminated SQL dollar quote")
			}
			i += end + len(marker)
			start = false
		case body[i] == ';':
			start = true
			i++
		case (body[i] >= 'A' && body[i] <= 'Z') || (body[i] >= 'a' && body[i] <= 'z') || body[i] == '_' || body[i] >= 128:
			begin := i
			// PostgreSQL identifiers may contain dollars and UTF-8 bytes.
			// A dollar inside an identifier cannot begin a dollar-quoted body.
			for i < len(body) && ((body[i] >= 'A' && body[i] <= 'Z') || (body[i] >= 'a' && body[i] <= 'z') || (body[i] >= '0' && body[i] <= '9') || body[i] == '_' || body[i] == '$' || body[i] >= 128) {
				i++
			}
			if start {
				switch strings.ToUpper(body[begin:i]) {
				case "BEGIN", "START", "COMMIT", "ROLLBACK", "END", "ABORT", "PREPARE":
					return errors.New("top-level transaction control is forbidden; the runner owns the transaction")
				}
				start = false
			}
		default:
			i++
		}
	}
	return nil
}

func prepareHistory(ctx context.Context, conn *sql.Conn, files []migration, options Options, statementTimeout, txnLockTimeout time.Duration) (map[string]bool, error) {
	historyCtx, cancel := context.WithTimeout(ctx, statementTimeout)
	defer cancel()
	tx, err := conn.BeginTx(historyCtx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := setTimeouts(historyCtx, tx, txnLockTimeout, statementTimeout); err != nil {
		return nil, err
	}
	var untracked bool
	if err := tx.QueryRowContext(historyCtx, `SELECT to_regclass('schema_migrations') IS NULL AND EXISTS (
		SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p')
		AND c.relname <> 'sellersho_test_guard'
	)`).Scan(&untracked); err != nil {
		return nil, fmt.Errorf("inspect migration history: %w", err)
	}
	if untracked {
		return nil, errors.New("existing nonempty schema has no migration history; reconcile and restore verified history before migration")
	}
	if _, err := tx.ExecContext(historyCtx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), checksum TEXT NOT NULL
	); ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum TEXT`); err != nil {
		return nil, fmt.Errorf("initialize migration history: %w", err)
	}
	rows, err := tx.QueryContext(historyCtx, `SELECT version, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
	}
	checksums := make(map[string]string)
	var legacy []string
	for rows.Next() {
		var name string
		var checksum sql.NullString
		if err := rows.Scan(&name, &checksum); err != nil {
			rows.Close()
			return nil, err
		}
		checksums[name] = checksum.String
		if !checksum.Valid || checksum.String == "" {
			legacy = append(legacy, name)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	known := make(map[string]migration, len(files))
	for _, file := range files {
		known[file.name] = file
	}
	var historical map[string]string
	if err := json.Unmarshal(historicalJSON, &historical); err != nil {
		return nil, errors.New("invalid embedded historical migration manifest")
	}
	var aliases map[string]string
	if err := json.Unmarshal(legacyAliasesJSON, &aliases); err != nil {
		return nil, errors.New("invalid embedded legacy migration aliases")
	}
	resolved := make(map[string]migration, len(checksums))
	var lastApplied string
	for name, checksum := range checksums {
		file, ok := known[name]
		if !ok {
			targetName, aliasOK := aliases[name]
			target, targetOK := known[targetName]
			_, targetApplied := checksums[targetName]
			if !aliasOK || !targetOK || !targetApplied || historical[targetName] != target.checksum {
				return nil, fmt.Errorf("applied migration %s missing from required directory", name)
			}
			file = target
		}
		if checksum != "" && checksum != file.checksum {
			return nil, fmt.Errorf("checksum drift for applied migration %s", name)
		}
		resolved[name] = file
		if _, canonical := known[name]; canonical && name > lastApplied {
			lastApplied = name
		}
	}
	for _, file := range files {
		if _, applied := checksums[file.name]; !applied && file.name < lastApplied {
			return nil, fmt.Errorf("out-of-order migration %s precedes applied history", file.name)
		}
	}
	if len(legacy) > 0 {
		if !options.AdoptLegacy {
			return nil, errors.New("legacy migration history has no checksums; verify the documented baseline and run migrate -adopt-legacy explicitly")
		}
		for _, name := range legacy {
			file := resolved[name]
			manifestName := name
			if target, ok := aliases[name]; ok {
				manifestName = target
			}
			if expected, ok := historical[manifestName]; !ok || expected != file.checksum {
				return nil, fmt.Errorf("legacy migration %s does not match verified historical manifest; adoption refused", name)
			}
		}
		for _, name := range legacy {
			if _, err := tx.ExecContext(historyCtx, `UPDATE schema_migrations SET checksum = $1 WHERE version = $2`, resolved[name].checksum, name); err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.ExecContext(historyCtx, `ALTER TABLE schema_migrations ALTER COLUMN checksum SET NOT NULL`); err != nil {
		return nil, fmt.Errorf("migration checksum constraint: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	applied := make(map[string]bool, len(checksums))
	for name := range checksums {
		applied[name] = true
	}
	return applied, nil
}

// setTimeouts splits the old single per-migration knob (MIG-02 D-5):
// statement_timeout and idle_in_transaction_session_timeout follow the
// statement budget, lock_timeout follows the separate lock budget.
func setTimeouts(ctx context.Context, tx *sql.Tx, txnLock, statement time.Duration) error {
	lockMillis := txnLock.Milliseconds()
	if lockMillis < 1 {
		lockMillis = 1
	}
	stmtMillis := statement.Milliseconds()
	if stmtMillis < 1 {
		stmtMillis = 1
	}
	_, err := tx.ExecContext(ctx, `SELECT set_config('statement_timeout', $1, true), set_config('lock_timeout', $2, true), set_config('idle_in_transaction_session_timeout', $1, true), set_config('standard_conforming_strings', 'on', true)`, fmt.Sprintf("%dms", stmtMillis), fmt.Sprintf("%dms", lockMillis))
	return err
}

func apply(ctx context.Context, conn *sql.Conn, file migration, txnLock, statement time.Duration) error {
	migrationCtx, cancel := context.WithTimeout(ctx, statement)
	defer cancel()
	tx, err := conn.BeginTx(migrationCtx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setTimeouts(migrationCtx, tx, txnLock, statement); err != nil {
		return err
	}
	if _, err := tx.ExecContext(migrationCtx, file.body); err != nil {
		return err
	}
	if _, err := tx.ExecContext(migrationCtx, `INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)`, file.name, file.checksum); err != nil {
		return err
	}
	return tx.Commit()
}
