<p align="center">
  <img src="assets/logo.png" alt="ZeroTwo" width="160" />
</p>

<h1 align="center">ZeroTwo</h1>

<p align="center">
  <strong>Zero-dependency, ultra-lightweight autonomous AI agent for low-resource Linux and terminal hardware.</strong>
</p>

<p align="center">
  <a href="https://github.com/as1furrahman/zerotwo/actions"><img src="https://img.shields.io/badge/Build-Passing-brightgreen?style=flat-square&logo=githubactions" alt="Build Status" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" alt="License" /></a>
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go" alt="Go" />
  <img src="https://img.shields.io/badge/Dependencies-0-success?style=flat-square" alt="0 Dependencies" />
  <img src="https://img.shields.io/badge/Binary-<7MB-informational?style=flat-square" alt="Binary Size" />
  <img src="https://img.shields.io/badge/RAM-<12MB-purple?style=flat-square" alt="RAM" />
</p>

---

ZeroTwo is an autonomous personal AI agent and shell assistant built with **zero external dependencies** using only the Go standard library. 

Engineered specifically for low-resource environments—from servers and laptops to Kindle e-readers, Raspberry Pis, and Android (Termux)—ZeroTwo boots in under 15ms, consumes less than 12MB of RAM, and compiles into a single static binary.

## Features

- **Zero External Dependencies**: Pure Go standard library (`go.mod` has 0 third-party packages).
- **Ultra-Low Footprint**: Single < 7MB static binary, < 12MB RSS memory, < 15ms startup time.
- **Hardware Agnostic**: First-class support for x86_64, ARM64, ARMv7 (Kindle / Pi 2), ARMv6 (Pi Zero), and Android.
- **Parallel Dispatch**: Concurrent file inspection, web search, and telemetry with safe sequential mutations.
- **Default-Deny Security**: Centralized authorization gatekeeper, strict SSRF blocking, and delimiter-wrapped taint tracking.
- **Flexible Providers**: Native support for any OpenAI-compatible API (Groq, Ollama, OpenAI, vLLM).

## Quickstart

```bash
# Install via Go
go install github.com/as1furrahman/zerotwo/cmd/zerotwo@latest

# Configure provider (Groq, Ollama, or OpenAI)
export GROQ_API_KEY="your-api-key"

# Run interactive agent
zerotwo
```

## Usage

```bash
# Interactive multi-turn REPL
zerotwo

# Natural language to shell command (-e)
zerotwo -e "find files larger than 100MB modified this week"

# Headless / piped execution (-m)
cat /var/log/nginx/error.log | zerotwo -m "Identify the root cause of these errors"

# Interactive profile learning (-learn)
zerotwo -learn
```

## Cross-Compilation

ZeroTwo cross-compiles cleanly with zero CGO requirements:

```bash
make build                 # Host native binary
make build-armv7           # Kindle Paperwhite / Raspberry Pi 2
make build-armv6           # Raspberry Pi Zero / Pi 1
make build-arm64           # Modern ARM64 servers / SBCs
make build-android-arm64   # Android (Termux)
```

## Security

ZeroTwo operates under a default-deny security model. Mutating file operations, network requests, and shell executions require confirmation. Interactive authorization prompts read `/dev/tty` directly and fail closed in unattended background pipelines. Outbound requests are filtered to prevent SSRF against loopback, private subnets (RFC1918), and cloud metadata endpoints.

## License

[MIT License](LICENSE) © 2026 as1furrahman
