###############################################################################
# QUANTA Makefile — Go 1.24+
###############################################################################

# -------- project paths ------------------------------------------------------
MODULE_PATH    := quanta                       # <- the module directive in go.mod
PROTO_DIR      := api/proto
PROTO_FILES    := $(wildcard $(PROTO_DIR)/v1/*.proto)

# -------- local protoc plug-ins ---------------------------------------------
TOOLS_BIN      := $(CURDIR)/tools/bin
export PATH    := $(TOOLS_BIN):$(PATH)
PROTOC_GEN_GO  := $(TOOLS_BIN)/protoc-gen-go
PROTOC_GEN_GRPC:= $(TOOLS_BIN)/protoc-gen-go-grpc

# ensure tools dir exists
$(TOOLS_BIN):
	@mkdir -p $@

$(PROTOC_GEN_GO): | $(TOOLS_BIN)
	@echo "• installing protoc-gen-go"
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@cp `go env GOPATH`/bin/protoc-gen-go $@

$(PROTOC_GEN_GRPC): | $(TOOLS_BIN)
	@echo "• installing protoc-gen-go-grpc"
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@cp `go env GOPATH`/bin/protoc-gen-go-grpc $@

tools: $(PROTOC_GEN_GO) $(PROTOC_GEN_GRPC)

###############################################################################
# code-gen
###############################################################################
proto: tools
	@echo "• Generating protobuf (+gRPC stubs)…"
	@protoc -I $(PROTO_DIR) \
		--go_out=$(PROTO_DIR)          --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_DIR)     --go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)

###############################################################################
# convenience targets
###############################################################################
GO_PKGS := $(shell go list ./... | grep -vE '/mock(s)?')

build: proto
	@go build ./...

test: proto
	@go test -short $(GO_PKGS)

vet:
	@go vet ./...

clean:
	@rm -rf tools/bin
	@rm -f coverage.out cp.out
	@rm -rf bin

# -------- cross-compile linux binaries for containers -----------------------
ARCH ?= amd64
BIN_DIR := $(CURDIR)/bin/linux-$(ARCH)

build-linux: proto
	@echo "• Building Linux $(ARCH) binaries into $(BIN_DIR)"
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -trimpath -ldflags='-s -w' -o $(BIN_DIR)/engine ./cmd/engine
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -trimpath -ldflags='-s -w' -o $(BIN_DIR)/uppercase ./examples/transformers/uppercase
	@ls -lh $(BIN_DIR)

# -------- docker images ------------------------------------------------------
ENGINE_IMG := quanta-engine:local
UPPER_IMG  := quanta-uppercase:local

.PHONY: docker-build docker-up docker-down docker-logs docker-smoke

docker-build: build-linux
	@echo "• Building Docker images (ARCH=$(ARCH))"
	@docker build --no-cache --build-arg BIN_DIR=bin/linux-$(ARCH) -f Dockerfile.engine    -t $(ENGINE_IMG) .
	@docker build --no-cache --build-arg BIN_DIR=bin/linux-$(ARCH) -f Dockerfile.uppercase -t $(UPPER_IMG) .

# Requires Docker Desktop with Compose v2 (docker compose)
docker-up: docker-down docker-build
	@echo "• Starting stack"
	@ARCH=$(ARCH) docker compose up -d --build --force-recreate

docker-down:
	@docker compose down

docker-logs:
	@docker compose logs -f --tail=200

docker-smoke:
	@echo "• Smoke-check metrics endpoint"
	@sleep 2
	@curl -sf http://localhost:9100/metrics | head -n 5

.PHONY: proto tools build test vet clean build-linux
