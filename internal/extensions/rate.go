package extensions

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
)

type RateLimit struct {
	RequestsPerSecond int `json:"requests_per_second" required:"true" doc:"Token refill rate per binding in requests per second."`
	Burst             int `json:"burst" required:"true" doc:"Maximum immediately available tokens; excess receives 429 without queuing."`
}

func registerRate(r *middleware.Registry) error {
	limits := config.CurrentLimits()
	schema := config.Schema(RateLimit{})
	for _, field := range []string{"requests_per_second", "burst"} {
		p := schema["properties"].(map[string]any)[field].(map[string]any)
		p["minimum"] = 1
		if field == "burst" {
			p["maximum"] = limits.MaxBurst
		} else {
			p["maximum"] = limits.MaxRequestsPerSecond
		}
	}
	return r.Register(middleware.Type{Name: "rate_limit", Version: 1,
		Description: "Limits requests per second with a separate token bucket for each route binding.",
		Schema:      schema, Example: RateLimit{RequestsPerSecond: min(100, limits.MaxRequestsPerSecond), Burst: min(20, limits.MaxBurst)},
		Factory: func(raw json.RawMessage) (middleware.Middleware, error) {
			var o RateLimit
			if err := config.DecodeTyped(raw, &o); err != nil {
				return nil, err
			}
			if o.RequestsPerSecond < 1 || o.RequestsPerSecond > limits.MaxRequestsPerSecond || o.Burst < 1 || o.Burst > limits.MaxBurst {
				return nil, fmt.Errorf("requests_per_second must be 1-%d and burst 1-%d", limits.MaxRequestsPerSecond, limits.MaxBurst)
			}
			// ponytail: one bucket per binding, with no per-client map or cross-replica quota.
			var mu sync.Mutex
			tokens, previous := float64(o.Burst), time.Now()
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					now := time.Now()
					tokens = math.Min(float64(o.Burst), tokens+now.Sub(previous).Seconds()*float64(o.RequestsPerSecond))
					previous = now
					allowed := tokens >= 1
					if allowed {
						tokens--
					}
					mu.Unlock()
					if !allowed {
						w.Header().Set("Retry-After", "1")
						http.Error(w, "request rate limit reached", http.StatusTooManyRequests)
						return
					}
					next.ServeHTTP(w, r)
				})
			}, nil
		}})
}
