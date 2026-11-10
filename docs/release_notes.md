# Release notes

## Unreleased

- Add optional `service_id` metadata to routes and upstreams, including GUI creation,
  editing, display, and search. It preserves case and accepts ASCII letters, digits,
  underscores, and hyphens; it does not change routing or enforce relationships.
- Database schema version 3 adds the two metadata columns. Stop old processes, run
  `routetus migrate`, then start all new processes; old binaries refuse the new schema.
  Existing revisions remain readable with empty Service IDs.

- Add per-route concurrency, rate, token allowlist, and request body policies; custom
  headers use the existing `headers` type. Rate/concurrency state is independent per
  route binding and serving replica; reloads recreate it.
- Move policy ceilings and the custom-header byte bound into bootstrap `limits`.
  Process-wide defaults still apply without attached policies. The maximum supported
  process/route body bound is now 1 GiB; its process default remains 16 MiB.
- Token configurations contain environment references only. Missing/invalid secrets
  reject compilation; accepted route tokens are consumed before forwarding, and token
  values never appear in request logs or validation errors.
- Remove the test-only `powered_by` type and `limits.max_powered_characters`. Convert
  active definitions to `headers.response_set` before upgrading, remove the obsolete
  bootstrap key, and avoid rolling back to old revisions containing the removed type.
  See [migration instructions](../getting_started.md#replace-the-test-powered_by-handler).
- Use neutral graphite surfaces for the GUI dark appearance.

## v0.3.0 — 2026-11-10

- Upgrade from v0.2.0: run `routetus migrate`, then restart every `serve` and `manage`
  process on v0.3.0. Release archives are named `routetus_v0.3.0_<os>_<arch>.tar.gz`.
  The optional GUI is not in the archives or gateway image; build it from a source
  checkout (see getting_started.md, Local route GUI).

- Configuration limits move from code constants to the bootstrap `limits` section:
  `max_entities`, `max_targets`, `max_connections`, `max_priority`, `max_sample_every`,
  `max_powered_characters`, and `max_identifier_length`. Defaults are unchanged; each
  accepts up to 10 times its default. The served OpenAPI document reports the process
  limits. Use identical `limits` on every `serve` and `manage` process.
- Database schema version 2 drops the PostgreSQL upper bound on upstream connection
  columns (they still must be at least 0); `limits.max_connections` validates the upper
  bound. Upgrade: run `routetus migrate` before starting new binaries. New binaries
  refuse a version 1 schema; old binaries refuse version 2, so restart all processes
  on the new version after migrating.

- Upstream creation is available only from the Upstreams tab; remove its duplicate
  shortcut from the route form while preserving drafts across tab switches.

- The local GUI offers Light, Dark, and System appearance choices and remembers the
  preference in browser local storage when available.
- The GUI only attaches existing route handler configurations. It has no handler
  creation form or write endpoint; global handlers are omitted from the panel and guide.
  Hidden existing global+route bindings are preserved when editing routes.

- Add an optional, separate local route GUI container using the Management API. It
  provides Routes and Upstreams tabs with list/add/edit/delete and generation
  preconditions. A small guide describes the available route handlers.
  Credentials stay server-side. The gateway image/archive exclude it.
- Handler type metadata adds an optional `description` for discoverable help text.

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
