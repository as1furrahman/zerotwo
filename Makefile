GO ?= $(shell which go 2>/dev/null || echo $(HOME)/.local/bin/go)
QEMU_ARM ?= $(shell which qemu-arm 2>/dev/null || echo $(HOME)/.local/bin/qemu-arm)
QEMU_ARM64 ?= $(shell which qemu-aarch64 2>/dev/null || echo $(HOME)/.local/bin/qemu-aarch64)
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -s -w -X zerotwo/pkg/provider.BuildTime=$(BUILD_TIME)

.PHONY: all build build-all build-armv6 build-armv7 build-arm64 build-android-arm64 test test-race test-qemu test-qemu-armv6 test-qemu-armv7 test-qemu-arm64 bench clean

all: build-all

build-all: build build-armv6 build-armv7 build-arm64 build-android-arm64

build:
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/zerotwo cmd/zerotwo/main.go

build-armv6:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/zerotwo-linux-armv6 cmd/zerotwo/main.go

build-armv7:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/zerotwo-linux-armv7 cmd/zerotwo/main.go

build-arm64:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/zerotwo-linux-arm64 cmd/zerotwo/main.go

build-android-arm64:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/zerotwo-android-arm64 cmd/zerotwo/main.go

test:
	$(GO) test -v -timeout 120s ./...

test-race:
	@if command -v gcc >/dev/null 2>&1; then \
		echo "Running race detector on host..."; \
		CGO_ENABLED=1 $(GO) test -race -v -timeout 120s ./...; \
	else \
		echo "Race detector requires CGO and a C compiler (gcc/clang). Skipping on this architecture."; \
	fi

bench: build
	@echo "=== ZeroTwo Benchmarks ==="
	@echo "Measuring binary size:"
	@ls -lh bin/zerotwo
	@echo "Measuring startup time (target: < 15ms):"
	@time bin/zerotwo -help >/dev/null
	@echo "Benchmark complete."

test-qemu: build-armv7 build-arm64
	@echo "=== Testing ARMv7 under QEMU ==="
	$(QEMU_ARM) bin/zerotwo-linux-armv7 -dry-run "QEMU ARMv7 verification"
	@echo "=== Testing ARM64 under QEMU ==="
	$(QEMU_ARM64) bin/zerotwo-linux-arm64 -dry-run "QEMU ARM64 verification"

test-qemu-armv7:
	@echo "=== Running All Packages under QEMU ARMv7 (32-bit) ==="
	@for p in cmd/zerotwo pkg/agent pkg/auth pkg/config pkg/provider pkg/session pkg/shell pkg/tools pkg/ui; do \
		echo "=== Testing $$p under QEMU ARMv7 ==="; \
		pkg_name=$$(basename $$p); \
		GOARCH=arm GOARM=7 CGO_ENABLED=0 $(GO) test -c -o /tmp/$$pkg_name.test ./$$p && \
		(cd $$p && QEMU_EXEC="$(QEMU_ARM)" $(QEMU_ARM) /tmp/$$pkg_name.test) || exit 1; \
	done

test-qemu-armv6:
	@echo "=== Running All Packages under QEMU ARMv6 (arm1176 core) ==="
	@for p in cmd/zerotwo pkg/agent pkg/auth pkg/config pkg/provider pkg/session pkg/shell pkg/tools pkg/ui; do \
		echo "=== Testing $$p under QEMU ARMv6 ==="; \
		pkg_name=$$(basename $$p); \
		GOARCH=arm GOARM=6 CGO_ENABLED=0 $(GO) test -c -o /tmp/$$pkg_name.test6 ./$$p && \
		(cd $$p && QEMU_EXEC="$(QEMU_ARM) -cpu arm1176" $(QEMU_ARM) -cpu arm1176 /tmp/$$pkg_name.test6) || exit 1; \
	done

test-qemu-arm64:
	@echo "=== Running All Packages under QEMU ARM64 (AArch64) ==="
	@for p in cmd/zerotwo pkg/agent pkg/auth pkg/config pkg/provider pkg/session pkg/shell pkg/tools pkg/ui; do \
		echo "=== Testing $$p under QEMU ARM64 ==="; \
		pkg_name=$$(basename $$p); \
		GOARCH=arm64 CGO_ENABLED=0 $(GO) test -c -o /tmp/$$pkg_name.test64 ./$$p && \
		(cd $$p && QEMU_EXEC="$(QEMU_ARM64)" $(QEMU_ARM64) /tmp/$$pkg_name.test64) || exit 1; \
	done

clean:
	rm -rf bin
