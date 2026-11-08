package gateway_test

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

type pipeListener struct {
	conn     net.Conn
	accepted atomic.Bool
	done     chan struct{}
	once     sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	if l.accepted.CompareAndSwap(false, true) {
		return l.conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return l.conn.LocalAddr() }

func TestDeadlineCoversFinalFlushAndRejectedBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "small buffered response") }))
	defer backend.Close()
	for _, request := range []string{"GET / HTTP/1.1\r\nHost: example.com\r\n\r\n", "POST /a%2Fb HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n"} {
		t.Run(request[:4], func(t *testing.T) {
			compiler := testutil.Compiler(t)
			compiler.Limits.RequestTimeout = "100ms"
			c := testutil.Config(backend.URL)
			c.Routes[0].RequestTimeout = config.D(100 * time.Millisecond)
			snapshot, err := compiler.Compile(c)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			serverConn, client := net.Pipe()
			defer client.Close()
			listener := &pipeListener{conn: serverConn, done: make(chan struct{})}
			closed := make(chan struct{})
			var once sync.Once
			server := &http.Server{Handler: snapshot, ReadHeaderTimeout: time.Second, ConnState: func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					once.Do(func() { close(closed) })
				}
			}}
			defer server.Close()
			go server.Serve(listener)
			_ = client.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err = io.WriteString(client, request); err != nil {
				t.Fatal(err)
			}
			// net.Pipe has no socket buffer: unread response/body I/O must hit its deadline.
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("request I/O remained active beyond its deadline")
			}
		})
	}
}
