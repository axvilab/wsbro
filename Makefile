.PHONY: build test-up test-down test test-go test-api test-dusk rebuild clean logs

BINARY=wsbro
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMPOSE=docker compose -f examples/laravel/docker-compose.yml

## Build

build:
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BINARY) .

## Test environment

test-up: build
	$(COMPOSE) up --build -d
	@echo ""
	@echo "  WSBro:      http://localhost:6001"
	@echo "  Dashboard:  http://localhost:6001/dashboard"
	@echo "  Laravel:    http://localhost:8000"
	@echo "  Chrome VNC: http://localhost:7900 (password: secret)"
	@echo ""

test-down:
	$(COMPOSE) down

rebuild: build
	$(COMPOSE) restart wsbro
	@echo "WSBro restarted with fresh binary"

## Testing

test-go:
	go test ./server/ -v -timeout 30s

test-api: test-up
	@echo "Waiting for services..."
	@sleep 5
	$(COMPOSE) exec laravel php artisan test --filter=WsBroApiTest --testdox

test-dusk: test-up
	@echo "Waiting for services..."
	@sleep 5
	$(COMPOSE) exec laravel php artisan dusk --filter=WsBroTest --testdox

test: test-go test-api test-dusk
	@echo ""
	@echo "All tests passed!"

## Cleanup

clean: test-down
	rm -f $(BINARY)
	$(COMPOSE) down -v --rmi local

## Logs

logs:
	$(COMPOSE) logs -f

logs-wsbro:
	$(COMPOSE) logs -f wsbro

logs-laravel:
	$(COMPOSE) logs -f laravel