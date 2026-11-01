VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

.PHONY: build run test test-integration lint e2e bench docker clean

## build: compile the wsbro binary
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o wsbro .

## run: start a local server with the dashboard enabled (password: secret)
run:
	WSBRO_DASHBOARD_PASSWORD=secret go run .

## test: unit and integration tests with the race detector
test:
	go test -race -count=1 ./...

## test-integration: also run the Redis and NATS adapter tests against real servers
test-integration:
	docker run -d --rm --name wsbro-test-redis -p 6379:6379 redis:7-alpine
	docker run -d --rm --name wsbro-test-nats -p 4222:4222 nats:2-alpine
	WSBRO_TEST_REDIS_ADDR=localhost:6379 WSBRO_TEST_NATS_URL=nats://localhost:4222 \
		go test -race -count=1 ./... ; status=$$?; \
		docker stop wsbro-test-redis wsbro-test-nats >/dev/null; exit $$status

## lint: gofmt, go vet, staticcheck and govulncheck
lint:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

## e2e: Laravel end-to-end tests (LARAVEL=12|13, BROADCASTER=reverb|pusher)
e2e:
	cd examples/laravel && docker compose up --build --abort-on-container-exit --exit-code-from e2e; \
		status=$$?; docker compose down -v; exit $$status

## bench: load test a running server (override with ARGS="-conns 50000")
bench:
	go run ./cmd/wsbro-bench $(ARGS)

## docker: build the Docker image
docker:
	docker build --build-arg VERSION=$(VERSION) -t wsbro:$(VERSION) .

clean:
	rm -f wsbro coverage.out
