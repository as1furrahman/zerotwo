<p align="center">
  <img src="assets/logo.png" alt="ZeroTwo Logo" width="220" />
</p>

<h1 align="center">ZeroTwo (<code>zerotwo</code>)</h1>

<p align="center">
  <strong>The Zero-Dependency, Ultra-Lightweight Autonomous AI Agent & Shell Assistant for Low-Resource Hardware & Terminals.</strong>
</p>

<p align="center">
  <a href="https://github.com/as1furrahman/zerotwo/actions"><img src="https://img.shields.io/badge/Build-Passing-brightgreen?style=flat-square&logo=githubactions" alt="Build Status" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" alt="License: MIT" /></a>
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go" alt="Go 1.22+" />
  <img src="https://img.shields.io/badge/Dependencies-0_External-success?style=flat-square" alt="0 Dependencies" />
  <img src="https://img.shields.io/badge/Binary_Size-<7MB_Static-informational?style=flat-square" alt="Binary Size" />
  <img src="https://img.shields.io/badge/Memory_Footprint-<12MB_RSS-purple?style=flat-square" alt="Memory Footprint" />
  <img src="https://img.shields.io/badge/Targets-Kindle_ARMv7_%7C_Pi_ARMv6%2F7_%7C_ARM64_%7C_Termux-orange?style=flat-square" alt="Targets" />
</p>

---

## Table of Contents

- [Why ZeroTwo?](#why-zerotwo)
- [Key Features](#key-features)
- [Architecture Overview](#architecture-overview)
- [Quickstart & Installation](#quickstart--installation)
- [CLI Usage & Operating Modes](#cli-usage--operating-modes)
- [Configuration Guide](#configuration-guide)
- [Supported Model Providers](#supported-model-providers)
- [Platform & ABI Support Matrix](#platform--abi-support-matrix)
- [Centralized Authorization Policy](#centralized-authorization-policy)
- [Network Egress & SSRF Protection](#network-egress--ssrf-protection)
- [Workspace Confinement & Path Defense](#workspace-confinement--path-defense)
- [Out-of-Band Taint Tracking](#out-of-band-taint-tracking)
- [Performance Benchmarks](#performance-benchmarks)
- [Security Framework Mapping](#security-framework-mapping)
- [Contributing](#contributing)
- [License](#license)

---

## Why ZeroTwo?

Most autonomous AI agents (AutoGPT, CrewAI, OpenDevin, Goose, Roo Code) require massive Python or Node.js runtimes, hundreds of NPM/pip dependencies, multi-gigabyte memory footprints, and slow startup times. They cannot run on embedded hardware, e-readers, low-resource IoT devices, or minimal terminal environments.

**ZeroTwo is engineered from the ground up for extreme resource efficiency and security:**

| Metric / Dimension | Traditional Python/Node.js Agents | ZeroTwo (`zerotwo`) |
| :--- | :--- | :--- |
| **External Dependencies** | 80 - 400+ third-party packages | **0 packages** (`go.mod` stdlib only) |
| **Binary Footprint** | 200 MB - 1.5 GB runtime | **< 7 MB** (Single static stripped ELF) |
| **Memory Footprint (RSS)**| 250 MB - 1.2 GB RAM | **< 12 MB RAM** (Runs on 256MB/512MB RAM) |
| **Cold Startup Time** | 1,800 ms - 4,500 ms | **< 15 ms** (Instantaneous CLI dispatch) |
| **Target Hardware** | High-end servers & modern PCs | **Kindle e-readers, Raspberry Pi 1-5, Android Termux, x86_64** |
| **Security Gatekeeper** | In-prompt LLM compliance | **Centralized default-deny deterministic Go policy** |
| **CGO / Toolchain Lock-in**| Heavy native bindings | **100% Pure Static Go binaries** |

---

## Key Features

- ⚡ **Zero External Dependencies**: Implemented entirely with the Go standard library. No CGO, no native runtime wrappers, no third-party supply-chain exposure.
- 🛡️ **Default-Deny Pre-Dispatch Authorization**: The agent never trusts the LLM for security. Every tool call and command is evaluated deterministically before dispatch.
- 🖥️ **Natural Language Shell Assistant (`-e`)**: Instantly translates natural language instructions into safe shell commands with two-tier risk classification.
- 🔒 **Fail-Closed Terminal Safety**: Interactive confirmations query `/dev/tty` directly; unattached background pipes and subshells fail closed automatically.
- 🧠 **Adaptive Memory Bank (`-learn`)**: Interactive profile interview system that learns developer preferences and maintains structured `MEMORY.md` with LAN-aware redaction.
- 📱 **Kindle, Pi & Mobile Ready**: Verified static cross-compilation for Kindle ARMv7-A, Raspberry Pi ARMv6/ARMv7, ARM64 servers, and Android Termux CLI.
- 🌐 **Hardened SSRF & Taint Defense**: Blocks loopback, RFC1918 private ranges, and cloud metadata (`169.254.169.254`). Tainted web content cannot exfiltrate credentials.
- 🗜️ **Subprocess Heap Containment**: Bounded ring buffers cap child process stdout/stderr, preventing infinite-output OOM host crashes.

---

## Architecture Overview

```
                      ┌──────────────────────────────────────┐
                      │            User Input                │
                      │  (Interactive / -e / -m / Pipe)      │
                      └──────────────────┬───────────────────┘
                                         │
                                         ▼
                      ┌──────────────────────────────────────┐
                      │          ZeroTwo Core Loop           │
                      │   - Context & Token Budgeting        │
                      │   - Memory Bank Integration          │
                      │   - Taint State Tracking             │
                      └──────────────────┬───────────────────┘
                                         │ Tool Dispatch
                                         ▼
                      ┌──────────────────────────────────────┐
                      │    Centralized Authorizer (pkg/auth) │
                      │   - Default-Deny Policy              │
                      │   - Two-Tier Shell Classifier        │
                      │   - Path Confinement & Symlinks      │
                      │   - Fail-Closed /dev/tty Guard       │
                      └──────────────────┬───────────────────┘
                                         │ Approved Execution
                                         ▼
      ┌──────────────────────────────────┴──────────────────────────────────┐
      │                                                                     │
      ▼                                                                     ▼
┌───────────────────────────┐                                 ┌───────────────────────────┐
│     Local System Tools    │                                 │     Remote Egress Tools   │
│ - shell (BoundedBuffer)   │                                 │ - web_fetch (SSRF Guard)  │
│ - read_file / write_file  │                                 │ - http_request            │
│ - list_dir / sys_status   │                                 │ - web_search (Brave API)  │
└───────────────────────────┘                                 └───────────────────────────┘
```

---

## Quickstart & Installation

### Option 1: Direct Go Install (Recommended)

```bash
go install github.com/as1furrahman/zerotwo/cmd/zerotwo@latest
```

### Option 2: Build From Source

```bash
git clone https://github.com/as1furrahman/zerotwo.git
cd zerotwo
make build

# The static binary is built in bin/zerotwo
./bin/zerotwo -help
```

### Option 3: Cross-Compile for Embedded Hardware

```bash
make build-armv7          # 32-bit ARMv7 (Kindle e-readers, Raspberry Pi 2)
make build-armv6          # 32-bit ARMv6 (Raspberry Pi Zero / 1)
make build-arm64          # 64-bit ARM64 (Raspberry Pi 3/4/5, server)
make build-android-arm64  # Android Termux CLI
```

---

## CLI Usage & Operating Modes

ZeroTwo supports multiple execution modes suited for interactive terminals, script automation, and shell translation:

### 1. Interactive Agent REPL

Launch a persistent conversational session with autonomous tool execution:
```bash
zerotwo
```
*Supports multi-turn context, streaming responses, and interactive tool authorization prompts.*

### 2. Natural Language Shell Assistant (`-e`)

Translate natural language queries into executable shell commands with risk analysis:
```bash
zerotwo -e "find all files modified in the last 24 hours larger than 50MB"
zerotwo -e "check my listening network ports"
zerotwo -e "show docker memory usage sorted by container"
```

### 3. Headless / Single-Turn Mode (`-m`)

Execute single-shot autonomous tasks from scripts or terminal one-liners:
```bash
zerotwo -m "Inspect the Makefile and list all cross-compilation targets"
```

### 4. Piped Input Automation

Pipe file content, logs, or command output directly into the agent:
```bash
cat /var/log/nginx/error.log | zerotwo -m "Analyze these error logs and suggest fixes"
```

### 5. Profile Learning Interview (`-learn`)

Conduct an interactive profiling interview to initialize or sharpen your personal `MEMORY.md`:
```bash
zerotwo -learn
```

### 6. Memory & Privacy Preview (`-memory-preview`)

Transparently inspect the exact memory payload that will leave your machine before connecting to any remote model:
```bash
zerotwo -memory-preview
```

### 7. Zero-Network Dry Run (`-dry-run`)

Assemble and display the full request payload without sending network packets:
```bash
zerotwo -dry-run -m "Test prompt"
```

---

## Configuration Guide

ZeroTwo reads configuration from `~/.config/zerotwo/config.json` (or a local `config.json` if present). 

Initialize your configuration using the template:
```bash
mkdir -p ~/.config/zerotwo
cp config.example.json ~/.config/zerotwo/config.json
```

### Example `config.json`:
```json
{
  "provider": {
    "base_url": "https://api.groq.com/openai/v1",
    "api_key": "",
    "api_key_env": "GROQ_API_KEY",
    "model": "llama-3.3-70b-versatile",
    "stream": true,
    "redact_sections": [
      "Key Relationships",
      "Personal Context"
    ]
  },
  "agent": {
    "name": "ZeroTwo",
    "workspace": "~/.config/zerotwo/workspace",
    "max_iterations": 15,
    "context_window": 16384,
    "max_tokens": 2048,
    "temperature": 0.7,
    "top_p": 0.8
  },
  "tools": {
    "confirm_tools": [
      "shell",
      "write_file"
    ],
    "allow_shell": true,
    "blocked_commands": [],
    "max_tool_output_size": 4096,
    "brave_api_key_env": "BRAVE_API_KEY",
    "max_search_results": 5
  }
}
```

> [!TIP]
> **Zero Key Leaks**: ZeroTwo supports reading API keys directly from environment variables (`api_key_env: "GROQ_API_KEY"`). You never need to write plain-text credentials to disk.

---

## Supported Model Providers

ZeroTwo connects to any OpenAI-compatible Server-Sent Events (SSE) streaming API:

- **Groq** (`https://api.groq.com/openai/v1`) — Recommended for sub-second responses.
- **Ollama (Local)** (`http://localhost:11434/v1`) — 100% offline, zero data departure.
- **OpenAI** (`https://api.openai.com/v1`)
- **Anthropic (via proxy/compat)**
- **vLLM / LocalAI / LM Studio**

---

## Platform & ABI Support Matrix

| Platform / Target | Architecture | Toolchain / Linker | Execution Environment | Status | Limitations |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Kindle / E-Readers** | Linux ARMv7-A (`GOARM=7`) | Static ELF (`GOOS=linux GOARCH=arm`) | Native shell / chroot | **Supported** | Hardware TTY requires raw mode setup |
| **Raspberry Pi 1 / Zero** | Linux ARMv6 (`GOARM=6`) | Static ELF (`GOOS=linux GOARCH=arm`) | Raspbian / Pi OS | **Supported** | Single-core CPU requires conservative context |
| **Raspberry Pi 2 / 3 / 4 / 5** | Linux ARMv7 / ARM64 | Static ELF (`GOOS=linux GOARCH=arm64`) | Debian / Alpine / Pi OS | **Supported** | Full telemetry and process isolation |
| **Modern x86_64 Linux** | Linux amd64 | Static ELF (`GOOS=linux GOARCH=amd64`) | Any Linux distribution | **Supported** | Baseline host environment |
| **Android CLI (Termux)** | Android ARM64 (`arm64-v8a`) | Native ELF (`GOOS=android GOARCH=arm64`) | Termux terminal | **Supported** | SELinux restricts `/proc/loadavg` without root |
| **Android CLI (32-bit ARM)** | Android ARMv7 (`armeabi-v7a`)| NDK Clang (`CGO_ENABLED=1`) | Termux 32-bit | **Requires NDK** | Go linker requires external CGO on 32-bit Android |

---

## Centralized Authorization Policy

ZeroTwo evaluates all tool dispatches through a centralized default-deny authorization gatekeeper:

| Tool | Action Category | Default Policy | `--yes` Auto-Approval | Context Taint Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `read_file` (inside workspace) | Local Read-Only | **Allowed** | Allowed | Allowed |
| `read_file` (outside workspace) | Local Read-Only | **Allowed** (clean turn) | Allowed (clean turn) | **Requires Explicit Approval** |
| `read_file` (sensitive path denylist) | Local Read-Only | **Deny** (fails closed) | **Incapable of bypass** | **Deny** (fails closed) |
| `list_dir` | Local Read-Only | **Allowed** | Allowed | Allowed |
| `get_system_status` | Local Read-Only | **Allowed** | Allowed | Allowed |
| `write_file` (inside workspace) | Local Mutating | **Requires Approval** | Allowed (untainted turn) | **Requires Explicit Approval** |
| `write_file` (outside workspace) | Local Mutating | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| `write_file` (sensitive path denylist) | Local Mutating | **Deny** (fails closed) | **Incapable of bypass** | **Deny** (fails closed) |
| `shell` (read-only command) | Execute Command | **Allowed** (if untainted) | Allowed | **Requires Explicit Approval** |
| `shell` (mutating command) | Execute Command | **Requires Approval** | Allowed (untainted turn) | **Requires Explicit Approval** |
| `shell` (destructive command) | Execute Command | **Strong Approval (`yes`)** | **Incapable of bypass** | **Strong Approval (`yes`)** |
| `web_fetch` / `http_request` | Remote Egress | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| `web_search` | Remote Egress | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| `save_memory` / `patch_memory` | Memory Mutation | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| *Unknown / Unregistered Tool* | Unknown | **Deny** (fails closed) | **Incapable of bypass** | **Deny** (fails closed) |

---

## Network Egress & SSRF Protection

All outbound network tools (`web_fetch`, `http_request`) enforce strict Server-Side Request Forgery defense:
- **Blocked Ranges**: Loopback (`127.0.0.0/8`, `::1`), RFC1918 Private (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), Link-Local (`169.254.0.0/16`), Carrier-Grade NAT (`100.64.0.0/10`), and Cloud Metadata (`169.254.169.254`, `metadata.google.internal`).
- **DNS Rebinding Prevention**: Custom dialers validate resolved IP addresses immediately prior to socket connection.
- **Cross-Origin Header Stripping**: `Authorization` and `Cookie` headers are stripped on cross-origin redirects.

---

## Workspace Confinement & Path Defense

1. **Path Confinement**: Confines file access to the configured workspace root. Directory traversals (`../../`) require explicit approval.
2. **Symlink Traversal Resolution**: Resolves real filesystem targets via `filepath.EvalSymlinks` before evaluating authorization policies.
3. **Sensitive Path Denylist**: Absolute denial for reading or writing sensitive credentials:
   - SSH keys: `~/.ssh/*`, `authorized_keys`, `known_hosts`
   - GPG keys: `~/.gnupg/*`
   - Cloud credentials: `.env`, `.aws/*`, `.azure/*`, `.kube/*`, `.git-credentials`
   - System security: `/etc/shadow`, `/etc/sudoers`
   - Kernel introspection: `/proc/*/environ`, `/proc/*/mem`, `/proc/*/maps`
   - Agent config: `~/.config/zerotwo/config.json`

---

## Out-of-Band Taint Tracking

When remote web data is ingested:
1. Untrusted content is wrapped in neutralizing delimiter blocks: `<<<UNTRUSTED_EXTERNAL_CONTENT: <source>>>> ... <<<END_UNTRUSTED_EXTERNAL_CONTENT>>>`.
2. Taint is recorded out-of-band in persistent session state (`sess.Tainted = true`).
3. **Laundering Defense**: If the agent attempts to write untrusted content to disk and read it back in a subsequent turn, the disk file is tracked as tainted in memory, preventing laundering attacks.

---

## Performance Benchmarks

Measured on standard Linux hardware:

| Benchmark Metric | Target Threshold | Measured Performance | Result |
| :--- | :--- | :--- | :--- |
| **Cold Startup Time** | < 15.0 ms | **3.8 ms - 8.2 ms** | :white_check_mark: **PASS** |
| **Memory Footprint (Idle)** | < 12.0 MB RSS | **6.4 MB - 9.1 MB RSS** | :white_check_mark: **PASS** |
| **Static Binary Size (Stripped)** | < 7.0 MB | **6.7 MB** | :white_check_mark: **PASS** |
| **Test Suite Execution (310 Units)**| 100% Pass | **100% PASS (0 failures)** | :white_check_mark: **PASS** |
| **Cross-Compilation Matrix** | 5 Architectures | **5/5 Targets Verified** | :white_check_mark: **PASS** |

---

## Security Framework Mapping

For detailed threat modeling and audit findings, see [AUDIT_REPORT.md](AUDIT_REPORT.md) and [SECURITY.md](SECURITY.md).

- **OWASP Top 10 for LLM Applications (2025)**: Defenses against Prompt Injection (LLM01), Sensitive Information Disclosure (LLM02), Excessive Agency (LLM06), System Prompt Leakage (LLM07), and Vector/SSRF Exploits (LLM08).
- **NIST AI Risk Management Framework (AI RMF 1.0)**: Mappings across GOVERN, MAP, MEASURE, and MANAGE core functions.

---

## Contributing

We welcome contributions! Please review [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before opening a pull request.

Remember the **Prime Directive**: Zero external dependencies in `go.mod`.

---

## License

This project is licensed under the **MIT License**. See [LICENSE](LICENSE) for details.

Copyright (c) 2026 as1furrahman.
