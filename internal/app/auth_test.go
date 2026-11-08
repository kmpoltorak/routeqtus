package app

import (
	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiagnosticsAuthentication(t *testing.T) {
	token := strings.Repeat("private-admin-token", 2)
	c := bootstrap.Defaults()
	c.Admin.Pprof = true
	h := diagnostics(c, token, nil, nil)
	for _, path := range []string{"/healthz", "/readyz", "/status", "/metrics", "/debug/pprof/"} {
		for _, auth := range []string{"", "Bearer wrong", "Basic " + token, "bearer " + token} {
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("Authorization", auth)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" || strings.Contains(w.Body.String(), token) {
				t.Fatalf("%s status=%d headers=%v body=%q", path, w.Code, w.Header(), w.Body.String())
			}
		}
	}
	r := httptest.NewRequest("GET", "/healthz", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "alive\n" {
		t.Fatal(w.Code, w.Body.String())
	}
}
