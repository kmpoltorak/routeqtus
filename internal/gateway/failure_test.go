package gateway_test

import (
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestTruncatedStreamAbortsWithoutSecondErrorResponse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
	}))
	defer backend.Close()
	snapshot := compiled(t, testutil.Config(backend.URL))
	front := httptest.NewServer(snapshot)
	defer front.Close()
	res, err := front.Client().Get(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err == nil || res.StatusCode != 200 || string(b) != "data: first\n\n" {
		t.Fatalf("status=%d body=%q err=%v", res.StatusCode, b, err)
	}
	metrics := httptest.NewRecorder()
	snapshot.WriteMetrics(metrics)
	if !strings.Contains(metrics.Body.String(), `routeqtus_errors_total{route_id="demo"} 1`) {
		t.Fatal(metrics.Body.String())
	}
}
func TestUpstreamTLSIsVerified(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	bad := compiled(t, c)
	w := httptest.NewRecorder()
	bad.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 502 {
		t.Fatal("untrusted certificate accepted", w.Code)
	}
	t.Setenv("ROUTEQTUS_TLS_TEST_CA", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw})))
	c.Upstreams[0].TLS.CAEnv = "ROUTEQTUS_TLS_TEST_CA"
	good := compiled(t, c)
	w = httptest.NewRecorder()
	good.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 204 {
		t.Fatal("valid CA rejected", w.Code)
	}
	// httptest's certificate includes example.com; use a name outside its SANs.
	c.Upstreams[0].TLS.ServerName = "mismatch.example.net"
	bad = compiled(t, c)
	w = httptest.NewRecorder()
	bad.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 502 {
		t.Fatal("wrong TLS name accepted", w.Code)
	}
}
