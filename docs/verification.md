# Verification

Local verification was performed on 2026-11-08 with Go 1.27.3, macOS/amd64,
Docker Engine 29.8.0, and real PostgreSQL 17.11. CI repeats correctness and container
checks on Ubuntu. Run commands from the repository root.

| Check | Command / evidence | Local result |
| --- | --- | --- |
| Formatting and static analysis | `make check` | Passed |
| Unit and real local HTTP tests | `go test ./...` | Passed |
| Unit/HTTP race detector | `go test -race ./...` | Passed |
| PostgreSQL integration | `make test-integration` or `sh scripts/test-integration.sh` | Passed with isolated databases |
| PostgreSQL race detector | `sh scripts/test-integration.sh -race` | Passed |
| Strict JSON fuzzing | `go test ./internal/strictjson -run='^$' -fuzz=FuzzDecode -fuzztime=5s` | Passed; 321277 executions in recorded run |
| Request-path fuzzing | `go test ./internal/gateway -run='^$' -fuzz=FuzzRequestPath -fuzztime=5s` | Passed; 175676 executions in recorded run |
| Vulnerability scan | `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` | No vulnerabilities found |
| Static build | `make build` (`CGO_ENABLED=0`) | Passed |
| Container | `docker build --tag routeqtus:local .` | Passed; numeric non-root user and bundled CAs |
| Complete demo | `make demo` on a fresh demo database | Passed; expected backend output observed |
| API examples | `sh examples/api.sh` against TLS demo | Passed, including expected 422 and 412 responses |
| OpenAPI/field reference | `make docs`; `TestPublishedContractMatchesRegistry` | Generated; runtime registry and file compared |
| Every operational field | `TestEveryOperationalFieldRoundTripsThroughAPIAndPostgres` | Passed, including TLS references, explicit zero/false, and ordering |
| Request-path SQL | `TestDatabaseOutageAndNoRequestSQL` | 100 requests caused zero configuration pool acquisitions |
| Live changes/outage | `TestRuntimeTrafficDuringPublicationAndOutage` | Zero traffic errors; [raw measurements](benchmark-control-plane.txt) |
| Sabotage | `ROUTEQTUS_TEST_DATABASE_URL=... make sabotage` | Four deliberate behavior failures detected; [record](sabotage.txt) |
| Performance | See [benchmarks](benchmarks.md) | Actual raw data, comparison, CPU profile, and limits recorded |

Integration tests require an administrative, disposable PostgreSQL connection with
database-creation privileges. They create uniquely named databases and remove only
those databases afterward. `make test-integration` creates and removes a loopback
container when no test DSN is supplied. Test credentials and cleartext PostgreSQL
transport belong only to that local disposable environment.

## Contract and failure coverage

Tests cover routing precedence, host/method matching, 404/405/Allow, prefix boundaries,
encoded separators, ambiguous queries, prefix stripping, Host rewriting, and round robin.
Handler tests cover schema examples, required/unknown options, ordering, disabled
wrappers, control characters, and log secret redaction. HTTP tests use real listeners
for cancellation, full deadlines, chunked bodies, HTTP/2 TLS, verified upstream TLS,
SSE first-event delivery, trailers, truncated streams, and bidirectional WebSockets.
Writer wrappers preserve response-controller flushing and hijacking.

PostgreSQL tests cover migration locking, Ping and pool exhaustion deadlines,
cross-revision foreign keys, immutable published rows, parameterized metadata,
publication, generation audit, ETag races, dry-run/invalid-candidate non-publication,
PATCH/null, rollback, and whole-configuration dependency validation. Reload tests
cover traffic during updates, invalid revisions, polling without notifications,
notification-session termination/reconnect, two-instance convergence, staleness,
recovery, snapshot pinning/retirement, and shutdown of active WebSockets. A separate
regression ensures that a stalled metrics client cannot block publication. Protocol tests
verify WebSocket exchange against a TLS backend advertising HTTP/2 as well as plain HTTP.
Process tests
exercise empty/missing-database startup, first publication, and manage restart while
serve continues routing.

## Sabotage evidence

`scripts/sabotage.py` restores the original source in a `finally` block after each case
and requires the named test's actual assertion failure, not a compilation error.

- Removed duplicate-key rejection → `TestAmbiguousDocuments` failed.
- Removed streamed body enforcement → `TestTimeoutCancellationAndBodyLimits` failed
  because the oversized chunked request returned 204 instead of 413.
- Removed immediate-peer trust enforcement → `TestForwarding` failed on a spoofed scheme.
- Removed expected-generation enforcement → `TestControlPlaneAndReload` failed because
  concurrent writes no longer produced exactly one successful commit and one precondition failure.

The complete checks were rerun after restoration. The metric-collision regression was
also observed failing before its fix: a route named `unmatched` duplicated fallback
series. Fallback now uses an empty route ID, which cannot collide with a configured ID.

The first Linux CI run exposed an incompatible old govulncheck analyzer (v1.1.4),
which panicked while analyzing Go 1.27 syntax. The pinned tool was upgraded to v1.9.0
and the full source scan passed both natively and in the Go 1.27.3 Linux container.
The vulnerability check was retained without weakening its failure behavior.

## Used skills and limits

Applied skills: ponytail (minimal implementation), codebase-design (module interfaces),
research (primary-source verification and `research.md`), ponytail-review (final review),
and pr (reviewable PR description). The research skill used one background research
agent. No optional skill/plugin was installed and agent configuration was not changed.

These checks do not certify production capacity, external-generator saturation,
Kubernetes/HPA behavior, multi-host failure recovery, database-version upgrade paths,
or Windows support. Three microbenchmark repetitions are insufficient for benchstat's
95% confidence interval; allocation results and observed timings are reported without
a statistical speedup claim. The independent load generator, backend, and gateway must
be deployed on representative separate hosts before setting production capacity/SLOs.
