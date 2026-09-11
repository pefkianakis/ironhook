.PHONY: fmt vet test test-race build run-api run-worker compose-up compose-down check

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

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