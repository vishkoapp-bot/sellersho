package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
)

func TestQUAL01WarehouseGETOnly(t *testing.T) {
	for _, tc := range []struct {
		path string
		kind string
		want string
	}{
		{"/api/v1/warehouse/orders/ready-to-pack", "pack", `{"orders":[],"success":true}`},
		{"/api/v1/warehouse/orders/ready-to-ship", "ship", `{"orders":[],"success":true}`},
		// PERF-05 added paging keys to the existing returns array. Assert
		// the complete empty-page contract as well as the method guard.
		{"/api/v1/warehouses/returns", "", `{"hasMore":false,"limit":50,"nextCursor":"","returns":[]}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
				t.Run(method, func(t *testing.T) {
					queries := 0
					db := sql.OpenDB(qual01WarehouseEmptyConnector{queries: &queries})
					defer db.Close()
					req := httptest.NewRequest(method, tc.path, nil)
					res := httptest.NewRecorder()
					if tc.kind == "" {
						handleWarehousesReturns(res, req, req.Context(), repository.NewOrderRepository(db))
					} else {
						handleT01WarehouseOrdersReady(res, req, req.Context(), repository.NewWarehouseRepository(db), tc.kind)
					}
					if method != http.MethodGet {
						if res.Code != http.StatusMethodNotAllowed || queries != 0 {
							t.Fatalf("status=%d queries=%d, want 405 with no data access", res.Code, queries)
						}
						return
					}
					if res.Code != http.StatusOK || queries != 1 || strings.TrimSpace(res.Body.String()) != tc.want {
						t.Fatalf("status=%d queries=%d body=%q, want 200, one query and %s", res.Code, queries, res.Body.String(), tc.want)
					}
				})
			}
		})
	}
}

// An in-process SQL stub exercises the unchanged successful empty-queue path
// without connecting to a database or providing real warehouse data.
type qual01WarehouseEmptyConnector struct {
	queries *int
}

func (c qual01WarehouseEmptyConnector) Connect(context.Context) (driver.Conn, error) {
	return qual01WarehouseEmptyConn{queries: c.queries}, nil
}

func (qual01WarehouseEmptyConnector) Driver() driver.Driver { return nil }

type qual01WarehouseEmptyConn struct {
	driver.Conn // only QueryContext and Close are used
	queries     *int
}

func (c qual01WarehouseEmptyConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	*c.queries = *c.queries + 1
	return qual01WarehouseEmptyRows{}, nil
}

func (qual01WarehouseEmptyConn) Close() error { return nil }

type qual01WarehouseEmptyRows struct{}

func (qual01WarehouseEmptyRows) Columns() []string         { return nil }
func (qual01WarehouseEmptyRows) Close() error              { return nil }
func (qual01WarehouseEmptyRows) Next([]driver.Value) error { return io.EOF }
