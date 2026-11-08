// routetus-bench compares local closed-loop workloads. It is not a capacity claim.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kmpoltorak/routetus/internal/app"
	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/config"
)

type result struct {
	Mode        string  `json:"mode"`
	Routes      int     `json:"routes"`
	Payload     int     `json:"payload_bytes"`
	Concurrency int     `json:"concurrency"`
	Match       bool    `json:"match"`
	HTTPS       bool    `json:"https"`
	Stream      bool    `json:"stream"`
	Requests    int     `json:"requests"`
	Errors      int     `json:"errors"`
	Skipped     int     `json:"skipped_requests"`
	Sampled     int     `json:"latency_samples"`
	Seconds     float64 `json:"seconds"`
	RPS         float64 `json:"rps"`
	P50         float64 `json:"p50_ms"`
	P95         float64 `json:"p95_ms"`
	P99         float64 `json:"p99_ms"`
	CPU         float64 `json:"process_cpu_seconds"`
	RSS         int64   `json:"process_peak_rss_bytes"`
	FirstEvent  float64 `json:"first_event_mean_ms,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	duration := flag.Duration("duration", time.Second, "measurement duration per case")
	warmup := flag.Duration("warmup", 200*time.Millisecond, "warmup per case")
	routeList := flag.String("routes", "1,100,1000", "route counts")
	payloadList := flag.String("payloads", "0,1024,65536", "response sizes")
	concurrencyList := flag.String("concurrency", "1,16", "closed-loop client counts")
	modes := flag.String("modes", "baseline,minimal,typical", "variants")
	https := flag.Bool("https", false, "TLS with HTTP/2 on both hops")
	stream := flag.Bool("stream", false, "responses contain two SSE events 2s apart")
	profile := flag.String("cpuprofile", "", "optional CPU profile file")
	flag.Parse()
	if *duration <= 0 || *warmup < 0 {
		return fmt.Errorf("invalid benchmark durations")
	}
	routes, err := numbers(*routeList, 1, 10000)
	if err != nil {
		return err
	}
	payloads, err := numbers(*payloadList, 0, 1<<20)
	if err != nil {
		return err
	}
	clients, err := numbers(*concurrencyList, 1, 1024)
	if err != nil {
		return err
	}
	if *profile != "" {
		f, err := os.Create(*profile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err = pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}
	enc := json.NewEncoder(os.Stdout)
	fmt.Fprintf(os.Stderr, "Go=%s OS=%s arch=%s CPUs=%d GOMAXPROCS=%d warmup=%s duration=%s; generator/backend/gateway share one process; closed-loop, no scheduled arrivals\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0), *warmup, *duration)
	for _, n := range routes {
		for _, size := range payloads {
			for _, concurrency := range clients {
				for _, mode := range strings.Split(*modes, ",") {
					for _, match := range []bool{true, false} {
						if (mode == "baseline" || mode == "direct") && !match {
							continue
						}
						r, err := measure(mode, n, size, concurrency, match, *https, *stream, *warmup, *duration)
						if err != nil {
							return err
						}
						if err = enc.Encode(r); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}
func numbers(s string, min, max int) ([]int, error) {
	out := []int{}
	for _, part := range strings.Split(s, ",") {
		n, err := strconv.Atoi(part)
		if err != nil || n < min || n > max {
			return nil, fmt.Errorf("invalid benchmark dimension %q", part)
		}
		out = append(out, n)
	}
	return out, nil
}
func measure(mode string, n, size, concurrency int, match, https, stream bool, warmup, duration time.Duration) (result, error) {
	r := result{Mode: mode, Routes: n, Payload: size, Concurrency: concurrency, Match: match, HTTPS: https, Stream: stream}
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = 'x'
	}
	backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: first\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-req.Context().Done():
				return
			case <-time.After(2 * time.Second):
			}
			fmt.Fprint(w, "data: last\n\n")
		}
		_, _ = w.Write(payload)
	}))
	backend.EnableHTTP2 = https
	if https {
		backend.StartTLS()
	} else {
		backend.Start()
	}
	defer backend.Close()
	boot := bootstrap.Defaults()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	compiler, err := app.Compiler(boot, logger)
	if err != nil {
		return r, err
	}
	var handler http.Handler
	u := config.Upstream{ID: "bench", Targets: []config.Target{{ID: "a", URL: backend.URL}}}
	defaults, err := (config.Config{Upstreams: []config.Upstream{u}}).Effective()
	if err != nil {
		return r, err
	}
	effective := defaults.Upstreams[0]
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: time.Duration(*effective.DialTimeout), KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxConnsPerHost: effective.MaxConns, MaxIdleConns: effective.MaxIdleConns, MaxIdleConnsPerHost: effective.MaxIdleConnsPerHost, IdleConnTimeout: time.Duration(*effective.IdleTimeout), TLSHandshakeTimeout: time.Duration(*effective.TLSHandshakeTimeout), ResponseHeaderTimeout: time.Duration(*effective.ResponseHeaderTimeout), ExpectContinueTimeout: time.Second}
	defer transport.CloseIdleConnections()
	if https {
		roots := x509.NewCertPool()
		roots.AddCert(backend.Certificate())
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
		if err = os.Setenv("ROUTETUS_BENCH_CA", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw}))); err != nil {
			return r, err
		}
		u.TLS.CAEnv = "ROUTETUS_BENCH_CA"
	}
	if mode == "direct" {
		handler = backend.Config.Handler
	} else if mode == "baseline" {
		target, _ := url.Parse(backend.URL)
		handler = &httputil.ReverseProxy{Transport: transport, Rewrite: func(p *httputil.ProxyRequest) { p.SetURL(target) }}
	} else {
		if mode != "minimal" && mode != "typical" {
			return r, fmt.Errorf("unknown mode %q", mode)
		}
		c := config.Config{SchemaVersion: 1, Upstreams: []config.Upstream{u}, Handlers: []config.Handler{}, GlobalHandlers: []string{}, Routes: []config.Route{}}
		c.Metrics = config.B(mode == "typical")
		if mode == "typical" {
			c.Handlers = []config.Handler{{ID: "id", Type: "request_id", Version: 1, Options: []byte(`{}`)}, {ID: "recover", Type: "recover", Version: 1, Options: []byte(`{}`)}, {ID: "log", Type: "access_log", Version: 1, Options: []byte(`{}`)}, {ID: "headers", Type: "headers", Version: 1, Options: []byte(`{"response_set":{"X-Gateway":"routetus"}}`)}}
			c.GlobalHandlers = []string{"id", "recover", "log", "headers"}
		}
		for i := 0; i < n; i++ {
			c.Routes = append(c.Routes, config.Route{ID: fmt.Sprintf("r%04d", i), Path: fmt.Sprintf("/route/%04d", i), Upstream: "bench", Handlers: []string{}})
			if stream {
				c.Routes[i].RequestTimeout = config.D(0)
			}
		}
		snapshot, err := compiler.Compile(c)
		if err != nil {
			return r, err
		}
		defer snapshot.Close()
		handler = snapshot
	}
	front := httptest.NewUnstartedServer(handler)
	front.EnableHTTP2 = https
	if https {
		front.StartTLS()
	} else {
		front.Start()
	}
	defer front.Close()
	client := front.Client()
	client.Timeout = 10 * time.Second
	clientTransport := client.Transport.(*http.Transport).Clone()
	clientTransport.MaxConnsPerHost = concurrency
	clientTransport.MaxIdleConns = concurrency * 2
	clientTransport.MaxIdleConnsPerHost = concurrency
	client.Transport = clientTransport
	defer clientTransport.CloseIdleConnections()
	p := "/missing"
	if match {
		p = fmt.Sprintf("/route/%04d", n-1)
	}
	type sample struct {
		latency          []float64
		requests, errors int
		first            float64
	}
	load := func(d time.Duration) []sample {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		out := make([]sample, concurrency)
		var wg sync.WaitGroup
		for i := range out {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				s := &out[i]
				for ctx.Err() == nil {
					start := time.Now()
					res, err := client.Get(front.URL + p)
					s.requests++
					if err != nil {
						s.errors++
						continue
					}
					want := 200
					if !match && mode != "baseline" {
						want = 404
					}
					if res.StatusCode != want {
						s.errors++
					}
					if stream && match {
						var first [1]byte
						if _, err = io.ReadFull(res.Body, first[:]); err != nil {
							s.errors++
						}
						s.first += time.Since(start).Seconds() * 1000
					}
					if _, err = io.Copy(io.Discard, res.Body); err != nil {
						s.errors++
					}
					res.Body.Close()
					if len(s.latency) < 1000000/concurrency {
						s.latency = append(s.latency, float64(time.Since(start))/1e6)
					}
				}
			}(i)
		}
		wg.Wait()
		return out
	}
	_ = load(warmup)
	cpuBefore, _ := resources()
	start := time.Now()
	samples := load(duration)
	r.Seconds = time.Since(start).Seconds()
	cpuAfter, rss := resources()
	r.CPU = cpuAfter - cpuBefore
	r.RSS = rss
	latencies := []float64{}
	for _, s := range samples {
		r.Requests += s.requests
		r.Errors += s.errors
		r.FirstEvent += s.first
		latencies = append(latencies, s.latency...)
	}
	r.Sampled = len(latencies)
	r.RPS = float64(r.Requests) / r.Seconds
	if stream && match && r.Requests > 0 {
		r.FirstEvent /= float64(r.Requests)
	}
	sort.Float64s(latencies)
	if len(latencies) > 0 {
		r.P50 = latencies[int(float64(len(latencies)-1)*.5)]
		r.P95 = latencies[int(float64(len(latencies)-1)*.95)]
		r.P99 = latencies[int(float64(len(latencies)-1)*.99)]
	}
	return r, nil
}
func resources() (float64, int64) {
	var r syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
		return 0, 0
	}
	cpu := float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6
	rss := r.Maxrss
	if runtime.GOOS != "darwin" {
		rss *= 1024
	}
	return cpu, rss
}
