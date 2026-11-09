package gateway_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestHandlerBindingConcurrencyScope(t *testing.T) {
	for _, tc := range []struct {
		name          string
		route, global bool
		otherStatus   int
	}{
		{"route bindings are independent", true, false, 204},
		{"global binding shares capacity", false, true, 503},
		{"global and route bindings are independent", true, true, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var enterOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/first" {
					enterOnce.Do(func() { close(entered) })
					<-release
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer backend.Close()
			defer unblock()
			compiler := testutil.Compiler(t)
			if err := compiler.Registry.Register(middleware.Type{
				Name: "test_limit", Version: 1, Schema: config.Schema(middleware.Empty{}),
				Factory: func(raw json.RawMessage) (middleware.Middleware, error) {
					var options middleware.Empty
					if err := config.DecodeTyped(raw, &options); err != nil {
						return nil, err
					}
					slots := make(chan struct{}, 1)
					return func(next http.Handler) http.Handler {
						return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							select {
							case slots <- struct{}{}:
								defer func() { <-slots }()
								next.ServeHTTP(w, r)
							default:
								http.Error(w, "request concurrency limit reached", http.StatusServiceUnavailable)
							}
						})
					}, nil
				},
			}); err != nil {
				t.Fatal(err)
			}
			c := testutil.Config(backend.URL)
			c.Handlers = append(c.Handlers, config.Handler{ID: "limit", Type: "test_limit", Version: 1, Options: []byte(`{}`)})
			c.Routes = []config.Route{
				{ID: "first", Path: "/first", Upstream: "demo"},
				{ID: "second", Path: "/second", Upstream: "demo"},
			}
			if tc.route {
				for i := range c.Routes {
					c.Routes[i].Handlers = []string{"limit"}
				}
			}
			if tc.global {
				c.GlobalHandlers = append(c.GlobalHandlers, "limit")
			}
			snapshot, err := compiler.Compile(c)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			frontend := httptest.NewServer(snapshot)
			defer frontend.Close()
			defer unblock()
			client := frontend.Client()
			client.Timeout = 5 * time.Second
			type result struct {
				status int
				err    error
			}
			request := func(path string) result {
				response, err := client.Get(frontend.URL + path)
				if err != nil {
					return result{err: err}
				}
				_, copyErr := io.Copy(io.Discard, response.Body)
				response.Body.Close()
				return result{status: response.StatusCode, err: copyErr}
			}
			first := make(chan result, 1)
			go func() { first <- request("/first") }()
			select {
			case <-entered:
			case got := <-first:
				t.Fatalf("first request failed before entering backend: %+v", got)
			case <-time.After(5 * time.Second):
				t.Fatal("first request did not reach backend")
			}
			for _, check := range []struct {
				path   string
				status int
			}{{"/first", 503}, {"/second", tc.otherStatus}} {
				got := request(check.path)
				if got.err != nil || got.status != check.status {
					t.Fatalf("%s: got %+v, want %d", check.path, got, check.status)
				}
			}
			unblock()
			if got := <-first; got.err != nil || got.status != 204 {
				t.Fatalf("first request: %+v", got)
			}
			if got := request("/first"); got.err != nil || got.status != 204 {
				t.Fatalf("permit was not released: %+v", got)
			}
		})
	}
}

func TestAccessLogSamplingIsIndependentPerRoute(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	compiler := testutil.Compiler(t)
	compiler.Registry = middleware.NewRegistry()
	if err := middleware.RegisterBuiltins(compiler.Registry, logger); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	c.Handlers = []config.Handler{{ID: "sampled-log", Type: "access_log", Version: 1, Options: []byte(`{"sample_every":2}`)}}
	c.GlobalHandlers = nil
	c.Routes = []config.Route{
		{ID: "first", Path: "/first", Upstream: "demo", Handlers: []string{"sampled-log"}},
		{ID: "second", Path: "/second", Upstream: "demo", Handlers: []string{"sampled-log"}},
	}
	snapshot, err := compiler.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for _, path := range []string{"/first", "/second", "/first", "/second"} {
		response := httptest.NewRecorder()
		snapshot.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 204 {
			t.Fatal(response.Code)
		}
	}
	for _, id := range []string{"first", "second"} {
		if got := strings.Count(logs.String(), `"route_id":"`+id+`"`); got != 1 {
			t.Fatalf("route %s: got %d sampled logs, want 1: %s", id, got, logs.String())
		}
	}
}

func TestUnboundAndDisabledHandlersRemainValidated(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		c := testutil.Config("http://example.com")
		h := config.Handler{ID: "invalid-unused", Type: "request_id", Version: 1, Options: []byte(`{"unknown":true}`)}
		if disabled {
			h.Enabled = config.B(false)
			c.Routes[0].Handlers = append(c.Routes[0].Handlers, h.ID)
		}
		c.Handlers = append(c.Handlers, h)
		snapshot, err := testutil.Compiler(t).Compile(c)
		if err == nil {
			snapshot.Close()
			t.Fatal("invalid handler options accepted")
		}
		fields := config.FieldErrors(err)
		if len(fields) != 1 || fields[0].HandlerID != h.ID {
			t.Fatalf("handler context lost: %v", err)
		}
	}
}

func TestHandlerBindingBuildFailureHasContext(t *testing.T) {
	for _, global := range []bool{false, true} {
		compiler := testutil.Compiler(t)
		builds := 0
		if err := compiler.Registry.Register(middleware.Type{
			Name: "failing_binding", Version: 1, Schema: config.Schema(middleware.Empty{}),
			Factory: func(json.RawMessage) (middleware.Middleware, error) {
				builds++
				if builds > 1 {
					return nil, fmt.Errorf("binding initialization failed")
				}
				return func(next http.Handler) http.Handler { return next }, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
		c := testutil.Config("http://example.com")
		c.Handlers = []config.Handler{{ID: "failed", Type: "failing_binding", Version: 1, Options: []byte(`{}`)}}
		c.GlobalHandlers = nil
		c.Routes[0].Handlers = []string{"failed"}
		wantPath, wantRoute := "/routes/0/handlers/0", "demo"
		if global {
			c.GlobalHandlers = []string{"failed"}
			c.Routes[0].Handlers = nil
			wantPath, wantRoute = "/global_handlers/0", ""
		}
		snapshot, err := compiler.Compile(c)
		if err == nil {
			snapshot.Close()
			t.Fatal("binding initialization failure accepted")
		}
		fields := config.FieldErrors(err)
		if len(fields) != 1 || fields[0].Path != wantPath || fields[0].HandlerID != "failed" || fields[0].RouteID != wantRoute || !strings.Contains(fields[0].Message, "binding initialization failed") {
			t.Fatalf("binding error context: %v", err)
		}
	}
}
