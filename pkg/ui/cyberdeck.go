package ui

import (
	"fmt"
	"regexp"
	"strings"
)

// CyberdeckBanner renders the ultra-compact high-contrast Strelizia / Netrunner HUD header.
func CyberdeckBanner(model, sessionID, workspace string, ramBlocks int, totalBlocks int) string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 40
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

	var line1 string
	if cols >= 90 {
		line1 = fmt.Sprintf(" ▲▲ CYBERDECK: ZEROTWO [FRANXX-OS] :: PISTIL: CODE:002 :: RAM %s :: %s", meter, truncateStr(model, cols-65))
	} else if cols >= 75 {
		cRam := (ramBlocks * 5) / totalBlocks
		if cRam > 5 {
			cRam = 5
		}
		cMeter := "[" + strings.Repeat("■", cRam) + strings.Repeat("□", 5-cRam) + "]"
		line1 = fmt.Sprintf(" ▲▲ CYBERDECK: ZEROTWO :: PISTIL: CODE:002 :: RAM %s :: %s", cMeter, truncateStr(model, cols-60))
	} else if cols >= 58 {
		// Breakpoint 2: Kindle Portrait (58-74 cols)
		cRam := (ramBlocks * 4) / totalBlocks
		if cRam > 4 {
			cRam = 4
		}
		cMeter := "[" + strings.Repeat("■", cRam) + strings.Repeat("□", 4-cRam) + "]"
		line1 = fmt.Sprintf(" ▲▲ ZEROTWO [002] :: RAM %s :: %s", cMeter, truncateStr(model, cols-32))
	} else {
		// Breakpoint 3: Kindle Narrow / Keyboard open (< 58 cols)
		cRam := (ramBlocks * 3) / totalBlocks
		if cRam > 3 {
			cRam = 3
		}
		cMeter := "[" + strings.Repeat("■", cRam) + strings.Repeat("□", 3-cRam) + "]"
		line1 = fmt.Sprintf(" ▲▲ ZEROTWO :: 002 :: %s", cMeter)
	}

	line1Runes := []rune(line1)
	if len(line1Runes) > cols {
		line1 = string(line1Runes[:cols])
	}

	line2 := strings.Repeat("─", cols)
	return fmt.Sprintf("%s\n%s\n", line1, line2)
}

// NeuralLinkEngagement renders the quick neural resonance banner before actions.
func NeuralLinkEngagement(actionDesc string) string {
	if actionDesc == "" {
		return " ▲▲ BREACH_PROTOCOL :: neural resonance established\n"
	}
	return fmt.Sprintf(" ▲▲ BREACH_PROTOCOL :: %s\n", actionDesc)
}

// QuickhackCard renders an autonomous tool action card with compact cyberdeck styling.
func QuickhackCard(toolName, cmdOrArgs, result string) string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 40
	}
	if cols > 80 {
		cols = 80
	}

	innerW := cols - 4
	if innerW < 26 {
		innerW = 26
	}

	cleanCmd := strings.ReplaceAll(strings.TrimSpace(cmdOrArgs), "\n", " ")
	maxCmd := innerW - len(toolName) - 16
	if maxCmd < 8 {
		maxCmd = 8
	}
	if len(cleanCmd) > maxCmd {
		cleanCmd = truncateStr(cleanCmd, maxCmd)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(" ┌ ▲▲ QUICKHACK [%s] $ %s\n", toolName, cleanCmd))

	if result != "" {
		resLines := strings.Split(strings.TrimSpace(result), "\n")
		firstLine := strings.TrimSpace(resLines[0])
		if len(firstLine) > innerW-6 {
			firstLine = truncateStr(firstLine, innerW-6)
		}
		if len(resLines) > 1 {
			sb.WriteString(fmt.Sprintf(" └ ➔ %s \033[90m[+%d lines]\033[0m\n", firstLine, len(resLines)-1))
		} else {
			sb.WriteString(fmt.Sprintf(" └ ➔ %s\n", firstLine))
		}
	} else {
		sb.WriteString(fmt.Sprintf(" └%s\n", strings.Repeat("─", cols-3)))
	}
	return sb.String()
}

// CyberdeckDock renders the compact interactive command bar at the bottom.
func CyberdeckDock() string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 40
	}
	if cols > 100 {
		cols = 100
	}

	var dockBar string
	if cols >= 78 {
		dockBar = " [DECK:ACTIVE] ⏎ BREACH  ? CYBERWARE  c MEM_SHARDS  x DATA_DUMP  q JACK_OUT "
	} else if cols >= 58 {
		dockBar = " [DECK] ⏎ run  ? menu  c shards  x dump  q quit "
	} else {
		dockBar = " [DECK] ⏎ ENTER  ? HELP  c SHARDS  q QUIT "
	}

	dockWidth := len([]rune(dockBar))
	rulePad := cols - dockWidth - 2
	if rulePad < 1 {
		rulePad = 1
	}
	line1 := fmt.Sprintf("──%s%s", dockBar, strings.Repeat("─", rulePad))
	line1Runes := []rune(line1)
	if len(line1Runes) > cols {
		line1 = string(line1Runes[:cols])
	}
	prompt := "\033[1;7m 002:darling \033[0m › "

	return fmt.Sprintf("%s\n%s", line1, prompt)
}

// DestructiveCyberwareWarning renders the high-contrast lethal ICE alert.
func DestructiveCyberwareWarning(action, target, hazard string) string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 40
	}
	if cols > 80 {
		cols = 80
	}

	w := cols - 4
	if w < 36 {
		w = 36
	}

	title := "▲▲ [DARLING, WAIT! CYBERWARE OVERHEAT // LETHAL ICE]"
	if w < 54 {
		title = "▲▲ [DARLING, WAIT! LETHAL ICE DETECTED]"
	}
	if len([]rune(title)) > w-2 {
		title = truncateStr(title, w-2)
	}
	titleLen := len([]rune(title))
	titlePad := w - 2 - titleLen
	if titlePad < 0 {
		titlePad = 0
	}

	formatRow := func(label, val string) string {
		labelLen := len([]rune(label))
		maxVal := w - 2 - labelLen
		if maxVal < 5 {
			maxVal = 5
		}
		tVal := truncateStr(val, maxVal)
		pad := w - 2 - labelLen - len([]rune(tVal))
		if pad < 0 {
			pad = 0
		}
		return fmt.Sprintf(" ║ %s%s%s ║\n", label, tVal, strings.Repeat(" ", pad))
	}

	var sb strings.Builder
	sb.WriteString("\n\033[1;7m")
	sb.WriteString(" ╔" + strings.Repeat("═", w) + "╗\n")
	sb.WriteString(fmt.Sprintf(" ║ %s%s ║\n", title, strings.Repeat(" ", titlePad)))
	sb.WriteString(" ╠" + strings.Repeat("═", w) + "╣\n")
	sb.WriteString(formatRow("Quickhack: ", action))
	sb.WriteString(formatRow("Target:    ", target))
	sb.WriteString(formatRow("Danger:    ", hazard))
	sb.WriteString(" ╚" + strings.Repeat("═", w) + "╝\033[0m\n")
	sb.WriteString(" Zero Two: \"I won't let you brick our deck, Darling. Type '\033[1mdarling\033[0m' to bypass: \" ")
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

	// Prevent excessive width per column
	for i := range colWidths {
		if colWidths[i] < 3 {
			colWidths[i] = 3
		}
		if colWidths[i] > 30 {
			colWidths[i] = 30
		}
	}

	// Clamp total table width to terminal width budget so it never wraps on Kindle screen
	termCols := GetTerminalCols()
	maxTableW := termCols - 4
	if maxTableW < 36 {
		maxTableW = 36
	}

	// Calculate total width with borders: sum(colWidths + 3) + 1
	totalW := 1
	for _, w := range colWidths {
		totalW += w + 3
	}

	if totalW > maxTableW {
		excess := totalW - maxTableW
		for excess > 0 {
			maxIdx := -1
			maxW := 3
			for i, w := range colWidths {
				if w > maxW {
					maxW = w
					maxIdx = i
				}
			}
			if maxIdx == -1 {
				break
			}
			colWidths[maxIdx]--
			excess--
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
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}
