package extensions_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/extensions"
	"github.com/kmpoltorak/routetus/internal/middleware"
)

func policy(t *testing.T, kind, options string) middleware.Middleware {
	t.Helper()
	r := middleware.NewRegistry()
	if err := extensions.Register(r); err != nil {
		t.Fatal(err)
	}
	m, err := r.Build(config.Handler{ID: "policy", Type: kind, Version: 1, Options: []byte(options)})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTokenAllowlist(t *testing.T) {
	t.Setenv("ROUTETUS_TEST_TOKEN_A", strings.Repeat("a", 32))
	t.Setenv("ROUTETUS_TEST_TOKEN_B", strings.Repeat("b", 32))
	handler := policy(t, "token_allowlist", `{"token_envs":["ROUTETUS_TEST_TOKEN_A","ROUTETUS_TEST_TOKEN_B"]}`)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("route token reached backend")
		}
		w.WriteHeader(204)
	}))
	for _, tc := range []struct {
		name   string
		values []string
		status int
	}{
		{"first token", []string{"Bearer " + strings.Repeat("a", 32)}, 204},
		{"second token", []string{"bearer " + strings.Repeat("b", 32)}, 204},
		{"missing", nil, 401},
		{"unknown", []string{"Bearer " + strings.Repeat("c", 32)}, 401},
		{"wrong scheme", []string{"Basic " + strings.Repeat("a", 32)}, 401},
		{"extra token", []string{"Bearer " + strings.Repeat("a", 32) + " extra"}, 401},
		{"duplicate", []string{"Bearer " + strings.Repeat("a", 32), "Bearer " + strings.Repeat("b", 32)}, 401},
		{"oversized", []string{"Bearer " + strings.Repeat("a", 4097)}, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?access_token="+strings.Repeat("a", 32), nil)
			for _, value := range tc.values {
				r.Header.Add("Authorization", value)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			if tc.status == 401 && w.Header().Get("WWW-Authenticate") != `Bearer realm="routetus"` {
				t.Fatal("missing challenge", w.Header())
			}
			if strings.Contains(w.Body.String(), strings.Repeat("a", 32)) {
				t.Fatal("response exposed token")
			}
		})
	}
}

func TestTokenAllowlistFailsClosed(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("a", 31), strings.Repeat("a", 4097), strings.Repeat("a", 32) + " bad"} {
		t.Setenv("ROUTETUS_TEST_INVALID_TOKEN", value)
		r := middleware.NewRegistry()
		if err := extensions.Register(r); err != nil {
			t.Fatal(err)
		}
		for _, disabled := range []bool{false, true} {
			_, err := r.Build(config.Handler{ID: "auth", Type: "token_allowlist", Version: 1, Enabled: config.B(!disabled), Options: []byte(`{"token_envs":["ROUTETUS_TEST_INVALID_TOKEN"]}`)})
			if err == nil {
				t.Fatalf("invalid secret accepted, disabled=%v", disabled)
			}
			if value != "" && strings.Contains(err.Error(), value) {
				t.Fatal("error exposes secret")
			}
		}
	}
	t.Setenv("ROUTETUS_TEST_TOKEN", strings.Repeat("a", 32))
	for _, raw := range []string{`{}`, `{"token_envs":null}`, `{"token_envs":[]}`, `{"token_envs":["not-valid!"]}`, `{"token_envs":["ROUTETUS_TEST_TOKEN","ROUTETUS_TEST_TOKEN"]}`, `{"token_envs":["ROUTETUS_TEST_TOKEN"],"unknown":true}`} {
		r := middleware.NewRegistry()
		if err := extensions.Register(r); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Build(config.Handler{ID: "auth", Type: "token_allowlist", Version: 1, Options: []byte(raw)}); err == nil {
			t.Fatalf("invalid options accepted: %s", raw)
		}
	}
}

func TestRateLimitBindingsAreIndependent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
		for range 2 {
			handler := policy(t, "rate_limit", `{"requests_per_second":1,"burst":1}`)(next)
			for _, want := range []int{204, 429} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
				if w.Code != want {
					t.Fatalf("binding status %d, want %d", w.Code, want)
				}
			}
		}
	})
}

func TestRateLimitBurstRefillAndParallelAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		handler := policy(t, "rate_limit", `{"requests_per_second":2,"burst":2}`)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
		request := func() *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			return w
		}
		var admitted atomic.Int32
		var group sync.WaitGroup
		for range 32 {
			group.Go(func() {
				w := request()
				if w.Code == 204 {
					admitted.Add(1)
				} else if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
					t.Errorf("rejection: %d %v", w.Code, w.Header())
				}
			})
		}
		group.Wait()
		if admitted.Load() != 2 {
			t.Fatalf("admitted %d, want 2", admitted.Load())
		}
		time.Sleep(500 * time.Millisecond)
		if w := request(); w.Code != 204 {
			t.Fatalf("refilled token rejected: %d", w.Code)
		}
		if w := request(); w.Code != 429 {
			t.Fatalf("refill admitted too many requests: %d", w.Code)
		}
		time.Sleep(time.Hour)
		for _, want := range []int{204, 204, 429} {
			if w := request(); w.Code != want {
				t.Fatalf("idle bucket: %d, want %d", w.Code, want)
			}
		}
	})
}

func TestConcurrencyPermitReleasedAfterPanic(t *testing.T) {
	limit := policy(t, "concurrency_limit", `{"limit":1}`)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected downstream panic")
			}
		}()
		limit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("test failure") })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	w := httptest.NewRecorder()
	limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 204 {
		t.Fatalf("panic leaked permit: %d", w.Code)
	}
}
