GO ?= go
PORT ?= 8080

.PHONY: all build run demo test test-race cover fmt vet tidy clean

all: fmt vet test

build:
	$(GO) build -o bin/server ./cmd/server

run:
	$(GO) run ./cmd/server

demo: build
	./examples/demo.sh

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

clean:
	rm -rf bin coverage.out
