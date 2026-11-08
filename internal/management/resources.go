package management

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/strictjson"
)

type resourceError struct {
	status int
	code   string
}

func (e resourceError) Error() string { return strings.ReplaceAll(e.code, "_", " ") }
func missing() error                  { return resourceError{404, "not_found"} }
func conflict() error                 { return resourceError{409, "resource_conflict"} }
func wire(c config.Config) map[string]any {
	b, _ := json.Marshal(c)
	var v map[string]any
	_ = json.Unmarshal(b, &v)
	return v
}

type location struct {
	parent     map[string]any
	key        string
	index      int
	collection bool
	id         string
}

func locate(root map[string]any, p string) (location, error) {
	parts := strings.Split(strings.TrimPrefix(p, "/v1/"), "/")
	if len(parts) == 1 && parts[0] == "config" {
		return location{parent: map[string]any{"config": root}, key: "config", index: -1}, nil
	}
	if len(parts) == 1 && parts[0] == "global-handlers" {
		return location{parent: root, key: "global_handlers", index: -1}, nil
	}
	if len(parts) == 1 && parts[0] == "settings" {
		return location{parent: root, key: "settings", index: -1}, nil
	}
	if len(parts) < 1 || parts[0] != "routes" && parts[0] != "upstreams" && parts[0] != "handlers" {
		return location{}, missing()
	}
	key := parts[0]
	if len(parts) == 1 {
		return location{parent: root, key: key, index: -1, collection: true}, nil
	}
	a, _ := root[key].([]any)
	idx := -1
	for i, v := range a {
		if v.(map[string]any)["id"] == parts[1] {
			idx = i
			break
		}
	}
	if idx < 0 {
		return location{}, missing()
	}
	if len(parts) == 2 {
		return location{parent: root, key: key, index: idx, id: parts[1]}, nil
	}
	obj := a[idx].(map[string]any)
	if len(parts) == 3 && parts[0] == "routes" && parts[2] == "handlers" {
		return location{parent: obj, key: "handlers", index: -1}, nil
	}
	if parts[0] != "upstreams" || parts[2] != "targets" {
		return location{}, missing()
	}
	if len(parts) == 3 {
		return location{parent: obj, key: "targets", index: -1, collection: true}, nil
	}
	if len(parts) != 4 {
		return location{}, missing()
	}
	targets, _ := obj["targets"].([]any)
	for i, v := range targets {
		if v.(map[string]any)["id"] == parts[3] {
			return location{parent: obj, key: "targets", index: i, id: parts[3]}, nil
		}
	}
	return location{}, missing()
}
func representation(c config.Config, p string) (any, error) {
	root := wire(c)
	l, err := locate(root, p)
	if err != nil {
		return nil, err
	}
	if l.key == "settings" {
		settings := map[string]any{}
		for _, key := range []string{"trusted_proxies", "metrics"} {
			if value, ok := root[key]; ok {
				settings[key] = value
			}
		}
		return settings, nil
	}
	if l.index < 0 {
		return l.parent[l.key], nil
	}
	return l.parent[l.key].([]any)[l.index], nil
}
func mutate(c config.Config, p, method string, b []byte) (config.Config, error) {
	root := wire(c)
	l, err := locate(root, p)
	if err != nil {
		return c, err
	}
	var patch any
	if len(b) > 0 {
		if err = strictjson.Decode(b, &patch); err != nil {
			return c, err
		}
	}
	if l.key == "settings" {
		if method != "PUT" && method != "PATCH" {
			return c, resourceError{405, "method_not_allowed"}
		}
		m, ok := patch.(map[string]any)
		if !ok {
			return c, config.Invalid("/", "settings must be an object")
		}
		for k := range m {
			if k != "trusted_proxies" && k != "metrics" {
				return c, config.Invalid("/"+k, "unknown setting")
			}
		}
		if method == "PUT" {
			delete(root, "trusted_proxies")
			delete(root, "metrics")
		}
		for k, v := range m {
			if v == nil && method == "PATCH" {
				delete(root, k)
			} else {
				root[k] = v
			}
		}
	} else if l.collection {
		if method != "POST" {
			return c, resourceError{405, "method_not_allowed"}
		}
		m, ok := patch.(map[string]any)
		if !ok {
			return c, config.Invalid("/", "resource must be an object")
		}
		id, _ := m["id"].(string)
		a, _ := l.parent[l.key].([]any)
		for _, v := range a {
			if v.(map[string]any)["id"] == id {
				return c, conflict()
			}
		}
		l.parent[l.key] = append(a, patch)
	} else if l.index >= 0 {
		a := l.parent[l.key].([]any)
		switch method {
		case "DELETE":
			l.parent[l.key] = append(a[:l.index], a[l.index+1:]...)
		case "PUT", "PATCH":
			if method == "PATCH" {
				patch = strictjson.Merge(a[l.index], patch)
			}
			m, ok := patch.(map[string]any)
			if !ok || m["id"] != l.id {
				return c, config.Invalid("/id", "resource ID must match its URL")
			}
			a[l.index] = patch
		default:
			return c, resourceError{405, "method_not_allowed"}
		}
	} else {
		if method != "PUT" && method != "PATCH" {
			return c, resourceError{405, "method_not_allowed"}
		}
		if method == "PATCH" {
			patch = strictjson.Merge(l.parent[l.key], patch)
		}
		if l.key == "config" {
			m, ok := patch.(map[string]any)
			if !ok {
				return c, fmt.Errorf("configuration must be an object")
			}
			root = m
		} else {
			l.parent[l.key] = patch
		}
	}
	data, err := json.Marshal(root)
	if err != nil {
		return c, err
	}
	return config.Decode(data)
}
