# Contributing to ZeroTwo

Thank you for your interest in contributing to ZeroTwo (`zerotwo`)!

ZeroTwo is an ultra-lightweight personal AI agent designed to run with extreme efficiency on low-resource Linux and ARM hardware (Kindle e-readers, Raspberry Pi, ARM64 servers, and Android Termux).

---

## The Prime Directive: Zero External Dependencies

ZeroTwo strictly enforces a **Zero External Dependencies** rule:
- `go.mod` must **never** include third-party packages or non-standard library modules.
- Everything—HTTP clients, SSE stream parsers, JSON tokenizers, terminal renderers, crypto, and subprocess controllers—must be implemented using the **Go standard library only**.
- Standard library Go ensures trivial cross-compilation across all architectures (`GOARM=6`, `GOARM=7`, `arm64`, `amd64`, `android`) without CGO toolchain lock-in or supply chain vulnerabilities.

Pull requests adding external dependencies to `go.mod` will be automatically rejected.

---

## Development Setup

### Prerequisites
- **Go 1.22+** (Go 1.23 recommended)
- **Make**
- Optional: `qemu-arm`, `qemu-aarch64` for emulated multi-architecture testing.

### Building
```bash
# Clone the repository
git clone https://github.com/as1furrahman/zerotwo.git
cd zerotwo

# Build the native static binary
make build

# Verify build
./bin/zerotwo -help
```

### Running Tests
All tests must pass 100% before submitting a PR:
```bash
# Run unit and integration tests
make test

# Run race detector (requires GCC on host)
make test-race

# Run cross-compilation matrix
make build-all

# Run QEMU emulation tests (if QEMU installed)
make test-qemu
```

---

## Coding Standards

1. **Idiomatic Go**: Code must be formatted with `gofmt` and pass `go vet ./...`.
2. **Error Handling**: Never discard errors silently. Avoid panics; fail closed with structured descriptive error messages.
3. **Resource Bounding**: Any reader or stream handler processing untrusted input must be bounded by a maximum capacity buffer (see `boundedBuffer` in `pkg/tools/shell.go`).
4. **Security-First**: Every new tool or action must define its authorization tier in `pkg/auth/policy.go`. Never rely on the LLM to follow prompt instructions for security boundaries.
5. **Documentation**: Add unit tests for every bug fix or new capability. Maintain godoc comments on exported types and functions.

---

## Pull Request Process

1. Fork the repository and create your branch from `master` (e.g. `feature/my-improvement` or `fix/auth-edge-case`).
2. Implement your changes, following the zero-dependency rule and coding standards.
3. Add unit tests verifying your change.
4. Ensure `make test` and `make build-all` pass with 0 errors.
5. Submit a pull request describing the rationale, testing steps, and impact.
