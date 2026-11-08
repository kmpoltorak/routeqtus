package runtime

import (
	"fmt"
	"net/http"

	"github.com/kmpoltorak/routeqtus/internal/postgres"
)

func writeSyncMetrics(w http.ResponseWriter, s Status, success, failures uint64, store *postgres.Store) {
	fmt.Fprint(w, "# HELP routeqtus_config_applied_generation Generation compiled by this instance.\n# TYPE routeqtus_config_applied_generation gauge\n"+
		"# HELP routeqtus_config_desired_generation Last observed committed generation.\n# TYPE routeqtus_config_desired_generation gauge\n"+
		"# HELP routeqtus_config_age_seconds Seconds since confirmation of the applied generation.\n# TYPE routeqtus_config_age_seconds gauge\n"+
		"# HELP routeqtus_config_last_confirmed_timestamp_seconds Unix timestamp of the last configuration confirmation.\n# TYPE routeqtus_config_last_confirmed_timestamp_seconds gauge\n"+
		"# HELP routeqtus_sync_success_total Successful configuration synchronizations.\n# TYPE routeqtus_sync_success_total counter\n"+
		"# HELP routeqtus_sync_errors_total Failed configuration synchronizations.\n# TYPE routeqtus_sync_errors_total counter\n"+
		"# HELP routeqtus_postgres_connections Current pooled PostgreSQL connections.\n# TYPE routeqtus_postgres_connections gauge\n"+
		"# HELP routeqtus_postgres_acquired_connections PostgreSQL connections currently acquired from the pool.\n# TYPE routeqtus_postgres_acquired_connections gauge\n"+
		"# HELP routeqtus_postgres_max_connections Maximum pooled PostgreSQL connections.\n# TYPE routeqtus_postgres_max_connections gauge\n")
	fmt.Fprintf(w, "routeqtus_config_applied_generation %d\nrouteqtus_config_desired_generation %d\nrouteqtus_config_age_seconds %g\nrouteqtus_config_last_confirmed_timestamp_seconds %g\nrouteqtus_sync_success_total %d\nrouteqtus_sync_errors_total %d\n", s.Applied, s.Desired, s.AgeSeconds, float64(s.LastConfirmed.UnixNano())/1e9, success, failures)
	p := store.Pool.Stat()
	fmt.Fprintf(w, "routeqtus_postgres_connections %d\nrouteqtus_postgres_acquired_connections %d\nrouteqtus_postgres_max_connections %d\n", p.TotalConns(), p.AcquiredConns(), p.MaxConns())
}
