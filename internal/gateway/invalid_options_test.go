package gateway_test

import (
	"testing"

	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestInvalidOptionsCannotBecomeEmptySnapshot(t *testing.T) {
	c := testutil.Config("http://example.com")
	c.Handlers[0].Options = []byte(`{`)
	s, err := testutil.Compiler(t).Compile(c)
	if err == nil {
		s.Close()
		t.Fatal("invalid JSON silently became an empty snapshot")
	}
}
