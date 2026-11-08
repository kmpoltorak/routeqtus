package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// Schema derives the wire fields from the actual typed contract, outside request handling.
func Schema(v any) map[string]any { return schema(reflect.TypeOf(v)) }
func schema(t reflect.Type) map[string]any {
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t.Kind() == reflect.Pointer {
		s := schema(t.Elem())
		s["nullable"] = true
		return s
	}
	if t == reflect.TypeFor[Duration]() {
		return map[string]any{"type": "string", "format": "duration", "pattern": "^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$", "description": "Go duration from 0s through 24h."}
	}
	if t == reflect.TypeFor[json.RawMessage]() {
		return map[string]any{"type": "object"}
	}
	s := map[string]any{}
	switch t.Kind() {
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		props := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			p := schema(f.Type)
			if doc := f.Tag.Get("doc"); doc != "" {
				p["description"] = doc
			}
			if d := f.Tag.Get("default"); d != "" {
				var v any
				if json.Unmarshal([]byte(d), &v) != nil {
					v = d
				}
				p["default"] = v
			}
			if e := f.Tag.Get("enum"); e != "" {
				var v any
				_ = json.Unmarshal([]byte(e), &v)
				p["enum"] = []any{v}
			}
			switch f.Name {
			case "ID", "Upstream":
				p["pattern"] = "^[A-Za-z0-9_-]{1,64}$"
				p["minLength"] = 1
				p["maxLength"] = 64
			case "Priority":
				p["minimum"] = -1000000
				p["maximum"] = 1000000
			case "MaxConns", "MaxIdleConns", "MaxIdleConnsPerHost":
				p["minimum"] = 0
				p["maximum"] = 100000
			case "SampleEvery":
				p["minimum"] = 0
				p["maximum"] = 1000000
			case "Value":
				p["minLength"] = 1
				p["maxLength"] = 128
				p["pattern"] = "^[^\\r\\n\\u0000]+$"
			case "URL":
				p["format"] = "uri"
			case "CAEnv", "CertEnv", "KeyEnv":
				p["pattern"] = "^([A-Za-z_][A-Za-z0-9_]*)?$"
			case "Methods":
				p["uniqueItems"] = true
				p["items"] = map[string]any{"type": "string", "pattern": "^[A-Z][A-Z0-9_-]{0,31}$"}
			case "Targets":
				p["minItems"] = 1
				p["maxItems"] = 1024
			case "Routes", "Upstreams", "Handlers":
				if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Struct {
					p["maxItems"] = 10000
				}
			}
			props[name] = p
			if f.Tag.Get("required") == "true" {
				required = append(required, name)
			}
		}
		s["type"] = "object"
		s["properties"] = props
		s["required"] = required
		s["additionalProperties"] = false
	case reflect.Slice:
		s["type"] = "array"
		s["items"] = schema(t.Elem())
	case reflect.Map:
		s["type"] = "object"
		s["additionalProperties"] = schema(t.Elem())
	case reflect.Bool:
		s["type"] = "boolean"
	case reflect.Int, reflect.Int64:
		s["type"] = "integer"
	case reflect.Float64:
		s["type"] = "number"
	default:
		s["type"] = "string"
	}
	return s
}
