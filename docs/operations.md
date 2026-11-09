# Operations

## System overview

Routetus is a front door for your HTTP services. Clients use its public address;
Routetus forwards each matching request to a configured backend and returns the
backend's response. Your backend implements the application behavior.

| Component | Responsibility |
| --- | --- |
| `routetus serve` | Accept client requests and forward them using settings held in memory |
| `routetus manage` | Expose the authenticated API for reading, validating, and publishing settings |
| `routetus config ...` | Act as an HTTP client of that API; curl can perform the same operations |
| PostgreSQL | Store routing settings, revision history, and the currently active revision |
| Backend services | Handle the forwarded request and generate the response |
| Kubernetes or another serving platform | Start and scale processes, route traffic to replicas, and manage their lifecycle |

`serve` and `manage` are separate processes started from the same executable. Their
work is independent: clients do not pass through `manage` when calling a backend.
See the [README diagram](../README.md#architecture) for these two paths and the
[source map](verification.md#repository-map) for the implementing packages.

### Request path

A route is a matching rule, such as `GET /api`. It selects an upstream: a named group
of backend addresses. A handler is an additional processing step, such as logging a
request or adding a response header. Global handlers apply across routes; route
handlers apply to the selected route.

For the configuration in `examples/config.json`:

1. A client sends `GET /api` to the public demo listener on port 8080.
2. The `demo` route matches and selects the upstream also named `demo`.
3. The upstream selects `backend-a:8081` or `backend-b:8081` in round-robin order
   within that serving instance. `strip_prefix: true` removes `/api`, so the backend
   receives `/`.
4. That backend generates `routetus demo backend: backend-a` or the equivalent
   `backend-b` response. The route's `powered` handler adds `X-Powered-By: Routetus`,
   and the gateway sends the response to the client.

The two demo containers run the same program from `cmd/demo-backend/main.go`;
`DEMO_BACKEND_NAME` distinguishes their responses. They are sample services, built
in the separate demo image target. The production gateway image excludes them.

### Configuration path

The startup YAML and operational routing settings have different jobs:

| Location | Purpose | When changes take effect |
| --- | --- | --- |
| `examples/bootstrap.yaml`, `examples/docker/manage.yaml`, `examples/docker/serve.yaml` | Process listeners, database environment references, timeouts, and synchronization settings | Process restart |
| `examples/config.json` | A sample document to submit to the Management API | Explicit validation/publication; editing the file alone does nothing |
| PostgreSQL | The active routes, upstreams, handlers, and their saved revisions | Background synchronization by each serving instance |
| Ignored `.env` and `.demo/tls/` | Local demo credentials and management TLS certificate/key | As described in [configuration sources](reference.md#configuration-sources) and [synchronization](#synchronization-and-failure) |

`manage` validates the complete proposed configuration and publishes it in one
transaction. Each successful publication creates a stored revision and advances the
generation, a counter used to reject conflicting edits. A revision ID identifies
saved content; the generation identifies the current publication state. Rollback
publishes old content as a new revision and generation rather than decrementing them.

Each serving instance loads and compiles changes in the background. Notifications
prompt a check; polling provides a fallback. The switch is atomic within one instance,
but replicas can apply a committed change at different times. Compare Management
API `/v1/status` with each serving instance's diagnostics `/status` to distinguish
committed settings from applied settings. See [API examples](api.md#inspect-and-add-a-route).
No configuration database query is needed to forward an individual client request.

## Horizontal scaling

Kubernetes or another serving platform owns replica count, scheduling, resource
requests/limits, and autoscaling from observed request load or CPU. Routetus does
not implement its own autoscaler or operator. Serving instances have no persistent
local configuration or per-client session state; each independently compiles the
active PostgreSQL revision into RAM. Load balancing and round-robin target counters
are local to each instance, not a cluster-wide sequence.

Scale `serve` independently from `manage`. Multiple management instances use the
same generation preconditions and PostgreSQL publication lock; exactly one concurrent
write against a generation can commit. They do not coordinate replica scaling.

Use `/readyz` for traffic admission and `/healthz` for process liveness. Database
outages affect readiness according to max_staleness, not liveness. Scrape the private
metrics listener and choose autoscaling thresholds from measurements on your actual
CPU/memory limits and workloads. There is no claimed universal requests-per-replica
capacity. Ensure the platform termination grace period exceeds the configured
shutdown timeout plus any load-balancer endpoint-propagation/drain allowance.

Budget database connections for the maximum replica count, not the current count:
`serve_replicas * (max_conns + notification_session) + manage_replicas * max_conns`,
plus migration/administrative connections and the database's reserved capacity.
Per-instance concurrency limits remain necessary even when an autoscaler is active:
new replicas take time to start and must load a valid revision before receiving traffic.
Validate autoscaling, resource limits, and multi-host networking in your deployment;
the repository's local tests do not substitute for those platform checks.

## Processes and trust

`serve` reads configuration and never exposes write endpoints. `manage` authenticates
every configuration endpoint, including schemas and OpenAPI, with one environment-backed
Bearer token. The bootstrap's `management.principal` is a service identity, not proof of
which human used a shared token. Publication stores that identity, time, description,
and optional client-declared author. The token is never persisted.

Invalid or missing PostgreSQL DSNs stop startup immediately. Transient connection
failures retry until `config_sync.startup_timeout`; the final error identifies a safe
category such as authentication, DNS, refused connection, or TLS certificate failure.
CLI connection errors use the same safe network categories. Raw database/server error
text, connection URLs, and credentials are omitted.

The default management and diagnostics addresses are numeric loopback. Bind management
outside loopback only with `cert_file` and `key_file`; TLS 1.2 is the minimum. A trusted
TLS reverse proxy can reach loopback management on the same host/network namespace.
For this topology, bind `manage` to `127.0.0.1:9080` (or numeric IPv6 loopback),
leave its certificate pair unset, and have the proxy terminate TLS using a certificate
trusted by clients before forwarding to that address. The proxy and process must share a host/network
namespace; a separate container's loopback is not the management process's loopback.
Restrict local process/Pod access, preserve the Bearer authorization header, and use
a trusted certificate on the client-facing proxy. Forwarded headers do not grant
authentication or exempt a non-loopback management bind from direct TLS. For a proxy
on another host/network namespace, keep direct TLS on `manage` as well.
Keep management private; there is no browser login or user-account subsystem.

Diagnostics are a separate HTTP listener. Non-loopback binding requires
`admin.auth_token_env`, and every diagnostics endpoint then requires its Bearer token.
pprof requires that token even on loopback. Put external diagnostics behind private
network/TLS infrastructure. Do not expose profiles to untrusted callers.

Public TLS also uses `server.cert_file` and `server.key_file`, with automatic HTTP/2
negotiation. Plain public HTTP is HTTP/1.1; h2c is not supported.

## Project naming

Current source uses the `routetus` SQL schema, `routetus_config` notification channel,
`ROUTETUS_` environment prefix, and `routetus_` metric names. Demo database roles are
`routetus_manage` and `routetus_serve`; the Compose project and images use Routetus names.
This rename targets a fresh test deployment. Recreate the demo database and update
external environment references, DSNs, and metric queries; no automatic old-name
migration or compatibility aliases are supplied. Credentials remain environment values.

## PostgreSQL and permissions

The migration owner runs `migrate` explicitly. An advisory transaction lock serializes
migrations. `serve` and `manage` only check schema version and never create tables.
The schema includes revision-scoped foreign keys, binding-position uniqueness, and
triggers that reject changes to sealed revisions. Constraints supplement application
validation; only the API/compiler validates all routing and handler semantics.

Create separate migration-owner, management, and read-only serving credentials.
`examples/docker/permissions.sql` shows grants.

Management readiness on both listeners uses one background health monitor. It samples
PostgreSQL at `config_sync.poll_interval` with `postgres.query_timeout`; a failed
sample makes readiness false, and a later successful sample restores it. Detection
can take up to one polling interval plus query time; readiness probes perform no SQL.
Shutdown cancellation makes the cached state unready immediately and closes the
monitor before the pool. Management `/readyz` remains authenticated.

Management can insert complete revisions
and update publication state; it cannot alter tables or delete revision history. Do not
give it schema ownership. Supply DSNs only through the named environment variable.
Native production DSNs must use `sslmode=verify-full` and a trusted database CA.

Each serving process has at most `max_conns` pooled connections plus one dedicated
LISTEN connection. Management has `max_conns` pooled connections. LISTEN must use a
session connection: transaction-pooling PgBouncer does not preserve it. Set
`config_sync.notifications: false` to poll through transaction pooling. Every query,
pool acquisition, transaction, and LISTEN establishment has a deadline.

Publication locks the singleton state row, checks expected generation, inserts all
entities and bindings, seals the revision, updates the active pointer, increments
generation, and executes `pg_notify` inside the same transaction. Entity inserts use a single
ordered pgx batch, avoiding one network round trip per row. Batch errors are drained
and the transaction rolls back before any active-state change or notification.
Rollback revalidates old data and publishes a new immutable revision. Sequence ID gaps do not indicate
lost publications and are not used to determine ordering.

`postgres.query_timeout` bounds each complete publication or load operation, including
pool acquisition. The default 2s is not a guarantee for the structural maximum of
10000 routes/upstreams/handlers. Publication validation/compilation also consumes the
management request budget outside the SQL transaction. Size `postgres.query_timeout`,
`management.request_timeout`, client deadlines, and synchronization startup/staleness
budgets together using representative data and database latency. Queue memory grows
with revision rows within the existing entity and management-body bounds.
See [large-revision measurements](benchmarks.md#large-revision-publication).

## Synchronization and failure

One worker per instance coalesces signals. Notification setup commits LISTEN before
signaling a fresh state read; polling supplies a fallback. Reconnect uses capped
exponential backoff and jitter and signals full state reconciliation. Notifications
contain generation only. They are hints, not a durable queue.

State and rows are read in a repeatable-read transaction. Revisions never change after
publication. Compilation happens outside request handling; afterward the worker checks
the active state again and discards a superseded build. A short reader lock pins a
snapshot for a request; swapping the pointer holds the writer lock. Reference counts
retire each old transport pool only after its last request completes.

Initial serving requires a valid active revision within `startup_timeout`. No hidden
seed is performed. An empty revision deliberately published by the administrator is
valid and serves 404s. `manage` readiness requires its API and database, not routing.

Serving readiness reads background state and performs no query/Ping. The age is time
since confirmation that the database active revision matches the applied snapshot.
A successful Ping, invalid revision, or failed synchronization cannot renew that age.
After `max_staleness`, readiness and new traffic return 503. This age includes time
waiting for CPU, compilation, and reconciliation during rapid publication; a reachable
database alone does not prevent expiry. Size the budget for actual load and scheduling
delays, including before autoscaling adds replicas. Existing requests, including
streams, keep their pinned snapshot; they end naturally or at their request/shutdown
deadline. Valid reconciliation restores readiness without process restart.

`SIGHUP` requests database synchronization. It does not reload YAML. Restart to change
listeners, process limits, DSNs, or tokens. Upstream TLS PEM environment references are
resolved when a snapshot is compiled; distribute them before publishing the reference.
Restart serving processes to reliably pick up changed environment material for an
unchanged generation.

For rolling deployments, install binaries supporting the existing and proposed
contracts first. Then publish a supported `schema_version` and handler contract version.
Never publish an unsupported contract while old instances still serve. Roll back
configuration before downgrading a binary that cannot read the new contracts.

## Routing and handlers

Higher priority wins, then explicit host, longer path, and exact before prefix;
methods select among matching candidates. Prefix `/api` matches `/api` and `/api/x`,
not `/apix`. A method mismatch can fall through to a broader matching route;
`methods` selects a route and does not provide access control. Missing paths return
404; unmatched methods return 405 with `Allow` when no candidate accepts the method.
Encoded separators, dot segments, repeated slashes, and ambiguous queries are rejected.
Request paths containing a decoded `%` (including `%25` and double encodings) return
400; encoded spaces and Unicode are supported.
Forwarding trust covers every X-Forwarded-For field line and rejects ambiguous schemes;
see [proxy trust](operations.md).

Global handlers run outside route handlers and also cover 404/405. Lists record
entry order; code after `next` runs in reverse. Disabled handlers add no wrapper.
A handler definition can be reused by several routes, but each route binding has
independent state. Global bindings have their own state shared across routes; they
do not reuse a route binding's state even when the ID matches. Instance state is
local to a compiled snapshot and resets on reload. Process-wide admission limits,
such as `server.max_in_flight`, are separate and persist across snapshot reloads.
See [handler scope](extensions.md#handler-scope).

| Handler | Options |
| --- | --- |
| `request_id` | `{}`; replace untrusted IDs with a generated ID |
| `recover` | `{}`; recover before response start, preserve `http.ErrAbortHandler` |
| `access_log` | `sample_every`; includes method, excludes bodies, URLs, query strings, and auth headers |
| `headers` | Allowed request/response set/remove maps and lists |
| `powered_by` | `value`; example registered extension |

Handler bounds are tested against their schemas. `powered_by.value` permits 1–128
Unicode characters, permits HTAB, and rejects other ASCII control characters.
Discover contracts at `/v1/handler-types`. See [extensions](extensions.md).

## Routing methods and fallback

`methods` filters routing candidates; it is not an authorization rule. For example:

| Route | Path | Prefix | Methods | Priority |
| --- | --- | --- | --- | ---: |
| get-admin | /api/admin | false | GET | 10 |
| api-fallback | /api | true | any (omitted) | 0 |

GET /api/admin selects get-admin. POST /api/admin skips it and selects api-fallback.
Only when no matching candidate accepts the method does the gateway return 405 with
Allow. Protect access through an actual authorization boundary, and avoid a permissive
fallback if your backend relies on specific route coverage. This behavior is covered
by `TestMethodSelectionFallsBackToBroaderRoute`.

## Request path policy

Route paths are decoded absolute paths. They cannot contain percent characters,
dot segments, repeated slashes, or ambiguous separators. Incoming paths are decoded
once and checked before routing: `%25` becomes a literal percent and is rejected
with 400, as are double encodings such as `%2525` or `%252f`. This deliberately
excludes legitimate literal-percent path names; rename those resources or expose
them through another interface. Percent-encoded spaces and valid Unicode remain
supported. Query strings have their own validation and can contain an encoded percent.

## Request lifecycle

Routes choose targets by atomic round robin. Transports and reverse proxies are built
once per snapshot. `http.DefaultTransport` is not changed. Environment HTTP proxies are
disabled unless the upstream explicitly enables them.

The gateway does not implement retries or recreate request bodies. Go's transport may
retry a network failure on a previously used connection when it considers the request
idempotent and its body absent or replayable. Incoming server bodies have no `GetBody`;
the gateway does not add one. Do not configure an idempotency header unless the backend
contract actually supports it.

The inherited request timeout covers upstream exchange and response copy. Setting a
route's `request_timeout` to `"0s"` explicitly disables it for SSE or WebSockets. There
is no short global public `WriteTimeout`. Context cancellation stops the transport;
HTTP response-controller read/write deadlines also bound blocked socket I/O. The
deadlines remain active through the server's final flush and unread-body cleanup;
net/http resets them for the next request. Invalid requests and admission errors
(unready or overloaded instances) receive the inherited deadline too. Timeout
before response start maps to 504, another upstream failure to 502, and an observed
body-limit violation to 413. An error after response start aborts the stream rather
than appending a second error body. The client may observe connection termination
instead of a 504 when the socket deadline has already expired.

Bodies and responses stream without whole-message buffering. The body limit applies
to content-length and chunked requests. Headers are bounded by the server limit;
Go's HTTP/1 parser has a documented small read-ahead allowance. The in-flight limit
rejects excess requests with 503; there is no unbounded admission queue.

Forwarding trust defaults to empty. `Forwarded` is discarded. A trusted immediate
peer permits interpreting all X-Forwarded-For field lines in wire order, from right
to left until the first untrusted hop. X-Forwarded-Proto is accepted only as one
field line containing exactly `http` or `https`; repeated or comma-separated values
fall back to the connection scheme. Invalid IP chains fall back to the peer.
Outgoing X-Forwarded-Host uses the received Host. The gateway sets forwarding headers,
configured request headers, and request IDs in Rewrite after hop-by-hop cleanup, so a
client's Connection header cannot remove them. Targets always come from administrator
configuration; request query/header values never select arbitrary backend URLs.

## Shutdown and observability

SIGTERM/SIGINT first marks serving unready, stops synchronization and new connections,
and drains ordinary requests and upgrades until `shutdown_timeout`. Remaining public
and hijacked connections are closed, then transports and the database pool are closed.

Kubernetes endpoint removal and load-balancer propagation are asynchronous. Routetus
starts closing its public listener when it receives SIGTERM; it does not add an
application-level propagation sleep. Give the platform time to remove the terminating
Pod from traffic before sending SIGTERM. On Kubernetes 1.34 or later, merge this
Pod-spec fragment into your Deployment:

```yaml
spec:
  terminationGracePeriodSeconds: 30
  containers:
    - name: serve
      lifecycle:
        preStop:
          sleep:
            seconds: 5
```

The kubelet executes this built-in sleep hook before SIGTERM, so it works with the
scratch image without a shell or sleep executable. Kubernetes 1.30-1.33 requires the
`PodLifecycleSleepAction` feature gate, enabled by default in those versions; it is
stable from 1.34. See [lifecycle hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)
and [feature-gate history](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/).
The termination grace countdown includes both preStop and process shutdown. The 5s
propagation delay and 30s grace above are illustrative, not measured platform limits;
choose the delay for your actual load balancer, and leave time beyond that delay plus
`server.shutdown_timeout`. Validate rolling updates with real traffic on your cluster;
this repository's tests do not establish Kubernetes rollout guarantees.

Process logs use slog JSON by default. Access logs are independently enabled by
PostgreSQL handler bindings; they contain HTTP method, status, request/route IDs, duration, and size,
never body, request URL/query, authorization, cookies, or DSNs. Panic values are not
logged because they can contain secrets; recovery logs include the stack trace.

Metrics use a small standard-library Prometheus text exporter: fixed latency buckets,
status classes, configured route IDs, and process synchronization/pool gauges/counters.
Each metric family includes HELP and TYPE metadata. A panic before response headers
counts as 5xx; an aborted response keeps its written status and increments errors.
No extra metrics dependency is needed for these fixed families. Route IDs are capped
by the 10000-route configuration limit. Removed snapshots disappear from exposition;
route request counters/histograms reset on reload. Generation is a gauge, never a label.
`admin.metrics: false` hides the endpoint; operational and per-route flags control
request collection. There is no synchronous SQL or database log write per request.

## Local demo troubleshooting

The listener addresses below belong to the Docker demo. They are distinct services:

| Address | Purpose |
| --- | --- |
| `http://127.0.0.1:8080` | Public proxy traffic, including `/api` |
| `https://127.0.0.1:9080` | Management API; configuration endpoints require the management token |
| `http://127.0.0.1:9090` | Serving diagnostics; requires the admin token |

Inspect container state and recent logs from the repository root:

```sh
docker compose ps -a
docker compose logs --tail=100 postgres manage serve backend-a backend-b
```

| Symptom | What to check |
| --- | --- |
| HTTP 412, `configuration generation changed` | Another publication already advanced the generation, or initial publication was repeated. Read current status and review the configuration before submitting with a new precondition; see [route editing](api.md#inspect-and-add-a-route). |
| HTTP 401 | Use the management token on port 9080 and the admin token on port 9090; load the matching environment values into the calling shell. |
| Management certificate verification fails | Trust `.demo/tls/cert.pem` with `CURL_CA_BUNDLE` for curl or `SSL_CERT_FILE` for the native CLI. Check expiry; the generated certificate lasts seven days. |
| Public HTTP 503 or `serve` fails to start | Inspect serving status/logs for a missing valid revision, expired configuration freshness, or the in-flight request limit. See [failure behavior](#synchronization-and-failure). |
| Public HTTP 502/504 | Check the configured backend addresses, Docker service networking, backend availability, and request/transport deadlines. Successful configuration validation does not prove reachability. |
| Editing JSON has no effect | Publish through the Management API; `serve` does not watch the import file. Editing bootstrap YAML instead requires a process restart. |
| `make demo` fails on an existing database | It intentionally publishes against generation 0. Restart an initialized demo using the commands in [getting started](../getting_started.md#local-docker-demo). |

To discard the local demo database, stop its containers and delete the Compose volume:

```sh
docker compose --profile tools down --volumes --remove-orphans
```

This permanently deletes all revisions and settings in `routetus_postgres-data`.
It preserves `.env`, `.demo/tls/`, and built images. Initialize again using
[the local demo instructions](../getting_started.md#local-docker-demo) only when you
intend to start with a fresh database. Ordinary `docker compose down` preserves data.

## Limitations

- No gateway-level retry, active backend health checking, circuit breaker, or account system.
- The standard HTTP transport can retry eligible requests on reused connection failures;
  see [transport behavior](operations.md). Bodies are never replayed by gateway code.
- Routing scans sorted candidates; [benchmarks](benchmarks.md) quantify local costs.
- Revision writes are batched inside one transaction. Structural entity caps do not
  guarantee publication within the default 2s SQL budget; size timeouts for your deployment.
- Metrics counters belong to snapshots and reset on reload; historical revisions remain
  in PostgreSQL. Revision history lists the latest 100; known IDs remain readable.
- TLS secret references must exist in both management and serving environments. There is
  no secret-value import/export or built-in encryption/key distribution service.
- No production capacity, external generator saturation, multi-host deployment, or
  Windows support claim. See [executed checks](verification.md).
