package workflows

import (
	"go.yaml.in/yaml/v4"
	"os"
	"strings"
	"testing"
)

func TestReleasePrivilegeIsolation(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Needs       string            `yaml:"needs"`
			If          string            `yaml:"if"`
			Steps       []struct {
				Uses string         `yaml:"uses"`
				Run  string         `yaml:"run"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err = yaml.Unmarshal(data, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Permissions) != 1 || w.Permissions["contents"] != "read" {
		t.Fatal("release default permissions must be contents:read")
	}
	verify, ok := w.Jobs["verify"]
	if !ok {
		t.Fatal("missing read-only verify job")
	}
	for name, job := range w.Jobs {
		if name != "publish" {
			for _, permission := range job.Permissions {
				if permission == "write" {
					t.Fatal("write permission on verification job", name)
				}
			}
		}
	}
	upload := false
	for _, step := range verify.Steps {
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			upload = true
		}
		if strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["persist-credentials"] != false {
			t.Fatal("checkout credentials persist into verification")
		}
	}
	if !upload {
		t.Fatal("verification must provide immutable release artifacts")
	}
	publish, ok := w.Jobs["publish"]
	if !ok || publish.Needs != "verify" || publish.If != "startsWith(github.ref, 'refs/tags/')" {
		t.Fatal("publication must depend on verification and only run on tags")
	}
	if len(publish.Permissions) != 1 || publish.Permissions["contents"] != "write" {
		t.Fatal("publication requires only contents:write")
	}
	if len(publish.Steps) != 3 {
		t.Fatal("publication must only download, checksum, and publish")
	}
	for i, step := range publish.Steps {
		switch i {
		case 0:
			if !strings.HasPrefix(step.Uses, "actions/download-artifact@") || step.With["artifact-ids"] != "${{ needs.verify.outputs.artifact-id }}" {
				t.Fatal("must download the exact verified artifact")
			}
		case 1:
			if step.Run != "sha256sum --strict --check checksums.txt" {
				t.Fatal("unexpected code execution in publication checksum step")
			}
		case 2:
			if step.Run != `gh release create "$VERSION" routeqtus_"$VERSION"_linux_amd64.tar.gz routeqtus_"$VERSION"_linux_arm64.tar.gz routeqtus_"$VERSION"_darwin_amd64.tar.gz routeqtus_"$VERSION"_darwin_arm64.tar.gz checksums.txt --repo "$GITHUB_REPOSITORY" --verify-tag --title "Routeqtus $VERSION" --notes-file release_notes.md` {
				t.Fatal("unexpected code execution in publication step")
			}
		}
	}
}
