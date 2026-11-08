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

func TestDecodePresenceAndRawNumbers(t *testing.T) {
	type document struct {
		Value   *bool           `json:"value"`
		Payload json.RawMessage `json:"payload" required:"true"`
	}
	for _, tc := range []struct {
		data  string
		valid bool
	}{
		{`{"payload":{},"value":null}`, true},
		{`{"payload":[],"value":false}`, true},
		{`{"payload":{"count":1e300}}`, true},
		{`{"payload":{"count":1e10000}}`, false},
		{`{"payload":null}`, false},
		{`{"payload":{"x":1,"x":2}}`, false},
		{`{"Payload":{}}`, false},
		{`{"value":false}`, false},
		{`{"payload":{}} {}`, false},
	} {
		var d document
		err := DecodeTyped([]byte(tc.data), &d)
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.data, err)
		}
		if tc.data == `{"payload":[],"value":false}` && (d.Value == nil || *d.Value) {
			t.Fatal("explicit false lost")
		}
	}
}
