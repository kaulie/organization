GO ?= go
BIN_NAME ?= orgd
SERVICE_PORT ?= 8080

.PHONY: all build package run demo test test-race cover fmt vet tidy clean

all: fmt vet test

build:
	$(GO) build -o bin/$(BIN_NAME) ./cmd/server

# 发版包：产出 outputs/（部署系统 release.sh 会调用 ./build.sh，这里是对等入口）
package:
	./build.sh

run:
	SERVICE_PORT=$(SERVICE_PORT) $(GO) run ./cmd/server

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
	rm -rf bin outputs coverage.out
