"use strict";

(() => {
  const system = matchMedia("(prefers-color-scheme: dark)");
  let preference = "system";
  try {
    const saved = localStorage.getItem("routetus-theme");
    if (["light", "dark", "system"].includes(saved)) preference = saved;
  } catch { /* Browser storage may be blocked; appearance still works for this page. */ }

  function apply() {
    document.documentElement.dataset.theme = preference === "system" ? (system.matches ? "dark" : "light") : preference;
  }

  apply();
  system.addEventListener("change", () => { if (preference === "system") apply(); });
  document.addEventListener("DOMContentLoaded", () => {
    const choice = document.querySelector("#theme");
    choice.value = preference;
    choice.addEventListener("change", () => {
      preference = choice.value;
      apply();
      try { localStorage.setItem("routetus-theme", preference); }
      catch { /* Keep the selected appearance even when persistence is unavailable. */ }
    });
  });
})();
