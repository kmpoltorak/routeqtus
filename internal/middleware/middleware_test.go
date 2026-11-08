package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kmpoltorak/routeqtus/internal/config"
)

func TestLogsNeverContainRequestSecrets(t *testing.T) {
	var logs bytes.Buffer
	r := NewRegistry()
	if err := RegisterBuiltins(r, slog.New(slog.NewJSONHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	m, err := r.Build(config.Handler{ID: "log", Type: "access_log", Version: 1, Options: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://example.com/path?secret=query-value", strings.NewReader("body-value"))
	req.Header.Set("Authorization", "Bearer token-value")
	req = WithState(req, &State{RequestID: "id", RouteID: "route"})
	w := &Writer{ResponseWriter: httptest.NewRecorder(), State: Current(req)}
	m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); w.WriteHeader(204) })).ServeHTTP(w, req)
	for _, secret := range []string{"query-value", "body-value", "token-value", "Authorization"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("secret in log", logs.String())
		}
	}
	if !strings.Contains(logs.String(), `"status":204`) {
		t.Fatal(logs.String())
	}
}
func TestDisabledHandlerAddsNoWrapper(t *testing.T) {
	r := NewRegistry()
	if err := RegisterBuiltins(r, slog.Default()); err != nil {
		t.Fatal(err)
	}
	m, err := r.Build(config.Handler{ID: "id", Type: "request_id", Version: 1, Enabled: config.B(false), Options: []byte(`{}`)})
	if err != nil || m != nil {
		t.Fatalf("%v %v", m, err)
	}
}
