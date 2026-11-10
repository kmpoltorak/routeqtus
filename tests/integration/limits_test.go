//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/postgres"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestMigrationUpgradesVersionOneSchema(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	v1, err := os.ReadFile("../../internal/postgres/migrations/001.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(v1)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if s.CheckSchema(ctx) == nil {
		t.Fatal("version 1 schema accepted without migrate")
	}
	for range 2 {
		if err = s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	var versions []int
	rows, err := s.Pool.Query(ctx, "SELECT version FROM routetus.schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v int
		if err = rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	if len(versions) != 2 || versions[0] != 1 || versions[1] != postgres.SchemaVersion {
		t.Fatalf("versions %v", versions)
	}
}

func TestRaisedConnectionLimitPersists(t *testing.T) {
	t.Cleanup(func() { config.SetLimits(config.DefaultLimits) })
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	l := config.DefaultLimits
	l.MaxConnections = 1000000
	config.SetLimits(l)
	c := testutil.Config("http://example.com")
	c.Upstreams[0].MaxConns = 1000000
	c.Upstreams[0].MaxIdleConns = 1000000
	c.Upstreams[0].MaxIdleConnsPerHost = 1000000
	svc := configservice.Service{Store: s, Compiler: testutil.Compiler(t)}
	if _, err := svc.Publish(ctx, c, 0, postgres.Metadata{Principal: "limits-test"}); err != nil {
		t.Fatal(err)
	}
	_, loaded, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u := loaded.Upstreams[0]
	if u.MaxConns != 1000000 || u.MaxIdleConns != 1000000 || u.MaxIdleConnsPerHost != 1000000 {
		t.Fatalf("loaded %+v", u)
	}
	// Published rows are immutable, so inspect the constraints instead of writing -1.
	for _, column := range []string{"max_conns", "max_idle_conns", "max_idle_conns_per_host"} {
		var definition string
		if err = s.Pool.QueryRow(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1", "upstreams_"+column+"_check").Scan(&definition); err != nil {
			t.Fatal(err)
		}
		if definition != "CHECK (("+column+" >= 0))" {
			t.Fatalf("%s constraint %q", column, definition)
		}
	}
}
