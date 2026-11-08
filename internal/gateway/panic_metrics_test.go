package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestPanicMetricsPreserveWrittenStatus(t *testing.T) {
	for _, written := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-response", true: "after-response"}[written], func(t *testing.T) {
			compiler := testutil.Compiler(t)
			err := compiler.Registry.Register(middleware.Type{Name: "panic", Version: 1, Schema: config.Schema(middleware.Empty{}), Factory: func(json.RawMessage) (middleware.Middleware, error) {
				return func(http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if written {
							w.WriteHeader(200)
						}
						panic("private panic value")
					})
				}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			c := testutil.Config("http://example.com")
			c.GlobalHandlers = nil
			c.Handlers = append(c.Handlers, config.Handler{ID: "panic", Type: "panic", Version: 1, Options: []byte(`{}`)})
			c.Routes[0].Handlers = []string{"panic"}
			s, err := compiler.Compile(c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			func() {
				defer func() {
					if recover() == nil {
						t.Error("panic was swallowed")
					}
				}()
				s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
			}()
			metrics := httptest.NewRecorder()
			s.WriteMetrics(metrics)
			class := "5xx"
			if written {
				class = "2xx"
			}
			for _, want := range []string{`routetus_errors_total{route_id="demo"} 1`, `routetus_requests_total{route_id="demo",status_class="` + class + `"} 1`, `routetus_in_flight{route_id="demo"} 0`} {
				if !strings.Contains(metrics.Body.String(), want) {
					t.Fatal("missing", want, metrics.Body.String())
				}
			}
		})
	}
}
