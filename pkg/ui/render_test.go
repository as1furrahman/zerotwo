package ui

import (
	"testing"
)

func TestGetTerminalRows(t *testing.T) {
	rows := GetTerminalRows()
	// Should return at least 5 rows (either ioctl or fallback 25)
	if rows < 5 {
		t.Errorf("unexpected rows count: %d", rows)
	}
}

func TestRenderPagedSmallText(t *testing.T) {
	// Text shorter than terminal rows renders immediately without interactive paging
	text := "line 1\nline 2\nline 3"
	RenderPaged(text)
}

func TestTerminalDetection(t *testing.T) {
	_ = IsStdinTerminal()
	_ = IsStdoutTerminal()
}
