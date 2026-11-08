# Routeqtus

Routeqtus is a Go HTTP API gateway for deployments that keep operational
configuration in PostgreSQL. A separate management process validates and publishes
complete revisions; serving processes compile them into immutable in-memory snapshots.
PostgreSQL is required at startup and periodically afterward to confirm freshness.
Kubernetes or another serving platform handles replica scaling from request load;
Routeqtus supplies per-instance readiness, metrics, resource limits, and draining.

```sh
make demo
curl --fail --silent http://127.0.0.1:8080/api
# routeqtus demo backend: backend-a
```

- Requests use one compiled snapshot without configuration SQL on the request path.
- Mutations require the generation of the complete configuration and commit atomically.
- Invalid revisions preserve the last valid snapshot; stale instances reject new traffic.
- Streaming responses, request bodies, and upgrades use bounded resources and deadlines,
  including final response flushing, rejected request bodies, and admission errors.
- Management requires a Bearer token; upstream TLS verification stays enabled.

## Install

Download an archive and verify `checksums.txt` from
[GitHub Releases](https://github.com/kmpoltorak/routeqtus/releases), or build with Go 1.27.3:

```sh
go build -trimpath -o routeqtus ./cmd/routeqtus
./routeqtus version
```

The container runs without root and includes CA certificates. PostgreSQL 17 or later
is required. The tested environment and limitations are in [verification](docs/verification.md).

## Quick start

With Docker Compose, OpenSSL, curl, and make, run `make demo` as a non-root user.
It explicitly migrates the database, starts `manage`, publishes the example revision,
and starts `serve`. It generates ignored credentials and local TLS files; it does not
overwrite an existing active configuration. Restart with `docker compose up -d`.

Follow [getting_started.md](getting_started.md) for the complete Docker and native
instructions, separate database roles, production TLS, and diagnostics access.

## Commands

| Command | Purpose |
| --- | --- |
| `serve --config PATH` | Read PostgreSQL, compile snapshots, serve public HTTP |
| `manage --config PATH` | Run the authenticated Management API on its own listener |
| `check --config PATH` | Validate bootstrap YAML offline; does not resolve secrets |
| `migrate --config PATH` | Apply versioned SQL with migration-owner credentials |
| `config validate --file PATH` | Validate through Management API without publication |
| `config apply --file PATH --generation N` | Publish a complete JSON revision through HTTP |
| `config show [--effective]` | Export configured values, or inspect resolved defaults |
| `config status` | Read the committed revision and generation |
| `config rollback --revision ID --generation N` | Revalidate history and publish a new generation |
| `openapi` | Print the generated OpenAPI contract without opening the database |

All `config` commands accept `--api` (default `http://127.0.0.1:9080`) and
`--token-env` (default `ROUTEQTUS_MANAGEMENT_TOKEN`). Remote API URLs require HTTPS.
`apply` and `rollback` accept `--description` and `--author`; the author is a client
claim, separate from the token's authenticated service principal.

## Configuration

| Source | Contains | Changes take effect |
| --- | --- | --- |
| Bootstrap YAML | Listeners, environment references, process limits, synchronization | Restart |
| PostgreSQL revision | Routes, targets, upstream transport/TLS, handlers, forwarding trust, metrics | Snapshot reload |
| Environment | DSNs, management/admin tokens, referenced upstream TLS material | Restart/recompile as documented |

YAML rejects routing sections, unknown/duplicate keys, nulls, aliases, and extra
documents. JSON imports reject unknown/duplicate keys and unsupported versions.
JSON is an import format, never an alternative runtime configuration source.

See [bootstrap and every operational field](docs/reference.md),
[OpenAPI](docs/openapi.json), and [API examples](docs/api.md).

## Routing and handlers

Higher priority wins, then explicit host, longer path, and exact before prefix;
methods select among matching candidates. Prefix `/api` matches `/api` and `/api/x`,
not `/apix`. A method mismatch can fall through to a broader matching route;
`methods` selects a route and does not provide access control. Missing paths return
404; unmatched methods return 405 with `Allow` when no candidate accepts the method.
Encoded separators, dot segments, repeated slashes, and ambiguous queries are rejected.
Forwarding trust covers every X-Forwarded-For field line and rejects ambiguous schemes;
see [proxy trust](docs/operations.md).

Global handlers run outside route handlers and also cover 404/405. Lists record
entry order; code after `next` runs in reverse. Disabled handlers add no wrapper.

| Handler | Options |
| --- | --- |
| `request_id` | `{}`; replace untrusted IDs with a generated ID |
| `recover` | `{}`; recover before response start, preserve `http.ErrAbortHandler` |
| `access_log` | `sample_every`; includes method, excludes bodies, URLs, query strings, and auth headers |
| `headers` | Allowed request/response set/remove maps and lists |
| `powered_by` | `value`; example registered extension |

Discover contracts at `/v1/handler-types`. See [extensions](docs/extensions.md).

## Operation

Invalid or missing database DSNs fail immediately. Startup and CLI connection errors
report safe failure categories without printing DSNs, tokens, or server error text.

`serve` reads a complete revision before opening its public listener. LISTEN/NOTIFY
accelerates updates; jittered polling recovers missed signals. A dedicated session
connection is required for notifications; disable them behind transaction pooling.
The connection budget is `postgres.max_conns + 1` for serving with notifications,
and `postgres.max_conns` otherwise. Multiply by process count.

After `max_staleness`, new requests and readiness return 503. Existing streams
continue on their original snapshot until they end or shutdown closes them. A valid
database confirmation restores readiness. Publication is atomic in PostgreSQL and
per instance; it does not switch a cluster simultaneously.

Diagnostics use a separate listener: `/healthz`, `/readyz`, `/status`, `/metrics`.
Metric families include HELP/TYPE metadata; unwritten panics count as 5xx.
Non-loopback diagnostics require an admin token. pprof is off and requires an admin
token when enabled. Protect the diagnostic network; admin HTTP does not provide TLS.
For Kubernetes rollouts, add a preStop propagation delay before SIGTERM.
See [operations](docs/operations.md) for deadlines, TLS, shutdown, and deployment order.

## Limitations

- No gateway-level retry, active backend health checking, circuit breaker, or account system.
- The standard HTTP transport can retry eligible requests on reused connection failures;
  see [transport behavior](docs/operations.md). Bodies are never replayed by gateway code.
- Routing scans sorted candidates; [benchmarks](docs/benchmarks.md) quantify local costs.
- Metrics counters belong to snapshots and reset on reload; historical revisions remain
  in PostgreSQL. Revision history lists the latest 100; known IDs remain readable.
- TLS secret references must exist in both management and serving environments. There is
  no secret-value import/export or built-in encryption/key distribution service.
- No production capacity, external generator saturation, multi-host deployment, or
  Windows support claim. See [executed checks](docs/verification.md).

## Development

| Target | Action |
| --- | --- |
| `make build`, `make run` | Static binary; run native `serve` |
| `make check`, `make test` | Publication manifest, documentation links, formatting/vet; unit and HTTP tests |
| `make test-race`, `make test-integration` | Race checks; disposable real PostgreSQL |
| `make fuzz`, `make vuln` | Bounded fuzzing; govulncheck |
| `make bench`, `make loadbench` | Router microbenchmarks; proxy comparisons |
| `make docs`, `make demo` | Regenerate OpenAPI; initialize local demo |

Tests exercise public behavior with real HTTP servers, HTTP/2 TLS, SSE and WebSocket
traffic, and isolated PostgreSQL databases. Integration tests require database-creation
privileges and never reuse the application's database. Their script starts a disposable
loopback PostgreSQL container unless `ROUTEQTUS_TEST_DATABASE_URL` is supplied.
Verification includes deliberate sabotage checks; results and commands are recorded.

Direct dependencies are pgx/v5 for PostgreSQL pooling and the maintained YAML parser
from `yaml/go-yaml`. HTTP, logging, CLI, TLS, metrics exposition, and JSON use Go's
standard library. No ORM, router framework, or plugin loader is used.

Built with an AI coding assistant. I wrote the spec and reviewed every change.

## License

[MIT](LICENSE).
