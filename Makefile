VERSION ?= dev
.PHONY: build run check test test-race test-integration fuzz bench loadbench vuln docs demo sabotage
build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X github.com/kmpoltorak/routeqtus/internal/cli.Version=$(VERSION)' -o bin/routeqtus ./cmd/routeqtus
run: build
	bin/routeqtus serve --config examples/bootstrap.yaml
check:
	test -z "$$(gofmt -l internal cmd tests)"
	go vet ./...
test:
	go test ./...
test-race:
	go test -race ./...
test-integration:
	sh scripts/test-integration.sh
fuzz:
	go test ./internal/strictjson -run='^$$' -fuzz=FuzzDecode -fuzztime=5s
	go test ./internal/gateway -run='^$$' -fuzz=FuzzRequestPath -fuzztime=5s
bench:
	go test ./internal/gateway -run='^$$' -bench=. -benchmem -count=3
loadbench:
	go run ./cmd/routeqtus-bench
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
docs: build
	bin/routeqtus openapi > docs/openapi.json
	python3 scripts/reference.py
demo:
	sh scripts/demo.sh
sabotage:
	python3 scripts/sabotage.py
