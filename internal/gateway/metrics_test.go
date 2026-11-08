package gateway_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestUnmatchedMetricsCannotCollideWithRouteID(t *testing.T) {
	c := testutil.Config("http://example.com")
	c.Routes[0].ID = "unmatched"
	s := compiled(t, c)
	w := httptest.NewRecorder()
	s.WriteMetrics(w)
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(w.Body.String()), "\n") {
		key, _, _ := strings.Cut(line, " ")
		if seen[key] {
			t.Fatal("duplicate metric series", key)
		}
		seen[key] = true
	}
}
