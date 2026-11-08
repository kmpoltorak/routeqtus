// Package extensions demonstrates adding a handler without editing the router.
package extensions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/middleware"
)

type Powered struct {
	Value string `json:"value" required:"true" doc:"X-Powered-By value, 1-128 Unicode characters with HTAB allowed and other ASCII controls rejected."`
}

var poweredValue = regexp.MustCompile(config.PoweredValuePattern)

func Register(r *middleware.Registry) error {
	return r.Register(middleware.Type{Name: "powered_by", Version: 1, Schema: config.Schema(Powered{}), Example: Powered{Value: "Routeqtus"}, Factory: func(b json.RawMessage) (middleware.Middleware, error) {
		var o Powered
		if err := config.DecodeTyped(b, &o); err != nil {
			return nil, err
		}
		characters := utf8.RuneCountInString(o.Value)
		if characters < 1 || characters > config.MaxPoweredCharacters || !poweredValue.MatchString(o.Value) {
			return nil, fmt.Errorf("value must have 1-%d characters with HTAB allowed and other ASCII controls rejected", config.MaxPoweredCharacters)
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
