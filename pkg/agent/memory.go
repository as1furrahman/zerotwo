package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"zerotwo/pkg/config"
	"zerotwo/pkg/tools"
)

const memoryFile = "MEMORY.md"

type MemoryStore struct {
	path string
}

func NewMemoryStore(workspace string) *MemoryStore {
	_ = os.MkdirAll(workspace, 0700)
	config.CheckDirPermissions(workspace, 0700)
	p := filepath.Join(workspace, memoryFile)
	return &MemoryStore{path: p}
}

func (m *MemoryStore) Path() string {
	return m.path
}

// LoadRaw reads the full MEMORY.md file without any redactions.
func (m *MemoryStore) LoadRaw() string {
	data, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		return fmt.Sprintf("[memory read error: %v]", err)
	}
	config.CheckFilePermissions(m.path, 0600)
	return string(data)
}

// LoadFiltered reads MEMORY.md and redacts any sections matching redactSections.
func (m *MemoryStore) LoadFiltered(redactSections []string) string {
	raw := m.LoadRaw()
	if raw == "" || len(redactSections) == 0 {
		return raw
	}
	return RedactSections(raw, redactSections)
}

// RedactSections strips out markdown sections matching the given list of section titles.
func RedactSections(content string, redactList []string) string {
	if len(redactList) == 0 {
		return content
	}

	redactMap := make(map[string]bool)
	for _, s := range redactList {
		redactMap[strings.ToLower(strings.TrimSpace(s))] = true
	}

	lines := strings.Split(content, "\n")
	var result []string
	skipping := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			title := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			if redactMap[title] {
				skipping = true
				continue
			} else {
				skipping = false
			}
		} else if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			skipping = false
		}

		if !skipping {
			result = append(result, line)
		}
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

// Save atomically writes new memory content.
func (m *MemoryStore) Save(content string) error {
	return config.AtomicWriteFile(m.path, []byte(content), 0600)
}

// Patch updates or appends a specific "## Section" in MEMORY.md.
func (m *MemoryStore) Patch(section, content string) error {
	current := m.LoadRaw()
	updated := ApplySectionPatch(current, section, content)
	return m.Save(updated)
}

// ApplySectionPatch updates or appends a section. Free of unused variables.
func ApplySectionPatch(doc, section, newContent string) string {
	lines := strings.Split(doc, "\n")
	targetHeader := "## " + strings.TrimSpace(section)
	newContent = strings.TrimSpace(newContent)

	startIndex := -1
	endIndex := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, targetHeader) {
			startIndex = i
			continue
		}
		if startIndex != -1 && endIndex == -1 {
			// Find next section header or horizontal rule
			if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "# ") {
				endIndex = i
				break
			}
		}
	}

	sectionBlock := fmt.Sprintf("%s\n%s\n", targetHeader, newContent)

	if startIndex == -1 {
		// Section not found: append it to the end
		doc = strings.TrimRight(doc, "\n")
		if doc == "" {
			return sectionBlock
		}
		return doc + "\n\n" + sectionBlock
	}

	if endIndex == -1 {
		endIndex = len(lines)
	}

	var newLines []string
	newLines = append(newLines, lines[:startIndex]...)
	newLines = append(newLines, sectionBlock)
	newLines = append(newLines, lines[endIndex:]...)

	return strings.Join(newLines, "\n")
}

// ── SaveMemoryTool ────────────────────────────────────────────────────────────

type SaveMemoryTool struct {
	store *MemoryStore
}

func NewSaveMemoryTool(store *MemoryStore) tools.Tool {
	return &SaveMemoryTool{store: store}
}

func (t *SaveMemoryTool) Name() string { return "save_memory" }
func (t *SaveMemoryTool) Description() string {
	return "Update long-term memory (MEMORY.md). Overwrites current memory completely. Changes take effect on the NEXT session or via /reload."
}
func (t *SaveMemoryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"content": {"type": "string", "description": "Full markdown content for MEMORY.md"},
			"note": {"type": "string", "description": "Optional brief changelog note"}
		},
		"required": ["content"]
	}`)
}
func (t *SaveMemoryTool) Run(argsJSON string) (string, error) {
	var args struct {
		Content string `json:"content"`
		Note    string `json:"note"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(args.Content))
	note := args.Note
	if note == "" {
		note = "memory updated"
	}
	sb.WriteString(fmt.Sprintf("\n\n---\n*Last updated: %s — %s*\n", time.Now().Format("2006-01-02 15:04"), note))

	if err := t.store.Save(sb.String()); err != nil {
		return "", err
	}
	return "Memory updated successfully. (Note: changes take effect next session or with /reload)", nil
}

// ── PatchMemoryTool ───────────────────────────────────────────────────────────

type PatchMemoryTool struct {
	store *MemoryStore
}

func NewPatchMemoryTool(store *MemoryStore) tools.Tool {
	return &PatchMemoryTool{store: store}
}

func (t *PatchMemoryTool) Name() string { return "patch_memory" }
func (t *PatchMemoryTool) Description() string {
	return "Update or append a single section in MEMORY.md without rewriting the entire file. Section name should match the header (e.g. 'Active Projects')."
}
func (t *PatchMemoryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"section": {"type": "string", "description": "Section header title without '## '"},
			"content": {"type": "string", "description": "New content for this section"}
		},
		"required": ["section", "content"]
	}`)
}
func (t *PatchMemoryTool) Run(argsJSON string) (string, error) {
	var args struct {
		Section string `json:"section"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Section == "" {
		return "", fmt.Errorf("section cannot be empty")
	}

	if err := t.store.Patch(args.Section, args.Content); err != nil {
		return "", err
	}
	return fmt.Sprintf("Section %q patched successfully. (Note: changes take effect next session or with /reload)", args.Section), nil
}
