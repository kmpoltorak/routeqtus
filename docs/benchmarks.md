# Benchmarks

Measured locally on 2026-11-08. These are reproducible development measurements,
not a production capacity guarantee or autoscaling threshold.

## Environment and method

- Intel Core i7-1068NG7 at 2.30 GHz, 8 logical CPUs, 32 GiB RAM; macOS/amd64.
- Go 1.27.3, GOMAXPROCS=8; no explicit CPU affinity/cgroup limit or dedicated host.
- Generator, gateway snapshot, and backend share one process and loopback network.
- Closed-loop clients: 1 or 16. No scheduled-arrival queue; skipped requests are 0.
- Warmup: 200 ms per case. Requested measurement: 1 second per case. In-flight
  requests finish before the actual elapsed duration is recorded. SSE responses last
  2 seconds, so their cases exceed the requested measurement window.
- HTTP matrix: 1/100/1000 routes, 0/1024/65536-byte responses, match and miss.
  Baseline has no router; its repeated route-count cases are controls, not route scans.
- HTTPS: verified TLS on both hops, HTTP/2; 1024-byte payload, all route counts.
- SSE: first event is flushed immediately, second after 2 seconds; 1/1000 routes.
- Minimal: no optional handlers, request metrics disabled. Typical: request_id,
  recover, access_log, headers, and metrics. Log formatting is measured with an
  io.Discard sink; container stdout/disk costs are not represented.
- Shared upstream transport settings match between baseline and gateway. Client idle
  pools match concurrency to avoid connection churn. No global transport is modified.
- Network comparison measures compiled snapshots. The separate control-plane test
  below includes real Runtime admission/freshness/pinning and PostgreSQL synchronization.
- CPU and peak RSS fields include generator, frontend, backend, and gateway together.
  RSS is the process high-water mark across cases, not isolated gateway RSS.
- The final HTTP matrix was repeated after review. HTTPS/SSE captures precede the
  final metric error-accounting change; no production speed claim relies on them.
- Stripped native CGO-disabled development binary: 13751552 bytes. Release archive sizes differ.

## Dataset totals

| Dataset | Cases | Requests | Errors | Skipped |
| --- | ---: | ---: | ---: | ---: |
| [http](benchmark-http.jsonl) | 90 | 1514724 | 0 | 0 |
| [https](benchmark-https.jsonl) | 30 | 363738 | 0 | 0 |
| [stream](benchmark-stream.jsonl) | 20 | 241773 | 0 | 0 |
| [direct](benchmark-direct.jsonl) | 2 | 82904 | 0 | 0 |

SSE totals include high-throughput 404 cases. Count only match=true cases when
evaluating stream samples; percentiles for one-client streams have very few samples.

## Example: 1024-byte responses, 16 clients, one route

These are individual observed cases, not an average across independent runs.

| Protocol | Variant | Requests/s | p50 ms | p95 ms | p99 ms | CPU s | Peak RSS MiB |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| http | baseline | 19960 | 0.583 | 1.592 | 2.039 | 5.415 | 21.86 |
| http | minimal | 20075 | 0.616 | 1.627 | 1.897 | 5.979 | 21.97 |
| http | typical | 17267 | 0.713 | 1.922 | 2.445 | 5.691 | 22.21 |
| https | baseline | 14937 | 1.074 | 1.679 | 2.159 | 5.679 | 20.65 |
| https | minimal | 10490 | 1.304 | 2.082 | 2.985 | 4.649 | 21.74 |
| https | typical | 10753 | 1.458 | 2.279 | 3.070 | 5.853 | 22.70 |
| stream | baseline | 8 | 2004.119 | 2004.401 | 2004.401 | 0.034 | 16.15 |
| stream | minimal | 8 | 2004.061 | 2004.633 | 2004.633 | 0.033 | 16.16 |
| stream | typical | 8 | 2002.120 | 2002.502 | 2002.502 | 0.013 | 18.59 |
| direct | direct | 67834 | 0.184 | 0.630 | 1.066 | 6.220 | 16.38 |

Direct backend controls use the same generator without either proxy hop. They provide
a headroom check, but co-location cannot prove that an external generator would never
be a bottleneck. CPU scheduling and loopback syscalls affect every variant. Run a
separate generator and backend on representative hosts before deriving a capacity limit.

## Routing allocations and CPU profile

[Raw microbenchmarks](microbench.txt), [before](microbench-before.txt), and
[benchstat comparison](microbench-summary.txt) report ns/op, B/op, and allocs/op.
The measured router cases now allocate 0 B/op and 0 allocs/op. They scan sorted routes;
1000-route lookup remains linear. The removed allocation came from constructing a
SplitHostPort error for a host without a port. Three samples do not establish
a 95% confidence interval or statistical timing improvement.

The [HTTP CPU profile summary](profile-http.txt) is dominated by loopback syscalls and
runtime scheduling in this co-located workload. It did not justify GC changes, unsafe,
custom event loops, or replacing net/http. No such tuning was performed.

## Publication and database outage under traffic

[Raw runtime/control-plane output](benchmark-control-plane.txt) measures four clients
against a real Runtime and local backend with real PostgreSQL. It records a stable
phase, ten complete publications under traffic, and a database-unreachable phase
within max_staleness. Each reports requests, errors, RPS, and p50/p95/p99. There were
zero errors in those recorded phases. A separate test verifies new requests become
503 after staleness and recover after database access returns.

`TestDatabaseOutageAndNoRequestSQL` also holds synchronization idle and verifies
100 proxy requests cause zero configuration pool acquisitions. Configuration queries
are caused by synchronization/publication, not by proxy request count. These tests
do not claim that a committed revision switches every replica simultaneously.

## Reproduce

```sh
go run ./cmd/routeqtus-bench -routes=1,100,1000 -payloads=0,1024,65536 -concurrency=1,16 -duration=1s -warmup=200ms -cpuprofile=http.prof > http.jsonl
go run ./cmd/routeqtus-bench -https -routes=1,100,1000 -payloads=1024 -concurrency=1,16 -duration=1s -warmup=200ms > https.jsonl
go run ./cmd/routeqtus-bench -stream -routes=1,1000 -payloads=1024 -concurrency=1,16 -duration=1s -warmup=200ms > stream.jsonl
go run ./cmd/routeqtus-bench -modes=direct -routes=1 -payloads=1024 -concurrency=1,16 -duration=1s -warmup=200ms > direct.jsonl
go test ./internal/gateway -run='^$' -bench=. -benchmem -benchtime=500ms -count=3 > microbench.txt
go run golang.org/x/perf/cmd/benchstat@v0.0.0-20261009192801-be2c69fb417e microbench.txt
go tool pprof -top http.prof
ROUTEQTUS_TEST_DATABASE_URL=... go test -tags=integration ./tests/integration -run TestRuntimeTrafficDuringPublicationAndOutage -count=1 -v
```

The test DSN must refer to a disposable administrative database with CREATE DATABASE
permission. Use the local integration-container script for ordinary integration checks.

## Invalid first run

[Initial invalid run](benchmark-initial-invalid.jsonl) is retained for transparency.
Its generator kept too few idle connections for 16 clients and exhausted local
ephemeral ports. It is excluded from the tables above. Fixing the generator pool
removed those errors; this was not evidence of gateway capacity or a backend outage.
