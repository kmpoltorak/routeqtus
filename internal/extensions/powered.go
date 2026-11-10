// Package extensions demonstrates adding a handler without editing the router.
package extensions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
)

type Powered struct {
	Value string `json:"value" required:"true" doc:"X-Powered-By value, 1-128 Unicode characters with HTAB allowed and other ASCII controls rejected."`
}

var poweredValue = regexp.MustCompile(config.PoweredValuePattern)

func Register(r *middleware.Registry) error {
	return r.Register(middleware.Type{Name: "powered_by", Description: "Adds an X-Powered-By response header.", Version: 1, Schema: config.Schema(Powered{}), Example: Powered{Value: "Routetus"}, Factory: func(b json.RawMessage) (middleware.Middleware, error) {
		var o Powered
		if err := config.DecodeTyped(b, &o); err != nil {
			return nil, err
		}
		characters, limit := utf8.RuneCountInString(o.Value), config.CurrentLimits().MaxPoweredCharacters
		if characters < 1 || characters > limit || !poweredValue.MatchString(o.Value) {
			return nil, fmt.Errorf("value must have 1-%d characters with HTAB allowed and other ASCII controls rejected", limit)
		}
		h := middleware.Headers{ResponseSet: map[string]string{"X-Powered-By": o.Value}}
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s := middleware.Current(r)
				s.Headers = append(s.Headers, &h)
				next.ServeHTTP(w, r)
			})
		}, nil
	}})
}
