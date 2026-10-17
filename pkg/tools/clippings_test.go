package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleClippings = `Designing Data-Intensive Applications (Martin Kleppmann)
- Your Highlight on page 142 | Location 2174-2178 | Added on Monday, October 14, 2024 10:15:32 PM

The fundamental problem of distributed consensus is to get several nodes to agree on something.
==========
Clean Architecture (Robert C. Martin)
- Your Highlight on page 50 | Location 890-892 | Added on Saturday, October 12, 2024 3:10:00 PM

The goal of software architecture is to minimize the human resources required to build and maintain the system.
==========
The Pragmatic Programmer (David Thomas)
- Your Note on page 20 | Location 412 | Added on Tuesday, October 08, 2024 1:00:00 PM

Don't live with broken windows.
==========
`

func TestParseClippings(t *testing.T) {
	items := ParseClippings(sampleClippings)
	if len(items) != 3 {
		t.Fatalf("expected 3 clippings, got %d", len(items))
	}

	if items[0].Title != "Designing Data-Intensive Applications" {
		t.Errorf("unexpected title: %s", items[0].Title)
	}
	if items[0].Author != "Martin Kleppmann" {
		t.Errorf("unexpected author: %s", items[0].Author)
	}
	if items[0].Location != "2174-2178" {
		t.Errorf("unexpected location: %s", items[0].Location)
	}
	if !strings.Contains(items[0].Content, "distributed consensus") {
		t.Errorf("unexpected content: %s", items[0].Content)
	}

	if items[2].Author != "David Thomas" {
		t.Errorf("unexpected author: %s", items[2].Author)
	}
}

func TestReadClippingsTool(t *testing.T) {
	tmpDir := t.TempDir()
	clipFile := filepath.Join(tmpDir, "My Clippings.txt")
	if err := os.WriteFile(clipFile, []byte(sampleClippings), 0600); err != nil {
		t.Fatalf("failed to write test clippings: %v", err)
	}

	tool := NewReadClippingsTool(tmpDir)

	// Test 1: Read all with query
	args, _ := json.Marshal(map[string]interface{}{
		"path":  clipFile,
		"limit": 5,
		"query": "consensus",
	})
	res, err := tool.Run(string(args))
	if err != nil {
		t.Fatalf("tool execution failed: %v", err)
	}
	if !strings.Contains(res, "Kleppmann") {
		t.Errorf("expected Kleppmann in result, got: %s", res)
	}

	// Test 2: Query non-matching
	args2, _ := json.Marshal(map[string]interface{}{
		"path":  clipFile,
		"limit": 5,
		"query": "non_existent_book_keyword",
	})
	res2, err := tool.Run(string(args2))
	if err != nil {
		t.Fatalf("tool execution failed: %v", err)
	}
	if !strings.Contains(res2, "No reading highlights found") {
		t.Errorf("expected no highlights message, got: %s", res2)
	}
}
