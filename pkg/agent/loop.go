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
	"sync"
	"syscall"
	"time"
	"unicode"
	"zerotwo/pkg/auth"
	"zerotwo/pkg/config"
	"zerotwo/pkg/provider"
	"zerotwo/pkg/session"
	"zerotwo/pkg/tools"
	"zerotwo/pkg/ui"
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
		if err != nil && (strings.Contains(err.Error(), "413") || strings.Contains(err.Error(), "Request too large") || strings.Contains(err.Error(), "rate_limit_exceeded")) && len(trimmedMsgs) > 2 {
			// Provider rejected request due to rate limit or token payload ceiling; aggressively trim to system prompt + last message and retry
			retryMsgs := []provider.Message{trimmedMsgs[0], trimmedMsgs[len(trimmedMsgs)-1]}
			req.Messages = retryMsgs
			resp, err = l.client.Complete(ctx, req, onToken)
		}
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

			// Partition tool calls into consecutive execution batches
			var batches []*toolBatch
			for i, tc := range assistantMsg.ToolCalls {
				name := tc.Function.Name
				args := strings.TrimSpace(tc.Function.Arguments)
				if args == "" {
					args = "{}"
				}
				item := &toolCallItem{
					index:    i,
					call:     tc,
					toolName: name,
					toolArgs: args,
				}

				if isConcurrentSafeTool(name) {
					if len(batches) > 0 && batches[len(batches)-1].concurrent {
						batches[len(batches)-1].items = append(batches[len(batches)-1].items, item)
					} else {
						batches = append(batches, &toolBatch{
							concurrent: true,
							items:      []*toolCallItem{item},
						})
					}
				} else {
					batches = append(batches, &toolBatch{
						concurrent: false,
						items:      []*toolCallItem{item},
					})
				}
			}

			for batchIdx, batch := range batches {
				if ctx.Err() != nil {
					l.cancelRemainingBatches(ctx, sess, batches, batchIdx, 0)
					break
				}

				// Authorization and validation step (evaluated sequentially on /dev/tty)
				for itemIdx, item := range batch.items {
					if ctx.Err() != nil {
						l.cancelRemainingBatches(ctx, sess, batches, batchIdx, itemIdx)
						break
					}

					var rawJSON json.RawMessage
					if err := json.Unmarshal([]byte(item.toolArgs), &rawJSON); err != nil {
						item.failed = true
						item.result = fmt.Sprintf("TOOL EXECUTION ERROR: malformed JSON arguments from model: %v (raw: %q)", err, item.toolArgs)
						continue
					}

					if err := l.checkToolConfirmation(ctx, item.toolName, item.toolArgs, isTainted); err != nil {
						item.failed = true
						item.result = fmt.Sprintf("TOOL EXECUTION DENIED BY USER: %v", err)
						continue
					}
				}

				if ctx.Err() != nil {
					break
				}

				// Execution step: concurrent dispatch for safe read-only batches, sequential for mutating batches
				if batch.concurrent && len(batch.items) > 1 {
					sem := make(chan struct{}, maxConcurrentTools)
					var wg sync.WaitGroup
					for _, item := range batch.items {
						if item.failed {
							continue
						}
						wg.Add(1)
						go func(it *toolCallItem) {
							defer wg.Done()
							sem <- struct{}{}
							defer func() { <-sem }()
							it.result = l.registry.DispatchContext(ctx, it.toolName, it.toolArgs)
						}(item)
					}
					wg.Wait()
				} else {
					for _, item := range batch.items {
						if item.failed {
							continue
						}
						item.result = l.registry.DispatchContext(ctx, item.toolName, item.toolArgs)
					}
				}

				// Post-execution step: collect results in exact original order, update taint, and append tool messages
				for _, item := range batch.items {
					toolResult := item.result

					if ui.IsStdoutTerminal() {
						fmt.Print(ui.QuickhackCard(item.toolName, item.toolArgs, toolResult))
					}

					// Check if tool introduced untrusted external content or remote egress
					if strings.Contains(toolResult, "<<<UNTRUSTED_EXTERNAL_CONTENT") ||
						item.toolName == "web_fetch" || item.toolName == "http_request" || item.toolName == "web_search" {
						isTainted = true
						sess.Tainted = true
					}

					// If write_file or patch_file was executed during a tainted turn, mark the target file as tainted
					if (item.toolName == "write_file" || item.toolName == "patch_file") && isTainted {
						var writeArgs struct {
							Path string `json:"path"`
						}
						if err := json.Unmarshal([]byte(item.toolArgs), &writeArgs); err == nil && writeArgs.Path != "" {
							tools.MarkFileTainted(writeArgs.Path)
						}
					}

					sess.Messages = append(sess.Messages, provider.Message{
						Role:       "tool",
						ToolCallID: item.call.ID,
						Content:    toolResult,
					})
				}

				if ctx.Err() != nil && batchIdx+1 < len(batches) {
					l.cancelRemainingBatches(ctx, sess, batches, batchIdx+1, 0)
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

	fmt.Fprint(w, ui.DestructiveCyberwareWarning(details.Action, details.Target, details.Warning))

	input, err := readLineWithContext(ctx, r)
	if err != nil {
		return err
	}

	ans := strings.ToLower(strings.TrimSpace(input))
	if ans == "yes" || ans == "darling" {
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

// isFdReadable checks if fd has data ready to read, with a given timeout.
// It uses syscall.Select without modifying any file descriptor flags (O_NONBLOCK is never touched).
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

// readLineWithContext reads a single newline-terminated line from r respecting ctx cancellation.
// For *os.File handles (/dev/tty, stdin, ptys, pipes), it uses select-based polling without modifying
// any terminal flags (O_NONBLOCK is NEVER set or altered), completely eliminating orphaned background
// reader goroutines while ensuring file descriptor flags remain unchanged across cancellations and exits.
//
// Partial Input Design Note:
// When a confirmation prompt is cancelled (e.g. user presses Ctrl-C / SIGINT), partial keystrokes
// are discarded and do NOT survive to the next prompt. This prevents accidental execution of subsequent
// actions (e.g. leftover 'y' keystrokes from an aborted prompt silently auto-confirming a subsequent prompt).
// Additionally, on real POSIX terminals in canonical mode (ICANON), the kernel tty line discipline automatically
// flushes input queues on VINTR (SIGINT), ensuring discarded partial input matches POSIX terminal behavior.
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
		if res.err != nil && res.err != io.EOF {
			return res.line, res.err
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

const maxConcurrentTools = 8

// isConcurrentSafeTool returns whether a tool is read-only and safe for concurrent dispatch.
func isConcurrentSafeTool(name string) bool {
	switch name {
	case "read_file", "list_dir", "get_system_status", "web_fetch", "web_search":
		return true
	default:
		return false
	}
}

type toolCallItem struct {
	index    int
	call     provider.ToolCall
	toolName string
	toolArgs string
	failed   bool
	result   string
}

type toolBatch struct {
	concurrent bool
	items      []*toolCallItem
}

func (l *Loop) cancelRemainingBatches(ctx context.Context, sess *session.Session, batches []*toolBatch, fromBatch, fromItem int) {
	for b := fromBatch; b < len(batches); b++ {
		startItem := 0
		if b == fromBatch {
			startItem = fromItem
		}
		for i := startItem; i < len(batches[b].items); i++ {
			remTc := batches[b].items[i].call
			sess.Messages = append(sess.Messages, provider.Message{
				Role:       "tool",
				ToolCallID: remTc.ID,
				Content:    fmt.Sprintf("TOOL EXECUTION CANCELLED: request was aborted by user (%v)", ctx.Err()),
			})
		}
	}
}
