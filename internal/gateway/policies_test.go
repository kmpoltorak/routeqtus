package gateway_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/extensions"
	"github.com/kmpoltorak/routetus/internal/middleware"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestRouteTokensNeverReachBackendOrLogs(t *testing.T) {
	allowed, rejected := strings.Repeat("allowed-token-", 4), strings.Repeat("rejected-token-", 4)
	t.Setenv("ROUTETUS_TEST_ROUTE_SECRET", allowed)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	registry := middleware.NewRegistry()
	if err := middleware.RegisterBuiltins(registry, logger); err != nil {
		t.Fatal(err)
	}
	if err := extensions.Register(registry); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/protected" && r.Header.Get("Authorization") != "" {
			t.Error("token forwarded")
		}
		w.WriteHeader(204)
	}))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	compiler.Logger, compiler.Registry = logger, registry
	c := testutil.Config(backend.URL)
	c.Handlers = append(c.Handlers, config.Handler{ID: "auth", Type: "token_allowlist", Version: 1, Options: []byte(`{"token_envs":["ROUTETUS_TEST_ROUTE_SECRET"]}`)})
	c.Routes = []config.Route{
		{ID: "protected", Path: "/protected", Upstream: "demo", Handlers: []string{"auth"}},
		{ID: "open", Path: "/open", Upstream: "demo"},
	}
	snapshot, err := compiler.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for _, tc := range []struct {
		path, token string
		status      int
	}{
		{"/protected", allowed, 204}, {"/protected", rejected, 401}, {"/protected", "", 401}, {"/open", "", 204},
	} {
		r := httptest.NewRequest("GET", tc.path+"?access_token="+rejected, nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		snapshot.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s status=%d, want=%d", tc.path, w.Code, tc.status)
		}
		for _, secret := range []string{allowed, rejected} {
			if strings.Contains(logs.String(), secret) || strings.Contains(w.Body.String(), secret) {
				t.Fatal("token exposed in logs or error response")
			}
		}
	}
	if strings.Count(logs.String(), `"msg":"request"`) != 4 {
		t.Fatal("missing access log evidence")
	}
}

func TestRouteBodyLimitPreservesProcessCeiling(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		w.WriteHeader(204)
	}))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	c.Handlers = append(c.Handlers,
		config.Handler{ID: "small", Type: "body_limit", Version: 1, Options: []byte(`{"max_bytes":3}`)},
		config.Handler{ID: "large", Type: "body_limit", Version: 1, Options: []byte(`{"max_bytes":64}`)},
		config.Handler{ID: "disabled", Type: "body_limit", Version: 1, Enabled: config.B(false), Options: []byte(`{"max_bytes":1}`)},
	)
	c.Routes = []config.Route{
		{ID: "small", Path: "/small", Upstream: "demo", Handlers: []string{"small"}},
		{ID: "large", Path: "/large", Upstream: "demo", Handlers: []string{"large"}},
		{ID: "open", Path: "/open", Upstream: "demo"},
		{ID: "disabled", Path: "/disabled", Upstream: "demo", Handlers: []string{"disabled"}},
	}
	compiler := testutil.Compiler(t)
	compiler.Limits.MaxBodyBytes = 8
	snapshot, err := compiler.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	frontend := httptest.NewServer(snapshot)
	defer frontend.Close()
	client := frontend.Client()
	client.Timeout = 3 * time.Second
	for _, chunked := range []bool{false, true} {
		for _, tc := range []struct {
			path, body string
			status     int
		}{
			{"/small", "abc", 204}, {"/small", "abcd", 413},
			{"/open", "abcd", 204}, {"/open", "123456789", 413},
			{"/large", "12345678", 204}, {"/large", "123456789", 413},
			{"/disabled", "abcd", 204},
		} {
			r, err := http.NewRequest("POST", frontend.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if chunked {
				r.ContentLength = -1
			}
			response, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if err != nil || response.StatusCode != tc.status {
				t.Fatalf("chunked=%v %s length=%d: status=%d error=%v, want %d", chunked, tc.path, len(tc.body), response.StatusCode, err, tc.status)
			}
		}
	}
}
