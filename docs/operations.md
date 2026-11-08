# Operations

## Horizontal scaling

Kubernetes or another serving platform owns replica count, scheduling, resource
requests/limits, and autoscaling from observed request load or CPU. Routeqtus does
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
Keep management private; there is no browser login or user-account subsystem.

Diagnostics are a separate HTTP listener. Non-loopback binding requires
`admin.auth_token_env`, and every diagnostics endpoint then requires its Bearer token.
pprof requires that token even on loopback. Put external diagnostics behind private
network/TLS infrastructure. Do not expose profiles to untrusted callers.

Public TLS also uses `server.cert_file` and `server.key_file`, with automatic HTTP/2
negotiation. Plain public HTTP is HTTP/1.1; h2c is not supported.

## PostgreSQL and permissions

The migration owner runs `migrate` explicitly. An advisory transaction lock serializes
migrations. `serve` and `manage` only check schema version and never create tables.
The schema includes revision-scoped foreign keys, binding-position uniqueness, and
triggers that reject changes to sealed revisions. Constraints supplement application
validation; only the API/compiler validates all routing and handler semantics.

Create separate migration-owner, management, and read-only serving credentials.
`examples/docker/permissions.sql` shows grants. Management can insert complete revisions
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
generation, and executes `pg_notify` inside the same transaction. Rollback revalidates
old data and publishes a new immutable revision. Sequence ID gaps do not indicate
lost publications and are not used to determine ordering.

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
After `max_staleness`, readiness and new traffic return 503. Existing requests, including
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
