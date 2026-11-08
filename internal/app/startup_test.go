package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
)

func TestInvalidDSNFailsBeforeStartupTimeout(t *testing.T) {
	for _, tc := range []struct{ dsn, want string }{
		{"postgres://user:private-password@%zz", "invalid postgres DSN"},
		{"", "DSN environment reference is unavailable"},
	} {
		t.Setenv("ROUTEQTUS_TEST_STARTUP_DSN", tc.dsn)
		c := bootstrap.Defaults()
		c.Postgres.DSNEnv = "ROUTEQTUS_TEST_STARTUP_DSN"
		c.Sync.StartupTimeout = "200ms"
		err := Run(context.Background(), "serve", c, slog.New(slog.NewJSONHandler(io.Discard, nil)))
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "startup timeout") || strings.Contains(err.Error(), "private-password") {
			t.Fatalf("error=%v; want immediate safe configuration error %q", err, tc.want)
		}
	}
}
