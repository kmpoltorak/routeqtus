package gateway_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestResponseHeaderOrderAcrossProtocols(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "audit" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			fmt.Fprint(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: audit\r\nX-Order: backend\r\nX-Remove: backend\r\n\r\n")
			return
		}
		w.Header().Set("X-Order", "backend")
		w.Header().Set("X-Remove", "backend")
		w.WriteHeader(200)
	}))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	c.Routes[0].Path, c.Routes[0].Prefix = "/api", false
	c.Handlers = append(c.Handlers,
		config.Handler{ID: "outer", Type: "headers", Version: 1, Options: []byte(`{"response_set":{"X-Order":"outer","X-Request-Id":"fake"},"response_remove":["X-Remove"]}`)},
		config.Handler{ID: "inner", Type: "headers", Version: 1, Options: []byte(`{"response_set":{"X-Order":"inner","X-Remove":"inner"}}`)},
	)
	c.GlobalHandlers = append(c.GlobalHandlers, "outer")
	c.Routes[0].Handlers = append(c.Routes[0].Handlers, "inner")
	front := httptest.NewServer(compiled(t, c))
	defer front.Close()
	for _, tc := range []struct {
		name, path string
		status     int
		upgrade    bool
	}{
		{"http", "/api", 200, false},
		{"upgrade", "/api", 101, true},
		{"local-error", "/missing", 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", front.URL+tc.path, nil)
			if tc.upgrade {
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Upgrade", "audit")
			}
			res, err := front.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status || res.Header.Get("X-Order") != "outer" || res.Header.Get("X-Remove") != "" || len(res.Header.Get("X-Request-Id")) != 32 {
				t.Fatalf("status=%d headers=%v", res.StatusCode, res.Header)
			}
		})
	}
}
