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

.PHONY: proto tools build test vet clean
