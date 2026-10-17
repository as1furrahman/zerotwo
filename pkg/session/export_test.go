package session

import (
	"os"
	"strings"
	"testing"
	"zerotwo/pkg/provider"
)

func TestExportDataDump(t *testing.T) {
	tmpDir := t.TempDir()

	sess := &Session{
		ID: "cyberdeck-test",
		Messages: []provider.Message{
			{Role: "user", Content: "How do we secure our dropbear port?"},
			{Role: "assistant", Content: "We enforce public key authentication only, Darling."},
		},
	}

	exportedPath, err := ExportDataDump(sess, tmpDir)
	if err != nil {
		t.Fatalf("ExportDataDump failed: %v", err)
	}

	if !strings.HasSuffix(exportedPath, ".html") {
		t.Errorf("expected html extension, got: %s", exportedPath)
	}

	data, err := os.ReadFile(exportedPath)
	if err != nil {
		t.Fatalf("could not read exported html: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "[DARLING]") {
		t.Errorf("missing [DARLING] tag: %s", content)
	}
	if !strings.Contains(content, "[▲▲ ZERO TWO · CODE:002]") {
		t.Errorf("missing [▲▲ ZERO TWO] tag: %s", content)
	}
	if !strings.Contains(content, "public key authentication") {
		t.Errorf("missing message content: %s", content)
	}
}

func TestExportDataDumpEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	sess := &Session{ID: "empty-test", Messages: nil}

	_, err := ExportDataDump(sess, tmpDir)
	if err == nil {
		t.Errorf("expected error on empty session, got nil")
	}
}
