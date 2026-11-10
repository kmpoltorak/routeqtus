package extensions

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
)

type BodyLimit struct {
	MaxBytes int64 `json:"max_bytes" required:"true" doc:"Maximum request body bytes; cannot raise the process limit. Excess receives 413 before the response starts."`
}

func registerBody(r *middleware.Registry) error {
	ceiling := config.CurrentLimits().MaxRouteBodyBytes
	schema := config.Schema(BodyLimit{})
	maximum := schema["properties"].(map[string]any)["max_bytes"].(map[string]any)
	maximum["minimum"], maximum["maximum"] = 1, ceiling
	return r.Register(middleware.Type{Name: "body_limit", Version: 1,
		Description: "Adds a request body size limit for a route, including chunked uploads.",
		Schema:      schema, Example: BodyLimit{MaxBytes: min(1048576, ceiling)},
		Factory: func(raw json.RawMessage) (middleware.Middleware, error) {
			var o BodyLimit
			if err := config.DecodeTyped(raw, &o); err != nil {
				return nil, err
			}
			if o.MaxBytes < 1 || o.MaxBytes > ceiling {
				return nil, fmt.Errorf("max_bytes must be between 1 and %d", ceiling)
			}
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if middleware.LimitBody(w, r, o.MaxBytes) {
						next.ServeHTTP(w, r)
					}
				})
			}, nil
		}})
}
