"use strict";

const assert = require("node:assert/strict");

module.exports = async function checkPolicyOptions(page, base) {
  const token = process.env.ROUTETUS_ROUTE_TOKEN;
  assert.ok(token && token.length >= 32, "The disposable demo needs ROUTETUS_ROUTE_TOKEN in the test environment");
  const config = async () => {
    const response = await fetch(base + "/api/config");
    assert.equal(response.status, 200);
    return { body: await response.json(), etag: response.headers.get("etag") };
  };
  const publish = async (current, body, status = 200) => {
    const response = await fetch(base + "/api/config", { method: "PUT", headers: { Origin: base, "Content-Type": "application/json", "If-Match": current.etag }, body: JSON.stringify(body) });
    assert.equal(response.status, status);
  };
  const refresh = async () => {
    await page.getByRole("button", { name: "Refresh" }).click();
    await page.locator("#notice").filter({ hasText: "Configuration refreshed." }).waitFor();
  };
  const policy = name => page.locator('[data-policy-type="' + name + '"]');
  const option = (type, name) => policy(type).locator('[name="' + name + '"]');
  const ids = ["policy-" + Date.now().toString(36) + "-a", "policy-" + Date.now().toString(36) + "-b"];
  const first = await config();
  const fill = async id => {
    await page.getByLabel("Route ID", { exact: true }).fill(id);
    await page.getByLabel("Request path", { exact: true }).fill("/" + id);
    await page.getByLabel("Upstream", { exact: true }).selectOption(first.body.upstreams[0].id);
  };
  const save = async id => {
    await page.getByRole("button", { name: "Save route", exact: true }).click();
    await page.locator("#notice").filter({ hasText: 'Route "' + id + '" updated.' }).waitFor();
  };
  await fill(ids[0]);
  await page.locator('input[name="methods"][value="POST"]').check();
  for (const name of ["headers", "concurrency_limit", "rate_limit", "body_limit", "token_allowlist"]) await policy(name).locator('input[name="policies"]').check();
  await option("headers", "response_set").fill("X-Route: first");
  await option("concurrency_limit", "limit").fill("2");
  await option("rate_limit", "requests_per_second").fill("10000");
  await option("rate_limit", "burst").fill("10000");
  await option("body_limit", "max_bytes").fill("128");
  await option("token_allowlist", "token_envs").fill("ROUTETUS_ROUTE_TOKEN");
  const before = await config();
  await page.getByRole("button", { name: "Create route", exact: true }).click();
  await page.locator("#notice").filter({ hasText: 'Route "' + ids[0] + '" created.' }).waitFor();
  const created = await config();
  assert.equal(Number(created.etag.match(/\d+/)[0]), Number(before.etag.match(/\d+/)[0]) + 1, "Route and policies must publish in one generation");
  const routeA = created.body.routes.find(route => route.id === ids[0]);
  assert.equal(routeA.handlers.length, 5);
  assert.ok(!JSON.stringify(created.body).includes(token), "Configuration must never contain a route token value");
  const publicURL = process.env.ROUTETUS_PUBLIC_TEST_URL || "http://127.0.0.1:8080";
  const request = async (id, options = {}) => {
    // Publication is asynchronous: wait for the serving snapshot, without weakening assertions.
    for (let i = 0; i < 40; i++) {
      const response = await fetch(publicURL + "/" + id, options);
      if (response.status !== 404) { await response.arrayBuffer(); return response; }
      await response.arrayBuffer();
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error("Serving snapshot did not apply the policy route");
  };
  assert.equal((await request(ids[0])).status, 401);
  const authenticated = { headers: { Authorization: "Bearer " + token } };
  const allowed = await request(ids[0], authenticated);
  assert.equal(allowed.status, 200);
  assert.equal(allowed.headers.get("X-Route"), "first");
  assert.equal((await request(ids[0], { ...authenticated, method: "POST", body: "x".repeat(129) })).status, 413);

  await fill(ids[1]);
  await page.getByRole("button", { name: "Create route", exact: true }).click();
  await page.locator("#notice").filter({ hasText: 'Route "' + ids[1] + '" created.' }).waitFor();
  const shared = await config();
  shared.body.routes.find(route => route.id === ids[1]).handlers = [...routeA.handlers];
  await publish(shared, shared.body);
  await refresh();
  await page.getByRole("button", { name: "Edit route " + ids[0], exact: true }).click();
  assert.equal(await option("concurrency_limit", "limit").inputValue(), "2");
  if (process.env.ROUTETUS_UI_SCREENSHOT_DIR) {
    await page.screenshot({ path: process.env.ROUTETUS_UI_SCREENSHOT_DIR + "/policy-options-desktop.png", fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Expanded policy fields overflow on mobile");
    await page.screenshot({ path: process.env.ROUTETUS_UI_SCREENSHOT_DIR + "/policy-options-mobile.png", fullPage: true });
    await page.setViewportSize({ width: 1440, height: 1080 });
  }
  await option("concurrency_limit", "limit").fill("7");
  await option("headers", "response_set").fill("X-Route: changed");
  await save(ids[0]);
  const edited = await config();
  const optionsFor = (document, id, type) => {
    const route = document.routes.find(route => route.id === id);
    return document.handlers.find(handler => route.handlers.includes(handler.id) && handler.type === type).options;
  };
  assert.equal(optionsFor(edited.body, ids[0], "concurrency_limit").limit, 7);
  assert.equal(optionsFor(edited.body, ids[1], "concurrency_limit").limit, 2);
  assert.deepEqual(optionsFor(edited.body, ids[1], "headers"), { response_set: { "X-Route": "first" } });
  assert.deepEqual(edited.body.global_handlers, first.body.global_handlers);

  await page.getByRole("button", { name: "Edit route " + ids[0], exact: true }).click();
  await option("headers", "request_set").fill("Authorization: forbidden");
  const valid = await config();
  await page.getByRole("button", { name: "Save route", exact: true }).click();
  await page.locator("#notice.error").filter({ hasText: "disallowed header" }).waitFor();
  assert.deepEqual(await config(), valid, "Rejected policy changes must not publish a route or definition");
  await page.getByRole("button", { name: "Cancel edit", exact: true }).click();

  await page.getByRole("button", { name: "Edit route " + ids[0], exact: true }).click();
  await option("concurrency_limit", "limit").fill("8");
  const concurrent = await config();
  const currentRoute = concurrent.body.routes.find(route => route.id === ids[0]);
  concurrent.body.handlers.find(handler => currentRoute.handlers.includes(handler.id) && handler.type === "concurrency_limit").options.limit = 9;
  await publish(concurrent, concurrent.body);
  await page.getByRole("button", { name: "Save route", exact: true }).click();
  await page.locator("#notice.error").filter({ hasText: "Configuration changed while you were editing" }).waitFor();
  assert.equal(await option("concurrency_limit", "limit").inputValue(), "8");
  const conflict = await config();
  await page.getByRole("button", { name: "Save route", exact: true }).click();
  await page.locator("#notice.error").filter({ hasText: "Handler options changed" }).waitFor();
  assert.deepEqual(await config(), conflict, "A concurrent policy edit must require review");
  await page.getByRole("button", { name: "Cancel edit", exact: true }).click();
  await page.getByRole("button", { name: "Edit route " + ids[0], exact: true }).click();
  assert.equal(await option("concurrency_limit", "limit").inputValue(), "9");
  await page.getByRole("button", { name: "Cancel edit", exact: true }).click();

  const unknown = await config();
  const invalid = structuredClone(unknown.body);
  invalid.handlers.push({ id: ids[0] + "-unknown", type: "not_registered", version: 1, options: {} });
  await publish(unknown, invalid, 422);
  assert.deepEqual(await config(), unknown, "An unknown type cannot be created through the GUI API");
  for (const id of ids) {
    const current = await config();
    const response = await fetch(base + "/api/routes/" + id, { method: "DELETE", headers: { Origin: base, "If-Match": current.etag } });
    assert.ok(response.ok);
  }
  await refresh();
};
