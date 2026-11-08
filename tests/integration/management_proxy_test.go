//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/app"
	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestTLSProxyToLoopbackManagement(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := bootstrap.Defaults()
	c.Postgres.DSNEnv = "ROUTEQTUS_INTEGRATION_DSN"
	c.Management.Listen, c.Admin.Listen = freeAddress(t), freeAddress(t)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("p", 32)
	t.Setenv(c.Management.AuthTokenEnv, token)
	runctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Run(runctx, "manage", c, testutil.Compiler(t).Logger) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	target, err := url.Parse("http://" + c.Management.Listen)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	front := httptest.NewTLSServer(proxy)
	defer front.Close()
	client := front.Client() // Trust only this test proxy's certificate.
	client.Timeout = time.Second
	eventually(t, 2*time.Second, func() bool {
		r, _ := http.NewRequest("GET", front.URL+"/readyz", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		res, e := client.Do(r)
		if e != nil {
			return false
		}
		res.Body.Close()
		return res.StatusCode == 200
	})
	for _, auth := range []string{"", "Bearer wrong", "Bearer " + token} {
		data, err := json.Marshal(testutil.Config("http://example.com"))
		if err != nil {
			t.Fatal(err)
		}
		r, _ := http.NewRequest("PUT", front.URL+"/v1/config", bytes.NewReader(data))
		r.Header.Set("Authorization", auth)
		r.Header.Set("If-Match", `"g-0"`)
		r.Header.Set("X-Forwarded-Proto", "https")
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := 401
		if auth == "Bearer "+token {
			want = 200
		}
		if res.TLS == nil || res.StatusCode != want {
			t.Fatalf("TLS proxy auth response: %d", res.StatusCode)
		}
		st, err := s.State(ctx)
		generation := int64(0)
		if want == 200 {
			generation = 1
		}
		if err != nil || st.Generation != generation {
			t.Fatal("proxy authentication failed to protect publication", st, err)
		}
	}
}
