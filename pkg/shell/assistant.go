package shell

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"zerotwo/pkg/config"
	"zerotwo/pkg/provider"
	"zerotwo/pkg/tools"
)

type Assistant struct {
	client     *provider.Client
	classifier *TokenizingClassifier
	autoYes    bool
}

func NewAssistant(client *provider.Client, autoYes bool) *Assistant {
	return &Assistant{
		client:     client,
		classifier: NewTokenizingClassifier(),
		autoYes:    autoYes,
	}
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
	for {
		sev, reason := a.classifier.Classify(command)

		fmt.Printf("\n\033[1m%s\033[0m\n", command)
		if reason != "" {
			fmt.Printf("Notice: %s\n", reason)
		}

		// Check if we can auto-approve with --yes (ONLY for read-only commands!)
		if a.autoYes && sev == SeverityReadOnly {
			fmt.Println("[Auto-approving read-only command via --yes]")
			return runShellCommand(command)
		}

		// Open /dev/tty for interactive prompt (fail closed if unavailable)
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return 1, fmt.Errorf("confirmation denied: cannot open /dev/tty (no controlling terminal for verification): %w", err)
		}

		var promptText string
		switch sev {
		case SeverityReadOnly:
			promptText = "[Enter: Run | r: Revise | q: Quit]: "
		case SeverityGeneric:
			promptText = "[Type 'y' to run | r: Revise | q: Quit]: "
		case SeverityDestructive:
			promptText = "[WARNING: Destructive command detected! Type 'yes' to execute | r: Revise | q: Quit]: "
		}

		fmt.Fprint(tty, promptText)
		type readResult struct {
			line string
			err  error
		}
		ch := make(chan readResult, 1)
		go func() {
			reader := bufio.NewReader(tty)
			line, err := reader.ReadString('\n')
			ch <- readResult{line: line, err: err}
		}()

		var input string
		select {
		case <-ctx.Done():
			_ = tty.Close()
			return 130, fmt.Errorf("interrupted by user cancellation: %w", ctx.Err())
		case res := <-ch:
			_ = tty.Close()
			if res.err != nil {
				return 1, fmt.Errorf("failed reading user confirmation from /dev/tty: %w", res.err)
			}
			input = res.line
		}

		ans := strings.TrimSpace(input)

		switch sev {
		case SeverityReadOnly:
			if ans == "" || strings.EqualFold(ans, "e") || strings.EqualFold(ans, "y") {
				return runShellCommand(command)
			}
		case SeverityGeneric:
			if strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes") {
				return runShellCommand(command)
			}
		case SeverityDestructive:
			if ans == "yes" {
				return runShellCommand(command)
			}
		}

		if strings.EqualFold(ans, "r") {
			// Revise
			fmt.Print("Enter your revision / modification: ")
			ttyRev, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
			if err != nil {
				return 1, fmt.Errorf("cannot open /dev/tty: %w", err)
			}
			revReader := bufio.NewReader(ttyRev)
			revText, _ := revReader.ReadString('\n')
			_ = ttyRev.Close()
			rev := strings.TrimSpace(revText)
			if rev != "" {
				newQuery := fmt.Sprintf("%s\nRevision requested by user: %s", query, rev)
				return a.Run(ctx, newQuery)
			}
			continue
		}

		if strings.EqualFold(ans, "q") || ans == "exit" {
			fmt.Println("Cancelled.")
			return 0, nil
		}

		fmt.Println("Action denied or unrecognized response.")
		return 1, nil
	}
}

func runShellCommand(command string) (int, error) {
	shPath := tools.FindShellPath()
	cmd := exec.Command(shPath, "-c", command) // #nosec G204 -- core interactive assistant command execution gated by classifier
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
