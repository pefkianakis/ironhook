.PHONY: fmt vet test test-race build run-api run-worker compose-up compose-down check sqlc sqlc-check test test-integration test-integration-race verify

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	go test -tags=integration ./...

test-integration-race:
	go test -race -tags=integration ./...

build:
	mkdir -p bin
	go build -o bin/ironhook-api ./cmd/api
	go build -o bin/ironhook-worker ./cmd/worker

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

compose-up:
	docker compose up -d

compose-down:
	docker compose down

check: fmt vet test build

sqlc:
	sqlc generate

sqlc-check:
	sqlc generate
	git diff --exit-code -- internal/platform/postgres/sqlc

verify:
	sqlc generate
	gofmt -w internal
	go test ./...
	go test -tags=integration ./...
	go test -race -tags=integration ./...