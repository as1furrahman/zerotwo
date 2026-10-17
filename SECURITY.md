# Security Policy

## Reporting Security Vulnerabilities

The ZeroTwo team takes security vulnerabilities seriously. Because ZeroTwo executes shell commands, inspects filesystems, and interacts with remote LLMs, we implement strict defense-in-depth principles.

If you believe you have discovered a vulnerability in ZeroTwo (e.g. sandbox escapes, authorization bypasses, SSRF flaws, or credential leakage), please **do not open a public GitHub issue**.

Instead, please report it via one of the following methods:

1. **GitHub Security Advisory**: Submit a private advisory report via [GitHub Security Advisories](https://github.com/as1furrahman/zerotwo/security/advisories/new).
2. **Encrypted Email**: Send the details to `asifur.rahman@tutanota.de` with subject `[SECURITY] ZeroTwo Vulnerability Report`.

Please include:
- A description of the vulnerability and its potential impact.
- Step-by-step instructions or proof-of-concept (PoC) code to reproduce the issue.
- Operating system, architecture, and Go runtime version.

We will acknowledge receipt within 48 hours and work on a remediation before any public disclosure.

---

## Supported Versions

| Version | Supported |
| :--- | :--- |
| `0.1.x` (Latest / `master`) | :white_check_mark: |
| Older releases | :x: |

---

## Core Security Architecture & Principles

ZeroTwo does not rely on LLM system prompt instructions or model compliance for security. All security boundaries are enforced by deterministic Go code immediately before tool dispatch:

1. **Default-Deny Pre-Dispatch Authorization (`pkg/auth`)**:
   Every tool invocation passes through a centralized authorizer. Egress networking, mutating commands, and persistent memory mutations strictly require explicit user approval.
2. **Fail-Closed Terminal Guard**:
   Interactive confirmations query `/dev/tty` directly. If executed in an unattached background pipe, headless daemon, or subshell, confirmations fail closed immediately.
3. **Workspace Path Confinement & Symlink Evaluation (`pkg/tools/files.go`)**:
   Filesystem reads and writes resolve real symlink targets (`filepath.EvalSymlinks`) to prevent directory traversal and symlink-based boundary escapes.
4. **Out-of-Band Taint Tracking (`pkg/agent/loop.go`)**:
   Remote content ingested via web tools is wrapped in neutralizing delimiter blocks and tracked out-of-band in persistent session state (`sess.Tainted`), preventing prompt-injection-driven credential theft.
5. **Strict SSRF Defense (`pkg/tools/ssrf.go`)**:
   Custom HTTP dialers re-resolve DNS immediately prior to socket establishment and block loopback, RFC1918 private ranges, link-local, carrier-grade NAT, and cloud metadata IP endpoints (`169.254.169.254`, `metadata.google.internal`).
6. **Bounded Subprocess Buffers (`pkg/tools/shell.go`)**:
   Shell process stdout/stderr streams are piped through fixed-capacity bounded buffers, preventing child processes from crashing the host via infinite output OOM attacks.

For a full severity-ranked audit report, refer to [AUDIT_REPORT.md](AUDIT_REPORT.md).
