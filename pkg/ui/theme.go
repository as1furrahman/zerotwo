package ui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

type ThemeStyle string

const (
	ThemeHybrid    ThemeStyle = "hybrid"
	ThemeStrelizia ThemeStyle = "strelizia"
	ThemeTelemetry ThemeStyle = "telemetry"
	ThemeTokyo     ThemeStyle = "tokyo"
	ThemeMinimal   ThemeStyle = "minimal"
)

var CurrentTheme = ThemeMinimal

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes all ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// RuneWidth returns the terminal visual column width for a rune.
func RuneWidth(r rune) int {
	if r < 0x20 {
		return 0
	}
	if r >= 0x20 && r < 0x7f {
		return 1
	}

	// CJK Unified Ideographs & Hiragana / Katakana
	if (r >= 0x3000 && r <= 0x9FFF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFF01 && r <= 0xFF60) ||
		(r >= 0x3040 && r <= 0x309F) || // Hiragana
		(r >= 0x30A0 && r <= 0x30FF) { // Katakana
		return 2
	}

	// Full-width Japanese punctuation
	if r == '「' || r == '」' || r == '・' || r == 'ー' {
		return 2
	}

	// Default fallback
	if unicode.Is(unicode.Scripts["Han"], r) {
		return 2
	}

	return 1
}

// StringWidth returns the visual column width of a string.
func StringWidth(s string) int {
	clean := StripANSI(s)
	w := 0
	for _, r := range clean {
		w += RuneWidth(r)
	}
	return w
}

// SetTheme sets the active theme by name. Returns true if recognized.
func SetTheme(theme string) bool {
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "hybrid", "h":
		CurrentTheme = ThemeHybrid
		return true
	case "strelizia", "s", "pilot":
		CurrentTheme = ThemeStrelizia
		return true
	case "telemetry", "t", "cyberdeck":
		CurrentTheme = ThemeTelemetry
		return true
	case "tokyo", "j", "japan":
		CurrentTheme = ThemeTokyo
		return true
	case "minimal", "m", "default":
		CurrentTheme = ThemeMinimal
		return true
	default:
		return false
	}
}

// AvailableThemes lists supported themes.
func AvailableThemes() []string {
	return []string{"hybrid", "strelizia", "telemetry", "tokyo", "minimal"}
}

// SafeRule creates a horizontal rule with prefix and suffix guaranteed not to exceed targetW.
func SafeRule(prefix, suffix string, targetW int) string {
	pW := StringWidth(prefix)
	sW := StringWidth(suffix)
	dashCount := targetW - pW - sW
	if dashCount >= 0 {
		return prefix + strings.Repeat("─", dashCount) + suffix
	}
	// If prefix + suffix exceeds targetW, truncate prefix to fit targetW - sW
	if sW < targetW {
		availForPrefix := targetW - sW
		truncatedPrefix := TruncateToWidth(prefix, availForPrefix)
		return truncatedPrefix + suffix
	}
	return TruncateToWidth(prefix+suffix, targetW)
}

// SafeBox creates a framed box around text guaranteed not to exceed targetW.
func SafeBox(innerText string, targetW int) string {
	top := "┌" + strings.Repeat("─", targetW-2) + "┐"
	bot := "└" + strings.Repeat("─", targetW-2) + "┘"
	w := StringWidth(innerText)
	pad := targetW - 2 - w
	if pad < 0 {
		pad = 0
	}
	mid := "│" + innerText + strings.Repeat(" ", pad) + "│"
	return fmt.Sprintf("%s\n%s\n%s", top, mid, bot)
}

// TruncateToWidth truncates a string so that its visual column width does not exceed maxW.
func TruncateToWidth(s string, maxW int) string {
	clean := StripANSI(s)
	curW := 0
	var out []rune
	for _, r := range clean {
		rw := RuneWidth(r)
		if curW+rw > maxW {
			break
		}
		curW += rw
		out = append(out, r)
	}
	return string(out)
}

// RenderThemeBanner renders the top banner based on CurrentTheme.
func RenderThemeBanner(model, sessionID, workspace string, ramBlocks, totalBlocks int) string {
	cols := GetTerminalCols()
	targetW := cols - 4
	if targetW < 36 {
		targetW = 36
	}
	if targetW > 44 && cols <= 50 {
		targetW = 44
	}

	tel := GetTelemetry()
	displayModel := model
	if idx := strings.LastIndex(displayModel, "/"); idx != -1 {
		displayModel = displayModel[idx+1:]
	}
	if len(displayModel) > 12 {
		displayModel = displayModel[:12]
	}

	cRam := (ramBlocks * 3) / totalBlocks
	if cRam > 3 {
		cRam = 3
	}
	meter := strings.Repeat("▰", cRam) + strings.Repeat("▱", 3-cRam)

	switch CurrentTheme {
	case ThemeStrelizia:
		inner := fmt.Sprintf(" \033[7m 002:PISTIL \033[0m · %s · ⚡%d%% · %s", meter, tel.BatteryPct, displayModel)
		return fmt.Sprintf("\033[1m%s\033[0m\n", SafeBox(inner, targetW))

	case ThemeTelemetry:
		top := fmt.Sprintf("✦ CYBERDECK:002 · ⚡%d%%%s · %d°C · %dM", tel.BatteryPct, tel.BatteryGauge(), tel.TempC, tel.RAMFreeMB)
		line1 := TruncateToWidth(top, targetW)
		rule := strings.Repeat("─", targetW)
		return fmt.Sprintf("\033[1m%s\n%s\033[0m\n", line1, rule)

	case ThemeTokyo:
		top := fmt.Sprintf("▲ ▲ ゼロツー // CODE:002 · %s · %s", meter, displayModel)
		line1 := TruncateToWidth(top, targetW)
		rule := strings.Repeat("─", targetW)
		return fmt.Sprintf("\033[1m%s\n%s\033[0m\n", line1, rule)

	case ThemeHybrid:
		top := fmt.Sprintf("▲ ▲ ✦ ゼロツー // 002 · ⚡%d%%%s · %d°C", tel.BatteryPct, tel.BatteryGauge(), tel.TempC)
		line1 := TruncateToWidth(top, targetW)
		rule := strings.Repeat("─", targetW)
		return fmt.Sprintf("\033[1m%s\n%s\033[0m\n", line1, rule)

	case ThemeMinimal:
		fallthrough
	default:
		top := fmt.Sprintf("✦ ZEROTWO // 002 · %s · %s", meter, displayModel)
		rule := strings.Repeat("─", targetW)
		return fmt.Sprintf("\033[1m%s\n%s\033[0m\n", top, rule)
	}
}

// RenderThemeDock renders the bottom input dock and prompt based on CurrentTheme.
func RenderThemeDock() string {
	cols := GetTerminalCols()
	targetW := cols - 4
	if targetW < 36 {
		targetW = 36
	}
	if targetW > 44 && cols <= 50 {
		targetW = 44
	}

	tel := GetTelemetry()

	var topLine, prompt string
	switch CurrentTheme {
	case ThemeStrelizia:
		topLine = SafeRule("/// ✦ STRELIZIA ", " ? HELP · q QUIT ///", targetW)
		prompt = "\033[7m 002:darling \033[0m › "
	case ThemeTelemetry:
		topLine = SafeRule(fmt.Sprintf("/// ✦ SYS:OK ─ L:%.2f ", tel.LoadAvg), " ? HELP · q ///", targetW)
		prompt = "002:darling › "
	case ThemeTokyo:
		topLine = SafeRule("/// ✦ 機体同期 [ONLINE] ", " ? HELP · q ///", targetW)
		prompt = "ダーリン › "
	case ThemeHybrid:
		topLine = SafeRule("/// ✦ STRELIZIA ", " ? HELP · q QUIT ///", targetW)
		prompt = "\033[7m 002:darling \033[0m › "
	case ThemeMinimal:
		fallthrough
	default:
		topLine = SafeRule("/// ✦ DECK:ACTIVE ", " ? HELP · q QUIT ///", targetW)
		prompt = "002:darling › "
	}

	return fmt.Sprintf("\r%s\n%s", topLine, prompt)
}

// RenderThemeAssistantHeader renders the speaker header based on CurrentTheme.
func RenderThemeAssistantHeader() string {
	cols := GetTerminalCols()
	targetW := cols - 4
	if targetW < 36 {
		targetW = 36
	}
	if targetW > 44 && cols <= 50 {
		targetW = 44
	}

	switch CurrentTheme {
	case ThemeStrelizia:
		return SafeRule("/// \033[7m CODE:002:ZEROTWO \033[0m ", " ///", targetW) + "\n"
	case ThemeTelemetry:
		return SafeRule("/// ✦ 002:TELEMETRY ", " ///", targetW) + "\n"
	case ThemeTokyo:
		return SafeRule("/// ✦ ゼロツー // CODE:002 ", " ///", targetW) + "\n"
	case ThemeHybrid:
		return SafeRule("/// \033[7m ✦ ZERO TWO // CODE:002 \033[0m ", " ///", targetW) + "\n"
	case ThemeMinimal:
		fallthrough
	default:
		return SafeRule("/// ✦ ZERO TWO // CODE:002 ", " ///", targetW) + "\n"
	}
}

// RenderThemeQuickhackCard renders a tool execution card based on CurrentTheme.
func RenderThemeQuickhackCard(toolName, cmdOrArgs, result string) string {
	cols := GetTerminalCols()
	targetW := cols - 4
	if targetW < 36 {
		targetW = 36
	}
	if targetW > 44 && cols <= 50 {
		targetW = 44
	}

	var label string
	switch CurrentTheme {
	case ThemeTokyo:
		label = fmt.Sprintf("── ⚡ 簡易ハック: %s ", toolName)
	case ThemeTelemetry:
		label = fmt.Sprintf("── ⚡ EXEC: %s ", toolName)
	default:
		label = fmt.Sprintf("── ⚡ QUICKHACK: %s ", toolName)
	}

	topLine := SafeRule(label, "", targetW)
	resLine := fmt.Sprintf("  › %s\n", result)
	return fmt.Sprintf("\n%s\n%s", topLine, resLine)
}

// GenerateDemoMockup produces a full-screen demo mockup for any theme.
func GenerateDemoMockup(theme ThemeStyle) string {
	prev := CurrentTheme
	CurrentTheme = theme
	defer func() { CurrentTheme = prev }()

	banner := RenderThemeBanner("gpt-oss-120b", "sess-002", "/root", 8, 10)
	dock := RenderThemeDock()
	card := RenderThemeQuickhackCard("get_system_status", "", "CPU: 0.31 · RAM: 204M free · 47°C")
	hdr := RenderThemeAssistantHeader()

	var msg string
	switch theme {
	case ThemeStrelizia:
		msg = "Darling, our neural synchrony is at 98.4%.\nAll airframe systems are green."
	case ThemeTelemetry:
		msg = "• CPU load: 0.31 / 0.33 / 0.39\n• Memory: 204 MiB available / 502 MiB total\n• Thermals: CPU 47.4°C · Battery 78%"
	case ThemeTokyo:
		msg = "「ダーリン、機体の状態は良好だよ。」\n\n• CPU負荷: 0.31 (安定)\n• 残りバッテリー: 78% (放電中)"
	case ThemeHybrid:
		msg = "System telemetry nominal, Darling:\n\n- CPU load: 0.31 / 0.33 / 0.39\n- Thermals: 47.4°C · Battery: 78%"
	case ThemeMinimal:
		fallthrough
	default:
		msg = "All systems operational, Darling."
	}

	return fmt.Sprintf("\033[2J\033[H%s%s%sshow system status\n%s\n%s%s\n\n%s\033[7m \033[0m",
		banner, dock, "\n", card, hdr, msg, dock)
}
