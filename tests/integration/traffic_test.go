//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/postgres"
	"github.com/kmpoltorak/routetus/internal/runtime"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestRuntimeTrafficDuringPublicationAndOutage(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s.Timeout = 500 * time.Millisecond
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	svc := configservice.Service{Store: s, Compiler: compiler}
	c := testutil.Config(backend.URL)
	if _, err := svc.Publish(ctx, c, 0, postgres.Metadata{Principal: "traffic"}); err != nil {
		t.Fatal(err)
	}
	settings := bootstrap.Defaults().Sync
	settings.PollInterval = "40ms"
	settings.MaxStaleness = "2s"
	rt := runtime.New(s, compiler, settings, compiler.Logger)
	runctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer rt.Close()
	go rt.Run(runctx)
	eventually(t, 2*time.Second, rt.Ready)
	front := httptest.NewServer(rt)
	defer front.Close()
	phase := func(name string, during func() error) {
		t.Helper()
		start := time.Now()
		var requests, failures atomic.Int64
		var mu sync.Mutex
		latency := []float64{}
		loadctx, stop := context.WithCancel(ctx)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for loadctx.Err() == nil {
					begin := time.Now()
					res, err := front.Client().Get(front.URL)
					requests.Add(1)
					if err != nil {
						failures.Add(1)
						continue
					}
					b, err := io.ReadAll(res.Body)
					res.Body.Close()
					if err != nil || res.StatusCode != 200 || string(b) != "ok" {
						failures.Add(1)
					}
					mu.Lock()
					latency = append(latency, float64(time.Since(begin))/1e6)
					mu.Unlock()
				}
			}()
		}
		err := during()
		remaining := 300*time.Millisecond - time.Since(start)
		if remaining > 0 {
			time.Sleep(remaining)
		}
		stop()
		wg.Wait()
		if err != nil {
			t.Fatal(err)
		}
		if failures.Load() != 0 || len(latency) == 0 {
			t.Fatal(name, "failures", failures.Load())
		}
		slices.Sort(latency)
		t.Logf("phase=%s concurrency=4 requests=%d errors=%d elapsed_s=%.3f rps=%.0f p50_ms=%.3f p95_ms=%.3f p99_ms=%.3f", name, requests.Load(), failures.Load(), time.Since(start).Seconds(), float64(requests.Load())/time.Since(start).Seconds(), latency[len(latency)/2], latency[int(float64(len(latency)-1)*.95)], latency[int(float64(len(latency)-1)*.99)])
	}
	phase("stable", func() error { return nil })
	phase("publication", func() error {
		for i := 0; i < 10; i++ {
			st, err := s.State(ctx)
			if err != nil {
				return err
			}
			c.Handlers[len(c.Handlers)-1].Options = []byte(fmt.Sprintf(`{"value":"generation-%d"}`, st.Generation+1))
			if _, err = svc.Publish(ctx, c, st.Generation, postgres.Metadata{Principal: "traffic"}); err != nil {
				return err
			}
		}
		return nil
	})
	st, err := s.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, func() bool { return rt.Status().Applied == st.Generation })
	admin, err := pgx.Connect(ctx, os.Getenv("ROUTETUS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	db := s.Pool.Config().ConnConfig.Database
	defer admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS true")
	phase("database-outage-last-known-good", func() error {
		if _, err = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS false"); err != nil {
			return err
		}
		_, err = admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1", db)
		rt.Signal()
		return err
	})
	if _, err = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS true"); err != nil {
		t.Fatal(err)
	}
	rt.Signal()
	eventually(t, 2*time.Second, func() bool { return rt.Status().Error == "" && rt.Ready() })
}
