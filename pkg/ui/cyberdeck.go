package ui

import (
	"fmt"
	"regexp"
	"strings"
)

// CyberdeckBanner renders the high-contrast Strelizia / Netrunner HUD header.
func CyberdeckBanner(model, sessionID, workspace string, ramBlocks int, totalBlocks int) string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 80
	}
	if cols > 100 {
		cols = 100
	}

	if ramBlocks <= 0 {
		ramBlocks = 8
	}
	if totalBlocks <= 0 {
		totalBlocks = 10
	}
	if ramBlocks > totalBlocks {
		ramBlocks = totalBlocks
	}

	meter := "[" + strings.Repeat("■", ramBlocks) + strings.Repeat("□", totalBlocks-ramBlocks) + "]"

	// Line 1: Horns + Cyberdeck Title + Slashes
	titlePrefix := " ▲▲ // CYBERDECK: ZEROTWO // [FRANXX-OS v2.0] // ICE: BREACHED "
	slashCount := cols - len(titlePrefix)
	if slashCount < 2 {
		slashCount = 2
	}
	line1 := titlePrefix + strings.Repeat("/", slashCount)

	// Line 2: Roles + RAM Buffer
	line2 := fmt.Sprintf(" :: PISTIL: CODE:002 :: STAMEN: DARLING :: RAM BUFFER: %s %d/%d", meter, ramBlocks, totalBlocks)

	// Line 3: Telemetry & Model
	line3 := fmt.Sprintf(" :: NEURAL_SYNC: 99.8%% :: BUS: ARMv7-STATIC :: MODEL: %s", model)

	// Line 4: Double rule border
	line4 := strings.Repeat("═", cols)

	return fmt.Sprintf("\n%s\n%s\n%s\n%s\n", line1, line2, line3, line4)
}

// NeuralLinkEngagement renders the quick neural resonance banner before actions.
func NeuralLinkEngagement(actionDesc string) string {
	var sb strings.Builder
	sb.WriteString("\n \033[1m▲▲ BREACH_PROTOCOL\033[0m \033[90m::\033[0m neural resonance established\n")
	if actionDesc != "" {
		sb.WriteString(fmt.Sprintf(" \033[90m::\033[0m %s\n", actionDesc))
	}
	return sb.String()
}

// QuickhackCard renders an autonomous tool action card with cyberdeck styling.
func QuickhackCard(toolName, cmdOrArgs, result string) string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 80
	}
	if cols > 90 {
		cols = 90
	}

	innerW := cols - 4
	if innerW < 30 {
		innerW = 30
	}

	headerText := fmt.Sprintf("▲▲ UPLOAD: QUICKHACK [%s]", toolName)
	barLen := cols - len(headerText) - 6
	if barLen < 3 {
		barLen = 3
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n ┌─ \033[1m%s\033[0m %s┐\n", headerText, strings.Repeat("─", barLen)))

	// Clean inputs
	cmdLines := strings.Split(strings.TrimSpace(cmdOrArgs), "\n")
	for _, l := range cmdLines {
		if len(l) > innerW {
			l = l[:innerW-3] + "..."
		}
		sb.WriteString(fmt.Sprintf(" │ \033[90m$\033[0m %s\n", l))
	}

	if result != "" {
		resLines := strings.Split(strings.TrimSpace(result), "\n")
		// Limit preview to 4 lines
		maxRes := 4
		if len(resLines) < maxRes {
			maxRes = len(resLines)
		}
		for i := 0; i < maxRes; i++ {
			l := resLines[i]
			if len(l) > innerW-3 {
				l = l[:innerW-6] + "..."
			}
			if i == 0 {
				sb.WriteString(fmt.Sprintf(" │ \033[1m➔\033[0m %s\n", l))
			} else {
				sb.WriteString(fmt.Sprintf(" │   %s\n", l))
			}
		}
		if len(resLines) > maxRes {
			sb.WriteString(fmt.Sprintf(" │   \033[90m[+%d more lines]\033[0m\n", len(resLines)-maxRes))
		}
	}

	sb.WriteString(fmt.Sprintf(" └%s┘\n", strings.Repeat("─", cols-3)))
	return sb.String()
}

// CyberdeckDock renders the interactive command bar at the bottom.
func CyberdeckDock() string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 80
	}
	if cols > 100 {
		cols = 100
	}

	slashLine := strings.Repeat("/", cols)
	bar := " [DECK:ACTIVE]  ⏎ BREACH   ? CYBERWARE   c MEM_SHARDS   x DATA_DUMP   q JACK_OUT"
	prompt := "\n \033[1;7m 002:darling \033[0m › "

	return fmt.Sprintf("\n%s\n%s\n%s\n%s", slashLine, bar, slashLine, prompt)
}

// DestructiveCyberwareWarning renders the high-contrast lethal ICE alert.
func DestructiveCyberwareWarning(action, target, hazard string) string {
	cols := GetTerminalCols()
	if cols < 50 {
		cols = 70
	}
	if cols > 80 {
		cols = 80
	}

	w := cols - 4

	var sb strings.Builder
	sb.WriteString("\n\033[1;7m")
	sb.WriteString(" ╔" + strings.Repeat("═", w) + "╗\n")
	sb.WriteString(" ║ ▲▲ [DARLING, WAIT! CYBERWARE OVERHEAT // LETHAL ICE DETECTED] " + strings.Repeat(" ", w-64) + "║\n")
	sb.WriteString(" ╠" + strings.Repeat("═", w) + "╣\n")
	sb.WriteString(fmt.Sprintf(" ║ Quickhack: %-55s ║\n", truncateStr(action, 55)))
	sb.WriteString(fmt.Sprintf(" ║ Target:    %-55s ║\n", truncateStr(target, 55)))
	sb.WriteString(fmt.Sprintf(" ║ Danger:    %-55s ║\n", truncateStr(hazard, 55)))
	sb.WriteString(" ╚" + strings.Repeat("═", w) + "╝\033[0m\n\n")
	sb.WriteString(" \033[1mZero Two\033[0m: \"I won't let you brick our deck, Darling.\n")
	sb.WriteString("            If you're dead set on this run, type '\033[1mdarling\033[0m' to bypass ICE: \" ")
	return sb.String()
}

// MarkdownToTerm converts markdown formatting into crisp ANSI styling for terminals.
func MarkdownToTerm(content string) string {
	// 1. Headers: # Header -> Bold uppercase
	headerRe := regexp.MustCompile(`(?m)^#{1,6}\s+(.*)$`)
	content = headerRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := headerRe.FindStringSubmatch(m)
		if len(sub) > 1 {
			return "\n\033[1m" + strings.ToUpper(sub[1]) + "\033[0m"
		}
		return m
	})

	// 2. Bold: **text** or __text__ -> ANSI Bold
	content = regexp.MustCompile(`\*\*(.*?)\*\*`).ReplaceAllString(content, "\033[1m$1\033[0m")
	content = regexp.MustCompile(`__(.*?)__`).ReplaceAllString(content, "\033[1m$1\033[0m")

	// 3. Italic: *text* or _text_ -> ANSI Italic
	content = regexp.MustCompile(`\*([^\*\n]+)\*`).ReplaceAllString(content, "\033[3m$1\033[0m")
	content = regexp.MustCompile(`_([^_\n]+)_`).ReplaceAllString(content, "\033[3m$1\033[0m")

	// 4. Blockquotes: > quote -> dim italic
	quoteRe := regexp.MustCompile(`(?m)^(>+)\s*(.*)$`)
	content = quoteRe.ReplaceAllString(content, "\033[90;3m│ $2\033[0m")

	// 5. Inline Code: `code` -> Inverted / Bold highlight
	content = regexp.MustCompile("`([^`\n]+)`").ReplaceAllString(content, "\033[7m $1 \033[0m")

	// 6. Markdown pipe tables alignment
	content = AlignMarkdownTables(content)

	return content
}

// AlignMarkdownTables parses and aligns markdown tables to fit terminal columns.
func AlignMarkdownTables(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	var tableBuffer []string

	flushTable := func() {
		if len(tableBuffer) == 0 {
			return
		}
		formatted := formatTableLines(tableBuffer)
		result = append(result, formatted...)
		tableBuffer = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			tableBuffer = append(tableBuffer, trimmed)
		} else {
			flushTable()
			result = append(result, line)
		}
	}
	flushTable()

	return strings.Join(result, "\n")
}

func formatTableLines(lines []string) []string {
	if len(lines) < 2 {
		return lines
	}

	var rows [][]string
	for _, l := range lines {
		cells := strings.Split(strings.Trim(l, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, cells)
	}

	// Calculate max column counts and widths
	numCols := 0
	for _, r := range rows {
		if len(r) > numCols {
			numCols = len(r)
		}
	}
	if numCols == 0 {
		return lines
	}

	colWidths := make([]int, numCols)
	for _, r := range rows {
		for i, c := range r {
			w := len(c)
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	// Prevent excessive width
	for i := range colWidths {
		if colWidths[i] < 3 {
			colWidths[i] = 3
		}
		if colWidths[i] > 30 {
			colWidths[i] = 30
		}
	}

	var formatted []string
	for rowIdx, r := range rows {
		// Detect separator row
		isSep := true
		for _, c := range r {
			if !strings.HasPrefix(c, "-") && !strings.HasPrefix(c, ":-") {
				isSep = false
				break
			}
		}

		if isSep {
			var parts []string
			for i := 0; i < numCols; i++ {
				parts = append(parts, strings.Repeat("─", colWidths[i]+2))
			}
			formatted = append(formatted, " ├"+strings.Join(parts, "┼")+"┤")
			continue
		}

		var cellsFormatted []string
		for i := 0; i < numCols; i++ {
			val := ""
			if i < len(r) {
				val = r[i]
			}
			if len(val) > colWidths[i] {
				val = val[:colWidths[i]-1] + "…"
			}
			pad := colWidths[i] - len(val)
			if pad < 0 {
				pad = 0
			}
			if rowIdx == 0 {
				cellsFormatted = append(cellsFormatted, fmt.Sprintf(" \033[1m%s\033[0m%s ", val, strings.Repeat(" ", pad)))
			} else {
				cellsFormatted = append(cellsFormatted, fmt.Sprintf(" %s%s ", val, strings.Repeat(" ", pad)))
			}
		}
		formatted = append(formatted, " │"+strings.Join(cellsFormatted, "│")+"│")
	}

	return formatted
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
