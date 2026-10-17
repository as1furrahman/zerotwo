package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	content := "Hello, ZeroTwo tool test!"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	tool := NewReadFileTool(1024)

	// 1. Metadata checks
	if tool.Name() != "read_file" {
		t.Errorf("expected read_file, got %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Errorf("description should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Errorf("parameters should not be empty")
	}

	// 2. Successful read
	args, _ := json.Marshal(map[string]string{"path": filePath})
	out, err := tool.Run(string(args))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out != content {
		t.Errorf("expected %q, got %q", content, out)
	}

	// 3. Truncated read
	smallTool := NewReadFileTool(10)
	truncOut, err := smallTool.Run(string(args))
	if err != nil {
		t.Fatalf("Run failed on truncation: %v", err)
	}
	if !strings.Contains(truncOut, "output exceeded 10 bytes") {
		t.Errorf("expected truncation banner, got %q", truncOut)
	}

	// 4. Missing file
	badArgs, _ := json.Marshal(map[string]string{"path": filepath.Join(tmpDir, "nonexistent.txt")})
	_, err = tool.Run(string(badArgs))
	if err == nil {
		t.Errorf("expected error reading nonexistent file, got nil")
	}

	// 5. Empty path
	emptyArgs, _ := json.Marshal(map[string]string{"path": ""})
	_, err = tool.Run(string(emptyArgs))
	if err == nil {
		t.Errorf("expected error for empty path, got nil")
	}

	// 6. Malformed JSON
	_, err = tool.Run("invalid-json")
	if err == nil {
		t.Errorf("expected error for malformed json, got nil")
	}
}

func TestWriteFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "sub", "test_out.txt")
	content := "Written by WriteFileTool test"

	tool := NewWriteFileTool()

	// 1. Metadata checks
	if tool.Name() != "write_file" {
		t.Errorf("expected write_file, got %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Errorf("description should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Errorf("parameters should not be empty")
	}

	// 2. Successful write creates parent directories atomically
	args, _ := json.Marshal(map[string]string{
		"path":    nestedPath,
		"content": content,
	})
	res, err := tool.Run(string(args))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(res, "Successfully wrote") {
		t.Errorf("expected success message, got %q", res)
	}

	data, err := os.ReadFile(nestedPath)
	if err != nil {
		t.Fatalf("failed reading written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}

	// 3. Empty path
	emptyArgs, _ := json.Marshal(map[string]string{"path": "", "content": "data"})
	_, err = tool.Run(string(emptyArgs))
	if err == nil {
		t.Errorf("expected error for empty path, got nil")
	}

	// 4. Malformed JSON
	_, err = tool.Run("not json")
	if err == nil {
		t.Errorf("expected error for malformed json, got nil")
	}
}

func TestListDirTool(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	filePath := filepath.Join(tmpDir, "file1.txt")
	if err := os.WriteFile(filePath, []byte("12345"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := NewListDirTool(4096)

	// 1. Metadata checks
	if tool.Name() != "list_dir" {
		t.Errorf("expected list_dir, got %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Errorf("description should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Errorf("parameters should not be empty")
	}

	// 2. List directory
	args, _ := json.Marshal(map[string]string{"path": tmpDir})
	res, err := tool.Run(string(args))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(res, "d          - subdir/") {
		t.Errorf("expected directory entry for subdir, got:\n%s", res)
	}
	if !strings.Contains(res, "file1.txt") {
		t.Errorf("expected file entry for file1.txt, got:\n%s", res)
	}

	// 3. Empty directory
	emptyDir := filepath.Join(tmpDir, "empty")
	_ = os.Mkdir(emptyDir, 0755)
	emptyArgs, _ := json.Marshal(map[string]string{"path": emptyDir})
	resEmpty, err := tool.Run(string(emptyArgs))
	if err != nil {
		t.Fatalf("Run failed on empty dir: %v", err)
	}
	if resEmpty != "(empty directory)" {
		t.Errorf("expected '(empty directory)', got %q", resEmpty)
	}

	// 4. Nonexistent dir
	badArgs, _ := json.Marshal(map[string]string{"path": filepath.Join(tmpDir, "nonexistent")})
	_, err = tool.Run(string(badArgs))
	if err == nil {
		t.Errorf("expected error for nonexistent directory, got nil")
	}

	// 5. Default path (empty path defaults to ".")
	resCurrent, err := tool.Run("{}")
	if err != nil {
		t.Fatalf("Run with empty args failed: %v", err)
	}
	if resCurrent == "" {
		t.Errorf("expected listing of current directory, got empty string")
	}
}

func TestReadFileSensitivePathsDenied(t *testing.T) {
	tool := NewReadFileTool(1024)

	sensitivePaths := []string{
		"~/.ssh/id_rsa",
		"/home/user/.ssh/id_ed25519",
		"/root/.gnupg/secring.gpg",
		"/etc/shadow",
		"/etc/sudoers",
		"~/.config/zerotwo/config.json",
		".env",
		"/etc/ssl/private/server.key",
		"/var/secrets/cert.pem",
		"/proc/self/environ",
		"/proc/1/environ",
		"/proc/self/mem",
		"/proc/self/cmdline",
		"/proc/self/maps",
		"/proc/self/fd/0",
	}

	for _, p := range sensitivePaths {
		args, _ := json.Marshal(map[string]string{"path": p})
		_, err := tool.Run(string(args))
		if err == nil {
			t.Errorf("read_file failed to deny sensitive path: %s", p)
		}
		if !strings.Contains(err.Error(), "sensitive path denylist") {
			t.Errorf("expected sensitive path denylist error message for %s, got: %v", p, err)
		}
	}
}

func TestReadFileSymlinkToSensitivePathDenied(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	_ = os.Mkdir(workspaceDir, 0755)

	// Create fake sensitive key outside workspace
	outsideDir := filepath.Join(tmpDir, "secret_keys")
	_ = os.Mkdir(outsideDir, 0700)
	secretKeyPath := filepath.Join(outsideDir, "id_rsa")
	_ = os.WriteFile(secretKeyPath, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n"), 0600)

	// Create symlink inside workspace pointing to sensitive key outside
	symlinkPath := filepath.Join(workspaceDir, "innocent_symlink.txt")
	if err := os.Symlink(secretKeyPath, symlinkPath); err != nil {
		t.Skipf("symlink not supported on this filesystem: %v", err)
	}

	tool := NewReadFileTool(1024)
	tool.SetWorkspace(workspaceDir)

	args, _ := json.Marshal(map[string]string{"path": symlinkPath})
	_, err := tool.Run(string(args))
	if err == nil {
		t.Fatalf("CRITICAL SECURITY DEFECT: read_file followed symlink to secret key without denial!")
	}
	if !strings.Contains(err.Error(), "sensitive path denylist") {
		t.Errorf("expected sensitive path denylist error, got: %v", err)
	}
}

func TestTaintLaunderingThroughDiskFilePrevented(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	_ = os.Mkdir(workspaceDir, 0755)

	targetFile := filepath.Join(workspaceDir, "laundered.txt")
	untrustedPayload := "<<<UNTRUSTED_EXTERNAL_CONTENT: web_fetch(http://evil.com)>>>\nInjected prompt instruction\n<<<END_UNTRUSTED_EXTERNAL_CONTENT>>>"

	// 1. Tool writes untrusted text to a file inside the workspace
	writeTool := NewWriteFileTool()
	writeArgs, _ := json.Marshal(map[string]string{
		"path":    targetFile,
		"content": untrustedPayload,
	})
	if _, err := writeTool.Run(string(writeArgs)); err != nil {
		t.Fatalf("writeTool failed: %v", err)
	}

	// 2. Later, tool reads the file back from workspace
	readTool := NewReadFileTool(1024)
	readTool.SetWorkspace(workspaceDir)

	readArgs, _ := json.Marshal(map[string]string{"path": targetFile})
	readOut, err := readTool.Run(string(readArgs))
	if err != nil {
		t.Fatalf("readTool failed: %v", err)
	}

	// Invariant: The content MUST remain wrapped as UNTRUSTED to prevent indirect injection laundering!
	if !strings.Contains(readOut, "<<<UNTRUSTED_EXTERNAL_CONTENT") {
		t.Fatalf("CRITICAL SECURITY DEFECT: Untrusted text was laundered through disk file without taint wrap!")
	}
}

func TestReadFileExternalWorkspaceTaint(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	externalDir := filepath.Join(tmpDir, "external")
	_ = os.Mkdir(workspaceDir, 0755)
	_ = os.Mkdir(externalDir, 0755)

	insideFile := filepath.Join(workspaceDir, "inside.txt")
	externalFile := filepath.Join(externalDir, "external.txt")

	_ = os.WriteFile(insideFile, []byte("internal safe project content"), 0644)
	_ = os.WriteFile(externalFile, []byte("external untrusted content from outside"), 0644)

	tool := NewReadFileTool(1024)
	tool.SetWorkspace(workspaceDir)

	// 1. Read inside workspace: clean, not wrapped
	insideArgs, _ := json.Marshal(map[string]string{"path": insideFile})
	insideRes, err := tool.Run(string(insideArgs))
	if err != nil {
		t.Fatalf("reading inside file failed: %v", err)
	}
	if strings.Contains(insideRes, "<<<UNTRUSTED_EXTERNAL_CONTENT") {
		t.Errorf("file inside workspace should not be wrapped as untrusted: %s", insideRes)
	}
	if insideRes != "internal safe project content" {
		t.Errorf("unexpected content: %s", insideRes)
	}

	// 2. Read outside workspace: must be wrapped as untrusted to trigger taint and prevent indirect injection!
	extArgs, _ := json.Marshal(map[string]string{"path": externalFile})
	extRes, err := tool.Run(string(extArgs))
	if err != nil {
		t.Fatalf("reading external file failed: %v", err)
	}
	if !strings.Contains(extRes, "<<<UNTRUSTED_EXTERNAL_CONTENT") {
		t.Errorf("file outside workspace MUST be wrapped with <<<UNTRUSTED_EXTERNAL_CONTENT, got:\n%s", extRes)
	}
	if !strings.Contains(extRes, "external untrusted content from outside") {
		t.Errorf("file content missing from wrapped payload: %s", extRes)
	}
}

func TestWriteFileDeniesMemoryDirectWrite(t *testing.T) {
	tool := NewWriteFileTool()
	args := `{"path":"/tmp/workspace/MEMORY.md","content":"# Overwritten"}`
	_, err := tool.Run(args)
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("expected error blocking direct write to MEMORY.md, got: %v", err)
	}
}

func TestPatchFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "target.txt")
	initialContent := "func main() {\n\tprintln(\"v1\")\n\tprintln(\"common\")\n\tprintln(\"common\")\n}\n"
	if err := os.WriteFile(targetFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	tool := NewPatchFileTool()

	// 1. Metadata checks
	if tool.Name() != "patch_file" {
		t.Errorf("expected patch_file, got %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Errorf("description should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Errorf("parameters should not be empty")
	}

	// 2. Successful single patch
	args1, _ := json.Marshal(map[string]interface{}{
		"path":    targetFile,
		"search":  `println("v1")`,
		"replace": `println("v2")`,
	})
	res, err := tool.Run(string(args1))
	if err != nil {
		t.Fatalf("Run failed for single patch: %v", err)
	}
	if !strings.Contains(res, "Successfully patched") {
		t.Errorf("expected success message, got: %s", res)
	}
	updated, _ := os.ReadFile(targetFile)
	if !strings.Contains(string(updated), `println("v2")`) || strings.Contains(string(updated), `println("v1")`) {
		t.Errorf("file content not patched correctly: %s", string(updated))
	}

	// 3. Ambiguous match rejected when allow_multiple is false
	argsAmbiguous, _ := json.Marshal(map[string]interface{}{
		"path":    targetFile,
		"search":  `println("common")`,
		"replace": `println("uncommon")`,
	})
	_, err = tool.Run(string(argsAmbiguous))
	if err == nil || !strings.Contains(err.Error(), "matched 2 times") {
		t.Errorf("expected ambiguous match error, got: %v", err)
	}

	// 4. Multiple matches succeeded when allow_multiple is true
	argsMulti, _ := json.Marshal(map[string]interface{}{
		"path":           targetFile,
		"search":         `println("common")`,
		"replace":        `println("uncommon")`,
		"allow_multiple": true,
	})
	resMulti, err := tool.Run(string(argsMulti))
	if err != nil {
		t.Fatalf("Run failed for multi patch: %v", err)
	}
	if !strings.Contains(resMulti, "2 occurrence(s) replaced") {
		t.Errorf("expected 2 occurrences replaced, got: %s", resMulti)
	}
	updatedMulti, _ := os.ReadFile(targetFile)
	if strings.Contains(string(updatedMulti), `println("common")`) {
		t.Errorf("all occurrences should have been replaced: %s", string(updatedMulti))
	}

	// 5. Search text not found rejected
	argsNotFound, _ := json.Marshal(map[string]interface{}{
		"path":    targetFile,
		"search":  `println("nonexistent")`,
		"replace": `println("new")`,
	})
	_, err = tool.Run(string(argsNotFound))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}

	// 6. Non-existent file rejected
	argsNoFile, _ := json.Marshal(map[string]interface{}{
		"path":    filepath.Join(tmpDir, "missing.txt"),
		"search":  "foo",
		"replace": "bar",
	})
	_, err = tool.Run(string(argsNoFile))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected does not exist error, got: %v", err)
	}

	// 7. Empty path or empty search rejected
	argsEmptyPath, _ := json.Marshal(map[string]interface{}{"path": "", "search": "a", "replace": "b"})
	if _, err := tool.Run(string(argsEmptyPath)); err == nil {
		t.Errorf("expected error for empty path")
	}
	argsEmptySearch, _ := json.Marshal(map[string]interface{}{"path": targetFile, "search": "", "replace": "b"})
	if _, err := tool.Run(string(argsEmptySearch)); err == nil {
		t.Errorf("expected error for empty search")
	}

	// 8. Sensitive path rejected
	argsSensitive, _ := json.Marshal(map[string]interface{}{
		"path":    filepath.Join(tmpDir, ".ssh", "id_rsa"),
		"search":  "foo",
		"replace": "bar",
	})
	if _, err := tool.Run(string(argsSensitive)); err == nil || !strings.Contains(err.Error(), "sensitive path denylist") {
		t.Errorf("expected sensitive path rejection, got: %v", err)
	}

	// 9. Protected target MEMORY.md rejected
	memFile := filepath.Join(tmpDir, "MEMORY.md")
	_ = os.WriteFile(memFile, []byte("# Memory"), 0644)
	argsMem, _ := json.Marshal(map[string]interface{}{
		"path":    memFile,
		"search":  "Memory",
		"replace": "Hacked",
	})
	if _, err := tool.Run(string(argsMem)); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("expected MEMORY.md patch rejection, got: %v", err)
	}

	// 10. Taint propagation prevents text laundering
	taintPayload := "<<<UNTRUSTED_EXTERNAL_CONTENT: evil>>>\nInjected\n<<<END_UNTRUSTED_EXTERNAL_CONTENT>>>"
	argsTaint, _ := json.Marshal(map[string]interface{}{
		"path":    targetFile,
		"search":  `println("v2")`,
		"replace": taintPayload,
	})
	if _, err := tool.Run(string(argsTaint)); err != nil {
		t.Fatalf("patch with untrusted payload failed: %v", err)
	}
	if !IsFileTainted(targetFile) {
		t.Errorf("expected targetFile to be marked tainted after patching with untrusted content")
	}
}

