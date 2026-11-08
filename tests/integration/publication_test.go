//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/postgres"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

type delayedReadConn struct{ net.Conn }

func (c delayedReadConn) Read(p []byte) (int, error) {
	// Charge each wire read 1ms so per-row round trips cannot hide behind loopback latency.
	time.Sleep(time.Millisecond)
	return c.Conn.Read(p)
}

func TestLargeRevisionPublication(t *testing.T) {
	for _, count := range []int{2000, 10000} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			s := database(t)
			ctx := context.Background()
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			poolConfig := s.Pool.Config()
			dial := poolConfig.ConnConfig.DialFunc
			poolConfig.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
				conn, err := dial(ctx, network, address)
				if err != nil {
					return nil, err
				}
				return delayedReadConn{conn}, nil
			}
			s.Pool.Close()
			var err error
			s.Pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
			if err != nil {
				t.Fatal(err)
			}
			s.Timeout = 2 * time.Second
			if count == 10000 {
				s.Timeout = 10 * time.Second
			}
			c := testutil.Config("http://example.com")
			c.Routes = nil
			for i := 0; i < count; i++ {
				c.Routes = append(c.Routes, config.Route{ID: fmt.Sprintf("route-%d", i), Path: fmt.Sprintf("/route-%d", i), Upstream: "demo", Handlers: []string{"powered"}})
			}
			svc := configservice.Service{Store: s, Compiler: testutil.Compiler(t)}
			start := time.Now()
			publication, err := svc.Publish(ctx, c, 0, postgres.Metadata{Principal: "large-revision-test"})
			if err != nil {
				t.Fatalf("publish %d routes within %s failed after %s: %v", count, s.Timeout, time.Since(start), err)
			}
			publishTime := time.Since(start)
			start = time.Now()
			loadedState, loaded, err := s.Load(ctx)
			if err != nil || loadedState != publication.State || len(loaded.Routes) != count {
				t.Fatalf("load: routes=%d err=%v", len(loaded.Routes), err)
			}
			t.Logf("routes=%d added-read-delay=1ms publication=%s load=%s budget=%s", count, publishTime, time.Since(start), s.Timeout)

			// A late foreign-key error must leave neither a revision nor a notification.
			listener, err := s.Listen(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close(ctx)
			bad := c
			bad.Routes = append([]config.Route(nil), c.Routes...)
			bad.Routes[len(bad.Routes)-1].Upstream = "missing"
			_, err = s.Publish(ctx, bad, publication.Generation, postgres.Metadata{Principal: "invalid-test"})
			var constraint *pgconn.PgError
			if !errors.As(err, &constraint) || constraint.Code != "23503" {
				t.Fatalf("expected late upstream foreign-key failure, got %v", err)
			}
			state, err := s.State(ctx)
			if err != nil || state != publication.State {
				t.Fatal("failed publication changed state", state, err)
			}
			var revisions int
			if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM routetus.config_revisions").Scan(&revisions); err != nil || revisions != 1 {
				t.Fatal("failed revision leaked", revisions, err)
			}
			wait, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			notification, err := listener.WaitForNotification(wait)
			cancel()
			if err == nil || notification != nil {
				t.Fatal("failed publication notified", notification, err)
			}
			// The connection must remain usable after a failed batch and rollback.
			next, err := svc.Publish(ctx, c, publication.Generation, postgres.Metadata{Principal: "retry-test"})
			if err != nil || next.Generation != publication.Generation+1 {
				t.Fatal("retry publication failed", next, err)
			}
			wait, cancel = context.WithTimeout(ctx, time.Second)
			defer cancel()
			notification, err = listener.WaitForNotification(wait)
			if err != nil || notification.Payload != strconv.FormatInt(next.Generation, 10) {
				t.Fatal("successful retry notification missing", notification, err)
			}
		})
	}
}

func TestEmptyRevisionPublication(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := configservice.Service{Store: s, Compiler: testutil.Compiler(t)}
	publication, err := svc.Publish(ctx, postgres.Empty(), 0, postgres.Metadata{Principal: "empty-test"})
	if err != nil {
		t.Fatal(err)
	}
	state, loaded, err := s.Load(ctx)
	if err != nil || state != publication.State || len(loaded.Routes) != 0 || len(loaded.Upstreams) != 0 {
		t.Fatal("empty revision failed", state, loaded, err)
	}
}
