package management

import (
	"bytes"
	"os"
	"testing"

	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestPublishedContractMatchesRegistry(t *testing.T) {
	compiler := testutil.Compiler(t)
	a := New(&configservice.Service{Compiler: compiler}, "", "", 1, 1, nil)
	published, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(published), bytes.TrimSpace(a.Contract())) {
		t.Fatal("published OpenAPI differs from live registry; run make docs")
	}
}
