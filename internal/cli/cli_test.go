package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientIsGenerationCheckedHTTP(t *testing.T) {
	t.Setenv("ROUTEQTUS_CLI_TEST_TOKEN", strings.Repeat("a", 32))
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
	err := Run(context.Background(), []string{"config", "apply", "--file", p, "--api", s.URL, "--token-env", "ROUTEQTUS_CLI_TEST_TOKEN", "--generation", "0"}, &output, &output)
	if err != nil || !called || !strings.Contains(output.String(), "committed") {
		t.Fatal(err, output.String())
	}
	called = false
	if Run(context.Background(), []string{"config", "apply", "--file", p, "--api", s.URL, "--token-env", "ROUTEQTUS_CLI_TEST_TOKEN"}, &output, &output) == nil || called {
		t.Fatal("blind overwrite attempted")
	}
}
