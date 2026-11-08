# Adding a handler

The contract is `type Middleware func(http.Handler) http.Handler`. A registry entry
contains the type name, contract version, factory, typed options schema, and example.
Its factory decodes and validates options once while the revision is compiled.

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
