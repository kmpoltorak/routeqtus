package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutetusUsage(t *testing.T) {
	var out bytes.Buffer
	err := Run(context.Background(), nil, &out, &out)
	if err == nil || !strings.HasPrefix(err.Error(), "usage: routetus ") {
		t.Fatalf("unexpected CLI usage: %v", err)
	}
}

func TestDefaultManagementTokenEnvironment(t *testing.T) {
	token := strings.Repeat("rename-test-token", 3)
	t.Setenv("ROUTETUS_MANAGEMENT_TOKEN", token)
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/v1/status" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("default token environment did not authenticate request")
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"generation":7}`))
	}))
	defer server.Close()
	var out bytes.Buffer
	err := Run(context.Background(), []string{"config", "status", "--api", server.URL}, &out, &out)
	if err != nil || !called || !strings.Contains(out.String(), `"generation":7`) {
		t.Fatalf("default token request failed: called=%v err=%v", called, err)
	}
}
