package ui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kmpoltorak/routetus/internal/ui"
)

func localUI(t *testing.T, apiURL, token string) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	handler, err := ui.New(apiURL, token, server.Listener.Addr().String())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(handler.Close)
	server.Config.Handler = handler
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestConfigReadUsesServerToken(t *testing.T) {
	token := strings.Repeat("server-only-token", 3)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/config" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Cookie") != "" {
			t.Errorf("unexpected Management API request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", 400)
			return
		}
		w.Header().Set("ETag", `"g-7"`)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"routes":[],"upstreams":[],"handlers":[]}`)
	}))
	defer api.Close()
	server := localUI(t, api.URL, token)
	request, err := http.NewRequest("GET", server.URL+"/api/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer browser-value")
	request.Header.Set("Cookie", "browser-cookie=example")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.Header.Get("ETag") != `"g-7"` || strings.Contains(string(body), token) || !strings.Contains(string(body), `"routes":[]`) {
		t.Fatalf("config response: %d %s", response.StatusCode, body)
	}
}

func TestRouteCreationPreservesPreconditionAndErrors(t *testing.T) {
	for _, status := range []int{201, 412, 422} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			token := strings.Repeat("server-only-token", 3)
			body := `{"id":"hello","path":"/hello","upstream":"demo","handlers":[]}`
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, err := io.ReadAll(r.Body)
				if err != nil || string(got) != body || r.Method != "POST" || r.URL.Path != "/v1/routes" || r.Header.Get("If-Match") != `"g-7"` || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "Bearer "+token {
					t.Errorf("unexpected creation request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("ETag", `"g-8"`)
				w.WriteHeader(status)
				io.WriteString(w, `{"message":"example response","fields":[{"path":"/path","message":"example field error"}]}`)
			}))
			defer api.Close()
			server := localUI(t, api.URL, token)
			request, err := http.NewRequest("POST", server.URL+"/api/routes", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Origin", server.URL)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("If-Match", `"g-7"`)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			content, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != status || response.Header.Get("ETag") != `"g-8"` || !strings.Contains(string(content), "example field error") || strings.Contains(string(content), token) {
				t.Fatalf("creation response: %d %s", response.StatusCode, content)
			}
		})
	}
}

func TestBrowserRequestsCannotUseLocalCredentials(t *testing.T) {
	var forwarded atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1); io.WriteString(w, `{}`) }))
	defer api.Close()
	server := localUI(t, api.URL, strings.Repeat("t", 32))
	for _, tc := range []struct {
		name, method, path, origin, contentType, fetchSite, host, body string
		duplicateOrigin                                                bool
		status                                                         int
	}{
		{name: "cross origin read", method: "GET", path: "/api/config", origin: "https://example.net", status: 403},
		{name: "cross site read", method: "GET", path: "/api/config", fetchSite: "cross-site", status: 403},
		{name: "missing origin", method: "POST", path: "/api/routes", contentType: "application/json", body: `{}`, status: 403},
		{name: "null origin", method: "POST", path: "/api/routes", origin: "null", contentType: "application/json", body: `{}`, status: 403},
		{name: "cross origin write", method: "POST", path: "/api/routes", origin: "https://example.net", contentType: "application/json", body: `{}`, status: 403},
		{name: "duplicate origin", method: "POST", path: "/api/routes", origin: server.URL, duplicateOrigin: true, contentType: "application/json", body: `{}`, status: 403},
		{name: "form content", method: "POST", path: "/api/routes", origin: server.URL, contentType: "application/x-www-form-urlencoded", body: "id=hello", status: 415},
		{name: "untrusted host", method: "GET", path: "/api/config", host: "example.net:9180", status: 403},
		{name: "unexpected endpoint", method: "GET", path: "/api/revisions", status: 404},
		{name: "unexpected mutation", method: "DELETE", path: "/api/routes", origin: server.URL, status: 405},
		{name: "large document", method: "POST", path: "/api/routes", origin: server.URL, contentType: "application/json", body: strings.Repeat(" ", (4<<20)+1), status: 413},
		{name: "patch missing origin", method: "PATCH", path: "/api/routes/hello", contentType: "application/json", body: `{}`, status: 403},
		{name: "upstream patch missing origin", method: "PATCH", path: "/api/upstreams/demo", contentType: "application/json", body: `{}`, status: 403},
		{name: "upstream delete missing origin", method: "DELETE", path: "/api/upstreams/demo", status: 403},
		{name: "patch form content", method: "PATCH", path: "/api/upstreams/demo", origin: server.URL, contentType: "text/plain", body: `{}`, status: 415},
		{name: "large patch", method: "PATCH", path: "/api/routes/hello", origin: server.URL, contentType: "application/json", body: strings.Repeat(" ", (4<<20)+1), status: 413},
		{name: "handler editing unavailable", method: "PATCH", path: "/api/handlers/example", origin: server.URL, contentType: "application/json", body: `{}`, status: 404},
		{name: "target endpoint unavailable", method: "DELETE", path: "/api/upstreams/demo/targets/a", origin: server.URL, status: 404},
		{name: "invalid upstream ID", method: "DELETE", path: "/api/upstreams/..", origin: server.URL, status: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if tc.host != "" {
				request.Host = tc.host
			}
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			if tc.duplicateOrigin {
				request.Header.Add("Origin", "https://example.net")
			}
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			if tc.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, tc.status)
			}
		})
	}
	if forwarded.Load() != 0 {
		t.Fatalf("forwarded %d forbidden requests", forwarded.Load())
	}
}

func TestLocalGUIConfiguration(t *testing.T) {
	for _, tc := range []struct {
		api, token, host string
		valid            bool
	}{
		{"http://127.0.0.1:9080", strings.Repeat("t", 32), "127.0.0.1:9180", true},
		{"https://manage.example.com", strings.Repeat("t", 32), "[::1]:9180", true},
		{"http://manage.example.com:9080", strings.Repeat("t", 32), "127.0.0.1:9180", false},
		{"https://example.com/api", strings.Repeat("t", 32), "127.0.0.1:9180", false},
		{"https://user:example@example.com", strings.Repeat("t", 32), "127.0.0.1:9180", false},
		{"https://example.com?token=example", strings.Repeat("t", 32), "127.0.0.1:9180", false},
		{"http://127.0.0.1:9080", strings.Repeat("t", 31), "127.0.0.1:9180", false},
		{"http://127.0.0.1:9080", "", "127.0.0.1:9180", false},
		{"http://127.0.0.1:9080", strings.Repeat("t", 32), "0.0.0.0:9180", false},
		{"http://127.0.0.1:9080", strings.Repeat("t", 32), "localhost:9180", false},
	} {
		handler, err := ui.New(tc.api, tc.token, tc.host)
		if handler != nil {
			handler.Close()
		}
		if (err == nil) != tc.valid {
			t.Fatalf("valid=%v: %v", tc.valid, err)
		}
	}
}

func TestStaticAssetsHaveNoCredentials(t *testing.T) {
	token := strings.Repeat("server-only-token", 3)
	server := localUI(t, "http://127.0.0.1:9080", token)
	for _, path := range []string{"/", "/app.js", "/styles.css"} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || len(body) == 0 || strings.Contains(string(body), token) || response.Header.Get("Content-Security-Policy") == "" || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("unsafe or missing asset %s: %d", path, response.StatusCode)
		}
	}
}

func TestRedirectsDoNotReceiveServerToken(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer api.Close()
	server := localUI(t, api.URL, strings.Repeat("t", 32))
	response, err := server.Client().Get(server.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 302 || leaked.Load() || response.Header.Get("Location") != "" {
		t.Fatal("Management API redirect was followed or exposed")
	}
}

func TestUntrustedManagementTLSFailsSafely(t *testing.T) {
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) }))
	defer api.Close()
	token := strings.Repeat("server-only-token", 3)
	server := localUI(t, api.URL, token)
	response, err := server.Client().Get(server.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 502 || strings.Contains(string(body), token) || strings.Contains(string(body), api.URL) || !strings.Contains(string(body), "TLS certificate verification failed") {
		t.Fatalf("unsafe TLS error: %d %s", response.StatusCode, body)
	}
}

func TestResourceCreationAndHandlerDiscovery(t *testing.T) {
	token := strings.Repeat("t", 32)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("server token missing")
		}
		if r.Method == "POST" && r.Header.Get("If-Match") != `"g-7"` {
			t.Error("generation missing")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/handler-types":
			io.WriteString(w, `[{"name":"access_log","version":1,"description":"Logs requests","schema":{"type":"object","properties":{"sample_every":{"type":"integer","default":1}}}}]`)
		case "POST /v1/upstreams", "POST /v1/handlers":
			io.WriteString(w, `{"status":"committed"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	server := localUI(t, api.URL, token)
	for _, tc := range []struct{ method, path, body string }{{"GET", "/api/handler-types", ""}, {"POST", "/api/upstreams", `{"id":"demo","targets":[{"id":"a","url":"http://example.com"}]}`}, {"POST", "/api/handlers", `{"id":"log","type":"access_log","version":1,"options":{"sample_every":2}}`}} {
		request, err := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Origin", server.URL)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", `"g-7"`)
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("%s %s: %d", tc.method, tc.path, response.StatusCode)
		}
	}
}

func TestRouteDeletionRequiresOriginAndPreservesGeneration(t *testing.T) {
	var forwarded atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		if r.Method != "DELETE" || r.URL.Path != "/v1/routes/hello" || r.Header.Get("If-Match") != `"g-7"` {
			t.Error("incorrect delete request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(412)
		io.WriteString(w, `{"message":"configuration generation changed"}`)
	}))
	defer api.Close()
	server := localUI(t, api.URL, strings.Repeat("t", 32))
	for _, tc := range []struct {
		path, origin string
		status       int
	}{{"/api/routes/hello", "", 403}, {"/api/routes/hello", "https://example.net", 403}, {"/api/routes/../config", server.URL, 404}, {"/api/routes/hello", server.URL, 412}} {
		request, err := http.NewRequest("DELETE", server.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if tc.origin != "" {
			request.Header.Set("Origin", tc.origin)
		}
		request.Header.Set("If-Match", `"g-7"`)
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("%s origin %s: %d, want %d", tc.path, tc.origin, response.StatusCode, tc.status)
		}
	}
	if forwarded.Load() != 1 {
		t.Fatalf("forwarded %d deletes, want 1", forwarded.Load())
	}
}

func TestResourceEditsAndUpstreamDeletion(t *testing.T) {
	for _, resource := range []string{"routes", "upstreams"} {
		for _, method := range []string{"PATCH", "DELETE"} {
			for _, status := range []int{200, 412, 422} {
				t.Run(resource+method+http.StatusText(status), func(t *testing.T) {
					api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != method || r.URL.Path != "/v1/"+resource+"/hello" || r.Header.Get("If-Match") != `"g-7"` || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("t", 32) {
							t.Error("mutation lost its target, credentials, or generation")
						}
						if method == "PATCH" {
							body, _ := io.ReadAll(r.Body)
							if string(body) != `{"path":"/edited"}` || r.Header.Get("Content-Type") != "application/json" {
								t.Error("patch body or content type lost")
							}
						}
						w.Header().Set("ETag", `"g-8"`)
						w.WriteHeader(status)
						io.WriteString(w, `{"message":"example","fields":[{"path":"/routes/demo/upstream","message":"unknown upstream"}]}`)
					}))
					defer api.Close()
					server := localUI(t, api.URL, strings.Repeat("t", 32))
					request, err := http.NewRequest(method, server.URL+"/api/"+resource+"/hello", strings.NewReader(`{"path":"/edited"}`))
					if err != nil {
						t.Fatal(err)
					}
					request.Header.Set("Origin", server.URL)
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("If-Match", `"g-7"`)
					response, err := server.Client().Do(request)
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					body, _ := io.ReadAll(response.Body)
					if response.StatusCode != status || response.Header.Get("ETag") != `"g-8"` || !strings.Contains(string(body), "unknown upstream") {
						t.Fatalf("mutation response: %d %s", response.StatusCode, body)
					}
				})
			}
		}
	}
}
