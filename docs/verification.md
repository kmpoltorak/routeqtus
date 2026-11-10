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
| Container | `docker build --tag routetus:ci .`; `sh scripts/check-image.sh` | Default production filesystem excludes demo backend, retains CAs, and uses numeric non-root user |
| Complete demo | `make demo` on a fresh demo database | Passed; expected backend output observed |
| API examples | `sh examples/api.sh` against TLS demo | Passed, including expected 422 and 412 responses |
| OpenAPI/field reference | `make docs`; `TestPublishedContractMatchesRegistry` | Generated; runtime registry and file compared |
| Every operational field | `TestEveryOperationalFieldRoundTripsThroughAPIAndPostgres` | Passed, including TLS references, explicit zero/false, and ordering |
| Request-path SQL | `TestDatabaseOutageAndNoRequestSQL` | 100 requests caused zero configuration pool acquisitions |
| Live changes/outage | `TestRuntimeTrafficDuringPublicationAndOutage` | Zero traffic errors; summary in [benchmarks](benchmarks.md) |
| Sabotage | `ROUTETUS_TEST_DATABASE_URL=... make sabotage` | Four deliberate behavior failures detected; sources restored |
| Performance | See [benchmarks](benchmarks.md) | Measured results, method, and limits summarized |

Integration tests require an administrative, disposable PostgreSQL connection with
database-creation privileges. They create uniquely named databases and remove only
those databases afterward. `make test-integration` creates and removes a loopback
container when no test DSN is supplied. Test credentials and cleartext PostgreSQL
transport belong only to that local disposable environment.

## Repository map

Application source is in `internal/`; `cmd/` contains the small executable entry points.
Start with [the system overview](operations.md#system-overview) to follow the request
and configuration paths before reading individual packages.

| Location | Responsibility |
| --- | --- |
| `cmd/routetus/` | Gateway executable entry point |
| `cmd/routetus-ui/`, `internal/ui/` | Separate local GUI executable, HTTP interface, and embedded HTML/CSS/JavaScript |
| `cmd/demo-backend/`, `cmd/routetus-bench/` | Demo response server and development benchmark generator |
| `internal/cli/` | Command parsing and the Management API HTTP client |
| `internal/app/`, `internal/bootstrap/` | Process startup/shutdown, listeners, diagnostics, and startup YAML |
| `internal/management/`, `internal/configservice/` | HTTP API resources and shared validation/publication operations |
| `internal/config/`, `internal/strictjson/` | Operational types, limits, schemas, errors, and strict JSON decoding |
| `internal/postgres/` | Database connections, migrations, revisions, notifications, and health monitoring |
| `internal/runtime/` | Background synchronization, readiness, and replacement/retirement of active settings |
| `internal/gateway/` | Compile settings into request handlers; match routes and proxy HTTP traffic |
| `internal/middleware/`, `internal/extensions/` | Shared processing steps and the registered `powered_by` extension |
| `internal/neterror/` | Safe connection-error categories used by process startup and the CLI |
| `internal/testutil/`, package `*_test.go` files | Shared test fixtures and unit/HTTP tests beside their implementation |
| `tests/integration/`, `tests/workflows/` | Real PostgreSQL/process integration tests and release-workflow contract tests |
| `examples/`, `compose.yaml`, `Dockerfile` | Sample configuration, local service wiring, and production/demo image targets |
| `scripts/`, `.github/workflows/` | Test/demo/build/documentation helpers and GitHub CI/release automation |
| `README.md`, `getting_started.md`, `docs/` | Project summary/diagram, startup instructions, and detailed documentation |

`docs/openapi.json` and `docs/reference.md` are generated by `make docs` from the
registered contract and `scripts/reference.py`. Change their source definitions or
generator rather than editing generated output. Local credentials, demo certificates,
build output, and scratch files are ignored; they are not application source.

## Development commands

| Target | Action |
| --- | --- |
| `make build`, `make run` | Static binary; run native `serve` |
| `make build-ui` | Build the separate optional local GUI executable |
| `make check`, `make test` | Publication manifest, documentation links, formatting/vet; unit and HTTP tests |
| `make test-race`, `make test-integration` | Race checks; disposable real PostgreSQL |
| `make fuzz`, `make vuln` | Bounded fuzzing; govulncheck |
| `make bench`, `make loadbench` | Router microbenchmarks; proxy comparisons |
| `make docs`, `make demo` | Regenerate OpenAPI; initialize local demo |
| `make sabotage` | Temporarily weaken guards, require test failures, restore sources |

For a routine correctness check, run `make check test test-race`, followed by
`make test-integration` with Docker running. Integration tests start their own
PostgreSQL container unless a test DSN is provided; an initialized application demo
is not required. For manual traffic checks, use the public listener described in
[operations](operations.md#local-demo-troubleshooting).

A shell loop with curl can check response codes and individual request times. An
all-200 result confirms success only for that run and workload. The requested `xargs`
parallelism is an upper limit, not proof that all requests overlapped. Total elapsed
time is needed to calculate requests per second; averages and maxima do not provide
latency percentiles. See [benchmarks](benchmarks.md) for the measured methodology.

Tests exercise public behavior with real HTTP servers, HTTP/2 TLS, SSE and WebSocket
traffic, and isolated PostgreSQL databases. Integration tests require database-creation
privileges and never reuse the application's database. Their script starts a disposable
loopback PostgreSQL container unless `ROUTETUS_TEST_DATABASE_URL` is supplied.
Security regressions cover remote CLI HTTPS, diagnostics authentication, token and
metadata byte limits, and latest-100 history with older-ID reads.
Regressions also cover header precedence on HTTP, errors, and upgrades, plus database
connection release after malformed stored values. Verification includes deliberate
sabotage checks; results and commands are recorded. `make sabotage` requires an
explicit disposable `ROUTETUS_TEST_DATABASE_URL` with database-creation privileges.
It edits source files temporarily and restores them in `finally`; run it without
concurrent edits/tests and inspect the working diff if the process is forcibly killed.
Root `AGENTS.md` and `CLAUDE.md` are published source instructions; release archives
omit them. Plans, prompts, and raw working reports remain excluded.

## Security boundary coverage

Tests reject remote plaintext CLI URLs before a request, non-loopback diagnostics
without a token reference, and unauthorized diagnostic requests. Process tests cover
0/31/32-byte management and admin tokens. Real HTTP/PostgreSQL publication tests cover
256-byte authors and 4096-byte descriptions, including UTF-8 byte boundaries and
unchanged revision state on rejection. A 105-revision test verifies newest-100 ordering
and an older known-ID read. Each guard was deliberately weakened through local overlays;
the named regression failed, and normal sources passed afterward.

## Cached management readiness

A real PostgreSQL regression observed 100 SQL acquisitions for 100 readiness probes
before the fix and zero after it. Management and diagnostics endpoints share a bounded
background health sample. Tests cover database outage/recovery on both real process
listeners, authentication, immediate unready state on monitor cancellation, and worker
cleanup. Serving readiness continues to use its runtime snapshot state.

## Schema and validator agreement

Boundary tests derive declared numeric, pattern, length, and collection constraints
from schemas and exercise the real compiler/factories. Coverage includes priority,
connection/default-zero limits, sampling, identifiers/methods, 1024 targets and 10000
entities, plus Unicode and control characters in powered_by values. Common constants
keep declarations and validators aligned. The previous byte-based powered_by limit
rejected schema-valid Unicode strings; its pattern also admitted controls rejected by
the factory. Values use the documented character limit and permit HTAB while
rejecting other ASCII controls, matching the v0.1.0 header-value behavior. The
pre-release blanket tab rejection was corrected before publishing v0.1.1; no manual
tab-cleanup migration is required. Ordinary header handler behavior is unchanged.

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

Configuration decoding uses a duplicate/depth scan, a typed decode, then a separate
generic parse for field-presence validation. The extra control-plane parse replaces
the recursive tree-collection flag; parsing is not part of proxy request handling.
Tests preserve required/case-sensitive fields, nullable
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

## Configuration error regressions

`TestResourceConversionErrors` covers malformed RawMessage and numbers outside
float64 range, including preservation of the conversion cause. A real PostgreSQL
case stored a valid JSONB value outside that range: its GET previously returned 200
with value 0. It now returns a safe 503, and PATCH leaves revision/history unchanged.
`TestIndependentErrorsHaveEntityContextAndDoNotPublish` checks two failures in each
compilation phase, stable IDs with JSON Pointers, no revisions, and no notification.
The baseline returned only one error per candidate. Context tests also cover wrapped
and joined errors, missing required fields, bindings, malformed IDs, and invalid indices.
`TestRuntimeRejectionLogsRevisionAndKeepsSnapshot` preserves working proxy traffic
while rejecting three stored revisions, checks each revision ID even for identical
messages, and excludes target credentials. The baseline logged two entries with no
revision IDs for three rejected revisions. Unit and focused PostgreSQL race checks pass.

## Strict JSON simplification

The selective rollback removes DecodeDocument and the recursive collect flag; row
collectors, response-header reuse, numeric startup logging, and error context remain.
Existing tests preserve raw JSON number range versus generic configuration float64
range, required/null/case/false semantics, depth, duplicate keys, and trailing values.
`TestDecodedObjectKeyUniqueness` additionally checks escaped equivalent keys and
separate object scopes. Removing duplicate rejection makes its assertions fail.
This favors simpler configuration code and restores the extra parse/allocation cost;
no performance improvement is claimed. Management round trips and bounded fuzzing
passed after the change; the bounded fuzz run completed 328463 executions.
Sabotage overlays also removed required-field enforcement and bypassed the generic
float64 range check; both existing characterization tests failed as intended.

## Operational contract checks

`TestTLSProxyToLoopbackManagement` starts a real management process with PostgreSQL
and no direct TLS on numeric loopback. A trusted local TLS reverse proxy forwards
readiness and publication. Missing/incorrect Bearer tokens return 401 and leave
generation unchanged even with X-Forwarded-Proto: https; the valid token publishes.
`TestManagementTLSListenerBoundary` retains direct TLS outside numeric loopback and
requires certificate/key pairs. This tests the supported same-namespace topology;
no arbitrary plaintext remote-proxy mode is introduced.

`TestPercentRequestPathPolicy` exercises the documented 400 for decoded percent,
%25, and double encodings, checks that rejected paths never reach the backend, and
preserves encoded spaces/Unicode and query-percent values. Sabotage overlays removed
the percent guard, the non-loopback TLS requirement, and proxy-path authentication;
each named test failed its intended assertion. Linear route scanning
and latest-100 history are deliberate bounded policies with ponytail comments.

A combined local run with concurrent fuzzing and separate race suites failed the
reload-traffic assertion. Instrumented repetitions reproduced 503 when confirmation
age exceeded that test's 250ms staleness budget, without a database error; five
isolated repetitions passed. This is the existing fail-closed freshness policy under
CPU contention. The complete PostgreSQL race suite passed when run separately
from CPU stress; no test assertion or staleness threshold was weakened.

## Dependencies and skills used

The two direct runtime dependencies are pgx/v5 for PostgreSQL pooling/batching and
the maintained yaml/go-yaml parser for strict bootstrap YAML. HTTP, logging, CLI,
TLS, JSON, and the fixed metrics exporter use the Go standard library. No ORM,
router framework, runtime plugin loader, or additional validation dependency is used.

The following skills were actually used during implementation and follow-up work:

| Skill | Applied work |
| --- | --- |
| ponytail | Small complete changes, explicit trade-offs, existing helpers/dependencies |
| ponytail-review | Connected-code and diff review before pull requests |
| codebase-design | Interfaces for snapshots, configuration publication, and the separate local GUI |
| tdd | HTTP and browser behavior checked before and during local GUI implementation |
| research | Primary-source checks for platform and workflow behavior |
| diagnosing-bugs | Reproduce CI, runtime, and demo failures before fixing them |
| pr | Concrete changes, before/after evidence, verification, and merge risk |
| writing-for-agents | Check the shared AGENTS rules and CLAUDE import before source publication |

Plans, prompts, raw reports, logs, and benchmark inputs remain working material;
they are excluded from the public source and release manifests. Root `AGENTS.md`
and `CLAUDE.md` are explicitly allowed in source, while archives still exclude them.
The publication self-check asserts this distinction and retains rejection of nested
instruction files, lowercase variants, private directories, and traversal paths.

## Bounded compiler reports

`TestValidationReportCountAndSize` covers 0/1/100/101/10000 bad routes, retained entity
context, and the exact ordinary omission field. The baseline produced 10000 fields
and a 707779-byte message. `TestValidationReportLongDetails` covers large untrusted
handler-type names and UTF-8-safe message truncation; its baseline exceeded 1.6 MB.
Reporting is bounded once after Compile adds context: at most 100 errors plus an
omission entry, 256 bytes per message, and an 8 KiB formatted report. Independent
validation still rejects the entire candidate; WithErrorContext is unchanged.
`TestValidationReportAcrossAPIStatusAndLog` checks bounded real 422/status/log JSON,
no publication/history/notification on API rejection, and last-good proxy traffic
when privileged storage inserts the bad revision. The existing Problem schema is
unchanged; no new response fields or dependencies are introduced.

## powered_by legacy compatibility

The shared schema/factory pattern allows HTAB and still rejects NUL, CR, LF, DEL,
and other invalid ASCII controls. An independent control-character regression checks
both acceptance and rejection, alongside the existing Unicode/schema boundaries.
`TestServeStartsWithLegacyPoweredHTAB` loads a stored legacy revision, starts a real
serve process, observes readiness, and receives the unchanged interior tab through
an actual proxied response header. Before the fix it failed with a startup timeout.
RFC 9110 [field values](https://www.rfc-editor.org/rfc/rfc9110.html#section-5.5)
permit interior HTAB; no validation looseness beyond the old header behavior is added.

## Routetus naming checks

`TestRoutetusUsage` checks the public CLI name. `TestDefaultManagementTokenEnvironment`
authenticates a real HTTP request using the default ROUTETUS_MANAGEMENT_TOKEN reference.
Both failed before the rename and passed afterward; restoring the previous default or
an incorrect usage name through local overlays made the intended assertions fail.
Unit/race and full PostgreSQL normal/race checks passed under the new module path,
SQL schema, notification channel, and environment names. A fresh Routetus Compose
project passed migration, role grants, publication, and serving; TLS API examples and
both named backend responses passed. Production-image checks verify the new binary,
CA bundle, numeric non-root user, and absence of the demo backend. The rename uses
fresh test data rather than a compatibility migration. Existing releases remain immutable.

## Handler binding isolation

`TestHandlerBindingConcurrencyScope` uses real frontend/backend HTTP servers and a
stateful test extension. It verifies saturation on one route does not exhaust another
route using the same handler ID, while explicit global bindings share capacity. It
also binds the same ID globally and locally to prove their state is separate, and
checks capacity returns after the held request completes.

`TestAccessLogSamplingIsIndependentPerRoute` exercises the shipped `access_log` type:
each route emits its own first sampled request. Before the fix, the shared counter
sampled only one of the two interleaved routes. `TestUnboundAndDisabledHandlersRemainValidated`
retains strict option validation and handler IDs for unused and disabled definitions.

## Local GUI checks

Unit HTTP tests cover server-only credentials, resource creation/editing/deletion, type discovery, generation propagation, validation/conflict
responses, exact Host/Origin checks, malformed/duplicate origins, wrong content type,
request size, loopback configuration, static assets, redirect rejection, and untrusted
Management API TLS. They use real HTTP servers and do not need the application demo.

CI starts the optional GUI container after initializing its disposable demo, verifies
its API connection, and runs `tests/ui/browser.cjs` with Playwright 1.65.0 and Chrome.
Browser checks exercise the handler guide, numeric handler options, upstream registration,
Routes/Upstreams tabs, CRUD and filtering, cancellation, stale generation refresh and retry,
preserved advanced fields, handler/method order, stable target IDs, target-array conflicts,
referenced-upstream deletion rejection (including a late concurrent reference), invalid-route
and invalid-backend rejection, safe rendering of path text, and mobile layout in both sections.
Playwright is test tooling only; it is not a Go/runtime dependency and adds no frontend
build step. The production image check rejects a leaked GUI executable.

`tests/ui/theme.cjs` checks Light/Dark/System selection, live OS changes in System
mode, explicit overrides, persistence after reload, invalid preferences, and blocked
storage. It checks rendered page/card/input/dialog/tab/error colors. The selector
regression failed before the control existed. `tests/ui/handlers.cjs` checks automatic
global summaries, disabled-global labels, and their exclusion from new route choices;
the CRUD flow also checks preservation and removal of existing double bindings.
Sabotage overrides that restored global checkboxes and removed preference persistence
failed the selection and reload assertions respectively. The normal implementation
passes both checks. Overrides and screenshots remain private working material.

To repeat the browser check, install Playwright into an ignored or temporary tool directory,
initialize a disposable demo with the GUI profile, and run from the repository root:

```sh
npm install --prefix /private/tmp/routetus-ui-browser --no-audit --no-fund playwright@1.65.0
NODE_PATH=/private/tmp/routetus-ui-browser/node_modules ROUTETUS_UI_TEST_DISPOSABLE=1 node tests/ui/browser.cjs
```

Chrome must be installed. `ROUTETUS_UI_URL` overrides the default `http://127.0.0.1:9180`.
The test creates routes and intentionally triggers conflicts/validation errors; use only
a disposable configuration. Optional screenshots go to `ROUTETUS_UI_SCREENSHOT_DIR`.
It does not establish production capacity or support a public GUI deployment.

CRUD regression sabotage checks removed If-Match forwarding through a temporary Go
overlay: `TestResourceEditsAndUpstreamDeletion` failed for both resource types. A
temporary browser response override replaced the edited-field PATCH with the full old
route: the concurrent-priority assertion failed (`42` instead of `43`). The unchanged
implementation passes both checks. Working overlays and screenshots are not published.
