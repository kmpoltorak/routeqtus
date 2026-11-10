// Package config defines the complete, versioned operational configuration.
package config

import (
	"encoding/json"
	"fmt"
	"time"
)

type Duration time.Duration

func (d Duration) String() string               { return time.Duration(d).String() }
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string")
	}
	v, err := time.ParseDuration(s)
	if err != nil || v < 0 || v > 24*time.Hour {
		return fmt.Errorf("duration must be between 0s and 24h")
	}
	*d = Duration(v)
	return nil
}
func D(v time.Duration) *Duration { d := Duration(v); return &d }
func B(v bool) *bool              { return &v }

type Config struct {
	SchemaVersion  int        `json:"schema_version" required:"true" enum:"1" doc:"Configuration contract version."`
	TrustedProxies []string   `json:"trusted_proxies,omitempty" doc:"Trusted peer CIDRs; empty trusts no incoming forwarding headers."`
	Metrics        *bool      `json:"metrics,omitempty" default:"true" doc:"Collect request metrics; bootstrap can independently disable the endpoint."`
	Upstreams      []Upstream `json:"upstreams" required:"true" doc:"Named backend pools."`
	Handlers       []Handler  `json:"handlers" required:"true" doc:"Registered handler definitions; bindings have independent runtime state."`
	GlobalHandlers []string   `json:"global_handlers" required:"true" doc:"Handler IDs in global entry order; share state across routes and also wrap 404 and 405."`
	Routes         []Route    `json:"routes" required:"true" doc:"Complete routing table."`
}
type Upstream struct {
	ServiceID             string    `json:"service_id,omitempty" doc:"Optional service label: ASCII letters, digits, underscore, or hyphen; preserves case and does not affect routing."`
	ID                    string    `json:"id" required:"true" doc:"Unique stable identifier: 1-64 ASCII letters, digits, underscore, or hyphen."`
	Targets               []Target  `json:"targets" required:"true" doc:"At least one target, selected by lock-free round robin."`
	DialTimeout           *Duration `json:"dial_timeout,omitempty" default:"3s" doc:"TCP connect timeout; zero disables."`
	TLSHandshakeTimeout   *Duration `json:"tls_handshake_timeout,omitempty" default:"5s" doc:"TLS handshake timeout; zero disables."`
	ResponseHeaderTimeout *Duration `json:"response_header_timeout,omitempty" default:"10s" doc:"Wait for upstream response headers; zero disables."`
	IdleTimeout           *Duration `json:"idle_timeout,omitempty" default:"60s" doc:"Idle connection timeout; zero disables."`
	MaxConns              int       `json:"max_conns,omitempty" default:"128" doc:"Maximum connections per target; 0 selects default, range 1-100000."`
	MaxIdleConns          int       `json:"max_idle_conns,omitempty" default:"128" doc:"Pool-wide idle connection limit; 0 selects default, range 1-100000."`
	MaxIdleConnsPerHost   int       `json:"max_idle_conns_per_host,omitempty" default:"32" doc:"Idle connections per target; 0 selects default, range 1-100000."`
	ProxyFromEnvironment  bool      `json:"proxy_from_environment,omitempty" default:"false" doc:"Explicitly enable HTTP_PROXY/HTTPS_PROXY/NO_PROXY."`
	TLS                   TLS       `json:"tls,omitempty" doc:"Verified upstream TLS; environment references never expose secret values."`
}
type TLS struct {
	ServerName string `json:"server_name,omitempty" doc:"Override verified TLS server name; empty uses target host."`
	CAEnv      string `json:"ca_env,omitempty" doc:"Environment variable containing additional PEM CA certificates."`
	CertEnv    string `json:"cert_env,omitempty" doc:"Environment variable containing a PEM client certificate; requires key_env."`
	KeyEnv     string `json:"key_env,omitempty" doc:"Environment variable containing a PEM client key; requires cert_env."`
}
type Target struct {
	ID  string `json:"id" required:"true" doc:"Unique identifier within its upstream."`
	URL string `json:"url" required:"true" doc:"Absolute http/https URL with optional base path; no userinfo, query, or fragment."`
}
type Route struct {
	ServiceID      string    `json:"service_id,omitempty" doc:"Optional service label: ASCII letters, digits, underscore, or hyphen; preserves case and does not affect routing."`
	ID             string    `json:"id" required:"true" doc:"Unique route identifier."`
	Host           string    `json:"host,omitempty" doc:"Optional lowercase DNS host without port; empty matches all hosts."`
	Methods        []string  `json:"methods,omitempty" doc:"Uppercase HTTP methods; empty matches all methods."`
	Path           string    `json:"path" required:"true" doc:"Decoded absolute path without dot segments, repeated slashes, or percent escapes."`
	Prefix         bool      `json:"prefix,omitempty" default:"false" doc:"Segment-boundary prefix matcher; otherwise exact."`
	Priority       int       `json:"priority,omitempty" default:"0" doc:"Higher values win before host/path specificity; range -1000000 to 1000000."`
	StripPrefix    bool      `json:"strip_prefix,omitempty" default:"false" doc:"Remove matched prefix; requires prefix=true."`
	HostHeader     string    `json:"host_header,omitempty" doc:"Explicit outgoing Host; empty uses selected target host."`
	Upstream       string    `json:"upstream" required:"true" doc:"Existing upstream ID."`
	RequestTimeout *Duration `json:"request_timeout,omitempty" doc:"Full request deadline; null inherits bootstrap, 0s explicitly enables long streams."`
	Metrics        *bool     `json:"metrics,omitempty" default:"true" doc:"Collect route metrics when operational and bootstrap metrics are enabled."`
	Handlers       []string  `json:"handlers" required:"true" doc:"Handler IDs in route entry order, inside global handlers; each binding has independent state."`
}
type Handler struct {
	ID      string          `json:"id" required:"true" doc:"Unique instance identifier."`
	Type    string          `json:"type" required:"true" doc:"Registered handler type; discover through /v1/handler-types."`
	Version int             `json:"version" required:"true" enum:"1" doc:"Handler contract version."`
	Enabled *bool           `json:"enabled,omitempty" default:"true" doc:"False removes this handler from the request chain."`
	Options json.RawMessage `json:"options" required:"true" doc:"Typed options described by the selected handler schema."`
}

func (c Config) Effective() (Config, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return Config{}, Invalid("/", "configuration contains invalid JSON")
	}
	var out Config
	if err = json.Unmarshal(b, &out); err != nil {
		return Config{}, Invalid("/", "configuration contains invalid field values")
	}
	if out.Metrics == nil {
		out.Metrics = B(true)
	}
	for i := range out.Upstreams {
		u := &out.Upstreams[i]
		if u.DialTimeout == nil {
			u.DialTimeout = D(3 * time.Second)
		}
		if u.TLSHandshakeTimeout == nil {
			u.TLSHandshakeTimeout = D(5 * time.Second)
		}
		if u.ResponseHeaderTimeout == nil {
			u.ResponseHeaderTimeout = D(10 * time.Second)
		}
		if u.IdleTimeout == nil {
			u.IdleTimeout = D(time.Minute)
		}
		if u.MaxConns == 0 {
			u.MaxConns = 128
		}
		if u.MaxIdleConns == 0 {
			u.MaxIdleConns = 128
		}
		if u.MaxIdleConnsPerHost == 0 {
			u.MaxIdleConnsPerHost = 32
		}
	}
	for i := range out.Handlers {
		if out.Handlers[i].Enabled == nil {
			out.Handlers[i].Enabled = B(true)
		}
	}
	for i := range out.Routes {
		if out.Routes[i].Metrics == nil {
			out.Routes[i].Metrics = B(true)
		}
	}
	return out, nil
}
