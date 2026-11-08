package strictjson

import "testing"

func TestAmbiguousDocuments(t *testing.T) {
	for _, s := range []string{`{"x":1,"x":2}`, `{"x":{"a":1,"a":2}}`, `{} {}`, `[`, ``} {
		var v any
		if Decode([]byte(s), &v) == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestUnknownFields(t *testing.T) {
	var v struct {
		A int `json:"a"`
	}
	if Decode([]byte(`{"b":1}`), &v) == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestMerge(t *testing.T) {
	v := Merge(map[string]any{"a": 1, "b": map[string]any{"x": 1, "y": 2}}, map[string]any{"b": map[string]any{"x": nil}, "c": false}).(map[string]any)
	if v["a"] != 1 || v["c"] != false || v["b"].(map[string]any)["x"] != nil || v["b"].(map[string]any)["y"] != 2 {
		t.Fatal(v)
	}
}
func FuzzDecode(f *testing.F) {
	f.Add(`{"a":1}`)
	f.Add(`{"a":1,"a":2}`)
	f.Fuzz(func(t *testing.T, s string) { var v any; _ = Decode([]byte(s), &v) })
}
