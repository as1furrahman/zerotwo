package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"zerotwo/pkg/config"
	"zerotwo/pkg/provider"
)

// CommandExecutor defines the signature for running authorized shell commands.
type CommandExecutor func(command string) (int, error)

type Assistant struct {
	client     *provider.Client
	classifier *TokenizingClassifier
	authorizer CommandAuthorizer
	autoYes    bool
	executor   CommandExecutor
	confirmIn  io.Reader
	confirmOut io.Writer
}

func NewAssistant(client *provider.Client, authorizer CommandAuthorizer, autoYes bool) *Assistant {
	classifier := NewTokenizingClassifier()
	if authorizer == nil {
		authorizer = &defaultAuthorizer{classifier: classifier, autoYes: autoYes}
	}
	return &Assistant{
		client:     client,
		classifier: classifier,
		authorizer: authorizer,
		autoYes:    autoYes,
		executor:   runShellCommand,
	}
}

// SetExecutor overrides the command execution engine (used in tests to inject mock runners).
func (a *Assistant) SetExecutor(exec CommandExecutor) {
	a.executor = exec
}

// SetConfirmIO overrides the confirmation reader and writer (used in tests for fake I/O).
func (a *Assistant) SetConfirmIO(in io.Reader, out io.Writer) {
	a.confirmIn = in
	a.confirmOut = out
}

// AskShellPrompt instructs the LLM to output only the single raw shell command for Linux.
const shellSystemPrompt = `You are a terminal command assistant for a low-power Linux system.
Translate the user's natural language request into a single, valid, safe shell command.
Output ONLY the raw shell command. No markdown code blocks, no backticks, no explanations, no commentary.`

func (a *Assistant) Run(ctx context.Context, naturalLanguage string) (int, error) {
	req := provider.CompletionRequest{
		Messages: []provider.Message{
			{Role: "system", Content: shellSystemPrompt},
			{Role: "user", Content: naturalLanguage},
		},
		MaxTokens:   256,
		Temperature: 0.1,
		Stream:      false,
	}

	resp, err := a.client.Complete(ctx, req, nil)
	if err != nil {
		return 1, fmt.Errorf("LLM error: %w", err)
	}

	command := strings.TrimSpace(resp.Choices[0].Message.Content)
	// Clean any stray markdown ticks if model emitted them
	command = strings.TrimPrefix(command, "```bash")
	command = strings.TrimPrefix(command, "```sh")
	command = strings.TrimPrefix(command, "```")
	command = strings.TrimSuffix(command, "```")
	command = strings.TrimSpace(command)

	if command == "" {
		return 1, fmt.Errorf("no command generated")
	}

	return a.InteractiveLoop(ctx, command, naturalLanguage)
}

func (a *Assistant) InteractiveLoop(ctx context.Context, command, query string) (int, error) {
	out := a.confirmOut
	if out == nil {
		out = os.Stdout
	}

	for {
		fmt.Fprintf(out, "\n\033[1m%s\033[0m\n", command)

		// 1. Evaluate centralized authorization policy first
		dec, details, err := a.authorizer.AuthorizeCommand(command)

		// Absolute Denial: MEMORY.md, sessions/*.json, or explicitly blocked commands
		if dec == DecisionDeny {
			warn := details.Warning
			if warn == "" && err != nil {
				warn = err.Error()
			}
			fmt.Fprintf(out, "\n[SECURITY POLICY DENIAL: %s]\n", warn)
			return 1, fmt.Errorf("command denied by security policy: %s", warn)
		}

		// Read-only command with auto-yes: allowed without interactive confirmation
		if dec == DecisionAllow && a.autoYes {
			fmt.Fprintln(out, "[Auto-approving read-only command via --yes]")
			return a.executor(command)
		}

		if details.Warning != "" {
			fmt.Fprintf(out, "Notice: %s\n", details.Warning)
		}

		// Open confirmation I/O (injected mock or /dev/tty, fail-closed if unavailable)
		var confirmReader io.Reader
		var cleanupReader func()

		if a.confirmIn != nil {
			confirmReader = a.confirmIn
			cleanupReader = func() {}
		} else {
			tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
			if err != nil {
				return 1, fmt.Errorf("confirmation denied: cannot open /dev/tty (no controlling terminal for verification): %w", err)
			}
			confirmReader = tty
			cleanupReader = func() { _ = tty.Close() }
		}

		var promptText string
		switch dec {
		case DecisionRequireStrongApproval:
			promptText = "[WARNING: Destructive command detected! Type 'yes' to execute | r: Revise | q: Quit]: "
		case DecisionRequireApproval:
			if details.Category == "egress_remote" {
				promptText = "[Network egress detected. Type 'y' to run | r: Revise | q: Quit]: "
			} else {
				promptText = "[Type 'y' to run | r: Revise | q: Quit]: "
			}
		default:
			// DecisionAllow without auto-yes
			promptText = "[Enter: Run | r: Revise | q: Quit]: "
		}

		fmt.Fprint(out, promptText)

		input, err := readLineWithContext(ctx, confirmReader)
		cleanupReader()

		if err != nil {
			if errors.Is(err, context.Canceled) || (ctx != nil && ctx.Err() != nil) {
				return 130, fmt.Errorf("interrupted by user cancellation: %w", ctx.Err())
			}
			return 1, fmt.Errorf("failed reading user confirmation: %w", err)
		}

		ans := strings.TrimSpace(input)

		switch dec {
		case DecisionAllow:
			if ans == "" || strings.EqualFold(ans, "e") || strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes") {
				return a.executor(command)
			}
		case DecisionRequireApproval:
			if strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes") {
				return a.executor(command)
			}
		case DecisionRequireStrongApproval:
			if ans == "yes" {
				return a.executor(command)
			}
		}

		if strings.EqualFold(ans, "r") {
			// Revise
			fmt.Fprint(out, "Enter your revision / modification: ")
			var revReader io.Reader
			var cleanupRev func()
			if a.confirmIn != nil {
				revReader = a.confirmIn
				cleanupRev = func() {}
			} else {
				ttyRev, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
				if err != nil {
					return 1, fmt.Errorf("cannot open /dev/tty: %w", err)
				}
				revReader = ttyRev
				cleanupRev = func() { _ = ttyRev.Close() }
			}

			revText, revErr := readLineWithContext(ctx, revReader)
			cleanupRev()

			if revErr != nil {
				return 1, fmt.Errorf("failed reading revision: %w", revErr)
			}
			rev := strings.TrimSpace(revText)
			if rev != "" {
				newQuery := fmt.Sprintf("%s\nRevision requested by user: %s", query, rev)
				return a.Run(ctx, newQuery)
			}
			continue
		}

		if strings.EqualFold(ans, "q") || ans == "exit" {
			fmt.Fprintln(out, "Cancelled.")
			return 0, nil
		}

		fmt.Fprintln(out, "Action denied or unrecognized response.")
		return 1, nil
	}
}

func isFdReadable(fd int, timeout time.Duration) (bool, error) {
	var readFds syscall.FdSet
	readFds.Bits[fd/64] |= 1 << (uint(fd) % 64)
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	n, err := syscall.Select(fd+1, &readFds, nil, nil, &tv)
	if err != nil {
		if errors.Is(err, syscall.EINTR) {
			return false, nil
		}
		return false, err
	}
	return n > 0, nil
}

func readLineWithContext(ctx context.Context, r io.Reader) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var line []byte
	oneByte := make([]byte, 1)

	if f, ok := r.(*os.File); ok {
		fd := int(f.Fd())
		for {
			select {
			case <-ctx.Done():
				return string(line), fmt.Errorf("confirmation cancelled: %w", ctx.Err())
			default:
			}

			ready, err := isFdReadable(fd, 50*time.Millisecond)
			if err != nil {
				return string(line), err
			}
			if !ready {
				continue
			}

			n, err := f.Read(oneByte)
			if n > 0 {
				if oneByte[0] == '\n' {
					break
				}
				line = append(line, oneByte[0])
			}
			if err != nil {
				if err == io.EOF {
					if len(line) > 0 {
						break
					}
					return string(line), io.EOF
				}
				return string(line), err
			}
		}
		return string(line), nil
	}

	// Fallback for non-file mock readers in unit tests (e.g. strings.Reader, bytes.Buffer)
	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		reader := bufio.NewReader(r)
		lineStr, err := reader.ReadString('\n')
		ch <- readResult{line: lineStr, err: err}
	}()

	select {
	case <-ctx.Done():
		return string(line), fmt.Errorf("confirmation cancelled: %w", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			if res.err == io.EOF && len(res.line) == 0 {
				return "", io.EOF
			}
			if res.err != io.EOF {
				return res.line, res.err
			}
		}
		return res.line, nil
	}
}

// FindShellPath locates the appropriate shell binary across Linux, Android Termux, and Android system.
func FindShellPath() string {
	if p, err := exec.LookPath("sh"); err == nil {
		return p
	}
	// Termux standard path fallback
	const termuxSh = "/data/data/com.termux/files/usr/bin/sh"
	if _, err := os.Stat(termuxSh); err == nil {
		return termuxSh
	}
	// Android native shell fallback
	const androidSh = "/system/bin/sh"
	if _, err := os.Stat(androidSh); err == nil {
		return androidSh
	}
	return "/bin/sh"
}

func runShellCommand(command string) (int, error) {
	shPath := FindShellPath()
	cmd := exec.Command(shPath, "-c", command) // #nosec G204 -- core interactive assistant command execution gated by classifier & authorizer
	cmd.Env = config.SanitizeChildEnv(os.Environ())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

type defaultAuthorizer struct {
	classifier *TokenizingClassifier
	autoYes    bool
}

func (d *defaultAuthorizer) AuthorizeCommand(cmd string) (Decision, ApprovalDetails, error) {
	if isMod, reason := IsModifyingProtectedTarget(cmd, ""); isMod {
		return DecisionDeny, ApprovalDetails{
			Category: "mutate_memory",
			Action:   "shell",
			Target:   cmd,
			Warning:  fmt.Sprintf("modifying MEMORY.md or session files via shell is forbidden (%s)", reason),
		}, fmt.Errorf("command denied: modifying MEMORY.md or session files via shell is forbidden")
	}

	if isNet, reason := IsNetworkCapableCommand(cmd); isNet {
		return DecisionRequireApproval, ApprovalDetails{
			Category:       "egress_remote",
			Action:         "execute_network_command",
			Target:         cmd,
			RequiresStrong: false,
			Warning:        fmt.Sprintf("Network-capable command detected (%s). Cannot be auto-approved by --yes.", reason),
		}, nil
	}

	sev, reason := d.classifier.Classify(cmd)
	if sev == SeverityDestructive {
		return DecisionRequireStrongApproval, ApprovalDetails{
			Category:       "destructive",
			Action:         "execute_destructive_command",
			Target:         cmd,
			RequiresStrong: true,
			Warning:        fmt.Sprintf("Destructive command detected (%s). Full 'yes' confirmation required. Cannot be approved by --yes.", reason),
		}, nil
	}
	if sev == SeverityGeneric {
		return DecisionRequireApproval, ApprovalDetails{
			Category:       "execute_command",
			Action:         "execute_command",
			Target:         cmd,
			RequiresStrong: false,
			Warning:        fmt.Sprintf("Mutating command or sensitive path detected (%s). Cannot be auto-approved by --yes.", reason),
		}, nil
	}

	if d.autoYes {
		return DecisionAllow, ApprovalDetails{
			Category: "read_only_local",
			Action:   "execute_read_only_command",
			Target:   cmd,
		}, nil
	}

	return DecisionRequireApproval, ApprovalDetails{
		Category: "read_only_local",
		Action:   "execute_read_only_command",
		Target:   cmd,
	}, nil
}
