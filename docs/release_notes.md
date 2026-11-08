# Release notes

## v0.1.1 — 2026-11-08

This patch release includes the fixes merged after v0.1.0 (PRs #5–#20).

- Reject spoofed multiline X-Forwarded-For chains and ambiguous X-Forwarded-Proto values.
- Count unwritten panics as 5xx in metrics/access logs; emit metric HELP/TYPE metadata.
- Report safe startup/CLI connection failure categories without exposing credentials.
- Batch revision inserts in one atomic transaction; preserve rollback and notifications.
- Identify the two demo backend instances and clarify method fallback and Kubernetes draining.
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

### Upgrade notes

- Correct tabs in existing `powered_by.value` options before upgrading. The new
  validator rejects tabs and other ASCII controls; Unicode length counts characters.
- Validation error objects retain their JSON Pointer and may add `route_id`,
  `upstream_id`, or `handler_id`. Clients must accept these optional fields.
- Bootstrap and database schema versions remain 1; no new migration is introduced.
- Readiness detection uses `config_sync.poll_interval` plus the query-timeout budget.
  Configure staleness/timeouts for actual load; autoscaling does not remove per-instance limits.

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
