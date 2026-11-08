//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kmpoltorak/routetus/internal/app"
	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/postgres"
	"github.com/kmpoltorak/routetus/internal/runtime"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestDatabaseOutageAndNoRequestSQL(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	svc := configservice.Service{Store: s, Compiler: compiler}
	if _, err := svc.Publish(ctx, testutil.Config(backend.URL), 0, postgres.Metadata{Principal: "outage-test"}); err != nil {
		t.Fatal(err)
	}
	c := bootstrap.Defaults().Sync
	c.Notifications = false
	c.PollInterval = "1h"
	c.MaxStaleness = "500ms"
	// Only the outage phase needs a short query deadline; schema setup uses its normal budget.
	s.Timeout = 50 * time.Millisecond
	rt := runtime.New(s, compiler, c, compiler.Logger)
	runctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer rt.Close()
	go rt.Run(runctx)
	eventually(t, time.Second, rt.Ready)
	before := s.Pool.Stat().AcquireCount()
	requests := 0
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		requests++
	}
	if after := s.Pool.Stat().AcquireCount(); after != before {
		t.Fatalf("%d requests caused %d configuration SQL acquisitions", requests, after-before)
	}
	t.Logf("%d proxy requests caused zero configuration SQL acquisitions", requests)
	rt.Signal()
	eventually(t, time.Second, func() bool { return rt.Status().AgeSeconds < .03 })
	admin, err := pgx.Connect(ctx, os.Getenv("ROUTETUS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	db := s.Pool.Config().ConnConfig.Database
	if _, err = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS false"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS true")
	if _, err = admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1", db); err != nil {
		t.Fatal(err)
	}
	rt.Signal()
	eventually(t, time.Second, func() bool { return rt.Status().Error != "" })
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatal("last-known-good not served", w.Code)
	}
	eventually(t, time.Second, func() bool { return !rt.Ready() })
	w = httptest.NewRecorder()
	rt.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	if _, err = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS true"); err != nil {
		t.Fatal(err)
	}
	rt.Signal()
	eventually(t, 2*time.Second, rt.Ready)
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_ = l.Close()
	return address
}
func TestFirstRunAndManagementRestart(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	b := bootstrap.Defaults()
	b.Postgres.DSNEnv = "ROUTETUS_INTEGRATION_DSN"
	b.Postgres.QueryTimeout = "1s"
	b.Server.Listen = freeAddress(t)
	b.Admin.Listen = freeAddress(t)
	b.Management.Listen = freeAddress(t)
	b.Sync.PollInterval = "30ms"
	b.Sync.StartupTimeout = "500ms"
	b.Server.ShutdownTimeout = "500ms"
	token := strings.Repeat("m", 32)
	b.Management.AuthTokenEnv = "ROUTETUS_PROCESS_TOKEN"
	t.Setenv(b.Management.AuthTokenEnv, token)
	logger := testutil.Compiler(t).Logger
	start := time.Now()
	if err := app.Run(ctx, "serve", b, logger); err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("serve accepted an empty database or failed unboundedly", err)
	}
	manageCtx, stopManage := context.WithCancel(ctx)
	manageDone := make(chan error, 1)
	go func() { manageDone <- app.Run(manageCtx, "manage", b, logger) }()
	defer stopManage()
	client := &http.Client{Timeout: time.Second}
	eventually(t, 2*time.Second, func() bool {
		req, _ := http.NewRequest("GET", "http://"+b.Management.Listen+"/readyz", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req)
		if err != nil {
			return false
		}
		res.Body.Close()
		return res.StatusCode == 200
	})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer backend.Close()
	c := testutil.Config(backend.URL)
	raw, _ := json.Marshal(c)
	req, _ := http.NewRequest("PUT", "http://"+b.Management.Listen+"/v1/config", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-Match", `"g-0"`)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	serveBoot := b
	serveBoot.Admin.Listen = freeAddress(t)
	serveBoot.Management.AuthTokenEnv = "ROUTETUS_UNUSED_SERVE_TOKEN"
	serveCtx, stopServe := context.WithCancel(ctx)
	serveDone := make(chan error, 1)
	go func() { serveDone <- app.Run(serveCtx, "serve", serveBoot, logger) }()
	defer stopServe()
	getProxy := func() bool {
		res, err := client.Get("http://" + b.Server.Listen + "/")
		if err != nil {
			return false
		}
		defer res.Body.Close()
		return res.StatusCode == 200
	}
	eventually(t, 2*time.Second, getProxy)
	stopManage()
	if err = <-manageDone; err != nil {
		t.Fatal(err)
	}
	if !getProxy() {
		t.Fatal("manage shutdown interrupted serve")
	}
	manageCtx, stopManage = context.WithCancel(ctx)
	defer stopManage()
	go func() { manageDone <- app.Run(manageCtx, "manage", b, logger) }()
	eventually(t, 2*time.Second, func() bool {
		res, err := client.Get("http://" + b.Admin.Listen + "/healthz")
		if err != nil {
			return false
		}
		res.Body.Close()
		return res.StatusCode == 200
	})
	if !getProxy() {
		t.Fatal("manage restart interrupted serve")
	}
	stopManage()
	stopServe()
	if err = <-manageDone; err != nil {
		t.Fatal(err)
	}
	if err = <-serveDone; err != nil {
		t.Fatal(err)
	}
	missing := b
	missing.Postgres.DSNEnv = "ROUTETUS_MISSING_DSN"
	t.Setenv(missing.Postgres.DSNEnv, "postgres://postgres@127.0.0.1:1/routetus?sslmode=disable")
	missing.Sync.StartupTimeout = "50ms"
	if err = app.Run(ctx, "serve", missing, logger); err == nil {
		t.Fatal("start without database accepted")
	}
}
