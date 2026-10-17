package auth

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"zerotwo/pkg/shell"
	"zerotwo/pkg/tools"
)

// ActionCategory classifies the capability tier of a requested tool execution.
type ActionCategory string

const (
	CategoryReadOnlyLocal  ActionCategory = "read_only_local"
	CategoryWriteLocal     ActionCategory = "write_local"
	CategoryMutateMemory   ActionCategory = "mutate_memory"
	CategoryExecuteCommand ActionCategory = "execute_command"
	CategoryEgressRemote   ActionCategory = "egress_remote"
	CategoryDestructive    ActionCategory = "destructive"
)

// Decision defines the authorization verdict.
type Decision = shell.Decision

const (
	DecisionAllow                 = shell.DecisionAllow
	DecisionRequireApproval       = shell.DecisionRequireApproval
	DecisionRequireStrongApproval = shell.DecisionRequireStrongApproval
	DecisionDeny                  = shell.DecisionDeny
)

// AuthorizationRequest encapsulates all context needed to authorize a tool action.
type AuthorizationRequest struct {
	ToolName        string
	ArgsJSON        string
	ContextTainted  bool
	AutoYes         bool
	AllowShell      bool
	Workspace       string
	BlockedCommands []string
	ConfirmTools    []string
}

// ApprovalDetails specifies what to display to the user when approval is required.
type ApprovalDetails struct {
	Category       ActionCategory
	Action         string
	Target         string
	DataScope      string
	RequiresStrong bool
	Warning        string
}

// Policy is the centralized authorization engine enforcing tool boundaries.
type Policy struct{}

// NewPolicy creates a new centralized authorization policy.
func NewPolicy() *Policy {
	return &Policy{}
}

// Authorize evaluates an incoming tool call against the security policy immediately before dispatch.
func (p *Policy) Authorize(req AuthorizationRequest) (Decision, ApprovalDetails, error) {
	name := strings.TrimSpace(req.ToolName)
	args := strings.TrimSpace(req.ArgsJSON)
	if args == "" {
		args = "{}"
	}

	switch name {
	case "shell":
		return p.authorizeShell(req, args)
	case "web_fetch", "http_request", "web_search":
		return p.authorizeRemoteEgress(req, name, args)
	case "save_memory", "patch_memory":
		return p.authorizeMemoryMutation(req, name, args)
	case "write_file":
		return p.authorizeFileWrite(req, args)
	case "patch_file":
		return p.authorizeFilePatch(req, args)
	case "read_file":
		return p.authorizeFileRead(req, args)
	case "list_dir":
		return p.authorizeListDir(req, args)
	case "get_system_status":
		return p.authorizeSystemStatus(req)
	default:
		// Unknown tool: fail closed by default
		return DecisionDeny, ApprovalDetails{
			Category: ActionCategory("unknown"),
			Action:   "unknown_tool",
			Target:   name,
			Warning:  fmt.Sprintf("unrecognized tool %q not registered in authorization policy", name),
		}, fmt.Errorf("unknown tool %q denied by default policy", name)
	}
}

func (p *Policy) authorizeShell(req AuthorizationRequest, args string) (Decision, ApprovalDetails, error) {
	if !req.AllowShell {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryExecuteCommand,
			Action:   "shell",
			Warning:  "arbitrary shell command execution is disabled by configuration (tools.allow_shell=false)",
		}, fmt.Errorf("shell execution disabled by configuration")
	}

	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryExecuteCommand,
			Action:   "shell",
			Warning:  "malformed JSON arguments for shell tool",
		}, fmt.Errorf("malformed shell tool arguments: %w", err)
	}

	cmd := strings.TrimSpace(payload.Command)
	if cmd == "" {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryExecuteCommand,
			Action:   "shell",
			Warning:  "empty shell command",
		}, fmt.Errorf("empty shell command")
	}

	// Check configurable blocked commands
	for _, blocked := range req.BlockedCommands {
		b := strings.TrimSpace(blocked)
		if b == "" {
			continue
		}
		fields := strings.Fields(cmd)
		if len(fields) > 0 && (fields[0] == b || strings.HasSuffix(fields[0], "/"+b)) {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryExecuteCommand,
				Action:   "shell",
				Target:   cmd,
				Warning:  fmt.Sprintf("command %q is blocked by configuration (tools.blocked_commands)", b),
			}, fmt.Errorf("command %q is explicitly blocked by policy", b)
		}
	}

	// Check if command attempts to modify MEMORY.md or session files (sessions/*.json)
	if isModifying, targetReason := isModifyingProtectedTarget(cmd, req.Workspace); isModifying {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryMutateMemory,
			Action:   "shell",
			Target:   cmd,
			Warning:  fmt.Sprintf("modifying MEMORY.md or session files via shell is forbidden (%s); use save_memory or patch_memory tools instead", targetReason),
		}, fmt.Errorf("command denied: modifying MEMORY.md or session files via shell is forbidden")
	}

	// Check if command is network-capable (curl, wget, nc, scp, ssh, git push/clone, rsync, python/perl sockets)
	if isNet, netReason := isNetworkCapableCommand(cmd); isNet {
		return DecisionRequireApproval, ApprovalDetails{
			Category:       CategoryEgressRemote,
			Action:         "execute_network_command",
			Target:         cmd,
			DataScope:      "network egress / remote network communication",
			RequiresStrong: false,
			Warning:        fmt.Sprintf("Network-capable command detected (%s). Cannot be auto-approved by --yes.", netReason),
		}, nil
	}

	sev, reason := shell.ClassifyCommand(cmd)

	if sev == shell.SeverityDestructive {
		return DecisionRequireStrongApproval, ApprovalDetails{
			Category:       CategoryDestructive,
			Action:         "execute_destructive_command",
			Target:         cmd,
			DataScope:      "filesystem / system state modification",
			RequiresStrong: true,
			Warning:        fmt.Sprintf("Destructive command detected (%s). Full 'yes' confirmation required. Cannot be approved by --yes.", reason),
		}, nil
	}

	if sev == shell.SeverityGeneric {
		return DecisionRequireApproval, ApprovalDetails{
			Category:       CategoryExecuteCommand,
			Action:         "execute_command",
			Target:         cmd,
			DataScope:      "local process execution",
			RequiresStrong: false,
			Warning:        fmt.Sprintf("Mutating command or sensitive path detected (%s). Cannot be auto-approved by --yes.", reason),
		}, nil
	}

	// SeverityReadOnly:
	if req.ContextTainted {
		return DecisionRequireApproval, ApprovalDetails{
			Category:       CategoryExecuteCommand,
			Action:         "execute_read_only_command",
			Target:         cmd,
			DataScope:      "local system inspection",
			RequiresStrong: false,
			Warning:        "Context is tainted by untrusted external data. Explicit approval required.",
		}, nil
	}

	if req.AutoYes {
		return DecisionAllow, ApprovalDetails{
			Category:  CategoryReadOnlyLocal,
			Action:    "execute_read_only_command",
			Target:    cmd,
			DataScope: "local system inspection",
		}, nil
	}

	// Check if shell is in confirm_tools
	for _, ct := range req.ConfirmTools {
		if ct == "shell" {
			return DecisionRequireApproval, ApprovalDetails{
				Category:  CategoryReadOnlyLocal,
				Action:    "execute_read_only_command",
				Target:    cmd,
				DataScope: "local system inspection",
			}, nil
		}
	}

	return DecisionAllow, ApprovalDetails{
		Category:  CategoryReadOnlyLocal,
		Action:    "execute_read_only_command",
		Target:    cmd,
		DataScope: "local system inspection",
	}, nil
}

func (p *Policy) authorizeRemoteEgress(req AuthorizationRequest, toolName, args string) (Decision, ApprovalDetails, error) {
	// Remote egress tools always require explicit user approval in the default policy.
	// AutoYes is incapable of bypassing remote outbound requests!
	warning := "Outbound network transmission: arbitrary external request."
	if req.ContextTainted {
		warning = "SECURITY WARNING: Request triggered while context is TAINTED by untrusted remote data! Possible data exfiltration attempt."
	}

	switch toolName {
	case "web_fetch":
		var payload struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal([]byte(args), &payload)
		return DecisionRequireApproval, ApprovalDetails{
			Category:       CategoryEgressRemote,
			Action:         "web_fetch",
			Target:         payload.URL,
			DataScope:      "HTTP GET request to external server; response ingested into conversation",
			RequiresStrong: false,
			Warning:        warning,
		}, nil

	case "http_request":
		var payload struct {
			Method string `json:"method"`
			URL    string `json:"url"`
			Body   string `json:"body"`
		}
		_ = json.Unmarshal([]byte(args), &payload)
		method := strings.ToUpper(payload.Method)
		if method == "" {
			method = "GET"
		}
		dataScope := fmt.Sprintf("HTTP %s to external server (payload size: %d bytes)", method, len(payload.Body))
		return DecisionRequireApproval, ApprovalDetails{
			Category:       CategoryEgressRemote,
			Action:         "http_request",
			Target:         fmt.Sprintf("%s %s", method, payload.URL),
			DataScope:      dataScope,
			RequiresStrong: false,
			Warning:        warning,
		}, nil

	case "web_search":
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal([]byte(args), &payload)
		return DecisionRequireApproval, ApprovalDetails{
			Category:       CategoryEgressRemote,
			Action:         "web_search",
			Target:         payload.Query,
			DataScope:      "search query terms transmitted to Brave Search API",
			RequiresStrong: false,
			Warning:        warning,
		}, nil
	}

	return DecisionRequireApproval, ApprovalDetails{
		Category: CategoryEgressRemote,
		Action:   toolName,
		Warning:  warning,
	}, nil
}

func (p *Policy) authorizeMemoryMutation(req AuthorizationRequest, toolName, args string) (Decision, ApprovalDetails, error) {
	warning := "Modifies long-term persistent memory (MEMORY.md). Changes affect all future sessions."
	if req.ContextTainted {
		warning = "SECURITY WARNING: Persistent memory mutation requested in a TAINTED turn! Possible prompt-injection persistence attempt."
	}

	var target string
	if toolName == "patch_memory" {
		var payload struct {
			Section string `json:"section"`
		}
		_ = json.Unmarshal([]byte(args), &payload)
		target = fmt.Sprintf("section %q in MEMORY.md", payload.Section)
	} else {
		target = "full overwrite of MEMORY.md"
	}

	// Memory mutation always requires user approval; --yes does not silently alter persistent memory.
	return DecisionRequireApproval, ApprovalDetails{
		Category:       CategoryMutateMemory,
		Action:         toolName,
		Target:         target,
		DataScope:      "persistent agent memory",
		RequiresStrong: false,
		Warning:        warning,
	}, nil
}

func (p *Policy) authorizeFileWrite(req AuthorizationRequest, args string) (Decision, ApprovalDetails, error) {
	var payload struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryWriteLocal,
			Action:   "write_file",
			Warning:  "malformed JSON arguments for write_file",
		}, fmt.Errorf("malformed write_file arguments: %w", err)
	}

	targetPath := strings.TrimSpace(payload.Path)
	if targetPath == "" {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryWriteLocal,
			Action:   "write_file",
			Warning:  "empty file path",
		}, fmt.Errorf("empty file path")
	}

	if tools.IsSensitivePath(targetPath) {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryWriteLocal,
			Action:   "write_file",
			Target:   targetPath,
			Warning:  "target matches sensitive path / credentials denylist",
		}, fmt.Errorf("write access denied: %s matches sensitive credentials/keys denylist", targetPath)
	}

	targetBase := strings.ToLower(filepath.Base(targetPath))
	targetClean := strings.ToLower(filepath.ToSlash(targetPath))
	if targetBase == "memory.md" {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryMutateMemory,
			Action:   "write_file",
			Target:   targetPath,
			Warning:  "direct file writes to MEMORY.md are forbidden; use save_memory or patch_memory tools instead",
		}, fmt.Errorf("write access denied: direct write to MEMORY.md is forbidden; use save_memory or patch_memory")
	}

	if (strings.Contains(targetClean, "/sessions/") || strings.HasPrefix(targetClean, "sessions/")) && strings.HasSuffix(targetClean, ".json") {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryWriteLocal,
			Action:   "write_file",
			Target:   targetPath,
			Warning:  "direct file writes to session files (sessions/*.json) are forbidden; managed by session manager",
		}, fmt.Errorf("write access denied: direct write to session files is forbidden")
	}

	if realPath, err := filepath.EvalSymlinks(targetPath); err == nil {
		realBase := strings.ToLower(filepath.Base(realPath))
		realClean := strings.ToLower(filepath.ToSlash(realPath))
		if realBase == "memory.md" {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryMutateMemory,
				Action:   "write_file",
				Target:   targetPath,
				Warning:  "symlink target resolves to MEMORY.md; use save_memory or patch_memory tools instead",
			}, fmt.Errorf("write access denied: symlink resolves to MEMORY.md")
		}
		if (strings.Contains(realClean, "/sessions/") || strings.HasPrefix(realClean, "sessions/")) && strings.HasSuffix(realClean, ".json") {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryWriteLocal,
				Action:   "write_file",
				Target:   targetPath,
				Warning:  "symlink target resolves to session file (sessions/*.json)",
			}, fmt.Errorf("write access denied: symlink resolves to session file")
		}
	}

	dirToEval := filepath.Dir(targetPath)
	if realDir, err := filepath.EvalSymlinks(dirToEval); err == nil {
		canonicalTarget := filepath.Join(realDir, filepath.Base(targetPath))
		canonicalBase := strings.ToLower(filepath.Base(canonicalTarget))
		canonicalClean := strings.ToLower(filepath.ToSlash(canonicalTarget))
		if canonicalBase == "memory.md" {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryMutateMemory,
				Action:   "write_file",
				Target:   targetPath,
				Warning:  "symlink target resolves to MEMORY.md; use save_memory or patch_memory tools instead",
			}, fmt.Errorf("write access denied: symlink resolves to MEMORY.md")
		}
		if (strings.Contains(canonicalClean, "/sessions/") || strings.HasPrefix(canonicalClean, "sessions/")) && strings.HasSuffix(canonicalClean, ".json") {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryWriteLocal,
				Action:   "write_file",
				Target:   targetPath,
				Warning:  "symlink target resolves to session file (sessions/*.json)",
			}, fmt.Errorf("write access denied: symlink resolves to session file")
		}
	}

	// Check workspace containment if configured
	// Check workspace containment if configured
	if req.Workspace != "" {
		wsAbs, errWs := filepath.Abs(req.Workspace)
		var pathAbs string
		if filepath.IsAbs(targetPath) {
			pathAbs = filepath.Clean(targetPath)
		} else if errWs == nil {
			pathAbs = filepath.Join(wsAbs, targetPath)
		} else {
			pathAbs = filepath.Clean(targetPath)
		}
		dirToEval := filepath.Dir(pathAbs)
		if realDir, err := filepath.EvalSymlinks(dirToEval); err == nil {
			pathAbs = filepath.Join(realDir, filepath.Base(pathAbs))
		}
		if realPath, err := filepath.EvalSymlinks(pathAbs); err == nil {
			pathAbs = realPath
		}
		if errWs == nil {
			rel, errRel := filepath.Rel(wsAbs, pathAbs)
			if errRel != nil || strings.HasPrefix(rel, "..") {
				// Writing outside workspace requires explicit user approval; AutoYes cannot bypass
				return DecisionRequireApproval, ApprovalDetails{
					Category:       CategoryWriteLocal,
					Action:         "write_file_outside_workspace",
					Target:         targetPath,
					DataScope:      fmt.Sprintf("write %d bytes outside workspace root (%s)", len(payload.Content), req.Workspace),
					RequiresStrong: false,
					Warning:        "Writing file outside workspace root directory. Requires explicit user authorization.",
				}, nil
			}
		}
	}

	warning := "File write to local filesystem."
	if req.ContextTainted {
		warning = "SECURITY WARNING: File write requested while context is TAINTED by untrusted external data! File will be marked tainted."
	}

	// File writes require confirmation by default.
	// If autoYes is true AND context is clean AND tool not in confirm_tools, could allow,
	// but default confirm_tools includes write_file.
	inConfirm := false
	for _, ct := range req.ConfirmTools {
		if ct == "write_file" || ct == "patch_file" {
			inConfirm = true
			break
		}
	}

	if req.ContextTainted || inConfirm {
		if req.AutoYes && !req.ContextTainted {
			return DecisionAllow, ApprovalDetails{
				Category:  CategoryWriteLocal,
				Action:    "write_file",
				Target:    targetPath,
				DataScope: fmt.Sprintf("write %d bytes to %s", len(payload.Content), targetPath),
			}, nil
		}
		return DecisionRequireApproval, ApprovalDetails{
			Category:       CategoryWriteLocal,
			Action:         "write_file",
			Target:         targetPath,
			DataScope:      fmt.Sprintf("write %d bytes to %s", len(payload.Content), targetPath),
			RequiresStrong: false,
			Warning:        warning,
		}, nil
	}

	return DecisionAllow, ApprovalDetails{
		Category:  CategoryWriteLocal,
		Action:    "write_file",
		Target:    targetPath,
		DataScope: fmt.Sprintf("write %d bytes to %s", len(payload.Content), targetPath),
	}, nil
}

func (p *Policy) authorizeFilePatch(req AuthorizationRequest, args string) (Decision, ApprovalDetails, error) {
	var payload struct {
		Path          string `json:"path"`
		Search        string `json:"search"`
		Replace       string `json:"replace"`
		AllowMultiple bool   `json:"allow_multiple"`
	}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryWriteLocal,
			Action:   "patch_file",
			Warning:  "malformed JSON arguments for patch_file",
		}, fmt.Errorf("malformed patch_file arguments: %w", err)
	}

	targetPath := strings.TrimSpace(payload.Path)
	if targetPath == "" {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryWriteLocal,
			Action:   "patch_file",
			Warning:  "empty file path",
		}, fmt.Errorf("empty file path")
	}

	if tools.IsSensitivePath(targetPath) {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryWriteLocal,
			Action:   "patch_file",
			Target:   targetPath,
			Warning:  "target matches sensitive path / credentials denylist",
		}, fmt.Errorf("patch access denied: %s matches sensitive credentials/keys denylist", targetPath)
	}

	targetBase := strings.ToLower(filepath.Base(targetPath))
	targetClean := strings.ToLower(filepath.ToSlash(targetPath))
	if targetBase == "memory.md" {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryMutateMemory,
			Action:   "patch_file",
			Target:   targetPath,
			Warning:  "direct file writes/patches to MEMORY.md are forbidden; use save_memory or patch_memory tools instead",
		}, fmt.Errorf("patch access denied: direct patch to MEMORY.md is forbidden; use patch_memory")
	}

	if (strings.Contains(targetClean, "/sessions/") || strings.HasPrefix(targetClean, "sessions/")) && strings.HasSuffix(targetClean, ".json") {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryWriteLocal,
			Action:   "patch_file",
			Target:   targetPath,
			Warning:  "direct file writes/patches to session files (sessions/*.json) are forbidden; managed by session manager",
		}, fmt.Errorf("patch access denied: direct patch to session files is forbidden")
	}

	if realPath, err := filepath.EvalSymlinks(targetPath); err == nil {
		realBase := strings.ToLower(filepath.Base(realPath))
		realClean := strings.ToLower(filepath.ToSlash(realPath))
		if realBase == "memory.md" {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryMutateMemory,
				Action:   "patch_file",
				Target:   targetPath,
				Warning:  "symlink target resolves to MEMORY.md; use save_memory or patch_memory tools instead",
			}, fmt.Errorf("patch access denied: symlink resolves to MEMORY.md")
		}
		if (strings.Contains(realClean, "/sessions/") || strings.HasPrefix(realClean, "sessions/")) && strings.HasSuffix(realClean, ".json") {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryWriteLocal,
				Action:   "patch_file",
				Target:   targetPath,
				Warning:  "symlink target resolves to session file (sessions/*.json)",
			}, fmt.Errorf("patch access denied: symlink resolves to session file")
		}
	}

	dirToEval := filepath.Dir(targetPath)
	if realDir, err := filepath.EvalSymlinks(dirToEval); err == nil {
		canonicalTarget := filepath.Join(realDir, filepath.Base(targetPath))
		canonicalBase := strings.ToLower(filepath.Base(canonicalTarget))
		canonicalClean := strings.ToLower(filepath.ToSlash(canonicalTarget))
		if canonicalBase == "memory.md" {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryMutateMemory,
				Action:   "patch_file",
				Target:   targetPath,
				Warning:  "symlink target resolves to MEMORY.md; use save_memory or patch_memory tools instead",
			}, fmt.Errorf("patch access denied: symlink resolves to MEMORY.md")
		}
		if (strings.Contains(canonicalClean, "/sessions/") || strings.HasPrefix(canonicalClean, "sessions/")) && strings.HasSuffix(canonicalClean, ".json") {
			return DecisionDeny, ApprovalDetails{
				Category: CategoryWriteLocal,
				Action:   "patch_file",
				Target:   targetPath,
				Warning:  "symlink target resolves to session file (sessions/*.json)",
			}, fmt.Errorf("patch access denied: symlink resolves to session file")
		}
	}

	// Check workspace containment if configured
	if req.Workspace != "" {
		wsAbs, errWs := filepath.Abs(req.Workspace)
		var pathAbs string
		if filepath.IsAbs(targetPath) {
			pathAbs = filepath.Clean(targetPath)
		} else if errWs == nil {
			pathAbs = filepath.Join(wsAbs, targetPath)
		} else {
			pathAbs = filepath.Clean(targetPath)
		}
		dirToEval := filepath.Dir(pathAbs)
		if realDir, err := filepath.EvalSymlinks(dirToEval); err == nil {
			pathAbs = filepath.Join(realDir, filepath.Base(pathAbs))
		}
		if realPath, err := filepath.EvalSymlinks(pathAbs); err == nil {
			pathAbs = realPath
		}
		if errWs == nil {
			rel, errRel := filepath.Rel(wsAbs, pathAbs)
			if errRel != nil || strings.HasPrefix(rel, "..") {
				return DecisionRequireApproval, ApprovalDetails{
					Category:       CategoryWriteLocal,
					Action:         "patch_file_outside_workspace",
					Target:         targetPath,
					DataScope:      fmt.Sprintf("patch file outside workspace root (%s)", req.Workspace),
					RequiresStrong: false,
					Warning:        "Patching file outside workspace root directory. Requires explicit user authorization.",
				}, nil
			}
		}
	}

	warning := "File patch on local filesystem."
	if req.ContextTainted {
		warning = "SECURITY WARNING: File patch requested while context is TAINTED by untrusted external data! File will be marked tainted."
	}

	inConfirm := false
	for _, ct := range req.ConfirmTools {
		if ct == "patch_file" || ct == "write_file" {
			inConfirm = true
			break
		}
	}

	dataScope := fmt.Sprintf("patch %s (search %d bytes, replace %d bytes)", targetPath, len(payload.Search), len(payload.Replace))
	if req.ContextTainted || inConfirm {
		if req.AutoYes && !req.ContextTainted {
			return DecisionAllow, ApprovalDetails{
				Category:  CategoryWriteLocal,
				Action:    "patch_file",
				Target:    targetPath,
				DataScope: dataScope,
			}, nil
		}
		return DecisionRequireApproval, ApprovalDetails{
			Category:       CategoryWriteLocal,
			Action:         "patch_file",
			Target:         targetPath,
			DataScope:      dataScope,
			RequiresStrong: false,
			Warning:        warning,
		}, nil
	}

	return DecisionAllow, ApprovalDetails{
		Category:  CategoryWriteLocal,
		Action:    "patch_file",
		Target:    targetPath,
		DataScope: dataScope,
	}, nil
}

func (p *Policy) authorizeFileRead(req AuthorizationRequest, args string) (Decision, ApprovalDetails, error) {
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryReadOnlyLocal,
			Action:   "read_file",
			Warning:  "malformed JSON arguments for read_file",
		}, fmt.Errorf("malformed read_file arguments: %w", err)
	}

	targetPath := strings.TrimSpace(payload.Path)
	if targetPath == "" {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryReadOnlyLocal,
			Action:   "read_file",
			Warning:  "empty file path",
		}, fmt.Errorf("empty file path")
	}

	// Check sensitive path denylist & symlinks
	if tools.IsSensitivePath(targetPath) {
		return DecisionDeny, ApprovalDetails{
			Category: CategoryReadOnlyLocal,
			Action:   "read_file",
			Target:   targetPath,
			Warning:  "path matches sensitive credentials/keys denylist",
		}, fmt.Errorf("access denied: %s matches sensitive path denylist", targetPath)
	}

	// Check workspace containment if configured
	if req.Workspace != "" {
		wsAbs, errWs := filepath.Abs(req.Workspace)
		var pathAbs string
		if filepath.IsAbs(targetPath) {
			pathAbs = filepath.Clean(targetPath)
		} else if errWs == nil {
			pathAbs = filepath.Join(wsAbs, targetPath)
		} else {
			pathAbs = filepath.Clean(targetPath)
		}
		if realPath, err := filepath.EvalSymlinks(pathAbs); err == nil {
			pathAbs = realPath
		}
		if errWs == nil {
			rel, errRel := filepath.Rel(wsAbs, pathAbs)
			if errRel != nil || strings.HasPrefix(rel, "..") {
				// Reading outside workspace
				if req.ContextTainted {
					return DecisionRequireApproval, ApprovalDetails{
						Category:       CategoryReadOnlyLocal,
						Action:         "read_file_outside_workspace",
						Target:         targetPath,
						DataScope:      "file outside workspace read during tainted turn",
						RequiresStrong: false,
						Warning:        "Reading external file outside workspace during a tainted turn.",
					}, nil
				}
			}
		}
	}

	// If read_file is in ConfirmTools
	for _, ct := range req.ConfirmTools {
		if ct == "read_file" {
			if req.AutoYes && !req.ContextTainted {
				return DecisionAllow, ApprovalDetails{Category: CategoryReadOnlyLocal, Action: "read_file", Target: targetPath}, nil
			}
			return DecisionRequireApproval, ApprovalDetails{
				Category:  CategoryReadOnlyLocal,
				Action:    "read_file",
				Target:    targetPath,
				DataScope: "local file read",
			}, nil
		}
	}

	return DecisionAllow, ApprovalDetails{
		Category:  CategoryReadOnlyLocal,
		Action:    "read_file",
		Target:    targetPath,
		DataScope: "local file read",
	}, nil
}

func (p *Policy) authorizeListDir(req AuthorizationRequest, args string) (Decision, ApprovalDetails, error) {
	for _, ct := range req.ConfirmTools {
		if ct == "list_dir" {
			if req.AutoYes && !req.ContextTainted {
				return DecisionAllow, ApprovalDetails{Category: CategoryReadOnlyLocal, Action: "list_dir"}, nil
			}
			return DecisionRequireApproval, ApprovalDetails{
				Category:  CategoryReadOnlyLocal,
				Action:    "list_dir",
				DataScope: "directory listing",
			}, nil
		}
	}
	return DecisionAllow, ApprovalDetails{Category: CategoryReadOnlyLocal, Action: "list_dir"}, nil
}

func (p *Policy) authorizeSystemStatus(req AuthorizationRequest) (Decision, ApprovalDetails, error) {
	for _, ct := range req.ConfirmTools {
		if ct == "get_system_status" {
			if req.AutoYes && !req.ContextTainted {
				return DecisionAllow, ApprovalDetails{Category: CategoryReadOnlyLocal, Action: "get_system_status"}, nil
			}
			return DecisionRequireApproval, ApprovalDetails{
				Category:  CategoryReadOnlyLocal,
				Action:    "get_system_status",
				DataScope: "system telemetry inspection",
			}, nil
		}
	}
	return DecisionAllow, ApprovalDetails{Category: CategoryReadOnlyLocal, Action: "get_system_status"}, nil
}

// AuthorizeShellCommand authorizes a single shell command string for standalone execution (e.g. -e assistant).
func (p *Policy) AuthorizeShellCommand(cmd string, autoYes bool, workspace string, blockedCommands []string) (Decision, ApprovalDetails, error) {
	req := AuthorizationRequest{
		ToolName:        "shell",
		ArgsJSON:        fmt.Sprintf("{\"command\":%q}", cmd),
		ContextTainted:  false,
		AutoYes:         autoYes,
		AllowShell:      true,
		Workspace:       workspace,
		BlockedCommands: blockedCommands,
	}
	return p.Authorize(req)
}

// ShellAuthorizer adapts Policy to shell.CommandAuthorizer for -e shell assistant mode.
type ShellAuthorizer struct {
	policy          *Policy
	workspace       string
	blockedCommands []string
	autoYes         bool
}

// NewShellAuthorizer creates a new authorizer bridging Policy to the shell assistant.
func NewShellAuthorizer(policy *Policy, workspace string, blockedCommands []string, autoYes bool) *ShellAuthorizer {
	return &ShellAuthorizer{
		policy:          policy,
		workspace:       workspace,
		blockedCommands: blockedCommands,
		autoYes:         autoYes,
	}
}

func (s *ShellAuthorizer) AuthorizeCommand(cmd string) (shell.Decision, shell.ApprovalDetails, error) {
	dec, details, err := s.policy.AuthorizeShellCommand(cmd, s.autoYes, s.workspace, s.blockedCommands)
	shellDetails := shell.ApprovalDetails{
		Category:       string(details.Category),
		Action:         details.Action,
		Target:         details.Target,
		DataScope:      details.DataScope,
		RequiresStrong: details.RequiresStrong,
		Warning:        details.Warning,
	}
	return dec, shellDetails, err
}

func isModifyingProtectedTarget(cmd string, workspace string) (bool, string) {
	return shell.IsModifyingProtectedTarget(cmd, workspace)
}

func isNetworkCapableCommand(cmd string) (bool, string) {
	return shell.IsNetworkCapableCommand(cmd)
}
