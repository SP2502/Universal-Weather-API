package database

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/lib/pq"
	"net"
	"sync"
	"time"
)

const postgresConnectTimeout = 3 * time.Second

type boundedConnector struct{ dsn string }

func (c boundedConnector) Driver() driver.Driver { return &pq.Driver{} }
func (c boundedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, postgresConnectTimeout)
	defer cancel()
	connector, err := pq.NewConnector(c.dsn)
	if err != nil {
		return nil, errors.New("invalid postgres connection configuration")
	}
	dial := &handshakeDialer{ctx: ctx}
	connector.Dialer(dial)
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(done); dial.close() })
	conn, err := connector.Connect(ctx)
	if !stop() {
		<-done
	}
	if ctx.Err() != nil {
		dial.close()
		return nil, ctx.Err()
	}
	if err != nil {
		dial.close()
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return nil, context.DeadlineExceeded
		}
		return nil, err
	}
	if err := dial.clearDeadline(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &contextBoundConn{Conn: conn, socket: dial.socket()}, nil
}

type handshakeDialer struct {
	ctx  context.Context
	mu   sync.Mutex
	conn net.Conn
}

func (d *handshakeDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(d.ctx, network, address)
}
func (d *handshakeDialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(d.ctx, timeout)
	defer cancel()
	return d.DialContext(ctx, network, address)
}
func (d *handshakeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	c, err := (&net.Dialer{Timeout: postgresConnectTimeout}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx.Err() != nil {
		c.Close()
		return nil, d.ctx.Err()
	}
	if deadline, ok := d.ctx.Deadline(); ok {
		if err = c.SetDeadline(deadline); err != nil {
			c.Close()
			return nil, err
		}
	}
	d.conn = c
	return c, nil
}
func (d *handshakeDialer) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != nil {
		_ = d.conn.Close()
	}
}
func (d *handshakeDialer) clearDeadline() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != nil {
		return d.conn.SetDeadline(time.Time{})
	}
	return nil
}

func (d *handshakeDialer) socket() net.Conn {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conn
}
