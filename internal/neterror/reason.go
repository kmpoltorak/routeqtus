// Package neterror describes connection failures without copying untrusted error text.
package neterror

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"syscall"
)

func Reason(err error) string {
	var certificate *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var dns *net.DNSError
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "request canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "connection timed out"
	case errors.As(err, &certificate):
		return "TLS certificate verification failed"
	case errors.As(err, &record):
		return "TLS handshake failed"
	case errors.As(err, &dns):
		return "DNS lookup failed"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.As(err, &network) && network.Timeout():
		return "connection timed out"
	default:
		return "connection failed"
	}
}
