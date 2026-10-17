package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivacyResolution(t *testing.T) {
	cases := []struct {
		url      string
		expected bool
	}{
		{"http://localhost:11434/v1", true},
		{"http://127.0.0.1:11434/v1", true},
		{"http://192.168.1.100:11434/v1", true},
		{"http://10.0.0.5:8000/v1", true},
		{"http://mybox.local:11434/v1", true},
		{"https://api.groq.com/openai/v1", false},
		{"https://generativelanguage.googleapis.com/v1beta/openai", false},
		{"https://api.openai.com/v1", false},
		{"https://api.deepseek.com", false},
	}

	for _, tc := range cases {
		cfg := DefaultConfig()
		cfg.Provider.BaseURL = tc.url
		cfg.Resolve()
		if cfg.Provider.SendMemory == nil {
			t.Fatalf("SendMemory was nil for %s", tc.url)
		}
		if *cfg.Provider.SendMemory != tc.expected {
			t.Errorf("URL %s: expected SendMemory=%v, got %v", tc.url, tc.expected, *cfg.Provider.SendMemory)
		}
	}
}

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "test.txt")

	data := []byte("hello atomic world")
	if err := AtomicWriteFile(target, data, 0600); err != nil {
		t.Fatalf("AtomicWriteFile failed: %v", err)
	}

	read, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(read) != string(data) {
		t.Fatalf("expected %q, got %q", string(data), string(read))
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected perm 0600, got %04o", info.Mode().Perm())
	}
}

func TestCheckPermissionsWarnings(t *testing.T) {
	dir := t.TempDir()

	// 1. Directory with strict 0700 perm -> should return true (secure)
	strictDir := filepath.Join(dir, "strict")
	if err := os.Mkdir(strictDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !CheckDirPermissions(strictDir, 0700) {
		t.Errorf("expected 0700 directory to pass CheckDirPermissions as secure")
	}

	// 2. Directory with loose 0775 perm -> should return false (warning emitted)
	looseDir := filepath.Join(dir, "loose")
	if err := os.Mkdir(looseDir, 0775); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if CheckDirPermissions(looseDir, 0700) {
		t.Errorf("expected 0775 directory to trigger warning in CheckDirPermissions")
	}

	// 3. File with strict 0600 perm -> should return true (secure)
	strictFile := filepath.Join(strictDir, "config.json")
	if err := os.WriteFile(strictFile, []byte("{}"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if !CheckFilePermissions(strictFile, 0600) {
		t.Errorf("expected 0600 file to pass CheckFilePermissions as secure")
	}

	// 4. File with loose 0664 perm -> should return false (warning emitted)
	looseFile := filepath.Join(strictDir, "loose.json")
	if err := os.WriteFile(looseFile, []byte("{}"), 0664); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if CheckFilePermissions(looseFile, 0600) {
		t.Errorf("expected 0664 file to trigger warning in CheckFilePermissions")
	}
}

func TestSanitizeChildEnv(t *testing.T) {
	input := []string{
		"PATH=/usr/bin:/bin",
		"USER=asif",
		"HOME=/home/asif",
		"GROQ_API_KEY=gsk_secret123",
		"OPENAI_API_KEY=sk-test456",
		"ANTHROPIC_AUTH_TOKEN=ant-789",
		"AWS_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"DATABASE_PASSWORD=supersecret",
		"DATABASE_URL=postgres://user:pass@localhost:5432/db",
		"SSH_AUTH_SOCK=/tmp/ssh-agent.sock",
		"GITHUB_PAT=ghp_personal_access_token_123", // gitleaks:allow
		"BRAVE_APIKEY=brave999",                    // gitleaks:allow
		"MY_CREDENTIALS=creds",
		"SHELL=/bin/bash",
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
		"TZ=UTC",
	}

	clean := SanitizeChildEnv(input)

	// Verify all non-allowlisted / sensitive keys are purged
	forbiddenKeys := []string{
		"GROQ_API_KEY",
		"OPENAI_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"AWS_SECRET_KEY",
		"DATABASE_PASSWORD",
		"DATABASE_URL",
		"SSH_AUTH_SOCK",
		"GITHUB_PAT",
		"BRAVE_APIKEY",
		"MY_CREDENTIALS",
	}
	for _, env := range clean {
		for _, forbidden := range forbiddenKeys {
			if strings.HasPrefix(env, forbidden+"=") {
				t.Errorf("SanitizeChildEnv failed to strip sensitive/unlisted env var: %s", env)
			}
		}
	}

	// Verify safe allowlist keys are preserved
	safeKeys := map[string]bool{"PATH": false, "USER": false, "HOME": false, "SHELL": false, "TERM": false, "LANG": false, "TZ": false}
	for _, env := range clean {
		for k := range safeKeys {
			if strings.HasPrefix(env, k+"=") {
				safeKeys[k] = true
			}
		}
	}
	for k, found := range safeKeys {
		if !found {
			t.Errorf("SanitizeChildEnv unexpectedly stripped safe env var: %s", k)
		}
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	cases := []struct {
		input    string
		expected string
	}{
		{"~", home},
		{"~/", home},
		{"~/test", filepath.Join(home, "test")},
		{"~/.config/zerotwo", filepath.Join(home, ".config", "zerotwo")},
		{"/var/log", "/var/log"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tc := range cases {
		got := ExpandPath(tc.input)
		if got != tc.expected {
			t.Errorf("ExpandPath(%q): expected %q, got %q", tc.input, tc.expected, got)
		}
	}
}

func TestConfigDirConsistency(t *testing.T) {
	dir := ConfigDir()
	path := ConfigPath()

	home, _ := os.UserHomeDir()
	expectedDir := filepath.Join(home, ".config", "zerotwo")
	expectedPath := filepath.Join(expectedDir, "config.json")

	if dir != expectedDir {
		t.Errorf("expected ConfigDir=%q, got %q", expectedDir, dir)
	}
	if path != expectedPath {
		t.Errorf("expected ConfigPath=%q, got %q", expectedPath, path)
	}

	cfg := DefaultConfig()
	cfg.Resolve()
	if strings.HasPrefix(cfg.Agent.Workspace, "~") {
		t.Errorf("workspace has unexpanded tilde: %q", cfg.Agent.Workspace)
	}
}
