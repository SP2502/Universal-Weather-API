package database

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// This uses the genuine pq startup path against an accepted-but-silent TCP
// peer. It verifies cancellation, NOT PostgreSQL SQL/migration acceptance.
func TestPostgresHandshakeHonorsDeadlineAndCancellation(t *testing.T) {
	for _, mode := range []string{"deadline", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				c, e := listener.Accept()
				if e == nil {
					accepted <- c
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				c, e := (boundedConnector{dsn: "postgres://audit:fixture@" + listener.Addr().String() + "/audit?sslmode=disable"}).Connect(ctx)
				if c != nil {
					c.Close()
				}
				done <- e
			}()
			var peer net.Conn
			select {
			case peer = <-accepted:
				defer peer.Close()
			case <-time.After(time.Second):
				t.Fatal("connection not accepted")
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("wrong cancellation error:%v", err)
				}
			case <-time.After(time.Second):
				peer.Close()
				t.Fatal("startup ignored cancellation")
			}
			_ = peer.SetReadDeadline(time.Now().Add(time.Second))
			buf := make([]byte, 4096)
			for {
				_, err = peer.Read(buf)
				if err != nil {
					break
				}
			}
			if e, ok := err.(net.Error); ok && e.Timeout() {
				t.Fatal("cancel did not close startup socket")
			}
		})
	}
}
