// Package runtime synchronizes PostgreSQL revisions and safely retires snapshots.
package runtime

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/gateway"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
)

type active struct {
	snapshot *gateway.Snapshot
	state    postgres.State
	refs     atomic.Int64
}

func (a *active) release() {
	if a.refs.Add(-1) == 0 {
		a.snapshot.Close()
	}
}

type Runtime struct {
	Store       *postgres.Store
	Compiler    *gateway.Compiler
	Sync        bootstrap.Sync
	Logger      *slog.Logger
	mu          sync.RWMutex
	current     *active
	closing     atomic.Bool
	desired     atomic.Int64
	confirmed   atomic.Pointer[time.Time]
	success     atomic.Uint64
	errors      atomic.Uint64
	inflight    atomic.Int64
	slots       chan struct{}
	signals     chan struct{}
	connMu      sync.Mutex
	connections map[net.Conn]bool
	lastError   string
}
type Status struct {
	Applied       int64     `json:"applied_generation"`
	Desired       int64     `json:"desired_generation"`
	RevisionID    int64     `json:"revision_id"`
	LastConfirmed time.Time `json:"last_confirmed"`
	AgeSeconds    float64   `json:"age_seconds"`
	Ready         bool      `json:"ready"`
	Error         string    `json:"error,omitempty"`
}

func New(store *postgres.Store, compiler *gateway.Compiler, c bootstrap.Sync, logger *slog.Logger) *Runtime {
	return &Runtime{Store: store, Compiler: compiler, Sync: c, Logger: logger, slots: make(chan struct{}, compiler.Limits.MaxInFlight), signals: make(chan struct{}, 1), connections: map[net.Conn]bool{}}
}
func (r *Runtime) Signal() {
	select {
	case r.signals <- struct{}{}:
	default:
	}
}
func (r *Runtime) Ready() bool {
	last := r.confirmed.Load()
	return !r.closing.Load() && last != nil && time.Since(*last) <= bootstrap.Duration(r.Sync.MaxStaleness)
}
func (r *Runtime) Status() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := Status{Desired: r.desired.Load(), Ready: r.Ready(), Error: r.lastError}
	if r.current != nil {
		s.Applied = r.current.state.Generation
		s.RevisionID = r.current.state.RevisionID
	}
	if last := r.confirmed.Load(); last != nil {
		s.LastConfirmed = *last
		s.AgeSeconds = time.Since(s.LastConfirmed).Seconds()
	}
	return s
}
func (r *Runtime) synchronize(ctx context.Context) {
	st, err := r.Store.State(ctx)
	if err != nil {
		r.reject("database synchronization unavailable")
		return
	}
	r.desired.Store(st.Generation)
	r.mu.RLock()
	same := r.current != nil && r.current.state == st
	r.mu.RUnlock()
	if same {
		r.confirm()
		return
	}
	if st.RevisionID == 0 {
		r.reject("no active configuration revision")
		return
	}
	st, c, err := r.Store.Load(ctx)
	if err != nil {
		r.reject("configuration read failed")
		return
	}
	r.desired.Store(st.Generation)
	snapshot, err := r.Compiler.Compile(c)
	if err != nil {
		r.reject("revision rejected: " + err.Error())
		return
	}
	latest, err := r.Store.State(ctx)
	if err != nil {
		snapshot.Close()
		r.reject("publication confirmation unavailable")
		return
	}
	if latest != st {
		snapshot.Close()
		r.Signal()
		return
	}
	a := &active{snapshot: snapshot, state: st}
	a.refs.Store(1)
	r.mu.Lock()
	if r.closing.Load() {
		r.mu.Unlock()
		snapshot.Close()
		return
	}
	old := r.current
	r.current = a
	r.lastError = ""
	r.mu.Unlock()
	r.confirm()
	if old != nil {
		old.release()
	}
	r.Logger.Info("configuration applied", "revision_id", st.RevisionID, "generation", st.Generation)
}
func (r *Runtime) confirm() {
	now := time.Now()
	r.confirmed.Store(&now)
	r.success.Add(1)
	r.mu.Lock()
	r.lastError = ""
	r.mu.Unlock()
}
func (r *Runtime) reject(message string) {
	r.errors.Add(1)
	r.mu.Lock()
	changed := r.lastError != message
	r.lastError = message
	r.mu.Unlock()
	if changed {
		r.Logger.Error(message, "desired_generation", r.desired.Load())
	}
}
func (r *Runtime) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	if r.Sync.Notifications {
		go func() { defer close(done); r.notifications(ctx) }()
	} else {
		close(done)
		r.Signal()
	}
	defer func() { cancel(); <-done }()
	// LISTEN commits before its first synchronization signal; polling remains a fallback.
	timer := time.NewTimer(bootstrap.Duration(r.Sync.PollInterval))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.signals:
			r.synchronize(ctx)
		case <-timer.C:
			r.synchronize(ctx)
			interval := bootstrap.Duration(r.Sync.PollInterval)
			timer.Reset(interval*9/10 + time.Duration(rand.Int64N(int64(interval/5)+1)))
		}
	}
}
func (r *Runtime) notifications(ctx context.Context) {
	backoff := 100 * time.Millisecond
	for ctx.Err() == nil {
		conn, err := r.Store.Listen(ctx)
		if err == nil {
			r.Logger.Info("configuration notifications connected")
			r.Signal()
			backoff = 100 * time.Millisecond
			for ctx.Err() == nil {
				if _, err = conn.WaitForNotification(ctx); err != nil {
					break
				}
				r.Signal()
			}
			closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = conn.Close(closeCtx)
			cancel()
		}
		if ctx.Err() != nil {
			return
		}
		r.Logger.Warn("configuration notifications disconnected")
		r.Signal()
		timer := time.NewTimer(backoff + time.Duration(rand.Int64N(int64(backoff/4)+1)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}
func (r *Runtime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !r.Ready() {
		http.Error(w, "configuration unavailable or stale", 503)
		return
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	default:
		http.Error(w, "gateway concurrency limit reached", 503)
		return
	}
	r.mu.RLock()
	a := r.current
	if a != nil {
		a.refs.Add(1)
	}
	r.mu.RUnlock()
	if a == nil {
		http.Error(w, "configuration unavailable", 503)
		return
	}
	defer a.release()
	r.inflight.Add(1)
	defer r.inflight.Add(-1)
	a.snapshot.ServeTracked(w, req, r.track)
}

type tracked struct {
	net.Conn
	owner *Runtime
	once  sync.Once
}

func (c *tracked) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.owner.connMu.Lock(); delete(c.owner.connections, c); c.owner.connMu.Unlock() })
	return err
}
func (r *Runtime) track(c net.Conn) net.Conn {
	t := &tracked{Conn: c, owner: r}
	r.connMu.Lock()
	r.connections[t] = true
	r.connMu.Unlock()
	return t
}
func (r *Runtime) Stop() { r.closing.Store(true) }
func (r *Runtime) Close() {
	r.Stop()
	r.connMu.Lock()
	conns := make([]net.Conn, 0, len(r.connections))
	for c := range r.connections {
		conns = append(conns, c)
	}
	r.connMu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	r.mu.Lock()
	old := r.current
	r.current = nil
	r.mu.Unlock()
	if old != nil {
		old.release()
	}
}
func (r *Runtime) Wait(ctx context.Context) {
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for r.inflight.Load() > 0 {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (r *Runtime) Metrics(w http.ResponseWriter) {
	r.mu.RLock()
	a := r.current
	if a != nil {
		a.refs.Add(1)
	}
	r.mu.RUnlock()
	if a != nil {
		defer a.release()
		a.snapshot.WriteMetrics(w)
	}
	s := r.Status()
	writeSyncMetrics(w, s, r.success.Load(), r.errors.Load(), r.Store)
}
