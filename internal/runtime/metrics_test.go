package runtime

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kmpoltorak/routetus/internal/postgres"
)

func TestSyncMetricTypesAndValues(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://postgres@127.0.0.1:1/routetus_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	w := httptest.NewRecorder()
	writeSyncMetrics(w, Status{Applied: 5, Desired: 7, AgeSeconds: 2, LastConfirmed: time.Unix(123, 0)}, 11, 3, &postgres.Store{Pool: pool})
	text := w.Body.String()
	for _, name := range []string{"config_applied_generation", "config_desired_generation", "config_age_seconds", "config_last_confirmed_timestamp_seconds", "postgres_connections", "postgres_acquired_connections", "postgres_max_connections", "sync_success_total", "sync_errors_total"} {
		kind := "gauge"
		if strings.HasPrefix(name, "sync_") {
			kind = "counter"
		}
		if strings.Count(text, "# TYPE routetus_"+name+" "+kind+"\n") != 1 || !strings.Contains(text, "# HELP routetus_"+name+" ") {
			t.Fatal("invalid metadata", name, text)
		}
	}
	for _, line := range []string{"routetus_config_applied_generation 5\n", "routetus_config_desired_generation 7\n", "routetus_config_last_confirmed_timestamp_seconds 123\n", "routetus_sync_success_total 11\n", "routetus_sync_errors_total 3\n"} {
		if !strings.Contains(text, line) {
			t.Fatal("missing value", line, text)
		}
	}
}
