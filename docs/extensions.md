# Adding a handler

The contract is `type Middleware func(http.Handler) http.Handler`. A registry entry
contains the type name, short description, contract version, factory, typed options schema, and example.
The GUI handler guide uses that description and schema to explain options.
Factories decode and validate options during compilation, never during requests.
Compilation validates all definitions and creates a fresh middleware for each binding.

See `internal/extensions/powered.go` for a complete extension. Its `Register` function
is called explicitly by `app.Compiler`; no `init`, dynamic code loading, or router
modification is involved. The typed `Powered` options struct supplies the schema;
`config.DecodeTyped` rejects unknown, duplicate, missing-required, and null fields.
The factory enforces the value constraint and returns a prepared closure.

Its operational representation is already in `examples/config.json`:

```json
{"id":"powered","type":"powered_by","version":1,"options":{"value":"Routetus"}}
```

The route's `handlers: ["powered"]` binds it in entry order. Publishing through
`config apply` stores it in `handlers.options` JSONB and the revision-scoped binding
table. `GET /v1/handler-types/powered_by/schema` exposes its registered options contract
automatically. `docs/openapi.json` is regenerated with `make docs`.

Global handlers enter first, then route handlers; response-side work executes in
reverse order. Headers are applied in the correct proxy phases, including WebSocket
upgrades. Built-in `headers` forbids authentication, cookie, framing, hop-by-hop, Host,
and forwarding headers; use the dedicated Host/forwarding configuration instead.

For a new type, add its typed options/factory/registration file and call registration
from `app.Compiler`. Keep shared dependencies in constructors, never a global locator.
Do not parse options, look up factories, or reflect on types during requests. A disabled
instance is validated but contributes no wrapper. Do not log options or panic values.

Wrap HTTP writers only when necessary; implement `Unwrap()` for response-controller
compatibility and preserve flushing/hijacking. Reuse the shared writer/state where
possible. Outgoing request-header changes must run after proxy hop-by-hop cleanup;
the shared state's prepared header operations demonstrate that phase.

Tests `TestRouting`, `TestRecoverAndHandlerOrder`, `TestSSETrailersAndHTTP2`,
`TestWebSocket`, and `TestHandlerSchemaAndValidatorConformance` exercise the example,
wrapper capabilities, order, and registry schema/examples. Add observable tests for
your handler and a deliberate sabotage check before publishing its contract.

## Handler scope

A configured handler ID names reusable settings. Each attachment to a route creates
an independent runtime instance: the same limiter definition on 15 routes gives each
route its own budget. Keep mutable counters, channels, and other state inside the
factory's returned closures; do not share them through package globals.

An explicit `global_handlers` binding creates one separate instance around all routes,
including 404/405 responses. Its state is shared across those requests within that
snapshot. Binding the same ID both globally and on a route creates two independent
instances; both processing steps execute. Choose the scope deliberately.

Instances are local to one snapshot in one serving process. Reloads recreate them,
and replicas do not share counters. The process-wide `server.max_in_flight` admission
limit is separate from route handlers and remains in force across snapshot reloads.
Factories may be called multiple times while validating and building a candidate,
including for unused or disabled definitions; keep their state local and avoid
external side effects during construction.

## Use existing limits first

The shipped registry has `request_id`, `recover`, `access_log`, `headers`, and
`powered_by`. It has no configurable per-route concurrency-limiter handler.
Before writing one, check whether an existing limit fits your requirement:

| Setting | What it limits | Scope |
| --- | --- | --- |
| Bootstrap `server.max_in_flight` | Concurrent public requests; excess receives 503 | One serving process, across all routes |
| Bootstrap `management.max_in_flight` | Concurrent Management API requests; excess receives 503 | One management process |
| Operational `upstreams[].max_conns` | Transport connections to each backend target; requests may wait for capacity until their deadlines | One upstream transport in one snapshot |

These limits are different from requests per second or open incoming TCP connections.
HTTP/2 can carry several requests over one connection, and idle keep-alive sockets
have no active request. An HTTP handler cannot enforce a raw client-socket limit;
that requires listener/connection lifecycle integration or a suitable front proxy.
See [configuration fields](reference.md) and [request lifecycle](operations.md#request-lifecycle).

## Example: add a request concurrency handler

The following is a tutorial extension, not a type already registered in the released
binary. It allows a fixed number of requests through a handler instance at once and
immediately returns 503 when full. It does not queue requests, impose a requests-per-second
rate, or count raw sockets. Use it when the existing process-wide limit is too broad.

Create `internal/extensions/concurrency.go`:

```go
package extensions

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
)

const maxConcurrentRequests = 10000

type ConcurrencyOptions struct {
	Limit int `json:"limit" required:"true" doc:"Maximum simultaneous requests through this handler binding."`
}

func RegisterConcurrencyLimit(registry *middleware.Registry) error {
	schema := config.Schema(ConcurrencyOptions{})
	limit := schema["properties"].(map[string]any)["limit"].(map[string]any)
	limit["minimum"] = 1
	limit["maximum"] = maxConcurrentRequests
	return registry.Register(middleware.Type{
		Name: "concurrency_limit", Version: 1,
        Description: "Limits simultaneous requests for each route binding.",
		Schema: schema, Example: ConcurrencyOptions{Limit: 16},
		Factory: func(raw json.RawMessage) (middleware.Middleware, error) {
			var options ConcurrencyOptions
			if err := config.DecodeTyped(raw, &options); err != nil {
				return nil, err
			}
			if options.Limit < 1 || options.Limit > maxConcurrentRequests {
				return nil, fmt.Errorf("limit must be between 1 and %d", maxConcurrentRequests)
			}
			slots := make(chan struct{}, options.Limit)
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					select {
					case slots <- struct{}{}:
						defer func() { <-slots }()
						next.ServeHTTP(w, r)
					default:
						http.Error(w, "request concurrency limit reached", http.StatusServiceUnavailable)
					}
				})
			}, nil
		},
	})
}
```

The channel is created by the factory, not inside a request. The compiler invokes
the factory separately for each binding, so routes using the same handler ID have
independent capacity. The deferred release runs even when downstream panics. A slot
remains occupied until the downstream handler returns; this does not track the
lifetime of a socket handed away by a handler.

At the beginning of the existing `Register` function in `internal/extensions/powered.go`,
before its current registration, add:

```go
if err := RegisterConcurrencyLimit(r); err != nil {
    return err
}
```

`app.Compiler` and the test compiler already call `extensions.Register`; no router
change or runtime plugin loader is needed. Keep the example's schema bounds and
factory bounds in agreement. An omitted/null, non-integer, unknown, or out-of-range
option must fail configuration validation.

### Test the extension

Create `internal/extensions/concurrency_test.go` with a regression that exercises
saturation and permit release over real HTTP:

```go
package extensions

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
)

func TestConcurrencyLimitRejectsAndReleases(t *testing.T) {
	registry := middleware.NewRegistry()
	if err := RegisterConcurrencyLimit(registry); err != nil {
		t.Fatal(err)
	}
	limiter, err := registry.Build(config.Handler{
		ID: "limit", Type: "concurrency_limit", Version: 1,
		Options: []byte(`{"limit":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	server := httptest.NewServer(limiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	defer server.Close()
	defer unblock()
	client := server.Client()
	client.Timeout = 5 * time.Second
	first := make(chan error, 1)
	go func() {
		response, err := client.Get(server.URL)
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = fmt.Errorf("first request status: %d", response.StatusCode)
			}
		}
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not enter downstream")
	}
	checkStatus := func(want int) {
		t.Helper()
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("status = %d, want %d", response.StatusCode, want)
		}
	}
	checkStatus(http.StatusServiceUnavailable)
	if calls.Load() != 1 {
		t.Fatal("rejected request entered downstream")
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	checkStatus(http.StatusNoContent)
}
```

Also cover these cases before deploying:

- With limit 1, hold the first request in a real `httptest.Server` using channels;
  confirm a second request receives 503 without entering the downstream handler.
- Release the first request; a subsequent request must succeed. Exercise downstream
  cancellation and panic too, so a lost permit cannot permanently block the handler.
- Bind the same handler ID to two routes: saturating one must not reject a request
  on the other. A global binding must share capacity across routes, while global and
  route bindings of the same ID must have separate state. Verify disabled bindings
  add no wrapper.
- Reject missing/null/unknown options, wrong types, 0, negative values, and values
  above 10000. Check both accepted boundaries against the published schema.

Run `go test -race ./internal/extensions ./internal/gateway ./internal/management`,
then `make docs check test`. Sabotage the rejection path and permit release separately:
the saturation and recovery tests must fail, then pass after restoring the implementation.
`TestHandlerSchemaAndValidatorConformance` checks registered examples; keep the generated
OpenAPI and reference synchronized with the new type.

### Deploy and bind the extension

Rebuild and deploy the modified binary to management and serving processes before
publishing this type. Run `make docs` for the modified registry. Confirm the running
Management API advertises `concurrency_limit` at `/v1/handler-types` and its schema at
`/v1/handler-types/concurrency_limit/schema`. A stock binary rejects the unknown type.

Create a handler instance through `/v1/handlers` with the current generation precondition:

```json
{"id":"api-limit","type":"concurrency_limit","version":1,"options":{"limit":16}}
```

Add `api-limit` to a route's handler list, for example `["api-limit", "powered"]`,
using `/v1/routes/demo/handlers`. Preserve the other required bindings, and read a
fresh generation after each mutation. See the [API editing example](api.md#inspect-and-add-a-route)
for authentication, generation handling, and checking when a serving instance has applied
an update. Reusing `api-limit` on another route gives that route its own limit of 16;
it does not consume the first route's budget. An explicit global binding alone would
instead limit all routes together within that snapshot. Binding it globally and on
a route applies both independent budgets.

The example limit is local to one compiled snapshot in one process. Configuration
reload creates fresh state while old in-flight requests can still finish; limits
do not span old/new snapshots or replicas. It is not a strict shared backend or
cluster-wide quota. Use the existing process-wide limit for admission across reloads,
and design shared state explicitly if a quota must span replicas. Keep logging and
recovery outside the limiter if rejected requests should pass through those handlers.
