package tools

import (
	"strings"
	"testing"
)

func TestSystemStatusGracefulExecution(t *testing.T) {
	tool := NewSystemStatusTool(4096)

	out, err := tool.Run("{}")
	if err != nil {
		t.Fatalf("GetSystemStatus failed: %v", err)
	}

	if len(out) == 0 {
		t.Fatalf("expected non-empty system status output")
	}

	// Must at least report CPU load or memory on standard Linux
	if !strings.Contains(out, "CPU Load") && !strings.Contains(out, "Memory") && !strings.Contains(out, "telemetry") {
		t.Errorf("expected standard telemetry or graceful fallback, got: %s", out)
	}
}
