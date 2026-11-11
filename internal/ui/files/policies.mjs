export const policyNames = ["headers", "concurrency_limit", "rate_limit", "body_limit", "token_allowlist"];

export function same(a, b) {
  if (a === b) return true;
  if (!a || !b || typeof a !== "object" || typeof b !== "object" || Array.isArray(a) !== Array.isArray(b)) return false;
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every(key => Object.hasOwn(b, key) && same(a[key], b[key]));
}

function availableID(used, preferred, maximum) {
  const short = preferred.slice(0, maximum);
  if (short && !used.has(short)) return short;
  const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-";
  for (let i = 0; i <= used.size; i++) {
    let n = i, id = "";
    do { id = alphabet[n % alphabet.length] + id; n = Math.floor(n / alphabet.length) - 1; } while (n >= 0);
    if (id.length > maximum) break;
    if (!used.has(id)) return id;
  }
  throw new Error("No handler identifiers are available within the configured length limit.");
}

// One candidate and one generation precondition publish the route and its policies together.
export function candidate(config, change, originalRoute, policies, maximumIDLength) {
  const next = structuredClone(config);
  const current = originalRoute && next.routes.find(route => route.id === originalRoute.id);
  if (originalRoute && !current) throw new Error("This route was removed. Cancel editing and refresh.");
  if (!originalRoute && next.routes.some(route => route.id === change.id)) throw new Error("This route ID already exists.");
  const route = { ...(current || { handlers: [] }), ...change };
  const dirty = policy => policy.source ? !policy.selected || !same(policy.options, policy.source.options) : policy.selected;
  if (policies.some(dirty)) {
    if (originalRoute && !same(current.handlers, originalRoute.handlers)) throw new Error("Route handlers changed. Cancel editing and reopen the route to review them.");
    route.handlers = [...route.handlers];
    const used = new Set(next.handlers.map(handler => handler.id));
    for (const policy of policies.filter(dirty)) {
      const source = policy.source;
      if (!policy.selected) {
        route.handlers = route.handlers.filter(id => id !== source.id);
        continue;
      }
      const currentHandler = source && next.handlers.find(handler => handler.id === source.id);
      if (source && !same(currentHandler, source)) throw new Error("Handler options changed. Cancel editing and reopen the route to review them.");
      const shared = source && (next.global_handlers.includes(source.id) || next.routes.some(other => other.id !== route.id && other.handlers.includes(source.id)));
      if (source && !shared) {
        currentHandler.options = structuredClone(policy.options);
      } else {
        const id = availableID(used, route.id + "-" + policy.type, maximumIDLength);
        used.add(id);
        next.handlers.push({ ...(source || {}), id, type: policy.type, version: policy.version, options: structuredClone(policy.options) });
        if (source) route.handlers = route.handlers.map(value => value === source.id ? id : value);
        else route.handlers.push(id);
      }
    }
  }
  // ponytail: detached definitions remain API presets; deleting a binding never deletes a user's preset.
  if (current) next.routes[next.routes.indexOf(current)] = route;
  else next.routes.push(route);
  return next;
}

const labels = {
  limit: "Concurrent requests", requests_per_second: "Requests per second", burst: "Burst requests",
  max_bytes: "Maximum body bytes", token_envs: "Token environment variables",
  request_set: "Request headers to set", response_set: "Response headers to set",
  request_remove: "Request headers to remove", response_remove: "Response headers to remove",
};

function node(tag, text, className) {
  const result = document.createElement(tag);
  if (text !== undefined) result.textContent = text;
  if (className) result.className = className;
  return result;
}

function lines(value) { return value.split(/\r?\n/).filter(line => line.trim()); }

function readOptions(fields, original = {}) {
  const options = Object.create(null);
  for (const { input, name, schema } of fields) {
    if (schema.type === "integer") {
      if (!Number.isSafeInteger(input.valueAsNumber)) throw new Error("Enter a whole number for " + labels[name] + ".");
      options[name] = input.valueAsNumber;
    } else if (schema.type === "object") {
      const entries = lines(input.value).map(line => {
        const colon = line.indexOf(":");
        if (colon < 1) throw new Error("Use one Header-Name: value per line.");
        return [line.slice(0, colon).trim(), line.slice(colon + 1).replace(/^ /, "")];
      });
      if (new Set(entries.map(([key]) => key.toLowerCase())).size !== entries.length) throw new Error("Each header name must occur only once in a field.");
      if (entries.length || Object.hasOwn(original, name)) options[name] = Object.fromEntries(entries);
    } else {
      const values = lines(input.value).map(line => line.trim());
      if (name === "token_envs" && (!values.length || values.some(value => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(value)))) throw new Error("Enter environment variable names, not token values.");
      if (values.length || Object.hasOwn(original, name)) options[name] = values;
    }
  }
  return options;
}

export class PolicyEditor {
  constructor(container) { this.container = container; this.cards = null; this.types = []; }

  configure(types, contract, config) {
    this.types = policyNames.flatMap(name => types.filter(type => type.name === name && type.version === 1));
    this.maximumIDLength = contract.components?.schemas?.Handler?.oneOf?.[0]?.properties?.id?.maxLength;
    if (!Number.isSafeInteger(this.maximumIDLength) || this.maximumIDLength < 1) throw new Error("Handler identifier limits could not be read.");
    if (this.cards === null) this.render(config);
  }

  render(config, route = null) {
    this.container.replaceChildren();
    this.cards = [];
    const globals = new Set(config.global_handlers);
    const attached = (route?.handlers || []).map(id => config.handlers.find(handler => handler.id === id)).filter(handler => handler && !globals.has(handler.id));
    for (const type of this.types) {
      const existing = attached.filter(handler => handler.type === type.name);
      for (const source of existing.length ? existing : [null]) {
        const card = node("section", undefined, "policy-card");
        card.dataset.policyType = type.name;
        const label = node("label", undefined, "handler-choice");
        const selected = document.createElement("input");
        selected.type = "checkbox"; selected.name = "policies"; selected.value = source?.id || type.name;
        selected.checked = Boolean(source); selected.disabled = source?.enabled === false;
        label.append(selected, node("span", type.name + (selected.disabled ? " · disabled" : "")));
        card.append(label, node("p", type.description, "hint"));
        const controls = node("div", undefined, "policy-fields");
        const fields = [];
        const options = source?.options || type.example || {};
        for (const [name, schema] of Object.entries(type.schema.properties || {})) {
          const input = document.createElement(schema.type === "integer" ? "input" : "textarea");
          input.id = "policy-" + this.cards.length + "-" + name; input.name = name;
          const fieldLabel = node("label", labels[name] || name); fieldLabel.htmlFor = input.id;
          if (schema.type === "integer") {
            input.type = "number"; input.step = "1"; input.required = true;
            input.min = schema.minimum; input.max = schema.maximum; input.value = options[name] ?? schema.default ?? schema.minimum;
          } else {
            input.rows = 2;
            input.spellcheck = false;
            input.value = schema.type === "object" ? Object.entries(options[name] || {}).map(([key, value]) => key + ": " + value).join("\n") : (options[name] || []).join("\n");
            if (name === "token_envs") {
              input.required = true; input.placeholder = "ROUTETUS_ROUTE_TOKEN"; input.autocomplete = "off";
            } else input.placeholder = schema.type === "object" ? "X-Example: value" : "One name per line";
          }
          controls.append(fieldLabel, input);
          fields.push({ input, name, schema });
        }
        if (type.name === "token_allowlist") controls.append(node("p", "Names only. Provision matching secrets in both manage and serve; token values never belong in this form.", "hint"));
        if (type.name === "body_limit") controls.append(node("p", "Bytes: 1 MiB = 1048576; 1 GiB = 1073741824. The process body limit still applies.", "hint"));
        const toggle = () => {
          controls.hidden = !selected.checked;
          for (const { input } of fields) input.disabled = !selected.checked || selected.disabled;
        };
        selected.addEventListener("change", toggle); toggle();
        card.append(controls);
        this.container.append(card);
        this.cards.push({ source: source && structuredClone(source), type: type.name, version: type.version, selected, fields });
      }
    }
    if (!this.cards.length) this.container.append(node("p", "No supported route policies are registered on the server.", "hint"));
  }

  values() {
    return this.cards.map(card => ({ source: card.source, type: card.type, version: card.version, selected: card.selected.checked,
      options: card.selected.checked ? (card.selected.disabled ? structuredClone(card.source.options) : readOptions(card.fields, card.source?.options)) : {} }));
  }
}
