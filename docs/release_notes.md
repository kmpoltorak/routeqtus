# Release notes

## Unreleased

- Give each route handler binding independent runtime state, even when routes reuse
  one handler ID. Explicit global bindings retain their own shared state across routes.
- Stateful extensions and route-local access-log sampling no longer share counters
  between routes. Factories run during validation and separately for each binding;
  keep mutable state local to the factory result. Instance state still resets on
  configuration reload and remains local to each serving process.
- Process-wide admission limits remain independent of route handlers.

## v0.2.0 — 2026-11-09

- Rename the module, CLI, images, archive names, metrics, SQL namespace, notification
  channel, demo database/roles, and environment prefixes to Routetus.
- This rename targets a fresh test deployment. Recreate the demo database; no old-name
  compatibility aliases or automatic data migration are provided.
- Release archives are named `routetus_v0.2.0_<os>_<arch>.tar.gz` and contain the
  `routetus` executable. Linux and macOS builds are provided for amd64 and arm64.
- This is a breaking rename from v0.1.1: update commands, environment variables,
  metric queries, and database provisioning before starting the new version.
  Historical releases retain their original artifact and executable names.

## v0.1.1 — 2026-11-08

This patch release includes the fixes merged after v0.1.0, including the final
validation-report and legacy-header compatibility regressions.

- Reject spoofed multiline X-Forwarded-For chains and ambiguous X-Forwarded-Proto values.
- Count unwritten panics as 5xx in metrics/access logs; emit metric HELP/TYPE metadata.
- Report safe startup/CLI connection failure categories without exposing credentials.
- Batch revision inserts in one atomic transaction; preserve rollback and notifications.
- Identify the two demo backend instances and clarify method fallback and Kubernetes draining.
- Isolate release verification from write-enabled publication; publish verified artifacts only.
- Cover token, TLS, metadata, diagnostics, and revision-history boundaries with regression tests.
- Sample management database readiness in the background without SQL per probe.
- Exclude the demo backend from the default production image.
- Align schema and validator boundaries; powered_by values count Unicode characters
  and retain v0.1.0 HTAB compatibility while rejecting other invalid ASCII controls.
- Propagate stored JSON conversion failures as safe 503 responses; return independent
  field errors with entity IDs and identify rejected revisions in runtime logs.
- Bound compiler errors in API responses, runtime status, and logs without adding
  Problem schema fields.
- Simplify strict JSON scanning while preserving its validation checks. Configuration
  parsing deliberately uses a separate presence-check parse again.
- Document the trusted TLS-proxy-to-loopback topology and rejection of percent path names.

### Upgrade notes

- Validation error objects retain their JSON Pointer and may add `route_id`,
  `upstream_id`, or `handler_id`. Clients must accept these optional fields.
- Compiler reports retain at most 100 field errors plus an ordinary root field with
  the omitted count. Messages are capped at 256 bytes, and formatted reports at 8 KiB.

See [verification](verification.md) for executed checks and [benchmarks](benchmarks.md)
for measurements and their limits. No production or Kubernetes rollout is certified.

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
