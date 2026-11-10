"use strict";

const assert = require("node:assert/strict");

module.exports = async function checkSystemTheme(page) {
  const choice = page.getByLabel("Theme", { exact: true });
  await choice.selectOption("system");
  const checkColors = async (target, dark) => {
    await target.waitForFunction((background) => getComputedStyle(document.querySelector(".button.secondary")).backgroundColor === background,
      dark ? "rgb(36, 40, 50)" : "rgb(255, 255, 255)");
    const colors = await target.evaluate(() => {
      const style = (selector) => {
        const value = getComputedStyle(document.querySelector(selector));
        return { background: value.backgroundColor, color: value.color };
      };
      return { page: style(":root"), card: style(".card"), input: style("#route-id"), dialog: style("dialog"), tabs: style(".tabs"), error: style("#upstream-error") };
    });
    assert.equal(colors.page.background, dark ? "rgb(17, 19, 24)" : "rgb(244, 247, 246)", "Page must respect the selected appearance");
    assert.equal(colors.card.background, dark ? "rgb(27, 30, 37)" : "rgb(255, 255, 255)");
    assert.equal(colors.input.background, dark ? "rgb(20, 23, 29)" : "rgb(255, 255, 255)");
    if (dark) {
      assert.equal(colors.dialog.background, colors.card.background);
      assert.equal(colors.input.color, "rgb(231, 233, 238)");
      assert.equal(colors.tabs.background, "rgb(36, 40, 50)");
      assert.equal(colors.error.background, "rgb(65, 40, 44)");
    }
  };
  for (const scheme of ["light", "dark", "light"]) {
    await page.emulateMedia({ colorScheme: scheme });
    await checkColors(page, scheme === "dark");
  }
  await choice.selectOption("light");
  await page.emulateMedia({ colorScheme: "dark" });
  await checkColors(page, false);
  await choice.selectOption("dark");
  await page.emulateMedia({ colorScheme: "light" });
  await checkColors(page, true);
  await page.reload();
  await page.locator("#connection.connected").waitFor();
  assert.equal(await choice.inputValue(), "dark", "The selected appearance must survive reload");
  await checkColors(page, true);
  await choice.selectOption("system");
  await checkColors(page, false);
  await page.evaluate(() => localStorage.setItem("routetus-theme", "invalid"));
  await page.reload();
  await page.locator("#connection.connected").waitFor();
  assert.equal(await choice.inputValue(), "system");
  await checkColors(page, false);
  const blocked = await page.context().newPage();
  const errors = [];
  blocked.on("pageerror", (error) => errors.push(error.message));
  try {
    await blocked.addInitScript(() => Object.defineProperty(window, "localStorage", { get() { throw new DOMException("Storage blocked", "SecurityError"); } }));
    await blocked.emulateMedia({ colorScheme: "light" });
    await blocked.goto(page.url());
    await blocked.locator("#connection.connected").waitFor();
    await blocked.getByLabel("Theme", { exact: true }).selectOption("dark");
    await checkColors(blocked, true);
    assert.deepEqual(errors, [], "Blocked storage must not break the panel");
  } finally { await blocked.close(); }
  await choice.selectOption("system");
};
