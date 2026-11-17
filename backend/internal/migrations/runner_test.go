package migrations

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// Every fixture connection is checked by testdb before schema creation. Removing
// transactional execution, checksum validation or the advisory lock breaks the
// corresponding persisted-state assertions below.
func fixtureDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	base, _ := testdb.Open(t, dsn)
	schema := "mig_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := base.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	dsn += separator + "search_path=" + schema
	db, _ := testdb.Open(t, dsn)
	return db, dsn
}

func migrationFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMissingDirectoryRefused(t *testing.T) {
	if _, err := Run(context.Background(), nil, Options{Dir: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("missing required directory was accepted")
	}
}

func TestTransactionEscapeAfterStandardStringRefused(t *testing.T) {
	for _, body := range []string{
		`SELECT '\'; COMMIT; SELECT 1/0; -- '`,
		`SELECT "\"; COMMIT; -- "`,
		`CREATE TABLE t(foo$tag$ INT); COMMIT; SELECT 1/0; -- $tag$`,
	} {
		if err := validateSQL(body); err == nil {
			t.Fatalf("transaction escape hidden by quote parsing: %s", body)
		}
	}
}

func TestTransactionWordsInsideLiteralsAllowed(t *testing.T) {
	for _, body := range []string{
		`DO $$ BEGIN RAISE NOTICE 'COMMIT;'; END $$; SELECT 1`,
		`SELECT E'\'; COMMIT; still literal'; SELECT 1`,
		`SELECT 'doubled ''; COMMIT; still literal'; SELECT 1`,
	} {
		if err := validateSQL(body); err != nil {
			t.Fatalf("valid SQL rejected: %v", err)
		}
	}
}

func TestTransactionEscapeRefusedBeforeAnyDDL(t *testing.T) {
	db, _ := fixtureDB(t)
	dir := t.TempDir()
	migrationFile(t, dir, "001_escape.up.sql", `CREATE TABLE escaped_commit (id INT); COMMIT; SELECT 1/0`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err == nil {
		t.Fatal("transaction escape accepted")
	}
	if count(t, db, `SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename = 'escaped_commit'`) != 0 {
		t.Fatal("migration escaped the runner transaction and left partial DDL")
	}
}

func TestUntrackedExistingSchemaRefused(t *testing.T) {
	db, _ := fixtureDB(t)
	if _, err := db.Exec(`CREATE TABLE existing_business_data (id INT); INSERT INTO existing_business_data VALUES (7)`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	migrationFile(t, dir, "001_initial.up.sql", `DROP TABLE existing_business_data`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err == nil {
		t.Fatal("untracked existing schema was treated as fresh")
	}
	if count(t, db, `SELECT count(*) FROM existing_business_data WHERE id = 7`) != 1 {
		t.Fatal("untracked data changed")
	}
}

func TestFreshUpgradeAndImmutableHistory(t *testing.T) {
	db, _ := fixtureDB(t)
	dir := t.TempDir()
	migrationFile(t, dir, "001_first.up.sql", `CREATE TABLE sample (id INT PRIMARY KEY); INSERT INTO sample VALUES (1)`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	migrationFile(t, dir, "002_second.up.sql", `ALTER TABLE sample ADD COLUMN label TEXT; UPDATE sample SET label = 'upgraded'`)
	report, err := Run(context.Background(), db, Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Applied) != 1 || report.Applied[0].Version != "002_second.up.sql" || report.Applied[0].Duration <= 0 {
		t.Fatalf("missing upgrade timing: %+v", report)
	}
	if count(t, db, `SELECT count(*) FROM sample WHERE label = 'upgraded'`) != 1 {
		t.Fatal("upgrade lost existing row")
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations WHERE length(checksum) = 64`) != 2 {
		t.Fatal("checksums missing")
	}
	migrationFile(t, dir, "001_first.up.sql", `DROP TABLE sample`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err == nil {
		t.Fatal("changed applied migration was accepted")
	}
	if count(t, db, `SELECT count(*) FROM sample`) != 1 {
		t.Fatal("drift executed")
	}
	if err := os.Remove(filepath.Join(dir, "001_first.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err == nil {
		t.Fatal("missing applied migration was accepted")
	}
}

func TestFailureRollsBackAndRetryIsSafe(t *testing.T) {
	db, _ := fixtureDB(t)
	dir := t.TempDir()
	migrationFile(t, dir, "001_ok.up.sql", `CREATE TABLE first_success (id INT)`)
	migrationFile(t, dir, "002_fail.up.sql", `CREATE TABLE partial_failure (id INT); SELECT 1/0`)
	migrationFile(t, dir, "003_later.up.sql", `CREATE TABLE never_reached (id INT)`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err == nil {
		t.Fatal("SQL failure reported success")
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations`) != 1 {
		t.Fatal("failed migration marked applied")
	}
	if count(t, db, `SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename IN ('partial_failure','never_reached')`) != 0 {
		t.Fatal("partial or later DDL persisted")
	}
	migrationFile(t, dir, "002_fail.up.sql", `CREATE TABLE partial_failure (id INT)`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations`) != 3 {
		t.Fatal("retry did not finish")
	}
}

func TestConcurrentRunnersSerialize(t *testing.T) {
	db, _ := fixtureDB(t)
	db.SetMaxOpenConns(4)
	dir := t.TempDir()
	migrationFile(t, dir, "001_once.up.sql", `CREATE TABLE exactly_once (id INT); SELECT pg_sleep(0.1); INSERT INTO exactly_once VALUES (1)`)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := Run(context.Background(), db, Options{Dir: dir}); errs <- err }()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if count(t, db, `SELECT count(*) FROM exactly_once`) != 1 {
		t.Fatal("migration executed more than once")
	}
}

func TestTimeoutRollsBack(t *testing.T) {
	db, _ := fixtureDB(t)
	dir := t.TempDir()
	migrationFile(t, dir, "001_initial.up.sql", `SELECT 1`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	migrationFile(t, dir, "002_slow.up.sql", `CREATE TABLE timeout_probe (id INT); SELECT pg_sleep(3)`)
	if _, err := Run(context.Background(), db, Options{Dir: dir, MigrationTimeout: 100 * time.Millisecond}); err == nil {
		t.Fatal("migration timeout ignored")
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations`) != 1 {
		t.Fatal("timed out migration recorded")
	}
	if count(t, db, `SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename = 'timeout_probe'`) != 0 {
		t.Fatal("timed out DDL persisted")
	}
}

func TestLegacyAdoptionRequiresVerifiedManifest(t *testing.T) {
	db, _ := fixtureDB(t)
	dir := t.TempDir()
	name := "000015_store_ad_widgets.up.sql"
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	migrationFile(t, dir, name, string(body))
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()); INSERT INTO schema_migrations(version) VALUES ('000015_store_ad_widgets.up.sql')`); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err == nil {
		t.Fatal("legacy record silently trusted")
	}
	migrationFile(t, dir, name, string(body)+"\n-- drift")
	if _, err := Run(context.Background(), db, Options{Dir: dir, AdoptLegacy: true}); err == nil {
		t.Fatal("legacy drift silently adopted")
	}
	migrationFile(t, dir, name, string(body))
	if _, err := Run(context.Background(), db, Options{Dir: dir, AdoptLegacy: true}); err != nil {
		t.Fatal(err)
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations WHERE length(checksum) = 64`) != 1 {
		t.Fatal("verified checksum not recorded")
	}
}

func TestLegacyAliasAdoptionPreservesLedgerRows(t *testing.T) {
	db, _ := fixtureDB(t)
	dir := t.TempDir()
	name := "202610190003_supplier_approval.up.sql"
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	migrationFile(t, dir, name, string(body))
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
		version VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	); INSERT INTO schema_migrations(version) VALUES
		('202610190003'),
		('202610190003_supplier_approval.up.sql')`); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), db, Options{Dir: dir, AdoptLegacy: true}); err != nil {
		t.Fatal(err)
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations`) != 2 {
		t.Fatal("legacy alias row was not preserved")
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations WHERE length(checksum) = 64`) != 2 {
		t.Fatal("canonical and alias checksums were not adopted")
	}
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err != nil {
		t.Fatalf("strict verification after alias adoption: %v", err)
	}
}

func TestLegacyAliasCannotReplaceCanonicalHistory(t *testing.T) {
	db, _ := fixtureDB(t)
	dir := t.TempDir()
	name := "202610190003_supplier_approval.up.sql"
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	migrationFile(t, dir, name, string(body))
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
		version VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	); INSERT INTO schema_migrations(version) VALUES ('202610190003')`); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), db, Options{Dir: dir, AdoptLegacy: true}); err == nil {
		t.Fatal("legacy alias replaced its missing canonical history row")
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations`) != 1 {
		t.Fatal("failed alias adoption changed migration history")
	}
}

func TestBackdatedMigrationRefused(t *testing.T) {
	db, _ := fixtureDB(t)
	dir := t.TempDir()
	migrationFile(t, dir, "002_initial.up.sql", `SELECT 1`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	migrationFile(t, dir, "001_backdated.up.sql", `CREATE TABLE bad_order (id INT)`)
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err == nil {
		t.Fatal("out-of-order insert accepted")
	}
}

func TestHistoricalPreviousVersionUpgrade(t *testing.T) {
	db, _ := fixtureDB(t)
	// The harness already installed uuid-ossp in public during its bootstrap.
	// Keep this fixture's search_path isolated while exposing the extension's
	// UUID function under the same name historical SQL expects.
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()); CREATE FUNCTION uuid_generate_v4() RETURNS uuid LANGUAGE SQL AS 'SELECT public.uuid_generate_v4()'`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	entries, err := os.ReadDir(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	const last = "202611130005_store_variant_defaults.up.sql"
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") || entry.Name() >= last {
			continue
		}
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		migrationFile(t, dir, entry.Name(), string(body))
	}
	if _, err := Run(context.Background(), db, Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations`) != 178 {
		t.Fatal("historical previous-version fixture incomplete")
	}
	if _, err := db.Exec(`INSERT INTO users (id, mobile, full_name) VALUES ('f7ca8ec1-1ef5-4d68-a08c-7ada029fda67', '09999999178', 'Upgrade preservation fixture'); ALTER TABLE schema_migrations DROP COLUMN checksum`); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", last))
	if err != nil {
		t.Fatal(err)
	}
	migrationFile(t, dir, last, string(body))
	report, err := Run(context.Background(), db, Options{Dir: dir, AdoptLegacy: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Applied) != 1 || report.Applied[0].Version != last {
		t.Fatalf("incorrect historical upgrade: %+v", report)
	}
	if count(t, db, `SELECT count(*) FROM users WHERE id = 'f7ca8ec1-1ef5-4d68-a08c-7ada029fda67' AND full_name = 'Upgrade preservation fixture'`) != 1 {
		t.Fatal("historical upgrade changed business fixture")
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations WHERE length(checksum) = 64`) != 179 {
		t.Fatal("historical checksum adoption incomplete")
	}
}

func TestMigrationProcessHelper(t *testing.T) {
	if os.Getenv("MIGRATION_CRASH_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	db, _ := testdb.Open(t, os.Getenv("TEST_DATABASE_URL"))
	if _, err := Run(context.Background(), db, Options{Dir: os.Getenv("MIGRATION_CRASH_DIR"), MigrationTimeout: 3 * time.Second}); err != nil {
		t.Fatal(err)
	}
}

func TestProcessTerminationRollsBackAndUnlocks(t *testing.T) {
	db, dsn := fixtureDB(t)
	dir := t.TempDir()
	migrationFile(t, dir, "001_crash.up.sql", `CREATE TABLE crash_probe (id INT); SELECT pg_sleep(1.5) /* migration_crash_probe */; INSERT INTO crash_probe VALUES (1)`)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestMigrationProcessHelper$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TEST_DATABASE_URL=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TEST_DATABASE_URL="+dsn, "MIGRATION_CRASH_CHILD=1", "MIGRATION_CRASH_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		if count(t, db, `SELECT count(*) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND query LIKE '%migration_crash_probe%' AND wait_event = 'PgSleep'`) > 0 {
			started = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !started {
		t.Fatal("child did not enter migration")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	// The disconnected backend detects client death after the current statement;
	// the migration's server-side timeout bounds that rollback even after a crash.
	if _, err := Run(context.Background(), db, Options{Dir: dir, LockTimeout: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if count(t, db, `SELECT count(*) FROM crash_probe`) != 1 || count(t, db, `SELECT count(*) FROM schema_migrations`) != 1 {
		t.Fatal("crash left partial or duplicate migration")
	}
}
