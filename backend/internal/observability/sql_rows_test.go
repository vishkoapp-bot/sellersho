package observability

import (
	"bytes"
	"context"
	"database/sql/driver"
	"io"
	"log"
	"strconv"
	"strings"
	"testing"
	"time"
)

type slowRows struct{ n int }

func (r *slowRows) Columns() []string { return []string{"value"} }
func (r *slowRows) Close() error      { return nil }
func (r *slowRows) Next(v []driver.Value) error {
	time.Sleep(25 * time.Millisecond)
	r.n++
	if r.n > 1 {
		return io.EOF
	}
	v[0] = 1
	return nil
}
func TestSQLRowsStreamTimingAndSingleFinalization(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	ctx := NewContext(context.Background())
	r := &sqlRows{Rows: &slowRows{}, ctx: ctx, started: time.Now()}
	if logs.Len() != 0 {
		t.Fatal("logged before consumption")
	}
	if e := r.Next(make([]driver.Value, 1)); e != nil {
		t.Fatal(e)
	}
	if logs.Len() != 0 {
		t.Fatal("logged before EOF")
	}
	if e := r.Next(make([]driver.Value, 1)); e != io.EOF {
		t.Fatal(e)
	}
	r.Close()
	s := logs.String()
	if strings.Count(s, "component=sql") != 1 || !strings.Contains(s, RequestID(ctx)) {
		t.Fatal("missing or duplicate finalization")
	}
	duration, err := strconv.ParseInt(strings.TrimSpace(strings.Split(s, "duration_ms=")[1]), 10, 64)
	if err != nil || duration < 50 {
		t.Fatal("missed streaming time")
	}
}
