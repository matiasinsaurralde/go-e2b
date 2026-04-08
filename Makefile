# Upstream commit of packages/envd/spec/process/process.proto to pin against.
PROTO_COMMIT := $(shell cat proto/envd/VERSION)

PROTO_URL := https://raw.githubusercontent.com/e2b-dev/infra/$(PROTO_COMMIT)/packages/envd/spec/process/process.proto
PROTO_DST  := proto/envd/process/process.proto
GEN_DIR    := internal/gen

.PHONY: proto-sync generate test lint gosec

## proto-sync: fetch process.proto at the pinned commit and regenerate bindings.
proto-sync:
	@echo "Fetching process.proto @ $(PROTO_COMMIT)"
	curl -sSL $(PROTO_URL) -o $(PROTO_DST)
	@# Re-inject the go_package option that upstream does not carry.
	@if ! grep -q "go_package" $(PROTO_DST); then \
		sed -i '' 's|^package process;|package process;\n\noption go_package = "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process;process";|' $(PROTO_DST); \
	fi
	$(MAKE) generate

## generate: regenerate Go bindings from the vendored proto (requires buf in PATH).
generate:
	@echo "Generating Go bindings..."
	buf generate
	go mod tidy

## proto-upgrade: fetch the latest upstream commit for process.proto, update VERSION, and regenerate.
proto-upgrade:
	@echo "Looking up latest upstream commit for process.proto..."
	$(eval NEW_COMMIT := $(shell curl -s "https://api.github.com/repos/e2b-dev/infra/commits?path=packages/envd/spec/process/process.proto&per_page=1" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['sha'])"))
	@echo "Latest commit: $(NEW_COMMIT)"
	@echo "$(NEW_COMMIT)" > proto/envd/VERSION
	$(MAKE) proto-sync

## test: run tests with the race detector.
test:
	go test ./... -count=1 -race

## lint: run golangci-lint.
lint:
	golangci-lint run ./...

## gosec: run gosec security scanner (excluding generated code).
gosec:
	gosec -exclude-dir=$(GEN_DIR) ./...
