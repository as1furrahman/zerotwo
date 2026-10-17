package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"zerotwo/pkg/config"
)

// FindShellPath locates the appropriate shell binary across Linux, Android Termux, and Android system.
func FindShellPath() string {
	if p, err := exec.LookPath("sh"); err == nil {
		return p
	}
	// Termux standard path fallback
	const termuxSh = "/data/data/com.termux/files/usr/bin/sh"
	if _, err := os.Stat(termuxSh); err == nil {
		return termuxSh
	}
	// Android /system/bin/sh fallback
	if _, err := os.Stat("/system/bin/sh"); err == nil {
		return "/system/bin/sh"
	}
	return "/bin/sh"
}

type ShellTool struct {
	defaultTimeout time.Duration
	maxOutput      int
}

func NewShellTool(maxOutput int) *ShellTool {
	return &ShellTool{
		defaultTimeout: 60 * time.Second,
		maxOutput:      maxOutput,
	}
}

func (t *ShellTool) Name() string { return "shell" }
func (t *ShellTool) Description() string {
	return "Execute a shell command and return stdout+stderr. Runs in its own process group with a 60s default timeout."
}
func (t *ShellTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The shell command to execute"},
			"timeout_seconds": {"type": "integer", "description": "Optional timeout in seconds (default 60, max 300)"}
		},
		"required": ["command"]
	}`)
}
func (t *ShellTool) Run(argsJSON string) (string, error) {
	return t.RunWithContext(context.Background(), argsJSON)
}

func (t *ShellTool) RunWithContext(parentCtx context.Context, argsJSON string) (string, error) {
	var args struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

	timeout := t.defaultTimeout
	if args.TimeoutSeconds > 0 {
		secs := args.TimeoutSeconds
		if secs > 300 {
			secs = 300
		}
		timeout = time.Duration(secs) * time.Second
	}

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	shPath := FindShellPath()
	cmd := exec.CommandContext(ctx, shPath, "-c", args.Command) // #nosec G204 -- core agent shell execution tool gated by security confirmations
	cmd.Env = config.SanitizeChildEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err := cmd.Start()
	if err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var waitErr error
	select {
	case <-ctx.Done():
		// Kill the entire process group!
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		// Await cmd.Wait() completion to ensure stdout/stderr buffers are completely flushed
		<-done
		if parentCtx.Err() != nil {
			waitErr = fmt.Errorf("command interrupted by user cancellation: %w", parentCtx.Err())
		} else {
			waitErr = fmt.Errorf("command timed out after %v", timeout)
		}
	case waitErr = <-done:
	}

	var sb strings.Builder
	if stdout.Len() > 0 {
		sb.WriteString("STDOUT:\n")
		sb.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("STDERR:\n")
		sb.WriteString(stderr.String())
	}
	if waitErr != nil {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("[EXIT: %v]", waitErr))
	}
	if sb.Len() == 0 {
		return "[command completed with no output]", nil
	}

	return TruncateOutput(sb.String(), t.maxOutput), nil
}
