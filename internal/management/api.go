// Package management exposes the authenticated, generation-checked control plane.
package management

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/configservice"
	"github.com/kmpoltorak/routeqtus/internal/middleware"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
	"github.com/kmpoltorak/routeqtus/internal/strictjson"
)

type API struct {
	Service   *configservice.Service
	Token     string
	Principal string
	MaxBody   int64
	OpenAPI   []byte
	Ready     func() bool
	slots     chan struct{}
}

func New(s *configservice.Service, token, principal string, body int64, concurrent int, ready func() bool) *API {
	a := &API{Service: s, Token: token, Principal: principal, MaxBody: body, Ready: ready, slots: make(chan struct{}, concurrent)}
	a.OpenAPI = a.Contract()
	return a
}
func Authorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	want := sha256.Sum256([]byte("Bearer " + token))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}
func ETag(g int64) string { return fmt.Sprintf("\"g-%d\"", g) }

type problem struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	RequestID string              `json:"request_id"`
	Fields    []config.FieldError `json:"fields"`
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, r *http.Request, status int, code, message string, err error) {
	p := problem{Code: code, Message: message, RequestID: w.Header().Get("X-Request-Id"), Fields: []config.FieldError{}}
	p.Fields = append(p.Fields, config.FieldErrors(err)...)
	write(w, status, p)
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Request-Id", middleware.RequestID())
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/healthz" && r.Method == "GET" {
		write(w, 200, map[string]string{"status": "alive"})
		return
	}
	if !Authorized(r, a.Token) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		fail(w, r, 401, "unauthorized", "authentication required", nil)
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		fail(w, r, 503, "overloaded", "management concurrency limit reached", nil)
		return
	}
	if r.URL.Path == "/readyz" && r.Method == "GET" {
		if a.Ready == nil || !a.Ready() {
			a.storeError(w, r, errors.New("unavailable"))
			return
		}
		write(w, 200, map[string]string{"status": "ready"})
		return
	}
	if r.URL.Path == "/openapi.json" && r.Method == "GET" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(a.OpenAPI)
		return
	}
	if r.URL.Path == "/v1/handler-types" && r.Method == "GET" {
		write(w, 200, a.Service.Compiler.Registry.Types())
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/handler-types/") && strings.HasSuffix(r.URL.Path, "/schema") && r.Method == "GET" {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/handler-types/"), "/schema")
		t, ok := a.Service.Compiler.Registry.Get(name)
		if !ok {
			fail(w, r, 404, "not_found", "handler type not found", nil)
			return
		}
		write(w, 200, t.Schema)
		return
	}
	if r.URL.Path == "/v1/revisions" && r.Method == "GET" {
		h, err := a.Service.Store.History(r.Context())
		if err != nil {
			a.storeError(w, r, err)
			return
		}
		write(w, 200, h)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/revisions/") && r.Method == "GET" {
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/v1/revisions/"), 10, 64)
		if err != nil {
			fail(w, r, 404, "not_found", "revision not found", nil)
			return
		}
		c, err := a.Service.Store.Revision(r.Context(), id)
		if err != nil {
			a.storeError(w, r, err)
			return
		}
		st, err := a.Service.Store.State(r.Context())
		if err != nil {
			a.storeError(w, r, err)
			return
		}
		w.Header().Set("ETag", ETag(st.Generation))
		write(w, 200, c)
		return
	}
	if r.URL.Path == "/v1/config/validate" && r.Method == "POST" {
		b, ok := a.body(w, r)
		if !ok {
			return
		}
		c, err := config.Decode(b)
		if err == nil {
			err = a.Service.Validate(c)
		}
		if err != nil {
			fail(w, r, 422, "invalid_configuration", "configuration rejected", err)
			return
		}
		write(w, 200, map[string]bool{"valid": true})
		return
	}
	st, c, err := a.Service.Store.Load(r.Context())
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	w.Header().Set("ETag", ETag(st.Generation))
	if r.URL.Path == "/v1/status" && r.Method == "GET" {
		write(w, 200, st)
		return
	}
	if r.URL.Path == "/v1/config/effective" && r.Method == "GET" {
		c, err = c.Effective()
		if err != nil {
			a.storeError(w, r, err)
			return
		}
		for i := range c.Handlers {
			c.Handlers[i].Options = a.Service.Compiler.Registry.EffectiveOptions(c.Handlers[i])
		}
		for i := range c.Routes {
			if c.Routes[i].RequestTimeout == nil {
				c.Routes[i].RequestTimeout = config.D(bootstrap.Duration(a.Service.Compiler.Limits.RequestTimeout))
			}
		}
		write(w, 200, c)
		return
	}
	if r.URL.Path == "/v1/rollback" && r.Method == "POST" {
		g, ok := expected(w, r)
		if !ok {
			return
		}
		b, ok := a.body(w, r)
		if !ok {
			return
		}
		var v struct {
			RevisionID int64 `json:"revision_id" required:"true"`
		}
		if err = config.DecodeTyped(b, &v); err != nil {
			fail(w, r, 422, "invalid_configuration", "invalid rollback request", err)
			return
		}
		p, err := a.Service.Rollback(r.Context(), v.RevisionID, g, a.metadata(r))
		if err != nil {
			a.storeError(w, r, err)
			return
		}
		w.Header().Set("ETag", ETag(p.Generation))
		write(w, 200, p)
		return
	}
	if r.Method == "GET" {
		v, err := representation(c, r.URL.Path)
		if err != nil {
			var conversion conversionError
			if errors.As(err, &conversion) {
				a.storeError(w, r, err)
			} else {
				fail(w, r, 404, "not_found", "resource not found", nil)
			}
			return
		}
		write(w, 200, v)
		return
	}
	if r.Method != "PUT" && r.Method != "PATCH" && r.Method != "POST" && r.Method != "DELETE" {
		w.Header().Set("Allow", "GET, POST, PUT, PATCH, DELETE")
		fail(w, r, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	g, ok := expected(w, r)
	if !ok {
		return
	}
	if g != st.Generation {
		a.storeError(w, r, postgres.ErrPrecondition)
		return
	}
	var b []byte
	if r.Method != "DELETE" {
		b, ok = a.body(w, r)
		if !ok {
			return
		}
	}
	candidate, err := mutate(c, r.URL.Path, r.Method, b)
	if err != nil {
		var e resourceError
		var conversion conversionError
		if errors.As(err, &conversion) {
			a.storeError(w, r, err)
		} else if errors.As(err, &e) {
			fail(w, r, e.status, e.code, e.Error(), nil)
		} else {
			fail(w, r, 422, "invalid_configuration", "configuration rejected", err)
		}
		return
	}
	p, err := a.Service.Publish(r.Context(), candidate, g, a.metadata(r))
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	w.Header().Set("ETag", ETag(p.Generation))
	write(w, 200, p)
}
func (a *API) body(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, a.MaxBody)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, r, 413, "document_too_large", "document exceeds management body limit", nil)
		} else {
			fail(w, r, 400, "invalid_json", "cannot read document", nil)
		}
		return nil, false
	}
	var raw any
	if err = strictjson.Decode(b, &raw); err != nil {
		fail(w, r, 400, "invalid_json", "invalid or ambiguous JSON", nil)
		return nil, false
	}
	return b, true
}
func expected(w http.ResponseWriter, r *http.Request) (int64, bool) {
	v := r.Header.Get("If-Match")
	if v == "" {
		fail(w, r, 428, "precondition_required", "If-Match is required", nil)
		return 0, false
	}
	if !strings.HasPrefix(v, "\"g-") || !strings.HasSuffix(v, "\"") {
		fail(w, r, 412, "precondition_failed", "If-Match must be a concrete configuration ETag", nil)
		return 0, false
	}
	g, err := strconv.ParseInt(v[3:len(v)-1], 10, 64)
	if err != nil || g < 0 || v != ETag(g) {
		fail(w, r, 412, "precondition_failed", "invalid ETag", nil)
		return 0, false
	}
	return g, true
}
func (a *API) metadata(r *http.Request) postgres.Metadata {
	return postgres.Metadata{Principal: a.Principal, Author: r.Header.Get("X-Revision-Author"), Description: r.Header.Get("X-Revision-Description")}
}
func (a *API) storeError(w http.ResponseWriter, r *http.Request, err error) {
	var f config.FieldError
	switch {
	case errors.Is(err, postgres.ErrPrecondition):
		fail(w, r, 412, "precondition_failed", "configuration generation changed", nil)
	case errors.Is(err, postgres.ErrNotFound):
		fail(w, r, 404, "not_found", "revision not found", nil)
	case errors.As(err, &f):
		fail(w, r, 422, "invalid_configuration", "configuration rejected", err)
	default:
		fail(w, r, 503, "store_unavailable", "configuration store unavailable", nil)
	}
}
