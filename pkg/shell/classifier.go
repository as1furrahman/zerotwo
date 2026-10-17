package shell

import (
	"path/filepath"
	"strings"
	"unicode"
)

type Severity int

const (
	SeverityReadOnly Severity = iota
	SeverityGeneric
	SeverityDestructive
)

// TokenizingClassifier implements a default-deny shell safety classifier.
type TokenizingClassifier struct{}

func NewTokenizingClassifier() *TokenizingClassifier {
	return &TokenizingClassifier{}
}

// ClassifyCommand classifies a shell command using a new tokenizing classifier.
func ClassifyCommand(command string) (Severity, string) {
	return NewTokenizingClassifier().Classify(command)
}

// Classify parses the command pipeline/chain and returns the highest severity level found.
func (c *TokenizingClassifier) Classify(command string) (Severity, string) {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return SeverityReadOnly, ""
	}

	// 0. Fail-closed on unterminated quotes (syntax error and parser evasion)
	if hasUnbalancedQuotes(cmd) {
		return SeverityDestructive, "contains unterminated quote"
	}

	// 1. Check for immediate destructive-or-unknown shell constructs:
	// backticks, $(...), here-docs <<, process substitution <(), >(), eval, source
	if strings.Contains(cmd, "`") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "${") {
		return SeverityDestructive, "contains subshell or variable expansion"
	}
	if strings.Contains(cmd, "<<") {
		return SeverityDestructive, "contains here-doc"
	}
	if strings.Contains(cmd, "<(") || strings.Contains(cmd, ">(") {
		return SeverityDestructive, "contains process substitution"
	}

	// 2. Split into segments honoring quotes
	segments := splitCommandChain(cmd)
	if len(segments) == 0 {
		return SeverityReadOnly, ""
	}

	highestSev := SeverityReadOnly
	highestReason := ""

	for _, seg := range segments {
		sev, reason := c.classifySegment(seg)
		if sev > highestSev {
			highestSev = sev
			highestReason = reason
		}
		if highestSev == SeverityDestructive {
			break
		}
	}

	return highestSev, highestReason
}

func (c *TokenizingClassifier) classifySegment(segment string) (Severity, string) {
	seg := strings.TrimSpace(segment)
	if seg == "" {
		return SeverityReadOnly, ""
	}

	// Check write redirections: >, >>, &>, >|, 1>, 2>
	if hasWriteRedirection(seg) {
		return SeverityDestructive, "contains write redirection"
	}

	tokens := tokenizeWords(seg)
	if len(tokens) == 0 {
		return SeverityReadOnly, ""
	}

	tokens, hasPrivEsc, hasMutatingWrapper := stripExecutionWrappers(tokens)
	if len(tokens) == 0 {
		if hasPrivEsc {
			return SeverityDestructive, "privilege escalation wrapper without command"
		}
		if hasMutatingWrapper {
			return SeverityGeneric, "wrapper creates side-effect output"
		}
		return SeverityGeneric, "empty command after wrapper stripping"
	}

	baseCmd := normalizeBaseCommand(tokens[0])
	args := tokens[1:]

	// Check explicitly destructive patterns
	if isExplicitlyDestructive(baseCmd, args) {
		return SeverityDestructive, "destructive command pattern detected"
	}

	// Check if any argument accesses sensitive credentials or configuration paths
	for _, a := range args {
		if isSensitivePathOrSecret(a) {
			return SeverityGeneric, "command references sensitive path or credentials (" + unquoteToken(a) + ")"
		}
	}

	// Check read-only allowlist
	isRO, reason := isReadOnlyCommand(baseCmd, args)
	if !isRO {
		// If not read-only, default to Generic or Destructive
		if hasPrivEsc {
			return SeverityDestructive, "non-read-only command with privilege escalation (" + reason + ")"
		}
		return SeverityGeneric, reason
	}

	// If it was read-only but has sudo/doas/pkexec, raise severity by one level!
	if hasPrivEsc {
		return SeverityGeneric, "read-only command elevated with privilege wrapper"
	}

	// If it was read-only but wrapped in nohup (creates nohup.out), raise severity!
	if hasMutatingWrapper {
		return SeverityGeneric, "command wrapped with nohup (creates nohup.out)"
	}

	return SeverityReadOnly, ""
}

func stripExecutionWrappers(tokens []string) (remaining []string, hasPrivEsc bool, hasMutatingWrapper bool) {
	for len(tokens) > 0 {
		w := unquoteToken(tokens[0])

		// Privilege escalation wrappers: sudo, doas, pkexec
		if w == "sudo" || w == "doas" || w == "pkexec" {
			hasPrivEsc = true
			tokens = tokens[1:]
			for len(tokens) > 0 {
				tok := unquoteToken(tokens[0])
				if tok == "-u" || tok == "-g" || tok == "-p" || tok == "-h" {
					if len(tokens) > 1 {
						tokens = tokens[2:]
						continue
					}
					tokens = tokens[1:]
					continue
				}
				if strings.HasPrefix(tok, "-") {
					tokens = tokens[1:]
					continue
				}
				break
			}
			continue
		}

		// nohup (creates nohup.out)
		if w == "nohup" {
			hasMutatingWrapper = true
			tokens = tokens[1:]
			continue
		}

		// builtin, exec
		if w == "builtin" || w == "exec" {
			tokens = tokens[1:]
			continue
		}

		// command (e.g. command rm, command -p rm)
		if w == "command" {
			tokens = tokens[1:]
			for len(tokens) > 0 && strings.HasPrefix(unquoteToken(tokens[0]), "-") {
				tokens = tokens[1:]
			}
			continue
		}

		// time (e.g. time rm, time -p rm)
		if w == "time" {
			tokens = tokens[1:]
			for len(tokens) > 0 && strings.HasPrefix(unquoteToken(tokens[0]), "-") {
				tokens = tokens[1:]
			}
			continue
		}

		// nice (e.g. nice rm, nice -n 10 rm, nice -10 rm)
		if w == "nice" {
			tokens = tokens[1:]
			for len(tokens) > 0 {
				tok := unquoteToken(tokens[0])
				if tok == "-n" {
					if len(tokens) > 1 {
						tokens = tokens[2:]
						continue
					}
					tokens = tokens[1:]
					continue
				}
				if strings.HasPrefix(tok, "-") {
					tokens = tokens[1:]
					continue
				}
				break
			}
			continue
		}

		// stdbuf (e.g. stdbuf -o0 rm, stdbuf -i0 -o0 -eL rm)
		if w == "stdbuf" {
			tokens = tokens[1:]
			for len(tokens) > 0 {
				tok := unquoteToken(tokens[0])
				if tok == "-i" || tok == "-o" || tok == "-e" {
					if len(tokens) > 1 {
						tokens = tokens[2:]
						continue
					}
					tokens = tokens[1:]
					continue
				}
				if strings.HasPrefix(tok, "-i") || strings.HasPrefix(tok, "-o") || strings.HasPrefix(tok, "-e") {
					tokens = tokens[1:]
					continue
				}
				break
			}
			continue
		}

		// timeout (e.g. timeout 5 rm, timeout -k 10s 5s rm)
		if w == "timeout" {
			tokens = tokens[1:]
			for len(tokens) > 0 {
				tok := unquoteToken(tokens[0])
				if tok == "-k" || tok == "-s" || tok == "--kill-after" || tok == "--signal" {
					if len(tokens) > 1 {
						tokens = tokens[2:]
						continue
					}
					tokens = tokens[1:]
					continue
				}
				if strings.HasPrefix(tok, "-") {
					tokens = tokens[1:]
					continue
				}
				// The next non-flag token is duration (e.g. "5", "5s", "10m")
				tokens = tokens[1:]
				break
			}
			continue
		}

		// env (e.g. env rm x, env -i rm x, env FOO=1 rm x)
		if w == "env" {
			if len(tokens) == 1 {
				break
			}
			idx := 1
			for idx < len(tokens) {
				tok := unquoteToken(tokens[idx])
				if tok == "-u" {
					idx += 2
					continue
				}
				if strings.HasPrefix(tok, "-") {
					idx++
					continue
				}
				if isEnvAssignment(tok) {
					idx++
					continue
				}
				break
			}
			if idx < len(tokens) {
				hasMutatingWrapper = true // env invocation carries environment injection risk
				tokens = tokens[idx:]
				continue
			}
			break
		}

		// busybox (e.g. busybox ls, busybox rm)
		if w == "busybox" {
			tokens = tokens[1:]
			continue
		}

		// Leading environment assignments (e.g. FOO=1 rm x)
		if isEnvAssignment(w) {
			tokens = tokens[1:]
			continue
		}

		break
	}
	return tokens, hasPrivEsc, hasMutatingWrapper
}

func isExplicitlyDestructive(baseCmd string, args []string) bool {
	// Destructive or unknown execution wrappers
	if baseCmd == "eval" || baseCmd == "source" || baseCmd == "." || baseCmd == "xargs" || baseCmd == "sh" || baseCmd == "bash" || baseCmd == "zsh" {
		return true
	}

	// All file-destroying commands: rm (all forms), unlink, shred, truncate
	if baseCmd == "rm" || baseCmd == "unlink" || baseCmd == "shred" || baseCmd == "truncate" {
		return true
	}

	// sed in-place file mutation: sed -i, sed --in-place
	if baseCmd == "sed" {
		for _, a := range args {
			aClean := unquoteToken(a)
			if strings.HasPrefix(aClean, "-i") || strings.HasPrefix(aClean, "--in-place") {
				return true
			}
		}
	}

	// git destructive worktree/history operations: reset --hard/--merge, clean -f/-fd/-fx
	if baseCmd == "git" && len(args) > 0 {
		sub := unquoteToken(args[0])
		if sub == "reset" {
			for _, a := range args[1:] {
				if unquoteToken(a) == "--hard" || unquoteToken(a) == "--merge" {
					return true
				}
			}
		}
		if sub == "clean" {
			for _, a := range args[1:] {
				aClean := unquoteToken(a)
				if strings.HasPrefix(aClean, "-") && (strings.Contains(aClean, "f") || strings.Contains(aClean, "x")) {
					return true
				}
			}
		}
	}

	// crontab removal
	if baseCmd == "crontab" {
		for _, a := range args {
			aClean := unquoteToken(a)
			if aClean == "-r" || aClean == "--remove" {
				return true
			}
		}
	}

	// rsync destination deletion
	if baseCmd == "rsync" {
		for _, a := range args {
			aClean := unquoteToken(a)
			if strings.HasPrefix(aClean, "--delete") {
				return true
			}
		}
	}

	// Arbitrary code execution interpreters: python, perl, ruby, lua, node with inline script flag
	if baseCmd == "python" || baseCmd == "python3" || baseCmd == "python2" || baseCmd == "perl" || baseCmd == "ruby" || baseCmd == "lua" || baseCmd == "node" {
		for _, a := range args {
			aClean := unquoteToken(a)
			if aClean == "-c" || aClean == "-e" {
				return true
			}
		}
	}

	// awk arbitrary command execution or in-place edit
	if baseCmd == "awk" || baseCmd == "gawk" || baseCmd == "mawk" {
		for _, a := range args {
			aClean := unquoteToken(a)
			if strings.Contains(aClean, "system(") || strings.HasPrefix(aClean, "-i") {
				return true
			}
		}
	}

	// Process kill: kill, pkill, killall
	if baseCmd == "kill" || baseCmd == "pkill" || baseCmd == "killall" {
		return true
	}

	// find with -delete or -exec
	if baseCmd == "find" {
		for _, a := range args {
			aClean := unquoteToken(a)
			if aClean == "-delete" || aClean == "-exec" || aClean == "-execdir" || aClean == "-ok" {
				return true
			}
		}
	}

	// dd, mkfs, fdisk, parted, sfdisk
	if baseCmd == "dd" || strings.HasPrefix(baseCmd, "mkfs") || baseCmd == "fdisk" || baseCmd == "parted" || baseCmd == "sfdisk" {
		return true
	}

	// chmod -R, chown -R
	if baseCmd == "chmod" || baseCmd == "chown" || baseCmd == "chgrp" {
		for _, a := range args {
			aClean := unquoteToken(a)
			if strings.HasPrefix(aClean, "-") && (strings.Contains(aClean, "R") || aClean == "--recursive") {
				return true
			}
		}
	}

	// shutdown, reboot, poweroff, halt, init 0
	if baseCmd == "shutdown" || baseCmd == "reboot" || baseCmd == "poweroff" || baseCmd == "halt" {
		return true
	}
	if baseCmd == "init" && len(args) > 0 && unquoteToken(args[0]) == "0" {
		return true
	}

	// curl ... | sh, wget ... | sh is caught at pipeline level
	return false
}

func isReadOnlyCommand(baseCmd string, args []string) (bool, string) {
	switch baseCmd {
	case "ls":
		// Flag allowlist: -a, -l, -h, -t, -S, -r, -1, -F, -d, -R
		return validateFlags(args, map[rune]bool{
			'a': true, 'l': true, 'h': true, 't': true, 'S': true, 'r': true, '1': true, 'F': true, 'd': true, 'R': true,
		})
	case "cat":
		// Flag allowlist: -n, -b, -s, -v, -E, -T, -A
		return validateFlags(args, map[rune]bool{
			'n': true, 'b': true, 's': true, 'v': true, 'E': true, 'T': true, 'A': true,
		})
	case "head", "tail":
		// Flag allowlist: -n, -c, -q, -v, -f
		return validateFlags(args, map[rune]bool{
			'n': true, 'c': true, 'q': true, 'v': true, 'f': true,
		})
	case "grep", "egrep", "fgrep":
		// Flag allowlist: -i, -v, -c, -l, -L, -n, -r, -R, -E, -F, -w, -x, -q
		return validateFlags(args, map[rune]bool{
			'i': true, 'v': true, 'c': true, 'l': true, 'L': true, 'n': true, 'r': true, 'R': true, 'E': true, 'F': true, 'w': true, 'x': true, 'q': true,
		})
	case "find":
		// Reject -delete, -exec, -execdir, -ok, -fprint*, -fls
		for _, a := range args {
			aClean := unquoteToken(a)
			if aClean == "-delete" || aClean == "-exec" || aClean == "-execdir" || aClean == "-ok" ||
				strings.HasPrefix(aClean, "-fprint") || aClean == "-fls" {
				return false, "find with mutating action: " + aClean
			}
		}
		return true, ""
	case "wc":
		return validateFlags(args, map[rune]bool{'l': true, 'w': true, 'c': true, 'm': true, 'L': true})
	case "diff":
		return validateFlags(args, map[rune]bool{'u': true, 'c': true, 'r': true, 'q': true, 'i': true, 'w': true, 'b': true})
	case "stat":
		return validateFlags(args, map[rune]bool{'c': true, 'f': true, 'L': true, 't': true})
	case "file":
		return validateFlags(args, map[rune]bool{'b': true, 'i': true, 'L': true, 'z': true})
	case "df", "du":
		return validateFlags(args, map[rune]bool{'h': true, 'k': true, 'm': true, 's': true, 'a': true, 'd': true, 'x': true})
	case "free":
		return validateFlags(args, map[rune]bool{'h': true, 'b': true, 'k': true, 'm': true, 'g': true, 's': true, 'c': true, 't': true})
	case "uptime":
		return validateFlags(args, map[rune]bool{'p': true, 's': true})
	case "uname":
		return validateFlags(args, map[rune]bool{'a': true, 's': true, 'n': true, 'r': true, 'v': true, 'm': true, 'p': true, 'i': true, 'o': true})
	case "ps":
		// ps aux, -ef, -u, -p
		return true, ""
	case "top":
		// Must include -b (batch mode)
		hasBatch := false
		for _, a := range args {
			if strings.Contains(unquoteToken(a), "b") {
				hasBatch = true
				break
			}
		}
		if !hasBatch {
			return false, "top without batch mode flag -b"
		}
		return true, ""
	case "dmesg":
		// Never -c or -C
		for _, a := range args {
			aClean := unquoteToken(a)
			if strings.HasPrefix(aClean, "-") && (strings.Contains(aClean, "c") || strings.Contains(aClean, "C")) {
				return false, "dmesg with clearing flag (-c / -C)"
			}
		}
		return true, ""
	case "date":
		// Plain date, date +FORMAT, date -u, date -R. Never -s or --set
		for _, a := range args {
			aClean := unquoteToken(a)
			if aClean == "-s" || strings.HasPrefix(aClean, "--set") {
				return false, "date with clock-setting flag (-s / --set)"
			}
		}
		return true, ""
	case "echo", "pwd", "which":
		return true, ""
	case "ip":
		// Only show / list subcommands
		if len(args) == 0 {
			return true, ""
		}
		sub := unquoteToken(args[0])
		if sub == "link" || sub == "addr" || sub == "route" || sub == "rule" || sub == "neigh" {
			for _, a := range args[1:] {
				action := unquoteToken(a)
				if action == "set" || action == "add" || action == "del" || action == "delete" || action == "change" || action == "replace" {
					return false, "ip mutating action: " + action
				}
			}
		} else {
			return false, "unknown ip subcommand: " + sub
		}
		return true, ""
	case "ifconfig":
		// Bare ifconfig or ifconfig <interface> without IP assignment
		if len(args) > 1 {
			return false, "ifconfig with arguments may assign addresses"
		}
		return true, ""
	case "ss", "netstat":
		// Only list/stats flags
		return validateFlags(args, map[rune]bool{'t': true, 'u': true, 'l': true, 'p': true, 'n': true, 'a': true, 's': true, 'e': true, 'r': true})
	case "ping":
		// Needs -c flag, otherwise it pings forever
		hasCount := false
		for i, a := range args {
			aClean := unquoteToken(a)
			if aClean == "-c" && i+1 < len(args) {
				hasCount = true
				break
			}
			if strings.HasPrefix(aClean, "-c") && len(aClean) > 2 {
				hasCount = true
				break
			}
		}
		if !hasCount {
			return false, "ping without count flag -c"
		}
		return true, ""
	case "lsblk", "lscpu", "lsof":
		return true, ""
	default:
		return false, "command " + baseCmd + " not in read-only allowlist"
	}
}

func validateFlags(args []string, allowed map[rune]bool) (bool, string) {
	for _, a := range args {
		aClean := unquoteToken(a)
		if strings.HasPrefix(aClean, "-") && !strings.HasPrefix(aClean, "--") {
			for _, r := range aClean[1:] {
				if unicode.IsDigit(r) {
					continue
				}
				if !allowed[r] {
					return false, "flag -" + string(r) + " not allowlisted"
				}
			}
		}
	}
	return true, ""
}

func hasUnbalancedQuotes(cmd string) bool {
	inQuote := rune(0)
	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			inQuote = r
			continue
		}
	}
	return inQuote != 0
}

func hasWriteRedirection(seg string) bool {
	inQuote := rune(0)
	runes := []rune(seg)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			inQuote = r
			continue
		}
		if r == '>' {
			return true
		}
	}
	return false
}

func splitCommandChain(cmd string) []string {
	var segments []string
	var current strings.Builder
	runes := []rune(cmd)
	inQuote := rune(0)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Quote handling
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			}
			current.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			inQuote = r
			current.WriteRune(r)
			continue
		}

		// Split on separators: &&, ||, |, ;, \n, &
		if r == '\n' || r == ';' {
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
			continue
		}

		if r == '&' {
			if i+1 < len(runes) && runes[i+1] == '&' {
				i++
			}
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
			continue
		}

		if r == '|' {
			if i+1 < len(runes) && (runes[i+1] == '|' || runes[i+1] == '&') {
				i++
			}
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		segments = append(segments, current.String())
	}

	var cleaned []string
	for _, s := range segments {
		sTrim := strings.TrimSpace(s)
		if sTrim != "" {
			cleaned = append(cleaned, sTrim)
		}
	}
	return cleaned
}

func tokenizeWords(seg string) []string {
	var words []string
	var current strings.Builder
	inQuote := rune(0)
	runes := []rune(seg)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			}
			current.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			inQuote = r
			current.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

func isEnvAssignment(token string) bool {
	idx := strings.Index(token, "=")
	if idx <= 0 {
		return false
	}
	for i, r := range token[:idx] {
		if i == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func unquoteToken(s string) string {
	s = strings.TrimSpace(s)
	// Remove outer quotes if wrapped
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	// Strip leading backslash escape e.g. \rm -> rm
	s = strings.TrimPrefix(s, "\\")
	return s
}

func normalizeBaseCommand(token string) string {
	t := unquoteToken(token)
	// If path like /bin/rm or /usr/local/bin/ls, take basename
	idx := strings.LastIndex(t, "/")
	if idx >= 0 && idx < len(t)-1 {
		t = t[idx+1:]
	}
	return unquoteToken(t)
}

func isSensitivePathOrSecret(token string) bool {
	raw := strings.Trim(unquoteToken(token), "\"'`")
	if raw == "" {
		return false
	}

	pathsToCheck := []string{raw}
	if realPath, err := filepath.EvalSymlinks(raw); err == nil && realPath != raw {
		pathsToCheck = append(pathsToCheck, realPath)
	}

	for _, p := range pathsToCheck {
		clean := strings.ToLower(p)

		// 1. /proc protection: block environ, mem, cmdline, fd, maps for all processes (including $PPID, self, etc.)
		if strings.Contains(clean, "/proc") || strings.Contains(clean, "proc/") {
			if strings.Contains(clean, "environ") || strings.Contains(clean, "mem") ||
				strings.Contains(clean, "cmdline") || strings.Contains(clean, "maps") ||
				strings.Contains(clean, "smaps") || strings.Contains(clean, "status") ||
				strings.Contains(clean, "fd") {
				return true
			}
		}

		// 2. Sensitive directory and credential patterns
		sensitivePatterns := []string{
			".ssh",
			"id_rsa",
			"id_ed25519",
			"id_ecdsa",
			"id_dsa",
			"known_hosts",
			"authorized_keys",
			".gnupg",
			".config/zerotwo/config.json",
			"zerotwo/config.json",
			"/etc/shadow",
			"/etc/gshadow",
			"/etc/sudoers",
			".env",
			".aws",
			".azure",
			".kube",
			".docker/config.json",
			".dockercfg",
			".git-credentials",
			".netrc",
			".bash_history",
			".zsh_history",
			"~root",
		}
		for _, pat := range sensitivePatterns {
			if strings.Contains(clean, pat) {
				return true
			}
		}

		// 3. Sensitive file extensions
		sensitiveSuffixes := []string{".pem", ".key", ".pfx", ".pkcs12"}
		for _, suf := range sensitiveSuffixes {
			if strings.HasSuffix(clean, suf) {
				return true
			}
		}

		// 4. Wildcards / globs targeting sensitive paths (e.g. ~/.ss?/id_*, ~root/.ssh/*)
		if strings.Contains(clean, ".ss") || strings.Contains(clean, "id_") ||
			(strings.Contains(clean, "proc") && strings.Contains(clean, "env")) {
			if strings.Contains(clean, "*") || strings.Contains(clean, "?") || strings.Contains(clean, "[") {
				return true
			}
		}
	}
	return false
}
