package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/postgres"
)

func TestManagementMetricMetadata(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://postgres@127.0.0.1:1/routetus_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	w := httptest.NewRecorder()
	diagnostics(bootstrap.Defaults(), "", &postgres.Store{Pool: pool}, nil, nil).ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	for _, want := range []string{"# HELP routetus_postgres_connections ", "# TYPE routetus_postgres_connections gauge\n", "routetus_postgres_connections 0\n"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal("missing", want, w.Body.String())
		}
	}
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/plain; version=0.0.4" {
		t.Fatal(w.Code, w.Header())
	}
}
