// Package app composes process lifecycle without putting implementation in main.
package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/configservice"
	"github.com/kmpoltorak/routeqtus/internal/extensions"
	"github.com/kmpoltorak/routeqtus/internal/gateway"
	"github.com/kmpoltorak/routeqtus/internal/management"
	"github.com/kmpoltorak/routeqtus/internal/middleware"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
	"github.com/kmpoltorak/routeqtus/internal/runtime"
)

func Compiler(c bootstrap.Config, logger *slog.Logger) (*gateway.Compiler, error) {
	r := middleware.NewRegistry()
	if err := middleware.RegisterBuiltins(r, logger); err != nil {
		return nil, err
	}
	if err := extensions.Register(r); err != nil {
		return nil, err
	}
	return &gateway.Compiler{Registry: r, Logger: logger, Limits: c.Server, Metrics: c.Admin.Metrics}, nil
}
func Logger(c bootstrap.Logging, w io.Writer) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(c.Level))
	opts := &slog.HandlerOptions{Level: level}
	if c.Format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func Run(parent context.Context, mode string, c bootstrap.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, bootstrap.Duration(c.Sync.StartupTimeout))
	defer cancel()
	var store *postgres.Store
	var err error
	for {
		store, err = postgres.Open(startup, c.Postgres)
		if err == nil {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-startup.Done():
			timer.Stop()
			return fmt.Errorf("startup timeout: postgres unavailable")
		case <-timer.C:
		}
	}
	defer store.Close()
	if mode == "migrate" {
		if err = store.Migrate(startup); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		logger.Info("database schema ready", "schema_version", 1)
		return nil
	}
	if err = store.CheckSchema(startup); err != nil {
		return err
	}
	compiler, err := Compiler(c, logger)
	if err != nil {
		return err
	}
	workerCtx, workerStop := context.WithCancel(ctx)
	defer workerStop()
	var rt *runtime.Runtime
	var public *http.Server
	var listener net.Listener
	if mode == "serve" {
		rt = runtime.New(store, compiler, c.Sync, logger)
		defer rt.Close()
		go rt.Run(workerCtx)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for !rt.Ready() {
			select {
			case <-startup.Done():
				return fmt.Errorf("startup timeout: no valid active configuration")
			case <-tick.C:
			}
		}
		listener, err = net.Listen("tcp", c.Server.Listen)
		if err != nil {
			return fmt.Errorf("public listener: %w", err)
		}
		public = &http.Server{Addr: c.Server.Listen, Handler: rt, ReadHeaderTimeout: bootstrap.Duration(c.Server.ReadHeaderTimeout), IdleTimeout: bootstrap.Duration(c.Server.IdleTimeout), MaxHeaderBytes: c.Server.MaxHeaderBytes, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelDebug), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	} else {
		token := os.Getenv(c.Management.AuthTokenEnv)
		if len(token) < 32 {
			return fmt.Errorf("management token environment reference must provide at least 32 bytes")
		}
		api := management.New(&configservice.Service{Store: store, Compiler: compiler}, token, c.Management.Principal, c.Management.MaxBodyBytes, c.Management.MaxInFlight)
		listener, err = net.Listen("tcp", c.Management.Listen)
		if err != nil {
			return fmt.Errorf("management listener: %w", err)
		}
		timeout := bootstrap.Duration(c.Management.RequestTimeout)
		public = &http.Server{Addr: c.Management.Listen, Handler: deadline(api, timeout), ReadHeaderTimeout: bootstrap.Duration(c.Server.ReadHeaderTimeout), ReadTimeout: timeout, WriteTimeout: timeout, IdleTimeout: bootstrap.Duration(c.Server.IdleTimeout), MaxHeaderBytes: c.Server.MaxHeaderBytes, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelDebug), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	}
	defer listener.Close()
	adminListener, err := net.Listen("tcp", c.Admin.Listen)
	if err != nil {
		return fmt.Errorf("admin listener: %w", err)
	}
	defer adminListener.Close()
	adminToken := ""
	if c.Admin.AuthTokenEnv != "" {
		adminToken = os.Getenv(c.Admin.AuthTokenEnv)
		if len(adminToken) < 32 {
			return fmt.Errorf("admin token environment reference must provide at least 32 bytes")
		}
	}
	admin := &http.Server{Handler: diagnostics(c, adminToken, store, rt), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelDebug)}
	failures := make(chan error, 2)
	go func() { failures <- admin.Serve(adminListener) }()
	cert, key := c.Server.CertFile, c.Server.KeyFile
	if mode == "manage" {
		cert, key = c.Management.CertFile, c.Management.KeyFile
	}
	go func() {
		if cert != "" {
			failures <- public.ServeTLS(listener, cert, key)
		} else {
			failures <- public.Serve(listener)
		}
	}()
	logger.Info("process started", "mode", mode, "listen", public.Addr, "pool_max_conns", c.Postgres.MaxConns, "listen_connections", boolInt(mode == "serve" && c.Sync.Notifications))
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	running := true
	for running {
		select {
		case <-ctx.Done():
			running = false
		case err = <-failures:
			running = false
		case <-hup:
			if rt != nil {
				rt.Signal()
			}
		}
	}
	logger.Info("shutdown started", "mode", mode)
	if rt != nil {
		rt.Stop()
	}
	workerStop()
	drain, drainCancel := context.WithTimeout(context.Background(), bootstrap.Duration(c.Server.ShutdownTimeout))
	defer drainCancel()
	_ = public.Shutdown(drain)
	if rt != nil {
		rt.Wait(drain)
		rt.Close()
	}
	_ = public.Close()
	_ = admin.Shutdown(drain)
	_ = admin.Close()
	logger.Info("shutdown completed", "mode", mode)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listener failed")
	}
	return nil
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func deadline(next http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func diagnostics(c bootstrap.Config, token string, store *postgres.Store, rt *runtime.Runtime) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "alive\n") })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ready := true
		if rt != nil {
			ready = rt.Ready()
		} else {
			ready = store.Ping(r.Context()) == nil
		}
		if !ready {
			http.Error(w, "not ready", 503)
			return
		}
		_, _ = io.WriteString(w, "ready\n")
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		if rt == nil {
			http.Error(w, "serve diagnostics only", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rt.Status())
	})
	if c.Admin.Metrics {
		mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			if rt != nil {
				rt.Metrics(w)
			} else {
				p := store.Pool.Stat()
				fmt.Fprintf(w, "routeqtus_postgres_connections %d\n", p.TotalConns())
			}
		})
	}
	if c.Admin.Pprof {
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && !management.Authorized(r, token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "authentication required", 401)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
