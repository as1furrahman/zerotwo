package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestGetTerminalCols(t *testing.T) {
	cols := GetTerminalCols()
	if cols < 10 {
		t.Errorf("unexpected cols count: %d", cols)
	}
}

func TestCyberdeckBanner(t *testing.T) {
	banner := CyberdeckBanner("gemini-2.5-flash", "test-sess", "/tmp/ws", 8, 10)
	if !strings.Contains(banner, "CYBERDECK: ZEROTWO") {
		t.Errorf("banner missing title: %s", banner)
	}
	if !strings.Contains(banner, "PISTIL: CODE:002") {
		t.Errorf("banner missing pistil: %s", banner)
	}
	if !strings.Contains(banner, "gemini-2.5-flash") {
		t.Errorf("banner missing model: %s", banner)
	}
}

func TestNeuralLinkEngagement(t *testing.T) {
	out := NeuralLinkEngagement("sniffing memory bus...")
	if !strings.Contains(out, "BREACH_PROTOCOL") {
		t.Errorf("missing BREACH_PROTOCOL: %s", out)
	}
	if !strings.Contains(out, "sniffing memory bus...") {
		t.Errorf("missing action desc: %s", out)
	}
}

func TestQuickhackCard(t *testing.T) {
	card := QuickhackCard("shell_exec", "netstat -tulpn", "tcp 0 0 0.0.0.0:2222 LISTEN")
	if !strings.Contains(card, "QUICKHACK [shell_exec]") {
		t.Errorf("missing quickhack title: %s", card)
	}
	if !strings.Contains(card, "netstat -tulpn") {
		t.Errorf("missing command: %s", card)
	}
	if !strings.Contains(card, "2222") {
		t.Errorf("missing result: %s", card)
	}
}

func TestCyberdeckDock(t *testing.T) {
	dock := CyberdeckDock()
	if !strings.Contains(dock, "DECK:ACTIVE") {
		t.Errorf("missing dock label: %s", dock)
	}
	if !strings.Contains(dock, "002:darling") {
		t.Errorf("missing prompt: %s", dock)
	}
}

func TestDestructiveCyberwareWarning(t *testing.T) {
	warn := DestructiveCyberwareWarning("rm -rf /mnt/us/documents/*", "Documents Partition", "Permanent data loss")
	if !strings.Contains(warn, "DARLING, WAIT! CYBERWARE OVERHEAT") {
		t.Errorf("missing overheat header: %s", warn)
	}
	if !strings.Contains(warn, "rm -rf") {
		t.Errorf("missing action: %s", warn)
	}
}

func TestMarkdownToTerm(t *testing.T) {
	input := `# Main Title
This is **bold** text and *italic* text and ` + "`inline_code`" + `.
> This is a quote block.

| Name | Role | Status |
| --- | --- | --- |
| ZeroTwo | AI Pistil | Online |
| Darling | Human Stamen | Linked |
`
	out := MarkdownToTerm(input)
	if !strings.Contains(out, "MAIN TITLE") {
		t.Errorf("expected uppercase bold header, got: %s", out)
	}
	if !strings.Contains(out, "\033[1mbold\033[0m") {
		t.Errorf("expected bold ANSI formatting, got: %s", out)
	}
	if !strings.Contains(out, "\033[3mitalic\033[0m") {
		t.Errorf("expected italic ANSI formatting, got: %s", out)
	}
	if !strings.Contains(out, "│") {
		t.Errorf("expected table or quote border, got: %s", out)
	}
}

func TestNarrowTerminalKindle(t *testing.T) {
	// Test simulated Kindle portrait columns: 50, 60, 70
	widths := []int{45, 55, 60, 65, 72}
	origCols := os.Getenv("COLUMNS")
	defer func() {
		if origCols != "" {
			os.Setenv("COLUMNS", origCols)
		} else {
			os.Unsetenv("COLUMNS")
		}
	}()

	for _, w := range widths {
		os.Setenv("COLUMNS", fmt.Sprintf("%d", w))
		banner := CyberdeckBanner("qwen/qwen3.8-27b", "sess-test", "/tmp", 4, 5)
		if len(banner) == 0 {
			t.Errorf("empty banner for cols=%d", w)
		}
		dock := CyberdeckDock()
		if len(dock) == 0 {
			t.Errorf("empty dock for cols=%d", w)
		}
		card := QuickhackCard("shell", "ls -la /mnt/us/koreader", "crash.log\nkoreader.sh")
		if len(card) == 0 {
			t.Errorf("empty card for cols=%d", w)
		}
		warn := DestructiveCyberwareWarning("rm -rf /mnt/us", "/mnt/us", "Data loss")
		if len(warn) == 0 {
			t.Errorf("empty warn for cols=%d", w)
		}

		table := "| Col A | Col B | Col C |\n| --- | --- | --- |\n| ExtremelyLongTextInCell1 | ExtremelyLongTextInCell2 | LongText3 |"
		tableTerm := MarkdownToTerm(table)
		lines := strings.Split(tableTerm, "\n")
		for _, l := range lines {
			clean := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(l, "")
			runeCount := len([]rune(clean))
			if runeCount > w+5 {
				t.Errorf("table line exceeded col limit %d: len=%d line=%q", w, runeCount, clean)
			}
		}

		// Test ToolAuthCard
		authCard := ToolAuthCard("shell", "ps aux | grep test", "local process", "sensitive path")
		if !strings.Contains(authCard, "NEURAL AUTHORIZATION") {
			t.Errorf("authCard missing title: %s", authCard)
		}

		// Test AssistantHeader
		hdr := AssistantHeader()
		if !strings.Contains(hdr, "ZERO TWO") {
			t.Errorf("assistant header missing ZERO TWO: %s", hdr)
		}
	}
}

func TestToolAuthCard(t *testing.T) {
	card := ToolAuthCard("shell (network)", "curl -I https://api.groq.com", "remote egress", "network access")
	if !strings.Contains(card, "NEURAL AUTHORIZATION") {
		t.Errorf("missing header: %s", card)
	}
	if !strings.Contains(card, "Quickhack:") {
		t.Errorf("missing Quickhack label: %s", card)
	}
	if !strings.Contains(card, "Confirm execution? [y/N]:") {
		t.Errorf("missing confirm prompt: %s", card)
	}
}

func TestWrapText(t *testing.T) {
	input := "This is a long sentence that should definitely be wrapped onto multiple lines cleanly without breaking words in half."
	wrapped := WrapText(input, 30)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines, got %d: %q", len(lines), wrapped)
	}
	for _, l := range lines {
		if len([]rune(l)) > 30 {
			t.Errorf("line exceeded 30 runes: len=%d line=%q", len([]rune(l)), l)
		}
	}
}

func TestFencedCodeBlock(t *testing.T) {
	input := "```python\ndef hello():\n    print(\"Hello Darling\")\n```"
	rendered := MarkdownToTerm(input)
	if !strings.Contains(rendered, "CODE: PYTHON") {
		t.Errorf("expected python code badge, got: %s", rendered)
	}
	if !strings.Contains(rendered, "print(\"Hello Darling\")") {
		t.Errorf("expected code body, got: %s", rendered)
	}
}

func TestPixelPerfectNarrowKindleAudit(t *testing.T) {
	origCols := os.Getenv("COLUMNS")
	defer func() {
		if origCols != "" {
			os.Setenv("COLUMNS", origCols)
		} else {
			os.Unsetenv("COLUMNS")
		}
	}()
	os.Setenv("COLUMNS", "48")

	components := map[string]string{
		"banner":        CyberdeckBanner("openai/gpt-oss-120b", "default", "/mnt/debian", 6, 10),
		"dock":          CyberdeckDock(),
		"quickhack":     QuickhackCard("get_system_status", "{}", "CPU Load: 0.49, 0.47\nMem: 200MB free"),
		"quickhack_cmd": QuickhackCard("shell", `{"command": "ls -la /mnt/us"}`, "file1\nfile2\nfile3"),
		"engagement":    NeuralLinkEngagement("quickhack engaged..."),
		"assistant_hdr": AssistantHeader(),
		"auth_card":     ToolAuthCard("shell", "rm -rf /tmp/foo", "local", "destructive"),
	}

	for name, comp := range components {
		// 1. Assert no floating/broken rounded corner stubs
		for _, badRune := range []string{"╭", "╮", "╰", "╯"} {
			if strings.Contains(comp, badRune) {
				t.Errorf("component %s contains broken corner %q: %s", name, badRune, comp)
			}
		}
		// 2. Assert no double guillemet broken chevron
		if strings.Contains(comp, "❯") {
			t.Errorf("component %s contains unaligned chevron '❯' (renders as '»' in DejaVu Sans Mono): %s", name, comp)
		}
		// 3. Assert width within 46 columns (cols - 2 margin)
		lines := strings.Split(comp, "\n")
		for _, l := range lines {
			clean := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(l, "")
			clean = strings.TrimPrefix(clean, "\r")
			runeCount := len([]rune(clean))
			if runeCount > 46 {
				t.Errorf("component %s line exceeded 46 cols (len=%d): %q", name, runeCount, clean)
			}
		}
	}
}


