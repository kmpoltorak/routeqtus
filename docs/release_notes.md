# Routeqtus v0.1.0

Initial release of the Go HTTP gateway with PostgreSQL-backed configuration.

- Separate `serve` and authenticated `manage` processes, with a thin HTTP CLI.
- Immutable revisions, generation preconditions, validation, publication, and rollback.
- In-memory routing, explicit middleware registry, round robin, verified upstream TLS.
- SSE, WebSocket upgrades, trailers, bounded resources, and graceful shutdown.
- Polling and LISTEN/NOTIFY synchronization with a bounded staleness policy.
- OpenAPI, local Docker demo, getting-started instructions, and repeatable benchmarks.

Read `docs/verification.md` for executed checks and `docs/benchmarks.md` for measured
results and their limits. This first release has not been validated in production.
