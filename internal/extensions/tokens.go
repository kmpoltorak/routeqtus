package extensions

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/middleware"
)

var bearerToken = regexp.MustCompile(`^[A-Za-z0-9._~+/-]+=*$`)

type TokenAllowlist struct {
	TokenEnvs []string `json:"token_envs" required:"true" doc:"Unique environment references containing Bearer tokens of at least 32 bytes; values are never stored in configuration. Required in manage and serve, including disabled definitions."`
}

func registerTokens(r *middleware.Registry) error {
	limits := config.CurrentLimits()
	schema := config.Schema(TokenAllowlist{})
	refs := schema["properties"].(map[string]any)["token_envs"].(map[string]any)
	refs["minItems"], refs["maxItems"], refs["uniqueItems"] = 1, limits.MaxTokenReferences, true
	refs["items"] = map[string]any{"type": "string", "pattern": `^[A-Za-z_][A-Za-z0-9_]*$`}
	return r.Register(middleware.Type{Name: "token_allowlist", Version: 1,
		Description: "Requires an allowed Bearer token and removes it before forwarding to the backend.",
		Schema:      schema, Example: TokenAllowlist{TokenEnvs: []string{"ROUTETUS_ROUTE_TOKEN"}},
		Factory: func(raw json.RawMessage) (middleware.Middleware, error) {
			var o TokenAllowlist
			if err := config.DecodeTyped(raw, &o); err != nil {
				return nil, err
			}
			if len(o.TokenEnvs) < 1 || len(o.TokenEnvs) > limits.MaxTokenReferences {
				return nil, fmt.Errorf("token_envs must contain 1-%d references", limits.MaxTokenReferences)
			}
			seen := map[string]bool{}
			hashes := make([][sha256.Size]byte, 0, len(o.TokenEnvs))
			for i, name := range o.TokenEnvs {
				if !bootstrap.EnvName(name) || seen[name] {
					return nil, fmt.Errorf("token_envs entry %d is invalid or duplicated", i)
				}
				seen[name] = true
				token := os.Getenv(name)
				if len(token) < 32 || len(token) > limits.MaxTokenBytes || !bearerToken.MatchString(token) {
					return nil, fmt.Errorf("token_envs entry %d must reference a 32-%d byte Bearer token", i, limits.MaxTokenBytes)
				}
				hashes = append(hashes, sha256.Sum256([]byte(token)))
			}
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					values := r.Header.Values("Authorization")
					matched := 0
					if len(values) == 1 {
						scheme, token, ok := strings.Cut(values[0], " ")
						if ok && strings.EqualFold(scheme, "Bearer") && len(token) >= 32 && len(token) <= limits.MaxTokenBytes && bearerToken.MatchString(token) {
							sum := sha256.Sum256([]byte(token))
							for _, allowed := range hashes {
								matched |= subtle.ConstantTimeCompare(sum[:], allowed[:])
							}
						}
					}
					if matched != 1 {
						w.Header().Set("WWW-Authenticate", `Bearer realm="routetus"`)
						http.Error(w, "unauthorized", http.StatusUnauthorized)
						return
					}
					r.Header.Del("Authorization")
					next.ServeHTTP(w, r)
				})
			}, nil
		}})
}
