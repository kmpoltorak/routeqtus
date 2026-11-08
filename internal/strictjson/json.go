// Package strictjson decodes administrative documents without ambiguous keys.
package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func Decode(data []byte, dst any) error {
	_, err := decode(data, dst, false)
	return err
}

// DecodeDocument also returns the generic JSON tree for field-presence checks.
// Its numbers have the same float64 range as json.Unmarshal into any.
func DecodeDocument(data []byte, dst any) (any, error) {
	return decode(data, dst, true)
}

func decode(data []byte, dst any, collect bool) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	if !collect {
		// Raw JSON destinations retain their existing number range.
		d.UseNumber()
	}
	document, err := value(d, 0, collect)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON value")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return document, d.Decode(dst)
}

func value(d *json.Decoder, depth int, collect bool) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("JSON nesting exceeds 64")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		var object map[string]any
		if collect {
			object = map[string]any{}
		}
		for d.More() {
			t, err = d.Token()
			if err != nil {
				return nil, err
			}
			key, ok := t.(string)
			if !ok {
				return nil, fmt.Errorf("expected object key")
			}
			if seen[key] {
				return nil, fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			v, err := value(d, depth+1, collect)
			if err != nil {
				return nil, err
			}
			if collect {
				object[key] = v
			}
		}
		_, err = d.Token()
		return object, err
	case '[':
		array := []any{}
		for d.More() {
			v, err := value(d, depth+1, collect)
			if err != nil {
				return nil, err
			}
			if collect {
				array = append(array, v)
			}
		}
		_, err = d.Token()
		if !collect {
			return nil, err
		}
		return array, err
	default:
		return nil, fmt.Errorf("unexpected delimiter")
	}
}

// Merge implements RFC 7396. Validation of the complete result happens separately.
func Merge(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	t, ok := target.(map[string]any)
	if !ok {
		t = map[string]any{}
	}
	for k, v := range p {
		if v == nil {
			delete(t, k)
		} else {
			t[k] = Merge(t[k], v)
		}
	}
	return t
}
