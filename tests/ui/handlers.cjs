"use strict";

const assert = require("node:assert/strict");

async function checkVisibleHandlers(page, config) {
  const globals = new Set(config.global_handlers || []);
  const available = config.handlers.filter((handler) => !globals.has(handler.id));
  assert.equal(await page.getByRole("button", { name: /Configure handler|Create configuration/ }).count(), 0);
  assert.equal(await page.locator("#handler-dialog").count(), 0);
  assert.equal(await page.getByRole("list", { name: "Global handlers" }).count(), 0);
  assert.deepEqual(await page.locator('input[name="handlers"]').evaluateAll((inputs) => inputs.map((input) => input.value)), available.map((handler) => handler.id));
  assert.equal(await page.locator("#handler-count").textContent(), String(available.length));
  await page.getByRole("button", { name: "Handler guide", exact: true }).click();
  const types = [...new Set(available.map((handler) => handler.type))].sort();
  await page.waitForFunction((count) => document.querySelectorAll("#guide-content h3").length === count, types.length);
  if (!types.length) await page.getByText("No route-specific handlers configured.", { exact: true }).waitFor();
  assert.deepEqual((await page.locator("#guide-content h3").allTextContents()).sort(), types);
  await page.getByRole("button", { name: "Close handler guide" }).click();
}

module.exports = async function checkHandlerChoices(page, config) {
  await checkVisibleHandlers(page, config);
  const globals = new Set(config.global_handlers || []);
  const local = config.handlers.find((handler) => !globals.has(handler.id));
  for (const kind of ["disabled", "empty"]) {
    const fixture = structuredClone(config);
    if (kind === "disabled") {
      if (!local || !fixture.routes.length) continue;
      fixture.handlers.find((handler) => handler.id === local.id).enabled = false;
      fixture.routes[0].handlers = [...new Set([...fixture.routes[0].handlers, local.id])];
    } else {
      fixture.handlers = fixture.handlers.filter((handler) => globals.has(handler.id));
      for (const route of fixture.routes) route.handlers = route.handlers.filter((id) => globals.has(id));
    }
    const preview = await page.context().newPage();
    try {
      await preview.route("**/api/config", (route) => route.fulfill({ json: fixture, headers: { ETag: '"g-1"' } }));
      await preview.goto(page.url());
      await preview.locator("#connection.connected").waitFor();
      await checkVisibleHandlers(preview, fixture);
      if (kind === "disabled") {
        await preview.getByRole("button", { name: "Edit route " + fixture.routes[0].id, exact: true }).click();
        const binding = preview.locator('input[name="handlers"][value="' + local.id + '"]');
        assert.equal(await binding.isChecked(), true);
        assert.equal(await binding.isDisabled(), true);
      }
    } finally { await preview.close(); }
  }
};
