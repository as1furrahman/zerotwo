package ui

import (
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
