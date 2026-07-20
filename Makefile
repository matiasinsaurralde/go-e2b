# Upstream commit of packages/envd/spec to pin the vendored protos against.
PROTO_COMMIT := $(shell cat proto/envd/VERSION)

# Proto files to vendor from upstream, relative to packages/envd/spec/ upstream
# and to proto/envd/ locally. Add new protos here — proto-sync loops over them.
PROTOS := process/process.proto filesystem/filesystem.proto

PROTO_BASE_URL := https://raw.githubusercontent.com/e2b-dev/infra/$(PROTO_COMMIT)/packages/envd/spec
PROTO_DST_DIR  := proto/envd
GEN_DIR        := internal/gen

.PHONY: proto-sync generate test lint gosec

## proto-sync: fetch all vendored protos at the pinned commit and regenerate bindings.
proto-sync:
	@for p in $(PROTOS); do \
		echo "Fetching $$p @ $(PROTO_COMMIT)"; \
		mkdir -p $(PROTO_DST_DIR)/$$(dirname $$p); \
		curl -sSL $(PROTO_BASE_URL)/$$p -o $(PROTO_DST_DIR)/$$p; \
		pkg=$$(sed -n 's/^package \([a-z0-9_]*\);/\1/p' $(PROTO_DST_DIR)/$$p | head -1); \
		if ! grep -q "go_package" $(PROTO_DST_DIR)/$$p; then \
			echo "  injecting go_package for package $$pkg"; \
			sed -i '' "s|^package $$pkg;|package $$pkg;\n\noption go_package = \"github.com/matiasinsaurralde/go-e2b/$(GEN_DIR)/envd/$$pkg;$$pkg\";|" $(PROTO_DST_DIR)/$$p; \
		fi; \
	done
	$(MAKE) generate

## generate: regenerate Go bindings from the vendored protos (requires buf in PATH).
generate:
	@echo "Generating Go bindings..."
	buf generate
	go mod tidy

## proto-upgrade: pin to the latest upstream commit touching the envd spec, then re-sync.
proto-upgrade:
	@echo "Looking up latest upstream commit for the envd spec..."
	$(eval NEW_COMMIT := $(shell curl -s "https://api.github.com/repos/e2b-dev/infra/commits?path=packages/envd/spec&per_page=1" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['sha'])"))
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
