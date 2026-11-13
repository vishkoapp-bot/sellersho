package testdb

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDisposableIdentity(t *testing.T) {
	for _, dsn := range []string{"", "postgres://localhost/drup_db", "dbname=sellersho_test_one", "postgres://localhost/sellersho_test_", "postgres://localhost/sellersho_test_one?host=production", "postgres://localhost/sellersho_test_one?dbname=drup_db", "postgres://localhost/sellersho_test_one?service=production"} {
		if _, err := Identity(dsn); err == nil {
			t.Errorf("accepted unsafe test connection")
		}
	}
	if got, err := Identity("postgres://localhost/sellersho_test_one?sslmode=disable&search_path=fixture,public"); err != nil || got != "sellersho_test_one" {
		t.Fatalf("identity = %q, %v", got, err)
	}
}

// A reachable application endpoint must see no connection, including when a
// legacy/application environment variable happens to be present.
func TestUnsafeTargetNeverConnects(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dsn := "postgres://" + listener.Addr().String() + "/drup_db?sslmode=disable"
	for _, mode := range []string{"explicit-unsafe", "no-opt-in"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGuardSubprocess$")
		cmd.Env = append(os.Environ(), "SELLERSHO_GUARD_CHILD="+mode, "DATABASE_URL="+dsn, "CAPABILITY_AUDIT_DATABASE_URL="+dsn)
		out, err := cmd.CombinedOutput()
		if mode == "explicit-unsafe" && (err == nil || !strings.Contains(string(out), "test database name")) {
			t.Fatalf("unsafe target was not rejected before connection: %s", out)
		}
		if mode == "no-opt-in" && err != nil {
			t.Fatalf("absent opt-in did not skip: %s", out)
		}
	}
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	if conn, err := listener.Accept(); err == nil {
		conn.Close()
		t.Fatal("guard connected to the application sentinel")
	}
}

func TestGuardSubprocess(t *testing.T) {
	switch os.Getenv("SELLERSHO_GUARD_CHILD") {
	case "explicit-unsafe":
		Open(t, os.Getenv("DATABASE_URL"))
	case "no-opt-in":
		Open(t, "")
	}
}

func TestEveryDatabaseTestUsesGuard(t *testing.T) {
	err := filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "/testdb/") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{"sql.Open(", "config.LoadConfig(", "repository.NewPostgresDB("} {
			if strings.Contains(string(b), forbidden) {
				t.Errorf("%s bypasses disposable database guard with %s", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
