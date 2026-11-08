package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kmpoltorak/routetus/internal/config"
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
	if !strings.Contains(logs.String(), `"status":204`) || !strings.Contains(logs.String(), `"method":"POST"`) {
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

func TestRecoverLogsStackWithoutPanicValue(t *testing.T) {
	for _, mode := range []string{"before", "after", "abort"} {
		t.Run(mode, func(t *testing.T) {
			var logs bytes.Buffer
			registry := NewRegistry()
			if err := RegisterBuiltins(registry, slog.New(slog.NewJSONHandler(&logs, nil))); err != nil {
				t.Fatal(err)
			}
			m, err := registry.Build(config.Handler{ID: "recover", Type: "recover", Version: 1, Options: []byte(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			state := &State{RequestID: "id", RouteID: "route"}
			req := WithState(httptest.NewRequest("GET", "http://example.com/?secret=query-value", nil), state)
			writer := &Writer{ResponseWriter: httptest.NewRecorder(), State: state}
			var propagated any
			func() {
				defer func() { propagated = recover() }()
				m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if mode == "after" {
						w.WriteHeader(201)
					}
					if mode == "abort" {
						panic(http.ErrAbortHandler)
					}
					panic("private-panic-value")
				})).ServeHTTP(writer, req)
			}()
			if mode == "abort" {
				if propagated != http.ErrAbortHandler || logs.Len() != 0 {
					t.Fatal(propagated, logs.String())
				}
				return
			}
			if mode == "before" && (state.Status != 500 || propagated != nil) || mode == "after" && (state.Status != 201 || propagated != http.ErrAbortHandler) {
				t.Fatal(state.Status, propagated)
			}
			if !strings.Contains(logs.String(), `"stack":`) || !strings.Contains(logs.String(), "TestRecoverLogsStackWithoutPanicValue") {
				t.Fatal("missing stack", logs.String())
			}
			for _, secret := range []string{"private-panic-value", "query-value"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("secret in panic log", logs.String())
				}
			}
		})
	}
}

func TestAccessLogPanicStatus(t *testing.T) {
	for _, written := range []bool{false, true} {
		var logs bytes.Buffer
		registry := NewRegistry()
		if err := RegisterBuiltins(registry, slog.New(slog.NewJSONHandler(&logs, nil))); err != nil {
			t.Fatal(err)
		}
		m, err := registry.Build(config.Handler{ID: "log", Type: "access_log", Version: 1, Options: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		state := &State{}
		func() {
			defer func() {
				if recover() == nil {
					t.Error("panic was swallowed")
				}
			}()
			m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if written {
					w.WriteHeader(201)
				}
				panic("private-panic-value")
			})).ServeHTTP(&Writer{ResponseWriter: httptest.NewRecorder(), State: state}, WithState(httptest.NewRequest("POST", "/", nil), state))
		}()
		want := `"status":500`
		if written {
			want = `"status":201`
		}
		if !strings.Contains(logs.String(), want) || strings.Contains(logs.String(), "private-panic-value") {
			t.Fatal(logs.String())
		}
	}
}
