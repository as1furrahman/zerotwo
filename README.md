<p align="center">
  <img src="assets/logo.png" alt="ZeroTwo Logo" width="200" />
</p>

<h1 align="center">ZeroTwo</h1>

<p align="center">
  <strong>The zero-dependency, ultra-lightweight personal AI agent for low-resource Linux and terminal hardware.</strong>
</p>

<p align="center">
  <a href="https://github.com/as1furrahman/zerotwo/actions"><img src="https://img.shields.io/badge/Build-Passing-brightgreen?style=flat-square&logo=githubactions" alt="Build Status" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" alt="License: MIT" /></a>
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go" alt="Go 1.22+" />
  <img src="https://img.shields.io/badge/Dependencies-0_External-success?style=flat-square" alt="0 Dependencies" />
  <img src="https://img.shields.io/badge/Binary-<7MB_Static-informational?style=flat-square" alt="Binary Size" />
  <img src="https://img.shields.io/badge/Memory-<12MB_RSS-purple?style=flat-square" alt="Memory Footprint" />
</p>

---

ZeroTwo (`zerotwo`) is an autonomous personal AI agent and shell assistant built with **zero external dependencies** using the Go standard library. 

Engineered specifically for low-resource environments—from modern x86/ARM64 servers to Kindle e-readers, Raspberry Pis, and Android (Termux)—ZeroTwo boots in under 15ms, uses less than 12MB of RAM, and compiles into a single static binary.

---

## Highlights

- ⚡ **Zero External Dependencies**: Standard library Go only (`go.mod` has 0 third-party packages).
- 🪶 **Ultra-Low Resource Footprint**: < 7MB static binary, < 12MB RSS RAM, < 15ms startup time.
- 🛡️ **Default-Deny Security**: Centralized pre-dispatch authorization gatekeeper for all tool calls and commands.
- 🖥️ **Natural Language Shell Assistant**: Instant `-e` mode with intelligent risk classification.
- 🧠 **Persistent Context & Memory**: Adaptive `MEMORY.md` with interactive interview learning (`-learn`) and LAN-aware privacy redactions.
- 📱 **Low-Resource Hardware Ready**: Verified cross-compilation for Kindle ARMv7, Raspberry Pi ARMv6/7, and Android Termux.
- 🌐 **Hardened Network Defense**: Out-of-band taint tracking and strict SSRF blocking (loopback, private subnets, cloud metadata).

---

## Quickstart

### 1. Installation

**Via `go install` (Recommended):**
```bash
go install github.com/as1furrahman/zerotwo/cmd/zerotwo@latest
```

**Or build from source:**
```bash
git clone https://github.com/as1furrahman/zerotwo.git
cd zerotwo
make build
# Binary is located at bin/zerotwo
```

### 2. Configuration

Set your model provider API key as an environment variable:

```bash
# Recommended: Groq (ultra-fast sub-second responses)
export GROQ_API_KEY="your-groq-api-key"

# Or use local Ollama (100% offline, zero network departure)
# zerotwo connects to http://localhost:11434/v1 by default if configured
```

Create an optional configuration file:
```bash
mkdir -p ~/.config/zerotwo
cp config.example.json ~/.config/zerotwo/config.json
```

### 3. Run

```bash
# Start an interactive session
zerotwo

# Or translate natural language to shell commands
zerotwo -e "show top 5 memory consuming processes"

# Or run a single-shot headless task
zerotwo -m "Analyze the project structure and summarize it"
```

---

## Usage Modes

### Interactive REPL
```bash
zerotwo
```
Autonomous multi-turn agent with streaming responses, tool execution, and interactive permission prompts.

### Shell Assistant (`-e`)
```bash
zerotwo -e "find files larger than 100MB modified this week"
zerotwo -e "docker stats sorted by memory"
```
Translates natural language instructions into safe shell commands with two-tier risk classification.

### Headless & Piped Automation (`-m`)
```bash
# Single prompt
zerotwo -m "Summarize git log for the last 3 commits"

# Piped logs or file content
cat /var/log/nginx/error.log | zerotwo -m "Identify the root cause of these errors"
```

### Profile Learning (`-learn`)
```bash
zerotwo -learn
```
Launches an interactive interview to learn your coding style, preferred tools, and project context, saving them to `~/.config/zerotwo/MEMORY.md`.

### Privacy & Dry Run
```bash
# Preview what memory leaves the device
zerotwo -memory-preview

# Zero-network execution test
zerotwo -dry-run -m "Test prompt"
```

---

## Supported Providers

ZeroTwo connects to any OpenAI-compatible SSE streaming API:

| Provider | Base URL | Recommended Model |
| :--- | :--- | :--- |
| **Groq** | `https://api.groq.com/openai/v1` | `llama-3.3-70b-versatile` |
| **Ollama (Local)** | `http://localhost:11434/v1` | `qwen2.5-coder`, `llama3.2` |
| **OpenAI** | `https://api.openai.com/v1` | `gpt-4o-mini`, `gpt-4o` |
| **vLLM / LocalAI** | `http://localhost:8000/v1` | Any hosted model |

---

## Supported Platforms

| Platform | Architecture | Status |
| :--- | :--- | :--- |
| **Kindle / E-Readers** | Linux ARMv7-A (`GOARM=7`) | :white_check_mark: Supported |
| **Raspberry Pi 1 / Zero** | Linux ARMv6 (`GOARM=6`) | :white_check_mark: Supported |
| **Raspberry Pi 2 / 3 / 4 / 5** | Linux ARMv7 / ARM64 | :white_check_mark: Supported |
| **Modern Linux** | Linux x86_64 (`amd64`) | :white_check_mark: Supported |
| **Android CLI** | Android ARM64 (Termux) | :white_check_mark: Supported |

Cross-compile for any target with Make:
```bash
make build-armv7          # Kindle, Pi 2
make build-armv6          # Pi Zero, Pi 1
make build-arm64          # Pi 3/4/5, servers
make build-android-arm64  # Android Termux
```

---

## Security

ZeroTwo implements a strict **Default-Deny** security model:
- **Authorization Gatekeeper**: Mutating file writes, network requests, and destructive shell commands require explicit user confirmation.
- **Fail-Closed Terminal Guard**: Interactive prompts query `/dev/tty` directly; unattached background pipelines fail closed.
- **SSRF Defense**: Automatically blocks loopback, private subnets (RFC1918), and cloud metadata endpoints (`169.254.169.254`).
- **Taint Tracking**: Web content is wrapped in neutralizing delimiter blocks and tracked out-of-band to prevent prompt injection and data exfiltration.

For full audit findings and threat modeling, see [AUDIT_REPORT.md](AUDIT_REPORT.md) and [SECURITY.md](SECURITY.md).

---

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before opening a pull request.

> **Prime Directive**: Zero external dependencies. All contributions must use the Go standard library only.

---

## License

[MIT License](LICENSE) © 2026 as1furrahman.
