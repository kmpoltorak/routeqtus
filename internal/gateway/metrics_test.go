package gateway_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestUnmatchedMetricsCannotCollideWithRouteID(t *testing.T) {
	c := testutil.Config("http://example.com")
	c.Routes[0].ID = "unmatched"
	s := compiled(t, c)
	w := httptest.NewRecorder()
	s.WriteMetrics(w)
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(w.Body.String()), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _ := strings.Cut(line, " ")
		if seen[key] {
			t.Fatal("duplicate metric series", key)
		}
		seen[key] = true
	}
}

func TestRequestMetricMetadata(t *testing.T) {
	s := compiled(t, testutil.Config("http://example.com"))
	w := httptest.NewRecorder()
	s.WriteMetrics(w)
	for name, kind := range map[string]string{"routetus_errors_total": "counter", "routetus_requests_total": "counter", "routetus_in_flight": "gauge", "routetus_request_duration_seconds": "histogram"} {
		text := w.Body.String()
		if strings.Count(text, "# TYPE "+name+" "+kind+"\n") != 1 || !strings.Contains(text, "# HELP "+name+" ") {
			t.Fatal("missing metadata", name, text)
		}
	}
}
