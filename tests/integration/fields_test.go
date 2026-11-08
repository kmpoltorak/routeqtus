//go:build integration

package integration

import (
	"bytes"
	"cmp"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/configservice"
	"github.com/kmpoltorak/routeqtus/internal/management"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestEveryOperationalFieldRoundTripsThroughAPIAndPostgres(t *testing.T) {
	s := database(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw})
	key, err := x509.MarshalPKCS8PrivateKey(backend.TLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROUTEQTUS_TEST_CA", string(certificate))
	t.Setenv("ROUTEQTUS_TEST_CERT", string(certificate))
	t.Setenv("ROUTEQTUS_TEST_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})))
	c := testutil.Config(backend.URL)
	c.Metrics = config.B(false)
	c.TrustedProxies = []string{"198.51.100.0/24"}
	u := &c.Upstreams[0]
	u.DialTimeout = config.D(0)
	u.TLSHandshakeTimeout = config.D(time.Second)
	u.ResponseHeaderTimeout = config.D(2 * time.Second)
	u.IdleTimeout = config.D(3 * time.Second)
	u.MaxConns = 7
	u.MaxIdleConns = 9
	u.MaxIdleConnsPerHost = 2
	u.ProxyFromEnvironment = true
	u.TLS = config.TLS{ServerName: "example.com", CAEnv: "ROUTEQTUS_TEST_CA", CertEnv: "ROUTEQTUS_TEST_CERT", KeyEnv: "ROUTEQTUS_TEST_KEY"}
	r := &c.Routes[0]
	r.Host = "example.com"
	r.Methods = []string{"GET", "POST"}
	r.Path = "/api"
	r.Prefix = true
	r.Priority = 42
	r.StripPrefix = true
	r.HostHeader = "example.net:8081"
	r.RequestTimeout = config.D(0)
	r.Metrics = config.B(false)
	c.Handlers[0].Enabled = config.B(false)
	c.Handlers[2].Options = []byte(`{"sample_every":17}`)
	c.Handlers[3].Options = []byte(`{"request_set":{"X-Test":"request"},"request_remove":["X-Remove"],"response_set":{"X-Test":"response"},"response_remove":["X-Remove"]}`)
	c.Upstreams[0].Targets = append(c.Upstreams[0].Targets, config.Target{ID: "b", URL: backend.URL})
	second := c.Upstreams[0]
	second.ID = "secondary"
	second.Targets = []config.Target{c.Upstreams[0].Targets[1], c.Upstreams[0].Targets[0]}
	c.Upstreams = append(c.Upstreams, second)
	c.Routes = append(c.Routes, config.Route{ID: "secondary", Path: "/secondary", Upstream: "secondary", Handlers: []string{"headers", "powered"}})
	slices.SortFunc(c.Handlers, func(a, b config.Handler) int { return cmp.Compare(a.ID, b.ID) })
	compiler := testutil.Compiler(t)
	token := strings.Repeat("x", 32)
	api := httptest.NewServer(management.New(&configservice.Service{Store: s, Compiler: compiler}, token, "field-test", 1<<20, 8, nil))
	defer api.Close()
	data, _ := json.Marshal(c)
	req, _ := http.NewRequest("PUT", api.URL+"/v1/config", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-Match", `"g-0"`)
	res, err := api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	req, _ = http.NewRequest("GET", api.URL+"/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	var want, got any
	_ = json.Unmarshal(data, &want)
	_ = json.Unmarshal(body, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("configured round-trip changed fields\nwant: %s\ngot: %s", data, body)
	}
	if bytes.Contains(body, certificate) || bytes.Contains(body, key) || res.Header.Get("ETag") != `"g-1"` {
		t.Fatal("secret material exported or wrong ETag")
	}
}
