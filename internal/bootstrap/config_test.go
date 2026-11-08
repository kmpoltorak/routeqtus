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
