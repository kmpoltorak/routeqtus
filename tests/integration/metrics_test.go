//go:build integration

package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/configservice"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
	"github.com/kmpoltorak/routeqtus/internal/runtime"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

type stalledMetrics struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *stalledMetrics) Header() http.Header { return http.Header{} }
func (w *stalledMetrics) WriteHeader(int)     {}
func (w *stalledMetrics) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started); <-w.release })
	return len(p), nil
}
func TestSlowMetricsDoesNotBlockSnapshotPublication(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	compiler := testutil.Compiler(t)
	svc := configservice.Service{Store: s, Compiler: compiler}
	c := testutil.Config("http://example.com")
	if _, err := svc.Publish(ctx, c, 0, postgres.Metadata{Principal: "metrics"}); err != nil {
		t.Fatal(err)
	}
	settings := bootstrap.Defaults().Sync
	settings.PollInterval = "40ms"
	runctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rt := runtime.New(s, compiler, settings, compiler.Logger)
	defer rt.Close()
	go rt.Run(runctx)
	eventually(t, time.Second, rt.Ready)
	w := &stalledMetrics{started: make(chan struct{}), release: make(chan struct{})}
	defer close(w.release)
	go rt.Metrics(w)
	<-w.started
	if _, err := svc.Publish(ctx, c, 1, postgres.Metadata{Principal: "metrics"}); err != nil {
		t.Fatal(err)
	}
	rt.Signal()
	applied := make(chan struct{})
	go func() {
		for rt.Status().Applied != 2 {
			time.Sleep(10 * time.Millisecond)
		}
		close(applied)
	}()
	select {
	case <-applied:
	case <-time.After(time.Second):
		t.Fatal("a slow metrics client held the snapshot publication lock")
	}
}
