package provider

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"zerotwo/pkg/config"
)

// ── Wire Types ────────────────────────────────────────────────────────────────

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Stream      bool      `json:"stream"`
}

type CompletionResponse struct {
	ID      string   `json:"id,omitempty"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ── Client ────────────────────────────────────────────────────────────────────

type TokenCallback func(token string)

type Client struct {
	cfg        *config.Config
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func NewClient(cfg *config.Config) (*Client, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if cfg.Provider.CACertFile != "" {
		caCert, err := os.ReadFile(cfg.Provider.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("reading ca_cert_file: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", cfg.Provider.CACertFile)
		}
		tlsConfig.RootCAs = caPool
	}

	dialTimeout := time.Duration(cfg.Timeouts.DialSeconds) * time.Second
	tlsTimeout := time.Duration(cfg.Timeouts.TLSHandshakeSeconds) * time.Second
	respHeaderTimeout := time.Duration(cfg.Timeouts.ResponseHeaderSeconds) * time.Second
	totalTimeout := time.Duration(cfg.Timeouts.TotalSeconds) * time.Second

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: respHeaderTimeout,
		TLSClientConfig:       tlsConfig,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   totalTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("stopped after 3 redirects")
			}
			if len(via) > 0 && !strings.EqualFold(via[0].URL.Host, req.URL.Host) {
				return fmt.Errorf("provider redirect to external host %q denied to prevent credential leakage", req.URL.Host)
			}
			return nil
		},
	}

	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
		baseURL:    strings.TrimRight(cfg.Provider.BaseURL, "/"),
		apiKey:     cfg.Provider.APIKey,
		model:      cfg.Provider.Model,
	}, nil
}

func (c *Client) Complete(ctx context.Context, req CompletionRequest, onToken TokenCallback) (*CompletionResponse, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	// 1. Clock check against build floor
	if now := time.Now().UTC(); now.Before(BuildTimeFloor()) {
		fmt.Fprintf(os.Stderr, "WARNING: System clock (%s) is earlier than build time (%s). Embedded boards without an RTC fail TLS validation. Update your clock with 'date -s \"YYYY-MM-DD HH:MM:SS\"' or NTP.\n",
			now.Format(time.RFC3339), BuildTimeFloor().Format(time.RFC3339))
	}

	maxRetries := 3
	var lastErr error
	var hasStreamedOutput bool

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Check if output was already streamed to the caller — DO NOT retry if so!
			if hasStreamedOutput {
				return nil, fmt.Errorf("network error during stream: %w (retry aborted because output was already produced)", lastErr)
			}
		}

		resp, streamed, retryable, err := c.doRequest(ctx, req, onToken)
		if err == nil {
			return resp, nil
		}
		if streamed {
			hasStreamedOutput = true
		}
		lastErr = err

		if !retryable || attempt == maxRetries || ctx.Err() != nil {
			break
		}

		backoff := c.computeBackoff(attempt, lastErr)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}

	// Run network diagnostics on final failure
	diagMsg := c.diagnoseNetworkError(lastErr)
	if diagMsg != "" {
		return nil, fmt.Errorf("%w\n\nDIAGNOSTIC GUIDANCE:\n%s", lastErr, diagMsg)
	}

	return nil, lastErr
}

func (c *Client) doRequest(ctx context.Context, req CompletionRequest, onToken TokenCallback) (*CompletionResponse, bool, bool, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, false, false, fmt.Errorf("marshalling request: %w", err)
	}

	endpoint := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, false, false, fmt.Errorf("creating http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("User-Agent", "ZeroTwo-Agent/1.0 (Linux; Termux)")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, false, true, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		retryAfter := resp.Header.Get("Retry-After")
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, false, true, &HTTPStatusError{
			StatusCode: resp.StatusCode,
			RetryAfter: retryAfter,
			Body:       string(body),
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, false, false, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	if req.Stream {
		return c.readSSEStream(ctx, resp.Body, onToken)
	}

	var compResp CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&compResp); err != nil {
		return nil, false, false, fmt.Errorf("decoding response: %w", err)
	}
	if compResp.Error != nil && compResp.Error.Message != "" {
		return nil, false, false, fmt.Errorf("API error: %s", compResp.Error.Message)
	}
	for i := range compResp.Choices {
		for j := range compResp.Choices[i].Message.ToolCalls {
			if strings.TrimSpace(compResp.Choices[i].Message.ToolCalls[j].Function.Arguments) == "" {
				compResp.Choices[i].Message.ToolCalls[j].Function.Arguments = "{}"
			}
		}
	}
	return &compResp, false, false, nil
}

type HTTPStatusError struct {
	StatusCode int
	RetryAfter string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e.RetryAfter != "" {
		return fmt.Sprintf("HTTP %d (Retry-After: %s): %s", e.StatusCode, e.RetryAfter, e.Body)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// maxSSELineBytes bounds individual SSE lines to 256 KB to prevent runaway memory allocation.
const maxSSELineBytes = 256 * 1024

func readBoundedSSELine(r *bufio.Reader, maxBytes int) (string, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if len(chunk) > 0 {
			if len(buf)+len(chunk) > maxBytes {
				return "", fmt.Errorf("sse line length exceeded limit of %d bytes", maxBytes)
			}
			buf = append(buf, chunk...)
		}
		if err != nil {
			return string(buf), err
		}
		if !isPrefix {
			break
		}
	}
	return string(buf), nil
}

func (c *Client) readSSEStream(ctx context.Context, body io.Reader, onToken TokenCallback) (*CompletionResponse, bool, bool, error) {
	reader := bufio.NewReader(body)
	idleTimeout := time.Duration(c.cfg.Timeouts.IdleSeconds) * time.Second
	if idleTimeout <= 0 {
		idleTimeout = 45 * time.Second
	}

	var fullContent strings.Builder
	toolCallsMap := make(map[int]*ToolCall)
	var finishReason string
	var usage *Usage
	hasStreamed := false

	for {
		type readResult struct {
			line string
			err  error
		}
		ch := make(chan readResult, 1)

		go func() {
			line, err := readBoundedSSELine(reader, maxSSELineBytes)
			ch <- readResult{line: line, err: err}
		}()

		select {
		case <-ctx.Done():
			return nil, hasStreamed, false, ctx.Err()
		case <-time.After(idleTimeout):
			return nil, hasStreamed, false, fmt.Errorf("stream idle timeout after %v with no data", idleTimeout)
		case res := <-ch:
			line := strings.TrimSpace(res.line)
			if line != "" && !strings.HasPrefix(line, ":") && strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "[DONE]" {
					goto Done
				}

				var chunk struct {
					Choices []struct {
						Index int `json:"index"`
						Delta struct {
							Role      string     `json:"role"`
							Content   string     `json:"content"`
							ToolCalls []ToolCall `json:"tool_calls"`
						} `json:"delta"`
						FinishReason string `json:"finish_reason"`
					} `json:"choices"`
					Usage *Usage `json:"usage"`
				}

				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					continue
				}

				if chunk.Usage != nil {
					usage = chunk.Usage
				}

				if len(chunk.Choices) > 0 {
					choice := chunk.Choices[0]
					if choice.FinishReason != "" {
						finishReason = choice.FinishReason
					}
					if choice.Delta.Content != "" {
						hasStreamed = true
						fullContent.WriteString(choice.Delta.Content)
						if onToken != nil {
							onToken(choice.Delta.Content)
						}
					}

					for _, tc := range choice.Delta.ToolCalls {
						idx := tc.Index
						existing, ok := toolCallsMap[idx]

						// Disambiguate parallel tool calls with omitted or colliding indexes (common in Ollama streams)
						if ok && existing != nil {
							hasNewID := tc.ID != "" && existing.ID != "" && tc.ID != existing.ID
							argsTrimmed := strings.TrimSpace(existing.Function.Arguments)
							argsComplete := argsTrimmed != "" && json.Valid([]byte(argsTrimmed))
							hasNewCall := tc.Function.Name != "" && existing.Function.Name != "" && (tc.Function.Name != existing.Function.Name || argsComplete)
							if hasNewID || hasNewCall {
								idx = len(toolCallsMap)
								existing = nil
								ok = false
							}
						}

						if !ok {
							callID := tc.ID
							if callID == "" {
								callID = fmt.Sprintf("call_%d", idx)
							}
							toolCallsMap[idx] = &ToolCall{
								Index: idx,
								ID:    callID,
								Type:  tc.Type,
								Function: FunctionCall{
									Name:      tc.Function.Name,
									Arguments: tc.Function.Arguments,
								},
							}
						} else {
							if tc.ID != "" {
								existing.ID = tc.ID
							}
							if tc.Type != "" {
								existing.Type = tc.Type
							}
							if tc.Function.Name != "" {
								existing.Function.Name = tc.Function.Name
							}
							existing.Function.Arguments += tc.Function.Arguments
						}
					}
				}
			}

			if res.err != nil {
				if errors.Is(res.err, io.EOF) {
					goto Done
				}
				return nil, hasStreamed, true, res.err
			}
		}
	}

Done:
	var toolCalls []ToolCall
	if len(toolCallsMap) > 0 {
		indices := make([]int, 0, len(toolCallsMap))
		for idx := range toolCallsMap {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		for _, idx := range indices {
			tc := toolCallsMap[idx]
			if tc.ID == "" {
				tc.ID = fmt.Sprintf("call_%d", idx)
			}
			if tc.Type == "" {
				tc.Type = "function"
			}
			if strings.TrimSpace(tc.Function.Arguments) == "" {
				tc.Function.Arguments = "{}"
			}
			toolCalls = append(toolCalls, *tc)
		}
	}

	resp := &CompletionResponse{
		Choices: []Choice{
			{
				Message: Message{
					Role:      "assistant",
					Content:   fullContent.String(),
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: usage,
	}
	return resp, hasStreamed, false, nil
}

func (c *Client) computeBackoff(attempt int, err error) time.Duration {
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) && httpErr.RetryAfter != "" {
		if secs, parseErr := strconv.Atoi(httpErr.RetryAfter); parseErr == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
		if t, parseErr := http.ParseTime(httpErr.RetryAfter); parseErr == nil {
			wait := time.Until(t)
			if wait > 0 && wait < 60*time.Second {
				return wait
			}
		}
	}

	base := 1 * time.Second
	mult := 1 << attempt
	var b [2]byte
	_, _ = rand.Read(b[:])
	jitterMs := (int(b[0])<<8 | int(b[1])) % 500
	jitter := time.Duration(jitterMs) * time.Millisecond
	backoff := (base * time.Duration(mult)) + jitter
	if backoff > 30*time.Second {
		backoff = 30 * time.Second
	}
	return backoff
}

func (c *Client) diagnoseNetworkError(err error) string {
	if err == nil {
		return ""
	}

	var sb strings.Builder

	// Check x509 cert errors
	var certInvErr x509.CertificateInvalidError
	if errors.As(err, &certInvErr) {
		now := time.Now().UTC()
		sb.WriteString(fmt.Sprintf("• TLS Certificate Validation Failed: System time is %s.\n  The server certificate may not be valid for this date. If your device has no hardware RTC, set the system clock: date -s \"YYYY-MM-DD HH:MM:SS\"\n", now.Format(time.RFC3339)))
	}

	var authErr x509.UnknownAuthorityError
	if errors.As(err, &authErr) {
		sb.WriteString("• Missing CA Certificates: The system could not verify the provider's certificate authority.\n  Ensure CA certificates are installed ('apt-get install ca-certificates' / 'apk add ca-certificates') or specify \"ca_cert_file\" in config.json.\n")
	}

	// Check DNS resolution
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		sb.WriteString(fmt.Sprintf("• DNS Lookup Failed for %s.\n", dnsErr.Name))
		if _, statErr := os.Stat("/etc/resolv.conf"); os.IsNotExist(statErr) {
			sb.WriteString("  /etc/resolv.conf is missing (common in minimal chroots/containers).\n  Fix by adding a DNS nameserver, e.g.: echo \"nameserver 1.1.1.1\" > /etc/resolv.conf\n")
		} else if data, readErr := os.ReadFile("/etc/resolv.conf"); readErr == nil && len(strings.TrimSpace(string(data))) == 0 {
			sb.WriteString("  /etc/resolv.conf is empty. Add a nameserver: echo \"nameserver 1.1.1.1\" > /etc/resolv.conf\n")
		}
	}

	// Check URL schema
	if u, parseErr := url.Parse(c.baseURL); parseErr == nil && u.Scheme != "http" && u.Scheme != "https" {
		sb.WriteString(fmt.Sprintf("• Invalid BaseURL schema: %q. Must begin with http:// or https://\n", c.baseURL))
	}

	return sb.String()
}
