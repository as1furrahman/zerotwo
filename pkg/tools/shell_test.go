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

func TestBoundedBufferCapsMemory(t *testing.T) {
	limit := 1024
	bb := newBoundedBuffer(limit)

	// Write 500 KB of data into 1 KB buffer
	chunk := make([]byte, 10240)
	for i := range chunk {
		chunk[i] = 'A'
	}

	for i := 0; i < 50; i++ {
		n, err := bb.Write(chunk)
		if err != nil {
			t.Fatalf("unexpected error writing to boundedBuffer: %v", err)
		}
		if n != len(chunk) {
			t.Errorf("expected return n=%d, got %d", len(chunk), n)
		}
	}

	if bb.Len() != limit {
		t.Errorf("expected buffer length capped at %d, got %d", limit, bb.Len())
	}

	if len(bb.String()) != limit {
		t.Errorf("expected string length %d, got %d", limit, len(bb.String()))
	}
}

func TestShellToolDeniesProtectedTargets(t *testing.T) {
	tool := NewShellTool(4096)

	blockedCmds := []string{
		"echo malicious > MEMORY.md",
		"cat >> MEMORY.md",
		"rm MEMORY.md",
		"touch MEMORY.md",
		"echo '{}' > sessions/001.json",
		"rm sessions/active.json",
	}

	for _, cmd := range blockedCmds {
		args := `{"command":` + `"` + cmd + `"}`
		_, err := tool.Run(args)
		if err == nil {
			t.Errorf("CRITICAL SECURITY DEFECT: ShellTool executed protected target modification: %s", cmd)
		}
		if !strings.Contains(err.Error(), "forbidden") {
			t.Errorf("expected error mentioning forbidden, got: %v", err)
		}
	}
}
