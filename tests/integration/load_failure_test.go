//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRevisionDecodeFailureReleasesConnection(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	poolConfig := s.Pool.Config()
	poolConfig.MaxConns = 1
	s.Pool.Close()
	var err error
	s.Pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err = s.Pool.QueryRow(ctx, "INSERT INTO routetus.config_revisions(schema_version,principal,generation) VALUES(1,'decode-failure-test',1) RETURNING id").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, timeout string }{{"good", "1s"}, {"z-bad", "invalid-duration"}} {
		_, err = s.Pool.Exec(ctx, `INSERT INTO routetus.upstreams(revision_id,id,dial_timeout,max_conns,max_idle_conns,max_idle_conns_per_host,proxy_from_environment,tls_server_name,tls_ca_env,tls_cert_env,tls_key_env) VALUES($1,$2,$3,1,1,1,false,'','','','')`, revision, row.id, row.timeout)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE routetus.config_revisions SET sealed=true WHERE id=$1", revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Revision(ctx, revision); err == nil || !strings.Contains(err.Error(), "duration") {
		t.Fatalf("expected stored JSON duration failure, got %v", err)
	}
	if acquired := s.Pool.Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("decode error retained %d connections", acquired)
	}
	bounded, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err = s.Ping(bounded); err != nil {
		t.Fatal("single-connection pool unusable after decode failure", err)
	}
}
