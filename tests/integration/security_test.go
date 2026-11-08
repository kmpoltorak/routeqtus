//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kmpoltorak/routetus/internal/app"
	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/configservice"
	"github.com/kmpoltorak/routetus/internal/management"
	"github.com/kmpoltorak/routetus/internal/postgres"
	"github.com/kmpoltorak/routetus/internal/testutil"
)

func TestProcessTokenMinimum(t *testing.T) {
	s := database(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"management", "admin"} {
		for _, n := range []int{0, 31, 32} {
			t.Run(fmt.Sprintf("%s-%d", kind, n), func(t *testing.T) {
				c := bootstrap.Defaults()
				c.Postgres.DSNEnv = "ROUTETUS_INTEGRATION_DSN"
				c.Admin.Listen = freeAddress(t)
				c.Management.Listen = freeAddress(t)
				c.Management.AuthTokenEnv = "ROUTETUS_TEST_MANAGE_TOKEN"
				c.Admin.AuthTokenEnv = "ROUTETUS_TEST_ADMIN_TOKEN"
				t.Setenv(c.Management.AuthTokenEnv, strings.Repeat("m", 32))
				t.Setenv(c.Admin.AuthTokenEnv, strings.Repeat("a", 32))
				env := c.Management.AuthTokenEnv
				if kind == "admin" {
					env = c.Admin.AuthTokenEnv
				}
				t.Setenv(env, strings.Repeat("x", n))
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if n < 32 {
					err := app.Run(ctx, "manage", c, testutil.Compiler(t).Logger)
					if err == nil || !strings.Contains(err.Error(), kind+" token environment reference must provide at least 32 bytes") {
						t.Fatalf("short %s token accepted or wrong error: %v", kind, err)
					}
					return
				}
				done := make(chan error, 1)
				go func() { done <- app.Run(ctx, "manage", c, testutil.Compiler(t).Logger) }()
				client := &http.Client{Timeout: time.Second}
				eventually(t, time.Second, func() bool {
					r, _ := http.NewRequest("GET", "http://"+c.Admin.Listen+"/healthz", nil)
					v := strings.Repeat("a", 32)
					if kind == "admin" {
						v = strings.Repeat("x", 32)
					}
					r.Header.Set("Authorization", "Bearer "+v)
					res, e := client.Do(r)
					if e != nil {
						return false
					}
					res.Body.Close()
					return res.StatusCode == 200
				})
				cancel()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestPublicationMetadataByteLimits(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 32)
	api := httptest.NewServer(management.New(&configservice.Service{Store: s, Compiler: testutil.Compiler(t)}, token, "metadata-test", 1<<20, 8, nil))
	defer api.Close()
	for _, tc := range []struct {
		author, description string
		valid               bool
	}{
		{strings.Repeat("a", 256), strings.Repeat("d", 4096), true},
		{strings.Repeat("a", 257), "", false}, {"", strings.Repeat("d", 4097), false},
		{strings.Repeat("é", 128), strings.Repeat("é", 2048), true},
		{strings.Repeat("é", 128) + "a", "", false}, {"", strings.Repeat("é", 2048) + "a", false},
	} {
		before, e := s.State(ctx)
		if e != nil {
			t.Fatal(e)
		}
		data, _ := json.Marshal(postgres.Empty())
		r, _ := http.NewRequest("PUT", api.URL+"/v1/config", bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("If-Match", management.ETag(before.Generation))
		r.Header.Set("X-Revision-Author", tc.author)
		r.Header.Set("X-Revision-Description", tc.description)
		res, e := api.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		want := 422
		if tc.valid {
			want = 200
		}
		if res.StatusCode != want {
			t.Fatalf("author=%d description=%d status=%d", len(tc.author), len(tc.description), res.StatusCode)
		}
		after, e := s.State(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if tc.valid {
			if after.Generation != before.Generation+1 {
				t.Fatal("valid metadata did not publish")
			}
		} else if after != before {
			t.Fatal("oversized metadata changed generation")
		}
		var rows int64
		if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM routetus.config_revisions").Scan(&rows); e != nil || rows != after.Generation {
			t.Fatal("failed metadata leaked revision", rows, e)
		}
	}
}

func TestHistoryReturnsLatest100AndReadsOlderID(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := configservice.Service{Store: s, Compiler: testutil.Compiler(t)}
	var first int64
	for i := int64(0); i < 105; i++ {
		p, err := svc.Publish(ctx, postgres.Empty(), i, postgres.Metadata{Principal: "history-test"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = p.RevisionID
		}
	}
	history, err := s.History(ctx)
	if err != nil || len(history) != 100 {
		t.Fatal("history is not latest 100", len(history), err)
	}
	for i, r := range history {
		if r.Generation != 105-int64(i) {
			t.Fatal("history order", i, r.Generation)
		}
	}
	old, err := s.Revision(ctx, first)
	if err != nil || old.SchemaVersion != 1 || len(old.Routes) != 0 {
		t.Fatal("older known revision unreadable", old, err)
	}
}
