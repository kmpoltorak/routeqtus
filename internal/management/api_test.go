package management

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestAuthenticationAndETag(t *testing.T) {
	token := strings.Repeat("a", 32)
	for _, h := range []string{"", "Bearer wrong", "Basic " + token, "bearer " + token} {
		r := httptest.NewRequest("GET", "/v1/config", nil)
		r.Header.Set("Authorization", h)
		if Authorized(r, token) {
			t.Fatal(h)
		}
	}
	for _, s := range []string{"*", `W/"g-1"`, `"g-01"`, `"g--1"`, `"g-0", "g-1"`, ""} {
		r := httptest.NewRequest("PUT", "/", nil)
		r.Header.Set("If-Match", s)
		if _, ok := expected(httptest.NewRecorder(), r); ok {
			t.Fatal("accepted", s)
		}
	}
	r := httptest.NewRequest("PUT", "/", nil)
	r.Header.Set("If-Match", ETag(42))
	if g, ok := expected(httptest.NewRecorder(), r); !ok || g != 42 {
		t.Fatal(g, ok)
	}
}
func TestMergeAndWholeCandidate(t *testing.T) {
	c := testutil.Config("http://example.com")
	updated, err := mutate(c, "/v1/routes/demo", "PATCH", []byte(`{"request_timeout":"0s","metrics":false}`))
	if err != nil || updated.Routes[0].RequestTimeout == nil || *updated.Routes[0].RequestTimeout != 0 || *updated.Routes[0].Metrics {
		t.Fatalf("%+v %v", updated, err)
	}
	updated, err = mutate(updated, "/v1/routes/demo", "PATCH", []byte(`{"request_timeout":null,"metrics":null}`))
	if err != nil || updated.Routes[0].RequestTimeout != nil || updated.Routes[0].Metrics != nil {
		t.Fatal(updated, err)
	}
	if _, err = mutate(c, "/v1/routes/demo", "PATCH", []byte(`{"upstream":null}`)); err == nil {
		t.Fatal("required field removed")
	}
	updated, err = mutate(c, "/v1/upstreams/demo", "DELETE", nil)
	if err != nil {
		t.Fatal(err)
	}
	svc := configservice.Service{Compiler: testutil.Compiler(t)}
	if svc.Validate(updated) == nil {
		t.Fatal("dependency deletion accepted")
	}
}
func TestHandlerSchemaAndValidatorConformance(t *testing.T) {
	compiler := testutil.Compiler(t)
	a := New(&configservice.Service{Compiler: compiler}, "", "", 1024, 1, nil)
	var contract map[string]any
	if err := json.Unmarshal(a.Contract(), &contract); err != nil {
		t.Fatal(err)
	}
	schemas := contract["components"].(map[string]any)["schemas"].(map[string]any)
	for _, typ := range compiler.Registry.Types() {
		t.Run(typ.Name, func(t *testing.T) {
			if schemas["HandlerOptions_"+typ.Name] == nil {
				t.Fatal("missing schema")
			}
			b, _ := json.Marshal(typ.Example)
			if _, err := typ.Factory(b); err != nil {
				t.Fatal("example rejected", err)
			}
			var raw map[string]any
			_ = json.Unmarshal(b, &raw)
			raw["unknown"] = true
			b, _ = json.Marshal(raw)
			if _, err := typ.Factory(b); err == nil {
				t.Fatal("unknown option accepted")
			}
			props := typ.Schema["properties"].(map[string]any)
			for field := range props {
				if len(props[field].(map[string]any)) == 0 {
					t.Fatal("untyped field", field)
				}
			}
			for _, field := range typ.Schema["required"].([]string) {
				b, _ = json.Marshal(typ.Example)
				_ = json.Unmarshal(b, &raw)
				delete(raw, "unknown")
				delete(raw, field)
				b, _ = json.Marshal(raw)
				if _, err := typ.Factory(b); err == nil {
					t.Fatal("missing required option accepted", field)
				}
			}
		})
	}
	c := testutil.Config("http://example.com")
	c.Handlers[0].Enabled = config.B(false)
	if _, err := compiler.Compile(c); err != nil {
		t.Fatal(err)
	}
}
func TestBodyLimitsAndDuplicateKeys(t *testing.T) {
	a := New(&configservice.Service{Compiler: testutil.Compiler(t)}, "", "", 16, 1, nil)
	for _, tc := range []struct {
		b    string
		code int
	}{{strings.Repeat("a", 32), 413}, {`{"a":1,"a":2}`, 400}} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.b))
		w := httptest.NewRecorder()
		if _, ok := a.body(w, r); ok || w.Code != tc.code {
			t.Fatalf("%v %d", ok, w.Code)
		}
	}
}
