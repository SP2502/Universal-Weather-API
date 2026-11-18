package database

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"reflect"
	"sync"
	"time"
)

// lib/pq sends a separate cancel request when a context ends, but a silent
// server may never answer the original socket. Apply the operation deadline
// to that socket as well so database/sql can discard the broken connection.
type contextBoundConn struct {
	driver.Conn
	socket net.Conn
	mu     sync.Mutex
	leases map[*socketDeadlineLease]struct{}
}

type socketDeadlineLease struct {
	deadline time.Time
	canceled bool
}

func (c *contextBoundConn) setActiveDeadline() error {
	var deadline time.Time
	for lease := range c.leases {
		if lease.canceled {
			return c.socket.SetDeadline(time.Now())
		}
		if !lease.deadline.IsZero() && (deadline.IsZero() || lease.deadline.Before(deadline)) {
			deadline = lease.deadline
		}
	}
	return c.socket.SetDeadline(deadline)
}

func (c *contextBoundConn) arm(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.socket == nil {
		return nil, errors.New("postgres: connection socket unavailable")
	}
	deadline, _ := ctx.Deadline()
	lease := &socketDeadlineLease{deadline: deadline}
	c.mu.Lock()
	if c.leases == nil {
		c.leases = make(map[*socketDeadlineLease]struct{})
	}
	c.leases[lease] = struct{}{}
	if err := c.setActiveDeadline(); err != nil {
		delete(c.leases, lease)
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(finished)
		c.mu.Lock()
		lease.canceled = true
		_ = c.setActiveDeadline()
		c.mu.Unlock()
	})
	return func() {
		if !stop() {
			<-finished
		}
		c.mu.Lock()
		delete(c.leases, lease)
		_ = c.setActiveDeadline()
		c.mu.Unlock()
	}, nil
}

func (c *contextBoundConn) Ping(ctx context.Context) error {
	finish, err := c.arm(ctx)
	if err != nil {
		return err
	}
	defer finish()
	err = c.Conn.(driver.Pinger).Ping(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (c *contextBoundConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	finish, err := c.arm(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
	if ctx.Err() != nil {
		if rows != nil {
			_ = rows.Close()
		}
		finish()
		return nil, ctx.Err()
	}
	if err != nil {
		finish()
		return nil, err
	}
	return &contextBoundRows{Rows: rows, finish: finish}, nil
}

func (c *contextBoundConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	finish, err := c.arm(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	result, err := c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return result, err
}

func (c *contextBoundConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	finish, err := c.arm(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	stmt, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if ctx.Err() != nil {
		if stmt != nil {
			_ = stmt.Close()
		}
		return nil, ctx.Err()
	}
	return stmt, err
}

func (c *contextBoundConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	finish, err := c.arm(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if ctx.Err() != nil {
		if tx != nil {
			_ = tx.Rollback()
		}
		finish()
		return nil, ctx.Err()
	}
	if err != nil {
		finish()
		return nil, err
	}
	return &contextBoundTx{Tx: tx, finish: finish}, nil
}

func (c *contextBoundConn) ResetSession(ctx context.Context) error {
	return c.Conn.(driver.SessionResetter).ResetSession(ctx)
}

func (c *contextBoundConn) IsValid() bool {
	return c.Conn.(driver.Validator).IsValid()
}

type contextBoundRows struct {
	driver.Rows
	finish func()
	once   sync.Once
}

func (r *contextBoundRows) Close() error {
	err := r.Rows.Close()
	r.once.Do(r.finish)
	return err
}

func (r *contextBoundRows) HasNextResultSet() bool {
	return r.Rows.(driver.RowsNextResultSet).HasNextResultSet()
}

func (r *contextBoundRows) NextResultSet() error {
	return r.Rows.(driver.RowsNextResultSet).NextResultSet()
}

func (r *contextBoundRows) ColumnTypeScanType(index int) reflect.Type {
	return r.Rows.(driver.RowsColumnTypeScanType).ColumnTypeScanType(index)
}

func (r *contextBoundRows) ColumnTypeDatabaseTypeName(index int) string {
	return r.Rows.(driver.RowsColumnTypeDatabaseTypeName).ColumnTypeDatabaseTypeName(index)
}

func (r *contextBoundRows) ColumnTypeLength(index int) (int64, bool) {
	return r.Rows.(driver.RowsColumnTypeLength).ColumnTypeLength(index)
}

func (r *contextBoundRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	return r.Rows.(driver.RowsColumnTypePrecisionScale).ColumnTypePrecisionScale(index)
}

type contextBoundTx struct {
	driver.Tx
	finish func()
	once   sync.Once
}

func (t *contextBoundTx) Commit() error {
	defer t.once.Do(t.finish)
	return t.Tx.Commit()
}

func (t *contextBoundTx) Rollback() error {
	defer t.once.Do(t.finish)
	return t.Tx.Rollback()
}
