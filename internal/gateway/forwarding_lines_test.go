package gateway_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestForwardingFieldLines(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s", r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Forwarded-Proto"))
	}))
	defer backend.Close()
	for _, tc := range []struct {
		name    string
		trusted bool
		forIP   []string
		proto   []string
		want    string
	}{
		{"separate-lines", true, []string{"203.0.113.66", "198.51.100.7"}, []string{"https"}, "198.51.100.7|https"},
		{"comma-chain", true, []string{"203.0.113.66, 198.51.100.7"}, []string{"http"}, "198.51.100.7|http"},
		{"trusted-hop", true, []string{"198.51.100.7", "192.0.2.2"}, []string{"https"}, "198.51.100.7|https"},
		{"invalid-nearest-hop", true, []string{"203.0.113.66", "invalid"}, nil, "192.0.2.1|http"},
		{"ambiguous-proto-lines", true, nil, []string{"https", "http"}, "192.0.2.1|http"},
		{"duplicate-proto-lines", true, nil, []string{"https", "https"}, "192.0.2.1|http"},
		{"ambiguous-proto-list", true, nil, []string{"https, http"}, "192.0.2.1|http"},
		{"untrusted-peer", false, []string{"203.0.113.66", "198.51.100.7"}, []string{"https"}, "192.0.2.1|http"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testutil.Config(backend.URL)
			if tc.trusted {
				c.TrustedProxies = []string{"192.0.2.0/24"}
			}
			s := compiled(t, c)
			r := httptest.NewRequest("GET", "http://example.com/", nil)
			r.RemoteAddr = "192.0.2.1:1234"
			for _, value := range tc.forIP {
				r.Header.Add("X-Forwarded-For", value)
			}
			for _, value := range tc.proto {
				r.Header.Add("X-Forwarded-Proto", value)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != 200 || w.Body.String() != tc.want {
				t.Fatalf("status=%d forwarded=%q, want %q", w.Code, w.Body.String(), tc.want)
			}
		})
	}
}
