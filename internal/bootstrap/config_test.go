package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{{"minimal", "version: 1\n", true}, {"unknown", "routes: []\n", false}, {"duplicate", "version: 1\nversion: 1\n", false}, {"type", "server:\n  max_in_flight: nope\n", false}, {"range", "server:\n  max_in_flight: 0\n", false}, {"documents", "version: 1\n---\nversion: 1\n", false}, {"token", "management:\n  listen: ':9080'\n", false}, {"pprof", "admin:\n  pprof: true\n", false}} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "bootstrap.yaml")
			if err := os.WriteFile(p, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(p)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestNullAliasesAndOversizeRejected(t *testing.T) {
	for _, body := range []string{"version: null\n", "server: &server\n  listen: ':8080'\nadmin: *server\n"} {
		p := filepath.Join(t.TempDir(), "bootstrap.yaml")
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatal("ambiguous YAML accepted")
		}
	}
}

func TestAdminListenerTokenBoundary(t *testing.T) {
	for _, tc := range []struct {
		address, token string
		valid          bool
	}{
		{"127.0.0.1:9090", "", true}, {"[::1]:9090", "", true},
		{"0.0.0.0:9090", "", false}, {"[::]:9090", "", false},
		{"192.0.2.1:9090", "", false}, {"localhost:9090", "", false},
		{"0.0.0.0:9090", "ROUTEQTUS_ADMIN_TOKEN", true},
	} {
		p := filepath.Join(t.TempDir(), "bootstrap.yaml")
		body := "admin:\n  listen: '" + tc.address + "'\n  auth_token_env: '" + tc.token + "'\n"
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if (err == nil) != tc.valid {
			t.Fatalf("address=%s token-ref=%q err=%v", tc.address, tc.token, err)
		}
	}
}

func TestManagementTLSListenerBoundary(t *testing.T) {
	for _, tc := range []struct {
		address, cert, key string
		valid              bool
	}{
		{"127.0.0.1:9080", "", "", true}, {"[::1]:9080", "", "", true},
		{"0.0.0.0:9080", "", "", false}, {"[::]:9080", "", "", false},
		{"192.0.2.1:9080", "", "", false}, {"localhost:9080", "", "", false},
		{"0.0.0.0:9080", "cert.pem", "key.pem", true},
		{"127.0.0.1:9080", "cert.pem", "", false}, {"127.0.0.1:9080", "", "key.pem", false},
	} {
		c := Defaults()
		c.Management.Listen, c.Management.CertFile, c.Management.KeyFile = tc.address, tc.cert, tc.key
		if err := c.Validate(); (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}
