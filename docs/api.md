# Management API

The versioned contract is [openapi.json](openapi.json); the identical document is
served at authenticated `GET /openapi.json`. Handler contracts come from the explicit
registry at `GET /v1/handler-types` and `GET /v1/handler-types/{type}/schema`.
OpenAPI 3.0.3 is used for broad client compatibility. Its `nullable` keyword describes
optional override values. The field coverage/default table is [reference.md](reference.md).

## Requests and publication

Send `Authorization: Bearer $ROUTEQTUS_MANAGEMENT_TOKEN` on every configuration request.
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
curl --fail-with-body -H "Authorization: Bearer $ROUTEQTUS_MANAGEMENT_TOKEN" "$API/v1/handler-types"
curl --fail-with-body -X PUT -H "Authorization: Bearer $ROUTEQTUS_MANAGEMENT_TOKEN" \
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
bin/routeqtus config status --api "$API"
bin/routeqtus config validate --api "$API" --file examples/config.json
bin/routeqtus config apply --api "$API" --file examples/config.json --generation N --description 'Replace configuration'
bin/routeqtus config rollback --api "$API" --revision ID --generation N
```

Replace `N`/`ID` with observed values. Native CLI TLS trust uses standard Go system
roots or `SSL_CERT_FILE`; curl uses `CURL_CA_BUNDLE`. Redirects are not followed by the
CLI. It never writes to PostgreSQL directly; only `migrate` uses a direct database path.

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
