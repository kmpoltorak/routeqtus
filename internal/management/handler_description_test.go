package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestRegisteredHandlersExposeDescriptions(t *testing.T) {
	token := strings.Repeat("t", 32)
	server := httptest.NewServer(New(&configservice.Service{Compiler: testutil.Compiler(t)}, token, "test", 1<<20, 4, nil))
	defer server.Close()
	request, err := http.NewRequest("GET", server.URL+"/v1/handler-types", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var registered []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || len(registered) == 0 {
		t.Fatalf("unexpected registry: %d %+v", response.StatusCode, registered)
	}
	for _, handler := range registered {
		if handler.Description == "" {
			t.Errorf("handler %s has no description", handler.Name)
		}
	}
}
