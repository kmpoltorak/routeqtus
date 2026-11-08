#!/bin/sh
# This example publishes configuration. Use a disposable local demonstration database.
set -eu
: "${ROUTETUS_MANAGEMENT_TOKEN:?Provide the management bearer token}"
API=${API:-http://127.0.0.1:9080}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
get() { curl --fail-with-body --silent --show-error -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" "$API$1"; }
etag() { get /v1/status | jq -r '"\"g-\(.generation)\""'; }
mutate() {
  method=$1; path=$2; body=$3
  curl --fail-with-body --silent --show-error -X "$method" \
    -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" \
    -H "If-Match: $(etag)" -H 'Content-Type: application/merge-patch+json' \
    --data-binary "$body" "$API$path"
}
get /v1/handler-types
get /v1/handler-types/headers/schema
initial=$(mutate PUT /v1/config "$(cat examples/config.json)")
revision=$(printf '%s' "$initial" | jq -r .revision_id)
mutate POST /v1/upstreams '{"id":"extra","targets":[{"id":"a","url":"http://backend-a:8081"}]}'
mutate POST /v1/routes '{"id":"extra","path":"/extra","upstream":"extra","handlers":[]}'
mutate PATCH /v1/routes/extra '{"request_timeout":"0s","metrics":false}'
mutate PATCH /v1/routes/extra '{"request_timeout":null,"metrics":null}'
get /v1/config/effective
curl --fail-with-body --silent --show-error -X POST \
  -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" -H 'Content-Type: application/json' \
  --data-binary @examples/config.json "$API/v1/config/validate"
bad=$(jq '.routes[0].upstream="missing"' examples/config.json)
code=$(curl --silent --show-error -o "$tmp/error.json" -w '%{http_code}' -X POST \
  -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" -H 'Content-Type: application/json' \
  --data-binary "$bad" "$API/v1/config/validate")
test "$code" = 422
cat "$tmp/error.json"
code=$(curl --silent --show-error -o "$tmp/conflict.json" -w '%{http_code}' -X PUT \
  -H "Authorization: Bearer $ROUTETUS_MANAGEMENT_TOKEN" -H 'If-Match: "g-0"' \
  -H 'Content-Type: application/json' --data-binary @examples/config.json "$API/v1/config")
test "$code" = 412
cat "$tmp/conflict.json"
mutate POST /v1/rollback "{\"revision_id\":$revision}"
