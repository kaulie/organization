GO ?= go
BIN_NAME ?= orgd
SERVICE_PORT ?= 8080

.PHONY: all build package run demo test test-race cover fmt vet tidy clean register contract-swagger

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

# 只生成契约产物（docs/swagger.json），不上报 —— 本地看一眼注解长什么样
contract-swagger:
	swag init -g cmd/server/main.go -o docs --outputTypes json
	@python3 -c 'import json; print("docs/swagger.json:", len(json.load(open("docs/swagger.json"))["paths"]), "个路径")'

# 读注解 → 生成 OpenAPI → 登记到服务中心（幂等，见 scripts/register-contract.sh）
register:
	./scripts/register-contract.sh

