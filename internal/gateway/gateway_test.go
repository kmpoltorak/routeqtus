package gateway_test

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/gateway"
	"github.com/kmpoltorak/routetus/internal/middleware"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func compiled(t testing.TB, c config.Config) *gateway.Snapshot {
	t.Helper()
	s, err := testutil.Compiler(t).Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func TestRouting(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s %s %s", r.Host, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("X-Trusted"))
	}))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	c.Routes = []config.Route{{ID: "api", Path: "/api", Prefix: true, StripPrefix: true, HostHeader: "example.com", Methods: []string{"GET"}, Upstream: "demo", Handlers: []string{"route-headers"}}, {ID: "exact", Path: "/api/exact", Upstream: "demo", Handlers: []string{}, Methods: []string{"POST"}}}
	s := compiled(t, c)
	for _, tc := range []struct {
		method, path string
		code         int
		body, allow  string
	}{{"GET", "/api/a%20b?x=1&x=2&v=%2F", 200, "example.com /a%20b x=1&x=2&v=%2F gateway", ""}, {"GET", "/apix", 404, "", ""}, {"PUT", "/api/exact", 405, "", "GET, POST"}, {"GET", "/api%2Fsecret", 400, "", ""}, {"GET", "/api/%252e%252e", 400, "", ""}, {"GET", "/api/../x", 400, "", ""}, {"GET", "/api/?a=1;b=2", 400, "", ""}} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("Connection", "X-Trusted, X-Request-Id, X-Forwarded-For")
			r.Header.Set("X-Trusted", "spoof")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.code || tc.body != "" && w.Body.String() != tc.body || w.Header().Get("Allow") != tc.allow {
				t.Fatalf("%d %s %v", w.Code, w.Body, w.Header())
			}
			if tc.code == 200 && (w.Header().Get("X-Powered-By") != "Routetus" || w.Header().Get("X-Gateway") != "routetus") {
				t.Fatal(w.Header())
			}
		})
	}
}
func TestValidation(t *testing.T) {
	for _, mutate := range []func(*config.Config){func(c *config.Config) { c.Routes = append(c.Routes, c.Routes[0]); c.Routes[1].ID = "conflict" }, func(c *config.Config) { c.Routes[0].Upstream = "missing" }, func(c *config.Config) { c.Handlers[0].Options = []byte(`{"unknown":true}`) }, func(c *config.Config) { c.Handlers[0].Version = 2 }, func(c *config.Config) { c.GlobalHandlers = append(c.GlobalHandlers, "missing") }, func(c *config.Config) { c.Upstreams[0].Targets[0].URL = "http://user:password@example.com" }, func(c *config.Config) { c.Handlers[3].Options = []byte(`{"request_set":{"Authorization":"secret"}}`) }} {
		c := testutil.Config("http://example.com")
		mutate(&c)
		s, err := testutil.Compiler(t).Compile(c)
		if err == nil {
			s.Close()
			t.Fatal("invalid configuration accepted")
		}
	}
}
func TestForwarding(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s|%s|%s|%s", r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Forwarded-Proto"), r.Header.Get("Forwarded"), r.Header.Get("X-Trusted"), r.Header.Get("X-Request-Id"))
	}))
	defer backend.Close()
	for _, trusted := range []bool{false, true} {
		c := testutil.Config(backend.URL)
		if trusted {
			c.TrustedProxies = []string{"192.0.2.0/24"}
		}
		s := compiled(t, c)
		r := httptest.NewRequest("GET", "http://example.com/", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.1")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Forwarded", "for=spoof")
		r.Header.Set("Connection", "X-Trusted, X-Forwarded-For, X-Request-Id")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		want := "192.0.2.1|http||gateway|"
		if trusted {
			want = "203.0.113.1|https||gateway|"
		}
		if !strings.HasPrefix(w.Body.String(), want) || len(w.Body.String()) != len(want)+32 {
			t.Fatal(w.Body.String())
		}
	}
}
func TestSSETrailersAndHTTP2(t *testing.T) {
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Trailer", "X-Final")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "data: last\n\n")
		w.Header().Set("X-Final", "done")
	}))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	c.Routes[0].RequestTimeout = config.D(0)
	front := httptest.NewUnstartedServer(compiled(t, c))
	front.EnableHTTP2 = true
	front.StartTLS()
	defer front.Close()
	res, err := front.Client().Get(front.URL)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.ProtoMajor != 2 {
		close(release)
		t.Fatal(res.Proto)
	}
	first, err := bufio.NewReader(res.Body).ReadString('\n')
	close(release)
	if err != nil || first != "data: first\n" {
		t.Fatalf("%q %v", first, err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	if res.Trailer.Get("X-Final") != "done" {
		t.Fatal(res.Trailer)
	}
}
func TestTimeoutCancellationAndBodyLimits(t *testing.T) {
	canceled := make(chan struct{}, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/body" {
			_, err := io.Copy(io.Discard, r.Body)
			if err != nil {
				return
			}
			w.WriteHeader(204)
			return
		}
		<-r.Context().Done()
		canceled <- struct{}{}
	}))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	compiler.Limits.MaxBodyBytes = 8
	c := testutil.Config(backend.URL)
	c.Routes[0].RequestTimeout = config.D(100 * time.Millisecond)
	s, err := compiler.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	front := httptest.NewServer(s)
	defer front.Close()
	res, err := front.Client().Get(front.URL + "/timeout")
	if err == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 504 {
			t.Fatalf("status=%d", res.StatusCode)
		}
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("upstream not canceled")
	}
	for _, chunked := range []bool{false, true} {
		req, _ := http.NewRequest("POST", front.URL+"/body", strings.NewReader("0123456789"))
		if chunked {
			req.ContentLength = -1
		}
		res, err = front.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 413 {
			t.Fatalf("chunked=%v status=%d", chunked, res.StatusCode)
		}
	}
}
func TestWebSocket(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("backend_tls=%v", secure), func(t *testing.T) {
			backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
				conn, rw, err := w.(http.Hijacker).Hijack()
				if err != nil {
					return
				}
				defer conn.Close()
				fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(h[:]))
				_ = rw.Flush()
				frame := make([]byte, 8)
				if _, err = io.ReadFull(rw, frame); err != nil {
					return
				}
				for i := 0; i < 2; i++ {
					frame[6+i] ^= frame[2+i%4]
				}
				_, _ = conn.Write(append([]byte{0x81, 2}, frame[6:]...))
			}))
			if secure {
				backend.EnableHTTP2 = true
				backend.StartTLS()
			} else {
				backend.Start()
			}
			defer backend.Close()
			c := testutil.Config(backend.URL)
			if secure {
				t.Setenv("ROUTETUS_WS_TEST_CA", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw})))
				c.Upstreams[0].TLS.CAEnv = "ROUTETUS_WS_TEST_CA"
			}
			c.Routes[0].RequestTimeout = config.D(0)
			front := httptest.NewServer(compiled(t, c))
			defer front.Close()
			u, _ := url.Parse(front.URL)
			conn, err := net.DialTimeout("tcp", u.Host, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
			reader := bufio.NewReader(conn)
			res, err := http.ReadResponse(reader, nil)
			if err != nil || res.StatusCode != 101 {
				t.Fatalf("%v %v", res, err)
			}
			if res.Header.Get("X-Gateway") != "routetus" || res.Header.Get("X-Request-Id") == "" {
				t.Fatal(res.Header)
			}
			_, _ = conn.Write([]byte{0x81, 0x82, 1, 2, 3, 4, 'h' ^ 1, 'i' ^ 2})
			frame := make([]byte, 4)
			if _, err = io.ReadFull(reader, frame); err != nil || string(frame[2:]) != "hi" {
				t.Fatalf("%v %v", frame, err)
			}
		})
	}
}
func TestRecoverAndHandlerOrder(t *testing.T) {
	compiler := testutil.Compiler(t)
	order := []string{}
	for _, name := range []string{"outer", "inner"} {
		name := name
		err := compiler.Registry.Register(middleware.Type{Name: name, Version: 1, Schema: config.Schema(middleware.Empty{}), Factory: func(_ json.RawMessage) (middleware.Middleware, error) {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					order = append(order, name+" in")
					defer func() { order = append(order, name+" out") }()
					if name == "inner" {
						panic("secret panic contents")
					}
					next.ServeHTTP(w, r)
				})
			}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	c := testutil.Config("http://example.com")
	for _, name := range []string{"outer", "inner"} {
		c.Handlers = append(c.Handlers, config.Handler{ID: name, Type: name, Version: 1, Options: []byte(`{}`)})
	}
	c.GlobalHandlers = []string{"recover", "outer"}
	c.Routes[0].Handlers = []string{"inner"}
	s, err := compiler.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret") || strings.Join(order, ",") != "outer in,inner in,inner out,outer out" {
		t.Fatalf("%d %v", w.Code, order)
	}
}
func FuzzRequestPath(f *testing.F) {
	for _, s := range []string{"/api", "/api%2Fa", "/a%20b", "/%252e"} {
		f.Add(s)
	}
	c := testutil.Config("http://127.0.0.1:1")
	c.Routes = nil
	s := compiled(f, c)
	f.Fuzz(func(t *testing.T, p string) {
		u, err := url.ParseRequestURI(p)
		if err != nil || !strings.HasPrefix(p, "/") {
			return
		}
		r := &http.Request{Method: "GET", URL: u, Host: "example.com", RemoteAddr: "192.0.2.1:1", Header: make(http.Header), Body: http.NoBody}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
	})
}
