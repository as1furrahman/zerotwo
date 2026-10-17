package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"zerotwo/pkg/auth"
	"zerotwo/pkg/config"
	"zerotwo/pkg/provider"
	"zerotwo/pkg/session"
	"zerotwo/pkg/tools"
)

type Loop struct {
	cfg           *config.Config
	client        *provider.Client
	registry      *tools.Registry
	context       *ContextBuilder
	sessions      *session.Manager
	memory        *MemoryStore
	policy        *auth.Policy
	autoYes       bool
	calibRatio    float64 // calibrated prompt tokens per estimated token
	hasCalibData  bool
	confirmInput  io.Reader
	confirmOutput io.Writer
}

func (l *Loop) SetConfirmIO(in io.Reader, out io.Writer) {
	l.confirmInput = in
	l.confirmOutput = out
}

func NewLoop(
	cfg *config.Config,
	client *provider.Client,
	registry *tools.Registry,
	sessions *session.Manager,
	memory *MemoryStore,
	autoYes bool,
) *Loop {
	ctx := NewContextBuilder(cfg, memory, registry)
	return &Loop{
		cfg:        cfg,
		client:     client,
		registry:   registry,
		context:    ctx,
		sessions:   sessions,
		memory:     memory,
		policy:     auth.NewPolicy(),
		autoYes:    autoYes,
		calibRatio: 1.0,
	}
}

func (l *Loop) ContextBuilder() *ContextBuilder {
	return l.context
}

func (l *Loop) Run(ctx context.Context, userMessage string, sess *session.Session, onToken provider.TokenCallback) (string, error) {
	origLen := len(sess.Messages)
	userMsg := provider.Message{Role: "user", Content: userMessage}
	sess.Messages = append(sess.Messages, userMsg)

	providerTools := l.registry.AsProviderTools()
	maxIter := l.cfg.Agent.MaxIterations
	if maxIter <= 0 {
		maxIter = 15
	}

	completedLen := origLen
	completedToolCalls := 0

	for iter := 0; iter < maxIter; iter++ {
		// 1. Build messages with static system prompt
		fullMsgs := l.context.BuildMessages(sess.Messages)

		// 2. Token-budget context trimming preserving atomic tool pairs
		trimmedMsgs := l.TrimContext(fullMsgs, providerTools)

		req := provider.CompletionRequest{
			Model:       l.cfg.Provider.Model,
			Messages:    trimmedMsgs,
			Tools:       providerTools,
			MaxTokens:   l.cfg.Agent.MaxTokens,
			Temperature: l.cfg.Agent.Temperature,
			TopP:        l.cfg.Agent.TopP,
			Stream:      l.cfg.Provider.Stream,
		}

		// Estimate tokens before call to calibrate ratio later
		estTokensBefore := l.EstimateMessagesTokens(trimmedMsgs)

		resp, err := l.client.Complete(ctx, req, onToken)
		if err != nil {
			if completedLen > origLen {
				sess.Messages = sess.Messages[:completedLen]
				sess.Messages = append(sess.Messages, provider.Message{
					Role:    "assistant",
					Content: fmt.Sprintf("[Execution interrupted after %d completed tool action(s): %v]", completedToolCalls, err),
				})
				_ = l.saveSession(sess)
				return "", fmt.Errorf("agent interrupted after %d completed tool actions: %w", completedToolCalls, err)
			}
			sess.Messages = sess.Messages[:origLen]
			_ = l.saveSession(sess)
			return "", fmt.Errorf("LLM error: %w", err)
		}

		// Update running calibration ratio if provider returned usage
		if resp.Usage != nil && resp.Usage.PromptTokens > 0 && estTokensBefore > 0 {
			observedRatio := float64(resp.Usage.PromptTokens) / float64(estTokensBefore)
			if !l.hasCalibData {
				l.calibRatio = observedRatio
				l.hasCalibData = true
			} else {
				// Moving average: 70% existing, 30% observed
				l.calibRatio = (l.calibRatio * 0.7) + (observedRatio * 0.3)
			}
		}

		if resp == nil || len(resp.Choices) == 0 {
			if completedLen > origLen {
				sess.Messages = sess.Messages[:completedLen]
				sess.Messages = append(sess.Messages, provider.Message{
					Role:    "assistant",
					Content: fmt.Sprintf("[Execution ended: empty choices from LLM after %d completed tool action(s)]", completedToolCalls),
				})
				_ = l.saveSession(sess)
				return "", fmt.Errorf("empty choices from LLM after %d completed tool actions", completedToolCalls)
			}
			sess.Messages = sess.Messages[:origLen]
			_ = l.saveSession(sess)
			return "", fmt.Errorf("empty choices from LLM")
		}

		assistantMsg := resp.Choices[0].Message
		assistantMsg.Role = "assistant"
		if strings.TrimSpace(assistantMsg.Content) == "" && len(assistantMsg.ToolCalls) == 0 {
			if completedLen > origLen {
				sess.Messages = sess.Messages[:completedLen]
				sess.Messages = append(sess.Messages, provider.Message{
					Role:    "assistant",
					Content: fmt.Sprintf("[Execution ended: empty response from LLM after %d completed tool action(s)]", completedToolCalls),
				})
				_ = l.saveSession(sess)
				return "", fmt.Errorf("empty response from LLM after %d completed tool actions", completedToolCalls)
			}
			sess.Messages = sess.Messages[:origLen]
			_ = l.saveSession(sess)
			return "", fmt.Errorf("empty response received from LLM")
		}

		// Check if history or session is currently tainted (out-of-band persistent state or in-band marker)
		isTainted := sess.Tainted || l.IsContextTainted(sess.Messages)
		if isTainted {
			sess.Tainted = true
		}

		// Case 1: Model requested tool calls
		if len(assistantMsg.ToolCalls) > 0 {
			sess.Messages = append(sess.Messages, assistantMsg)

			for toolIdx, tc := range assistantMsg.ToolCalls {
				// If turn was already cancelled, complete all remaining tool calls so no dangling IDs remain
				if ctx.Err() != nil {
					for k := toolIdx; k < len(assistantMsg.ToolCalls); k++ {
						remTc := assistantMsg.ToolCalls[k]
						sess.Messages = append(sess.Messages, provider.Message{
							Role:       "tool",
							ToolCallID: remTc.ID,
							Content:    fmt.Sprintf("TOOL EXECUTION CANCELLED: request was aborted by user (%v)", ctx.Err()),
						})
					}
					break
				}

				toolName := tc.Function.Name
				toolArgs := strings.TrimSpace(tc.Function.Arguments)
				if toolArgs == "" {
					toolArgs = "{}"
				}

				// Tool arguments JSON pre-validation
				var rawJSON json.RawMessage
				if err := json.Unmarshal([]byte(toolArgs), &rawJSON); err != nil {
					toolResult := fmt.Sprintf("TOOL EXECUTION ERROR: malformed JSON arguments from model: %v (raw: %q)", err, toolArgs)
					sess.Messages = append(sess.Messages, provider.Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    toolResult,
					})
					continue
				}

				// Tool confirmation check via centralized authorization policy
				if err := l.checkToolConfirmation(ctx, toolName, toolArgs, isTainted); err != nil {
					// User denied or failed closed -> feed observation to model
					toolResult := fmt.Sprintf("TOOL EXECUTION DENIED BY USER: %v", err)
					sess.Messages = append(sess.Messages, provider.Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    toolResult,
					})
					continue
				}

				// Execute tool with context support
				toolResult := l.registry.DispatchContext(ctx, toolName, toolArgs)

				// Check if tool introduced untrusted external content or remote egress
				if strings.Contains(toolResult, "<<<UNTRUSTED_EXTERNAL_CONTENT") ||
					toolName == "web_fetch" || toolName == "http_request" || toolName == "web_search" {
					isTainted = true
					sess.Tainted = true
				}

				// If write_file was executed during a tainted turn, mark the target file as tainted
				// so reading it back later preserves the taint barrier and prevents text laundering!
				if toolName == "write_file" && isTainted {
					var writeArgs struct {
						Path string `json:"path"`
					}
					if err := json.Unmarshal([]byte(toolArgs), &writeArgs); err == nil && writeArgs.Path != "" {
						tools.MarkFileTainted(writeArgs.Path)
					}
				}

				sess.Messages = append(sess.Messages, provider.Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    toolResult,
				})

				// If execution of this tool got cancelled, complete any remaining tools in the batch
				if ctx.Err() != nil && toolIdx+1 < len(assistantMsg.ToolCalls) {
					for k := toolIdx + 1; k < len(assistantMsg.ToolCalls); k++ {
						remTc := assistantMsg.ToolCalls[k]
						sess.Messages = append(sess.Messages, provider.Message{
							Role:       "tool",
							ToolCallID: remTc.ID,
							Content:    fmt.Sprintf("TOOL EXECUTION CANCELLED: request was aborted by user (%v)", ctx.Err()),
						})
					}
					break
				}
			}

			// Save session state after tool actions
			_ = l.saveSession(sess)
			completedLen = len(sess.Messages)
			completedToolCalls += len(assistantMsg.ToolCalls)
			if ctx.Err() != nil {
				// Abort turn cleanly after satisfying all tool calls
				sess.Messages = append(sess.Messages, provider.Message{
					Role:    "assistant",
					Content: fmt.Sprintf("[Execution interrupted after %d tool action(s): %v]", completedToolCalls, ctx.Err()),
				})
				_ = l.saveSession(sess)
				return "", fmt.Errorf("agent interrupted during tool execution: %w", ctx.Err())
			}
			continue
		}

		// Case 2: Model completed with final text
		sess.Messages = append(sess.Messages, assistantMsg)
		_ = l.saveSession(sess)
		return assistantMsg.Content, nil
	}

	if completedLen > origLen {
		sess.Messages = sess.Messages[:completedLen]
		sess.Messages = append(sess.Messages, provider.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("[Execution stopped: reached maximum iterations (%d) after %d completed tool action(s)]", maxIter, completedToolCalls),
		})
		_ = l.saveSession(sess)
		return "", fmt.Errorf("agent exceeded max iterations (%d) after %d completed tool actions", maxIter, completedToolCalls)
	}
	sess.Messages = sess.Messages[:origLen]
	_ = l.saveSession(sess)
	return "", fmt.Errorf("agent exceeded max iterations (%d)", maxIter)
}

func (l *Loop) saveSession(sess *session.Session) error {
	if l.sessions == nil || sess == nil {
		return nil
	}
	if err := l.sessions.Save(sess); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to persist session %s: %v\n", sess.ID, err)
		return err
	}
	return nil
}

func (l *Loop) IsContextTainted(messages []provider.Message) bool {
	for _, m := range messages {
		if strings.Contains(m.Content, "<<<UNTRUSTED_EXTERNAL_CONTENT") {
			return true
		}
	}
	return false
}

func (l *Loop) checkToolConfirmation(ctx context.Context, name, args string, isTainted bool) error {
	req := auth.AuthorizationRequest{
		ToolName:        name,
		ArgsJSON:        args,
		ContextTainted:  isTainted,
		AutoYes:         l.autoYes,
		AllowShell:      l.cfg.Tools.AllowShell,
		Workspace:       l.cfg.Agent.Workspace,
		BlockedCommands: l.cfg.Tools.BlockedCommands,
		ConfirmTools:    l.cfg.Tools.ConfirmTools,
	}

	dec, details, err := l.policy.Authorize(req)
	switch dec {
	case auth.DecisionAllow:
		return nil
	case auth.DecisionDeny:
		if err != nil {
			return err
		}
		return fmt.Errorf("action denied by security policy: %s", details.Warning)
	case auth.DecisionRequireStrongApproval:
		return l.promptDestructiveConfirmationWithDetails(ctx, details)
	case auth.DecisionRequireApproval:
		return l.promptUserConfirmationWithDetails(ctx, details)
	default:
		return fmt.Errorf("unrecognized authorization decision: %v", dec)
	}
}

func (l *Loop) promptDestructiveConfirmation(ctx context.Context, name, args, reason string) error {
	details := auth.ApprovalDetails{
		Category:       auth.CategoryDestructive,
		Action:         name,
		Target:         args,
		Warning:        reason,
		RequiresStrong: true,
	}
	return l.promptDestructiveConfirmationWithDetails(ctx, details)
}

func (l *Loop) promptDestructiveConfirmationWithDetails(ctx context.Context, details auth.ApprovalDetails) error {
	var r io.Reader
	var w io.Writer
	var closer io.Closer
	if l.confirmInput != nil {
		r = l.confirmInput
		w = l.confirmOutput
		if w == nil {
			w = io.Discard
		}
	} else {
		// Read strictly from /dev/tty to avoid stdin pipe hangs
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("denied: cannot open /dev/tty (no controlling terminal): %w", err)
		}
		closer = tty
		r = tty
		w = tty
	}
	if closer != nil {
		defer closer.Close()
	}

	fmt.Fprintf(w, "\n[CRITICAL WARNING: Destructive Action Detected!]\n")
	if details.Action != "" {
		fmt.Fprintf(w, "Action: %s\n", details.Action)
	}
	if details.Target != "" {
		fmt.Fprintf(w, "Target: %s\n", details.Target)
	}
	if details.DataScope != "" {
		fmt.Fprintf(w, "Scope:  %s\n", details.DataScope)
	}
	if details.Warning != "" {
		fmt.Fprintf(w, "Notice: %s\n", details.Warning)
	}
	fmt.Fprintf(w, "Type 'yes' to execute | any other input to cancel: ")

	input, err := readLineWithContext(ctx, r)
	if err != nil {
		return err
	}

	ans := strings.TrimSpace(input)
	if ans == "yes" {
		return nil
	}
	return fmt.Errorf("destructive command denied by user")
}

func (l *Loop) promptUserConfirmation(ctx context.Context, name, args string, fromTaint bool) error {
	details := auth.ApprovalDetails{
		Action: name,
		Target: args,
	}
	if fromTaint {
		details.Warning = "Context is tainted by untrusted external data or references protected resources"
	}
	return l.promptUserConfirmationWithDetails(ctx, details)
}

func (l *Loop) promptUserConfirmationWithDetails(ctx context.Context, details auth.ApprovalDetails) error {
	var r io.Reader
	var w io.Writer
	var closer io.Closer
	if l.confirmInput != nil {
		r = l.confirmInput
		w = l.confirmOutput
		if w == nil {
			w = io.Discard
		}
	} else {
		// Read strictly from /dev/tty to avoid stdin pipe hangs
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("denied: cannot open /dev/tty (no controlling terminal): %w", err)
		}
		closer = tty
		r = tty
		w = tty
	}
	if closer != nil {
		defer closer.Close()
	}

	prefix := "[Tool Authorization Required]"
	if details.Category != "" {
		prefix = fmt.Sprintf("[Tool Authorization Required: %s]", details.Category)
	}

	fmt.Fprintf(w, "\n%s\n", prefix)
	if details.Action != "" {
		fmt.Fprintf(w, "Action: %s\n", details.Action)
	}
	if details.Target != "" {
		fmt.Fprintf(w, "Target: %s\n", details.Target)
	}
	if details.DataScope != "" {
		fmt.Fprintf(w, "Scope:  %s\n", details.DataScope)
	}
	if details.Warning != "" {
		fmt.Fprintf(w, "Notice: %s\n", details.Warning)
	}
	fmt.Fprintf(w, "Confirm execution? [y/N]: ")

	input, err := readLineWithContext(ctx, r)
	if err != nil {
		return err
	}

	ans := strings.TrimSpace(input)
	if strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes") {
		return nil
	}
	return fmt.Errorf("action denied by user")
}

// readLineWithContext reads a single newline-terminated line from r respecting ctx cancellation.
// For deadline-capable streams (*os.File on Linux, including /dev/tty and os.Pipe), it reads synchronously
// with a brief deadline, completely avoiding background reader goroutines that could linger and swallow
// subsequent keystrokes in interactive REPL sessions.
func readLineWithContext(ctx context.Context, r io.Reader) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if deadlineable, ok := r.(interface{ SetReadDeadline(time.Time) error }); ok {
		defer deadlineable.SetReadDeadline(time.Time{})
		var line []byte
		oneByte := make([]byte, 1)
		for {
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("confirmation cancelled: %w", ctx.Err())
			default:
			}

			_ = deadlineable.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			n, err := r.Read(oneByte)
			if n > 0 {
				if oneByte[0] == '\n' {
					break
				}
				line = append(line, oneByte[0])
			}
			if err != nil {
				if os.IsTimeout(err) || errors.Is(err, os.ErrDeadlineExceeded) {
					continue
				}
				if err == io.EOF {
					if len(line) > 0 {
						break
					}
					return "", io.EOF
				}
				return "", err
			}
		}
		return string(line), nil
	}

	// Fallback for non-deadline readers (such as in-memory buffers or io.Pipe in unit tests)
	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		reader := bufio.NewReader(r)
		line, err := reader.ReadString('\n')
		ch <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("confirmation cancelled: %w", ctx.Err())
	case res := <-ch:
		if res.err != nil && res.err != io.EOF {
			return "", res.err
		}
		return res.line, nil
	}
}

// ── Token Estimation & Trimming ───────────────────────────────────────────────

// EstimateTokenCount estimates tokens using script-aware rune weighting.
// Assumption: ASCII runes count as ~0.25 tokens (chars/4); non-ASCII runes count as ~0.65 tokens (factor 2.5).
func EstimateTokenCount(text string) int {
	var total float64
	for _, r := range text {
		if r <= unicode.MaxASCII {
			total += 0.25
		} else {
			total += 0.65 // script-aware non-ASCII weight
		}
	}
	tokens := int(total + 0.5)
	if tokens == 0 && len(text) > 0 {
		return 1
	}
	return tokens
}

func (l *Loop) EstimateMessagesTokens(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokenCount(m.Role)
		total += EstimateTokenCount(m.Content)
		for _, tc := range m.ToolCalls {
			total += EstimateTokenCount(tc.Function.Name)
			total += EstimateTokenCount(tc.Function.Arguments)
		}
	}
	if l.hasCalibData && l.calibRatio > 0 {
		return int(float64(total) * l.calibRatio)
	}
	return total
}

func (l *Loop) TrimContext(msgs []provider.Message, tools []provider.Tool) []provider.Message {
	if len(msgs) <= 1 {
		return msgs
	}

	ctxWindow := l.cfg.Agent.ContextWindow
	if ctxWindow <= 0 {
		ctxWindow = 16384
	}
	maxTokens := l.cfg.Agent.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	// Calculate tools tokens budget
	toolsBytes, _ := json.Marshal(tools)
	toolsTokens := EstimateTokenCount(string(toolsBytes))

	// Keep 25% safety margin under context window
	effectiveBudget := int(float64(ctxWindow)*0.75) - maxTokens - toolsTokens
	if effectiveBudget < 2048 {
		effectiveBudget = 2048
	}

	// System prompt at msgs[0] is ALWAYS pinned
	sysPrompt := msgs[0]
	history := msgs[1:]

	sysPromptTokens := EstimateTokenCount(sysPrompt.Content)
	maxSysPromptTokens := effectiveBudget - 500
	if maxSysPromptTokens < 1000 {
		maxSysPromptTokens = effectiveBudget / 2
	}
	if sysPromptTokens > maxSysPromptTokens {
		// Truncate oversized system prompt / memory to fit within its allowed share
		targetSysTokens := maxSysPromptTokens - 50
		if targetSysTokens < 500 {
			targetSysTokens = 500
		}
		runes := []rune(sysPrompt.Content)
		if sysPromptTokens > 0 && len(runes) > 0 {
			keepLen := len(runes) * targetSysTokens / sysPromptTokens
			if keepLen < len(runes) {
				runes = runes[:keepLen]
			}
		}
		sysPrompt.Content = string(runes) + "\n\n[NOTICE: System prompt / memory truncated to fit context budget]"
		sysPromptTokens = EstimateTokenCount(sysPrompt.Content)
	}

	historyBudget := effectiveBudget - sysPromptTokens
	if historyBudget < 200 {
		historyBudget = 200
	}

	// Group history into atomic conversation units
	// Invariant: An assistant message with tool_calls and all its succeeding tool responses MUST STAY TOGETHER!
	units := groupAtomicUnits(history)

	// Calculate total tokens of history
	for {
		currentTokens := sysPromptTokens
		for _, u := range units {
			for _, m := range u {
				currentTokens += EstimateTokenCount(m.Content)
				for _, tc := range m.ToolCalls {
					currentTokens += EstimateTokenCount(tc.Function.Arguments)
				}
			}
		}

		if currentTokens <= effectiveBudget || len(units) <= 1 {
			break
		}

		// Prune oldest atomic unit
		units = units[1:]
	}

	// If after pruning units, the remaining messages still exceed the history budget
	// (e.g. a single message is oversized), truncate the content with an explicit notice.
	for i := range units {
		for j := range units[i] {
			m := &units[i][j]
			msgTokens := EstimateTokenCount(m.Content)
			if msgTokens > historyBudget {
				targetTokens := historyBudget - 100
				if targetTokens < 200 {
					targetTokens = 200
				}
				runes := []rune(m.Content)
				if len(runes) > 0 && msgTokens > 0 {
					keepLen := len(runes) * targetTokens / msgTokens
					if keepLen < len(runes) {
						runes = runes[:keepLen]
					}
				}
				notice := fmt.Sprintf("\n\n[NOTICE: Input exceeded context budget (~%d tokens); truncated to ~%d tokens to fit context window]", msgTokens, targetTokens)
				m.Content = string(runes) + notice
			}
		}
	}

	var trimmed []provider.Message
	trimmed = append(trimmed, sysPrompt)
	for _, u := range units {
		trimmed = append(trimmed, u...)
	}
	return trimmed
}

// groupAtomicUnits binds tool_calls assistant messages and their tool results into an atomic group.
func groupAtomicUnits(history []provider.Message) [][]provider.Message {
	var units [][]provider.Message
	var currentUnit []provider.Message

	for i := 0; i < len(history); i++ {
		msg := history[i]

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			if len(currentUnit) > 0 {
				units = append(units, currentUnit)
				currentUnit = nil
			}
			// Start new atomic tool group
			toolGroup := []provider.Message{msg}
			// Collect matching tool results
			toolCallIDs := make(map[string]bool)
			for _, tc := range msg.ToolCalls {
				toolCallIDs[tc.ID] = true
			}

			for i+1 < len(history) && history[i+1].Role == "tool" {
				i++
				toolGroup = append(toolGroup, history[i])
			}
			units = append(units, toolGroup)
			continue
		}

		if msg.Role == "user" {
			if len(currentUnit) > 0 {
				units = append(units, currentUnit)
				currentUnit = nil
			}
		}

		currentUnit = append(currentUnit, msg)
	}

	if len(currentUnit) > 0 {
		units = append(units, currentUnit)
	}

	return units
}
