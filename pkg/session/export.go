package session

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExportDataDump formats the session into a clean e-reader HTML document for Kindle.
func ExportDataDump(sess *Session, workspace string) (string, error) {
	if sess == nil || len(sess.Messages) == 0 {
		return "", fmt.Errorf("no messages to export in active session")
	}

	targetDir := findExportDirectory(workspace)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		targetDir = filepath.Join(workspace, "ZeroTwo_Exports")
		_ = os.MkdirAll(targetDir, 0755)
	}

	timestamp := time.Now().Format("20060102_150405")
	cleanID := strings.ReplaceAll(sess.ID, "/", "_")
	fileName := fmt.Sprintf("DataDump_%s_%s.html", cleanID, timestamp)
	fullPath := filepath.Join(targetDir, fileName)

	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"UTF-8\">\n")
	sb.WriteString(fmt.Sprintf("<title>ZeroTwo Cyberdeck Data Dump · %s</title>\n", html.EscapeString(sess.ID)))
	sb.WriteString("<style>\n")
	sb.WriteString("  body { font-family: Georgia, 'Bookerly', serif; font-size: 1.1em; line-height: 1.6; margin: 5% 8%; color: #111; background: #fff; }\n")
	sb.WriteString("  h1 { font-size: 1.5em; border-bottom: 2px solid #222; padding-bottom: 8px; margin-bottom: 4px; }\n")
	sb.WriteString("  .meta { color: #555; font-size: 0.9em; margin-bottom: 24px; }\n")
	sb.WriteString("  .turn { margin-bottom: 24px; page-break-inside: avoid; }\n")
	sb.WriteString("  .badge { display: inline-block; font-family: monospace; font-size: 0.85em; font-weight: bold; padding: 2px 8px; border-radius: 3px; margin-bottom: 8px; }\n")
	sb.WriteString("  .user-badge { background: #222; color: #fff; }\n")
	sb.WriteString("  .ai-badge { background: #444; color: #fff; }\n")
	sb.WriteString("  .content { margin-left: 4px; }\n")
	sb.WriteString("  pre, code { font-family: 'Courier New', monospace; background: #f4f4f4; border-left: 3px solid #444; padding: 8px 12px; overflow-x: auto; }\n")
	sb.WriteString("  hr { border: none; border-top: 1px solid #ddd; margin: 24px 0; }\n")
	sb.WriteString("</style>\n</head>\n<body>\n")

	sb.WriteString(fmt.Sprintf("<h1>▲▲ ZEROTWO CYBERDECK // DATA DUMP</h1>\n"))
	sb.WriteString(fmt.Sprintf("<div class=\"meta\">Session: <strong>%s</strong> · Exported: %s · Turns: %d</div>\n<hr/>\n",
		html.EscapeString(sess.ID), time.Now().Format("Jan 02, 2006 15:04:05"), len(sess.Messages)))

	for _, msg := range sess.Messages {
		if msg.Role == "system" {
			continue
		}
		if msg.Role == "tool" {
			continue // skip raw tool payloads in reader mode
		}

		content := strings.TrimSpace(msg.Content)
		if content == "" && len(msg.ToolCalls) > 0 {
			continue
		}

		sb.WriteString("<div class=\"turn\">\n")
		if msg.Role == "user" {
			sb.WriteString("  <div class=\"badge user-badge\">[DARLING]</div>\n")
		} else {
			sb.WriteString("  <div class=\"badge ai-badge\">[▲▲ ZERO TWO · CODE:002]</div>\n")
		}

		formatted := formatContentHTML(content)
		sb.WriteString(fmt.Sprintf("  <div class=\"content\">%s</div>\n", formatted))
		sb.WriteString("</div>\n<hr/>\n")
	}

	sb.WriteString("</body>\n</html>\n")

	if err := os.WriteFile(fullPath, []byte(sb.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write export file to %s: %w", fullPath, err)
	}

	return fullPath, nil
}

func findExportDirectory(workspace string) string {
	candidates := []string{
		"/mnt/us/documents/ZeroTwo",
		"/mnt/us/documents",
		filepath.Join(workspace, "documents"),
		filepath.Join(workspace, "ZeroTwo_Exports"),
	}

	for _, d := range candidates {
		// Test writeability
		if err := os.MkdirAll(d, 0755); err == nil {
			testFile := filepath.Join(d, ".write_test")
			if err := os.WriteFile(testFile, []byte("ok"), 0644); err == nil {
				_ = os.Remove(testFile)
				return d
			}
		}
	}

	return filepath.Join(workspace, "ZeroTwo_Exports")
}

func formatContentHTML(raw string) string {
	escaped := html.EscapeString(raw)

	// Replace newlines with <p> or <br/>
	paragraphs := strings.Split(escaped, "\n\n")
	var formatted []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Replace single newlines with <br/>
		p = strings.ReplaceAll(p, "\n", "<br/>\n")
		formatted = append(formatted, fmt.Sprintf("<p>%s</p>", p))
	}

	return strings.Join(formatted, "\n")
}
