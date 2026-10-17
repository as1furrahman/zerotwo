package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactSections(t *testing.T) {
	doc := `# User Profile

## Identity
- Name: Asif

## Key Relationships
- Secret Contact: Alice

## Active Projects
- ZeroTwo: Ongoing

## Personal Context
- Sensitive Home Location: Classified
`

	redact := []string{"Key Relationships", "Personal Context"}
	filtered := RedactSections(doc, redact)

	if strings.Contains(filtered, "Secret Contact") {
		t.Errorf("expected Key Relationships to be redacted: %s", filtered)
	}
	if strings.Contains(filtered, "Sensitive Home Location") {
		t.Errorf("expected Personal Context to be redacted: %s", filtered)
	}
	if !strings.Contains(filtered, "Asif") || !strings.Contains(filtered, "ZeroTwo: Ongoing") {
		t.Errorf("unredacted sections missing: %s", filtered)
	}
}

func TestApplySectionPatch(t *testing.T) {
	doc := `## Identity
- Name: Test

## Projects
- Project 1
`

	// 1. Update existing section
	patched := ApplySectionPatch(doc, "Projects", "- Project 1\n- Project 2")
	if !strings.Contains(patched, "- Project 2") || !strings.Contains(patched, "## Identity") {
		t.Errorf("failed to patch existing section: %s", patched)
	}

	// 2. Append new section
	patched2 := ApplySectionPatch(patched, "Goals", "- Goal A")
	if !strings.Contains(patched2, "## Goals") || !strings.Contains(patched2, "- Goal A") {
		t.Errorf("failed to append new section: %s", patched2)
	}
}

func TestMemoryStoreAtomicSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)

	content := "# Test Memory\n\n## Section\nData"
	if err := store.Save(content); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	raw := store.LoadRaw()
	if raw != content {
		t.Errorf("expected %q, got %q", content, raw)
	}

	// Verify file path
	expectedPath := filepath.Join(dir, "MEMORY.md")
	if store.Path() != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, store.Path())
	}
}
