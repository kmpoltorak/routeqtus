package runtime

import (
	"fmt"
	"net/http"

	"github.com/kmpoltorak/routeqtus/internal/postgres"
)

func writeSyncMetrics(w http.ResponseWriter, s Status, success, failures uint64, store *postgres.Store) {
	fmt.Fprintf(w, "routeqtus_config_applied_generation %d\nrouteqtus_config_desired_generation %d\nrouteqtus_config_age_seconds %g\nrouteqtus_config_last_confirmed_timestamp_seconds %g\nrouteqtus_sync_success_total %d\nrouteqtus_sync_errors_total %d\n", s.Applied, s.Desired, s.AgeSeconds, float64(s.LastConfirmed.UnixNano())/1e9, success, failures)
	p := store.Pool.Stat()
	fmt.Fprintf(w, "routeqtus_postgres_connections %d\nrouteqtus_postgres_acquired_connections %d\nrouteqtus_postgres_max_connections %d\n", p.TotalConns(), p.AcquiredConns(), p.MaxConns())
}
