package backend_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"termail/internal/backend"
	"termail/internal/testserver"
)

// stalledServer accepts connections and never says anything (a server that
// hangs, or a network that swallows packets after the handshake).
func stalledServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	return ln.Addr().String()
}

// Close must not wait for LOGOUT / IDLE DONE: with push running against the
// local server it returns at once.
func TestCloseWithActiveIdleIsImmediate(t *testing.T) {
	ts, err := testserver.Start("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	ts.AddUser("ahmet@test.local", "gizli")
	b := backend.NewIMAP(backend.Config{Incoming: srvOf(ts.IMAPAddr), Outgoing: srvOf(ts.SMTPAddr), Username: "ahmet@test.local", Password: "gizli"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	online := make(chan struct{}, 4)
	watchDone := make(chan struct{})
	go func() {
		b.Watch(ctx, "INBOX", func(e backend.Event) {
			if e.Kind == backend.EvOnline {
				online <- struct{}{}
			}
		})
		close(watchDone)
	}()
	select {
	case <-online:
	case <-time.After(10 * time.Second):
		t.Fatal("push did not start")
	}
	if _, err := b.List(ctx, "INBOX", 10); err != nil {
		t.Fatal(err)
	}
	if err := b.Prewarm(ctx); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cancel()
	b.Close()
	took := time.Since(start)
	t.Logf("exit with IDLE + reading + outgoing connection open: %v", took)
	if took > 200*time.Millisecond {
		t.Fatalf("Close took %v", took)
	}
	select {
	case <-watchDone:
	case <-time.After(time.Second):
		t.Fatal("push loop did not stop")
	}
}

// Against a stalled server, Close returns at once even while commands and
// push are stuck waiting for it, and the stuck calls fail soon after.
func TestCloseWithStalledServerIsImmediate(t *testing.T) {
	addr := stalledServer(t)
	b := backend.NewIMAP(backend.Config{Incoming: srvOf(addr), Outgoing: srvOf(addr), Username: "a@b.c", Password: "x"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listDone := make(chan error, 1)
	go func() {
		_, err := b.List(ctx, "INBOX", 10)
		listDone <- err
	}()
	go b.Watch(ctx, "INBOX", func(backend.Event) {})
	time.Sleep(300 * time.Millisecond) // both are now stuck waiting for a greeting
	start := time.Now()
	cancel()
	b.Close()
	took := time.Since(start)
	t.Logf("exit with a stalled server: %v", took)
	if took > 200*time.Millisecond {
		t.Fatalf("Close took %v", took)
	}
	select {
	case <-listDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stuck command did not end after Close")
	}
	if _, err := b.List(context.Background(), "INBOX", 1); err == nil {
		t.Fatal("a closed backend must not reconnect")
	}
}
