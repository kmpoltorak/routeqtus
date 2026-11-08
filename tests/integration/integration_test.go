//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/configservice"
	"github.com/kmpoltorak/routeqtus/internal/management"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
	"github.com/kmpoltorak/routeqtus/internal/runtime"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func database(t *testing.T) *postgres.Store {
	t.Helper()
	dsn := os.Getenv("ROUTEQTUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("ROUTEQTUS_TEST_DATABASE_URL is required; use make test-integration")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("routeqtus_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	cfg, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Path = "/" + name
	t.Setenv("ROUTEQTUS_INTEGRATION_DSN", cfg.String())
	b := bootstrap.Defaults().Postgres
	b.DSNEnv = "ROUTEQTUS_INTEGRATION_DSN"
	b.QueryTimeout = "5s"
	s, err := postgres.Open(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func eventually(t *testing.T, timeout time.Duration, f func() bool) {
	t.Helper()
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not converge before deadline")
}
func TestControlPlaneAndReload(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.CheckSchema(ctx); err == nil {
		t.Fatal("missing migrations accepted")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Migrate(ctx) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "backend") }))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	svc := &configservice.Service{Store: s, Compiler: compiler}
	token := strings.Repeat("t", 32)
	api := httptest.NewServer(management.New(svc, token, "integration-service", 1<<20, 8))
	defer api.Close()
	request := func(method, path, etag string, body any, authorized bool) (int, http.Header, []byte) {
		t.Helper()
		var b []byte
		if body != nil {
			var err error
			b, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, _ := http.NewRequest(method, api.URL+path, bytes.NewReader(b))
		if authorized {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		res, err := api.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header, out
	}
	check := func(want int, method, path, etag string, body any) []byte {
		t.Helper()
		code, _, b := request(method, path, etag, body, true)
		if code != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, code, want, b)
		}
		return b
	}
	if code, _, _ := request("GET", "/v1/config", "", nil, false); code != 401 {
		t.Fatal(code)
	}
	check(200, "GET", "/readyz", "", nil)
	check(200, "GET", "/v1/status", "", nil)
	c := testutil.Config(backend.URL)
	c.Upstreams[0].Targets = append(c.Upstreams[0].Targets, config.Target{ID: "b", URL: backend.URL})
	check(428, "PUT", "/v1/config", "", c)
	check(412, "PUT", "/v1/config", "*", c)
	check(200, "POST", "/v1/config/validate", "", c)
	st, _ := s.State(ctx)
	if st.Generation != 0 {
		t.Fatal("dry-run published")
	}
	bad := c
	bad.Routes = append([]config.Route{}, c.Routes...)
	bad.Routes[0].Upstream = "missing"
	check(422, "PUT", "/v1/config", `"g-0"`, bad)
	st, _ = s.State(ctx)
	if st.Generation != 0 {
		t.Fatal("invalid revision published")
	}
	check(200, "PUT", "/v1/config", `"g-0"`, c)
	first, _ := s.State(ctx)
	if first.Generation != 1 {
		t.Fatal(first)
	}
	history, err := s.History(ctx)
	if err != nil || len(history) != 1 || history[0].Generation != 1 || history[0].ID != first.RevisionID {
		t.Fatal("publication audit lost generation", history, err)
	}
	_, loaded, err := s.Load(ctx)
	if err != nil || len(loaded.Upstreams[0].Targets) != 2 || len(loaded.GlobalHandlers) != 4 || len(loaded.Routes[0].Handlers) != 1 {
		t.Fatalf("%+v %v", loaded, err)
	}
	for _, q := range []string{"UPDATE routes SET host='example.com' WHERE revision_id=$1", "DELETE FROM handlers WHERE revision_id=$1", "UPDATE config_revisions SET description='changed' WHERE id=$1"} {
		if _, err = s.Pool.Exec(ctx, q, first.RevisionID); err == nil {
			t.Fatal("mutable published revision", q)
		}
	}
	check(200, "GET", "/v1/config/effective", "", nil)
	check(200, "GET", "/v1/handler-types", "", nil)
	check(200, "GET", "/v1/handler-types/headers/schema", "", nil)
	check(200, "GET", "/openapi.json", "", nil)
	check(200, "PATCH", "/v1/routes/demo", `"g-1"`, map[string]any{"request_timeout": "0s", "metrics": false})
	check(200, "PATCH", "/v1/routes/demo", `"g-2"`, map[string]any{"request_timeout": nil, "metrics": nil})
	_, loaded, err = s.Load(ctx)
	if err != nil || loaded.Routes[0].RequestTimeout != nil || loaded.Routes[0].Metrics != nil {
		t.Fatalf("null semantics %+v %v", loaded, err)
	}
	check(422, "PATCH", "/v1/routes/demo", `"g-3"`, map[string]any{"upstream": nil})
	check(422, "PATCH", "/v1/routes/demo", `"g-3"`, map[string]any{"unknown": false})
	check(422, "DELETE", "/v1/upstreams/demo", `"g-3"`, nil)
	check(200, "POST", "/v1/upstreams/demo/targets", `"g-3"`, config.Target{ID: "c", URL: backend.URL})
	check(409, "POST", "/v1/upstreams/demo/targets", `"g-4"`, config.Target{ID: "c", URL: backend.URL})
	check(200, "DELETE", "/v1/upstreams/demo/targets/c", `"g-4"`, nil)
	check(200, "POST", "/v1/rollback", `"g-5"`, map[string]any{"revision_id": first.RevisionID})
	st, _ = s.State(ctx)
	if st.Generation != 6 {
		t.Fatal("rollback did not increase generation", st)
	}
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := svc.Publish(ctx, c, 6, postgres.Metadata{Principal: "concurrent"})
			if err == nil && p.Generation == 7 {
				results <- 200
			} else if err == postgres.ErrPrecondition {
				results <- 412
			} else {
				results <- 500
			}
		}()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for n := range results {
		counts[n]++
	}
	if counts[200] != 1 || counts[412] != 1 {
		t.Fatal(counts)
	}
	var auditedPrincipal string
	if err = s.Pool.QueryRow(ctx, "SELECT principal FROM config_revisions WHERE id=$1", first.RevisionID).Scan(&auditedPrincipal); err != nil || auditedPrincipal != "integration-service" {
		t.Fatalf("%s %v", auditedPrincipal, err)
	}
	syncConfig := bootstrap.Defaults().Sync
	syncConfig.PollInterval = "40ms"
	syncConfig.MaxStaleness = "250ms"
	runctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rt1 := runtime.New(s, compiler, syncConfig, compiler.Logger)
	rt2 := runtime.New(s, compiler, syncConfig, compiler.Logger)
	defer rt1.Close()
	defer rt2.Close()
	go rt1.Run(runctx)
	go rt2.Run(runctx)
	eventually(t, 3*time.Second, func() bool { return rt1.Ready() && rt2.Ready() })
	front := httptest.NewServer(rt1)
	defer front.Close()
	var failures atomic.Int64
	trafficCtx, trafficCancel := context.WithCancel(ctx)
	var traffic sync.WaitGroup
	for i := 0; i < 4; i++ {
		traffic.Add(1)
		go func() {
			defer traffic.Done()
			for trafficCtx.Err() == nil {
				res, err := front.Client().Get(front.URL)
				if err != nil {
					failures.Add(1)
					continue
				}
				b, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if res.StatusCode != 200 || string(b) != "backend" {
					failures.Add(1)
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		st, _ = s.State(ctx)
		if _, err = svc.Publish(ctx, c, st.Generation, postgres.Metadata{Principal: "reload"}); err != nil {
			t.Fatal(err)
		}
	}
	st, _ = s.State(ctx)
	eventually(t, 3*time.Second, func() bool { return rt1.Status().Applied == st.Generation && rt2.Status().Applied == st.Generation })
	trafficCancel()
	traffic.Wait()
	if failures.Load() != 0 {
		t.Fatalf("reload caused %d failures", failures.Load())
	}
	// A poisoned revision is inserted through privileged test SQL, bypassing the API.
	var poison int64
	if err = s.Pool.QueryRow(ctx, "INSERT INTO config_revisions(schema_version,principal,generation) VALUES(1,'poison',$1) RETURNING id", st.Generation+1).Scan(&poison); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "INSERT INTO handlers VALUES($1,'bad','unknown',1,true,'{}');", poison); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE config_revisions SET sealed=true WHERE id=$1", poison); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE gateway_state SET active_revision=$1,generation=generation+1", poison); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, func() bool { return rt1.Status().Desired == st.Generation+1 })
	if rt1.Status().Applied != st.Generation {
		t.Fatal("invalid revision replaced active snapshot")
	}
	eventually(t, 2*time.Second, func() bool { return !rt1.Ready() })
	res, err := front.Client().Get(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatal("stale gateway accepted request", res.StatusCode)
	}
	if _, err = svc.Rollback(ctx, first.RevisionID, st.Generation+1, postgres.Metadata{Principal: "restore"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, func() bool { return rt1.Ready() && rt2.Ready() })
	// Drop notification sessions; periodic polling and reconnect must restore convergence.
	_, err = s.Pool.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND query='LISTEN routeqtus_config'")
	if err != nil {
		t.Fatal(err)
	}
	st, _ = s.State(ctx)
	if _, err = svc.Publish(ctx, c, st.Generation, postgres.Metadata{Principal: "after-reconnect"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool { return rt1.Status().Applied == st.Generation+1 && rt2.Status().Applied == st.Generation+1 })
	metrics := httptest.NewRecorder()
	rt1.Metrics(metrics)
	if !strings.Contains(metrics.Body.String(), "routeqtus_config_applied_generation") {
		t.Fatal(metrics.Body.String())
	}
}

func TestPoolDeadlineAndRelationalConstraints(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	acquired := make([]interface{ Release() }, 0, 4)
	for i := 0; i < 4; i++ {
		c, err := s.Pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		acquired = append(acquired, c)
	}
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := s.Ping(deadline); err == nil {
		t.Fatal("exhausted pool ignored context")
	}
	for _, c := range acquired {
		c.Release()
	}
	var a, b int64
	for _, id := range []*int64{&a, &b} {
		if err := s.Pool.QueryRow(ctx, "INSERT INTO config_revisions(schema_version,principal) VALUES(1,'test') RETURNING id").Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Pool.Exec(ctx, "INSERT INTO handlers VALUES($1,'h','headers',1,true,'{}')", a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, "INSERT INTO handler_bindings VALUES($1,NULL,0,'h')", b); err == nil {
		t.Fatal("cross-revision handler reference accepted")
	}
	inj := "quote'; DROP TABLE routeqtus.handlers;--"
	compiler := testutil.Compiler(t)
	svc := configservice.Service{Store: s, Compiler: compiler}
	c := testutil.Config("http://example.com")
	if _, err := svc.Publish(ctx, c, 0, postgres.Metadata{Principal: inj}); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := s.Pool.QueryRow(ctx, "SELECT principal FROM config_revisions WHERE principal=$1", inj).Scan(&got); err != nil || got != inj {
		t.Fatal("parameterized metadata failed", err)
	}
}
