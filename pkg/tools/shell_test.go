package tools

import (
	"strings"
	"testing"
	"time"
)

func TestProcessGroupKillOnTimeout(t *testing.T) {
	tool := NewShellTool(4096)

	start := time.Now()
	// Launch a long sleep child process inside a subshell
	out, err := tool.Run(`{"command":"sh -c 'sleep 30'","timeout_seconds":1}`)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error running tool: %v", err)
	}

	// Must terminate near 1s timeout, definitely not 30s
	if elapsed > 3*time.Second {
		t.Fatalf("process group was not killed promptly on timeout: took %v", elapsed)
	}

	if !strings.Contains(out, "command timed out after 1s") {
		t.Errorf("expected timeout notice in output, got: %s", out)
	}
}

func TestShellExecutionStandardOutput(t *testing.T) {
	tool := NewShellTool(4096)

	out, err := tool.Run(`{"command":"echo 'zerotwo shell test'"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "zerotwo shell test") {
		t.Errorf("expected stdout to contain test string, got: %s", out)
	}
}
