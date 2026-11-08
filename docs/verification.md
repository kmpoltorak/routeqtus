# Verification

Local verification was performed on 2026-11-08 with Go 1.27.3, macOS/amd64,
Docker Engine 29.8.0, and real PostgreSQL 17.11. CI repeats correctness and container
checks on Ubuntu. Run commands from the repository root.

| Check | Command / evidence | Local result |
| --- | --- | --- |
| Formatting and static analysis | `make check` | Passed |
| Publication manifest | `python3 scripts/check_publication.py` and archive checks | Excludes working material and checks local documentation links |
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
| Live changes/outage | `TestRuntimeTrafficDuringPublicationAndOutage` | Zero traffic errors; summary in [benchmarks](benchmarks.md) |
| Sabotage | `ROUTEQTUS_TEST_DATABASE_URL=... make sabotage` | Four deliberate behavior failures detected; sources restored |
| Performance | See [benchmarks](benchmarks.md) | Measured results, method, and limits summarized |

Integration tests require an administrative, disposable PostgreSQL connection with
database-creation privileges. They create uniquely named databases and remove only
those databases afterward. `make test-integration` creates and removes a loopback
container when no test DSN is supplied. Test credentials and cleartext PostgreSQL
transport belong only to that local disposable environment.

## Contract and failure coverage

Tests cover routing precedence, host/method matching, 404/405/Allow, prefix boundaries,
encoded separators, ambiguous queries, prefix stripping, Host rewriting, and round robin.
`TestMethodSelectionFallsBackToBroaderRoute` verifies GET-specific matching and POST
fallback to a broader any-method route; method matching is not an access-control layer.
Handler tests cover schema examples, required/unknown options, ordering, disabled
wrappers, control characters, and log secret redaction. HTTP tests use real listeners
for cancellation, full deadlines, chunked bodies, HTTP/2 TLS, verified upstream TLS,
SSE first-event delivery, trailers, truncated streams, and bidirectional WebSockets.
Writer wrappers preserve response-controller flushing and hijacking.
`TestDeadlineCoversFinalFlushAndRejectedBody` uses an unbuffered net.Pipe connection
to prove that a client refusing response I/O or withholding a rejected request body
cannot keep those operations alive past the configured deadline. It also covers
unready-instance rejection. Each case failed before its fix and passed afterward.

PostgreSQL tests cover migration locking, Ping and pool exhaustion deadlines,
cross-revision foreign keys, immutable published rows, parameterized metadata,
publication, generation audit, ETag races, dry-run/invalid-candidate non-publication,
PATCH/null, rollback, and whole-configuration dependency validation. Reload tests
cover traffic during updates, invalid revisions, polling without notifications,
notification-session termination/reconnect, two-instance convergence, staleness,
recovery, snapshot pinning/retirement, and shutdown of active WebSockets. A separate
regression ensures that a stalled metrics client cannot block publication. Protocol tests
verify WebSocket exchange against a TLS backend advertising HTTP/2 as well as plain HTTP.
`TestLargeRevisionPublication` publishes and loads 2000/10000 routes with a 1ms
delay per socket read, checks late-row rollback without notification, and verifies
retry and generation advancement. The 2000-route case failed at the 2s budget before
batching and passed after it; [measurements](benchmarks.md#large-revision-publication)
include the larger case and separate timeout budgets.
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

## Follow-up audit regressions

- `TestForwardingFieldLines` exercises multiple XFF field lines, invalid nearest hops,
  trusted/untrusted peers, and repeated or comma-separated Proto values. Four cases
  failed with the original first-line-only implementation and passed after the fix.
- Panic metric/access-log tests failed when unwritten panics were counted as 2xx.
  Tests also preserve written statuses, abort propagation, and zero in-flight cleanup.
- Recovery-log tests verify a stack trace without the panic value; access-log tests
  check the method while excluding credentials, bodies, and query strings.
- Metric tests check HELP/TYPE metadata and values for both serve and manage exporters.
  Restoring the old exporters through a local overlay made the tests fail.
- Startup tests reject missing/invalid DSNs before the startup timeout. Real CLI
  connection tests distinguish an untrusted TLS certificate and a refused connection.
  Error-classification tests ensure server messages, URLs, and credentials are omitted.

## Deferred audit cleanup checks

The row-loading refactor uses pgx collectors to close rows and propagate scan/decode
errors. Field round-trip coverage includes two upstreams with different target orders
and separate route handler bindings. `TestRevisionDecodeFailureReleasesConnection`
loads a valid row followed by a malformed stored duration through a one-connection
pool, then verifies that the error is returned and the pool still works.

`TestResponseHeaderOrderAcrossProtocols` checks global/route precedence, header removal,
and generated request IDs for HTTP 200, a 101 upgrade, and a gateway-generated 404.
Both response paths use the same header operation.

Configuration field-presence validation reuses the strict JSON scan's document tree,
with a separate typed decode. Tests preserve required/case-sensitive fields, nullable
pointers, explicit false, duplicate rejection, depth limits, and raw-number behavior.
The generic configuration tree retains its former float64 range check, while direct
raw JSON decoding still accepts valid JSON numbers beyond that range. The startup
`listen_connections` log field remains numeric after removing its conversion helper.

## Validation limits

These checks do not certify production capacity, external-generator saturation,
Kubernetes/HPA behavior, multi-host failure recovery, database-version upgrade paths,
or Windows support. Three microbenchmark repetitions are insufficient for benchstat's
95% confidence interval; allocation results and observed timings are reported without
a statistical speedup claim. The independent load generator, backend, and gateway must
be deployed on representative separate hosts before setting production capacity/SLOs.


## Release privilege boundary

Release verification, downloaded tools, and packaging run with contents:read. The
publish job requires successful verification and downloads the exact immutable artifact
from that run. It checks checksums and publishes without checking out source or
running project/build code. Pull requests touching the release workflow exercise
verification and packaging without publishing. `TestReleasePrivilegeIsolation` checks
the permission/dependency boundary and restricts the write-enabled job's steps.
