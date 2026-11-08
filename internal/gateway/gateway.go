// Package gateway compiles immutable routing snapshots and streams HTTP traffic.
package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/middleware"
)

type Compiler struct {
	Registry *middleware.Registry
	Logger   *slog.Logger
	Limits   bootstrap.Server
	Metrics  bool
}
type Snapshot struct {
	routes     []route
	handler    http.Handler
	transports []*http.Transport
	trusted    []netip.Prefix
	limits     bootstrap.Server
	metrics    bool
	Stats      []RouteStats
}
type route struct {
	config.Route
	handler http.Handler
	stat    int
}
type pool struct {
	targets   []*url.URL
	next      atomic.Uint64
	transport *http.Transport
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var method = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,31}$`)

func (c *Compiler) Compile(input config.Config) (out *Snapshot, err error) {
	s := &Snapshot{limits: c.Limits}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	if input.SchemaVersion != 1 {
		return nil, config.Invalid("/schema_version", "unsupported version")
	}
	if len(input.Routes) > 10000 || len(input.Upstreams) > 10000 || len(input.Handlers) > 10000 {
		return nil, config.Invalid("/", "at most 10000 routes, upstreams, or handlers")
	}
	d, e := input.Effective()
	if e != nil {
		return nil, e
	}
	s.metrics = c.Metrics && *d.Metrics
	for i, p := range d.TrustedProxies {
		v, e := netip.ParsePrefix(p)
		if e != nil {
			return nil, config.Invalid(fmt.Sprintf("/trusted_proxies/%d", i), "invalid CIDR")
		}
		s.trusted = append(s.trusted, v)
	}
	pools := map[string]*pool{}
	for i, u := range d.Upstreams {
		field := fmt.Sprintf("/upstreams/%d", i)
		if !identifier.MatchString(u.ID) || pools[u.ID] != nil {
			return nil, config.Invalid(field+"/id", "invalid or duplicate upstream ID")
		}
		if len(u.Targets) < 1 || len(u.Targets) > 1024 {
			return nil, config.Invalid(field+"/targets", "must have 1-1024 targets")
		}
		for _, n := range []int{u.MaxConns, u.MaxIdleConns, u.MaxIdleConnsPerHost} {
			if n < 1 || n > 100000 {
				return nil, config.Invalid(field, "connection limit outside range")
			}
		}
		tlsConfig, e := upstreamTLS(u.TLS)
		if e != nil {
			return nil, config.Invalid(field+"/tls", e.Error())
		}
		t := &http.Transport{DialContext: (&net.Dialer{Timeout: time.Duration(*u.DialTimeout), KeepAlive: 30 * time.Second}).DialContext, TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true, TLSHandshakeTimeout: time.Duration(*u.TLSHandshakeTimeout), ResponseHeaderTimeout: time.Duration(*u.ResponseHeaderTimeout), IdleConnTimeout: time.Duration(*u.IdleTimeout), MaxConnsPerHost: u.MaxConns, MaxIdleConns: u.MaxIdleConns, MaxIdleConnsPerHost: u.MaxIdleConnsPerHost, ExpectContinueTimeout: time.Second}
		if u.ProxyFromEnvironment {
			t.Proxy = http.ProxyFromEnvironment
		}
		s.transports = append(s.transports, t)
		p := &pool{transport: t}
		ids := map[string]bool{}
		for j, target := range u.Targets {
			if !identifier.MatchString(target.ID) || ids[target.ID] {
				return nil, config.Invalid(fmt.Sprintf("%s/targets/%d/id", field, j), "invalid or duplicate target ID")
			}
			ids[target.ID] = true
			v, e := url.Parse(target.URL)
			if e != nil || v.Host == "" || !validHost(strings.ToLower(v.Host)) || (v.Scheme != "http" && v.Scheme != "https") || v.User != nil || v.RawQuery != "" || v.Fragment != "" || v.Opaque != "" || v.ForceQuery || !validPath(v.Path, v.RawPath) {
				return nil, config.Invalid(fmt.Sprintf("%s/targets/%d/url", field, j), "invalid target URL")
			}
			p.targets = append(p.targets, v)
		}
		pools[u.ID] = p
	}
	instances := map[string]middleware.Middleware{}
	for i, h := range d.Handlers {
		field := fmt.Sprintf("/handlers/%d", i)
		if !identifier.MatchString(h.ID) {
			return nil, config.Invalid(field+"/id", "invalid handler ID")
		}
		if _, ok := instances[h.ID]; ok {
			return nil, config.Invalid(field+"/id", "duplicate handler ID")
		}
		m, e := c.Registry.Build(h)
		if e != nil {
			return nil, config.Invalid(field+"/options", fmt.Sprintf("handler %s (%s): %s", h.ID, h.Type, e))
		}
		instances[h.ID] = m
	}
	resolve := func(ids []string, field string) ([]middleware.Middleware, error) {
		ms := []middleware.Middleware{}
		seen := map[string]bool{}
		for i, id := range ids {
			m, ok := instances[id]
			if !ok || seen[id] {
				return nil, config.Invalid(fmt.Sprintf("%s/%d", field, i), "missing or duplicate handler binding")
			}
			seen[id] = true
			if m != nil {
				ms = append(ms, m)
			}
		}
		return ms, nil
	}
	global, e := resolve(d.GlobalHandlers, "/global_handlers")
	if e != nil {
		return nil, e
	}
	ids := map[string]bool{}
	for i, r := range d.Routes {
		field := fmt.Sprintf("/routes/%d", i)
		if !identifier.MatchString(r.ID) || ids[r.ID] {
			return nil, config.Invalid(field+"/id", "invalid or duplicate route ID")
		}
		ids[r.ID] = true
		if r.Path == "" || !strings.HasPrefix(r.Path, "/") || !validPath(r.Path, "") || strings.Contains(r.Path, "%") || strings.HasSuffix(r.Path, "/") && r.Path != "/" && r.Prefix {
			return nil, config.Invalid(field+"/path", "invalid or ambiguous route path")
		}
		if r.Priority < -1000000 || r.Priority > 1000000 {
			return nil, config.Invalid(field+"/priority", "outside range")
		}
		if r.StripPrefix && !r.Prefix {
			return nil, config.Invalid(field+"/strip_prefix", "requires prefix=true")
		}
		if !validHost(r.Host) || !validHost(r.HostHeader) {
			return nil, config.Invalid(field+"/host", "invalid host or host_header")
		}
		if strings.Contains(r.Host, ":") {
			return nil, config.Invalid(field+"/host", "host matcher must not contain a port")
		}
		methods := map[string]bool{}
		for _, m := range r.Methods {
			if !method.MatchString(m) || methods[m] {
				return nil, config.Invalid(field+"/methods", "invalid or duplicate method")
			}
			methods[m] = true
		}
		p := pools[r.Upstream]
		if p == nil {
			return nil, config.Invalid(field+"/upstream", "unknown upstream")
		}
		for _, old := range s.routes {
			if r.Host == old.Host && r.Path == old.Path && r.Prefix == old.Prefix && overlapMethods(r.Methods, old.Methods) {
				return nil, config.Invalid(field, "ambiguous route conflicts with "+old.ID)
			}
		}
		ms, e := resolve(r.Handlers, field+"/handlers")
		if e != nil {
			return nil, e
		}
		proxy := s.proxy(r, p, c.Logger)
		s.Stats = append(s.Stats, RouteStats{ID: r.ID, Enabled: s.metrics && *r.Metrics})
		s.routes = append(s.routes, route{Route: r, handler: middleware.Chain(proxy, ms), stat: len(s.Stats) - 1})
	}
	sort.Slice(s.routes, func(i, j int) bool { return less(s.routes[i].Route, s.routes[j].Route) })
	s.Stats = append(s.Stats, RouteStats{ID: "", Enabled: s.metrics})
	s.handler = middleware.Chain(http.HandlerFunc(s.dispatch), global)
	return s, nil
}

func validHost(h string) bool {
	if h == "" {
		return true
	}
	if strings.ToLower(h) != h || len(h) > 253 || strings.ContainsAny(h, "\r\n /\\@?#\x00") {
		return false
	}
	u, err := url.Parse("http://" + h)
	if err != nil || u.Host != h || u.Hostname() == "" {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}
func validPath(p, raw string) bool {
	if !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if r < 32 || r == 127 {
			return false
		}
	}
	if p == "" {
		return raw == ""
	}
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00\r\n") || strings.Contains(p, "//") || strings.Contains(p, "%") {
		return false
	}
	clean := path.Clean(p)
	if p != clean && p != clean+"/" {
		return false
	}
	if raw != "" {
		decoded, e := url.PathUnescape(raw)
		if e != nil || decoded != p {
			return false
		}
		lower := strings.ToLower(raw)
		if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
			return false
		}
	}
	return true
}
func overlapMethods(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
func less(a, b config.Route) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if (a.Host == "") != (b.Host == "") {
		return a.Host != ""
	}
	if len(a.Path) != len(b.Path) {
		return len(a.Path) > len(b.Path)
	}
	if a.Prefix != b.Prefix {
		return !a.Prefix
	}
	return a.ID < b.ID
}
func matches(r config.Route, host, p string) bool {
	if r.Host != "" && r.Host != host {
		return false
	}
	if !r.Prefix {
		return r.Path == p
	}
	return p == r.Path || r.Path == "/" || strings.HasPrefix(p, r.Path+"/")
}
func (s *Snapshot) match(r *http.Request) (*route, []string) {
	host := strings.ToLower(r.Host)
	if strings.Contains(host, ":") {
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
	}
	host = strings.TrimSuffix(host, ".")
	allow := map[string]bool{}
	for i := range s.routes {
		v := &s.routes[i]
		if !matches(v.Route, host, r.URL.Path) {
			continue
		}
		if len(v.Methods) == 0 {
			return v, nil
		}
		for _, m := range v.Methods {
			if m == r.Method {
				return v, nil
			}
			allow[m] = true
		}
	}
	methods := make([]string, 0, len(allow))
	for m := range allow {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	return nil, methods
}

type selectionKey struct{}
type selection struct {
	route *route
	allow []string
}

func (s *Snapshot) dispatch(w http.ResponseWriter, r *http.Request) {
	v := r.Context().Value(selectionKey{}).(selection)
	if v.route != nil {
		v.route.handler.ServeHTTP(w, r)
		return
	}
	if len(v.allow) > 0 {
		w.Header().Set("Allow", strings.Join(v.allow, ", "))
		http.Error(w, "method not allowed", 405)
		return
	}
	http.NotFound(w, r)
}
func (s *Snapshot) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.ServeTracked(w, r, nil) }
func (s *Snapshot) ServeTracked(w http.ResponseWriter, r *http.Request, track func(net.Conn) net.Conn) {
	if !validPath(r.URL.Path, r.URL.RawPath) {
		http.Error(w, "invalid request path", 400)
		return
	}
	if _, err := url.ParseQuery(r.URL.RawQuery); err != nil {
		http.Error(w, "invalid query", 400)
		return
	}
	v, allow := s.match(r)
	st := &middleware.State{Track: track}
	idx := len(s.Stats) - 1
	timeout := bootstrap.Duration(s.limits.RequestTimeout)
	if v != nil {
		st.RouteID = v.ID
		idx = v.stat
		if v.RequestTimeout != nil {
			timeout = time.Duration(*v.RequestTimeout)
		}
	}
	writer := &middleware.Writer{ResponseWriter: w, State: st}
	if r.ContentLength > s.limits.MaxBodyBytes {
		http.Error(writer, "request body too large", 413)
		return
	}
	r.Body = &limitedBody{ReadCloser: http.MaxBytesReader(writer, r.Body, s.limits.MaxBodyBytes), state: st, controller: http.NewResponseController(writer)}
	if timeout > 0 {
		deadline := time.Now().Add(timeout)
		ctx, cancel := context.WithDeadline(r.Context(), deadline)
		defer cancel()
		r = r.WithContext(ctx)
		rc := http.NewResponseController(writer)
		if r.ContentLength != 0 {
			_ = rc.SetReadDeadline(deadline)
		}
		_ = rc.SetWriteDeadline(deadline)
		defer func() { _ = rc.SetReadDeadline(time.Time{}); _ = rc.SetWriteDeadline(time.Time{}) }()
	}
	r = middleware.WithState(r, st)
	r = r.WithContext(context.WithValue(r.Context(), selectionKey{}, selection{v, allow}))
	stat := &s.Stats[idx]
	if stat.Enabled {
		start := time.Now()
		stat.InFlight.Add(1)
		defer func() {
			p := recover()
			stat.InFlight.Add(-1)
			status := st.Status
			if status == 0 {
				status = 200
			}
			stat.Observe(status, time.Since(start))
			if p != nil {
				if status < 500 {
					stat.Errors.Add(1)
				}
				panic(p)
			}
		}()
	}
	s.handler.ServeHTTP(writer, r)
}
func (s *Snapshot) proxy(route config.Route, p *pool, logger *slog.Logger) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{Transport: p.transport, Rewrite: func(pr *httputil.ProxyRequest) {
		if route.StripPrefix {
			strip(pr.Out, route.Path)
		}
		target := p.targets[(p.next.Add(1)-1)%uint64(len(p.targets))]
		pr.SetURL(target)
		pr.Out.URL.RawQuery = pr.In.URL.RawQuery
		pr.Out.URL.ForceQuery = pr.In.URL.ForceQuery
		if route.HostHeader != "" {
			pr.Out.Host = route.HostHeader
		}
		pr.Out.Header.Del("Forwarded")
		pr.Out.Header.Del("X-Forwarded-For")
		pr.Out.Header.Del("X-Forwarded-Host")
		pr.Out.Header.Del("X-Forwarded-Proto")
		client, proto := s.forwarded(pr.In)
		pr.Out.Header.Set("X-Forwarded-For", client)
		pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)
		pr.Out.Header.Set("X-Forwarded-Proto", proto)
		if st := middleware.Current(pr.In); st != nil {
			st.Rewrite(pr.Out)
		}
	}, ModifyResponse: func(response *http.Response) error {
		if st := middleware.Current(response.Request); st != nil {
			for i := len(st.Headers) - 1; i >= 0; i-- {
				h := st.Headers[i]
				for _, k := range h.ResponseRemove {
					response.Header.Del(k)
				}
				for k, v := range h.ResponseSet {
					response.Header.Set(k, v)
				}
			}
			if st.OutboundID {
				response.Header.Set("X-Request-Id", st.RequestID)
			}
		}
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		st := middleware.Current(r)
		if st != nil && st.Status != 0 {
			panic(http.ErrAbortHandler)
		}
		code := 502
		var timeout net.Error
		var body *http.MaxBytesError
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
			code = 504
		}
		if deadline, ok := r.Context().Deadline(); ok && !time.Now().Before(deadline) {
			code = 504
		}
		if code == 504 && r.ProtoMajor == 1 {
			w.Header().Set("Connection", "close")
		}
		if errors.As(err, &body) || st != nil && st.BodyTooLarge.Load() {
			code = 413
		}
		http.Error(w, http.StatusText(code), code)
	}, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelDebug)}
}

type limitedBody struct {
	io.ReadCloser
	state      *middleware.State
	controller *http.ResponseController
}

func (b *limitedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		b.state.BodyTooLarge.Store(true)
	}
	if err != nil {
		_ = b.controller.SetReadDeadline(time.Time{})
	}
	return n, err
}
func strip(r *http.Request, prefix string) {
	if prefix == "/" {
		return
	}
	if r.URL.RawPath != "" {
		n := 0
		for i := 0; i < len(prefix) && n < len(r.URL.RawPath); i++ {
			if r.URL.RawPath[n] == '%' {
				n += 3
			} else {
				n++
			}
		}
		r.URL.RawPath = r.URL.RawPath[n:]
		if r.URL.RawPath == "" {
			r.URL.RawPath = "/"
		}
	}
	r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
}
func (s *Snapshot) trustedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range s.trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
func (s *Snapshot) forwarded(r *http.Request) (string, string) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	if err != nil {
		return host, proto
	}
	client := peer
	if s.trustedIP(peer) {
		chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(chain) - 1; i >= 0 && s.trustedIP(client); i-- {
			ip, e := netip.ParseAddr(strings.TrimSpace(chain[i]))
			if e != nil {
				client = peer
				break
			}
			client = ip
		}
		if v := r.Header.Get("X-Forwarded-Proto"); v == "http" || v == "https" {
			proto = v
		}
	}
	return client.Unmap().String(), proto
}
func upstreamTLS(c config.TLS) (*tls.Config, error) {
	t := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.ServerName}
	for _, name := range []string{c.CAEnv, c.CertEnv, c.KeyEnv} {
		if name != "" && !bootstrap.EnvName(name) {
			return nil, fmt.Errorf("invalid environment reference")
		}
	}
	if c.CAEnv != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(os.Getenv(c.CAEnv))) {
			return nil, fmt.Errorf("CA environment reference unavailable or invalid")
		}
		t.RootCAs = roots
	}
	if (c.CertEnv == "") != (c.KeyEnv == "") {
		return nil, fmt.Errorf("cert_env and key_env must be provided together")
	}
	if c.CertEnv != "" {
		cert, err := tls.X509KeyPair([]byte(os.Getenv(c.CertEnv)), []byte(os.Getenv(c.KeyEnv)))
		if err != nil {
			return nil, fmt.Errorf("client TLS environment references unavailable or invalid")
		}
		t.Certificates = []tls.Certificate{cert}
	}
	return t, nil
}
func (s *Snapshot) Close() {
	for _, t := range s.transports {
		t.CloseIdleConnections()
	}
}

// RouteStats is owned by its snapshot; removal of a route removes its metric series.
type RouteStats struct {
	ID       string
	Enabled  bool
	Requests [6]atomic.Uint64
	InFlight atomic.Int64
	Buckets  [7]atomic.Uint64
	Nanos    atomic.Uint64
	Errors   atomic.Uint64
}

var buckets = []float64{.001, .005, .01, .05, .1, .5, 1}

func (s *RouteStats) Observe(status int, d time.Duration) {
	if !s.Enabled {
		return
	}
	if status >= 500 {
		s.Errors.Add(1)
	}
	class := status / 100
	if class < 1 || class > 5 {
		class = 5
	}
	s.Requests[class].Add(1)
	s.Nanos.Add(uint64(d))
	for i, b := range buckets {
		if d.Seconds() <= b {
			s.Buckets[i].Add(1)
		}
	}
}
func (s *Snapshot) WriteMetrics(w http.ResponseWriter) {
	for i := range s.Stats {
		st := &s.Stats[i]
		if !st.Enabled {
			continue
		}
		labels := "route_id=" + strconv.Quote(st.ID)
		fmt.Fprintf(w, "routeqtus_errors_total{%s} %d\n", labels, st.Errors.Load())
		var count uint64
		for c := 1; c <= 5; c++ {
			n := st.Requests[c].Load()
			count += n
			fmt.Fprintf(w, "routeqtus_requests_total{%s,status_class=%q} %d\n", labels, strconv.Itoa(c)+"xx", n)
		}
		fmt.Fprintf(w, "routeqtus_in_flight{%s} %d\nrouteqtus_request_duration_seconds_sum{%s} %g\nrouteqtus_request_duration_seconds_count{%s} %d\n", labels, st.InFlight.Load(), labels, float64(st.Nanos.Load())/1e9, labels, count)
		for j, b := range buckets {
			fmt.Fprintf(w, "routeqtus_request_duration_seconds_bucket{%s,le=%q} %d\n", labels, strconv.FormatFloat(b, 'g', -1, 64), st.Buckets[j].Load())
		}
		fmt.Fprintf(w, "routeqtus_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, count)
	}
}
