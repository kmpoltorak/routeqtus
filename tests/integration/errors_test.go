//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/runtime"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/management"
	"github.com/kmpoltorak/routetus/internal/postgres"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestStoredConversionFailureDoesNotPublish(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := postgres.Empty()
	c.Handlers = []config.Handler{{ID: "bad", Type: "powered_by", Version: 1, Options: json.RawMessage(`{"value":1e10000}`)}}
	p, err := s.Publish(ctx, c, 0, postgres.Metadata{Principal: "conversion-test"})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 32)
	api := management.New(&configservice.Service{Store: s, Compiler: testutil.Compiler(t)}, token, "conversion-test", 1<<20, 8, nil)
	for _, method := range []string{"GET", "PATCH"} {
		r := httptest.NewRequest(method, "/v1/handlers/bad", strings.NewReader(`{"options":{"value":"fixed"}}`))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("If-Match", management.ETag(p.Generation))
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"store_unavailable"`) {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "1e10000") {
			t.Fatal("stored data exposed in error")
		}
	}
	st, err := s.State(ctx)
	if err != nil || st.Generation != 1 || st.RevisionID != p.RevisionID {
		t.Fatal("conversion changed publication", st, err)
	}
	h, err := s.History(ctx)
	if err != nil || len(h) != 1 {
		t.Fatal("conversion leaked revision", h, err)
	}
}

func TestIndependentErrorsHaveEntityContextAndDoNotPublish(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	listener, err := s.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close(ctx)
	token := strings.Repeat("t", 32)
	api := management.New(&configservice.Service{Store: s, Compiler: testutil.Compiler(t)}, token, "errors-test", 1<<20, 8, nil)
	for _, phase := range []string{"upstreams", "handlers", "routes"} {
		t.Run(phase, func(t *testing.T) {
			c := testutil.Config("http://example.com")
			switch phase {
			case "upstreams":
				c.Upstreams = []config.Upstream{{ID: "first", Targets: []config.Target{}}, {ID: "second", Targets: []config.Target{}}}
			case "handlers":
				c.Handlers = []config.Handler{{ID: "first", Type: "missing", Version: 1, Options: json.RawMessage(`{}`)}, {ID: "second", Type: "missing", Version: 1, Options: json.RawMessage(`{}`)}}
			case "routes":
				c.Routes = []config.Route{{ID: "first", Path: "bad", Upstream: "demo", Handlers: []string{}}, {ID: "second", Path: "bad", Upstream: "demo", Handlers: []string{}}}
			}
			data, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("PUT", "/v1/config", bytes.NewReader(data))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("If-Match", `"g-0"`)
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			var p struct {
				Fields []map[string]string `json:"fields"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			if w.Code != 422 || len(p.Fields) != 2 {
				t.Fatalf("independent errors missing: %d %s", w.Code, w.Body.String())
			}
			key := strings.TrimSuffix(phase, "s") + "_id"
			for i, id := range []string{"first", "second"} {
				if p.Fields[i][key] != id || !strings.HasPrefix(p.Fields[i]["path"], fmt.Sprintf("/%s/%d", phase, i)) {
					t.Fatal("missing stable ID or JSON Pointer", p.Fields)
				}
			}
		})
	}
	st, err := s.State(ctx)
	if err != nil || st.Generation != 0 || st.RevisionID != 0 {
		t.Fatal("rejection changed publication", st, err)
	}
	h, err := s.History(ctx)
	if err != nil || len(h) != 0 {
		t.Fatal("rejection leaked revision", h, err)
	}
	wait, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if n, err := listener.WaitForNotification(wait); err == nil || n != nil {
		t.Fatal("rejection notified listeners", n, err)
	}
}

func TestRuntimeRejectionLogsRevisionAndKeepsSnapshot(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	good := testutil.Config(backend.URL)
	svc := configservice.Service{Store: s, Compiler: compiler}
	if _, err := svc.Publish(ctx, good, 0, postgres.Metadata{Principal: "rejection-test"}); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "runtime.jsonl")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	syncConfig := bootstrap.Defaults().Sync
	syncConfig.Notifications = false
	syncConfig.PollInterval = "1h"
	rt := runtime.New(s, compiler, syncConfig, slog.New(slog.NewJSONHandler(log, nil)))
	defer rt.Close()
	runctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); rt.Run(runctx) }()
	defer func() { cancel(); <-done }()
	eventually(t, time.Second, rt.Ready)
	var rejected []int64
	for i := 0; i < 3; i++ {
		bad := testutil.Config(backend.URL)
		key := "route_id=demo"
		if i < 2 {
			bad.Routes[0].Path = "/bad//path"
		} else {
			bad.Upstreams[0].Targets[0].URL = "http://secret-user:secret-password@example.com"
			key = "upstream_id=demo"
		}
		p, err := s.Publish(ctx, bad, int64(i+1), postgres.Metadata{Principal: "rejection-test"})
		if err != nil {
			t.Fatal(err)
		}
		rejected = append(rejected, p.RevisionID)
		rt.Signal()
		eventually(t, time.Second, func() bool { st := rt.Status(); return st.Desired == p.Generation && strings.Contains(st.Error, key) })
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 204 || rt.Status().Applied != 1 {
			t.Fatal("rejected revision replaced good snapshot", w.Code, rt.Status())
		}
	}
	cancel()
	<-done
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("secret-user")) || bytes.Contains(data, []byte("secret-password")) {
		t.Fatal("runtime log exposed target credentials")
	}
	var got []int64
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var entry struct {
			Message    string `json:"msg"`
			RevisionID int64  `json:"revision_id"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(entry.Message, "revision rejected:") {
			got = append(got, entry.RevisionID)
		}
	}
	if !slices.Equal(got, rejected) {
		t.Fatalf("rejection log IDs: got %v want %v", got, rejected)
	}
}
