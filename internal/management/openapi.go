package management

import (
	"encoding/json"
	"strings"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/middleware"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
)

func (a *API) Contract() []byte {
	schemas := map[string]any{"Config": config.Schema(config.Config{}), "Route": config.Schema(config.Route{}), "Upstream": config.Schema(config.Upstream{}), "Target": config.Schema(config.Target{}), "Handler": config.Schema(config.Handler{}), "Problem": config.Schema(problem{})}
	variants := []any{}
	schemas["Revision"] = config.Schema(postgres.Revision{})
	for _, t := range a.Service.Compiler.Registry.Types() {
		name := "HandlerOptions_" + t.Name
		schemas[name] = t.Schema
		h := config.Schema(config.Handler{})
		props := h["properties"].(map[string]any)
		props["type"] = map[string]any{"type": "string", "enum": []string{t.Name}}
		props["options"] = ref(name)
		variants = append(variants, h)
	}
	schemas["Handler"] = map[string]any{"oneOf": variants}
	configProps := schemas["Config"].(map[string]any)["properties"].(map[string]any)
	configProps["handlers"] = array(ref("Handler"))
	schemas["Settings"] = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"trusted_proxies": configProps["trusted_proxies"], "metrics": configProps["metrics"]}}
	schemas["Publication"] = map[string]any{"type": "object", "required": []string{"revision_id", "generation", "status"}, "properties": map[string]any{"revision_id": map[string]any{"type": "integer", "format": "int64"}, "generation": map[string]any{"type": "integer", "format": "int64", "minimum": 0}, "status": map[string]any{"type": "string", "enum": []string{"committed"}, "description": "Committed in PostgreSQL; instances apply asynchronously."}}}
	paths := map[string]any{}
	add := func(path, verb string, request, response any, mutation bool) {
		entry, ok := paths[path].(map[string]any)
		if !ok {
			entry = map[string]any{}
			paths[path] = entry
		}
		params := []any{}
		for _, p := range []string{"id", "target_id", "type", "revision_id"} {
			if strings.Contains(path, "{"+p+"}") {
				params = append(params, map[string]any{"name": p, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
			}
		}
		if mutation {
			params = append(params, map[string]any{"name": "If-Match", "in": "header", "required": true, "description": "Strong ETag for the complete configuration; e.g. \"g-0\". Wildcards are rejected.", "schema": map[string]any{"type": "string", "pattern": "^\"g-[0-9]+\"$"}})
		}
		op := map[string]any{"parameters": params, "responses": map[string]any{"200": map[string]any{"description": "Success", "headers": map[string]any{"ETag": map[string]any{"description": "Generation of the complete configuration.", "schema": map[string]any{"type": "string"}}}, "content": map[string]any{"application/json": map[string]any{"schema": response}}}, "default": map[string]any{"description": "400 invalid JSON; 401 authentication; 404 missing resource; 409 resource conflict; 412 stale precondition; 413 size limit; 422 validation; 428 missing precondition; 503 storage or concurrency limit.", "content": map[string]any{"application/json": map[string]any{"schema": ref("Problem")}}}}}
		if request != nil {
			media := "application/json"
			if verb == "patch" {
				media = "application/merge-patch+json"
			}
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{media: map[string]any{"schema": request}}}
		}
		entry[verb] = op
	}
	for _, resource := range []struct{ path, name string }{{"routes", "Route"}, {"upstreams", "Upstream"}, {"handlers", "Handler"}, {"upstreams/{id}/targets", "Target"}} {
		p := "/v1/" + resource.path
		name := resource.name
		add(p, "get", nil, array(ref(name)), false)
		add(p, "post", ref(name), ref("Publication"), true)
		id := "/{id}"
		if name == "Target" {
			id = "/{target_id}"
		}
		p += id
		add(p, "get", nil, ref(name), false)
		add(p, "put", ref(name), ref("Publication"), true)
		add(p, "patch", map[string]any{"description": "RFC 7396: omitted unchanged; null removes an optional value; required fields cannot be removed; arrays replace in full."}, ref("Publication"), true)
		add(p, "delete", nil, ref("Publication"), true)
	}
	for _, v := range []struct {
		path   string
		schema any
	}{{"/v1/config", ref("Config")}, {"/v1/settings", ref("Settings")}, {"/v1/global-handlers", array(map[string]any{"type": "string"})}, {"/v1/routes/{id}/handlers", array(map[string]any{"type": "string"})}} {
		add(v.path, "get", nil, v.schema, false)
		add(v.path, "put", v.schema, ref("Publication"), true)
		add(v.path, "patch", map[string]any{"description": "JSON Merge Patch. Null removes optional overrides; null on required fields is rejected."}, ref("Publication"), true)
	}
	add("/v1/config/effective", "get", nil, ref("Config"), false)
	add("/v1/config/validate", "post", ref("Config"), map[string]any{"type": "object", "properties": map[string]any{"valid": map[string]any{"type": "boolean"}}}, false)
	add("/v1/rollback", "post", map[string]any{"type": "object", "additionalProperties": false, "required": []string{"revision_id"}, "properties": map[string]any{"revision_id": map[string]any{"type": "integer", "minimum": 1}}}, ref("Publication"), true)
	add("/v1/status", "get", nil, map[string]any{"type": "object", "properties": map[string]any{"revision_id": map[string]any{"type": "integer"}, "generation": map[string]any{"type": "integer"}}}, false)
	add("/v1/revisions", "get", nil, map[string]any{"type": "array", "description": "Latest 100 immutable revisions with generation, authenticated principal, declared author, description, and timestamp.", "items": ref("Revision")}, false)
	add("/v1/revisions/{revision_id}", "get", nil, ref("Config"), false)
	add("/v1/handler-types", "get", nil, array(config.Schema(middleware.Type{})), false)
	add("/v1/handler-types/{type}/schema", "get", nil, map[string]any{"type": "object", "description": "Typed handler options schema."}, false)
	add("/openapi.json", "get", nil, map[string]any{"type": "object"}, false)
	for _, p := range []string{"/healthz", "/readyz"} {
		add(p, "get", nil, map[string]any{"type": "object"}, false)
	}
	paths["/healthz"].(map[string]any)["get"].(map[string]any)["security"] = []any{}
	doc := map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": "Routeqtus Management API", "version": "1.0.0", "description": "Configuration is published as complete immutable revisions. GET configured values for round-trip export; GET effective values only for inspection. Secrets are environment references: secret values are never accepted or returned. Null optional overrides inherit defaults; zero durations explicitly disable supported timeouts."}, "security": []any{map[string]any{"bearerAuth": []any{}}}, "paths": paths, "components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"}}, "schemas": schemas}}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}
func ref(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
func array(items any) map[string]any { return map[string]any{"type": "array", "items": items} }
