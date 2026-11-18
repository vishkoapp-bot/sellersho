package worker

import (
	"bytes"
	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"log"
	"os"
	"strings"
	"testing"
)

func TestOperationsCycleEmitsCorrelatedSQLAndJob(t *testing.T) {
	guard, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	_ = guard
	db, e := observability.OpenPostgres(os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	w := NewBackgroundWorker(db)
	w.SetOrderRepository(repository.NewOrderRepository(db))
	w.RunOperationsCycle()
	s := buf.String()
	var id string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "component=worker operation=unpaid-order-expiry") {
			for _, field := range strings.Fields(line) {
				if strings.HasPrefix(field, "request_id=") {
					id = strings.TrimPrefix(field, "request_id=")
				}
			}
		}
	}
	if len(id) != 32 || !strings.Contains(s, "request_id="+id+" component=sql") {
		t.Fatal("missing same correlation through actual job and SQL")
	}
}
