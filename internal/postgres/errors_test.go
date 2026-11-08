package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConnectionErrorsOmitServerMessages(t *testing.T) {
	for code, want := range map[string]string{"28P01": "postgres authentication failed", "28000": "postgres authentication failed", "3D000": "postgres database does not exist", "53300": "postgres connection limit reached", "unknown-private-code": "postgres server rejected connection"} {
		err := fmt.Errorf("private-DSN: %w", &pgconn.PgError{Code: code, Message: "private-password", Detail: "private-token"})
		if got := connectionFailure(err).Error(); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	if got := connectionFailure(errors.New("private-value")).Error(); got != "postgres connection failed" {
		t.Fatal(got)
	}
}
