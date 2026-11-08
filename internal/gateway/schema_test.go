package gateway_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/extensions"
	"github.com/kmpoltorak/routeqtus/internal/middleware"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func property(s map[string]any, key string) map[string]any {
	return s["properties"].(map[string]any)[key].(map[string]any)
}
func acceptsConfig(t *testing.T, c config.Config) bool {
	t.Helper()
	s, e := testutil.Compiler(t).Compile(c)
	if e == nil {
		s.Close()
	}
	return e == nil
}

func TestSchemaNumericBoundsMatchCompiler(t *testing.T) {
	route := config.Schema(config.Route{})
	upstream := config.Schema(config.Upstream{})
	for _, tc := range []struct {
		name   string
		schema map[string]any
		set    func(*config.Config, int)
	}{
		{"priority", property(route, "priority"), func(c *config.Config, n int) { c.Routes[0].Priority = n }},
		{"connections", property(upstream, "max_conns"), func(c *config.Config, n int) { c.Upstreams[0].MaxConns = n }},
		{"idle", property(upstream, "max_idle_conns"), func(c *config.Config, n int) { c.Upstreams[0].MaxIdleConns = n }},
		{"idle-host", property(upstream, "max_idle_conns_per_host"), func(c *config.Config, n int) { c.Upstreams[0].MaxIdleConnsPerHost = n }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lo, hi := tc.schema["minimum"].(int), tc.schema["maximum"].(int)
			for _, n := range []int{lo - 1, lo, hi, hi + 1} {
				c := testutil.Config("http://example.com")
				tc.set(&c, n)
				if got := acceptsConfig(t, c); got != (n >= lo && n <= hi) {
					t.Fatalf("schema [%d,%d], input=%d accepted=%v", lo, hi, n, got)
				}
			}
		})
	}
}

func TestSchemaFactoryBounds(t *testing.T) {
	compiler := testutil.Compiler(t)
	logType, _ := compiler.Registry.Get("access_log")
	sample := property(logType.Schema, "sample_every")
	lo, hi := sample["minimum"].(int), sample["maximum"].(int)
	for _, n := range []int{lo - 1, lo, hi, hi + 1} {
		b, _ := json.Marshal(middleware.AccessLog{SampleEvery: n})
		_, err := compiler.Registry.Build(config.Handler{ID: "log", Type: "access_log", Version: 1, Options: b})
		if (err == nil) != (n >= lo && n <= hi) {
			t.Fatalf("sample=%d err=%v", n, err)
		}
	}
	powered, _ := compiler.Registry.Get("powered_by")
	value := property(powered.Schema, "value")
	min, max := value["minLength"].(int), value["maxLength"].(int)
	// The legacy schema used an ECMA Unicode escape; both spellings describe NUL.
	pattern := regexp.MustCompile(strings.ReplaceAll(value["pattern"].(string), `\u0000`, `\x00`))
	for _, text := range []string{"", strings.Repeat("a", max), strings.Repeat("a", max+1), strings.Repeat("é", max), strings.Repeat("é", max+1), "a\x01b", "a\tb", "a\rb", "a\nb", "a\x7fb"} {
		n := utf8.RuneCountInString(text)
		want := n >= min && n <= max && pattern.MatchString(text)
		b, _ := json.Marshal(extensions.Powered{Value: text})
		_, err := compiler.Registry.Build(config.Handler{ID: "powered", Type: "powered_by", Version: 1, Options: b})
		if (err == nil) != want {
			t.Errorf("value=%q characters=%d schema-valid=%v err=%v", text, n, want, err)
		}
	}
}

func TestSchemaPatternsMatchCompiler(t *testing.T) {
	schema := config.Schema(config.Route{})
	id := property(schema, "id")
	pattern := regexp.MustCompile(id["pattern"].(string))
	max := id["maxLength"].(int)
	for _, value := range []string{"", "normal-id_1", strings.Repeat("a", max), strings.Repeat("a", max+1), "space value", "é", "x\n"} {
		c := testutil.Config("http://example.com")
		c.Routes[0].ID = value
		if got := acceptsConfig(t, c); got != (pattern.MatchString(value) && utf8.RuneCountInString(value) >= id["minLength"].(int) && utf8.RuneCountInString(value) <= max) {
			t.Fatalf("ID=%q accepted=%v", value, got)
		}
	}
	methods := property(schema, "methods")
	methodPattern := regexp.MustCompile(methods["items"].(map[string]any)["pattern"].(string))
	for _, values := range [][]string{nil, {"GET"}, {"GET", "GET"}, {"lower"}, {"G" + strings.Repeat("A", 31)}, {"G" + strings.Repeat("A", 32)}} {
		want := true
		seen := map[string]bool{}
		for _, v := range values {
			if !methodPattern.MatchString(v) || methods["uniqueItems"] == true && seen[v] {
				want = false
			}
			seen[v] = true
		}
		c := testutil.Config("http://example.com")
		c.Routes[0].Methods = values
		if got := acceptsConfig(t, c); got != want {
			t.Fatalf("methods=%v accepted=%v", values, got)
		}
	}
}

func TestSchemaCollectionCapsMatchCompiler(t *testing.T) {
	schema := config.Schema(config.Config{})
	for _, name := range []string{"routes", "upstreams", "handlers", "targets"} {
		t.Run(name, func(t *testing.T) {
			var declaration map[string]any
			if name == "targets" {
				declaration = property(config.Schema(config.Upstream{}), name)
			} else {
				declaration = property(schema, name)
			}
			max := declaration["maxItems"].(int)
			sizes := []int{max, max + 1}
			min := 0
			if v, ok := declaration["minItems"]; ok {
				min = v.(int)
				sizes = append([]int{min - 1, min}, sizes...)
			}
			for _, n := range sizes {
				c := testutil.Config("http://example.com")
				switch name {
				case "routes":
					base := c.Routes[0]
					c.Routes = nil
					for i := 0; i < n; i++ {
						v := base
						v.ID = fmt.Sprintf("r%d", i)
						v.Path = fmt.Sprintf("/r%d", i)
						c.Routes = append(c.Routes, v)
					}
				case "upstreams":
					base := c.Upstreams[0]
					c.Upstreams = nil
					for i := 0; i < n; i++ {
						v := base
						v.ID = fmt.Sprintf("u%d", i)
						c.Upstreams = append(c.Upstreams, v)
					}
					c.Routes[0].Upstream = "u0"
				case "handlers":
					for len(c.Handlers) < n {
						c.Handlers = append(c.Handlers, config.Handler{ID: fmt.Sprintf("h%d", len(c.Handlers)), Type: "request_id", Version: 1, Options: []byte(`{}`)})
					}
				case "targets":
					c.Upstreams[0].Targets = nil
					for i := 0; i < n; i++ {
						c.Upstreams[0].Targets = append(c.Upstreams[0].Targets, config.Target{ID: fmt.Sprintf("t%d", i), URL: "http://example.com"})
					}
				}
				if got := acceptsConfig(t, c); got != (n >= min && n <= max) {
					t.Fatalf("%s count=%d cap=%d accepted=%v", name, n, max, got)
				}
			}
		})
	}
}

func TestSchemaVersionsMatchCompiler(t *testing.T) {
	for _, kind := range []string{"configuration", "handler"} {
		schema := property(config.Schema(config.Config{}), "schema_version")
		if kind == "handler" {
			schema = property(config.Schema(config.Handler{}), "version")
		}
		for _, n := range []int{0, 1, 2} {
			want := false
			for _, v := range schema["enum"].([]any) {
				if v == float64(n) {
					want = true
				}
			}
			c := testutil.Config("http://example.com")
			if kind == "configuration" {
				c.SchemaVersion = n
			} else {
				c.Handlers[0].Version = n
			}
			if got := acceptsConfig(t, c); got != want {
				t.Fatalf("%s version=%d schema-valid=%v accepted=%v", kind, n, want, got)
			}
		}
	}
}

func TestPoweredValueControlCompatibility(t *testing.T) {
	compiler := testutil.Compiler(t)
	kind, _ := compiler.Registry.Get("powered_by")
	pattern := regexp.MustCompile(property(kind.Schema, "value")["pattern"].(string))
	for n := 0; n <= 127; n++ {
		text := "a" + string(rune(n)) + "b"
		b, _ := json.Marshal(extensions.Powered{Value: text})
		_, err := compiler.Registry.Build(config.Handler{ID: "powered", Type: "powered_by", Version: 1, Options: b})
		want := n == 9 || n >= 32 && n != 127
		if (err == nil) != want || pattern.MatchString(text) != want {
			t.Errorf("ASCII control %d: want accepted=%v, factory error=%v, schema accepted=%v", n, want, err, pattern.MatchString(text))
		}
	}
}
