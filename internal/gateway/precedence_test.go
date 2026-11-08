package gateway_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestHostPriorityAndRoundRobin(t *testing.T) {
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "a") }))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "b") }))
	defer b.Close()
	c := testutil.Config(a.URL)
	c.Upstreams = append(c.Upstreams, config.Upstream{ID: "other", Targets: []config.Target{{ID: "b", URL: b.URL}}})
	c.Routes = []config.Route{{ID: "wildcard", Path: "/api", Prefix: true, Upstream: "demo", Handlers: []string{}}, {ID: "host", Host: "example.com", Path: "/api", Prefix: true, Upstream: "other", Handlers: []string{}}, {ID: "priority", Path: "/api/high", Priority: 1, Upstream: "demo", Handlers: []string{}}}
	s := compiled(t, c)
	for _, tc := range []struct{ host, path, body string }{{"example.com", "/api", "b"}, {"example.net", "/api", "a"}, {"example.com", "/api/high", "a"}} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != tc.body {
			t.Fatalf("%+v %d %s", tc, w.Code, w.Body)
		}
	}
	c = testutil.Config(a.URL)
	c.Upstreams[0].Targets = append(c.Upstreams[0].Targets, config.Target{ID: "b", URL: b.URL})
	s = compiled(t, c)
	for i, want := range []string{"a", "b", "a", "b"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Body.String() != want {
			t.Fatalf("request %d: %s", i, w.Body)
		}
	}
}
func TestDuplicateMatcherCannotBeHiddenByPriority(t *testing.T) {
	c := testutil.Config("http://example.com")
	other := c.Routes[0]
	other.ID = "shadow"
	other.Priority = 1
	c.Routes = append(c.Routes, other)
	if s, err := testutil.Compiler(t).Compile(c); err == nil {
		s.Close()
		t.Fatal("duplicate matcher accepted")
	}
}

func TestMethodSelectionFallsBackToBroaderRoute(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Host) }))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	c.Routes = []config.Route{
		{ID: "get-admin", Path: "/api/admin", Methods: []string{"GET"}, Priority: 10, HostHeader: "specific.example.com", Upstream: "demo"},
		{ID: "api-fallback", Path: "/api", Prefix: true, HostHeader: "fallback.example.com", Upstream: "demo"},
	}
	s := compiled(t, c)
	for method, want := range map[string]string{"GET": "specific.example.com", "POST": "fallback.example.com"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, "http://example.com/api/admin", nil))
		if w.Code != 200 || w.Body.String() != want {
			t.Fatalf("%s: status=%d backend=%q, want %q", method, w.Code, w.Body.String(), want)
		}
	}
}
