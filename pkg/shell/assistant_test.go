package shell

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

type mockAuthorizer struct {
	decision Decision
	details  ApprovalDetails
	err      error
}

func (m *mockAuthorizer) AuthorizeCommand(cmd string) (Decision, ApprovalDetails, error) {
	if m.err != nil {
		return m.decision, m.details, m.err
	}
	return m.decision, m.details, nil
}

func TestAssistantInteractiveLoop_PolicyDenial(t *testing.T) {
	mockAuth := &mockAuthorizer{
		decision: DecisionDeny,
		details: ApprovalDetails{
			Category: "destructive",
			Warning:  "tampering with MEMORY.md is forbidden",
		},
	}

	asst := NewAssistant(nil, mockAuth, true) // autoYes = true
	var executed bool
	asst.SetExecutor(func(cmd string) (int, error) {
		executed = true
		return 0, nil
	})

	var out bytes.Buffer
	// Even if user could type "yes", loop must deny before reading input
	in := strings.NewReader("yes\n")
	asst.SetConfirmIO(in, &out)

	exitCode, err := asst.InteractiveLoop(context.Background(), "echo bad > MEMORY.md", "bad query")
	if err == nil {
		t.Fatal("expected error for policy denial, got nil")
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
	if executed {
		t.Error("SECURITY DEFECT: executor was called on policy denial!")
	}
	if !strings.Contains(out.String(), "SECURITY POLICY DENIAL") {
		t.Errorf("expected security denial output, got: %s", out.String())
	}
}

func TestAssistantInteractiveLoop_AutoYesReadOnly(t *testing.T) {
	mockAuth := &mockAuthorizer{
		decision: DecisionAllow,
		details:  ApprovalDetails{Category: "read_only_local"},
	}

	asst := NewAssistant(nil, mockAuth, true) // autoYes = true
	var executedCmd string
	asst.SetExecutor(func(cmd string) (int, error) {
		executedCmd = cmd
		return 0, nil
	})

	var out bytes.Buffer
	in := strings.NewReader("")
	asst.SetConfirmIO(in, &out)

	exitCode, err := asst.InteractiveLoop(context.Background(), "df -h", "show disk space")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if executedCmd != "df -h" {
		t.Errorf("expected 'df -h' executed, got %q", executedCmd)
	}
	if !strings.Contains(out.String(), "Auto-approving read-only command via --yes") {
		t.Errorf("expected auto-approve notice, got: %s", out.String())
	}
}

func TestAssistantInteractiveLoop_NetworkRequiresApproval_AutoYesDoesNotBypass(t *testing.T) {
	mockAuth := &mockAuthorizer{
		decision: DecisionRequireApproval,
		details: ApprovalDetails{
			Category: "egress_remote",
			Warning:  "remote network egress",
		},
	}

	// Case 1: autoYes = true, but user declines ("n")
	{
		asst := NewAssistant(nil, mockAuth, true) // autoYes = true
		var executed bool
		asst.SetExecutor(func(cmd string) (int, error) {
			executed = true
			return 0, nil
		})

		var out bytes.Buffer
		in := strings.NewReader("n\n")
		asst.SetConfirmIO(in, &out)

		exitCode, err := asst.InteractiveLoop(context.Background(), "curl https://example.com", "fetch")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exitCode != 1 {
			t.Errorf("expected exit code 1 for declined command, got %d", exitCode)
		}
		if executed {
			t.Error("SECURITY DEFECT: executor called when user entered 'n' under autoYes!")
		}
	}

	// Case 2: autoYes = true, user approves ("y")
	{
		asst := NewAssistant(nil, mockAuth, true) // autoYes = true
		var executed bool
		asst.SetExecutor(func(cmd string) (int, error) {
			executed = true
			return 0, nil
		})

		var out bytes.Buffer
		in := strings.NewReader("y\n")
		asst.SetConfirmIO(in, &out)

		exitCode, err := asst.InteractiveLoop(context.Background(), "curl https://example.com", "fetch")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}
		if !executed {
			t.Error("expected command to execute after explicit 'y' confirmation")
		}
	}
}

func TestAssistantInteractiveLoop_DestructiveRequiresStrongApproval(t *testing.T) {
	mockAuth := &mockAuthorizer{
		decision: DecisionRequireStrongApproval,
		details: ApprovalDetails{
			Category:       "destructive",
			RequiresStrong: true,
			Warning:        "destructive file deletion",
		},
	}

	// Case 1: autoYes = true, user types single 'y' -> MUST FAIL (requires exact "yes")
	{
		asst := NewAssistant(nil, mockAuth, true) // autoYes = true
		var executed bool
		asst.SetExecutor(func(cmd string) (int, error) {
			executed = true
			return 0, nil
		})

		var out bytes.Buffer
		in := strings.NewReader("y\n")
		asst.SetConfirmIO(in, &out)

		exitCode, err := asst.InteractiveLoop(context.Background(), "rm -rf /tmp/testdir", "remove dir")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}
		if executed {
			t.Error("SECURITY DEFECT: destructive command executed with single 'y'!")
		}
	}

	// Case 2: autoYes = true, user types exact "yes" -> SUCCEEDS
	{
		asst := NewAssistant(nil, mockAuth, true)
		var executed bool
		asst.SetExecutor(func(cmd string) (int, error) {
			executed = true
			return 0, nil
		})

		var out bytes.Buffer
		in := strings.NewReader("yes\n")
		asst.SetConfirmIO(in, &out)

		exitCode, err := asst.InteractiveLoop(context.Background(), "rm -rf /tmp/testdir", "remove dir")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}
		if !executed {
			t.Error("expected destructive command to execute after typing exact 'yes'")
		}
	}
}

func TestAssistantInteractiveLoop_FailClosedOnEOF(t *testing.T) {
	mockAuth := &mockAuthorizer{
		decision: DecisionRequireApproval,
		details:  ApprovalDetails{Category: "execute_command"},
	}

	asst := NewAssistant(nil, mockAuth, false)
	var executed bool
	asst.SetExecutor(func(cmd string) (int, error) {
		executed = true
		return 0, nil
	})

	var out bytes.Buffer
	// Empty reader simulates non-interactive EOF / closed terminal
	in := strings.NewReader("")
	asst.SetConfirmIO(in, &out)

	exitCode, err := asst.InteractiveLoop(context.Background(), "mkdir /tmp/newdir", "make dir")
	if err == nil {
		t.Fatal("expected error on EOF input, got nil")
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1 on EOF, got %d", exitCode)
	}
	if executed {
		t.Error("SECURITY DEFECT: command executed despite EOF / closed confirmation input!")
	}
}

func TestAssistantInteractiveLoop_Quit(t *testing.T) {
	mockAuth := &mockAuthorizer{
		decision: DecisionRequireApproval,
		details:  ApprovalDetails{Category: "execute_command"},
	}

	asst := NewAssistant(nil, mockAuth, false)
	var executed bool
	asst.SetExecutor(func(cmd string) (int, error) {
		executed = true
		return 0, nil
	})

	var out bytes.Buffer
	in := strings.NewReader("q\n")
	asst.SetConfirmIO(in, &out)

	exitCode, err := asst.InteractiveLoop(context.Background(), "ls -la", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0 on quit, got %d", exitCode)
	}
	if executed {
		t.Error("command should not execute on 'q'")
	}
}

func TestDefaultAuthorizer_BlocksProtectedTargets(t *testing.T) {
	auth := &defaultAuthorizer{
		classifier: NewTokenizingClassifier(),
		autoYes:    true,
	}

	blocked := []string{
		"echo test > MEMORY.md",
		"cat >> MEMORY.md",
		"rm MEMORY.md",
		"touch MEMORY.md",
		"echo foo > sessions/active.json",
		"rm -f sessions/001.json",
	}

	for _, cmd := range blocked {
		dec, details, _ := auth.AuthorizeCommand(cmd)
		if dec != DecisionDeny {
			t.Errorf("SECURITY DEFECT: defaultAuthorizer allowed %q (decision=%v)", cmd, dec)
		}
		if !strings.Contains(details.Warning, "MEMORY.md") && !strings.Contains(details.Warning, "sessions/") {
			t.Errorf("expected warning mentioning protected target, got: %s", details.Warning)
		}
	}
}

func TestDefaultAuthorizer_NetworkCapableCommands(t *testing.T) {
	auth := &defaultAuthorizer{
		classifier: NewTokenizingClassifier(),
		autoYes:    true,
	}

	netCmds := []string{
		"curl -s https://example.com",
		"wget http://test.org",
		"ssh user@remote",
		"ping 1.1.1.1",
		"nc -zv 127.0.0.1 80",
	}

	for _, cmd := range netCmds {
		dec, details, _ := auth.AuthorizeCommand(cmd)
		if dec != DecisionRequireApproval {
			t.Errorf("SECURITY DEFECT: network command %q had decision %v, expected RequireApproval", cmd, dec)
		}
		if details.Category != "egress_remote" {
			t.Errorf("expected egress_remote category, got %s", details.Category)
		}
	}
}
