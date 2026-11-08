# Routeqtus

Routeqtus is a Go HTTP API gateway with operational configuration stored in PostgreSQL.
A separate management process validates and publishes immutable revisions; serving
processes route requests using compiled in-memory snapshots. Kubernetes or another
serving platform handles replica scaling.

- No configuration SQL on the proxy request path.
- Atomic publication with generation preconditions and complete-candidate validation.
- Invalid revisions preserve the last valid snapshot; stale instances reject new traffic.
- Streaming HTTP, SSE, and WebSocket support with resource limits and graceful draining.
- Bearer-authenticated management and verified upstream TLS.

PostgreSQL 17 or later is required. See the executed checks and deployment limits
before choosing production capacity; no universal requests-per-instance guarantee is made.

## Get started

Follow [getting_started.md](getting_started.md) for installation, the Docker demo,
and native processes. Download binaries from
[GitHub Releases](https://github.com/kmpoltorak/routeqtus/releases).

## Documentation

| Document | Contents |
| --- | --- |
| [Configuration reference](docs/reference.md) | Sources, fields, defaults, constraints, bootstrap YAML |
| [Management API and CLI](docs/api.md) | Commands, endpoints, ETags, examples, errors, secrets |
| [OpenAPI contract](docs/openapi.json) | Generated machine-readable API schema |
| [Operations](docs/operations.md) | Routing, handlers, TLS/proxy trust, scaling, synchronization, shutdown, limitations |
| [Extensions](docs/extensions.md) | Registering and testing handlers |
| [Verification and development](docs/verification.md) | Make targets, test evidence, security checks, known test limits |
| [Benchmarks](docs/benchmarks.md) | Measurements, methodology, capacity limits |
| [Release notes](docs/release_notes.md) | Changes and upgrade requirements |

Built with an AI coding assistant. I wrote the spec and reviewed every change.

## License

[MIT](LICENSE).
