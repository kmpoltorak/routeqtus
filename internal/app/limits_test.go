package app

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

// Each case exceeds its default limit by one; raising that bootstrap limit by one admits it.
func TestBootstrapLimitsReachCompilerAndSchema(t *testing.T) {
	t.Cleanup(func() { config.SetLimits(config.DefaultLimits) })
	d := config.DefaultLimits
	raised := config.Limits{MaxEntities: d.MaxEntities + 1, MaxTargets: d.MaxTargets + 1, MaxConnections: d.MaxConnections + 1, MaxPriority: d.MaxPriority + 1, MaxSampleEvery: d.MaxSampleEvery + 1, MaxPoweredCharacters: d.MaxPoweredCharacters + 1, MaxIdentifierLength: d.MaxIdentifierLength + 1}
	handler := func(c *config.Config, id, options string) {
		for i := range c.Handlers {
			if c.Handlers[i].ID == id {
				c.Handlers[i].Options = json.RawMessage(options)
			}
		}
	}
	for name, over := range map[string]func(*config.Config){
		"max_entities": func(c *config.Config) {
			c.Routes = nil
			for i := 0; i < raised.MaxEntities; i++ {
				c.Routes = append(c.Routes, config.Route{ID: fmt.Sprintf("r%d", i), Path: fmt.Sprintf("/r%d", i), Upstream: "demo"})
			}
		},
		"max_targets": func(c *config.Config) {
			c.Upstreams[0].Targets = nil
			for i := 0; i < raised.MaxTargets; i++ {
				c.Upstreams[0].Targets = append(c.Upstreams[0].Targets, config.Target{ID: fmt.Sprintf("t%d", i), URL: "http://example.com"})
			}
		},
		"max_connections":  func(c *config.Config) { c.Upstreams[0].MaxConns = raised.MaxConnections },
		"max_priority":     func(c *config.Config) { c.Routes[0].Priority = raised.MaxPriority },
		"max_sample_every": func(c *config.Config) { handler(c, "log", fmt.Sprintf(`{"sample_every":%d}`, raised.MaxSampleEvery)) },
		"max_powered_characters": func(c *config.Config) {
			handler(c, "powered", `{"value":"`+strings.Repeat("a", raised.MaxPoweredCharacters)+`"}`)
		},
		"max_identifier_length": func(c *config.Config) { c.Routes[0].ID = strings.Repeat("r", raised.MaxIdentifierLength) },
	} {
		t.Run(name, func(t *testing.T) {
			c := testutil.Config("http://example.com")
			over(&c)
			for limits, valid := range map[config.Limits]bool{config.DefaultLimits: false, raised: true} {
				boot := bootstrap.Defaults()
				boot.Limits = limits
				compiler, err := Compiler(boot, slog.New(slog.NewJSONHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal(err)
				}
				s, err := compiler.Compile(c)
				if err == nil {
					s.Close()
				}
				if (err == nil) != valid {
					t.Fatalf("limits=%+v valid=%v err=%v", limits, valid, err)
				}
			}
		})
	}

	boot := bootstrap.Defaults()
	boot.Limits = raised
	compiler, err := Compiler(boot, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	prop := func(s map[string]any, path ...string) map[string]any {
		for _, p := range path {
			s = s["properties"].(map[string]any)[p].(map[string]any)
		}
		return s
	}
	upstream := config.Schema(config.Upstream{})
	route := config.Schema(config.Route{})
	got := map[string]any{
		"max_entities":          prop(config.Schema(config.Config{}), "upstreams")["maxItems"],
		"max_targets":           prop(upstream, "targets")["maxItems"],
		"max_connections":       prop(upstream, "max_conns")["maximum"],
		"max_priority":          prop(route, "priority")["maximum"],
		"max_identifier_length": prop(route, "id")["maxLength"],
	}
	for _, typ := range compiler.Registry.Types() {
		switch typ.Name {
		case "access_log":
			got["max_sample_every"] = prop(typ.Schema, "sample_every")["maximum"]
		case "powered_by":
			got["max_powered_characters"] = prop(typ.Schema, "value")["maxLength"]
		}
	}
	want := map[string]int{"max_entities": raised.MaxEntities, "max_targets": raised.MaxTargets, "max_connections": raised.MaxConnections, "max_priority": raised.MaxPriority, "max_sample_every": raised.MaxSampleEvery, "max_powered_characters": raised.MaxPoweredCharacters, "max_identifier_length": raised.MaxIdentifierLength}
	for name, v := range want {
		if got[name] != v {
			t.Errorf("schema %s = %v, want %d", name, got[name], v)
		}
	}
}
