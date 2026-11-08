package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestConcurrencyStalenessAndShutdown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release; w.WriteHeader(204) }))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	compiler.Limits.MaxInFlight = 1
	s, err := compiler.Compile(testutil.Config(backend.URL))
	if err != nil {
		t.Fatal(err)
	}
	r := New(nil, compiler, bootstrap.Defaults().Sync, compiler.Logger)
	a := &active{snapshot: s, state: postgres.State{RevisionID: 1, Generation: 1}}
	a.refs.Store(1)
	r.current = a
	r.confirm()
	defer r.Close()
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		done <- w.Code
	}()
	<-started
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 {
		t.Fatal("concurrency limit did not reject", w.Code)
	}
	r.Stop()
	if r.Ready() {
		t.Fatal("ready during shutdown")
	}
	close(release)
	if code := <-done; code != 204 {
		t.Fatal("active request did not drain", code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.Wait(ctx)
}
func TestStaleness(t *testing.T) {
	compiler := testutil.Compiler(t)
	c := bootstrap.Defaults().Sync
	c.MaxStaleness = "1ms"
	r := New(nil, compiler, c, compiler.Logger)
	if r.Ready() {
		t.Fatal("ready without snapshot")
	}
	old := time.Now().Add(-time.Second)
	r.confirmed.Store(&old)
	if r.Ready() {
		t.Fatal("stale state ready")
	}
}
