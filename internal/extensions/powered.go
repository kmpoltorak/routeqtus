// Package extensions demonstrates adding a handler without editing the router.
package extensions

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/middleware"
)

type Powered struct {
	Value string `json:"value" required:"true" doc:"X-Powered-By value, 1-128 characters without control characters."`
}

func Register(r *middleware.Registry) error {
	return r.Register(middleware.Type{Name: "powered_by", Version: 1, Schema: config.Schema(Powered{}), Example: Powered{Value: "Routeqtus"}, Factory: func(b json.RawMessage) (middleware.Middleware, error) {
		var o Powered
		if err := config.DecodeTyped(b, &o); err != nil {
			return nil, err
		}
		if len(o.Value) < 1 || len(o.Value) > 128 || !middleware.ValidHeaderValue(o.Value) {
			return nil, fmt.Errorf("value must have 1-128 characters without control characters")
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
