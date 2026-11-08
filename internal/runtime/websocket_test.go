package runtime

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/postgres"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestShutdownClosesWebSocket(t *testing.T) {
	backendClosed := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		defer close(backendClosed)
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = rw.Flush()
		_, _ = io.Copy(io.Discard, conn)
	}))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	c := testutil.Config(backend.URL)
	c.Routes[0].RequestTimeout = config.D(0)
	snapshot, err := compiler.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	r := New(nil, compiler, bootstrap.Defaults().Sync, compiler.Logger)
	a := &active{snapshot: snapshot, state: postgres.State{RevisionID: 1, Generation: 1}}
	a.refs.Store(1)
	r.current = a
	r.confirm()
	defer r.Close()
	front := httptest.NewServer(r)
	defer front.Close()
	conn, err := net.DialTimeout("tcp", front.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatalf("%v %v", response, err)
	}
	r.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	r.Wait(ctx)
	r.Close()
	var b [1]byte
	if _, err = conn.Read(b[:]); err == nil {
		t.Fatal("hijacked connection remained open")
	}
	select {
	case <-backendClosed:
	case <-time.After(time.Second):
		t.Fatal("upstream websocket leaked")
	}
}
