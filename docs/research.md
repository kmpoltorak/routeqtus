# Implementation references

Checked against primary sources on 2026-11-08. These are API facts and design
implications, not evidence that Routeqtus passes its tests.

## Version choices

| Component | Pin | Reason and source |
| --- | --- | --- |
| Go | `1.27.3` | Current stable download; includes the October security fixes. [Downloads](https://go.dev/dl/), [release history](https://go.dev/doc/devel/release#go1.27.3). |
| PostgreSQL driver | `github.com/jackc/pgx/v5 v5.12.0` | Latest release verified in the changelog and release tag; fixes two security vulnerabilities. Use its `pgxpool` package. [Changelog](https://github.com/jackc/pgx/blob/master/CHANGELOG.md), [release](https://github.com/jackc/pgx/releases/tag/v5.12.0). |
| YAML | `go.yaml.in/yaml/v4 v4.0.0` | First stable v4 release, published November 4; the release explicitly documents this import path. [Release](https://github.com/yaml/go-yaml/releases/tag/v4.0.0). |
| OpenAPI | `3.2.1` reference | Latest published specification checked; pin the document's chosen version explicitly. [Specification](https://spec.openapis.org/oas/v3.2.1.html). |

Some cached GitHub README/tag pages still describe unreleased YAML v4 or older
pgx tags. The dated stable releases above take precedence. Dependency downloads
and `go.sum` remain the build's verification of the selected module versions.
Direct checks of the Go module proxy's `@latest` endpoint also returned
[`v5.12.0`](https://proxy.golang.org/github.com/jackc/pgx/v5/@latest) and
[`v4.0.0`](https://proxy.golang.org/go.yaml.in/yaml/v4/@latest).

## YAML and database boundaries

YAML v4 provides `NewLoader` with `WithKnownFields()` and `WithUniqueKeys()`;
unknown-field rejection is off by default and unique-key checking is on by
default. The classic `Decoder.KnownFields(true)` is also available. `Unmarshal`
reads only the first document, so bootstrap validation must explicitly reject
additional documents. [YAML API](https://pkg.go.dev/go.yaml.in/yaml/v4).

`pgxpool.Ping(ctx)` acquires a pool connection and executes an empty statement.
`Begin`/`BeginTx` cancellation does not automatically roll back a transaction;
always commit or roll back explicitly. `Query` rows must be closed to return the
connection. `Pool.Close` waits for borrowed connections to return. This makes
bounded contexts and cleanup necessary even after an operation fails.
[Pool API](https://pkg.go.dev/github.com/jackc/pgx/v5@v5.12.0/pgxpool).

PostgreSQL `jsonb` discards duplicate object keys and preserves only the last
value. Reject duplicate JSON keys before storing JSONB; they cannot be detected
after storage. [JSON types](https://www.postgresql.org/docs/current/datatype-json.html).

## Publication and synchronization

`LISTEN` takes effect at commit and belongs to the database session. On startup
and reconnect: execute and commit `LISTEN`, then inspect current configuration
in a new transaction. Notifications already represented by that initial read
are harmless duplicates. [LISTEN](https://www.postgresql.org/docs/current/sql-listen.html).

`NOTIFY` is delivered only if its transaction commits. Use parameterized
`SELECT pg_notify($1, $2)` in the same transaction as revision insertion and
generation advancement. Payloads are under 8000 bytes by default; send only a
generation signal. Identical channel/payload notifications within one
transaction may collapse. Keep the listening connection out of long
transactions because delivery waits for transaction completion.
[NOTIFY](https://www.postgresql.org/docs/current/sql-notify.html).

Design implication: notifications accelerate a database-state check; they do
not replace periodic polling, reconnect synchronization, or a monotonic
generation. Use one dedicated session connection in addition to the query pool.
PgBouncer explicitly supports `LISTEN` in session pooling and rejects it in
transaction pooling. Use polling when a session connection is unavailable.
[PgBouncer feature map](https://www.pgbouncer.org/features.html).

## Reverse proxy and request lifetime

Construct `httputil.ReverseProxy` with `Rewrite`. Hop-by-hop and forwarding
headers are removed before it runs, so install trusted outbound headers there.
`SetURL` joins target and request paths and rewrites outbound Host; assign
`Out.Host` explicitly for a configured override. `Rewrite` removes unparsable
query parameters; preserving `In.URL.RawQuery` requires prior validation to
avoid disagreement with the backend. Streaming and unknown-length responses
flush immediately. [Proxy API](https://pkg.go.dev/net/http/httputil).

The standard proxy streams with a bounded copy buffer, forwards trailers, and
uses `ResponseController` for flushing and upgrade hijacking. Response-copy
errors after headers abort the request with `http.ErrAbortHandler`; recovery
middleware must re-panic that sentinel and must not append an error document.
[Proxy source](https://go.dev/src/net/http/httputil/reverseproxy.go).

Response-writer wrappers can expose `Unwrap() http.ResponseWriter`, allowing
`ResponseController` to find flushing, hijacking and deadline support. If a
wrapper tracks hijacked connections for shutdown, it must intercept `Hijack`
itself. Read/write deadlines use zero for no deadline and cannot be extended
after expiration. Controller calls must finish before `ServeHTTP` returns.
For HTTP/1 full-duplex body streaming, `EnableFullDuplex` prevents automatic
consumption of unread request bodies before response writes; HTTP/2 permits
concurrent reads and writes already.
[Controller source](https://go.dev/src/net/http/responsecontroller.go).

Design implication: combine request-context cancellation with per-request read
and write deadlines; keep those active through response copying, reset them
before the handler returns, and let explicit streaming routes disable them.
Avoid a short listener-wide `WriteTimeout` because it cannot choose per route.
`Server.Shutdown` neither closes nor waits for hijacked WebSockets, so track and
close them separately. The transport may retry eligible idempotent requests on
reused connections after network errors, even when gateway-level retries are
absent. [HTTP API](https://pkg.go.dev/net/http).

## Management contract

JSON Merge Patch uses `application/merge-patch+json`: absent members remain,
`null` removes an object member, objects merge recursively, and arrays/scalars
replace the entire corresponding value. Duplicate-member behavior is undefined.
Apply the patch to the configured representation, then decode and validate the
complete candidate; removing a required member must fail validation. Preserve
false, zero and empty collections when encoding the configured representation.
[RFC 7396](https://www.rfc-editor.org/rfc/rfc7396.html).

`If-Match` uses strong comparison. Routeqtus's stricter contract can require one
concrete generation tag and reject wildcard and weak tags. Perform its final
generation check inside the publication transaction to prevent lost updates.
[RFC 9110, If-Match](https://www.rfc-editor.org/rfc/rfc9110.html#section-13.1.1).

OpenAPI 3.2 schemas use JSON Schema Draft 2020-12. Set explicit `type` constraints:
keywords such as `pattern` and `minimum` do not imply a type. `format` is an
annotation by default, so application validation must enforce durations, URLs
and other operational constraints. Describe request and effective-response
schemas separately where their semantics differ.
[OpenAPI Schema Object](https://spec.openapis.org/oas/v3.2.1.html#schema-object).
