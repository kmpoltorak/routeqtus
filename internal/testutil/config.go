// Package testutil provides test configuration, never a runtime fallback source.
package testutil

import (
	"io"
	"log/slog"
	"testing"

	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/extensions"
	"github.com/kmpoltorak/routeqtus/internal/gateway"
	"github.com/kmpoltorak/routeqtus/internal/middleware"
)

func Compiler(t testing.TB) *gateway.Compiler {
	t.Helper()
	r := middleware.NewRegistry()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if err := middleware.RegisterBuiltins(r, logger); err != nil {
		t.Fatal(err)
	}
	if err := extensions.Register(r); err != nil {
		t.Fatal(err)
	}
	return &gateway.Compiler{Registry: r, Logger: logger, Limits: bootstrap.Defaults().Server, Metrics: true}
}
func Config(target string) config.Config {
	return config.Config{SchemaVersion: 1, Upstreams: []config.Upstream{{ID: "demo", Targets: []config.Target{{ID: "a", URL: target}}}}, Handlers: []config.Handler{{ID: "id", Type: "request_id", Version: 1, Options: []byte(`{}`)}, {ID: "recover", Type: "recover", Version: 1, Options: []byte(`{}`)}, {ID: "log", Type: "access_log", Version: 1, Options: []byte(`{}`)}, {ID: "headers", Type: "headers", Version: 1, Options: []byte(`{"request_set":{"X-Trusted":"gateway"},"response_set":{"X-Gateway":"routeqtus"}}`)}, {ID: "powered", Type: "powered_by", Version: 1, Options: []byte(`{"value":"Routeqtus"}`)}}, GlobalHandlers: []string{"id", "recover", "log", "headers"}, Routes: []config.Route{{ID: "demo", Path: "/", Prefix: true, Upstream: "demo", Handlers: []string{"powered"}}}}
}
