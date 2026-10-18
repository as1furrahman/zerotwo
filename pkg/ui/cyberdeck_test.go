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
			// strip ansi codes roughly
			clean := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(l, "")
			// Allow small margin for multi-byte runes
			runeCount := len([]rune(clean))
			if runeCount > w+5 {
				t.Errorf("table line exceeded col limit %d: len=%d line=%q", w, runeCount, clean)
			}
		}
	}
}

