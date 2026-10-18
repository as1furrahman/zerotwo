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
				// Kindle local user partition (/mnt/us) is trusted local on-device storage (KOReader, documents)
				cleanPath := filepath.Clean(pathAbs)
				if strings.HasPrefix(cleanPath, "/mnt/us/") || cleanPath == "/mnt/us" {
					return TruncateOutput(string(data), t.maxOutput), nil
				}
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

	targetBase := strings.ToLower(filepath.Base(args.Path))
	targetClean := strings.ToLower(filepath.ToSlash(args.Path))
	if targetBase == "memory.md" {
		return "", fmt.Errorf("write denied: direct file writing to MEMORY.md is forbidden; use save_memory or patch_memory tools")
	}

	if (strings.Contains(targetClean, "/sessions/") || strings.HasPrefix(targetClean, "sessions/")) && strings.HasSuffix(targetClean, ".json") {
		return "", fmt.Errorf("write denied: direct file writing to session files (sessions/*.json) is forbidden")
	}

	if realPath, err := filepath.EvalSymlinks(args.Path); err == nil {
		realBase := strings.ToLower(filepath.Base(realPath))
		realClean := strings.ToLower(filepath.ToSlash(realPath))
		if realBase == "memory.md" {
			return "", fmt.Errorf("write denied: target resolves to MEMORY.md; use save_memory or patch_memory tools")
		}
		if (strings.Contains(realClean, "/sessions/") || strings.HasPrefix(realClean, "sessions/")) && strings.HasSuffix(realClean, ".json") {
			return "", fmt.Errorf("write denied: target resolves to session file (sessions/*.json)")
		}
	}

	dirToEval := filepath.Dir(args.Path)
	if realDir, err := filepath.EvalSymlinks(dirToEval); err == nil {
		canonicalTarget := filepath.Join(realDir, filepath.Base(args.Path))
		canonicalBase := strings.ToLower(filepath.Base(canonicalTarget))
		canonicalClean := strings.ToLower(filepath.ToSlash(canonicalTarget))
		if canonicalBase == "memory.md" {
			return "", fmt.Errorf("write denied: target resolves to MEMORY.md; use save_memory or patch_memory tools")
		}
		if (strings.Contains(canonicalClean, "/sessions/") || strings.HasPrefix(canonicalClean, "sessions/")) && strings.HasSuffix(canonicalClean, ".json") {
			return "", fmt.Errorf("write denied: target resolves to session file (sessions/*.json)")
		}
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

type PatchFileTool struct{}

func NewPatchFileTool() *PatchFileTool {
	return &PatchFileTool{}
}

func (t *PatchFileTool) Name() string { return "patch_file" }
func (t *PatchFileTool) Description() string {
	return "Patch an existing file by searching for a specific block of text and replacing it with new text. Fails if the search string is not found or is ambiguous (unless allow_multiple is true)."
}
func (t *PatchFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path to patch"},
			"search": {"type": "string", "description": "Exact text block to find in the file"},
			"replace": {"type": "string", "description": "Replacement text block"},
			"allow_multiple": {"type": "boolean", "description": "If true, replace all occurrences. If false (default), require exactly one match"}
		},
		"required": ["path", "search", "replace"]
	}`)
}
func (t *PatchFileTool) Run(argsJSON string) (string, error) {
	var args struct {
		Path          string `json:"path"`
		Search        string `json:"search"`
		Replace       string `json:"replace"`
		AllowMultiple bool   `json:"allow_multiple"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	if args.Search == "" {
		return "", fmt.Errorf("search string cannot be empty")
	}

	if IsSensitivePath(args.Path) {
		return "", fmt.Errorf("patch denied: %s matches sensitive path denylist (credentials/keys protected)", args.Path)
	}

	targetBase := strings.ToLower(filepath.Base(args.Path))
	targetClean := strings.ToLower(filepath.ToSlash(args.Path))
	if targetBase == "memory.md" {
		return "", fmt.Errorf("patch denied: direct file patching to MEMORY.md is forbidden; use save_memory or patch_memory tools")
	}

	if (strings.Contains(targetClean, "/sessions/") || strings.HasPrefix(targetClean, "sessions/")) && strings.HasSuffix(targetClean, ".json") {
		return "", fmt.Errorf("patch denied: direct file patching to session files (sessions/*.json) is forbidden")
	}

	if realPath, err := filepath.EvalSymlinks(args.Path); err == nil {
		realBase := strings.ToLower(filepath.Base(realPath))
		realClean := strings.ToLower(filepath.ToSlash(realPath))
		if realBase == "memory.md" {
			return "", fmt.Errorf("patch denied: target resolves to MEMORY.md; use save_memory or patch_memory tools")
		}
		if (strings.Contains(realClean, "/sessions/") || strings.HasPrefix(realClean, "sessions/")) && strings.HasSuffix(realClean, ".json") {
			return "", fmt.Errorf("patch denied: target resolves to session file (sessions/*.json)")
		}
	}

	dirToEval := filepath.Dir(args.Path)
	if realDir, err := filepath.EvalSymlinks(dirToEval); err == nil {
		canonicalTarget := filepath.Join(realDir, filepath.Base(args.Path))
		canonicalBase := strings.ToLower(filepath.Base(canonicalTarget))
		canonicalClean := strings.ToLower(filepath.ToSlash(canonicalTarget))
		if canonicalBase == "memory.md" {
			return "", fmt.Errorf("patch denied: target resolves to MEMORY.md; use save_memory or patch_memory tools")
		}
		if (strings.Contains(canonicalClean, "/sessions/") || strings.HasPrefix(canonicalClean, "sessions/")) && strings.HasSuffix(canonicalClean, ".json") {
			return "", fmt.Errorf("patch denied: target resolves to session file (sessions/*.json)")
		}
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("patch denied: file %s does not exist; use write_file to create new files", args.Path)
		}
		return "", fmt.Errorf("patch failed to stat %s: %w", args.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("patch denied: %s is a directory, not a file", args.Path)
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("patch failed to read %s: %w", args.Path, err)
	}

	content := string(data)
	count := strings.Count(content, args.Search)
	if count == 0 {
		return "", fmt.Errorf("search text not found in %s", args.Path)
	}
	if count > 1 && !args.AllowMultiple {
		return "", fmt.Errorf("search text matched %d times in %s; specify more context lines to match uniquely, or set allow_multiple=true", count, args.Path)
	}

	var newContent string
	if args.AllowMultiple {
		newContent = strings.ReplaceAll(content, args.Search, args.Replace)
	} else {
		newContent = strings.Replace(content, args.Search, args.Replace, 1)
	}

	// If replacement content contains untrusted markers, mark this file path as tainted
	if strings.Contains(args.Replace, "<<<UNTRUSTED_EXTERNAL_CONTENT") {
		MarkFileTainted(args.Path)
	}

	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0644
	}

	if err := config.AtomicWriteFile(args.Path, []byte(newContent), mode); err != nil {
		return "", fmt.Errorf("patching file %s: %w", args.Path, err)
	}
	return fmt.Sprintf("Successfully patched %s (%d occurrence(s) replaced, %d bytes)", args.Path, count, len(newContent)), nil
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
