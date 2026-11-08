package neterror

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestReasonsDoNotCopyErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "request canceled"},
		{context.DeadlineExceeded, "connection timed out"},
		{&net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}, "connection timed out"},
		{&tls.CertificateVerificationError{Err: errors.New("private-certificate-value")}, "TLS certificate verification failed"},
		{tls.RecordHeaderError{Msg: "private-tls-value"}, "TLS handshake failed"},
		{&net.DNSError{Err: "private-dns-value", Name: "private-host"}, "DNS lookup failed"},
		{&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, "connection refused"},
		{&net.DNSError{IsTimeout: true}, "DNS lookup failed"},
		{errors.New("private-token and postgres://user:private-password@example.com"), "connection failed"},
	} {
		if got := Reason(fmt.Errorf("private-wrapper: %w", tc.err)); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}
