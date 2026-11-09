# Management API

The versioned contract is [openapi.json](openapi.json); the identical document is
served at authenticated `GET /openapi.json`. Handler contracts come from the explicit
registry at `GET /v1/handler-types` and `GET /v1/handler-types/{type}/schema`.
Type metadata may include an optional `description` used by the local GUI handler guide.
OpenAPI 3.0.3 is used for broad client compatibility. Its `nullable` keyword describes
optional override values. The field coverage/default table is [reference.md](reference.md).

## CLI reference

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
| `version` | Print the executable version |

All `config` commands accept `--api` (default `http://127.0.0.1:9080`) and
`--token-env` (default `ROUTETUS_MANAGEMENT_TOKEN`). Remote API URLs require HTTPS.
`apply` and `rollback` accept `--description` and `--author`; the author is a client
claim, separate from the token's authenticated service principal.

## Requests and publication

Send `Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN` on every configuration request.
For mutations, send the strong ETag of the complete configuration, e.g.
`If-Match: "g-0"` for the first publication. GET responses concerning configured
entities use the same generation ETag; editing a different resource invalidates it.
Wildcards and weak tags are rejected. Missing precondition is 428; stale is 412.

POST creates a resource, PUT replaces it, and PATCH applies JSON Merge Patch.
PATCH omission preserves a field; null removes it; required-field removal fails.
Arrays replace completely. A false metrics flag differs from omission; `"0s"`
explicitly disables an operational deadline, while null inherits a default.
Resource IDs cannot change through a single-resource URL. Use full configuration
replacement for coordinated renames and changes to multiple dependencies.

Every change is compiled and validated as a complete candidate before any write.
Validation does not connect to backends. A successful publication is:

```json
{"revision_id":1,"generation":1,"status":"committed"}
```

That acknowledges PostgreSQL commit, not cluster-wide application. Inspect each
serve instance's protected `/status` for desired/applied generations. Use request
headers `X-Revision-Description` and `X-Revision-Author` for declared metadata; the
authenticated principal comes only from the server's bootstrap.

## Endpoints

| Endpoint | Methods | Representation |
| --- | --- | --- |
| `/v1/config` | GET, PUT, PATCH | Complete configured snapshot; atomic batch |
| `/v1/config/effective` | GET | Resolved defaults for inspection |
| `/v1/config/validate` | POST | Dry-run full configuration; no write/NOTIFY |
| `/v1/settings` | GET, PUT, PATCH | trusted_proxies and metrics |
| `/v1/routes`, `/v1/upstreams`, `/v1/handlers` | GET, POST | Collections |
| `/v1/routes/{id}`, `/v1/upstreams/{id}`, `/v1/handlers/{id}` | GET, PUT, PATCH, DELETE | One resource |
| `/v1/upstreams/{id}/targets` | GET, POST | Ordered targets |
| `/v1/upstreams/{id}/targets/{target_id}` | GET, PUT, PATCH, DELETE | One target |
| `/v1/global-handlers`, `/v1/routes/{id}/handlers` | GET, PUT, PATCH | Handler ID lists in entry order |
| `/v1/revisions` | GET | Latest 100 revision metadata records |
| `/v1/revisions/{revision_id}` | GET | Historical configured snapshot |
| `/v1/rollback` | POST | `{ "revision_id": N }`; revalidate/publish with greater generation |
| `/v1/status` | GET | Committed revision and generation |
| `/v1/handler-types`, `/v1/handler-types/{type}/schema` | GET | Supported versioned contracts |
| `/openapi.json` | GET | Generated OpenAPI |
| `/healthz` | GET | Unauthenticated management liveness |
| `/readyz` | GET | Authenticated cached management/database readiness |

## curl and CLI examples

After starting `manage` against a fresh migrated database:

```sh
export API=http://127.0.0.1:9080
curl --fail-with-body -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" "$API/v1/handler-types"
curl --fail-with-body -X PUT -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" \
  -H 'If-Match: "g-0"' -H 'Content-Type: application/json' \
  --data-binary @examples/config.json "$API/v1/config"
```

`examples/api.sh` is executable through `sh` and demonstrates schema reads, complete
publication, creating an upstream and route, PATCH/false/null, effective GET, dry-run,
422 validation failure, 412 ETag conflict, and rollback. It requires curl and jq and
publishes demonstration configuration; run only against a disposable environment.

For the Docker TLS demo:

```sh
set -a; . ./.env; set +a
export API=https://127.0.0.1:9080
export CURL_CA_BUNDLE="$PWD/.demo/tls/cert.pem"
sh examples/api.sh
```

Equivalent thin-client commands:

```sh
bin/routetus config status --api "$API"
bin/routetus config validate --api "$API" --file examples/config.json
bin/routetus config apply --api "$API" --file examples/config.json --generation N --description 'Replace configuration'
bin/routetus config rollback --api "$API" --revision ID --generation N
```

Replace `N`/`ID` with observed values. Native CLI TLS trust uses standard Go system
roots or `SSL_CERT_FILE`; curl uses `CURL_CA_BUNDLE`. Redirects are not followed by the
CLI. The `config` subcommands use HTTP and never connect to PostgreSQL. The `serve`,
`manage`, and `migrate` process commands use their configured database credentials.

## Inspect and add a route

This example assumes an initialized local Docker demo, curl and jq, and an unused
route ID `hello`. It adds a real route to the demo's current configuration.

Load the demo credentials and TLS trust, then define a helper for authenticated reads:

```sh
set -a
. ./.env
set +a
export API=https://127.0.0.1:9080
export CURL_CA_BUNDLE="$PWD/.demo/tls/cert.pem"
api() {
  curl --fail-with-body --silent --show-error \
    -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" "$API$1"
}
```

Inspect a route, the upstream it selects, and a handler bound to it:

```sh
api /v1/routes/demo | jq
api /v1/upstreams/demo | jq
api /v1/handlers/powered | jq
api /v1/handler-types/powered_by/schema | jq
```

The handler ID `powered` names a configured instance; `powered_by` names its registered
type. Its schema explains which options the instance accepts. Use `api /v1/config`
for configured values or `api /v1/config/effective` to inspect resolved defaults;
export configured values when preparing a document for re-import.

Read the current generation and create an exact `GET /hello` route that uses the
existing demo upstream and header handler:

```sh
GEN=$(api /v1/status | jq -er '.generation')
curl --fail-with-body --silent --show-error -X POST \
  -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" \
  -H "If-Match: \"g-$GEN\"" \
  -H 'Content-Type: application/json' \
  --data '{"id":"hello","methods":["GET"],"path":"/hello","prefix":false,"strip_prefix":false,"upstream":"demo","handlers":["powered"]}' \
  "$API/v1/routes"
api /v1/routes/hello | jq
```

A successful write is visible in Management API reads after commit. The public route
works after that serving instance applies the revision; it is not a synchronized
cluster-wide switch. Read serving diagnostics to check its applied generation:

```sh
curl --fail-with-body --silent --show-error \
  -H "Authorization: Bearer $ROUTETUS_ADMIN_TOKEN" \
  http://127.0.0.1:9090/status | jq
```

Once `applied_generation` matches the committed `generation`, test the public route:

```sh
curl --fail-with-body --include http://127.0.0.1:8080/hello
```

With `strip_prefix: false`, the backend receives `/hello`. The demo backend responds
to that path, but your own backend must implement it. If creation returns 412, another
change advanced the generation: read the current state, review the intended edit,
and obtain a fresh generation before resubmitting. A 409 means `hello` already exists;
inspect it before deciding whether to edit it or use another ID.

## Errors and secrets

```json
{"code":"invalid_configuration","message":"configuration rejected","request_id":"...","fields":[{"path":"/routes/0/upstream","message":"unknown upstream","route_id":"hello","upstream_id":"missing"}]}
```

| HTTP | Meaning |
| --- | --- |
| 400 | Invalid JSON, duplicate keys, or unreadable body |
| 401 | Missing/incorrect token |
| 404 | Missing entity, revision, or schema |
| 405 | Unsupported endpoint method |
| 409 | Duplicate resource creation |
| 412 | Concrete generation precondition failed |
| 413 | Management document exceeds size limit |
| 422 | Unsupported/unknown fields, invalid type/range/reference, required-field removal, routing/handler conflict |
| 428 | Mutation omitted If-Match |
| 503 | Database unavailable, stored configuration conversion failed, or concurrency exhausted |

Field paths remain JSON Pointers. Valid known identifiers appear as optional
`route_id`, `upstream_id`, and `handler_id`; absent or malformed IDs are omitted.
Compilation reports independent entity failures from the first invalid phase
(upstreams, then handlers, then routes). It stops before dependent phases to avoid
cascading reference errors; this is not an exhaustive list of every bad field.
Compiler reports retain at most 100 field errors and use an 8 KiB text budget;
individual messages longer than 256 bytes end with `...` on a UTF-8 boundary.
A final ordinary field error, such as `{"path":"/","message":"9900 more errors omitted"}`,
counts errors omitted by either the number or byte budget. No Problem schema fields
are added. The same report reaches runtime logs and the diagnostics status error.
Stored JSON conversion failures return 503 and cannot publish a partial candidate.
Runtime rejection logs include the known `revision_id` and entity context; repeated
identical errors in different revisions are logged separately.

Storage errors do not return SQL, parameters, or DSNs. Operational secrets are only
environment references; no API field accepts a private-key value, database password,
or upstream authorization secret. Thus exports contain reference names and no masked
secret-value placeholder. A reference such as `***` is rejected rather than persisted
as new secret material. Keep referenced values available to management validation and
all serving instances before publication. Management tokens are bootstrap secrets and
never appear in configuration exports, revision metadata, schemas, or logs.
