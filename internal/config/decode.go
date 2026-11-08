package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/kmpoltorak/routeqtus/internal/strictjson"
)

type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e FieldError) Error() string       { return e.Path + ": " + e.Message }
func Invalid(path, message string) error { return FieldError{path, message} }

func Decode(data []byte) (Config, error) { var c Config; err := DecodeTyped(data, &c); return c, err }
func DecodeTyped(data []byte, dst any) error {
	raw, err := strictjson.DecodeDocument(data, dst)
	if err != nil {
		return Invalid("/", err.Error())
	}
	return required(raw, reflect.TypeOf(dst).Elem(), "")
}
func required(v any, t reflect.Type, path string) error {
	if t.Kind() == reflect.Pointer {
		if v == nil {
			return nil
		}
		return required(v, t.Elem(), path)
	}
	if t == reflect.TypeFor[json.RawMessage]() || t == reflect.TypeFor[Duration]() {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return Invalid(path, "must be an object")
		}
		allowed := map[string]bool{}
		for i := 0; i < t.NumField(); i++ {
			allowed[strings.Split(t.Field(i).Tag.Get("json"), ",")[0]] = true
		}
		for key := range m {
			if !allowed[key] {
				return Invalid(path+"/"+key, "unknown field (field names are case-sensitive)")
			}
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			key := strings.Split(f.Tag.Get("json"), ",")[0]
			x, present := m[key]
			p := path + "/" + key
			if !present {
				if f.Tag.Get("required") == "true" {
					return Invalid(p, "field is required")
				}
				continue
			}
			if x == nil && f.Type.Kind() != reflect.Pointer {
				return Invalid(p, "null is not allowed; omit optional fields to use defaults")
			}
			if err := required(x, f.Type, p); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if a, ok := v.([]any); ok {
			for i, x := range a {
				if err := required(x, t.Elem(), fmt.Sprintf("%s/%d", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
