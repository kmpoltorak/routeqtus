package extensions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
)

type ConcurrencyLimit struct {
	Limit int `json:"limit" required:"true" doc:"Maximum simultaneous requests per binding; excess receives 503 without queuing."`
}

func registerConcurrency(r *middleware.Registry) error {
	ceiling := config.CurrentLimits().MaxConcurrentRequests
	schema := config.Schema(ConcurrencyLimit{})
	limit := schema["properties"].(map[string]any)["limit"].(map[string]any)
	limit["minimum"], limit["maximum"] = 1, ceiling
	return r.Register(middleware.Type{Name: "concurrency_limit", Version: 1,
		Description: "Limits simultaneous requests independently for each route binding.",
		Schema:      schema, Example: ConcurrencyLimit{Limit: min(16, ceiling)},
		Factory: func(raw json.RawMessage) (middleware.Middleware, error) {
			var o ConcurrencyLimit
			if err := config.DecodeTyped(raw, &o); err != nil {
				return nil, err
			}
			if o.Limit < 1 || o.Limit > ceiling {
				return nil, fmt.Errorf("limit must be between 1 and %d", ceiling)
			}
			var active atomic.Int64
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if active.Add(1) > int64(o.Limit) {
						active.Add(-1)
						http.Error(w, "request concurrency limit reached", http.StatusServiceUnavailable)
						return
					}
					defer active.Add(-1)
					next.ServeHTTP(w, r)
				})
			}, nil
		}})
}
