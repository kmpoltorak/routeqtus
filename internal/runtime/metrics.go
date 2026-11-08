package runtime

import (
	"fmt"
	"net/http"

	"github.com/kmpoltorak/routetus/internal/postgres"
)

func writeSyncMetrics(w http.ResponseWriter, s Status, success, failures uint64, store *postgres.Store) {
	fmt.Fprint(w, "# HELP routetus_config_applied_generation Generation compiled by this instance.\n# TYPE routetus_config_applied_generation gauge\n"+
		"# HELP routetus_config_desired_generation Last observed committed generation.\n# TYPE routetus_config_desired_generation gauge\n"+
		"# HELP routetus_config_age_seconds Seconds since confirmation of the applied generation.\n# TYPE routetus_config_age_seconds gauge\n"+
		"# HELP routetus_config_last_confirmed_timestamp_seconds Unix timestamp of the last configuration confirmation.\n# TYPE routetus_config_last_confirmed_timestamp_seconds gauge\n"+
		"# HELP routetus_sync_success_total Successful configuration synchronizations.\n# TYPE routetus_sync_success_total counter\n"+
		"# HELP routetus_sync_errors_total Failed configuration synchronizations.\n# TYPE routetus_sync_errors_total counter\n"+
		"# HELP routetus_postgres_connections Current pooled PostgreSQL connections.\n# TYPE routetus_postgres_connections gauge\n"+
		"# HELP routetus_postgres_acquired_connections PostgreSQL connections currently acquired from the pool.\n# TYPE routetus_postgres_acquired_connections gauge\n"+
		"# HELP routetus_postgres_max_connections Maximum pooled PostgreSQL connections.\n# TYPE routetus_postgres_max_connections gauge\n")
	fmt.Fprintf(w, "routetus_config_applied_generation %d\nroutetus_config_desired_generation %d\nroutetus_config_age_seconds %g\nroutetus_config_last_confirmed_timestamp_seconds %g\nroutetus_sync_success_total %d\nroutetus_sync_errors_total %d\n", s.Applied, s.Desired, s.AgeSeconds, float64(s.LastConfirmed.UnixNano())/1e9, success, failures)
	p := store.Pool.Stat()
	fmt.Fprintf(w, "routetus_postgres_connections %d\nroutetus_postgres_acquired_connections %d\nroutetus_postgres_max_connections %d\n", p.TotalConns(), p.AcquiredConns(), p.MaxConns())
}
