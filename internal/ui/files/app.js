"use strict";

const $ = (selector) => document.querySelector(selector);
const form = $("#route-form");
const state = { config: null, etag: "", saving: false, loading: false, types: [], deleting: null, routeEdit: null, upstreamEdit: null };

function notice(message, error = false) {
  const box = $("#notice");
  box.textContent = message;
  box.className = error ? "notice error" : "notice";
  box.hidden = !message;
}

function connection(label, status) {
  $("#connection").textContent = label;
  $("#connection").className = "connection " + status;
}

function updateSubmit() {
  document.querySelectorAll(".resource-submit").forEach((button) => { button.disabled = state.saving || state.loading || !state.etag; });
  $("#submit").disabled = state.saving || state.loading || !state.etag || !state.config?.upstreams.length;
  $("#submit").firstChild.textContent = state.saving ? "Saving route " : state.routeEdit ? "Save route " : "Create route ";
  $("#cancel-route-edit").disabled = state.saving || state.loading;
  if (state.deleting?.kind === "upstreams" && referencingRoutes(state.deleting.id).length) $("#confirm-delete").disabled = true;
  if (state.upstreamEdit?.targetsChanged) $("#upstream-form button[type=submit]").disabled = true;
}

async function readResponse(response) {
  let payload;
  try { payload = await response.json(); }
  catch { throw new Error("The Management API returned an unreadable response."); }
  if (!response.ok) {
    const detail = Array.isArray(payload.fields) ? payload.fields.map((field) => field.path + ": " + field.message).join("\n") : "";
    const error = new Error((payload.message || "The request could not be completed.") + (detail ? "\n" + detail : ""));
    error.status = response.status;
    throw error;
  }
  return payload;
}

function renderRoutes() {
  const body = $("#routes");
  body.replaceChildren();
  const query = $("#search").value.trim().toLowerCase();
  const routes = (state.config?.routes || []).filter((route) => [route.id, route.path, route.upstream].some((value) => value.toLowerCase().includes(query)));
  $("#visible-count").textContent = String(routes.length);
  $("#route-table").hidden = !routes.length;
  $("#empty").hidden = Boolean(routes.length);
  $("#empty-title").textContent = query ? "No matching routes" : "No routes yet";
  $("#empty-description").textContent = query ? "Try another ID, path or upstream." : "Add a route to connect a path to your services.";
  for (const route of routes) {
    const row = document.createElement("tr");
    const pathCell = row.insertCell();
    const name = document.createElement("span");
    name.className = "route-name";
    name.textContent = route.id;
    const path = document.createElement("span");
    path.className = "route-path";
    path.textContent = route.path;
    const match = document.createElement("span");
    match.className = "route-match";
    match.textContent = route.prefix ? "Prefix match" : "Exact match";
    pathCell.append(name, path, match);
    const methodsCell = row.insertCell();
    for (const method of route.methods?.length ? route.methods : ["ANY"]) {
      const tag = document.createElement("span");
      tag.className = "method-tag " + (method === "POST" ? "post" : method === "DELETE" ? "delete" : "");
      tag.textContent = method;
      methodsCell.append(tag);
    }
    const destination = document.createElement("span");
    destination.className = "upstream-name";
    destination.textContent = route.upstream;
    row.insertCell().append(destination);
    const handlers = document.createElement("span");
    handlers.className = "handler-summary";
    handlers.textContent = route.handlers?.length ? route.handlers.join(", ") : "None";
    row.insertCell().append(handlers);
    row.insertCell().append(action("Edit", "route " + route.id, () => editRoute(route)), action("Delete", "route " + route.id, () => openDelete("routes", route.id)));
    body.append(row);
  }
}

function renderChoices() {
  const previousUpstream = $("#upstream").value;
  const selected = new Set([...form.querySelectorAll('input[name="handlers"]:checked')].map((input) => input.value));
  $("#upstream").replaceChildren(new Option("Choose an upstream", ""));
  for (const upstream of state.config.upstreams) $("#upstream").append(new Option(upstream.id, upstream.id));
  if (state.config.upstreams.some((item) => item.id === previousUpstream)) $("#upstream").value = previousUpstream;
  $("#upstream-hint").textContent = state.config.upstreams.length ? "Destination groups come from the Management API." : "No upstreams configured. Add one through the Management API first.";
  const choices = $("#handler-options");
  choices.replaceChildren();
  for (const handler of state.config.handlers) {
    const label = document.createElement("label");
    label.className = "handler-choice";
    label.title = handler.type + (handler.enabled === false ? " (disabled)" : "");
    const input = document.createElement("input");
    input.type = "checkbox";
    input.name = "handlers";
    input.value = handler.id;
    input.disabled = handler.enabled === false;
    input.checked = selected.has(handler.id);
    label.append(input, document.createTextNode(handler.id + (input.disabled ? " · disabled" : "")));
    choices.append(label);
  }
  if (!state.config.handlers.length) {
    const empty = document.createElement("p");
    empty.className = "hint";
    empty.textContent = "No handlers configured.";
    choices.append(empty);
  }
}

async function load(announce = false) {
  state.loading = true;
  $("#refresh").disabled = true;
  updateSubmit();
  try {
    const response = await fetch("/api/config", { cache: "no-store" });
    const config = await readResponse(response);
    if (![config.routes, config.upstreams, config.handlers].every(Array.isArray)) throw new Error("The configuration response is incomplete.");
    state.config = config;
    state.etag = response.headers.get("ETag") || "";
    $("#route-count").textContent = String(config.routes.length);
    $("#upstream-count").textContent = String(config.upstreams.length);
    $("#handler-count").textContent = String(config.handlers.length);
    renderRoutes();
    renderUpstreams();
    renderChoices();
    connection("Connected", "connected");
    if (!state.etag) notice("The Management API did not provide a configuration generation. Changes are unavailable.", true);
    else if (announce) notice("Configuration refreshed.");
    return true;
  } catch (error) {
    state.etag = "";
    connection("Unavailable", "unavailable");
    notice(error.status === 401 ? "The Management API rejected the GUI credentials. Check the GUI service configuration." : error.message, true);
    if (!state.config) {
      $("#empty-title").textContent = "Configuration unavailable";
      $("#empty-description").textContent = "Check the management service, then refresh.";
    }
    return false;
  } finally {
    state.loading = false;
    $("#refresh").disabled = false;
    updateSubmit();
  }
}

$("#search").addEventListener("input", renderRoutes);
$("#refresh").addEventListener("click", () => load(true));
$("#prefix").addEventListener("change", () => {
  $("#strip-prefix").disabled = !$("#prefix").checked;
  if (!$("#prefix").checked) $("#strip-prefix").checked = false;
});

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (state.saving || state.loading || !state.etag || !form.reportValidity()) return;
  const methods = [...form.querySelectorAll('input[name="methods"]:checked')].map((input) => input.value);
  if (!methods.length && !state.routeEdit) { notice("Select at least one HTTP method.", true); return; }
  const route = {
    id: $("#route-id").value.trim(), path: $("#route-path").value.trim(), methods,
    upstream: $("#upstream").value,
    handlers: [...form.querySelectorAll('input[name="handlers"]:checked')].map((input) => input.value),
    prefix: $("#prefix").checked, strip_prefix: $("#prefix").checked && $("#strip-prefix").checked,
  };
  const editing = state.routeEdit;
  const selected = new Set(route.handlers);
  if (editing) {
    route.handlers = [...editing.original.handlers.filter((id) => selected.has(id)), ...route.handlers.filter((id) => !editing.original.handlers.includes(id))];
    route.methods = [...(editing.original.methods || []).filter((method) => methods.includes(method)), ...methods.filter((method) => !(editing.original.methods || []).includes(method))];
  }
  const payload = editing ? changedFields(route, editing.original) : route;
  if (editing) delete payload.id;
  state.saving = true;
  $("#refresh").disabled = true;
  updateSubmit();
  notice("");
  try {
    const response = await fetch("/api/routes" + (editing ? "/" + encodeURIComponent(editing.id) : ""), { method: editing ? "PATCH" : "POST", headers: { "Content-Type": "application/json", "If-Match": editing ? editing.etag : state.etag }, body: JSON.stringify(payload) });
    await readResponse(response);
    resetRoute();
    if (await load()) notice('Route "' + route.id + '" ' + (editing ? "updated" : "created") + '. Serving instances apply the change in the background.');
  } catch (error) {
    if (error.status === 412) {
      if (await load()) {
        if (editing) editing.etag = state.etag;
        notice("Configuration changed while you were editing. The list is refreshed; review your form and try again.", true);
      }
    } else notice(error.message, true);
  } finally {
    state.saving = false;
    $("#refresh").disabled = false;
    updateSubmit();
  }
});

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}

function dialogError(id, message) {
  const box = $(id);
  box.textContent = message;
  box.hidden = !message;
}

function action(label, subject, onClick) {
  const button = element("button", label, "text-button" + (label === "Delete" ? " delete-link" : ""));
  button.type = "button";
  button.setAttribute("aria-label", label + " " + subject);
  button.addEventListener("click", () => { if (!state.saving && !state.loading) onClick(); });
  return button;
}

function changedFields(values, original) {
  return Object.fromEntries(Object.entries(values).filter(([key, value]) => JSON.stringify(value) !== JSON.stringify(original[key] ?? (Array.isArray(value) ? [] : typeof value === "boolean" ? false : ""))));
}

function showSection(section) {
  for (const name of ["routes", "upstreams"]) {
    $("#" + name + "-panel").hidden = name !== section;
    if (name === section) $("#" + name + "-tab").setAttribute("aria-current", "page");
    else $("#" + name + "-tab").removeAttribute("aria-current");
  }
  $("#page-title").textContent = section === "routes" ? "Routes" : "Upstreams";
  document.title = $("#page-title").textContent + " · Routetus";
}

for (const section of ["routes", "upstreams"]) $("#" + section + "-tab").addEventListener("click", () => showSection(section));

function resetRoute() {
  state.routeEdit = null;
  form.reset();
  form.querySelectorAll(".extra-method").forEach((node) => node.remove());
  $("#route-id").readOnly = false;
  $("#strip-prefix").disabled = true;
  $("#form-heading").textContent = "Add a route";
  $("#cancel-route-edit").hidden = true;
  updateSubmit();
}

function editRoute(route) {
  resetRoute();
  state.routeEdit = { id: route.id, original: structuredClone(route), etag: state.etag };
  $("#route-id").value = route.id;
  $("#route-id").readOnly = true;
  $("#route-path").value = route.path;
  $("#upstream").value = route.upstream;
  for (const method of route.methods || []) {
    if ([...form.querySelectorAll('input[name="methods"]')].some((input) => input.value === method)) continue;
    const label = element("label", undefined, "extra-method");
    const input = document.createElement("input");
    input.type = "checkbox"; input.name = "methods"; input.value = method;
    label.append(input, element("span", method));
    $(".method-options").append(label);
  }
  for (const input of form.querySelectorAll('input[name="methods"]')) input.checked = (route.methods || []).includes(input.value);
  for (const input of form.querySelectorAll('input[name="handlers"]')) input.checked = route.handlers.includes(input.value);
  $("#prefix").checked = Boolean(route.prefix);
  $("#strip-prefix").checked = Boolean(route.strip_prefix);
  $("#strip-prefix").disabled = !route.prefix;
  $("#form-heading").textContent = "Edit route";
  $("#cancel-route-edit").hidden = false;
  updateSubmit();
  $("#route-path").focus();
}

$("#cancel-route-edit").addEventListener("click", resetRoute);

function referencingRoutes(id) {
  return (state.config?.routes || []).filter((route) => route.upstream === id).map((route) => route.id);
}

function renderUpstreams() {
  const body = $("#upstreams");
  body.replaceChildren();
  const query = $("#upstream-search").value.trim().toLowerCase();
  const references = new Map();
  for (const route of state.config?.routes || []) {
    if (!references.has(route.upstream)) references.set(route.upstream, []);
    references.get(route.upstream).push(route.id);
  }
  for (const upstream of state.config?.upstreams || []) {
    if (![upstream.id, ...upstream.targets.map((target) => target.url)].some((value) => value.toLowerCase().includes(query))) continue;
    const row = document.createElement("tr");
    row.insertCell().append(element("span", upstream.id, "route-name"));
    const targets = row.insertCell();
    for (const target of upstream.targets) targets.append(element("span", target.id + " · " + target.url, "route-path"));
    row.insertCell().textContent = (references.get(upstream.id) || []).join(", ") || "None";
    row.insertCell().append(action("Edit", "upstream " + upstream.id, () => openUpstream(upstream)), action("Delete", "upstream " + upstream.id, () => openDelete("upstreams", upstream.id)));
    body.append(row);
  }
  $("#upstream-empty").hidden = Boolean(body.children.length);
}

$("#upstream-search").addEventListener("input", renderUpstreams);

function openDelete(kind, id) {
  state.deleting = { kind, id, etag: state.etag };
  renderDelete();
  $("#delete-dialog").showModal();
}

function renderDelete() {
  const { kind, id } = state.deleting;
  const upstream = kind === "upstreams";
  const current = state.config[kind].find((item) => item.id === id);
  $("#delete-heading").textContent = upstream ? "Delete upstream?" : "Delete route?";
  $("#confirm-delete").textContent = upstream ? "Delete upstream" : "Delete route";
  $("#delete-description").textContent = upstream ? "Delete all routes using this upstream first. Routes are never removed automatically." : "This removes the route from the published configuration. Its upstream and handlers remain available.";
  $("#delete-target").textContent = id + (upstream ? "" : " · " + current?.path);
  const references = upstream ? referencingRoutes(id) : [];
  dialogError("#delete-error", references.length ? "Remove these routes first: " + references.join(", ") : "");
  updateSubmit();
}

document.querySelectorAll("[data-close]").forEach((button) => button.addEventListener("click", () => $("#" + button.dataset.close).close()));

async function handlerTypes() {
  const response = await fetch("/api/handler-types", { cache: "no-store" });
  const types = await readResponse(response);
  if (!Array.isArray(types)) throw new Error("Handler types could not be read.");
  state.types = types;
  return types;
}

$("#handler-guide").addEventListener("click", async () => {
  const content = $("#guide-content");
  content.replaceChildren(element("p", "Loading registered handlers.", "hint"));
  $("#guide-dialog").showModal();
  try {
    content.replaceChildren();
    for (const type of await handlerTypes()) {
      const card = element("section", undefined, "guide-item");
      card.append(element("h3", type.name), element("p", type.description || "Custom registered handler. Review its option descriptions before use."));
      const options = Object.entries(type.schema?.properties || {});
      if (!options.length) card.append(element("p", "No options."));
      else {
        const list = document.createElement("ul");
        for (const [name, property] of options) list.append(element("li", name + ": " + (property.description || property.type || "JSON value")));
        card.append(list);
      }
      content.append(card);
    }
  } catch (error) { content.replaceChildren(element("p", error.message, "notice error")); }
});

function openUpstream(upstream = null) {
  state.upstreamEdit = upstream ? { id: upstream.id, original: structuredClone(upstream), etag: state.etag, targetsChanged: false } : null;
  $("#upstream-form").reset();
  $("#upstream-id").value = upstream?.id || "";
  $("#upstream-id").readOnly = Boolean(upstream);
  $("#upstream-heading").textContent = upstream ? "Edit upstream" : "New upstream";
  $("#upstream-form button[type=submit]").textContent = upstream ? "Save upstream" : "Create upstream";
  $("#new-targets").hidden = Boolean(upstream);
  $("#upstream-targets").required = !upstream;
  $("#edit-targets").hidden = !upstream;
  $("#target-fields").replaceChildren();
  for (const target of upstream?.targets || []) addTarget(target.id, target.url);
  dialogError("#upstream-error", "");
  updateSubmit();
  $("#upstream-dialog").showModal();
}

$("#new-upstream").addEventListener("click", () => openUpstream());
$("#add-upstream").addEventListener("click", () => openUpstream());

function addTarget(id, url = "") {
  const row = element("div", undefined, "target-row");
  row.dataset.id = id;
  const label = element("label", "Backend URL · " + id);
  const input = document.createElement("input");
  input.type = "url"; input.required = true; input.value = url;
  label.append(input);
  row.append(label, action("Remove", "backend " + id, () => row.remove()));
  $("#target-fields").append(row);
}

$("#add-target").addEventListener("click", () => {
  const ids = new Set([...(state.upstreamEdit?.original.targets || []).map((target) => target.id), ...[...$("#target-fields").children].map((row) => row.dataset.id)]);
  let index = 1;
  while (ids.has("target-" + index)) index++;
  addTarget("target-" + index);
});

function renderParameters() {
  const type = state.types.find((item) => item.name === $("#handler-type").value);
  const fields = $("#handler-fields");
  fields.replaceChildren();
  $("#handler-description").textContent = type?.description || "";
  if (!type) return;
  for (const [name, property] of Object.entries(type.schema?.properties || {})) {
    const id = "option-" + fields.children.length;
    const label = element("label", name.replaceAll("_", " "));
    label.htmlFor = id;
    let input;
    let kind = property.type || "object";
    if (Array.isArray(property.enum) || kind === "boolean") {
      input = document.createElement("select");
      input.append(new Option("Use default", ""));
      for (const value of property.enum || [true, false]) input.append(new Option(String(value), JSON.stringify(value)));
      kind = "enum";
    } else if (kind === "integer" || kind === "number") {
      input = document.createElement("input");
      input.type = "number";
      input.step = kind === "integer" ? "1" : "any";
      if (property.minimum !== undefined) input.min = property.minimum;
      if (property.maximum !== undefined) input.max = property.maximum;
    } else if (kind === "string") {
      input = document.createElement("input");
      input.type = "text";
    } else {
      input = document.createElement("textarea");
      input.rows = 3;
      input.placeholder = kind === "array" ? "[]" : "{}";
      kind = "json";
    }
    input.id = id;
    input.dataset.option = name;
    input.dataset.kind = kind;
    input.required = (type.schema?.required || []).includes(name);
    if (property.default !== undefined && property.default !== null) input.value = kind === "json" || kind === "enum" ? JSON.stringify(property.default) : String(property.default);
    fields.append(label, input);
    if (property.description) fields.append(element("p", property.description, "hint"));
  }
  if (!fields.children.length) fields.append(element("p", "This handler has no options.", "hint"));
}

$("#handler-type").addEventListener("change", renderParameters);
$("#new-handler").addEventListener("click", async () => {
  dialogError("#handler-error", "");
  $("#handler-dialog").showModal();
  try {
    const selected = $("#handler-type").value;
    $("#handler-type").replaceChildren(new Option("Choose a registered type", ""));
    for (const type of await handlerTypes()) $("#handler-type").append(new Option(type.name, type.name));
    $("#handler-type").value = selected;
    renderParameters();
  } catch (error) { dialogError("#handler-error", error.message); }
});

async function createResource(path, payload, dialog, errorBox, onCreated, editing = null) {
  if (state.saving || state.loading || !state.etag) return;
  state.saving = true;
  $("#refresh").disabled = true;
  updateSubmit();
  dialogError(errorBox, "");
  try {
    const response = await fetch(path + (editing ? "/" + encodeURIComponent(editing.id) : ""), { method: editing ? "PATCH" : "POST", headers: { "Content-Type": "application/json", "If-Match": editing ? editing.etag : state.etag }, body: JSON.stringify(payload) });
    await readResponse(response);
    if (await load()) {
      onCreated();
      $(dialog).close();
      $(dialog).querySelector("form").reset();
    } else dialogError(errorBox, "Saved, but configuration could not be refreshed. Refresh before continuing.");
  } catch (error) {
    if (error.status === 412 && await load()) {
      if (editing) {
        editing.etag = state.etag;
        const current = state.config.upstreams.find((item) => item.id === editing.id);
        editing.targetsChanged = !current || JSON.stringify(current.targets) !== JSON.stringify(editing.original.targets);
      }
      dialogError(errorBox, editing?.targetsChanged ? "Backend targets changed in another session. Your draft is kept here; close and reopen this upstream to review the latest targets before saving." : "Configuration changed. Review this form and submit again.");
    }
    else dialogError(errorBox, error.message);
  } finally {
    state.saving = false;
    $("#refresh").disabled = false;
    updateSubmit();
  }
}

$("#upstream-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const id = $("#upstream-id").value.trim();
  const editing = state.upstreamEdit;
  if (editing?.targetsChanged) return;
  const targets = editing ? [...$("#target-fields").children].map((row) => ({ id: row.dataset.id, url: row.querySelector("input").value.trim() })) : $("#upstream-targets").value.split(/\r?\n/).map((value) => value.trim()).filter(Boolean).map((url, index) => ({ id: "target-" + (index + 1), url }));
  if (!targets.length) { dialogError("#upstream-error", "Enter at least one backend URL."); return; }
  await createResource("/api/upstreams", editing ? changedFields({ targets }, editing.original) : { id, targets }, "#upstream-dialog", "#upstream-error", () => {
    $("#upstream").value = id;
    notice('Upstream "' + id + '" ' + (editing ? "updated" : "created and selected") + '.');
  }, editing);
});

$("#handler-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const type = state.types.find((item) => item.name === $("#handler-type").value);
  if (!type) { dialogError("#handler-error", "Choose a registered handler type."); return; }
  const options = Object.create(null);
  try {
    for (const input of $("#handler-fields").querySelectorAll("[data-option]")) {
      if (input.value === "" && !input.required) continue;
      const kind = input.dataset.kind;
      options[input.dataset.option] = kind === "json" || kind === "enum" ? JSON.parse(input.value) : kind === "integer" || kind === "number" ? Number(input.value) : input.value;
    }
  } catch { dialogError("#handler-error", "Enter valid JSON in the structured option fields."); return; }
  const id = $("#handler-id").value.trim();
  await createResource("/api/handlers", { id, type: type.name, version: type.version, options }, "#handler-dialog", "#handler-error", () => {
    for (const input of form.querySelectorAll('input[name="handlers"]')) if (input.value === id) input.checked = true;
    notice('Handler configuration "' + id + '" created and selected.');
  });
});

$("#confirm-delete").addEventListener("click", async () => {
  if (state.saving || state.loading || !state.etag || !state.deleting) return;
  const { kind, id, etag } = state.deleting;
  if (kind === "upstreams" && referencingRoutes(id).length) return;
  const label = kind === "upstreams" ? "Upstream" : "Route";
  state.saving = true;
  updateSubmit();
  dialogError("#delete-error", "");
  try {
    const response = await fetch("/api/" + kind + "/" + encodeURIComponent(id), { method: "DELETE", headers: { "If-Match": etag } });
    await readResponse(response);
    $("#delete-dialog").close();
    state.deleting = null;
    if (state.routeEdit?.id === id && kind === "routes") resetRoute();
    if (await load()) notice(label + ' "' + id + '" deleted.');
  } catch (error) {
    if (error.status === 412 && await load()) {
      const current = state.config[kind].find((item) => item.id === id);
      if (current) {
        state.deleting.etag = state.etag;
        renderDelete();
        if (kind !== "upstreams" || !referencingRoutes(id).length) dialogError("#delete-error", "Configuration changed. Review this " + label.toLowerCase() + " and confirm again.");
      } else {
        $("#delete-dialog").close();
        state.deleting = null;
        notice("This " + label.toLowerCase() + " has already been removed.");
      }
    } else dialogError("#delete-error", error.message);
  } finally { state.saving = false; updateSubmit(); }
});

load();
