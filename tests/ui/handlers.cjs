"use strict";

const assert = require("node:assert/strict");

module.exports = async function checkHandlerChoices(page, config) {
  const globals = page.getByRole("list", { name: "Global handlers" });
  assert.equal(await globals.getByRole("listitem").count(), config.global_handlers.length);
  for (const id of config.global_handlers) {
    const handler = config.handlers.find((item) => item.id === id);
    await globals.getByText(id + (handler.enabled === false ? " · disabled" : " · automatic"), { exact: true }).waitFor();
    assert.equal(await page.locator('input[name="handlers"][value="' + id + '"]').count(), 0, "Global handlers must not be offered as new route bindings");
  }
  if (!config.global_handlers.length) return;
  const disabled = await page.context().newPage();
  try {
    const fixture = structuredClone(config);
    const id = config.global_handlers[0];
    fixture.handlers.find((handler) => handler.id === id).enabled = false;
    await disabled.route("**/api/config", (route) => route.fulfill({ json: fixture, headers: { ETag: '"g-1"' } }));
    await disabled.goto(page.url());
    await disabled.locator("#connection.connected").waitFor();
    await disabled.getByRole("list", { name: "Global handlers" }).getByText(id + " · disabled", { exact: true }).waitFor();
    assert.equal(await disabled.locator('input[name="handlers"][value="' + id + '"]').count(), 0);
  } finally { await disabled.close(); }
};
