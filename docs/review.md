# Final ponytail review

What this change does: Routeqtus separates request serving from authenticated
configuration management. PostgreSQL publishes complete immutable revisions; each
serving process compiles and pins an in-memory snapshot for each request.

Review assumed horizontally scaled serving instances behind an external load balancer,
concurrent administrators, streaming traffic, and temporary PostgreSQL outages.
The serving platform owns autoscaling. The review traced bootstrap/CLI inputs through
validation, database transactions, compilation, proxy handling, metrics, and cleanup.

## Must fix — resolved before publication

1. **Invalid options could silently become an empty snapshot** (`internal/config/model.go`).
   JSON normalization discarded errors. A malformed internal candidate could lose its
   routes and still compile. Normalization now returns errors; the regression test
   rejects malformed options instead of accepting an empty routing table.

2. **Publication history lost generation** (`internal/postgres/store.go`).
   History retained revision ID but not the actual publication generation. Revisions now
   store generation explicitly, SQL publication validates it, and the integration test
   asserts that the first authenticated audit record retains generation 1.

3. **Sealing needed relational locking** (`internal/postgres/migrations/001.sql`).
   A plain trigger read could race an entity write against revision sealing. Entity
   triggers now lock the parent revision and reject invisible/moved revisions. State
   updates require a sealed revision with the next matching generation.

4. **Fallback metric labels could collide** (`internal/gateway/gateway.go`).
   A configured route named `unmatched` duplicated the fallback series. The regression
   failed before the fix; fallback now uses the otherwise-invalid empty route ID.

5. **Framing and stream failures needed explicit checks** (`internal/gateway`).
   Tests exposed a read deadline applied to bodyless keep-alive requests. Read deadlines
   now apply only to request-body I/O and clear after completion. Truncated responses
   abort without appending a second response and increment the bounded error counter.
   Upstream TLS tests reject untrusted certificates and mismatched names.

## Should fix — resolved before publication

6. **Cleanup could wait without a deadline** (`internal/postgres/store.go`).
   Background transaction rollback and failed LISTEN cleanup now use bounded contexts.
   Snapshot transports retire only after their last request; WebSocket shutdown and
   retirement tests verify those lifecycles.

7. **JSON field aliases bypassed exact names** (`internal/config/decode.go`).
   The standard decoder accepts case-insensitive field aliases. An exact field-name
   check now rejects those aliases as unknown fields, including nested typed options.
   Header values reject HTTP control characters.

8. **Benchmark clients exhausted local ports** (`cmd/routeqtus-bench`).
   The generator's idle pool was too small for its concurrency, causing connection
   churn. Its bounded pool now matches concurrency. The invalid first run remains
   available; corrected runs report errors and sample counts explicitly.

## Lean changes applied

A slow metrics scrape was also reproduced holding the snapshot publication lock.
Metrics now pins a snapshot and releases the lock before writing to the network;
the PostgreSQL regression verifies publication while a metrics writer is stalled.

Removed a counter wrapper and a status-encoding wrapper; used the standard atomic
counter and JSON encoder directly. Router microbenchmarks identified one allocation
from trying to split a host without a port. A colon check avoids constructing that
error; [benchstat](microbench-summary.txt) records 32 B/1 allocation becoming 0 B/0
allocations in the measured router cases. No runtime/GC tuning was added.

Verdict: Ship after the automated checks on the PR are green.

Not checked: production capacity, a real external generator, Kubernetes autoscaling,
multi-host operation, and Windows; these limits are also documented for users.
