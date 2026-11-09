# Getting started

Routetus needs PostgreSQL and an explicitly published revision before `serve`
accepts traffic. `manage` can start with an empty, migrated database.

See [release notes](docs/release_notes.md#v020--2026-11-09) for the breaking rename
in v0.2.0. Use a fresh demo database and the `ROUTETUS_` environment prefix; old-name
compatibility aliases and automatic data migration are not provided. Existing
`powered_by.value` options containing HTAB remain valid.

## Install

For a prebuilt binary, download the archive for your OS/architecture and `checksums.txt`
from [GitHub Releases](https://github.com/kmpoltorak/routetus/releases). Starting with
v0.2.0, archives use `routetus_v0.2.0_<os>_<arch>.tar.gz` and contain `routetus`. Verify its
SHA-256 checksum before extracting it. Archives contain the binary, examples, and
public documentation; they do not include the source Makefile. Check the version:

```sh
./routetus version
```

The Docker demo and source-build commands below use a Git checkout. PostgreSQL 17
or later is required. Native source builds use Go 1.27.3 and make. The production
container includes CA certificates, runs without root, and excludes the demo backend.

## Local Docker demo

Install Docker with Compose, OpenSSL, curl, and make. Run as a non-root user:

```sh
git clone https://github.com/kmpoltorak/routetus.git
cd routetus
make demo
curl --fail http://127.0.0.1:8080/api
```

Example output (the backend name alternates between `backend-a` and `backend-b`):

```text
routetus demo backend: backend-a
```

`make demo` generates random credentials in ignored `.env` and a seven-day local
TLS certificate in ignored `.demo/tls`. It builds separate production and demo images,
starts PostgreSQL and two demo targets, runs migrations, grants separate roles, starts
`manage`, publishes `examples/config.json` against generation 0, and starts `serve`.

This initialization command deliberately fails on an already initialized database
instead of overwriting its configuration. To restart existing processes, use
`docker compose up -d postgres backend-a backend-b manage serve`. Recreate the demo
certificate with OpenSSL when it expires; certificate changes require process restart.

The PostgreSQL connection uses `sslmode=disable` only inside this local demonstration.
Management uses TLS; host ports bind to loopback. These generated credentials and
the demo CA are for local testing, not production deployment.

Inspect management using the native binary:

```sh
make build
set -a
. ./.env
set +a
export SSL_CERT_FILE="$PWD/.demo/tls/cert.pem"
bin/routetus config status --api https://127.0.0.1:9080
bin/routetus config show --api https://127.0.0.1:9080 > configured.json
bin/routetus config validate --api https://127.0.0.1:9080 --file configured.json
bin/routetus config show --api https://127.0.0.1:9080 --effective
```

For diagnostics:

```sh
curl --fail -H "Authorization: Bearer $ROUTETUS_ADMIN_TOKEN" http://127.0.0.1:9090/readyz
curl --fail -H "Authorization: Bearer $ROUTETUS_ADMIN_TOKEN" http://127.0.0.1:9090/status
curl --fail -H "Authorization: Bearer $ROUTETUS_ADMIN_TOKEN" http://127.0.0.1:9090/metrics
```

Stop without deleting PostgreSQL data: `docker compose down`.
Deleting the `postgres-data` volume removes all revisions; do that only when you
intend to discard this local demo's configuration.

## Local route GUI

After initializing the Docker demo, start the optional, separate GUI container:

```sh
docker compose --profile ui up -d --build ui
```

Open **http://127.0.0.1:9180**. The panel lists routes and lets you add or delete a route,
register an upstream with backend URLs, and create a configured handler from a type
registered by the server. Handler options use typed fields derived from its schema;
structured map/array options use JSON fields. The top-bar **Handler guide** explains
the available types and their options.

It has no login screen; the Management API token stays in the GUI container.
Configuration conflicts refresh the list and preserve your form for review and retry.
Creating a handler gives its settings an ID; selecting that ID attaches it to the
route. Create a separate ID when a route needs different option values. New handler
types still require code registration and a binary deployment.

The `ui` profile builds its own image from this checkout. It is not part of the
production gateway image or the gateway release archive. No Node.js installation is
needed to run it. Stop just the panel with `docker compose --profile ui stop ui`;
stop all demo services without deleting data with `docker compose --profile ui down`.

For native development, build the separate executable with `make build-ui`, load
the demo environment as shown above, and run:

```sh
SSL_CERT_FILE="$PWD/.demo/tls/cert.pem" bin/routetus-ui --api https://127.0.0.1:9080
```

See [GUI operation and trust](docs/operations.md#local-gui) and
[browser checks](docs/verification.md#local-gui-checks) for details.

## Native processes

Use a source checkout, or the examples from an extracted release archive. For source:

```sh
git clone https://github.com/kmpoltorak/routetus.git
cd routetus
make build
```

When using a release archive, skip the build and replace `bin/routetus` in the
following commands with `./routetus`. Check the bootstrap offline:

```sh
bin/routetus check --config examples/bootstrap.yaml
```

Supply `DATABASE_URL` securely for the migration owner, then run:

```sh
bin/routetus migrate --config examples/bootstrap.yaml
```

Create separate login roles through your PostgreSQL administration tooling. Apply
the grants in `examples/docker/permissions.sql` after creating `routetus_serve`
and `routetus_manage`. The migrator owns the schema; the request-serving role has
SELECT only. Use distinct bootstrap files with distinct `dsn_env` references, or
separate process environments. Production DSNs must verify the database certificate:

```text
postgres://routetus_serve@db.example.com:5432/routetus?sslmode=verify-full&sslrootcert=/run/secrets/postgres-ca.pem
```

Provide the complete DSN, including authentication, through the named environment
variable. Never put it in YAML or command arguments.

The default admin listener is `127.0.0.1:9090`; two native processes on the same host
need different `admin.listen` addresses. Serve does not require the management token.
Use separate bootstrap copies and change the management process admin address before starting both processes, for example:

```sh
mkdir -p .local/native
cp examples/bootstrap.yaml .local/native/manage.yaml
cp examples/bootstrap.yaml .local/native/serve.yaml
```

In `.local/native/manage.yaml`, set `admin.listen` to `127.0.0.1:9091`; leave
`.local/native/serve.yaml` at `127.0.0.1:9090`. In each file, select the corresponding
role's DSN environment reference.

Start `manage` with its own DSN
and a random Bearer token of at least 32 bytes in `ROUTETUS_MANAGEMENT_TOKEN`:

```sh
bin/routetus manage --config .local/native/manage.yaml
```

In another terminal with the same token, publish your configuration through HTTP:

```sh
bin/routetus config validate --file examples/config.json
bin/routetus config apply --file examples/config.json --generation 0
```

The example targets are Docker service names. For native testing, replace them
with addresses of your own local backends before publishing. Validation checks
structure, not backend reachability.

With the read-only DSN in its environment, start the separate request-serving process:

```sh
bin/routetus serve --config .local/native/serve.yaml
```

For a TLS reverse proxy on the same host/network namespace, keep management bound
to numeric loopback; see the [proxy trust boundary](docs/operations.md#processes-and-trust).
Outside numeric loopback, management must terminate TLS itself.

See [the API examples](docs/api.md), [configuration reference](docs/reference.md),
[operations](docs/operations.md), and [extension guide](docs/extensions.md).
