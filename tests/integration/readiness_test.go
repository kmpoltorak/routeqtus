//go:build integration

package integration

import (
	"context"
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
	"github.com/kmpoltorak/routetus/internal/management"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestManagementReadinessDoesNotAcquireSQL(t *testing.T) {
	s := database(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 32)
	health := s.WatchHealth(context.Background(), time.Hour)
	defer health.Close()
	eventually(t, time.Second, health.Ready)
	api := management.New(&configservice.Service{Store: s, Compiler: testutil.Compiler(t)}, token, "ready-test", 1024, 8, health.Ready)
	before := s.Pool.Stat().AcquireCount()
	for i := 0; i < 100; i++ {
		r := httptest.NewRequest("GET", "/readyz", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if got := s.Pool.Stat().AcquireCount() - before; got != 0 {
		t.Fatalf("100 readiness probes acquired %d SQL connections", got)
	}
	health.Close()
	r := httptest.NewRequest("GET", "/readyz", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 503 || health.Ready() {
		t.Fatal("closed health monitor stayed ready", w.Code)
	}
}

func TestManageReadinessOutageAndRecovery(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := bootstrap.Defaults()
	c.Postgres.DSNEnv = "ROUTETUS_INTEGRATION_DSN"
	c.Postgres.QueryTimeout = "50ms"
	c.Sync.PollInterval = "20ms"
	c.Management.Listen = freeAddress(t)
	c.Admin.Listen = freeAddress(t)
	c.Management.AuthTokenEnv = "ROUTETUS_READINESS_TOKEN"
	token := strings.Repeat("r", 32)
	t.Setenv(c.Management.AuthTokenEnv, token)
	run, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Run(run, "manage", c, testutil.Compiler(t).Logger) }()
	defer func() {
		stop()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("manage health worker failed to stop")
		}
	}()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	both := func(want int) bool {
		for _, address := range []string{c.Management.Listen, c.Admin.Listen} {
			r, _ := http.NewRequest("GET", "http://"+address+"/readyz", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			res, err := client.Do(r)
			if err != nil {
				return false
			}
			res.Body.Close()
			if res.StatusCode != want {
				return false
			}
		}
		return true
	}
	eventually(t, 2*time.Second, func() bool { return both(200) })
	admin, err := pgx.Connect(ctx, os.Getenv("ROUTETUS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	db := pgx.Identifier{s.Pool.Config().ConnConfig.Database}.Sanitize()
	if _, err = admin.Exec(ctx, "ALTER DATABASE "+db+" ALLOW_CONNECTIONS false"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "ALTER DATABASE "+db+" ALLOW_CONNECTIONS true")
	if _, err = admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1", s.Pool.Config().ConnConfig.Database); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, func() bool { return both(503) })
	if _, err = admin.Exec(ctx, "ALTER DATABASE "+db+" ALLOW_CONNECTIONS true"); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, func() bool { return both(200) })
}
