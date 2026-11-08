# Release notes

## Unreleased

- Isolate release verification from write-enabled publication; publish verified artifacts only.
- Cover token, TLS, metadata, diagnostics, and revision-history boundaries with regression tests.
- Sample management database readiness in the background without SQL per probe.
- Exclude the demo backend from the default production image.
- Align schema and validator boundaries; powered_by values now count Unicode characters
  and reject all ASCII controls, including previously accepted tabs. Correct tab-containing
  values before upgrading.
- Propagate stored JSON conversion failures as safe 503 responses; return independent
  field errors with entity IDs and identify rejected revisions in runtime logs.
- Simplify strict JSON scanning while preserving its validation checks. Configuration
  parsing deliberately uses a separate presence-check parse again.
- Document the trusted TLS-proxy-to-loopback topology and rejection of percent path names.

## v0.1.0

Initial release of the Go HTTP gateway with PostgreSQL-backed configuration.

- Separate `serve` and authenticated `manage` processes, with a thin HTTP CLI.
- Immutable revisions, generation preconditions, validation, publication, and rollback.
- In-memory routing, explicit middleware registry, round robin, verified upstream TLS.
- SSE, WebSocket upgrades, trailers, bounded resources, and graceful shutdown.
- Polling and LISTEN/NOTIFY synchronization with a bounded staleness policy.
- OpenAPI, local Docker demo, getting-started instructions, and repeatable benchmarks.

Read `docs/verification.md` for executed checks and `docs/benchmarks.md` for measured
results and their limits. This first release has not been validated in production.
