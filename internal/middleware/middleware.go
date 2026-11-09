// Package middleware implements the explicit, typed extension registry.
package middleware

import (
	"bufio"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kmpoltorak/routetus/internal/config"
)

type Middleware func(http.Handler) http.Handler

// Factory validates options and creates fresh state for one binding. Compilation
// also invokes factories to validate definitions, including unused/disabled ones.
type Factory func(json.RawMessage) (Middleware, error)
type Type struct {
	Name    string         `json:"name"`
	Version int            `json:"version"`
	Schema  map[string]any `json:"schema"`
	Example any            `json:"example"`
	Factory Factory        `json:"-"`
}

func (r *Registry) EffectiveOptions(h config.Handler) json.RawMessage {
	t, ok := r.Get(h.Type)
	if !ok {
		return h.Options
	}
	var options map[string]any
	if json.Unmarshal(h.Options, &options) != nil {
		return h.Options
	}
	props, _ := t.Schema["properties"].(map[string]any)
	for key, raw := range props {
		property := raw.(map[string]any)
		if d, ok := property["default"]; ok {
			v, present := options[key]
			if !present || v == float64(0) {
				options[key] = d
			}
		}
	}
	b, err := json.Marshal(options)
	if err != nil {
		return h.Options
	}
	return b
}

type Registry struct{ types map[string]Type }

func NewRegistry() *Registry { return &Registry{types: map[string]Type{}} }
func (r *Registry) Register(t Type) error {
	if t.Name == "" || t.Version != 1 || t.Factory == nil {
		return fmt.Errorf("invalid handler registration")
	}
	if _, ok := r.types[t.Name]; ok {
		return fmt.Errorf("duplicate handler type %s", t.Name)
	}
	r.types[t.Name] = t
	return nil
}
func (r *Registry) Types() []Type {
	out := []Type{}
	for _, t := range r.types {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Type) int { return cmp.Compare(a.Name, b.Name) })
	return out
}
func (r *Registry) Get(name string) (Type, bool) { t, ok := r.types[name]; return t, ok }
func (r *Registry) Build(h config.Handler) (Middleware, error) {
	t, ok := r.types[h.Type]
	if !ok {
		return nil, fmt.Errorf("unknown handler type %q", h.Type)
	}
	if h.Version != t.Version {
		return nil, fmt.Errorf("unsupported contract version")
	}
	m, err := t.Factory(h.Options)
	if err != nil {
		return nil, err
	}
	if h.Enabled != nil && !*h.Enabled {
		return nil, nil
	}
	return m, nil
}
func Chain(next http.Handler, ms []Middleware) http.Handler {
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i] != nil {
			next = ms[i](next)
		}
	}
	return next
}

type stateKey struct{}
type State struct {
	BodyTooLarge atomic.Bool
	RequestID    string
	RouteID      string
	Status       int
	Bytes        int64
	Headers      []*Headers
	OutboundID   bool
	Track        func(net.Conn) net.Conn
}

func WithState(r *http.Request, s *State) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), stateKey{}, s))
}
func Current(r *http.Request) *State { s, _ := r.Context().Value(stateKey{}).(*State); return s }
func RequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("request ID entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}

type Writer struct {
	http.ResponseWriter
	State *State
}

func (w *Writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *Writer) WriteHeader(code int) {
	if w.State.Status != 0 {
		return
	}
	if code >= 200 || code == 101 {
		w.State.Status = code
		w.State.ApplyResponseHeaders(w.Header())
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *Writer) Write(b []byte) (int, error) {
	if w.State.Status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(b)
	w.State.Bytes += int64(n)
	return n, err
}
func (w *Writer) FlushError() error {
	if w.State.Status == 0 {
		w.WriteHeader(200)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *Writer) Flush() { _ = w.FlushError() }
func (w *Writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.State.Status = 101
		if w.State.Track != nil {
			c = w.State.Track(c)
		}
	}
	return c, rw, err
}

type Empty struct{}
type AccessLog struct {
	SampleEvery int `json:"sample_every,omitempty" default:"1" doc:"Log one in N requests; range 1-1000000. Zero selects default."`
}
type Headers struct {
	RequestSet     map[string]string `json:"request_set,omitempty" doc:"Allowed outgoing request headers to set after hop-by-hop cleanup."`
	RequestRemove  []string          `json:"request_remove,omitempty" doc:"Allowed outgoing request headers to remove."`
	ResponseSet    map[string]string `json:"response_set,omitempty" doc:"Allowed response headers to set."`
	ResponseRemove []string          `json:"response_remove,omitempty" doc:"Allowed response headers to remove."`
}

func apply(h http.Header, set map[string]string, remove []string) {
	for _, k := range remove {
		h.Del(k)
	}
	for k, v := range set {
		h.Set(k, v)
	}
}
func (s *State) Rewrite(r *http.Request) {
	for _, h := range s.Headers {
		apply(r.Header, h.RequestSet, h.RequestRemove)
	}
	if s.OutboundID {
		r.Header.Set("X-Request-Id", s.RequestID)
	}
}
func (s *State) ApplyResponseHeaders(header http.Header) {
	for i := len(s.Headers) - 1; i >= 0; i-- {
		h := s.Headers[i]
		apply(header, h.ResponseSet, h.ResponseRemove)
	}
	if s.OutboundID {
		header.Set("X-Request-Id", s.RequestID)
	}
}
func AllowedHeader(k string) bool {
	if k == "" {
		return false
	}
	for _, c := range k {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	switch strings.ToLower(k) {
	case "host", "connection", "proxy-connection", "keep-alive", "transfer-encoding", "content-length", "te", "trailer", "upgrade", "authorization", "proxy-authorization", "cookie", "set-cookie", "forwarded", "x-forwarded-for", "x-forwarded-host", "x-forwarded-proto":
		return false
	}
	return !strings.HasPrefix(strings.ToLower(k), "proxy-")
}

func ValidHeaderValue(v string) bool {
	for _, r := range v {
		if r < 32 && r != '\t' || r == 127 {
			return false
		}
	}
	return true
}
func RegisterBuiltins(r *Registry, logger *slog.Logger) error {
	types := []Type{
		{Name: "request_id", Version: 1, Schema: config.Schema(Empty{}), Example: Empty{}, Factory: func(b json.RawMessage) (Middleware, error) {
			var o Empty
			if err := config.DecodeTyped(b, &o); err != nil {
				return nil, err
			}
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s := Current(r)
					if s.RequestID == "" {
						s.RequestID = RequestID()
					}
					s.OutboundID = true
					w.Header().Set("X-Request-Id", s.RequestID)
					next.ServeHTTP(w, r)
				})
			}, nil
		}},
		{Name: "recover", Version: 1, Schema: config.Schema(Empty{}), Example: Empty{}, Factory: func(b json.RawMessage) (Middleware, error) {
			var o Empty
			if err := config.DecodeTyped(b, &o); err != nil {
				return nil, err
			}
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer func() {
						if p := recover(); p != nil {
							if p == http.ErrAbortHandler {
								panic(p)
							}
							s := Current(r)
							logger.Error("handler panic", "request_id", s.RequestID, "route_id", s.RouteID, "stack", string(debug.Stack()))
							if s.Status != 0 {
								panic(http.ErrAbortHandler)
							}
							http.Error(w, "internal server error", 500)
						}
					}()
					next.ServeHTTP(w, r)
				})
			}, nil
		}},
		{Name: "access_log", Version: 1, Schema: config.Schema(AccessLog{}), Example: AccessLog{SampleEvery: 1}, Factory: func(b json.RawMessage) (Middleware, error) {
			var o AccessLog
			if err := config.DecodeTyped(b, &o); err != nil {
				return nil, err
			}
			if o.SampleEvery < 0 || o.SampleEvery > config.MaxSampleEvery {
				return nil, fmt.Errorf("sample_every outside range")
			}
			if o.SampleEvery == 0 {
				o.SampleEvery = 1
			}
			var counter atomic.Uint64
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					start := time.Now()
					defer func() {
						p := recover()
						if (counter.Add(1)-1)%uint64(o.SampleEvery) == 0 {
							s := Current(r)
							status := s.Status
							if status == 0 {
								status = 200
								if p != nil {
									status = 500
								}
							}
							logger.Info("request", "request_id", s.RequestID, "route_id", s.RouteID, "method", r.Method, "status", status, "duration_seconds", time.Since(start).Seconds(), "response_bytes", s.Bytes)
						}
						if p != nil {
							panic(p)
						}
					}()
					next.ServeHTTP(w, r)
				})
			}, nil
		}},
		{Name: "headers", Version: 1, Schema: config.Schema(Headers{}), Example: Headers{ResponseSet: map[string]string{"X-Gateway": "routetus"}}, Factory: func(b json.RawMessage) (Middleware, error) {
			var o Headers
			if err := config.DecodeTyped(b, &o); err != nil {
				return nil, err
			}
			for _, set := range []map[string]string{o.RequestSet, o.ResponseSet} {
				for k, v := range set {
					if !AllowedHeader(k) || !ValidHeaderValue(v) || len(v) > 8192 {
						return nil, fmt.Errorf("disallowed header or value %q", k)
					}
				}
			}
			for _, remove := range [][]string{o.RequestRemove, o.ResponseRemove} {
				for _, k := range remove {
					if !AllowedHeader(k) {
						return nil, fmt.Errorf("disallowed header %q", k)
					}
				}
			}
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s := Current(r)
					s.Headers = append(s.Headers, &o)
					next.ServeHTTP(w, r)
				})
			}, nil
		}},
	}
	for _, t := range types {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}
