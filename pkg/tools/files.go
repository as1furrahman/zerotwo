package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"zerotwo/pkg/config"
)

var (
	taintedFilesMu sync.RWMutex
	taintedFiles   = make(map[string]bool)
)

// MarkFileTainted records that a file was written from untrusted content or in a tainted turn.
func MarkFileTainted(path string) {
	taintedFilesMu.Lock()
	defer taintedFilesMu.Unlock()
	abs, err := filepath.Abs(path)
	if err == nil {
		taintedFiles[abs] = true
	}
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		taintedFiles[realPath] = true
	}
}

// IsFileTainted checks if a file was previously marked as containing untrusted external content.
func IsFileTainted(path string) bool {
	taintedFilesMu.RLock()
	defer taintedFilesMu.RUnlock()
	abs, err := filepath.Abs(path)
	if err == nil && taintedFiles[abs] {
		return true
	}
	if realPath, err := filepath.EvalSymlinks(path); err == nil && taintedFiles[realPath] {
		return true
	}
	return false
}

// IsSensitivePath checks if a path references secret keys, credentials, or sensitive system configurations.
// It evaluates both the literal path and the real symlink target.
func IsSensitivePath(path string) bool {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return false
	}

	// Check both the literal path and the evaluated symlink destination
	pathsToCheck := []string{raw}
	if realPath, err := filepath.EvalSymlinks(raw); err == nil && realPath != raw {
		pathsToCheck = append(pathsToCheck, realPath)
	}

	for _, p := range pathsToCheck {
		cleaned := strings.ToLower(filepath.Clean(p))

		// 1. /proc protection: block environ, mem, cmdline, fd, maps for all processes
		if strings.Contains(cleaned, "/proc") || strings.Contains(cleaned, "proc/") {
			if strings.Contains(cleaned, "environ") || strings.Contains(cleaned, "mem") ||
				strings.Contains(cleaned, "cmdline") || strings.Contains(cleaned, "maps") ||
				strings.Contains(cleaned, "smaps") || strings.Contains(cleaned, "status") ||
				strings.Contains(cleaned, "fd") {
				return true
			}
		}

		// 2. Sensitive directory and credential patterns
		sensitivePatterns := []string{
			".ssh",
			"id_rsa",
			"id_ed25519",
			"id_ecdsa",
			"id_dsa",
			"known_hosts",
			"authorized_keys",
			".gnupg",
			".config/zerotwo/config.json",
			"zerotwo/config.json",
			"/etc/shadow",
			"/etc/gshadow",
			"/etc/sudoers",
			".env",
			".aws",
			".azure",
			".kube",
			".docker/config.json",
			".dockercfg",
			".git-credentials",
			".netrc",
			".bash_history",
			".zsh_history",
		}

		for _, pat := range sensitivePatterns {
			if strings.Contains(cleaned, pat) {
				return true
			}
		}

		// 3. Sensitive file extensions
		sensitiveSuffixes := []string{".pem", ".key", ".pfx", ".pkcs12"}
		for _, suf := range sensitiveSuffixes {
			if strings.HasSuffix(cleaned, suf) {
				return true
			}
		}

		// 4. Wildcards / globs targeting sensitive paths
		if strings.Contains(cleaned, ".ss") || strings.Contains(cleaned, "id_") ||
			(strings.Contains(cleaned, "proc") && strings.Contains(cleaned, "env")) {
			if strings.Contains(cleaned, "*") || strings.Contains(cleaned, "?") || strings.Contains(cleaned, "[") {
				return true
			}
		}
	}

	return false
}

type ReadFileTool struct {
	maxOutput int
	workspace string
}

func NewReadFileTool(maxOutput int) *ReadFileTool {
	return &ReadFileTool{maxOutput: maxOutput}
}

func (t *ReadFileTool) SetWorkspace(ws string) {
	t.workspace = ws
}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Read the contents of a file at the specified path."
}
func (t *ReadFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute or relative file path to read"}
		},
		"required": ["path"]
	}`)
}
func (t *ReadFileTool) Run(argsJSON string) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	if IsSensitivePath(args.Path) {
		return "", fmt.Errorf("access denied: %s matches sensitive path denylist (credentials/keys protected)", args.Path)
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", err
	}

	// If file was previously marked as tainted (written from untrusted content or tainted turn),
	// wrap it as untrusted external content to prevent text laundering!
	if IsFileTainted(args.Path) {
		return NeutralizeAndWrapUntrusted("read_file("+args.Path+")", string(data), t.maxOutput), nil
	}

	// If workspace is configured and file is outside workspace, wrap as untrusted external content
	// to prevent indirect prompt injections from uncontainable file reads.
	if t.workspace != "" {
		wsAbs, errWs := filepath.Abs(t.workspace)
		pathAbs, errPath := filepath.Abs(args.Path)
		if errWs == nil && errPath == nil {
			rel, errRel := filepath.Rel(wsAbs, pathAbs)
			if errRel != nil || strings.HasPrefix(rel, "..") {
				return NeutralizeAndWrapUntrusted("read_file("+args.Path+")", string(data), t.maxOutput), nil
			}
		}
	}

	return TruncateOutput(string(data), t.maxOutput), nil
}

type WriteFileTool struct{}

func NewWriteFileTool() *WriteFileTool {
	return &WriteFileTool{}
}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Write content to a file at the specified path. Creates parent directories if missing."
}
func (t *WriteFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path to write"},
			"content": {"type": "string", "description": "Full file content to write"}
		},
		"required": ["path", "content"]
	}`)
}
func (t *WriteFileTool) Run(argsJSON string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	if IsSensitivePath(args.Path) {
		return "", fmt.Errorf("write denied: %s matches sensitive path denylist (credentials/keys protected)", args.Path)
	}

	if strings.EqualFold(filepath.Base(args.Path), "memory.md") {
		return "", fmt.Errorf("write denied: direct file writing to MEMORY.md is forbidden; use save_memory or patch_memory tools")
	}

	// If content being written contains untrusted external markers, mark this file path as tainted
	// to prevent laundering untrusted text through files on disk!
	if strings.Contains(args.Content, "<<<UNTRUSTED_EXTERNAL_CONTENT") {
		MarkFileTainted(args.Path)
	}

	err := config.AtomicWriteFile(args.Path, []byte(args.Content), 0644)
	if err != nil {
		return "", fmt.Errorf("writing file %s: %w", args.Path, err)
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(args.Content), args.Path), nil
}

type ListDirTool struct {
	maxOutput int
}

func NewListDirTool(maxOutput int) *ListDirTool {
	return &ListDirTool{maxOutput: maxOutput}
}

func (t *ListDirTool) Name() string { return "list_dir" }
func (t *ListDirTool) Description() string {
	return "List directory contents including sizes and file types."
}
func (t *ListDirTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Directory path to list (defaults to current dir)"}
		}
	}`)
}
func (t *ListDirTool) Run(argsJSON string) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	dir := args.Path
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			sb.WriteString(fmt.Sprintf("%s (unknown)\n", entry.Name()))
			continue
		}
		if entry.IsDir() {
			sb.WriteString(fmt.Sprintf("d %10s %s/\n", "-", entry.Name()))
		} else {
			sb.WriteString(fmt.Sprintf("- %10d %s\n", info.Size(), entry.Name()))
		}
	}
	res := sb.String()
	if res == "" {
		res = "(empty directory)"
	}
	return TruncateOutput(res, t.maxOutput), nil
}
