package ui

import (
	"fmt"
	"regexp"
	"strings"
)

// CyberdeckBanner renders the aesthetic high-contrast Strelizia / Netrunner HUD header.
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

	cRam := (ramBlocks * 5) / totalBlocks
	if cRam > 5 {
		cRam = 5
	}
	meter := strings.Repeat("▰", cRam) + strings.Repeat("▱", 5-cRam)

	var titleBadge, rightBadge, midContent, botBadge string
	if cols >= 80 {
		titleBadge = " ╔═[ ◢◤ CYBERDECK: ZEROTWO // STRELIZIA ]"
		rightBadge = "[ ICE: BREACHED ]═╗"
		botBadge = "[ FRANXX-OS // SYNC: 99.8% ]══╝"
		midContent = fmt.Sprintf(" ║ ◈ PISTIL: CODE:002  ◈ RAM: %s  ◈ CORE: %s", meter, truncateStr(model, cols-48))
	} else if cols >= 58 {
		// Kindle Portrait (58-79 cols)
		titleBadge = " ╔═[ ◢◤ CYBERDECK: ZEROTWO ]"
		rightBadge = "[ 002 ]═╗"
		botBadge = "[ SYNC: 99.8% ]══╝"
		midContent = fmt.Sprintf(" ║ ◈ PISTIL: CODE:002 │ RAM: %s │ %s", meter, truncateStr(model, cols-40))
	} else {
		// Kindle Narrow / Keyboard open (< 58 cols)
		titleBadge = " ╔═[ 002:CYBERDECK ]"
		rightBadge = "[ OK ]═╗"
		botBadge = "[ 99.8% ]══╝"
		midContent = fmt.Sprintf(" ║ ◈ 002 │ %s │ %s", meter, truncateStr(model, cols-24))
	}

	topUsed := len([]rune(titleBadge)) + len([]rune(rightBadge))
	topPad := cols - topUsed
	if topPad < 1 {
		topPad = 1
	}
	topLine := titleBadge + strings.Repeat("═", topPad) + rightBadge

	midUsed := len([]rune(midContent))
	midPad := cols - midUsed - 1
	if midPad < 1 {
		midPad = 1
	}
	midLine := midContent + strings.Repeat(" ", midPad) + "║"

	botUsed := len([]rune(botBadge))
	botPad := cols - botUsed - 2
	if botPad < 1 {
		botPad = 1
	}
	botLine := " ╚" + strings.Repeat("═", botPad) + botBadge

	clamp := func(line string) string {
		r := []rune(line)
		if len(r) > cols {
			return string(r[:cols])
		}
		return line
	}

	return fmt.Sprintf("%s\n%s\n%s\n", clamp(topLine), clamp(midLine), clamp(botLine))
}

// NeuralLinkEngagement renders the quick neural resonance banner before actions.
func NeuralLinkEngagement(actionDesc string) string {
	if actionDesc == "" {
		return " ◢◤ [ ❖ BREACH_PROTOCOL ] ── neural resonance established ───\n"
	}
	return fmt.Sprintf(" ◢◤ [ ❖ BREACH_PROTOCOL ] ── %s ───\n", actionDesc)
}

// QuickhackCard renders an autonomous tool action card with aesthetic cyberdeck styling.
func QuickhackCard(toolName, cmdOrArgs, result string) string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 40
	}
	if cols > 80 {
		cols = 80
	}

	badge := fmt.Sprintf(" ╭─[ ⚡ QUICKHACK [%s] ]", toolName)
	cleanCmd := strings.ReplaceAll(strings.TrimSpace(cmdOrArgs), "\n", " ")

	availCmd := cols - len([]rune(badge)) - 10
	if availCmd < 8 {
		availCmd = 8
	}
	tCmd := truncateStr(cleanCmd, availCmd)

	headerPart := fmt.Sprintf("%s ── $ %s ", badge, tCmd)
	topPad := cols - len([]rune(headerPart)) - 1
	if topPad < 1 {
		topPad = 1
	}
	topLine := headerPart + strings.Repeat("─", topPad) + "╮"

	var botLine string
	if result != "" {
		resLines := strings.Split(strings.TrimSpace(result), "\n")
		firstLine := strings.TrimSpace(resLines[0])
		moreInfo := ""
		if len(resLines) > 1 {
			moreInfo = fmt.Sprintf(" [+%d lines]", len(resLines)-1)
		}
		availRes := cols - len([]rune(" ╰─➔ ")) - len([]rune(moreInfo)) - 4
		if availRes < 10 {
			availRes = 10
		}
		tRes := truncateStr(firstLine, availRes)
		botPart := fmt.Sprintf(" ╰─➔ %s%s ", tRes, moreInfo)
		botPad := cols - len([]rune(botPart)) - 1
		if botPad < 1 {
			botPad = 1
		}
		botLine = botPart + strings.Repeat("─", botPad) + "╯"
	} else {
		botLine = " ╰" + strings.Repeat("─", cols-3) + "╯"
	}

	clamp := func(line string) string {
		r := []rune(line)
		if len(r) > cols {
			return string(r[:cols])
		}
		return line
	}

	return fmt.Sprintf("%s\n%s\n", clamp(topLine), clamp(botLine))
}

// CyberdeckDock renders the aesthetic cyberdeck bezel and interactive command bar.
func CyberdeckDock() string {
	cols := GetTerminalCols()
	if cols < 40 {
		cols = 40
	}
	if cols > 100 {
		cols = 100
	}

	var leftBadge, rightMenu string
	if cols >= 80 {
		leftBadge = " ╭──[ ❖ CYBERDECK:ACTIVE // CODE:002 ]"
		rightMenu = "[ ⏎ RUN  ? MENU  c SHARDS  x DUMP  q QUIT ]──╮"
	} else if cols >= 62 {
		leftBadge = " ╭──[ ❖ DECK:ACTIVE ]"
		rightMenu = "[ ⏎ RUN  ? MENU  c SHARDS  q QUIT ]──╮"
	} else {
		leftBadge = " ╭──[ 002 ]"
		rightMenu = "[ ⏎ RUN  ? HELP  q QUIT ]──╮"
	}

	topUsed := len([]rune(leftBadge)) + len([]rune(rightMenu))
	pad := cols - topUsed
	if pad < 1 {
		pad = 1
	}
	topLine := leftBadge + strings.Repeat("─", pad) + rightMenu
	prompt := " ╰─❯ \033[1;7m 002:darling \033[0m › "

	r := []rune(topLine)
	if len(r) > cols {
		topLine = string(r[:cols])
	}

	return fmt.Sprintf("%s\n%s", topLine, prompt)
}

// DestructiveCyberwareWarning renders the aesthetic lethal ICE alert.
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
	if len([]rune(title)) > w-4 {
		title = truncateStr(title, w-4)
	}
	titleBadge := fmt.Sprintf(" ╔═[ %s ]", title)
	padTop := w - len([]rune(titleBadge)) + 2
	if padTop < 1 {
		padTop = 1
	}
	topLine := titleBadge + strings.Repeat("═", padTop) + "╗\n"

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
	sb.WriteString(topLine)
	sb.WriteString(formatRow("Quickhack: ", action))
	sb.WriteString(formatRow("Target:    ", target))
	sb.WriteString(formatRow("Danger:    ", hazard))
	sb.WriteString(" ╠" + strings.Repeat("═", w) + "╣\n")
	sb.WriteString(formatRow("Zero Two:  ", "\"I won't let you brick our deck, Darling.\""))
	sb.WriteString(" ╚" + strings.Repeat("═", w) + "╝\033[0m\n")
	sb.WriteString(" Type '\033[1mdarling\033[0m' to bypass ICE: ")
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
