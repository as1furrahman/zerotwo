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
		autoYesY    = flag.Bool("y", false, "Auto-approve non-destructive tool/command confirmations (shorthand)")
		noStream    = flag.Bool("no-stream", false, "Disable streaming mode")
	)
	flag.Parse()

	effectiveAutoYes := *autoYes || *autoYesY

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
		authorizer := auth.NewShellAuthorizer(policy, cfg.Agent.Workspace, cfg.Tools.BlockedCommands, effectiveAutoYes)
		assistant := shell.NewAssistant(client, authorizer, effectiveAutoYes)
		code, err := assistant.Run(ctx, *shellAssist)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Shell assistant error: %v\n", err)
		}
		os.Exit(code)
	}

	// Build tool registry
	registry := buildRegistry(cfg, mem)
	sessions := session.NewManager(cfg.Agent.Workspace)
	loop := agent.NewLoop(cfg, client, registry, sessions, mem, effectiveAutoYes)

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
	if userPrompt == "" && len(flag.Args()) > 0 {
		userPrompt = strings.Join(flag.Args(), " ")
	}
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
	fmt.Print(ui.CyberdeckBanner(modelDisplay, sess.ID, cfg.Agent.Workspace, 8, 10))

	scanner := bufio.NewScanner(os.Stdin)
	// 10 MB buffer for line input
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for {
		fmt.Print(ui.CyberdeckDock())
		if !scanner.Scan() {
			break // EOF / Ctrl-D
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Built-in Cyberdeck & REPL commands
		if input == "exit" || input == "quit" || input == "bye" || input == "q" {
			fmt.Println("\nJack out complete. Goodbye, Darling.")
			break
		}
		if input == "?" || input == "/menu" {
			printCyberdeckPalette()
			continue
		}
		if input == "/help" {
			printHelp()
			continue
		}
		if input == "c" || input == "/c" || input == "/shards" || input == "/clippings" {
			handleClippingsExplorer(cfg.Agent.Workspace, scanner, loop, sess)
			continue
		}
		if input == "x" || input == "/x" || input == "/export" || input == "/datadump" {
			path, err := session.ExportDataDump(sess, cfg.Agent.Workspace)
			if err != nil {
				fmt.Printf("\n\033[31mExport failed: %v\033[0m\n\n", err)
			} else {
				fmt.Printf("\n \033[1m▲▲ // DATA DUMP COMPILED ////////////////////////////////////////////\033[0m\n")
				fmt.Printf(" Target: %s\n", path)
				fmt.Printf(" Status: Indexed into Kindle Library. You can read it offline, Darling.\n")
				fmt.Printf(" ///////////////////////////////////////////////////////////////////////\n\n")
			}
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

		fmt.Print(ui.NeuralLinkEngagement("neural resonance engaged · streaming quickhack..."))

		firstToken := true
		var tokenCb provider.TokenCallback
		if cfg.Provider.Stream {
			tokenCb = func(token string) {
				if firstToken {
					fmt.Print(" \033[1m▲▲ ZERO TWO\033[0m\n")
					firstToken = false
				}
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
			fmt.Print(" \033[1m▲▲ ZERO TWO\033[0m\n")
			ui.RenderPaged(ui.MarkdownToTerm(response))
			fmt.Println()
		} else {
			fmt.Println()
		}
	}
}

func handleClippingsExplorer(workspace string, scanner *bufio.Scanner, loop *agent.Loop, sess *session.Session) {
	clipPath := tools.FindClippingsPath("", workspace)
	items, err := tools.ReadClippingsFile(clipPath, 5, "")
	if err != nil || len(items) == 0 {
		fmt.Printf("\n\033[90m▲▲ No memory shards found at %s\033[0m\n\n", clipPath)
		return
	}

	fmt.Printf("\n \033[1m▲▲ // MEMORY SHARDS: ACCESSED ////////////////////////////////////////\033[0m\n")
	fmt.Printf(" Source: %s\n\n", clipPath)
	for i, it := range items {
		authorStr := ""
		if it.Author != "" {
			authorStr = fmt.Sprintf(" (%s)", it.Author)
		}
		locStr := ""
		if it.Location != "" {
			locStr = fmt.Sprintf(" · Loc: %s", it.Location)
		}
		preview := it.Content
		if len(preview) > 120 {
			preview = preview[:117] + "..."
		}
		fmt.Printf(" [%d] \033[1m\"%s\"%s\033[0m%s\n", i+1, it.Title, authorStr, locStr)
		fmt.Printf("     \"%s\"\n\n", preview)
	}

	fmt.Printf(" Select shard [1-%d], or 'q' to abort: ", len(items))
	if !scanner.Scan() {
		return
	}
	choice := strings.TrimSpace(scanner.Text())
	if choice == "q" || choice == "" {
		return
	}

	idx := 0
	for i := range items {
		if choice == fmt.Sprintf("%d", i+1) {
			idx = i + 1
			break
		}
	}
	if idx == 0 {
		fmt.Println("Invalid selection.")
		return
	}

	selected := items[idx-1]
	prompt := fmt.Sprintf("Please explain and analyze this reading highlight from \"%s\":\n\n\"%s\"", selected.Title, selected.Content)

	fmt.Printf("\n \033[1mZero Two\033[0m: \"Decompiling shard from \033[1m%s\033[0m, Darling...\"\n", selected.Title)

	ctx, cancel := createInterruptContext()
	defer cancel()

	firstToken := true
	var tokenCb provider.TokenCallback
	tokenCb = func(token string) {
		if firstToken {
			fmt.Print("\n \033[1m▲▲ ZERO TWO\033[0m\n")
			firstToken = false
		}
		fmt.Print(token)
	}

	_, err = loop.Run(ctx, prompt, sess, tokenCb)
	if err != nil {
		fmt.Printf("\n\033[31merror: %v\033[0m\n", err)
	}
	fmt.Print("\n\n")
}

func printCyberdeckPalette() {
	fmt.Print(`
 ▲▲ CYBERDECK PALETTE ────────────────────────────────────────────────
 Deck Ops:
   c, /shards       — Access Reading Shards (My Clippings.txt)
   x, /export       — Compile active run into eBook Data Dump (HTML)
   /status, /clear  — Telemetry / Purge neural context
   /reload, /memory — Rebuild prompt / View MEMORY.md
   q, exit          — Jack out of cyberdeck
 Flags:
   -m "prompt"      — Single-shot non-interactive neural query
   -e "task"        — Shell assistant mode (translate English to bash)
   -y, -yes         — Auto-approve non-destructive quickhacks
 ─────────────────────────────────────────────────────────────────────
`)
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

	r.Register(tools.NewReadClippingsTool(cfg.Agent.Workspace))
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
