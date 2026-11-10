package gateway_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestOptionalServiceIDsAreAlphanumeric(t *testing.T) {
	for _, resource := range []string{"routes", "upstreams"} {
		for _, value := range []string{"", "Orders01", "orders-api", "orders_api", strings.Repeat("a", 64), strings.Repeat("a", 65), "orders api", "żółć"} {
			data, err := json.Marshal(testutil.Config("http://example.com"))
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			document[resource].([]any)[0].(map[string]any)["service_id"] = value
			data, err = json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			c, err := config.Decode(data)
			if err == nil {
				s, compileErr := testutil.Compiler(t).Compile(c)
				err = compileErr
				if s != nil {
					s.Close()
				}
			}
			valid := value == "" || value == "Orders01" || value == "orders-api" || value == "orders_api" || value == strings.Repeat("a", 64)
			if (err == nil) != valid {
				t.Fatalf("%s service_id=%q: %v", resource, value, err)
			}
			if !valid {
				fields := config.FieldErrors(err)
				if len(fields) != 1 || fields[0].Path != "/"+resource+"/0/service_id" {
					t.Fatalf("missing service field context: %v", err)
				}
			}
		}
	}
}
