package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientIsGenerationCheckedHTTP(t *testing.T) {
	t.Setenv("ROUTETUS_CLI_TEST_TOKEN", strings.Repeat("a", 32))
	called := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "PUT" || r.URL.Path != "/v1/config" || r.Header.Get("If-Match") != `"g-0"` || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 32) {
			t.Error(r.Method, r.URL, r.Header)
		}
		_, _ = w.Write([]byte(`{"status":"committed"}`))
	}))
	defer s.Close()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := Run(context.Background(), []string{"config", "apply", "--file", p, "--api", s.URL, "--token-env", "ROUTETUS_CLI_TEST_TOKEN", "--generation", "0"}, &output, &output)
	if err != nil || !called || !strings.Contains(output.String(), "committed") {
		t.Fatal(err, output.String())
	}
	called = false
	if Run(context.Background(), []string{"config", "apply", "--file", p, "--api", s.URL, "--token-env", "ROUTETUS_CLI_TEST_TOKEN"}, &output, &output) == nil || called {
		t.Fatal("blind overwrite attempted")
	}
}

func TestClientConnectionFailureReasons(t *testing.T) {
	t.Setenv("ROUTETUS_CLI_TEST_TOKEN", strings.Repeat("private-token", 3))
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS server received request") }))
	defer tlsServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	for _, tc := range []struct{ api, want string }{
		{tlsServer.URL, "TLS certificate verification failed"},
		{"http://" + address, "connection refused"},
	} {
		var output bytes.Buffer
		err := Run(context.Background(), []string{"config", "status", "--api", tc.api, "--token-env", "ROUTETUS_CLI_TEST_TOKEN"}, &output, &output)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("error=%v; want %s", err, tc.want)
		}
		if strings.Contains(err.Error()+output.String(), "private-token") {
			t.Fatal("token leaked")
		}
	}
}

func TestClientURLTrustBoundary(t *testing.T) {
	t.Setenv("ROUTETUS_CLI_TEST_TOKEN", strings.Repeat("t", 32))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, address := range []string{"http://example.com:9080", "http://192.0.2.1:9080", "http://127.0.0.1", "https://user:private-password@example.com", "https://example.com?token=private-token", "https://example.com/#secret"} {
		var out bytes.Buffer
		err := Run(ctx, []string{"config", "status", "--api", address, "--token-env", "ROUTETUS_CLI_TEST_TOKEN"}, &out, &out)
		if err == nil || !strings.Contains(err.Error(), "API URL must use HTTPS") {
			t.Fatalf("unsafe URL was not rejected before request: %q err=%v", address, err)
		}
		if strings.Contains(err.Error()+out.String(), "private-") {
			t.Fatal("URL secret leaked")
		}
	}
	for _, address := range []string{"https://example.com:9080", "http://127.0.0.1:9080", "http://[::1]:9080"} {
		var out bytes.Buffer
		err := Run(ctx, []string{"config", "status", "--api", address, "--token-env", "ROUTETUS_CLI_TEST_TOKEN"}, &out, &out)
		if err == nil || !strings.Contains(err.Error(), "request canceled") {
			t.Fatalf("safe URL did not reach canceled request: %q err=%v", address, err)
		}
	}
}
