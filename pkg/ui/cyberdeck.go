package ui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// CyberdeckBanner renders a minimal, high-contrast Strelizia / Netrunner HUD header.
func CyberdeckBanner(model, sessionID, workspace string, ramBlocks int, totalBlocks int) string {
	if CurrentTheme != ThemeMinimal {
		return RenderThemeBanner(model, sessionID, workspace, ramBlocks, totalBlocks)
	}
	cols := GetTerminalCols()
	targetW := cols - 2
	if targetW < 36 {
		targetW = 36
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

	displayModel := model
	if idx := strings.LastIndex(displayModel, "/"); idx != -1 && cols < 80 {
		displayModel = displayModel[idx+1:]
	}

	var line1 string
	if cols >= 75 {
		// Breakpoint 1: Standard Desktop / Wide terminal
		line1 = fmt.Sprintf("❖ CYBERDECK: ZEROTWO · PISTIL: CODE:002 · RAM: %s · %s", meter, truncateStr(model, targetW-58))
	} else if cols >= 58 {
		// Breakpoint 2: Kindle Portrait (58-74 cols)
		line1 = fmt.Sprintf("❖ CYBERDECK: ZEROTWO · PISTIL: 002 · RAM: %s · %s", meter, truncateStr(displayModel, targetW-50))
	} else {
		// Breakpoint 3: Kindle Narrow / Keyboard open (< 58 cols)
		cRamNarrow := (ramBlocks * 3) / totalBlocks
		if cRamNarrow > 3 {
			cRamNarrow = 3
		}
		meterNarrow := strings.Repeat("▰", cRamNarrow) + strings.Repeat("▱", 3-cRamNarrow)
		line1 = fmt.Sprintf("❖ ZEROTWO // 002 · %s · %s", meterNarrow, truncateStr(displayModel, targetW-24))
	}

	line1Runes := []rune(line1)
	if len(line1Runes) > targetW {
		line1 = string(line1Runes[:targetW])
	}

	line2 := strings.Repeat("─", targetW)
	return fmt.Sprintf("%s\n%s\n", line1, line2)
}

// NeuralLinkEngagement renders the quick neural resonance banner before actions.
func NeuralLinkEngagement(actionDesc string) string {
	cols := GetTerminalCols()
	targetW := cols - 2
	if targetW < 36 {
		targetW = 36
	}

	badge := "── ❖ LINK:ACTIVE "
	msg := "quickhack engaged"
	if cols >= 58 {
		badge = "── ❖ BREACH_PROTOCOL // CODE:002 "
		if actionDesc != "" {
			msg = actionDesc
		} else {
			msg = "neural resonance established"
		}
	} else if actionDesc != "" {
		msg = actionDesc
	}

	avail := targetW - len([]rune(badge)) - 4
	if len([]rune(msg)) > avail {
		msg = truncateStr(msg, avail)
	}
	pad := targetW - len([]rune(badge)) - len([]rune(msg)) - 2
	if pad < 2 {
		pad = 2
	}
	return fmt.Sprintf("%s%s %s\n", badge, msg, strings.Repeat("─", pad))
}

// AssistantHeader renders the aesthetic Cyberdeck speaker header for Zero Two.
func AssistantHeader() string {
	if CurrentTheme != ThemeMinimal {
		return RenderThemeAssistantHeader()
	}
	cols := GetTerminalCols()
	targetW := cols - 2
	if targetW < 36 {
		targetW = 36
	}

	var badge string
	if cols >= 62 {
		badge = "── ❖ ZERO TWO // CODE:002 "
	} else {
		badge = "── ❖ ZERO TWO "
	}

	pad := targetW - len([]rune(badge))
	if pad < 2 {
		pad = 2
	}
	return fmt.Sprintf("%s%s\n", badge, strings.Repeat("─", pad))
}

// QuickhackCard renders an autonomous tool action card with aesthetic cyberdeck styling.
func QuickhackCard(toolName, cmdOrArgs, result string) string {
	if CurrentTheme != ThemeMinimal {
		return RenderThemeQuickhackCard(toolName, cmdOrArgs, result)
	}
	cols := GetTerminalCols()
	targetW := cols - 2
	if targetW < 36 {
		targetW = 36
	}

	cleanCmd := strings.ReplaceAll(strings.TrimSpace(cmdOrArgs), "\n", " ")
	desc := ""
	if cleanCmd != "" && cleanCmd != "{}" {
		var argMap map[string]interface{}
		if err := json.Unmarshal([]byte(cleanCmd), &argMap); err == nil {
			if c, ok := argMap["command"].(string); ok && c != "" {
				desc = "$ " + c
			} else if p, ok := argMap["path"].(string); ok && p != "" {
				desc = p
			} else if u, ok := argMap["url"].(string); ok && u != "" {
				desc = u
			} else if q, ok := argMap["query"].(string); ok && q != "" {
				desc = q
			}
		}
		if desc == "" {
			desc = cleanCmd
		}
	}

	var label string
	if desc != "" {
		label = fmt.Sprintf("── ⚡ QUICKHACK [%s] %s ", toolName, desc)
	} else {
		label = fmt.Sprintf("── ⚡ QUICKHACK [%s] ", toolName)
	}

	maxLabelLen := targetW - 4
	if len([]rune(label)) > maxLabelLen {
		label = truncateStr(label, maxLabelLen) + " "
	}

	padTop := targetW - len([]rune(label))
	if padTop < 2 {
		padTop = 2
	}
	topLine := label + strings.Repeat("─", padTop)

	var botLine string
	if result != "" {
		resLines := strings.Split(strings.TrimSpace(result), "\n")
		firstLine := strings.TrimSpace(resLines[0])
		firstLine = strings.TrimPrefix(firstLine, "• ")
		firstLine = strings.TrimPrefix(firstLine, "- ")
		firstLine = strings.TrimPrefix(firstLine, "* ")
		firstLine = strings.TrimSpace(firstLine)

		moreInfo := ""
		if len(resLines) > 1 {
			moreInfo = fmt.Sprintf(" [+%d lines]", len(resLines)-1)
		}
		availRes := targetW - len([]rune("  › ")) - len([]rune(moreInfo))
		if availRes < 8 {
			availRes = 8
		}
		tRes := truncateStr(firstLine, availRes)
		botLine = fmt.Sprintf("  › %s%s", tRes, moreInfo)
	} else {
		botLine = "  › executing..."
	}

	clamp := func(line string) string {
		r := []rune(line)
		if len(r) > targetW {
			return string(r[:targetW])
		}
		return line
	}

	return fmt.Sprintf("%s\n%s\n", clamp(topLine), clamp(botLine))
}

// CyberdeckDock renders the aesthetic cyberdeck bezel and interactive command bar.
func CyberdeckDock() string {
	if CurrentTheme != ThemeMinimal {
		return RenderThemeDock()
	}
	cols := GetTerminalCols()
	targetW := cols - 2
	if targetW < 36 {
		targetW = 36
	}

	var leftBadge, rightMenu string
	if cols >= 80 {
		leftBadge = "── ❖ CYBERDECK:ACTIVE // CODE:002 "
		rightMenu = " ? MENU · c SHARDS · x DUMP · q QUIT ──"
	} else if cols >= 62 {
		leftBadge = "── ❖ DECK:ACTIVE "
		rightMenu = " ? MENU · c SHARDS · q QUIT ──"
	} else if cols >= 44 {
		leftBadge = "── ❖ DECK:ACTIVE "
		rightMenu = " ? HELP · q QUIT ──"
	} else {
		leftBadge = "── ❖ 002 "
		rightMenu = " ? HELP ──"
	}

	topUsed := len([]rune(leftBadge)) + len([]rune(rightMenu))
	pad := targetW - topUsed
	if pad < 1 {
		pad = 1
	}
	topLine := leftBadge + strings.Repeat("─", pad) + rightMenu

	r := []rune(topLine)
	if len(r) > targetW {
		topLine = string(r[:targetW])
	}

	prompt := "002:darling › "
	return fmt.Sprintf("\r%s\n%s", topLine, prompt)
}

// ToolAuthCard renders an aesthetic Cyberdeck authorization HUD card for interactive confirmation.
func ToolAuthCard(action, target, scope, warning string) string {
	cols := GetTerminalCols()
	targetW := cols - 2
	if targetW < 36 {
		targetW = 36
	}

	innerW := targetW - 4 // between │ and │
	if innerW < 26 {
		innerW = 26
	}

	titleBadge := "┌──[ ❖ NEURAL AUTHORIZATION ]"
	padTop := targetW - len([]rune(titleBadge)) - 1
	if padTop < 1 {
		padTop = 1
	}
	topLine := titleBadge + strings.Repeat("─", padTop) + "┐\n"

	formatLine := func(label, val string) string {
		cleanVal := strings.ReplaceAll(strings.TrimSpace(val), "\n", " ")
		maxVal := innerW - len([]rune(label))
		if maxVal < 6 {
			maxVal = 6
		}
		tVal := truncateStr(cleanVal, maxVal)
		pad := innerW - len([]rune(label)) - len([]rune(tVal))
		if pad < 0 {
			pad = 0
		}
		return fmt.Sprintf("│ %s%s%s │\n", label, tVal, strings.Repeat(" ", pad))
	}

	var sb strings.Builder
	sb.WriteString("\r\n")
	sb.WriteString(topLine)
	if action != "" {
		sb.WriteString(formatLine("Quickhack: ", action))
	}
	if target != "" {
		sb.WriteString(formatLine("Target:    ", target))
	}
	if scope != "" {
		sb.WriteString(formatLine("Scope:     ", scope))
	}
	if warning != "" {
		sb.WriteString(formatLine("Notice:    ", warning))
	}
	botLine := "└──" + strings.Repeat("─", targetW-4) + "┘\n"
	sb.WriteString(botLine)
	sb.WriteString(" Confirm execution? [y/N]: ")
	return sb.String()
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

	// 5. Fenced Code Blocks: ```lang ... ``` -> Boxed card with lang badge
	codeBlockRe := regexp.MustCompile("(?s)```(\\w*)\\n?(.*?)```")
	content = codeBlockRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := codeBlockRe.FindStringSubmatch(m)
		lang := ""
		code := ""
		if len(sub) > 1 {
			lang = strings.TrimSpace(sub[1])
		}
		if len(sub) > 2 {
			code = sub[2]
		}
		return formatCodeBlock(lang, code)
	})

	// 6. Inline Code: `code` -> Inverted / Bold highlight
	content = regexp.MustCompile("`([^`\n]+)`").ReplaceAllString(content, "\033[7m $1 \033[0m")

	// 7. Markdown pipe tables alignment
	content = AlignMarkdownTables(content)

	cols := GetTerminalCols()
	targetW := cols - 1
	if targetW < 36 {
		targetW = 36
	}
	content = WrapText(content, targetW)

	return content
}

func formatCodeBlock(lang, code string) string {
	cols := GetTerminalCols()
	targetW := cols - 2
	if targetW < 36 {
		targetW = 36
	}

	badge := "┌──[ CODE ]"
	if lang != "" {
		badge = fmt.Sprintf("┌──[ CODE: %s ]", strings.ToUpper(lang))
	}
	padTop := targetW - len([]rune(badge)) - 1
	if padTop < 1 {
		padTop = 1
	}
	topLine := badge + strings.Repeat("─", padTop) + "┐"
	botLine := "└──" + strings.Repeat("─", targetW-4) + "┘"

	innerW := targetW - 4
	if innerW < 26 {
		innerW = 26
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(topLine)
	sb.WriteString("\n")

	lines := strings.Split(strings.TrimRight(code, "\n"), "\n")
	for _, l := range lines {
		expanded := strings.ReplaceAll(l, "\t", "    ")
		r := []rune(expanded)
		if len(r) > innerW {
			expanded = string(r[:innerW-1]) + "…"
		}
		pad := innerW - len([]rune(expanded))
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(fmt.Sprintf("│ %s%s │\n", expanded, strings.Repeat(" ", pad)))
	}
	sb.WriteString(botLine)
	sb.WriteString("\n")
	return sb.String()
}

// WrapText wraps words in text to fit within maxCols, ignoring ANSI color escapes for length calculation.
func WrapText(text string, maxCols int) string {
	if maxCols <= 20 {
		return text
	}

	lines := strings.Split(text, "\n")
	var result []string

	ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Don't re-wrap table borders, box drawing borders, or headers
		if strings.HasPrefix(trimmed, "│") || strings.HasPrefix(trimmed, "┌") ||
			strings.HasPrefix(trimmed, "└") || strings.HasPrefix(trimmed, "├") ||
			strings.HasPrefix(trimmed, "─") || strings.HasPrefix(trimmed, "╔") ||
			strings.HasPrefix(trimmed, "╚") || strings.HasPrefix(trimmed, "║") ||
			strings.HasPrefix(trimmed, "╠") || strings.HasPrefix(trimmed, "╭") ||
			strings.HasPrefix(trimmed, "╰") || strings.HasPrefix(trimmed, "❖") {
			result = append(result, line)
			continue
		}

		words := strings.Fields(line)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}

		var curLine strings.Builder
		curLen := 0

		for _, w := range words {
			wLen := len([]rune(ansiRe.ReplaceAllString(w, "")))
			if curLen == 0 {
				curLine.WriteString(w)
				curLen = wLen
			} else if curLen+1+wLen <= maxCols {
				curLine.WriteString(" ")
				curLine.WriteString(w)
				curLen += 1 + wLen
			} else {
				result = append(result, curLine.String())
				curLine.Reset()
				curLine.WriteString(w)
				curLen = wLen
			}
		}
		if curLine.Len() > 0 {
			result = append(result, curLine.String())
		}
	}

	return strings.Join(result, "\n")
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
