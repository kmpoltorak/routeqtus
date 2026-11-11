"use strict";

const assert = require("node:assert/strict");
const names = ["body_limit", "concurrency_limit", "headers", "rate_limit", "token_allowlist"];

async function checkVisibleHandlers(page, expected = names) {
  assert.equal(await page.getByRole("button", { name: /Configure handler|Create configuration/ }).count(), 0);
  assert.equal(await page.locator("#handler-dialog").count(), 0);
  assert.equal(await page.getByRole("list", { name: "Global handlers" }).count(), 0);
  assert.deepEqual((await page.locator(".policy-card").evaluateAll(cards => cards.map(card => card.dataset.policyType))).sort(), expected);
  assert.equal(await page.locator("#handler-count").textContent(), String(expected.length));
  await page.getByRole("button", { name: "Handler guide", exact: true }).click();
  await page.waitForFunction(count => document.querySelectorAll("#guide-content h3").length === count, expected.length);
  assert.deepEqual((await page.locator("#guide-content h3").allTextContents()).sort(), expected);
  for (const name of ["recover", "access_log", "request_id"]) assert.equal(await page.locator("#guide-content").getByRole("heading", { name, exact: true }).count(), 0);
  await page.getByRole("button", { name: "Close handler guide" }).click();
}

module.exports = async function checkHandlerChoices(page, config) {
  await checkVisibleHandlers(page);
  const globals = new Set(config.global_handlers || []);
  const local = config.handlers.find(handler => !globals.has(handler.id) && handler.type === "headers");
  for (const kind of ["disabled", "empty", "unregistered"]) {
    const fixture = structuredClone(config);
    if (kind === "disabled") {
      assert.ok(local && fixture.routes.length);
      fixture.handlers.find((handler) => handler.id === local.id).enabled = false;
      fixture.routes[0].handlers = [...new Set([...fixture.routes[0].handlers, local.id])];
    } else {
      fixture.handlers = fixture.handlers.filter((handler) => globals.has(handler.id));
      for (const route of fixture.routes) route.handlers = route.handlers.filter((id) => globals.has(id));
    }
    const preview = await page.context().newPage();
    try {
      await preview.route("**/api/config", (route) => route.fulfill({ json: fixture, headers: { ETag: '"g-1"' } }));
      if (kind === "unregistered") await preview.route("**/api/handler-types", route => route.fulfill({ json: [] }));
      await preview.goto(page.url());
      await preview.locator("#connection.connected").waitFor();
      await checkVisibleHandlers(preview, kind === "unregistered" ? [] : names);
      if (kind === "disabled") {
        await preview.getByRole("button", { name: "Edit route " + fixture.routes[0].id, exact: true }).click();
        const binding = preview.locator('input[name="policies"][value="' + local.id + '"]');
        assert.equal(await binding.isChecked(), true);
        assert.equal(await binding.isDisabled(), true);
        assert.equal(await preview.locator(".policy-card").filter({ has: binding }).locator("textarea").first().isDisabled(), true);
      }
    } finally { await preview.close(); }
  }
};
