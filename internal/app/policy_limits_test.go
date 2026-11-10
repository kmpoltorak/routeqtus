package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestBootstrapPolicyLimitsReachCompilerAndSchema(t *testing.T) {
	t.Cleanup(func() { config.SetLimits(config.DefaultLimits) })
	t.Setenv("ROUTETUS_TEST_ROUTE_TOKEN", strings.Repeat("a", 32))
	boot := bootstrap.Defaults()
	boot.Limits.MaxConcurrentRequests = 3
	boot.Limits.MaxRequestsPerSecond = 3
	boot.Limits.MaxBurst = 3
	boot.Limits.MaxRouteBodyBytes = 3
	boot.Limits.MaxTokenReferences = 1
	boot.Limits.MaxTokenBytes = 32
	boot.Limits.MaxHeaderValueBytes = 3
	compiler, err := Compiler(boot, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind, field string
		options     map[string]any
	}{
		{"concurrency_limit", "limit", map[string]any{"limit": 3}},
		{"rate_limit", "requests_per_second", map[string]any{"requests_per_second": 3, "burst": 1}},
		{"rate_limit", "burst", map[string]any{"requests_per_second": 1, "burst": 3}},
		{"body_limit", "max_bytes", map[string]any{"max_bytes": 3}},
	} {
		t.Run(tc.kind+"/"+tc.field, func(t *testing.T) {
			kind, ok := compiler.Registry.Get(tc.kind)
			if !ok {
				t.Fatal("missing type", tc.kind)
			}
			prop := kind.Schema["properties"].(map[string]any)[tc.field].(map[string]any)
			bounds, err := json.Marshal(prop)
			if err != nil {
				t.Fatal(err)
			}
			var numeric struct{ Minimum, Maximum int }
			if err := json.Unmarshal(bounds, &numeric); err != nil {
				t.Fatal(err)
			}
			if numeric.Minimum != 1 || numeric.Maximum != 3 {
				t.Fatalf("wrong schema: %s", bounds)
			}
			for _, n := range []int{0, 1, 3, 4} {
				tc.options[tc.field] = n
				raw, err := json.Marshal(tc.options)
				if err != nil {
					t.Fatal(err)
				}
				c := testutil.Config("http://example.com")
				c.Handlers = []config.Handler{{ID: "policy", Type: tc.kind, Version: 1, Options: raw}}
				c.GlobalHandlers = nil
				c.Routes[0].Handlers = []string{"policy"}
				s, err := compiler.Compile(c)
				if err == nil {
					s.Close()
				}
				if (err == nil) != (n >= 1 && n <= 3) {
					t.Fatalf("%s=%d error=%v", tc.field, n, err)
				}
			}
		})
	}
	for _, side := range []string{"request_set", "response_set"} {
		for _, value := range []string{"abc", "abcd", "éé"} {
			raw, err := json.Marshal(map[string]any{side: map[string]string{"X-Custom": value}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Registry.Build(config.Handler{ID: "custom", Type: "headers", Version: 1, Options: raw})
			if (err == nil) != (len(value) <= 3) {
				t.Fatalf("header byte cap ignored: side=%s length=%d error=%v", side, len(value), err)
			}
		}
	}

	kind, _ := compiler.Registry.Get("token_allowlist")
	refs := kind.Schema["properties"].(map[string]any)["token_envs"].(map[string]any)
	if refs["maxItems"] != 1 {
		t.Fatalf("token reference schema: %v", refs)
	}
	for _, raw := range []string{`{"token_envs":["ROUTETUS_TEST_ROUTE_TOKEN","OTHER"]}`, `{"token_envs":["ROUTETUS_TEST_ROUTE_TOKEN"]}`} {
		_, err := compiler.Registry.Build(config.Handler{ID: "auth", Type: "token_allowlist", Version: 1, Options: []byte(raw)})
		if (err == nil) != (!strings.Contains(raw, "OTHER")) {
			t.Fatalf("token cap: %v", err)
		}
	}
	t.Setenv("ROUTETUS_TEST_ROUTE_TOKEN", strings.Repeat("a", 33))
	if _, err := compiler.Registry.Build(config.Handler{ID: "auth", Type: "token_allowlist", Version: 1, Options: []byte(`{"token_envs":["ROUTETUS_TEST_ROUTE_TOKEN"]}`)}); err == nil {
		t.Fatal("token byte ceiling ignored")
	}
}

func TestServiceIDsUseBootstrapIdentifierLimit(t *testing.T) {
	t.Cleanup(func() { config.SetLimits(config.DefaultLimits) })
	boot := bootstrap.Defaults()
	boot.Limits.MaxIdentifierLength = 3
	compiler, err := Compiler(boot, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"route", "upstream"} {
		for _, value := range []string{"", "A-1", "Long"} {
			c := config.Config{SchemaVersion: 1, Upstreams: []config.Upstream{{ID: "u", Targets: []config.Target{{ID: "a", URL: "http://example.com"}}}}, Routes: []config.Route{{ID: "r", Path: "/", Upstream: "u"}}}
			if resource == "route" {
				c.Routes[0].ServiceID = value
			} else {
				c.Upstreams[0].ServiceID = value
			}
			s, err := compiler.Compile(c)
			if s != nil {
				s.Close()
			}
			if (err == nil) != (value != "Long") {
				t.Fatalf("%s service_id=%q: %v", resource, value, err)
			}
		}
	}
	for _, schema := range []map[string]any{config.Schema(config.Route{}), config.Schema(config.Upstream{})} {
		if schema["properties"].(map[string]any)["service_id"].(map[string]any)["maxLength"] != 3 {
			t.Fatal("service schema ignored bootstrap")
		}
	}
}
