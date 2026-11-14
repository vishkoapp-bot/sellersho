package observability

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"github.com/lib/pq"
	"io"
	"reflect"
	"sync"
	"time"
)

// OpenPostgres wraps the original lib/pq connector, preserving database/sql
// cancellation, transactions, prepared statements and pool behavior. Telemetry
// observes the duration/outcome only and never sees SQL text or parameter values.
func OpenPostgres(dsn string) (*sql.DB, error) {
	c, e := pq.NewConnector(dsn)
	if e != nil {
		return nil, e
	}
	return sql.OpenDB(sqlConnector{c}), nil
}

type sqlConnector struct{ driver.Connector }

func (c sqlConnector) Connect(ctx context.Context) (driver.Conn, error) {
	v, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &sqlConn{Conn: v}, nil
}

type sqlConn struct{ driver.Conn }

func (c *sqlConn) Prepare(query string) (driver.Stmt, error) {
	s, e := c.Conn.Prepare(query)
	if e != nil {
		return nil, e
	}
	return &sqlStmt{Stmt: s}, nil
}
func (c *sqlConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	if v, ok := c.Conn.(driver.ConnPrepareContext); ok {
		s, e := v.PrepareContext(ctx, q)
		if e != nil {
			return nil, e
		}
		return &sqlStmt{Stmt: s}, nil
	}
	return c.Prepare(q)
}
func (c *sqlConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if v, ok := c.Conn.(driver.ConnBeginTx); ok {
		return v.BeginTx(ctx, opts)
	}
	return nil, driver.ErrSkip
}
func (c *sqlConn) Ping(ctx context.Context) error {
	if v, ok := c.Conn.(driver.Pinger); ok {
		return v.Ping(ctx)
	}
	return nil
}
func (c *sqlConn) ResetSession(ctx context.Context) error {
	if v, ok := c.Conn.(driver.SessionResetter); ok {
		return v.ResetSession(ctx)
	}
	return nil
}
func (c *sqlConn) IsValid() bool {
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}
func (c *sqlConn) CheckNamedValue(n *driver.NamedValue) error {
	if v, ok := c.Conn.(driver.NamedValueChecker); ok {
		return v.CheckNamedValue(n)
	}
	return driver.ErrSkip
}
func (c *sqlConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (result driver.Result, err error) {
	v, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	defer func() { LogOperation(ctx, "sql", "query", outcome(ctx, err), time.Since(start)) }()
	return v.ExecContext(ctx, q, args)
}
func (c *sqlConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (rows driver.Rows, err error) {
	v, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	rows, err = v.QueryContext(ctx, q, args)
	if err != nil {
		LogOperation(ctx, "sql", "query", outcome(ctx, err), time.Since(start))
		return nil, err
	}
	return &sqlRows{Rows: rows, ctx: ctx, started: start}, nil
}

type sqlStmt struct{ driver.Stmt }

func (s *sqlStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (res driver.Result, err error) {
	start := time.Now()
	defer func() { LogOperation(ctx, "sql", "query", outcome(ctx, err), time.Since(start)) }()
	if v, ok := s.Stmt.(driver.StmtExecContext); ok {
		return v.ExecContext(ctx, args)
	}
	values := make([]driver.Value, len(args))
	for i, a := range args {
		values[i] = a.Value
	}
	return s.Stmt.Exec(values)
}
func (s *sqlStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (rows driver.Rows, err error) {
	start := time.Now()
	if v, ok := s.Stmt.(driver.StmtQueryContext); ok {
		rows, err = v.QueryContext(ctx, args)
	} else {
		values := make([]driver.Value, len(args))
		for i, a := range args {
			values[i] = a.Value
		}
		rows, err = s.Stmt.Query(values)
	}
	if err != nil {
		LogOperation(ctx, "sql", "query", outcome(ctx, err), time.Since(start))
		return nil, err
	}
	return &sqlRows{Rows: rows, ctx: ctx, started: start}, nil
}

type sqlRows struct {
	driver.Rows
	ctx     context.Context
	started time.Time
	once    sync.Once
}

func (r *sqlRows) finish(err error) {
	r.once.Do(func() {
		if err == io.EOF {
			err = nil
		}
		LogOperation(r.ctx, "sql", "query", outcome(r.ctx, err), time.Since(r.started))
	})
}
func (r *sqlRows) Close() error { err := r.Rows.Close(); r.finish(err); return err }
func (r *sqlRows) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	if err != nil {
		if err != io.EOF || !r.HasNextResultSet() {
			r.finish(err)
		}
	}
	return err
}
func (r *sqlRows) HasNextResultSet() bool {
	v, ok := r.Rows.(driver.RowsNextResultSet)
	return ok && v.HasNextResultSet()
}
func (r *sqlRows) NextResultSet() error {
	if v, ok := r.Rows.(driver.RowsNextResultSet); ok {
		err := v.NextResultSet()
		if err != nil {
			r.finish(err)
		}
		return err
	}
	r.finish(nil)
	return io.EOF
}
func (r *sqlRows) ColumnTypeDatabaseTypeName(i int) string {
	if v, ok := r.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return v.ColumnTypeDatabaseTypeName(i)
	}
	return ""
}
func (r *sqlRows) ColumnTypeLength(i int) (int64, bool) {
	if v, ok := r.Rows.(driver.RowsColumnTypeLength); ok {
		return v.ColumnTypeLength(i)
	}
	return 0, false
}
func (r *sqlRows) ColumnTypeNullable(i int) (bool, bool) {
	if v, ok := r.Rows.(driver.RowsColumnTypeNullable); ok {
		return v.ColumnTypeNullable(i)
	}
	return false, false
}
func (r *sqlRows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if v, ok := r.Rows.(driver.RowsColumnTypePrecisionScale); ok {
		return v.ColumnTypePrecisionScale(i)
	}
	return 0, 0, false
}
func (r *sqlRows) ColumnTypeScanType(i int) reflect.Type {
	if v, ok := r.Rows.(driver.RowsColumnTypeScanType); ok {
		return v.ColumnTypeScanType(i)
	}
	return reflect.TypeOf(new(any)).Elem()
}
