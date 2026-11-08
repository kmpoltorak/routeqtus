# Routeqtus delivery checklist

Approved scope: `routeqtus_prompt.md`. Code, comments, logs, and documentation are in English.

- [x] Read the specification and repository rules; preserve existing local files.
- [x] Obtain implementation-plan approval and create a feature branch.
- [x] Verify Go, Docker, GitHub authentication, and stable dependency releases.
- [x] Bootstrap YAML: strict decoding, defaults, environment references, offline check.
- [x] Operational configuration: typed fields, strict JSON, defaults, schemas, validation.
- [x] Handler registry: request ID, recovery, access log, headers, extension example.
- [x] Gateway: deterministic routing, encoded paths, forwarding trust, load balancing.
- [x] Proxy lifecycle: streaming, HTTP/2 TLS, SSE, WebSocket, limits, I/O deadlines.
- [x] PostgreSQL: migrations, constraints, immutable revisions, roles, atomic publication.
- [x] Management API: authentication, ETags, full configuration, resource CRUD/PATCH.
- [x] Management API: validation, effective configuration, history, rollback, OpenAPI.
- [x] CLI: serve, manage, check, migrate, config validate/apply/show/status/rollback.
- [x] Synchronization: LISTEN/reconnect, jittered polling, atomic snapshots, staleness.
- [x] Diagnostics: health, readiness, protected status, bounded metrics, optional pprof.
- [x] Graceful shutdown including hijacked connections and old-snapshot cleanup.
- [x] Tests: observable behavior, failure modes, real PostgreSQL, two-instance updates.
- [x] Tests: sabotage checks, race detector, bounded fuzzing, vulnerability scan.
  - [x] Unit and PostgreSQL integration race detector.
  - [x] Bounded strict JSON and URL/path fuzzing.
  - [x] govulncheck: no vulnerabilities found.
  - [x] Deliberate sabotage checks and restoration.
- [x] Benchmarks: baseline comparison, route/payload/concurrency matrix, latency/resource report.
- [x] Docker image, Compose demo, environment example, Makefile, CI/release workflows.
- [x] README, getting_started, API/field reference, operations and extension documentation.
- [x] Apply ponytail-review; fix findings and remove unused code/dependencies.
- [ ] Open PR with verification evidence; obtain green CI before merge.
  - [x] Opened https://github.com/kmpoltorak/routeqtus/pull/1.
  - [ ] CI verification and fresh Compose demo jobs green.
- [ ] Publish GitHub release with binaries, checksums, notes, and documented limitations.

Risks to verify: ambiguous URLs; forwarding-header spoofing; streaming deadlines;
lost updates; invalid or stale snapshots; connection/resource leaks; secret disclosure.

No test or benchmark is considered passed unless its command actually completed.

## Verification log

- `go test ./...`: passed, including HTTP/2, SSE/trailers, bidirectional WebSocket,
  encoded-path and forwarding-header rejection, chunked-body limits, and shutdown.
- Real PostgreSQL integration: passed migration locking, immutable revisions,
  cross-revision constraints, concurrent publication, PATCH/null, rollback, invalid
  reload, reconnect, two-instance convergence, database outage, startup, and manage restart.
- Request-path SQL test: 100 proxy requests caused zero configuration acquisitions.
- `go vet ./...`, static binary build, OpenAPI generation, Docker image build: passed.
- Compose end-to-end demo: passed migration, separate roles, TLS management,
  first publication, read-only serving, and the documented proxy response. Initial
  mounted-shell initialization failed; using native psql initialization SQL fixed it.
- First load benchmark reported local socket exhaustion. Generator pooling was fixed;
  the invalid raw run is retained and excluded from accepted performance results.
- Race detector: unit/local HTTP and real PostgreSQL integration passed.
- Bounded fuzzing passed (strict JSON and request paths); govulncheck found no vulnerabilities.
- Executed `examples/api.sh` against TLS demo: schemas, publication, CRUD, PATCH/null,
  effective GET, dry-run, 422 validation, 412 conflict, and rollback passed.
- Final HTTP rerun passed: 90 cases, 1514724 requests, zero errors; CPU profile captured.
- HTTPS: 30 cases, zero errors. SSE: 20 cases including two-second streams, zero errors.
- Router microbenchmarks and benchstat comparison recorded; measured lookup allocation
  dropped from 32 B / 1 allocation to 0 B / 0 allocations. No statistical timing claim.
- Live Runtime benchmark passed stable, publication, and database-outage traffic phases.
- Final review tightened SQL revision locking and publication-generation audit,
  normalization errors, case-sensitive field validation, and cleanup deadlines.
- Four sabotage checks failed the intended tests; files were restored and tests passed.
- Final ponytail-review is documented in docs/review.md; findings were fixed and tested.
- PR #1 is open. Four Linux/macOS amd64/arm64 release archives were cross-compiled;
  native version smoke check and all SHA-256 checksums passed. CI and release remain open.
- Slow-metrics regression reproduced publication blocking, then passed after pinning
  the snapshot and releasing the lock before network writes.
- Initial CI: Compose demo passed; verification reached govulncheck, whose old v1.1.4
  analyzer panicked on Go 1.27 Linux syntax. Updated to official v1.9.0; native and
  Linux-container full source scans passed with no vulnerabilities. CI rerun pending.

## Deployment clarification

The serving platform (Kubernetes or equivalent) owns autoscaling and replica count.
Routeqtus must support independent stateless serving replicas, per-instance readiness,
bounded resources, useful metrics, and graceful drain. No custom autoscaler/operator
is in scope. Account for the connection budget at the maximum replica count.
