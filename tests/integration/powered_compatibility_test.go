//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/app"
	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestServeStartsWithLegacyPoweredHTAB(t *testing.T) {
	ctx := context.Background()
	s := database(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	c.Handlers[4].Options = []byte(`{"value":"legacy\tvalue"}`)
	// Storage bypasses the current compiler to model a revision accepted by v0.1.0.
	if _, err := s.Publish(ctx, c, 0, postgres.Metadata{Principal: "legacy-test"}); err != nil {
		t.Fatal(err)
	}
	boot := bootstrap.Defaults()
	boot.Postgres.DSNEnv = "ROUTEQTUS_INTEGRATION_DSN"
	boot.Server.Listen, boot.Admin.Listen = freeAddress(t), freeAddress(t)
	boot.Sync.Notifications = false
	boot.Sync.PollInterval = "20ms"
	boot.Sync.StartupTimeout = "500ms"
	runctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var runErr error
	go func() { defer close(done); runErr = app.Run(runctx, "serve", boot, testutil.Compiler(t).Logger) }()
	defer func() {
		cancel()
		<-done
		if runErr != nil {
			t.Error(runErr)
		}
	}()
	client := &http.Client{Timeout: time.Second}
	eventually(t, 2*time.Second, func() bool {
		select {
		case <-done:
			t.Fatalf("serve rejected legacy revision: %v", runErr)
		default:
		}
		res, err := client.Get("http://" + boot.Admin.Listen + "/readyz")
		if err != nil {
			return false
		}
		res.Body.Close()
		return res.StatusCode == 200
	})
	res, err := client.Get("http://" + boot.Server.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 204 || res.Header.Get("X-Powered-By") != "legacy\tvalue" {
		t.Fatal("legacy header changed", res.StatusCode, res.Header)
	}
}
