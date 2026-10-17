package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"zerotwo/pkg/config"
	"zerotwo/pkg/provider"
	"zerotwo/pkg/session"
	"zerotwo/pkg/tools"
)

func TestTokenEstimationRuneAware(t *testing.T) {
	// 1. ASCII text (16 chars -> ~4 tokens)
	asciiText := "Hello, world 123"
	asciiTokens := EstimateTokenCount(asciiText)
	if asciiTokens < 3 || asciiTokens > 6 {
		t.Errorf("unexpected token count for ASCII %q: %d", asciiText, asciiTokens)
	}

	// 2. Bengali text ("আমার সোনার বাংলা")
	// 16 runes, non-ASCII weight factor ~0.65 -> ~10-12 tokens
	bengaliText := "আমার সোনার বাংলা"
	bengaliTokens := EstimateTokenCount(bengaliText)
	if bengaliTokens < 8 || bengaliTokens > 15 {
		t.Errorf("unexpected token count for Bengali %q: %d", bengaliText, bengaliTokens)
	}

	// 3. CJK text ("你好世界，这是一段中文测试")
	// 13 runes, non-ASCII weight factor ~0.65 -> ~8-10 tokens
	cjkText := "你好世界，这是一段中文测试"
	cjkTokens := EstimateTokenCount(cjkText)
	if cjkTokens < 7 || cjkTokens > 12 {
		t.Errorf("unexpected token count for CJK %q: %d", cjkText, cjkTokens)
	}
}

func TestAtomicToolPairPreservation(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.ContextWindow = 200 // small window to force trimming
	cfg.Agent.MaxTokens = 50

	registry := tools.NewRegistry()
	store := NewMemoryStore(t.TempDir())
	loop := NewLoop(cfg, nil, registry, nil, store, true)

	sysMsg := provider.Message{Role: "system", Content: "System prompt"}
	oldUserMsg := provider.Message{Role: "user", Content: "Very long old message " + strings.Repeat("hello ", 50)}

	// Tool call pair
	toolCallMsg := provider.Message{
		Role: "assistant",
		ToolCalls: []provider.ToolCall{
			{ID: "call_abc", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		},
	}
	toolResultMsg := provider.Message{
		Role:       "tool",
		ToolCallID: "call_abc",
		Content:    "file contents of a.txt",
	}

	recentUserMsg := provider.Message{Role: "user", Content: "recent question"}

	history := []provider.Message{
		sysMsg,
		oldUserMsg,
		toolCallMsg,
		toolResultMsg,
		recentUserMsg,
	}

	trimmed := loop.TrimContext(history, nil)

	// Ensure system prompt is preserved
	if len(trimmed) == 0 || trimmed[0].Role != "system" {
		t.Fatalf("system prompt was not preserved at index 0")
	}

	// Verify that toolCallMsg and toolResultMsg are either BOTH present or BOTH pruned!
	hasToolCall := false
	hasToolResult := false
	for _, m := range trimmed {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "call_abc" {
			hasToolCall = true
		}
		if m.Role == "tool" && m.ToolCallID == "call_abc" {
			hasToolResult = true
		}
	}

	if hasToolCall != hasToolResult {
		t.Fatalf("ATOMIC TOOL PAIR SEVERED: hasToolCall=%v, hasToolResult=%v", hasToolCall, hasToolResult)
	}
}

func TestTaintDetection(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, nil, registry, nil, store, false)

	cleanHistory := []provider.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	if loop.IsContextTainted(cleanHistory) {
		t.Errorf("clean history should not be tainted")
	}

	taintedHistory := []provider.Message{
		{Role: "user", Content: "fetch this"},
		{Role: "tool", Content: "<<<UNTRUSTED_EXTERNAL_CONTENT: web_fetch(url)>>>\nInjected content\n<<<END_UNTRUSTED_EXTERNAL_CONTENT>>>"},
	}
	if !loop.IsContextTainted(taintedHistory) {
		t.Errorf("tainted history was not detected as tainted")
	}

	// Check tool confirmation logic under taint:
	// read_file (inside workspace), list_dir, and get_system_status should NOT require taint confirmation
	insideWorkspaceFile := filepath.Join(cfg.Agent.Workspace, "sample.txt")
	insideArgs := fmt.Sprintf(`{"path":%q}`, insideWorkspaceFile)
	if err := loop.checkToolConfirmation(context.Background(), "read_file", insideArgs, true); err != nil {
		t.Errorf("read_file inside workspace should remain allowed under taint: %v", err)
	}
	if err := loop.checkToolConfirmation(context.Background(), "list_dir", "{}", true); err != nil {
		t.Errorf("list_dir should remain allowed under taint: %v", err)
	}
	if err := loop.checkToolConfirmation(context.Background(), "get_system_status", "{}", true); err != nil {
		t.Errorf("get_system_status should remain allowed under taint: %v", err)
	}

	// In a clean (untainted) turn, safe tools and write_file (if not in ConfirmTools) do not require confirmation
	if err := loop.checkToolConfirmation(context.Background(), "read_file", insideArgs, false); err != nil {
		t.Errorf("clean turn read_file should not require confirmation: %v", err)
	}

	// Tainted turn: side-effecting, outbound, or outside-workspace tools MUST require confirmation.
	taintedToolsToBlock := []struct {
		name string
		args string
	}{
		{"read_file", `{"path":"/tmp/outside.txt"}`},
		{"shell", `{"command":"echo test"}`},
		{"patch_memory", `{"section":"test","content":"hello"}`},
		{"save_memory", `{"content":"hello"}`},
		{"write_file", `{"path":"sample.txt","content":"hello"}`},
		{"http_request", `{"url":"https://example.com"}`},
		{"web_fetch", `{"url":"https://example.com"}`},
		{"web_search", `{"query":"test"}`},
	}
	for _, tt := range taintedToolsToBlock {
		// Test rejection
		loop.SetConfirmIO(strings.NewReader("no\n"), io.Discard)
		err := loop.checkToolConfirmation(context.Background(), tt.name, tt.args, true)
		if err == nil {
			t.Errorf("expected tainted turn to require confirmation and reject for %q, but got nil error", tt.name)
		}

		// Test approval
		loop.SetConfirmIO(strings.NewReader("yes\n"), io.Discard)
		if err := loop.checkToolConfirmation(context.Background(), tt.name, tt.args, true); err != nil {
			t.Errorf("expected approved confirmation for %q, got: %v", tt.name, err)
		}
	}

	// Verify that autoYes (--yes flag) does NOT bypass taint confirmations!
	loopWithYes := NewLoop(cfg, nil, registry, nil, store, true) // autoYes = true
	loopWithYes.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loopWithYes.checkToolConfirmation(context.Background(), "shell", "{}", true); err == nil {
		t.Errorf("autoYes should NOT bypass taint confirmation for shell!")
	}
}

func TestTaintMultiTurnPersistence(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.Workspace = t.TempDir()
	cfg.Tools.ConfirmTools = []string{} // clear confirm_tools so clean turn has no prompts
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, nil, registry, nil, store, false)

	// Turn 1: Clean turn (no untrusted data)
	messages := []provider.Message{
		{Role: "user", Content: "What is the weather?"},
		{Role: "assistant", Content: "It's sunny."},
	}
	if loop.IsContextTainted(messages) {
		t.Fatalf("Turn 1 should not be tainted")
	}
	// Clean turn: safe tools inside workspace do not require confirmation
	wsFile := filepath.Join(cfg.Agent.Workspace, "test.txt")
	wsArgs := fmt.Sprintf(`{"path":%q}`, wsFile)
	if err := loop.checkToolConfirmation(context.Background(), "read_file", wsArgs, loop.IsContextTainted(messages)); err != nil {
		t.Errorf("clean turn read_file should not require confirmation: %v", err)
	}
	if err := loop.checkToolConfirmation(context.Background(), "write_file", wsArgs, loop.IsContextTainted(messages)); err != nil {
		t.Errorf("clean turn write_file should not require confirmation: %v", err)
	}

	// Turn 2: User asks to fetch a URL, tool returns untrusted remote content
	messages = append(messages,
		provider.Message{Role: "user", Content: "Fetch this URL: https://example.com"},
		provider.Message{Role: "tool", Content: tools.NeutralizeAndWrapUntrusted("web_fetch(https://example.com)", "Sample untrusted website content", 4096)},
	)
	if !loop.IsContextTainted(messages) {
		t.Fatalf("Turn 2 must be detected as tainted after web_fetch")
	}
	// While tainted in Turn 2:
	// a) fetch then shell requires confirmation:
	loop.SetConfirmIO(strings.NewReader("no\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"ls"}`, loop.IsContextTainted(messages)); err == nil {
		t.Errorf("expected tainted turn to require confirmation for shell, but got nil error")
	}
	// b) fetch then patch_memory requires confirmation:
	loop.SetConfirmIO(strings.NewReader("no\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "patch_memory", `{"section":"Test","content":"hack"}`, loop.IsContextTainted(messages)); err == nil {
		t.Errorf("expected tainted turn to require confirmation for patch_memory, but got nil error")
	}

	// Turn 3: User follows up with a completely innocent subsequent prompt in the SAME session:
	// "Can you update my memory now?"
	messages = append(messages,
		provider.Message{Role: "user", Content: "Can you update my memory now?"},
	)
	// CRITICAL FIX 2 REQUIREMENT: Taint MUST persist across turns for the rest of the session!
	if !loop.IsContextTainted(messages) {
		t.Fatalf("CRITICAL DEFECT: Turn 3 lost taint even though untrusted data remains in context history!")
	}
	loop.SetConfirmIO(strings.NewReader("no\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "patch_memory", `{"section":"Projects","content":"tainted"}`, loop.IsContextTainted(messages)); err == nil {
		t.Errorf("Turn 3 must STILL require confirmation for patch_memory due to persistent session taint!")
	}

	// Turn 4: Context is trimmed and the untrusted turn is removed from context history
	// Messages now only contain clean user messages
	cleanMessages := []provider.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Recent clean message"},
	}
	if loop.IsContextTainted(cleanMessages) {
		t.Errorf("Once untrusted data is trimmed out, context should no longer be tainted")
	}
}

func TestMockTTYConfirmationsFailClosed(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, nil, registry, nil, store, false)

	// Simulate EOF (closed reader / no TTY input available)
	loop.SetConfirmIO(strings.NewReader(""), io.Discard)
	err := loop.promptUserConfirmation(context.Background(), "shell", `{"command":"rm -rf /"}`, false)
	if err == nil {
		t.Fatalf("expected EOF / closed input to fail closed, got nil error")
	}
	if !strings.Contains(err.Error(), "failed reading confirmation") && !strings.Contains(err.Error(), "denied") {
		t.Errorf("expected denial/reading failure error, got: %v", err)
	}
}

func TestConcurrentLoopAccess(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, nil, registry, nil, store, false)

	// Concurrent goroutines checking taint, estimating tokens, and reading/writing
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			txt := fmt.Sprintf("goroutine %d message text testing concurrency", id)
			_ = EstimateTokenCount(txt)
			msgs := []provider.Message{
				{Role: "user", Content: txt},
				{Role: "assistant", Content: "reply"},
			}
			_ = loop.IsContextTainted(msgs)
		}(i)
	}
	wg.Wait()
}

func TestContextCancellationPreservesSession(t *testing.T) {
	// Mock slow LLM server that blocks until canceled
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"reply"}}]}`))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tmpDir := t.TempDir()
	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, client, registry, sessions, store, false)

	sess := &session.Session{
		ID: "test-sess",
		Messages: []provider.Message{
			{Role: "user", Content: "Previous prompt 1"},
			{Role: "assistant", Content: "Previous response 1"},
		},
	}

	// Create pre-cancelled context to simulate instant Ctrl-C
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = loop.Run(ctx, "New cancelled prompt", sess, nil)
	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}

	// Verify session history preserved exactly (no orphaned user prompt)
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages preserved in session, got %d", len(sess.Messages))
	}
	if sess.Messages[0].Content != "Previous prompt 1" || sess.Messages[1].Content != "Previous response 1" {
		t.Errorf("session history corrupted after cancellation: %+v", sess.Messages)
	}
}

func TestAgentShellDestructiveGateRequiresYes(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()

	// autoYes = true (--yes flag passed!)
	loop := NewLoop(cfg, nil, registry, nil, store, true)

	// 1. Destructive shell command with "y" (single letter) -> MUST BE REJECTED
	loop.SetConfirmIO(strings.NewReader("y\n"), io.Discard)
	err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"rm -rf /tmp/test"}`, false)
	if err == nil {
		t.Fatalf("destructive command 'rm -rf' must NOT be approved by 'y', expected denial")
	}

	// 2. Destructive shell command with "yes" -> APPROVED
	loop.SetConfirmIO(strings.NewReader("yes\n"), io.Discard)
	err = loop.checkToolConfirmation(context.Background(), "shell", `{"command":"rm -rf /tmp/test"}`, false)
	if err != nil {
		t.Fatalf("destructive command should be approved by typing 'yes', got: %v", err)
	}

	// 3. Destructive shell command with EOF (no tty input) -> FAILS CLOSED
	loop.SetConfirmIO(strings.NewReader(""), io.Discard)
	err = loop.checkToolConfirmation(context.Background(), "shell", `{"command":"rm -rf /tmp/test"}`, false)
	if err == nil {
		t.Fatalf("destructive command with no input must fail closed")
	}
}

func TestAgentShellCleanTurnDefaultConfirmTools(t *testing.T) {
	cfg := config.DefaultConfig() // ConfirmTools: ["shell", "write_file"]
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()

	// Non-autoYes loop
	loopNoYes := NewLoop(cfg, nil, registry, nil, store, false)

	// Clean turn: shell prompts, user types "n" -> denied
	loopNoYes.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loopNoYes.checkToolConfirmation(context.Background(), "shell", `{"command":"ls -l"}`, false); err == nil {
		t.Errorf("expected denial when user types 'n' for shell in clean turn")
	}

	// Clean turn: shell prompts, user types "y" -> approved
	loopNoYes.SetConfirmIO(strings.NewReader("y\n"), io.Discard)
	if err := loopNoYes.checkToolConfirmation(context.Background(), "shell", `{"command":"ls -l"}`, false); err != nil {
		t.Errorf("expected approval when user types 'y' for shell in clean turn: %v", err)
	}

	// Clean turn: write_file prompts, user types "n" -> denied
	loopNoYes.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loopNoYes.checkToolConfirmation(context.Background(), "write_file", `{"path":"a.txt"}`, false); err == nil {
		t.Errorf("expected denial when user types 'n' for write_file in clean turn")
	}

	// Clean turn: patch_file prompts, user types "n" -> denied
	loopNoYes.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loopNoYes.checkToolConfirmation(context.Background(), "patch_file", `{"path":"a.txt","search":"a","replace":"b"}`, false); err == nil {
		t.Errorf("expected denial when user types 'n' for patch_file in clean turn")
	}

	// autoYes loop
	loopYes := NewLoop(cfg, nil, registry, nil, store, true)

	// Clean turn: non-destructive shell command auto-approved with --yes
	if err := loopYes.checkToolConfirmation(context.Background(), "shell", `{"command":"ls -l"}`, false); err != nil {
		t.Errorf("expected autoYes to approve non-destructive shell in clean turn: %v", err)
	}

	// Clean turn: write_file auto-approved with --yes
	if err := loopYes.checkToolConfirmation(context.Background(), "write_file", `{"path":"a.txt"}`, false); err != nil {
		t.Errorf("expected autoYes to approve write_file in clean turn: %v", err)
	}

	// Clean turn: patch_file auto-approved with --yes
	if err := loopYes.checkToolConfirmation(context.Background(), "patch_file", `{"path":"a.txt","search":"a","replace":"b"}`, false); err != nil {
		t.Errorf("expected autoYes to approve patch_file in clean turn: %v", err)
	}

	// TAINTED turn: autoYes CANNOT approve shell, write_file, or patch_file!
	loopYes.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loopYes.checkToolConfirmation(context.Background(), "shell", `{"command":"ls -l"}`, true); err == nil {
		t.Errorf("autoYes must NOT bypass confirmation when context is tainted!")
	}
	loopYes.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loopYes.checkToolConfirmation(context.Background(), "write_file", `{"path":"a.txt"}`, true); err == nil {
		t.Errorf("autoYes must NOT bypass write_file confirmation when context is tainted!")
	}
	loopYes.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loopYes.checkToolConfirmation(context.Background(), "patch_file", `{"path":"a.txt","search":"a","replace":"b"}`, true); err == nil {
		t.Errorf("autoYes must NOT bypass patch_file confirmation when context is tainted!")
	}
}

func TestAgentMidStreamCancellationAndRollback(t *testing.T) {
	// SSE server that emits tokens slowly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"token1 \"}}]}\n\n"))
		flusher.Flush()

		// Wait for client to abort
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = true
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tmpDir := t.TempDir()
	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, client, registry, sessions, store, false)

	sess := &session.Session{
		ID: "sess-midstream",
		Messages: []provider.Message{
			{Role: "user", Content: "Turn 1 question"},
			{Role: "assistant", Content: "Turn 1 answer"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	var receivedToken bool

	// Cancel context during token callback (mid-stream abort!)
	tokenCb := func(token string) {
		receivedToken = true
		cancel()
	}

	_, err = loop.Run(ctx, "Turn 2 question to abort", sess, tokenCb)
	if err == nil {
		t.Fatalf("expected error on mid-stream abort")
	}
	if !receivedToken {
		t.Errorf("expected to receive at least one token before abort")
	}

	// Verify rollback to origLen (exactly 2 messages, no corrupted Turn 2)
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages after rollback, got %d", len(sess.Messages))
	}
	if sess.Messages[0].Content != "Turn 1 question" || sess.Messages[1].Content != "Turn 1 answer" {
		t.Errorf("session history corrupted after rollback: %+v", sess.Messages)
	}
}

func TestTrimContextOversizedSingleMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.ContextWindow = 4000
	cfg.Agent.MaxTokens = 500

	loop := NewLoop(cfg, nil, tools.NewRegistry(), nil, NewMemoryStore(t.TempDir()), false)

	oversized := strings.Repeat("A", 40000) // ~10,000 tokens
	msgs := []provider.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: oversized},
	}

	trimmed := loop.TrimContext(msgs, nil)
	if len(trimmed) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(trimmed))
	}

	userMsg := trimmed[1]
	if !strings.Contains(userMsg.Content, "[NOTICE: Input exceeded context budget") {
		t.Errorf("expected truncation notice in user message")
	}

	tokens := EstimateTokenCount(userMsg.Content)
	if tokens > 3500 {
		t.Errorf("oversized message was not truncated to fit context window: %d tokens", tokens)
	}
}

func TestSIGINTDeliveryCancelsLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n"))
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = true
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tmpDir := t.TempDir()
	loop := NewLoop(cfg, client, tools.NewRegistry(), session.NewManager(tmpDir), NewMemoryStore(tmpDir), false)
	sess := &session.Session{ID: "sigint-sess"}

	// Set up actual OS SIGINT notification context (identical to main.go)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Trigger real OS SIGINT signal after first token is received
	tokenCb := func(token string) {
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}

	_, err = loop.Run(ctx, "test prompt for sigint", sess, tokenCb)
	if err == nil {
		t.Fatalf("expected error from SIGINT cancellation")
	}
	if ctx.Err() != context.Canceled {
		t.Errorf("expected context.Canceled from SIGINT, got %v", ctx.Err())
	}
}

func TestRunPipedInputBudgetVerification(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		capturedBody = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := provider.CompletionResponse{
			Choices: []provider.Choice{
				{
					Index: 0,
					Message: provider.Message{
						Role:    "assistant",
						Content: "Processed piped input successfully within budget.",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Agent.ContextWindow = 12000
	cfg.Agent.MaxTokens = 2048
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tmpDir := t.TempDir()
	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, client, registry, sessions, store, false)

	sess := &session.Session{ID: "test-budget-wire"}

	// 150 KB input (37,500 tokens), exceeds context budget (~8,952 tokens)
	piped150k := strings.Repeat("X", 150000)

	reply, err := loop.Run(context.Background(), piped150k, sess, nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if reply != "Processed piped input successfully within budget." {
		t.Errorf("unexpected reply: %q", reply)
	}

	// Verify captured HTTP request body sent over the wire
	if len(capturedBody) == 0 {
		t.Fatalf("no request body captured by HTTP server")
	}

	var reqPayload provider.CompletionRequest
	if err := json.Unmarshal(capturedBody, &reqPayload); err != nil {
		t.Fatalf("failed decoding captured body: %v", err)
	}

	// Payload must be trimmed from 150 KB down to budget (< 50 KB)
	if len(capturedBody) >= 150000 {
		t.Fatalf("captured body size %d was not trimmed! Expected < 50000 bytes", len(capturedBody))
	}

	// Find the user message
	var userMsg *provider.Message
	for _, m := range reqPayload.Messages {
		if m.Role == "user" {
			userMsg = &m
			break
		}
	}
	if userMsg == nil {
		t.Fatalf("no user message found in captured payload")
	}

	if !strings.Contains(userMsg.Content, "[NOTICE: Input exceeded context budget") {
		t.Errorf("truncation notice missing from captured HTTP request content!")
	}

	userTokens := EstimateTokenCount(userMsg.Content)
	expectedMaxBudget := int(float64(cfg.Agent.ContextWindow)*0.75) - cfg.Agent.MaxTokens
	if userTokens > expectedMaxBudget {
		t.Errorf("user message tokens (%d) exceed expected maximum budget (%d)", userTokens, expectedMaxBudget)
	}
	t.Logf("captured user message tokens: %d, expected max budget: %d", userTokens, expectedMaxBudget)
}

func TestMidTurnCancellationPreservesCompletedSideEffects(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "side_effect.txt")

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			// Iteration 0: model issues write_file tool call
			writeArgs, _ := json.Marshal(map[string]string{
				"path":    targetFile,
				"content": "side effect data on disk",
			})
			resp := provider.CompletionResponse{
				Choices: []provider.Choice{
					{
						Index: 0,
						Message: provider.Message{
							Role: "assistant",
							ToolCalls: []provider.ToolCall{
								{
									Index: 0,
									ID:    "call_side_effect_1",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "write_file",
										Arguments: string(writeArgs),
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Iteration 1: slow response allowing context cancellation to interrupt
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			http.Error(w, "timeout", http.StatusGatewayTimeout)
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Agent.Workspace = tmpDir
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Tools.ConfirmTools = []string{} // allow write_file in test
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	registry.Register(tools.NewWriteFileTool())
	loop := NewLoop(cfg, client, registry, sessions, store, false)

	sess := &session.Session{
		ID: "side-effect-sess",
		Messages: []provider.Message{
			{Role: "user", Content: "initial user message"},
			{Role: "assistant", Content: "initial assistant message"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after 100ms when tool execution completed and iter 1 is pending
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err = loop.Run(ctx, "write side effect file", sess, nil)
	if err == nil {
		t.Fatalf("expected cancellation error")
	}

	// Verify disk side effect actually happened and was not reverted
	data, readErr := os.ReadFile(targetFile)
	if readErr != nil || string(data) != "side effect data on disk" {
		t.Fatalf("disk side effect missing: readErr=%v, data=%q", readErr, string(data))
	}

	// Verify session history PRESERVED the completed tool call and result!
	// Must NOT be rolled back to length 2!
	// Messages: [0: initial user, 1: initial assistant, 2: turn user, 3: assistant tool_calls, 4: tool result, 5: assistant notice]
	if len(sess.Messages) != 6 {
		t.Fatalf("expected 6 messages preserving side effects, got %d: %+v", len(sess.Messages), sess.Messages)
	}

	if sess.Messages[3].Role != "assistant" || len(sess.Messages[3].ToolCalls) != 1 {
		t.Errorf("tool_calls message lost in session: %+v", sess.Messages[3])
	}
	if sess.Messages[4].Role != "tool" || sess.Messages[4].ToolCallID != "call_side_effect_1" {
		t.Errorf("tool result message lost in session: %+v", sess.Messages[4])
	}
	if sess.Messages[5].Role != "assistant" || !strings.Contains(sess.Messages[5].Content, "interrupted after 1 completed tool action(s)") {
		t.Errorf("assistant interruption notice missing: %+v", sess.Messages[5])
	}
}

func TestMalformedToolArgumentsJSONReturnsToolError(t *testing.T) {
	var callCount int32
	var mu sync.Mutex
	var receivedMessages []provider.Message

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		var req provider.CompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		receivedMessages = req.Messages
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			// Model returns malformed JSON with concatenated objects
			resp := provider.CompletionResponse{
				Choices: []provider.Choice{
					{
						Index: 0,
						Message: provider.Message{
							Role: "assistant",
							ToolCalls: []provider.ToolCall{
								{
									Index: 0,
									ID:    "call_malformed",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "read_file",
										Arguments: `{"path":"a.txt"}{"path":"b.txt"}`, // Malformed JSON!
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Iteration 2: model sees syntax error and recovers
		resp := provider.CompletionResponse{
			Choices: []provider.Choice{
				{
					Index: 0,
					Message: provider.Message{
						Role:    "assistant",
						Content: "Detected and acknowledged argument syntax error.",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tmpDir := t.TempDir()
	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(1024))
	loop := NewLoop(cfg, client, registry, sessions, store, false)

	sess := &session.Session{ID: "malformed-args-sess"}
	reply, err := loop.Run(context.Background(), "trigger malformed call", sess, nil)
	if err != nil {
		t.Fatalf("unexpected loop error: %v", err)
	}
	if reply != "Detected and acknowledged argument syntax error." {
		t.Errorf("unexpected reply: %q", reply)
	}

	// Verify the tool response fed back to the model contained the syntax error notice
	mu.Lock()
	msgs := make([]provider.Message, len(receivedMessages))
	copy(msgs, receivedMessages)
	mu.Unlock()

	var toolResultMsg *provider.Message
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "call_malformed" {
			toolResultMsg = &m
			break
		}
	}
	if toolResultMsg == nil {
		t.Fatalf("tool result message was not sent to model in round 2")
	}
	if !strings.Contains(toolResultMsg.Content, "TOOL EXECUTION ERROR: malformed JSON arguments") {
		t.Errorf("expected malformed JSON notice in tool message, got: %q", toolResultMsg.Content)
	}
}

func TestShellCommandCancellationDuringExecution(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			resp := provider.CompletionResponse{
				Choices: []provider.Choice{
					{
						Index: 0,
						Message: provider.Message{
							Role: "assistant",
							ToolCalls: []provider.ToolCall{
								{
									Index: 0,
									ID:    "call_sleep_cmd",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "shell",
										Arguments: `{"command":"sleep 10"}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Tools.AllowShell = true
	cfg.Tools.ConfirmTools = []string{} // allow shell without prompt in test
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tmpDir := t.TempDir()
	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	registry.Register(tools.NewShellTool(4096))
	loop := NewLoop(cfg, client, registry, sessions, store, false)
	loop.SetConfirmIO(strings.NewReader("y\n"), io.Discard)

	sess := &session.Session{ID: "shell-cancel-sess"}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after 50ms while sleep 10 is executing
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = loop.Run(ctx, "execute sleep", sess, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error on cancelled shell command, got nil")
	}

	// Must finish quickly (< 1.5 seconds) rather than waiting 10 seconds for sleep
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("cancellation took %v; expected rapid process group termination (< 1.5s)", elapsed)
	}

	// Verify that the shell tool result was recorded as interrupted/cancelled
	var foundToolResult bool
	for _, m := range sess.Messages {
		if m.Role == "tool" && m.ToolCallID == "call_sleep_cmd" {
			foundToolResult = true
			if !strings.Contains(m.Content, "interrupted") && !strings.Contains(m.Content, "aborted") && !strings.Contains(m.Content, "cancel") {
				t.Errorf("tool result did not mention cancellation: %s", m.Content)
			}
		}
	}
	if !foundToolResult {
		t.Fatalf("tool result for call_sleep_cmd was not saved in session messages")
	}
}

// Custom tool that cancels context during execution
type cancelTriggerTool struct {
	cancelFunc context.CancelFunc
}

func (c *cancelTriggerTool) Name() string                { return "trigger_cancel" }
func (c *cancelTriggerTool) Description() string         { return "triggers context cancellation" }
func (c *cancelTriggerTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c *cancelTriggerTool) Run(argsJSON string) (string, error) {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	return "cancellation triggered", nil
}

func TestParallelToolBatchCancellationPreservesAllToolIDs(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			// Model issues 3 parallel tool calls:
			// 1: trigger_cancel (will cancel the turn's ctx)
			// 2: read_file (subsequent tool 1)
			// 3: get_system_status (subsequent tool 2)
			resp := provider.CompletionResponse{
				Choices: []provider.Choice{
					{
						Index: 0,
						Message: provider.Message{
							Role: "assistant",
							ToolCalls: []provider.ToolCall{
								{
									Index: 0,
									ID:    "call_batch_1",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "trigger_cancel",
										Arguments: `{}`,
									},
								},
								{
									Index: 1,
									ID:    "call_batch_2",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "read_file",
										Arguments: `{"path":"dummy.txt"}`,
									},
								},
								{
									Index: 2,
									ID:    "call_batch_3",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "get_system_status",
										Arguments: `{}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Tools.ConfirmTools = []string{}
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tmpDir := t.TempDir()
	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	trigger := &cancelTriggerTool{cancelFunc: cancel}
	registry.Register(trigger)
	registry.Register(tools.NewReadFileTool(1024))
	registry.Register(tools.NewSystemStatusTool(1024))

	loop := NewLoop(cfg, client, registry, sessions, store, false)
	sess := &session.Session{ID: "batch-cancel-sess"}

	_, err = loop.Run(ctx, "run parallel batch with cancel", sess, nil)
	if err == nil {
		t.Fatalf("expected loop to return cancellation error")
	}

	// Verify that ALL THREE tool call IDs have corresponding "tool" role messages!
	// This is critical to prevent dangling tool_calls and HTTP 400 errors from LLM providers.
	toolResultMap := make(map[string]provider.Message)
	for _, m := range sess.Messages {
		if m.Role == "tool" {
			toolResultMap[m.ToolCallID] = m
		}
	}

	for _, expectedID := range []string{"call_batch_1", "call_batch_2", "call_batch_3"} {
		msg, exists := toolResultMap[expectedID]
		if !exists {
			t.Errorf("MISSING TOOL RESULT for %s in session messages! Causes HTTP 400 on subsequent turns.", expectedID)
		} else {
			t.Logf("Found resolved tool result for %s: %s", expectedID, msg.Content)
		}
	}

	// Verify the final assistant message records interruption notice
	lastMsg := sess.Messages[len(sess.Messages)-1]
	if lastMsg.Role != "assistant" || !strings.Contains(lastMsg.Content, "Execution interrupted") {
		t.Errorf("expected final interruption notice in session history, got: %+v", lastMsg)
	}
}

func FuzzToolJSONValidation(f *testing.F) {
	seeds := []string{
		"{}",
		`{"command":"ls -la"}`,
		`{"path":"/tmp/test.txt"}`,
		`{"section":"Projects","content":"data"}`,
		"",
		"   ",
		"invalid json string",
		`{"path":`,
		`{"path":"a"}{"path":"b"}`,
		`{"command": "rm -rf /", "nested": {"a": [1, 2, 3]}}`,
		`{"url": "https://example.com", "method": "POST", "headers": {"X-Test": "1"}}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		toolArgs := strings.TrimSpace(input)
		if toolArgs == "" {
			toolArgs = "{}"
		}
		var rawJSON json.RawMessage
		_ = json.Unmarshal([]byte(toolArgs), &rawJSON)
	})
}

func TestRunContextTrimmingWithLargeMemoryAndLANSendMemory(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		capturedBody = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := provider.CompletionResponse{
			Choices: []provider.Choice{
				{
					Index: 0,
					Message: provider.Message{
						Role:    "assistant",
						Content: "Processed large memory + large prompt request.",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL // Localhost HTTP test server -> SendMemory resolves to true!
	cfg.Provider.Stream = false
	cfg.Agent.ContextWindow = 12000
	cfg.Agent.MaxTokens = 2048
	cfg.Resolve()

	tmpDir := t.TempDir()
	store := NewMemoryStore(tmpDir)

	// Create ~4,000 tokens of persistent memory
	largeMemoryText := strings.Repeat("persistent developer facts, system guidelines, architecture notes. ", 250)
	if err := store.Save("## BigProfile\n" + largeMemoryText); err != nil {
		t.Fatalf("failed saving large memory: %v", err)
	}

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	registry.Register(tools.NewSystemStatusTool(1024))
	registry.Register(tools.NewReadFileTool(1024))
	loop := NewLoop(cfg, client, registry, sessions, store, false)

	sess := &session.Session{ID: "large-mem-wire-sess"}

	// 150 KB user input (~37,500 tokens)
	piped150k := strings.Repeat("Y", 150000)

	reply, err := loop.Run(context.Background(), piped150k, sess, nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if reply != "Processed large memory + large prompt request." {
		t.Errorf("unexpected reply: %q", reply)
	}

	if len(capturedBody) == 0 {
		t.Fatalf("no request captured by test server")
	}

	var reqPayload provider.CompletionRequest
	if err := json.Unmarshal(capturedBody, &reqPayload); err != nil {
		t.Fatalf("failed decoding captured body: %v", err)
	}

	// Payload must be trimmed from 150 KB down to budget (< 50 KB)
	if len(capturedBody) >= 150000 {
		t.Fatalf("captured body %d was not trimmed! Expected < 50000 bytes", len(capturedBody))
	}

	var sysMsg, userMsg *provider.Message
	for i := range reqPayload.Messages {
		if reqPayload.Messages[i].Role == "system" {
			sysMsg = &reqPayload.Messages[i]
		}
		if reqPayload.Messages[i].Role == "user" {
			userMsg = &reqPayload.Messages[i]
		}
	}

	if sysMsg == nil {
		t.Fatalf("system prompt missing from captured payload")
	}
	if userMsg == nil {
		t.Fatalf("user message missing from captured payload")
	}

	// Verify large memory is actually in system prompt
	if !strings.Contains(sysMsg.Content, "BigProfile") {
		t.Errorf("expected large memory section in system prompt")
	}

	// Verify user message has truncation notice
	if !strings.Contains(userMsg.Content, "[NOTICE: Input exceeded context budget") {
		t.Errorf("truncation notice missing from captured user message content")
	}

	// Verify total tokens of the captured payload does NOT exceed 75% context window - maxTokens
	sysTokens := EstimateTokenCount(sysMsg.Content)
	userTokens := EstimateTokenCount(userMsg.Content)
	toolsBytes, _ := json.Marshal(reqPayload.Tools)
	toolsTokens := EstimateTokenCount(string(toolsBytes))

	totalPromptTokens := sysTokens + userTokens + toolsTokens
	maxAllowedTokens := int(float64(cfg.Agent.ContextWindow)*0.75) - cfg.Agent.MaxTokens

	if totalPromptTokens > maxAllowedTokens {
		t.Errorf("Total prompt tokens (%d) exceed maximum allowed context budget (%d)! sys=%d, user=%d, tools=%d",
			totalPromptTokens, maxAllowedTokens, sysTokens, userTokens, toolsTokens)
	}
	t.Logf("PASS: Large memory + oversized input: sysTokens=%d, userTokens=%d, toolsTokens=%d, total=%d <= maxAllowed=%d",
		sysTokens, userTokens, toolsTokens, totalPromptTokens, maxAllowedTokens)
}

func TestCtrlCConfirmationPromptImmediateCancellation(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, nil, registry, nil, store, false)

	// Set up pipe that blocks reading forever (simulating waiting for user input)
	pr, pw := io.Pipe()
	defer pw.Close()
	defer pr.Close()
	loop.SetConfirmIO(pr, io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel context after 25ms
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := loop.promptUserConfirmation(ctx, "shell", `{"command":"mkdir test"}`, false)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error on cancelled confirmation prompt, got nil")
	}
	if !strings.Contains(err.Error(), "cancelled") && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected cancellation error message, got: %v", err)
	}
	if elapsed > 1*time.Second {
		t.Errorf("promptUserConfirmation took too long to unblock (%v), expected < 1s", elapsed)
	}

	// Also test promptDestructiveConfirmation
	pr2, pw2 := io.Pipe()
	defer pw2.Close()
	defer pr2.Close()
	loop.SetConfirmIO(pr2, io.Discard)

	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel2()
	}()

	start2 := time.Now()
	err2 := loop.promptDestructiveConfirmation(ctx2, "shell", `{"command":"rm -rf /tmp/abc"}`, "destructive")
	elapsed2 := time.Since(start2)

	if err2 == nil {
		t.Fatalf("expected error on cancelled destructive confirmation prompt, got nil")
	}
	if !strings.Contains(err2.Error(), "cancelled") && !strings.Contains(err2.Error(), "canceled") {
		t.Errorf("expected cancellation error message, got: %v", err2)
	}
	if elapsed2 > 1*time.Second {
		t.Errorf("promptDestructiveConfirmation took too long to unblock (%v), expected < 1s", elapsed2)
	}
}

func TestAgentShellModelCallUnderYes(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	// autoYes = true
	loop := NewLoop(cfg, nil, registry, nil, store, true)

	// 1. Read-only command (ls): auto-approved
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"ls -la"}`, false); err != nil {
		t.Errorf("read-only ls should be auto-approved under autoYes, got: %v", err)
	}

	// 2. Mutating command (mkdir): requires 'y' confirmation even under autoYes!
	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"mkdir /tmp/newdir"}`, false); err == nil {
		t.Errorf("mutating command mkdir should NOT be auto-approved under autoYes!")
	}
	loop.SetConfirmIO(strings.NewReader("y\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"mkdir /tmp/newdir"}`, false); err != nil {
		t.Errorf("mutating command mkdir should succeed when user enters 'y': %v", err)
	}

	// 3. Destructive command (rm): requires typed 'yes' even under autoYes!
	loop.SetConfirmIO(strings.NewReader("y\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"rm /tmp/data"}`, false); err == nil {
		t.Errorf("destructive command rm must NOT be approved by 'y' under autoYes!")
	}
	loop.SetConfirmIO(strings.NewReader("yes\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"rm /tmp/data"}`, false); err != nil {
		t.Errorf("destructive command rm should succeed when user enters 'yes': %v", err)
	}

	// 4. Sensitive path command (cat ~/.ssh/id_rsa): classified as Generic -> requires confirmation!
	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"cat ~/.ssh/id_rsa"}`, false); err == nil {
		t.Errorf("cat ~/.ssh/id_rsa must NOT be auto-approved under autoYes!")
	}

	// 5. Bare env command: classified as Generic -> requires confirmation!
	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"env"}`, false); err == nil {
		t.Errorf("bare env must NOT be auto-approved under autoYes!")
	}

	// 6. /proc environment inspection (cat /proc/self/environ): requires confirmation!
	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"cat /proc/self/environ"}`, false); err == nil {
		t.Errorf("cat /proc/self/environ must NOT be auto-approved under autoYes!")
	}

	// 7. /proc parent environment inspection (cat /proc/$PPID/environ): requires confirmation!
	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"cat /proc/$PPID/environ"}`, false); err == nil {
		t.Errorf("cat /proc/$PPID/environ must NOT be auto-approved under autoYes!")
	}

	// 8. Glob targeting sensitive keys (cat ~/.ss?/id_*): requires confirmation!
	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	if err := loop.checkToolConfirmation(context.Background(), "shell", `{"command":"cat ~/.ss?/id_*"}`, false); err == nil {
		t.Errorf("cat ~/.ss?/id_* must NOT be auto-approved under autoYes!")
	}
}

func TestReadFileSensitivePathDeniedInLoop(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, nil, registry, nil, store, true) // autoYes = true

	// read_file accessing sensitive paths must prompt and NOT be auto-approved under autoYes
	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	err := loop.checkToolConfirmation(context.Background(), "read_file", `{"path":"~/.ssh/id_ed25519"}`, false)
	if err == nil {
		t.Errorf("read_file ~/.ssh/id_ed25519 must require confirmation even under autoYes!")
	}

	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	err = loop.checkToolConfirmation(context.Background(), "read_file", `{"path":".env"}`, false)
	if err == nil {
		t.Errorf("read_file .env must require confirmation even under autoYes!")
	}

	loop.SetConfirmIO(strings.NewReader("n\n"), io.Discard)
	err = loop.checkToolConfirmation(context.Background(), "read_file", `{"path":"/proc/self/environ"}`, false)
	if err == nil {
		t.Errorf("read_file /proc/self/environ must require confirmation even under autoYes!")
	}
}

func TestPromptCancellationDoesNotBlockSubsequentPrompt(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, nil, registry, nil, store, false)

	// Step 1: Prompt 1 is presented, but user cancels with Ctrl-C (ctx.Done())
	pr1, pw1 := io.Pipe()
	defer pw1.Close()
	loop.SetConfirmIO(pr1, io.Discard)

	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel1()
	}()

	err1 := loop.promptUserConfirmation(ctx1, "shell", `{"command":"mkdir test1"}`, false)
	if err1 == nil {
		t.Fatalf("expected error on cancelled prompt 1, got nil")
	}

	// Step 2: In REPL mode, user immediately enters a second command requiring prompt 2!
	// The cancelled reader goroutine from Prompt 1 MUST NOT swallow or block the input of Prompt 2!
	loop.SetConfirmIO(strings.NewReader("y\n"), io.Discard)
	err2 := loop.promptUserConfirmation(context.Background(), "shell", `{"command":"mkdir test2"}`, false)
	if err2 != nil {
		t.Fatalf("Prompt 2 failed after Prompt 1 cancellation! Reader was likely orphaned or swallowed input: %v", err2)
	}
}

func TestPromptFileDeadlineCancellationWithoutOrphanGoroutine(t *testing.T) {
	cfg := config.DefaultConfig()
	store := NewMemoryStore(t.TempDir())
	registry := tools.NewRegistry()
	loop := NewLoop(cfg, nil, registry, nil, store, false)

	// Use real OS file descriptor pipe (*os.File) to exercise deadline-based cancellation
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed creating os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	loop.SetConfirmIO(r, io.Discard)

	// Step 1: Cancel Prompt 1 while waiting on *os.File
	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel1()
	}()

	err1 := loop.promptUserConfirmation(ctx1, "shell", `{"command":"touch file1"}`, false)
	if err1 == nil {
		t.Fatalf("expected error on cancelled prompt 1, got nil")
	}

	// Step 2: Now send "y\n" into the SAME pipe descriptor.
	// If any zombie reader goroutine lingered from Step 1, it would consume this byte stream,
	// starving Step 2. With SetReadDeadline, Step 1 ran synchronously with zero background goroutines!
	go func() {
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte("y\n"))
	}()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()

	err2 := loop.promptUserConfirmation(ctx2, "shell", `{"command":"touch file2"}`, false)
	if err2 != nil {
		t.Fatalf("Prompt 2 failed on same os.Pipe after Prompt 1 cancellation! Zombie goroutine likely swallowed input: %v", err2)
	}
}

func TestReadLineWithContextErrNoDeadlineKeepsPartialInput(t *testing.T) {
	// Simulate user typing partial input "ye" without pressing newline before cancellation.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	if _, err := w.Write([]byte("ye")); err != nil {
		t.Fatalf("w.Write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	val, err := readLineWithContext(ctx, r)
	if err == nil {
		t.Fatalf("expected error on cancelled readLineWithContext, got nil (must fail closed)")
	}
	if !strings.Contains(err.Error(), "confirmation cancelled") {
		t.Errorf("expected confirmation cancelled error, got %v", err)
	}
	if val != "ye" {
		t.Errorf("expected partial input %q to be preserved in return value, got %q", "ye", val)
	}
}

func TestTerminalFdFlagsHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_TERMINAL_HELPER") != "1" {
		return
	}
	// Helper signals readiness then blocks in readLineWithContext on stdin
	fmt.Fprintln(os.Stdout, "READY")
	_, _ = readLineWithContext(context.Background(), os.Stdin)
	select {}
}

func TestTerminalFdFlagsUnchanged(t *testing.T) {
	// Ground Rule: Never leave O_NONBLOCK set on terminal file descriptors!
	// Verify that select-based readLineWithContext never modifies fcntl F_GETFL flags
	// across normal reads, cancelled prompts, second-Ctrl-C force exits, and kill -9.

	getFlags := func(f *os.File) int {
		flags, _, err := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETFL, 0)
		if err != 0 {
			t.Fatalf("fcntl F_GETFL failed: %v", err)
		}
		return int(flags)
	}

	// 1. Context Cancellation
	t.Run("ContextCancel", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		defer r.Close()
		defer w.Close()

		origFlags := getFlags(r)
		if origFlags&syscall.O_NONBLOCK != 0 {
			t.Fatalf("initial pipe unexpectedly had O_NONBLOCK set")
		}

		ctxCancel, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, _ = readLineWithContext(ctxCancel, r)
		cancel()

		flagsAfterCancel := getFlags(r)
		if flagsAfterCancel != origFlags {
			t.Errorf("flags changed after cancel: before=%d, after=%d", origFlags, flagsAfterCancel)
		}
		if flagsAfterCancel&syscall.O_NONBLOCK != 0 {
			t.Errorf("SECURITY/TTY VIOLATION: O_NONBLOCK was left set after cancel!")
		}
	})

	// 2. Normal Read Return
	t.Run("NormalReturn", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		defer r.Close()
		defer w.Close()

		origFlags := getFlags(r)
		go func() {
			time.Sleep(10 * time.Millisecond)
			_, _ = w.Write([]byte("yes\n"))
		}()
		val, err := readLineWithContext(context.Background(), r)
		if err != nil || val != "yes" {
			t.Errorf("normal read failed: val=%q, err=%v", val, err)
		}

		flagsAfterRead := getFlags(r)
		if flagsAfterRead != origFlags {
			t.Errorf("flags changed after read: before=%d, after=%d", origFlags, flagsAfterRead)
		}
		if flagsAfterRead&syscall.O_NONBLOCK != 0 {
			t.Errorf("SECURITY/TTY VIOLATION: O_NONBLOCK was left set after read!")
		}
	})

	// 3. Second-Ctrl-C Force Exit
	t.Run("SecondCtrlCForceExit", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		defer pr.Close()
		defer pw.Close()

		origFlags := getFlags(pw)
		if origFlags&syscall.O_NONBLOCK != 0 {
			t.Fatalf("initial pipe unexpectedly had O_NONBLOCK set")
		}

		var cmd *exec.Cmd
		if qemu := os.Getenv("QEMU_EXEC"); qemu != "" {
			parts := strings.Fields(qemu)
			args := append(parts[1:], os.Args[0], "-test.run=^TestTerminalFdFlagsHelperProcess$")
			cmd = exec.Command(parts[0], args...)
		} else {
			cmd = exec.Command(os.Args[0], "-test.run=^TestTerminalFdFlagsHelperProcess$")
		}
		cmd.Env = append(os.Environ(), "GO_WANT_TERMINAL_HELPER=1")
		cmd.Stdin = pr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("StdoutPipe: %v", err)
		}

		if err := cmd.Start(); err != nil {
			t.Fatalf("cmd.Start: %v", err)
		}

		reader := bufio.NewReader(stdout)
		line, err := reader.ReadString('\n')
		if err != nil || !strings.Contains(line, "READY") {
			_ = cmd.Process.Kill()
			t.Fatalf("helper process failed to start: %v, line=%q", err, line)
		}

		// Helper is actively reading via readLineWithContext on pr
		flagsDuringRead := getFlags(pw)
		if flagsDuringRead&syscall.O_NONBLOCK != 0 {
			t.Errorf("SECURITY/TTY VIOLATION: O_NONBLOCK was set on pipe during read!")
		}

		// Send SIGINT twice to simulate second Ctrl-C force exit
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
		time.Sleep(20 * time.Millisecond)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)

		_ = cmd.Wait()

		flagsAfterSigint := getFlags(pw)
		if flagsAfterSigint != origFlags {
			t.Errorf("flags changed after SIGINT: before=%d, after=%d", origFlags, flagsAfterSigint)
		}
		if flagsAfterSigint&syscall.O_NONBLOCK != 0 {
			t.Errorf("SECURITY/TTY VIOLATION: O_NONBLOCK was left set after SIGINT exit!")
		}
	})

	// 4. kill -9 (SIGKILL) Abrupt Termination
	t.Run("KillMinus9", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		defer pr.Close()
		defer pw.Close()

		origFlags := getFlags(pw)
		if origFlags&syscall.O_NONBLOCK != 0 {
			t.Fatalf("initial pipe unexpectedly had O_NONBLOCK set")
		}

		var cmd *exec.Cmd
		if qemu := os.Getenv("QEMU_EXEC"); qemu != "" {
			parts := strings.Fields(qemu)
			args := append(parts[1:], os.Args[0], "-test.run=^TestTerminalFdFlagsHelperProcess$")
			cmd = exec.Command(parts[0], args...)
		} else {
			cmd = exec.Command(os.Args[0], "-test.run=^TestTerminalFdFlagsHelperProcess$")
		}
		cmd.Env = append(os.Environ(), "GO_WANT_TERMINAL_HELPER=1")
		cmd.Stdin = pr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("StdoutPipe: %v", err)
		}

		if err := cmd.Start(); err != nil {
			t.Fatalf("cmd.Start: %v", err)
		}

		reader := bufio.NewReader(stdout)
		line, err := reader.ReadString('\n')
		if err != nil || !strings.Contains(line, "READY") {
			_ = cmd.Process.Kill()
			t.Fatalf("helper process failed to start: %v, line=%q", err, line)
		}

		// Helper is actively reading via readLineWithContext on pr
		// Kill abruptly with kill -9
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)

		_ = cmd.Wait()

		flagsAfterKill := getFlags(pw)
		if flagsAfterKill != origFlags {
			t.Errorf("flags changed after kill -9: before=%d, after=%d", origFlags, flagsAfterKill)
		}
		if flagsAfterKill&syscall.O_NONBLOCK != 0 {
			t.Errorf("SECURITY/TTY VIOLATION: O_NONBLOCK was left set after kill -9!")
		}
	})
}

func TestTrimContextWhenSystemPromptPlusMemoryExceedsBudget(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.ContextWindow = 4000
	cfg.Agent.MaxTokens = 1000

	loop := NewLoop(cfg, nil, tools.NewRegistry(), nil, NewMemoryStore(t.TempDir()), false)

	// Huge system prompt containing ~4,500 tokens of notes
	hugePrompt := "System header\n" + strings.Repeat("Extremely large persistent memory facts, guidelines, and context. ", 300)
	userMsg := provider.Message{Role: "user", Content: "User query that also has some length."}

	msgs := []provider.Message{
		{Role: "system", Content: hugePrompt},
		userMsg,
	}

	trimmed := loop.TrimContext(msgs, nil)
	if len(trimmed) < 2 {
		t.Fatalf("expected at least system prompt and user message, got %d", len(trimmed))
	}

	trimmedSys := trimmed[0]
	trimmedUser := trimmed[1]

	// Verify notice was added to system prompt indicating truncation to fit context budget
	if !strings.Contains(trimmedSys.Content, "[NOTICE: System prompt / memory truncated to fit context budget]") {
		t.Errorf("system prompt was not truncated when exceeding budget! Got content length %d", len(trimmedSys.Content))
	}

	sysTokens := EstimateTokenCount(trimmedSys.Content)
	userTokens := EstimateTokenCount(trimmedUser.Content)
	totalTokens := sysTokens + userTokens

	// Effective budget = floor(4000 * 0.75) - 1000 = 2000 (minimum floor 2048)
	// Total prompt tokens MUST fit within budget!
	effectiveBudget := int(float64(cfg.Agent.ContextWindow)*0.75) - cfg.Agent.MaxTokens
	if effectiveBudget < 2048 {
		effectiveBudget = 2048
	}

	if totalTokens > effectiveBudget {
		t.Errorf("Total prompt tokens (%d) exceed effective budget (%d)! sysTokens=%d, userTokens=%d",
			totalTokens, effectiveBudget, sysTokens, userTokens)
	}
	t.Logf("PASS: Oversized system prompt truncated successfully: sysTokens=%d, userTokens=%d, total=%d <= budget=%d",
		sysTokens, userTokens, totalTokens, effectiveBudget)
}

func TestConcurrentMultiToolExecution(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	file3 := filepath.Join(tmpDir, "file3.txt")
	_ = os.WriteFile(file1, []byte("content 1"), 0644)
	_ = os.WriteFile(file2, []byte("content 2"), 0644)
	_ = os.WriteFile(file3, []byte("content 3"), 0644)

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			// Iteration 0: Emit 3 concurrent read_file calls
			resp := provider.CompletionResponse{
				Choices: []provider.Choice{
					{
						Index: 0,
						Message: provider.Message{
							Role: "assistant",
							ToolCalls: []provider.ToolCall{
								{
									Index: 0,
									ID:    "call_read_1",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "read_file",
										Arguments: fmt.Sprintf(`{"path":%q}`, file1),
									},
								},
								{
									Index: 1,
									ID:    "call_read_2",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "read_file",
										Arguments: fmt.Sprintf(`{"path":%q}`, file2),
									},
								},
								{
									Index: 2,
									ID:    "call_read_3",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "read_file",
										Arguments: fmt.Sprintf(`{"path":%q}`, file3),
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Iteration 1: Return final response
		resp := provider.CompletionResponse{
			Choices: []provider.Choice{
				{
					Index: 0,
					Message: provider.Message{
						Role:    "assistant",
						Content: "All 3 files read concurrently!",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Agent.Workspace = tmpDir
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Tools.ConfirmTools = []string{}
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(1024))

	loop := NewLoop(cfg, client, registry, sessions, store, false)
	sess := &session.Session{ID: "concurrent-test-sess"}

	res, err := loop.Run(context.Background(), "read all 3 files", sess, nil)
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	if res != "All 3 files read concurrently!" {
		t.Errorf("unexpected agent response: %s", res)
	}

	// Verify exact order of tool responses matches call IDs
	var toolResults []provider.Message
	for _, m := range sess.Messages {
		if m.Role == "tool" {
			toolResults = append(toolResults, m)
		}
	}

	if len(toolResults) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(toolResults))
	}
	expectedIDs := []string{"call_read_1", "call_read_2", "call_read_3"}
	expectedContents := []string{"content 1", "content 2", "content 3"}
	for i := 0; i < 3; i++ {
		if toolResults[i].ToolCallID != expectedIDs[i] {
			t.Errorf("tool result %d: expected ID %q, got %q", i, expectedIDs[i], toolResults[i].ToolCallID)
		}
		if toolResults[i].Content != expectedContents[i] {
			t.Errorf("tool result %d: expected content %q, got %q", i, expectedContents[i], toolResults[i].Content)
		}
	}
}

func TestPatchFileExecutionInAgentLoop(t *testing.T) {
	tmpDir := t.TempDir()
	codeFile := filepath.Join(tmpDir, "code.go")
	_ = os.WriteFile(codeFile, []byte("package main\n\nfunc Version() string {\n\treturn \"1.0\"\n}\n"), 0644)

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			patchArgs, _ := json.Marshal(map[string]interface{}{
				"path":    codeFile,
				"search":  `return "1.0"`,
				"replace": `return "2.0"`,
			})
			resp := provider.CompletionResponse{
				Choices: []provider.Choice{
					{
						Index: 0,
						Message: provider.Message{
							Role: "assistant",
							ToolCalls: []provider.ToolCall{
								{
									Index: 0,
									ID:    "call_patch_1",
									Type:  "function",
									Function: provider.FunctionCall{
										Name:      "patch_file",
										Arguments: string(patchArgs),
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		resp := provider.CompletionResponse{
			Choices: []provider.Choice{
				{
					Index: 0,
					Message: provider.Message{
						Role:    "assistant",
						Content: "Code patched to v2.0 successfully!",
					},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Agent.Workspace = tmpDir
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Tools.ConfirmTools = []string{} // allow patch_file in test
	cfg.Resolve()

	client, err := provider.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	store := NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	registry.Register(tools.NewPatchFileTool())

	loop := NewLoop(cfg, client, registry, sessions, store, false)
	sess := &session.Session{ID: "patch-test-sess"}

	res, err := loop.Run(context.Background(), "patch code to 2.0", sess, nil)
	if err != nil {
		t.Fatalf("loop.Run failed: %v", err)
	}
	if res != "Code patched to v2.0 successfully!" {
		t.Errorf("unexpected agent response: %s", res)
	}

	// Verify file on disk actually got patched
	patchedData, err := os.ReadFile(codeFile)
	if err != nil {
		t.Fatalf("failed to read patched file: %v", err)
	}
	if !strings.Contains(string(patchedData), `return "2.0"`) || strings.Contains(string(patchedData), `return "1.0"`) {
		t.Errorf("file was not patched properly: %s", string(patchedData))
	}
}

