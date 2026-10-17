# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.1.0] - 2026-10-18

### Initial Production Release

#### Added
- **Core Agent Architecture**:
  - Pure Go standard library autonomous agent loop with multi-turn conversation memory.
  - Interactive profile learning interview mode (`-learn`) creating structured user persona memory (`MEMORY.md`).
  - Headless single-turn execution mode (`-m`) for shell pipelines and automated scripting.
  - Natural language shell assistant mode (`-e`) with instant command generation and safety validation.
  - Zero-network dry-run inspector (`-dry-run`) and memory export preview (`-memory-preview`).
- **Default-Deny Security Framework**:
  - Centralized pre-dispatch tool authorization policy (`pkg/auth`).
  - Two-tier shell command safety classifier analyzing privilege escalation, mutating operations, and destructive commands.
  - Hardened SSRF defense engine (`pkg/tools/ssrf.go`) blocking loopback, RFC1918, link-local, carrier-grade NAT, and cloud metadata IP endpoints (`169.254.169.254`, `metadata.google.internal`).
  - Out-of-band taint tracking (`sess.Tainted`) wrapping untrusted web payloads in delimiter blocks and preventing exfiltration.
  - Symlink-traversal hardening resolving real filesystem targets (`filepath.EvalSymlinks`).
  - Bounded subprocess stdout/stderr ring buffers preventing OOM host crashes from infinite child process output.
  - Fail-closed terminal safety querying `/dev/tty` directly.
- **Provider & Model Integration**:
  - High-performance SSE streaming client (`pkg/provider/client.go`) supporting Groq, OpenAI, Anthropic, Ollama, and local vLLM endpoints.
  - Dynamic token budgeting and prompt calibration engine.
  - Automatic Authorization header stripping on cross-origin redirects.
- **Brand Identity**:
  - Official release logo and icon: Matte Graphite Grey (`#18181C`) transparent cutout Arch Cartouche featuring verified raw Zero Two anime geometry from *Darling in the Franxx*.
- **Multi-Architecture Support**:
  - Verified static binary compilation across `linux/amd64`, `linux/arm64`, `linux/armv7` (Kindle, Pi 2), `linux/armv6` (Pi Zero/1), and `android/arm64` (Termux).
  - 100% test pass rate across 310 test units in 9 packages.
