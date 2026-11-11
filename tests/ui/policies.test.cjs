"use strict";

const assert = require("node:assert/strict");
const { test } = require("node:test");

const fixture = () => ({ schema_version: 1, upstreams: [{ id: "demo" }], global_handlers: ["global"], handlers: [
  { id: "global", type: "headers", version: 1, options: { response_set: { "X-Global": "kept" } } },
  { id: "shared", type: "concurrency_limit", version: 1, options: { limit: 2 } },
], routes: [
  { id: "a", path: "/a", upstream: "demo", priority: 17, handlers: ["global", "shared"] },
  { id: "b", path: "/b", upstream: "demo", handlers: ["shared"] },
] });

test("editing a shared policy changes only its route and preserves unrelated configuration", async () => {
  const { candidate } = await import("../../internal/ui/files/policies.mjs");
  const config = fixture();
  const original = structuredClone(config.handlers[1]);
  const result = candidate(config, { path: "/edited" }, config.routes[0], [
    { source: original, selected: true, type: "concurrency_limit", version: 1, options: { limit: 7 } },
  ], 64);
  assert.deepEqual(config, fixture(), "Draft construction mutated the loaded configuration");
  assert.equal(result.routes[0].path, "/edited");
  assert.equal(result.routes[0].priority, 17);
  assert.equal(result.routes[0].handlers[0], "global");
  assert.notEqual(result.routes[0].handlers[1], "shared");
  assert.equal(result.handlers.find(h => h.id === result.routes[0].handlers[1]).options.limit, 7);
  assert.deepEqual(result.routes[1], config.routes[1]);
  assert.deepEqual(result.handlers.find(h => h.id === "shared"), original);
  assert.deepEqual(result.global_handlers, ["global"]);
  assert.deepEqual(result.upstreams, config.upstreams);
});

test("an exclusive policy updates without accumulating definitions; detaching preserves presets", async () => {
  const { candidate } = await import("../../internal/ui/files/policies.mjs");
  const config = fixture(); config.routes[1].handlers = [];
  const policy = { source: config.handlers[1], type: "concurrency_limit", version: 1, selected: true, options: { limit: 9 } };
  const updated = candidate(config, {}, config.routes[0], [policy], 64);
  assert.equal(updated.handlers.length, 2);
  assert.deepEqual(updated.routes[0].handlers, ["global", "shared"]);
  assert.equal(updated.handlers[1].options.limit, 9);
  const detached = candidate(config, {}, config.routes[0], [{ ...policy, selected: false }], 64);
  assert.deepEqual(detached.routes[0].handlers, ["global"]);
  assert.deepEqual(detached.handlers, config.handlers);
});

test("global definitions are cloned when bound locally, never edited in place", async () => {
  const { candidate } = await import("../../internal/ui/files/policies.mjs");
  const config = fixture();
  const updated = candidate(config, {}, config.routes[0], [{ source: config.handlers[0], type: "headers", version: 1, selected: true, options: { response_set: { "X-Local": "a" } } }], 64);
  assert.deepEqual(updated.handlers[0], config.handlers[0]);
  assert.deepEqual(updated.global_handlers, ["global"]);
  assert.notEqual(updated.routes[0].handlers[0], "global");
});

test("concurrent handler changes require review, while unchanged drafts preserve external edits", async () => {
  const { candidate } = await import("../../internal/ui/files/policies.mjs");
  const config = fixture(); const originalRoute = structuredClone(config.routes[0]);
  const source = structuredClone(config.handlers[1]);
  const policy = { source, type: source.type, version: 1, selected: true, options: { limit: 7 } };
  config.handlers[1].options.limit = 13;
  assert.throws(() => candidate(config, {}, originalRoute, [policy], 64), /Handler options changed/);
  const preserved = candidate(config, { path: "/new" }, originalRoute, [{ ...policy, options: source.options }], 64);
  assert.equal(preserved.handlers[1].options.limit, 13);
  config.routes[0].handlers = ["shared"];
  assert.throws(() => candidate(config, {}, originalRoute, [policy], 64), /Route handlers changed/);
});

test("new route policies share one document and use collision-free bounded IDs", async () => {
  const { candidate } = await import("../../internal/ui/files/policies.mjs");
  const config = fixture();
  const updated = candidate(config, { id: "c", path: "/c", upstream: "demo" }, null, [
    { type: "rate_limit", version: 1, selected: true, options: { requests_per_second: 3, burst: 2 } },
    { type: "body_limit", version: 1, selected: true, options: { max_bytes: 4096 } },
  ], 1);
  const route = updated.routes.find(route => route.id === "c");
  assert.equal(route.handlers.length, 2);
  assert.equal(new Set(updated.handlers.map(h => h.id)).size, updated.handlers.length);
  assert.ok(route.handlers.every(id => id.length === 1));
  assert.deepEqual(updated.handlers.find(h => h.id === route.handlers[0]).options, { requests_per_second: 3, burst: 2 });
  assert.deepEqual(updated.handlers.find(h => h.id === route.handlers[1]).options, { max_bytes: 4096 });
  assert.throws(() => candidate(config, { id: "a" }, null, [], 64), /already exists/);
  assert.throws(() => candidate(config, {}, { id: "deleted" }, [], 64), /was removed/);
});
