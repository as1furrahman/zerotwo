package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ClippingItem represents an extracted highlight or note from Kindle's My Clippings.txt.
type ClippingItem struct {
	Index     int    `json:"index"`
	Title     string `json:"title"`
	Author    string `json:"author,omitempty"`
	Metadata  string `json:"metadata,omitempty"`
	Content   string `json:"content"`
	Location  string `json:"location,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// ParseClippings parses the standard Kindle My Clippings.txt block format.
func ParseClippings(raw string) []ClippingItem {
	blocks := strings.Split(raw, "==========")
	var items []ClippingItem

	titleAuthorRe := regexp.MustCompile(`^(.*?)(?:\s*\(([^)]+)\))?$`)
	locRe := regexp.MustCompile(`(?:Location|Loc\.)\s+([0-9-]+)`)
	addedRe := regexp.MustCompile(`(?:Added on|Date:)\s+(.*)$`)

	for _, block := range blocks {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" {
			continue
		}

		lines := strings.SplitN(trimmed, "\n", 3)
		if len(lines) < 2 {
			continue
		}

		titleLine := strings.TrimSpace(lines[0])
		metaLine := strings.TrimSpace(lines[1])
		content := ""
		if len(lines) >= 3 {
			content = strings.TrimSpace(lines[2])
		}

		if content == "" && strings.HasPrefix(metaLine, "-") {
			continue
		}

		title := titleLine
		author := ""
		if m := titleAuthorRe.FindStringSubmatch(titleLine); len(m) > 1 {
			title = strings.TrimSpace(m[1])
			if len(m) > 2 {
				author = strings.TrimSpace(m[2])
			}
		}

		location := ""
		if m := locRe.FindStringSubmatch(metaLine); len(m) > 1 {
			location = strings.TrimSpace(m[1])
		}

		timestamp := ""
		if m := addedRe.FindStringSubmatch(metaLine); len(m) > 1 {
			timestamp = strings.TrimSpace(m[1])
		}

		items = append(items, ClippingItem{
			Index:     len(items) + 1,
			Title:     title,
			Author:    author,
			Metadata:  metaLine,
			Content:   content,
			Location:  location,
			Timestamp: timestamp,
		})
	}

	return items
}

// FindClippingsPath searches candidate paths for Kindle My Clippings.txt.
func FindClippingsPath(overridePath, workspace string) string {
	if overridePath != "" {
		if _, err := os.Stat(overridePath); err == nil {
			return overridePath
		}
	}

	candidates := []string{
		"/mnt/us/documents/My Clippings.txt",
		filepath.Join(workspace, "My Clippings.txt"),
		filepath.Join(workspace, "documents", "My Clippings.txt"),
		"My Clippings.txt",
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	if overridePath != "" {
		return overridePath
	}
	return "/mnt/us/documents/My Clippings.txt"
}

// ReadClippingsFile reads and filters clippings from the specified path.
func ReadClippingsFile(path string, limit int, query string) ([]ClippingItem, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read clippings file at %s: %w", path, err)
	}

	all := ParseClippings(string(data))
	if len(all) == 0 {
		return nil, nil
	}

	var filtered []ClippingItem
	queryLower := strings.ToLower(strings.TrimSpace(query))

	for _, item := range all {
		if queryLower != "" {
			match := strings.Contains(strings.ToLower(item.Title), queryLower) ||
				strings.Contains(strings.ToLower(item.Author), queryLower) ||
				strings.Contains(strings.ToLower(item.Content), queryLower)
			if !match {
				continue
			}
		}
		filtered = append(filtered, item)
	}

	// Return the most recent N items
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}

	return filtered, nil
}

// ReadClippingsTool implements the Tool interface for reading Kindle clippings.
type ReadClippingsTool struct {
	workspace string
}

func NewReadClippingsTool(workspace string) *ReadClippingsTool {
	return &ReadClippingsTool{workspace: workspace}
}

func (t *ReadClippingsTool) SetWorkspace(ws string) {
	t.workspace = ws
}

func (t *ReadClippingsTool) Name() string { return "read_clippings" }

func (t *ReadClippingsTool) Description() string {
	return "Read recent reading highlights and notes (Memory Shards) from Kindle's My Clippings.txt. Allows filtering by book title, author, or keyword query."
}

func (t *ReadClippingsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Optional path to clippings file. Defaults to /mnt/us/documents/My Clippings.txt or workspace."},
			"limit": {"type": "integer", "description": "Maximum number of recent highlights to return (1-50, default 5)."},
			"query": {"type": "string", "description": "Optional search query to filter highlights by book title, author, or note content."}
		}
	}`)
}

func (t *ReadClippingsTool) Run(argsJSON string) (string, error) {
	var p struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
		Query string `json:"query"`
	}
	if strings.TrimSpace(argsJSON) != "" {
		_ = json.Unmarshal([]byte(argsJSON), &p)
	}

	clipPath := FindClippingsPath(p.Path, t.workspace)
	items, err := ReadClippingsFile(clipPath, p.Limit, p.Query)
	if err != nil {
		return "", err
	}

	if len(items) == 0 {
		return "No reading highlights found matching criteria.", nil
	}

	outBytes, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(outBytes), nil
}
