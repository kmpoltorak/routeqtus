package management

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestResourceConversionErrors(t *testing.T) {
	for _, raw := range []string{`{"value":`, `{"value":1e10000}`} {
		t.Run(raw, func(t *testing.T) {
			c := testutil.Config("http://example.com")
			c.Handlers[4].Options = json.RawMessage(raw)
			_, err := wire(c)
			var conversion conversionError
			if !errors.As(err, &conversion) || errors.Unwrap(err) == nil {
				t.Fatalf("conversion error was not preserved: %v", err)
			}
			if _, err := representation(c, "/v1/handlers/route-headers"); err == nil {
				t.Fatal("conversion failure returned a partial representation")
			}
			if _, err := mutate(c, "/v1/handlers/route-headers", "PUT", []byte(`{"id":"route-headers","type":"headers","version":1,"options":{"response_set":{"X-Powered-By":"fixed"}}}`)); err == nil {
				t.Fatal("conversion failure allowed a mutation")
			}
		})
	}
	c := testutil.Config("http://example.com")
	if _, err := representation(c, "/v1/handlers/route-headers"); err != nil {
		t.Fatal(err)
	}
}
