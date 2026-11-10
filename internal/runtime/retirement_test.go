package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/postgres"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestOldSnapshotLivesUntilLastRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			close(started)
			<-release
		}
		w.WriteHeader(204)
	}))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	c := testutil.Config(backend.URL)
	oldSnapshot, err := compiler.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	old := &active{snapshot: oldSnapshot, state: postgres.State{RevisionID: 1, Generation: 1}}
	old.refs.Store(1)
	r := New(nil, compiler, bootstrap.Defaults().Sync, compiler.Logger)
	r.current = old
	r.confirm()
	defer r.Close()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/hold", nil))
		done <- w
	}()
	<-started
	c.Handlers[len(c.Handlers)-1].Options = []byte(`{"response_set":{"X-Powered-By":"new"}}`)
	c.Routes[0].Metrics = config.B(false)
	nextSnapshot, err := compiler.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	next := &active{snapshot: nextSnapshot, state: postgres.State{RevisionID: 2, Generation: 2}}
	next.refs.Store(1)
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
	old.release()
	if old.refs.Load() != 1 {
		t.Fatal("old snapshot retired during active request")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Header().Get("X-Powered-By") != "new" {
		t.Fatal("new request used old snapshot")
	}
	close(release)
	w = <-done
	if w.Header().Get("X-Powered-By") != "Routetus" || old.refs.Load() != 0 {
		t.Fatal("old request changed snapshots or resources were not retired")
	}
}
