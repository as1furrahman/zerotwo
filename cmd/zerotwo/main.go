package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"zerotwo/pkg/agent"
	"zerotwo/pkg/auth"
	"zerotwo/pkg/config"
	"zerotwo/pkg/provider"
	"zerotwo/pkg/session"
	"zerotwo/pkg/shell"
	"zerotwo/pkg/tools"
	"zerotwo/pkg/ui"
)

const banner = `
  ┌──────────────────────────────────────────────┐
  │  ZeroTwo · Low-Resource Linux & ARM AI Agent │
  └──────────────────────────────────────────────┘
  Model: %s  |  Type 'exit' to quit
`

func main() {
	var (
		message     = flag.String("m", "", "Single message (non-interactive)")
		shellAssist = flag.String("e", "", "Shell assistant mode: translate natural language to command")
		sessionID   = flag.String("s", session.DefaultSessionID(), "Session ID")
		listModels  = flag.Bool("models", false, "Query provider endpoint and list served models")
		dryRun      = flag.Bool("dry-run", false, "Print full request payload (with masked keys) without sending")
		showMemory  = flag.Bool("memory", false, "Print raw local MEMORY.md and exit")
		memPreview  = flag.Bool("memory-preview", false, "Print exact memory payload that would be sent to provider")
		doLearn     = flag.Bool("learn", false, "Run interactive profile interview to build MEMORY.md")
		autoYes     = flag.Bool("yes", false, "Auto-approve non-destructive tool/command confirmations")
		noStream    = flag.Bool("no-stream", false, "Disable streaming mode")
	)
	flag.Parse()

	// 1. Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		os.Exit(1)
	}

	if *noStream {
		cfg.Provider.Stream = false
	}

	// 2. Initialize provider client
	client, err := provider.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Provider initialization error: %v\n", err)
		os.Exit(1)
	}

	// 3. Flag: List models dynamically
	if *listModels {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		models, err := client.ListModels(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing models from %s: %v\n", cfg.Provider.BaseURL, err)
			os.Exit(1)
		}
		fmt.Printf("Available models at %s:\n", cfg.Provider.BaseURL)
		for _, m := range models {
			fmt.Printf("  • %s\n", m)
		}
		return
	}

	// 4. Initialize workspace, memory store, and registry
	_ = os.MkdirAll(cfg.Agent.Workspace, 0700)
	config.CheckDirPermissions(cfg.Agent.Workspace, 0700)

	mem := agent.NewMemoryStore(cfg.Agent.Workspace)

	if *showMemory {
		content := mem.LoadRaw()
		if content == "" {
			fmt.Println("(no memory file found)")
		} else {
			fmt.Println(content)
		}
		return
	}

	if *memPreview {
		preview := mem.LoadFiltered(cfg.Provider.RedactSections)
		sendMemory := false
		if cfg.Provider.SendMemory != nil {
			sendMemory = *cfg.Provider.SendMemory
		}
		fmt.Printf("Provider BaseURL: %s\n", cfg.Provider.BaseURL)
		fmt.Printf("SendMemory: %v\n", sendMemory)
		fmt.Printf("Redacted Sections: %v\n\n", cfg.Provider.RedactSections)
		if !sendMemory {
			fmt.Println("(send_memory is false: NO memory will be transmitted to this provider)")
		} else if preview == "" {
			fmt.Println("(memory is empty)")
		} else {
			fmt.Println("--- PAYLOAD THAT WILL BE TRANSMITTED ---")
			fmt.Println(preview)
			fmt.Println("----------------------------------------")
		}
		return
	}

	// 5. Flag: -learn interview
	if *doLearn {
		iv := agent.NewInterviewer(client, mem)
		if err := iv.Run(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "Interview error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// 6. Flag: -e shell assistant
	if *shellAssist != "" {
		ctx, cancel := createInterruptContext()
		defer cancel()
		policy := auth.NewPolicy()
		authorizer := auth.NewShellAuthorizer(policy, cfg.Agent.Workspace, cfg.Tools.BlockedCommands, *autoYes)
		assistant := shell.NewAssistant(client, authorizer, *autoYes)
		code, err := assistant.Run(ctx, *shellAssist)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Shell assistant error: %v\n", err)
		}
		os.Exit(code)
	}

	// Build tool registry
	registry := buildRegistry(cfg, mem)
	sessions := session.NewManager(cfg.Agent.Workspace)
	loop := agent.NewLoop(cfg, client, registry, sessions, mem, *autoYes)

	sess, err := sessions.Load(*sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Session error: %v\n", err)
		os.Exit(1)
	}

	// 7. Check piped stdin input (with 10MB buffer support)
	var pipedInput string
	if !ui.IsStdinTerminal() {
		input, err := readPipedStdin(os.Stdin, 10*1024*1024)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		pipedInput = input
	}

	userPrompt := *message
	if pipedInput != "" {
		if userPrompt != "" {
			userPrompt = fmt.Sprintf("%s\n\n[Piped Input]:\n%s", userPrompt, pipedInput)
		} else {
			userPrompt = pipedInput
		}
	}

	// 8. Flag: -dry-run
	if *dryRun {
		runDryRun(cfg, loop, sess, userPrompt, registry)
		return
	}

	// 9. Single-shot / pipe execution
	if userPrompt != "" {
		ctx, cancel := createInterruptContext()
		defer cancel()

		var tokenCb provider.TokenCallback
		if cfg.Provider.Stream {
			tokenCb = func(token string) {
				fmt.Print(token)
			}
		}

		response, err := loop.Run(ctx, userPrompt, sess, tokenCb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nAgent error: %v\n", err)
			os.Exit(1)
		}
		if !cfg.Provider.Stream {
			ui.RenderPaged(response)
		} else {
			fmt.Println()
		}
		return
	}

	// 10. Interactive REPL (works in raw TTY, dumb terminal, or line-based fallback)
	modelDisplay := cfg.Provider.Model
	if modelDisplay == "" {
		modelDisplay = "(default from provider)"
	}
	fmt.Printf(banner, modelDisplay)
	fmt.Printf("  Endpoint: %s  |  Session: %s\n\n", cfg.Provider.BaseURL, sess.ID)

	scanner := bufio.NewScanner(os.Stdin)
	// 10 MB buffer for line input
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for {
		fmt.Print("\033[1myou>\033[0m ")
		if !scanner.Scan() {
			break // EOF / Ctrl-D
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Built-in REPL commands
		if input == "exit" || input == "quit" || input == "bye" {
			fmt.Println("Goodbye.")
			break
		}
		if input == "/help" {
			printHelp()
			continue
		}
		if input == "/status" {
			printStatus(cfg, sess)
			continue
		}
		if input == "/memory" {
			content := mem.LoadRaw()
			if content == "" {
				fmt.Println("(no memory found)")
			} else {
				ui.RenderPaged(content)
			}
			continue
		}
		if input == "/memory-preview" {
			preview := mem.LoadFiltered(cfg.Provider.RedactSections)
			sendMemory := false
			if cfg.Provider.SendMemory != nil {
				sendMemory = *cfg.Provider.SendMemory
			}
			fmt.Printf("SendMemory: %v  |  Redacted: %v\n", sendMemory, cfg.Provider.RedactSections)
			if !sendMemory {
				fmt.Println("(send_memory is false: memory will not be sent to this endpoint)")
			} else {
				ui.RenderPaged(preview)
			}
			continue
		}
		if input == "/reload" {
			loop.ContextBuilder().RebuildPrompt()
			fmt.Println("✓ System prompt reloaded from current config and memory.")
			continue
		}
		if input == "/clear" {
			sess.Messages = nil
			if err := sessions.Save(sess); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to persist cleared session: %v\n", err)
			}
			fmt.Println("Conversation cleared.")
			continue
		}

		ctx, cancel := createInterruptContext()

		fmt.Printf("\033[90m[%s is thinking...]\033[0m\n", cfg.Agent.Name)

		var tokenCb provider.TokenCallback
		if cfg.Provider.Stream {
			tokenCb = func(token string) {
				fmt.Print(token)
			}
		}

		response, err := loop.Run(ctx, input, sess, tokenCb)
		cancel()

		if err != nil {
			if ctx.Err() == context.Canceled {
				fmt.Fprintln(os.Stderr, "\n^C [request cancelled]")
			} else {
				fmt.Fprintf(os.Stderr, "\n\033[31merror: %v\033[0m\n", err)
			}
			continue
		}

		if !cfg.Provider.Stream {
			fmt.Println()
			ui.RenderPaged(response)
			fmt.Println()
		} else {
			fmt.Print("\n\n")
		}
	}
}

func buildRegistry(cfg *config.Config, mem *agent.MemoryStore) *tools.Registry {
	r := tools.NewRegistry()
	maxOut := cfg.Tools.MaxToolOutputSize

	rf := tools.NewReadFileTool(maxOut)
	rf.SetWorkspace(cfg.Agent.Workspace)
	r.Register(rf)
	r.Register(tools.NewWriteFileTool())
	r.Register(tools.NewPatchFileTool())
	r.Register(tools.NewListDirTool(maxOut))
	r.Register(tools.NewSystemStatusTool(maxOut))

	if cfg.Tools.AllowShell {
		r.Register(tools.NewShellTool(maxOut))
	}

	r.Register(tools.NewWebFetchTool(maxOut))
	r.Register(tools.NewHTTPRequestTool(maxOut))
	if cfg.Tools.BraveAPIKey != "" {
		r.Register(tools.NewSearchTool(cfg.Tools.BraveAPIKey, cfg.Tools.MaxSearchResults, maxOut))
	}

	r.Register(agent.NewSaveMemoryTool(mem))
	r.Register(agent.NewPatchMemoryTool(mem))

	return r
}

func runDryRun(cfg *config.Config, loop *agent.Loop, sess *session.Session, prompt string, registry *tools.Registry) {
	if prompt == "" {
		prompt = "(example user prompt)"
	}
	tempHistory := append([]provider.Message(nil), sess.Messages...)
	tempHistory = append(tempHistory, provider.Message{Role: "user", Content: prompt})

	fullMsgs := loop.ContextBuilder().BuildMessages(tempHistory)
	trimmedMsgs := loop.TrimContext(fullMsgs, registry.AsProviderTools())

	dryPayload := map[string]interface{}{
		"model":       cfg.Provider.Model,
		"messages":    trimmedMsgs,
		"tools":       registry.AsProviderTools(),
		"max_tokens":  cfg.Agent.MaxTokens,
		"temperature": cfg.Agent.Temperature,
		"top_p":       cfg.Agent.TopP,
		"stream":      cfg.Provider.Stream,
	}

	data, _ := json.MarshalIndent(dryPayload, "", "  ")
	fmt.Println("=== DRY RUN REQUEST PAYLOAD (API Key: " + config.MaskKey(cfg.Provider.APIKey) + ") ===")
	fmt.Println(string(data))
	fmt.Println("=========================================================================")
}

func printStatus(cfg *config.Config, sess *session.Session) {
	sendMem := false
	if cfg.Provider.SendMemory != nil {
		sendMem = *cfg.Provider.SendMemory
	}
	fmt.Printf("\n  Agent:       %s\n", cfg.Agent.Name)
	fmt.Printf("  Endpoint:    %s\n", cfg.Provider.BaseURL)
	fmt.Printf("  API Key:     %s\n", config.MaskKey(cfg.Provider.APIKey))
	fmt.Printf("  Model:       %s\n", cfg.Provider.Model)
	fmt.Printf("  Stream:      %v\n", cfg.Provider.Stream)
	fmt.Printf("  Send Memory: %v (LAN/Local=%v)\n", sendMem, config.IsLocalOrLAN(cfg.Provider.BaseURL))
	fmt.Printf("  Workspace:   %s\n", cfg.Agent.Workspace)
	fmt.Printf("  Session:     %s (%d messages)\n\n", sess.ID, len(sess.Messages))
}

func printHelp() {
	fmt.Print(`
Commands:
  /memory              — View raw long-term memory (MEMORY.md)
  /memory-preview      — View exact memory text sent to the configured provider
  /reload              — Rebuild static system prompt from updated config/memory
  /clear               — Clear conversation history for active session
  /status              — Display provider, model, masked keys, and memory settings
  /help                — Show this reference
  exit                 — Exit ZeroTwo

CLI Flags:
  -m "message"         — Single message non-interactive mode
  -e "task"            — Shell assistant mode: translate natural language to command
  -s <id>              — Named session ID (default: "default")
  -models              — Query provider endpoint and list served models
  -dry-run             — Print request payload with masked keys without sending
  -memory              — Print raw MEMORY.md and exit
  -memory-preview      — Print filtered memory sent to provider and exit
  -learn               — Run interactive profile interview to build MEMORY.md
  -yes                 — Auto-approve non-destructive tool/command confirmations
  -no-stream           — Turn off streaming mode
`)
}

// createInterruptContext sets up a graceful cancellation context for in-flight requests.
// The first SIGINT/SIGTERM cancels ctx gracefully; a second SIGINT forces immediate exit (code 130).
func createInterruptContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	doneCh := make(chan struct{})

	go func() {
		select {
		case <-sigCh:
			cancel()
			select {
			case <-sigCh:
				fmt.Fprintln(os.Stderr, "\n^C [force quit]")
				os.Exit(130)
			case <-doneCh:
				return
			}
		case <-doneCh:
			return
		}
	}()

	cleanup := func() {
		signal.Stop(sigCh)
		select {
		case <-doneCh:
		default:
			close(doneCh)
		}
		cancel()
	}
	return ctx, cleanup
}

func readPipedStdin(r io.Reader, maxBytes int) (string, error) {
	reader := bufio.NewReader(r)
	var sb strings.Builder
	buf := make([]byte, 64*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if sb.Len()+n > maxBytes {
				return "", fmt.Errorf("piped input exceeded %d MB limit", maxBytes/(1024*1024))
			}
			sb.Write(buf[:n])
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("reading stdin pipe: %w", err)
		}
	}
	return strings.TrimSpace(sb.String()), nil
}
