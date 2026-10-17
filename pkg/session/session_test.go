package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"zerotwo/pkg/provider"
)

func TestSessionSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	sess := &Session{
		ID: "my-test-sess",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello session test"},
			{Role: "assistant", Content: "Session test reply"},
		},
		Tainted: true,
	}

	if err := mgr.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	p := mgr.path(sess.ID)
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// Verify strict file permissions 0600
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected permissions 0600, got %#o", perm)
	}

	loaded, err := mgr.Load("my-test-sess")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != "my-test-sess" {
		t.Errorf("expected ID 'my-test-sess', got %q", loaded.ID)
	}
	if !loaded.Tainted {
		t.Errorf("expected loaded.Tainted to be true")
	}
	if len(loaded.Messages) != 2 || loaded.Messages[0].Content != "Hello session test" {
		t.Errorf("unexpected loaded messages: %+v", loaded.Messages)
	}
}

func TestSessionSanitizeID(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	// Path traversal attempt should be confined to sessions directory
	path := mgr.path("../../../../etc/passwd")
	if !strings.HasPrefix(path, filepath.Join(tmpDir, "sessions")) {
		t.Errorf("path traversal not prevented: %s", path)
	}
	if !strings.HasSuffix(path, "passwd.json") {
		t.Errorf("expected filename to end in passwd.json, got: %s", path)
	}

	// Empty ID defaults to default.json
	emptyPath := mgr.path("")
	if !strings.HasSuffix(emptyPath, "default.json") {
		t.Errorf("expected empty ID to resolve to default.json, got: %s", emptyPath)
	}
}

func TestSessionCorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	p := mgr.path("corrupt")
	if err := os.WriteFile(p, []byte("NOT-VALID-JSON"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := mgr.Load("corrupt")
	if err == nil {
		t.Fatalf("expected error loading corrupted JSON session file")
	}
}
