package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestFieldErrorContext(t *testing.T) {
	c := Config{Routes: []Route{{ID: "route", Upstream: "backend", Handlers: []string{"bound"}}}, Upstreams: []Upstream{{ID: "pool"}}, Handlers: []Handler{{ID: "handler"}}, GlobalHandlers: []string{"global"}}
	for _, tc := range []struct{ path, route, upstream, handler string }{
		{"/routes/0/path", "route", "", ""},
		{"/routes/0/upstream", "route", "backend", ""},
		{"/routes/0/handlers/0", "route", "", "bound"},
		{"/upstreams/0/targets/0/url", "", "pool", ""},
		{"/handlers/0/options", "", "", "handler"},
		{"/global_handlers/0", "", "", "global"},
		{"/routes/-1/path", "", "", ""}, {"/routes/1/path", "", "", ""}, {"/routes/no/path", "", "", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			fields := FieldErrors(c.WithErrorContext(fmt.Errorf("wrapped: %w", Invalid(tc.path, "invalid"))))
			if len(fields) != 1 {
				t.Fatal(fields)
			}
			f := fields[0]
			if f.Path != tc.path || f.RouteID != tc.route || f.UpstreamID != tc.upstream || f.HandlerID != tc.handler {
				t.Fatalf("wrong context: %+v", f)
			}
			if tc.route != "" && !strings.Contains(f.Error(), "route_id="+tc.route) {
				t.Fatal("text error omitted context", f)
			}
		})
	}
	c.Routes[0].ID = "invalid\nID"
	c.Routes[0].Upstream = ""
	f := FieldErrors(c.WithErrorContext(Invalid("/routes/0/upstream", "invalid")))[0]
	if f.RouteID != "" || f.UpstreamID != "" {
		t.Fatal("malformed or absent IDs reported", f)
	}
	joined := fmt.Errorf("wrapped: %w", errors.Join(Invalid("/a", "first"), fmt.Errorf("child: %w", Invalid("/b", "second"))))
	fields := FieldErrors(c.WithErrorContext(joined))
	if len(fields) != 2 || fields[0].Path != "/a" || fields[1].Path != "/b" {
		t.Fatal(fields)
	}
	if FieldErrors(nil) != nil || c.WithErrorContext(nil) != nil {
		t.Fatal("nil error changed")
	}
	other := errors.New("other")
	if c.WithErrorContext(other) != other {
		t.Fatal("non-field error changed")
	}
}

func TestDecodeMissingFieldHasEntityID(t *testing.T) {
	_, err := Decode([]byte(`{"schema_version":1,"upstreams":[],"handlers":[],"global_handlers":[],"routes":[{"id":"hello","upstream":"demo","handlers":[]}]}`))
	fields := FieldErrors(err)
	if len(fields) != 1 || fields[0].Path != "/routes/0/path" || fields[0].RouteID != "hello" {
		t.Fatal("decode lost entity context", err)
	}
}
