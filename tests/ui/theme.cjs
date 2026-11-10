"use strict";

const assert = require("node:assert/strict");

module.exports = async function checkSystemTheme(page) {
  for (const scheme of ["light", "dark", "light"]) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.waitForFunction((background) => getComputedStyle(document.querySelector(".button.secondary")).backgroundColor === background,
      scheme === "dark" ? "rgb(32, 47, 40)" : "rgb(255, 255, 255)");
    const colors = await page.evaluate(() => {
      const style = (selector) => {
        const value = getComputedStyle(document.querySelector(selector));
        return { background: value.backgroundColor, color: value.color };
      };
      return { page: style(":root"), card: style(".card"), input: style("#route-id"), dialog: style("dialog"), tabs: style(".tabs"), error: style("#upstream-error") };
    });
    const dark = scheme === "dark";
    assert.equal(colors.page.background, dark ? "rgb(16, 24, 21)" : "rgb(244, 247, 246)", "Page must follow the system theme without reloading");
    if (dark) {
      assert.equal(colors.card.background, "rgb(24, 35, 31)");
      assert.equal(colors.dialog.background, colors.card.background);
      assert.equal(colors.input.background, "rgb(17, 29, 23)");
      assert.equal(colors.input.color, "rgb(225, 235, 231)");
      assert.equal(colors.tabs.background, "rgb(32, 47, 40)");
      assert.equal(colors.error.background, "rgb(65, 40, 32)");
    } else {
      assert.equal(colors.card.background, "rgb(255, 255, 255)");
      assert.equal(colors.input.background, "rgb(255, 255, 255)");
    }
  }
};
