# Route handlers

Handlers are implemented and registered in Go. The Management API creates configured
instances of those types; the local GUI only attaches existing route-specific instances.
It does not create handler code or configurations. The GUI guide omits global bindings.

## Available route policies

| Type | Purpose | Example options | Rejection |
| --- | --- | --- | --- |
| `headers` | Set/remove permitted request or response headers | `{"response_set":{"X-Service":"example"}}` | Invalid configuration is rejected |
| `concurrency_limit` | Bound active requests on this route, without a queue | `{"limit":16}` | 503 |
| `rate_limit` | Token bucket shared by requests on this route | `{"requests_per_second":100,"burst":20}` | 429 with `Retry-After: 1` |
| `token_allowlist` | Require an allowed Bearer token | `{"token_envs":["ROUTETUS_ROUTE_TOKEN"]}` | 401 with a Bearer challenge |
| `body_limit` | Bound request body bytes, including chunked uploads | `{"max_bytes":1048576}` | 413 before response start |

`headers` already supports custom headers; no separate header type is needed.
It forbids authentication, cookie, framing, hop-by-hop, Host, and forwarding headers.
Use route `host_header` and forwarding configuration for their dedicated purposes.
Header values permit interior HTAB and reject other ASCII controls; their byte bound
comes from bootstrap `limits.max_header_value_bytes`.

Concurrency counts active HTTP requests, not incoming TCP sockets. HTTP/2 can carry
multiple requests on one connection. SSE and WebSockets occupy a permit until their
proxy operation ends. Permits are released on return, cancellation, or panic.

The rate bucket starts full, refills continuously, and never exceeds `burst`.
It is per route, not per IP or token. Denied requests do not wait in a queue.
`Retry-After: 1` is a conservative retry hint, not a reservation of future capacity.

Body limits stream data and do not buffer entire uploads. The lower of the process
and attached handler limits applies. A backend may receive part of an oversized chunked
upload before rejection; if a response has started, the gateway aborts the stream
instead of replacing it with a second status. This is a byte limit, not decompression.

## Defaults and bootstrap ceilings

Without an attached route policy, the process and upstream defaults still apply:

| Setting | Default | Meaning |
| --- | --- | --- |
| `server.max_in_flight` | 2048 | Active public requests across all routes in one process |
| `server.max_body_bytes` | 16777216 (16 MiB) | Request body limit across all routes; supported maximum is 1 GiB |
| `server.request_timeout` | 15s | Total request deadline, inherited unless the route overrides it |
| `upstreams[].dial_timeout` | 3s | Establishing a new backend TCP connection |
| `upstreams[].tls_handshake_timeout` | 5s | Backend TLS handshake |
| `upstreams[].response_header_timeout` | 10s | Waiting for backend response headers after sending the request |
| `upstreams[].max_conns` | 128 | Transport connections per backend target |

There is no implicit per-route rate limit or token requirement. Attaching a disabled
handler adds no wrapper. A route body limit cannot increase `server.max_body_bytes`;
process-wide admission remains in force even if a route concurrency limit is higher.

The bootstrap `limits` section bounds allowed policy settings, not live traffic.
For example, `limits.max_concurrent_requests: 1000` permits a configured handler limit
up to 1000; it does not automatically attach a 1000-request limiter to every route.
Use identical ceilings on `manage` and every `serve` process. Lower them only after
the active configuration fits. See [all fields and ranges](reference.md).

Do not create timeout handlers that duplicate existing settings. Use
`routes[].request_timeout` for the whole exchange and response copy, and the upstream
fields above for connection/header phases. A header timeout does not bound the response
body. Explicit route `request_timeout: "0s"` allows long streams; ordinary requests
should retain a finite deadline. These fields are configured through the API.

## Configure and attach a policy

For example, create this handler through `POST /v1/handlers`, with the management
Bearer token, JSON content type, and current `If-Match` generation:

```json
{"id":"api-concurrency","type":"concurrency_limit","version":1,"options":{"limit":16}}
```

Then include `api-concurrency` in the route's `handlers` list through the API or select
it in the local GUI. The registered schema is available at
`GET /v1/handler-types/concurrency_limit/schema`; new types also appear automatically
in OpenAPI. A useful entry order is:

```json
["api-auth", "api-rate", "api-concurrency", "api-body", "route-headers"]
```

Create those definitions before binding them. Authentication runs first, so denied
credentials do not consume the route's rate budget. Place headers earlier if they
must also affect policy rejection responses. Global request IDs, recovery, and access
logging are outside this list and are described in [operations](operations.md).

## Token allowlist

Send exactly one `Authorization: Bearer <token>` header over HTTPS, directly or through
your trusted TLS terminator. Tokens in query parameters are not accepted as credentials.
The gateway consumes the route token and removes Authorization before forwarding, so
backend services do not receive this credential. Avoid stacking multiple token policies
on one request; the first consumes the header.

Only environment names appear in operational JSON, PostgreSQL revisions, API responses,
and GUI data. Token values must be provided to both `manage` and `serve` through their
environment, for example from Kubernetes Secrets or an explicit Compose environment
mapping. Adding a token to the host `.env` alone does not inject it into containers.
Use route-specific tokens; do not reuse the Management API or diagnostics tokens.

Each referenced value must be a 32-byte or longer opaque token using the Bearer token
alphabet; maximum length/reference count comes from bootstrap. Empty lists, missing
values, duplicate references, invalid values, and unknown options reject compilation.
This validation also applies to unused or disabled definitions. Disabling a valid
instance bypasses authentication; an invalid secret never silently opens a route.

Factories hash tokens once. Requests compare fixed-size hashes against the complete
allowlist using constant-time comparisons. No raw token values are logged, included
in validation errors, or returned in error responses. Logs record request ID, route ID,
method, status, duration, and size, excluding headers, query strings, and bodies.
Do not use verbose HTTP traces or print environment values when diagnosing credentials.

Changing a process environment requires recreating that process. A configuration
recompile loads its current environment; old in-flight requests retain their original
snapshot. To rotate a token, provision an additional reference in all processes,
publish a list containing both references, move clients, then remove the old reference.

## Handler scope

A configured ID names reusable settings. Each route attachment creates fresh state:
the same concurrency definition on 15 routes gives each route its own capacity.
Rate buckets are independent too. State lives in the factory closure, not a package global.

An explicit `global_handlers` binding creates a separate shared instance around all
routes and 404/405 responses. Binding the same ID globally and locally executes both
policies. The GUI omits global definitions; use the API deliberately for that scope.

Instances belong to one snapshot in one process. Reloads reset counters/buckets while
old requests may still finish. Replicas do not share quotas. Use a separate distributed
service if a cluster-wide quota is required; `server.max_in_flight` continues to protect
one process across reloads. Factories also run during definition validation, including
unused/disabled definitions, so construction must have no external side effects.

## Add a new handler in code

The contract is `type Middleware func(http.Handler) http.Handler`. For a working
example, read `internal/extensions/concurrency.go`; registration is explicit in
`internal/extensions/register.go`, called by `app.Compiler`.

1. Add a file in `internal/extensions/` with typed options, JSON/doc/required tags,
   and a registration function. Decode with `config.DecodeTyped` to reject unknown,
   duplicate, missing-required, and null fields.
2. Derive its schema with `config.Schema`, adding bounds from bootstrap where needed.
   Register a unique name, description, contract version, schema, example, and factory.
3. Validate once in the factory and return prepared middleware. Keep mutable state
   inside the factory so bindings stay independent; avoid SQL, reflection, or option
   parsing on the request path.
4. Add registration to the existing list. No router changes, `init`, or runtime code
   loading are needed. Rebuild every management/serving binary that uses the type.
5. Test public HTTP behavior, malformed options, disabled instances, parallel requests,
   per-route isolation, and cleanup on cancellation/panic. Deliberately break a guard
   and confirm its test fails, restore it, then run `make check test test-race docs`.
6. Create an instance through the API and attach its ID to a route. Keep the GUI as
   an attachment tool, and update this guide and the release notes.

Wrap writers only when necessary; provide `Unwrap()` and preserve flushing/hijacking.
Use shared writer/state operations for headers and `middleware.LimitBody` for body
limits, so early rejections, streaming errors, and proxy errors remain consistent.
