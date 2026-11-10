// Package extensions registers route policies without editing the router.
package extensions

import "github.com/kmpoltorak/routetus/internal/middleware"

func Register(r *middleware.Registry) error {
	for _, register := range []func(*middleware.Registry) error{registerConcurrency, registerRate, registerTokens, registerBody} {
		if err := register(r); err != nil {
			return err
		}
	}
	return nil
}
