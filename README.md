# ZeroTwo (`zerotwo`)

An ultra-lightweight, zero-dependency personal AI agent and CLI tool written in Go (standard library only) for low-resource Linux devices and mobile environments: Kindle ARMv7, Raspberry Pi ARMv6/ARMv7, modern ARM64 systems, and Android terminal environments (Termux).

---

## Architecture & Design Principles

- **Zero External Dependencies**: Standard library Go only (`go.mod` has 0 third-party packages).
- **Extreme Resource Efficiency**: Low memory footprint (target < 12 MB RSS on low-resource Linux), fast startup (< 15 ms), statically linked binary (< 7 MB stripped).
- **Default-Deny Security**: Centralized pre-dispatch authorization policy across all agent tool calls.
- **Fail-Closed Terminal Safety**: Interactive confirmations query `/dev/tty` directly; unattached background pipelines fail closed.
- **Out-of-Band Taint Tracking**: Remote content ingestion permanently marks turn and session state to prevent prompt injection and data exfiltration.

---

## Platform & ABI Support Matrix

ZeroTwo distinguishes between **Linux CLI**, **Android Terminal CLI (Termux)**, and **Native Android Application (APK)**. They operate under fundamentally different ABIs, system paths, dynamic linkers, and permission models.

| Platform / Target | ABI / Architecture | Toolchain / Linker | Execution Environment | Status & Support Level | Verified Limitations |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Kindle / E-Readers** | Linux ARMv7-A (`GOARM=7`) | `GOOS=linux GOARCH=arm` (Static ELF) | Linux chroot / native shell (`/bin/sh`) | **Supported** (QEMU verified) | Hardware TTY may require raw terminal setup; no battery telemetry on some models |
| **Raspberry Pi 1 / Zero** | Linux ARMv6 (`GOARM=6`) | `GOOS=linux GOARCH=arm` (Static ELF) | Raspbian / Raspberry Pi OS (`/bin/sh`) | **Supported** (QEMU arm1176 verified) | Single-core CPU requires conservative context windows; slow TLS handshakes |
| **Raspberry Pi 2 / 3 / 4 / 5** | Linux ARMv7 / ARM64 | `GOOS=linux GOARCH=arm64` (Static ELF) | Raspberry Pi OS / Debian / Alpine | **Supported** (Host & QEMU verified) | Full system status and process-group isolation supported |
| **Modern x86_64 Linux** | Linux amd64 | `GOOS=linux GOARCH=amd64` (Static ELF) | Any Linux distribution | **Supported** (Primary development host) | Baseline development environment |
| **Android CLI (Termux)** | Android ARM64 (`arm64-v8a`) | `GOOS=android GOARCH=arm64` (PIE, `/system/bin/linker64`) or `GOOS=linux` | Termux terminal (`/data/data/com.termux/files/usr/bin/sh`) | **Supported** (Native ELF compilation verified) | SELinux restricts `/proc/kmsg`, `/proc/loadavg` without root; no `/dev/tty` outside terminal |
| **Android CLI (32-bit ARM)** | Android ARMv7 (`armeabi-v7a`) | NDK Clang (`armv7a-linux-androideabi-clang` with `CGO_ENABLED=1`) | Termux 32-bit | **Requires NDK CGO** | Go compiler requires external CGO linking for 32-bit Android; `CGO_ENABLED=0` unsupported by Go linker |
| **Android ARMv6** | `armeabi` (ARMv6) | *None* | *N/A* | **Unsupported** | Deprecated in NDK r16 and completely removed in NDK r17; modern Android requires ARMv7-A minimum |
| **Native Android App (APK)** | Android GUI / Service | Java / Kotlin + Android SDK + JNI / NDK | Android App Sandbox | **Roadmap / Architectural Scope** | CLI binary cannot run as an APK; requires Android Service wrapper, JNI bridge, and Foreground Service permissions |

---

## Architectural Delineation: Android vs. Low-Resource Linux

Do not conflate standard Linux ARM CLI binaries with Android support:

1. **Linker & Libc**: Linux CLI binaries target glibc or musl. Android binaries require the Android Bionic dynamic linker (`/system/bin/linker64` or `/system/bin/linker`) and Position-Independent Executable (PIE) format.
2. **Standard Paths**: Android lacks `/bin/sh`, `/tmp`, and `/usr`. Shell execution falls back to Termux prefix (`/data/data/com.termux/files/usr/bin/sh`) or Android system shell (`/system/bin/sh`).
3. **SELinux & Permissions**: Android enforces strict SELinux domain policies. CLI tools running in non-root app contexts cannot access hardware telemetry (`/proc/loadavg`, `/sys/class/thermal`) or external storage paths without Android runtime permissions.
4. **Native App (APK) Requirements**: To run ZeroTwo as a native background service or app on unrooted Android, it must be packaged into an APK containing:
   - An Android Foreground Service with a persistent notification.
   - A JNI/CGO wrapper bridging Go functions to Kotlin/Java.
   - Android-specific power management (`WAKE_LOCK`, battery optimization exemptions).

---

## Centralized Authorization Policy (`pkg/auth`)

Immediately before any tool is dispatched by the agent loop, the request is evaluated by a centralized authorization policy. The agent never relies on LLM compliance or system-prompt instructions for security.

### Tool Classification Matrix

| Tool | Action Category | Default Policy | `--yes` Auto-Approval | Context Taint Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `read_file` (inside workspace) | Local Read-Only | **Allowed** | Allowed | Allowed |
| `read_file` (outside workspace) | Local Read-Only | **Allowed** (clean turn) | Allowed (clean turn) | **Requires Explicit Approval** (prevents secret exfiltration) |
| `read_file` (sensitive path denylist) | Local Read-Only | **Deny** (fails closed) | **Incapable of bypass** | **Deny** (fails closed) |
| `list_dir` | Local Read-Only | **Allowed** | Allowed | Allowed |
| `get_system_status` | Local Read-Only | **Allowed** | Allowed | Allowed |
| `write_file` (inside workspace) | Local Mutating | **Requires Approval** (if in `confirm_tools`) | Allowed (only if turn untainted) | **Requires Explicit Approval** |
| `write_file` (outside workspace) | Local Mutating | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| `write_file` (sensitive path denylist) | Local Mutating | **Deny** (fails closed) | **Incapable of bypass** | **Deny** (fails closed) |
| `shell` (read-only command) | Execute Command | **Allowed** (if untainted) | Allowed | **Requires Explicit Approval** |
| `shell` (mutating command) | Execute Command | **Requires Approval** | Allowed (only if turn untainted) | **Requires Explicit Approval** |
| `shell` (destructive command) | Execute Command | **Strong Approval (`yes`)** | **Incapable of bypass** | **Strong Approval (`yes`)** |
| `shell` (blocked commands) | Execute Command | **Deny** (fails closed) | **Incapable of bypass** | **Deny** (fails closed) |
| `web_fetch` | Remote Egress | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| `http_request` | Remote Egress | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| `web_search` | Remote Egress | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| `save_memory` | Memory Mutation | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| `patch_memory` | Memory Mutation | **Requires Approval** | **Incapable of bypass** | **Requires Explicit Approval** |
| *Unknown / Unregistered Tool* | Unknown | **Deny** (fails closed) | **Incapable of bypass** | **Deny** (fails closed) |

---

## Network Egress & SSRF Protection (`pkg/tools/ssrf.go`)

All HTTP tools (`web_fetch`, `http_request`) utilize a hardened HTTP transport with strict Server-Side Request Forgery (SSRF) defense:

- **Scheme Validation**: Strictly allows `http` and `https`. Schemes like `file://`, `gopher://`, `ftp://` are immediately rejected.
- **IP Address Validation**:
  - Loopback: `127.0.0.0/8`, `::1`
  - RFC1918 Private: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
  - Link-Local: `169.254.0.0/16`, `fe80::/10`
  - Carrier-Grade NAT: `100.64.0.0/10`
  - Cloud Metadata Endpoints: `169.254.169.254`, `metadata.google.internal`
- **DNS Rebinding Prevention**: Custom dialer re-resolves and validates all host IPs immediately before establishing the socket connection.
- **Redirect Revalidation**: Every redirect hop is re-validated through the complete SSRF policy.
- **Header Stripping**: Sensitive headers (`Authorization`, `Cookie`) are stripped if a redirect crosses to a different host.
- **Resource Bounds**: Request bodies capped at 64 KB; response sizes strictly bounded.

---

## Workspace Confinement & Sensitive Path Defense

1. **Workspace Confinement**: File operations are confined to the configured workspace root (`cfg.Agent.Workspace`). Relative path traversals (`../../`) and paths outside the workspace require explicit confirmation and cannot be auto-approved via `--yes`.
2. **Symlink Traversal Prevention**: Evaluates both literal path strings and real symlink targets (`filepath.EvalSymlinks`) before authorization and dispatch.
3. **Sensitive Path Denylist**: Absolute denial for reading or writing sensitive system files, keys, and credentials:
   - SSH keys: `~/.ssh/id_rsa`, `~/.ssh/id_ed25519`, `authorized_keys`, `known_hosts`
   - GPG keys: `~/.gnupg/*`
   - Cloud & API credentials: `.env`, `.aws/*`, `.azure/*`, `.kube/*`, `.netrc`, `.git-credentials`
   - System auth: `/etc/shadow`, `/etc/gshadow`, `/etc/sudoers`
   - Process introspection: `/proc/*/environ`, `/proc/*/mem`, `/proc/*/cmdline`, `/proc/*/maps`, `/proc/*/fd`
   - Agent config: `~/.config/zerotwo/config.json` (protects live API keys)

---

## Out-of-Band Taint Tracking & Laundering Defense

When remote data is ingested (via `web_fetch`, `web_search`, or `http_request`):
1. Untrusted content is wrapped in delimited blocks: `<<<UNTRUSTED_EXTERNAL_CONTENT: <source>>>> ... <<<END_UNTRUSTED_EXTERNAL_CONTENT>>>`. Any internal delimiters inside the fetched payload are neutralized.
2. Taint is recorded out-of-band in `sess.Tainted = true`.
3. **Persistence Across Trimming & Reloads**: Because taint is stored directly on the `Session` struct in persistent storage (`sessions/<id>.json`), trimming old messages to fit token budgets does **not** reset the taint flag.
4. **Disk Laundering Prevention**: If the model writes untrusted content to disk via `write_file`, the target file is marked tainted in memory (`MarkFileTainted`). Reading that file back in a later turn re-wraps the contents in untrusted markers.

---

## Reliability & Resource Limits

- **Bounded Piped Stdin**: Piped inputs exceeding 10 MB are rejected immediately with an exit code of 1, preventing partial execution of truncated commands.
- **Bounded SSE Streaming Lines**: SSE line buffer length is capped at 256 KB (`maxSSELineBytes`), preventing runaway memory allocations on infinite lines.
- **Safe Empty/Malformed Provider Response Handling**: Handled cleanly without panics; malformed tool call arguments produce structured observation feedback.
- **Session Persistence Logging**: All session save errors are explicitly reported to `os.Stderr`.
- **Token & Context Budgeting**: Automatic prompt calibration dynamically tracks observed prompt tokens against estimations.

---

## Privacy Policy & Data Departure Matrix

> **Scope Disclosure**:
> Section redaction (`redact_sections`) **only applies to `MEMORY.md`**.
> User prompts and raw outputs from executed tools (e.g. `read_file`, `shell`) **are transmitted to the configured model provider endpoint**.

| Provider Endpoint Type | `send_memory` Default | What Leaves the Device? |
| :--- | :--- | :--- |
| **Localhost / LAN Host** (`localhost`, `127.0.0.1`, `192.168.*`, `10.*`) | `true` (enabled) | System prompt + unredacted `MEMORY.md` + conversation history + tool outputs over local network. |
| **Public Cloud APIs** (`api.groq.com`, `googleapis.com`, `api.openai.com`, etc.) | `false` (disabled) | System prompt + conversation history + tool outputs. **No `MEMORY.md` is sent** unless explicitly enabled. |

To inspect what memory leaves the device:
```bash
zerotwo -memory-preview
```

To perform a zero-network dry run:
```bash
zerotwo -dry-run -m "Your prompt here"
```

---

## Build & Testing Instructions

ZeroTwo requires Go 1.23+ and no external dependencies:

```bash
# Build native binary
make build

# Build Linux ARM binaries
make build-armv7    # 32-bit ARMv7 (Kindle, Pi 2)
make build-armv6    # 32-bit ARMv6 (Pi Zero / 1)
make build-arm64    # 64-bit ARM64 (Pi 3/4/5, server)

# Build Android ARM64 binary (Termux)
make build-android-arm64

# Run all unit tests
make test

# Run race detector (requires CGO / gcc on host)
make test-race

# Run QEMU emulation tests
make test-qemu
```

---

## Security Framework Mapping & Gap Disclosure

ZeroTwo implements defensive controls informed by cybersecurity frameworks. **Disclaimer**: ZeroTwo is an open-source research and utility project. These controls represent engineering hardening measures and do **not** constitute formal compliance certification or third-party audit verification.

### OWASP Top 10 for LLM Applications (2025)

| Risk Category | ZeroTwo Defense Implementation | Known Limitations & Gaps |
| :--- | :--- | :--- |
| **LLM01: Prompt Injection** | Delimited wrapping (`<<<UNTRUSTED_EXTERNAL_CONTENT>>>`), neutralization of closing tags, out-of-band session taint tracking. | In-band LLM prompt processing cannot be mathematically guaranteed immune to advanced jailbreaks. |
| **LLM02: Sensitive Info Disclosure** | Sensitive path denylist (`.ssh`, `/proc/*/environ`, API keys), LAN-aware memory redaction. | Tools reading user-specified non-denylisted files will transmit contents to the active provider. |
| **LLM06: Excessive Agency** | Centralized pre-dispatch authorization (`pkg/auth`), default-deny on outbound network and persistent memory mutation, two-tier shell safety gate. | Auto-approval (`--yes`) auto-approves safe mutating local commands (e.g. `mkdir`, `touch`) if untainted. |
| **LLM07: System Prompt Leakage** | `/memory-preview` and `-dry-run` transparently display what prompt is assembled; no secret tokens in system prompts. | Any model can potentially recite portions of its instructions if coaxed by user prompt. |
| **LLM08: Vector & SSRF Exploits** | Strict SSRF validator (`pkg/tools/ssrf.go`) blocking loopback, RFC1918, link-local, carrier-grade NAT, and cloud metadata. | Does not proxy through Tor; external DNS server responses are trusted for public IP resolution. |

### NIST AI Risk Management Framework (AI RMF 1.0)

| Core Function | Category | Implementation in ZeroTwo | Gaps & Unsupported Scope |
| :--- | :--- | :--- | :--- |
| **GOVERN** | Policies & Processes | Centralized authorization policy (`pkg/auth/policy.go`); fail-closed `/dev/tty` gate. | No enterprise multi-tenant role-based access control (RBAC). |
| **MAP** | Context & Risks | Explicit platform support matrix; separation of Linux CLI, Android CLI, and Android Native. | Hardware variations across non-standard Android OEM kernels cannot be exhaustively mapped. |
| **MEASURE** | Evaluation & Testing | Fuzz testing (`FuzzSSEStreamParser`), QEMU multi-architecture test suites, static analysis (`staticcheck`, `gosec`). | No formal automated benchmark measuring LLM hallucination rates. |
| **MANAGE** | Risk Treatment | Defense-in-depth: path confinement, symlink evaluation, environment variable scrubbing (`config.SanitizeChildEnv`). | User can explicitly approve hazardous commands via interactive prompt. |

---

## License

MIT License. Copyright (c) 2026.
