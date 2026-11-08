package gateway_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestPercentRequestPathPolicy(t *testing.T) {
	var reached atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		fmt.Fprintf(w, "%s|%s", r.URL.Path, r.URL.RawQuery)
	}))
	defer backend.Close()
	s := compiled(t, testutil.Config(backend.URL))
	front := httptest.NewServer(s)
	defer front.Close()
	allowed := int32(0)
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/literal%25", 400, ""}, {"/literal%2525", 400, ""}, {"/%252fsecret", 400, ""},
		{"/a%20b", 200, "/a b|"}, {"/caf%C3%A9", 200, "/café|"},
		{"/%E2%9C%93", 200, "/✓|"}, {"/a?value=%25", 200, "/a|value=%25"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			res, err := front.Client().Get(front.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != tc.status || tc.body != "" && string(body) != tc.body {
				t.Fatalf("%d %q %v", res.StatusCode, body, err)
			}
		})
		if tc.status == 200 {
			allowed++
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.URL.Path = "/literal%"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("decoded literal percent accepted", w.Code)
	}
	if got := reached.Load(); got != allowed {
		t.Fatalf("rejected paths reached backend: got %d want %d", got, allowed)
	}
}
