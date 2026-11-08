package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const minimal = `{"schema_version":1,"upstreams":[],"handlers":[],"global_handlers":[],"routes":[]}`

func TestStrictContract(t *testing.T) {
	for _, s := range []string{`{}`, strings.Replace(minimal, `"routes":[]`, `"routes":null`, 1), strings.Replace(minimal, `"routes":[]`, `"routes":[],"extra":true`, 1), strings.Replace(minimal, `"routes":[]`, `"routes":[],"routes":[]`, 1)} {
		if _, err := Decode([]byte(s)); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	if _, err := Decode([]byte(minimal)); err != nil {
		t.Fatal(err)
	}
}
func TestDurationAndDefaults(t *testing.T) {
	for _, s := range []string{`-1`, `"-1s"`, `"25h"`, `true`, `"bad"`} {
		var d Duration
		if json.Unmarshal([]byte(s), &d) == nil {
			t.Fatal(s)
		}
	}
	c := Config{Routes: []Route{{ID: "a", RequestTimeout: D(0)}}, Upstreams: []Upstream{{ID: "u"}}}
	e, err := c.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if e.Routes[0].RequestTimeout == nil || *e.Routes[0].RequestTimeout != 0 || time.Duration(*e.Upstreams[0].DialTimeout) != 3*time.Second || c.Upstreams[0].DialTimeout != nil {
		t.Fatal("defaults mutated configured values or lost zero")
	}
}
