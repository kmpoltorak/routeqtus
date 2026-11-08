package gateway_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestValidationReportCountAndSize(t *testing.T) {
	for _, n := range []int{0, 1, 100, 101, 10000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c := testutil.Config("http://example.com")
			c.Routes = make([]config.Route, n)
			for i := range c.Routes {
				c.Routes[i] = config.Route{ID: fmt.Sprintf("bad-%d", i), Path: "bad", Upstream: "demo"}
			}
			s, err := testutil.Compiler(t).Compile(c)
			if n == 0 {
				if err != nil {
					t.Fatal(err)
				}
				s.Close()
				return
			}
			if err == nil {
				s.Close()
				t.Fatal("invalid routes accepted")
			}
			fields := config.FieldErrors(err)
			shown := min(n, 100)
			want := shown
			if n > shown {
				want++
			}
			if len(fields) != want || len(err.Error()) > 8192 {
				t.Fatalf("fields=%d bytes=%d", len(fields), len(err.Error()))
			}
			for i, f := range fields[:shown] {
				if f.Path != fmt.Sprintf("/routes/%d/path", i) || f.RouteID != fmt.Sprintf("bad-%d", i) {
					t.Fatal("field context changed", f)
				}
			}
			if n > shown && (fields[shown].Path != "/" || fields[shown].Message != fmt.Sprintf("%d more errors omitted", n-shown)) {
				t.Fatal("wrong omitted count", fields[shown])
			}
		})
	}
}

func TestValidationReportLongDetails(t *testing.T) {
	for _, text := range []string{"é", "\x00\n\"\\", "\u2028"} {
		c := testutil.Config("http://example.com")
		c.Handlers = make([]config.Handler, 100)
		for i := range c.Handlers {
			c.Handlers[i] = config.Handler{ID: fmt.Sprintf("handler-%d", i), Type: strings.Repeat(text, 4096), Version: 1, Options: []byte(`{}`)}
		}
		_, err := testutil.Compiler(t).Compile(c)
		if err == nil {
			t.Fatal("invalid handlers accepted")
		}
		fields := config.FieldErrors(err)
		if len(err.Error()) > 8192 || !utf8.ValidString(err.Error()) {
			t.Fatalf("unbounded or invalid UTF-8 report: %d bytes", len(err.Error()))
		}
		for _, f := range fields {
			if len(f.Message) > 256 || !utf8.ValidString(f.Message) {
				t.Fatalf("unbounded or invalid detail: %d bytes", len(f.Message))
			}
		}
		encoded, e := json.Marshal(fields)
		if e != nil || len(encoded) > 64<<10 {
			t.Fatalf("encoded report exceeds budget: %d %v", len(encoded), e)
		}
		shown := len(fields) - 1
		if shown <= 0 || shown >= 100 || fields[shown].Path != "/" || fields[shown].Message != fmt.Sprintf("%d more errors omitted", 100-shown) {
			t.Fatal("byte budget lost omission count", len(fields), fields[len(fields)-1])
		}
	}
}
