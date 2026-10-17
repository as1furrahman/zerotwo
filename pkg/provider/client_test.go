package provider

import (
	"bytes"
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"zerotwo/pkg/config"
)

func TestClientRetriesAndBackoff(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att < 3 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"success"}}]}`)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = false
	cfg.Resolve()

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, nil)

	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if resp.Choices[0].Message.Content != "success" {
		t.Errorf("expected 'success', got %q", resp.Choices[0].Message.Content)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestStreamAssembleToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		chunks := []string{
			`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello "}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"world!"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"shell","arguments":"{\"command\":"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]}}]}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = true
	cfg.Resolve()

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var streamedTokens strings.Builder
	resp, err := client.Complete(context.Background(), CompletionRequest{
		Stream:   true,
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(token string) {
		streamedTokens.WriteString(token)
	})

	if err != nil {
		t.Fatalf("Complete stream: %v", err)
	}

	if streamedTokens.String() != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", streamedTokens.String())
	}

	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}

	tc := resp.Choices[0].Message.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "shell" || tc.Function.Arguments != `{"command":"ls"}` {
		t.Errorf("assembled tool call mismatch: %+v", tc)
	}
}

func TestListModels(t *testing.T) {
	// 1. Test {"data": [{"id": "models/qwen2.5"}]}
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"id":"models/qwen2.5"},{"id":"gpt-4o"}]}`)
	}))
	defer server1.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server1.URL
	client1, _ := NewClient(cfg)
	models1, err := client1.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models1) != 2 || models1[0] != "qwen2.5" || models1[1] != "gpt-4o" {
		t.Errorf("unexpected models: %v", models1)
	}

	// 2. Test bare array: ["llama-3", "mistral"]
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `["llama-3", "models/mistral"]`)
	}))
	defer server2.Close()

	cfg.Provider.BaseURL = server2.URL
	client2, _ := NewClient(cfg)
	models2, err := client2.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels bare: %v", err)
	}
	if len(models2) != 2 || models2[0] != "llama-3" || models2[1] != "mistral" {
		t.Errorf("unexpected models bare: %v", models2)
	}
}

func TestIdleWatchdogTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		// Send initial chunk
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"start\"}}]}\n\n")
		flusher.Flush()
		// Sleep for longer than client's idle timeout (set to 1s in config)
		time.Sleep(1500 * time.Millisecond)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"after delay\"}}]}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = true
	cfg.Timeouts.IdleSeconds = 1 // 1 second idle timeout for test
	cfg.Resolve()

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Complete(context.Background(), CompletionRequest{
		Stream:   true,
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(token string) {})

	if err == nil {
		t.Fatalf("expected stream idle timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "stream idle timeout") {
		t.Errorf("expected 'stream idle timeout' error, got %v", err)
	}
}

func TestSlowStreamSuccessAndNoRetryOnStreamedOutput(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		// Simulate slow LAN inference (e.g. Ollama CPU: chunks arriving every 80ms)
		// which is safely within 2s idle timeout
		chunks := []string{"Slow ", "LAN ", "Ollama "}
		for _, c := range chunks {
			time.Sleep(80 * time.Millisecond)
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", c)
			flusher.Flush()
		}

		if att == 1 {
			// Simulate connection abrupt disconnect / error after streaming started
			conn, _, _ := w.(http.Hijacker).Hijack()
			if conn != nil {
				conn.Close()
			}
			return
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = true
	cfg.Timeouts.IdleSeconds = 2
	cfg.Resolve()

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var collected strings.Builder
	_, err = client.Complete(context.Background(), CompletionRequest{
		Stream:   true,
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(token string) {
		collected.WriteString(token)
	})
	if err == nil {
		t.Errorf("expected stream idle timeout error, got nil")
	}
	if collected.String() != "Slow LAN Ollama " {
		t.Errorf("expected 'Slow LAN Ollama ', got %q", collected.String())
	}

	// CRITICAL FIX 1 REQUIREMENT: Do NOT retry a request that already produced streamed output!
	// Attempts must strictly be 1
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("CRITICAL DEFECT: retried a request that already produced streamed tokens! attempts=%d", attempts)
	}
}

func TestNetworkErrorDiagnostics(t *testing.T) {
	cfg := config.DefaultConfig()
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// 1. Test x509.CertificateInvalidError -> reports clock skew & date command
	certErr := x509.CertificateInvalidError{
		Cert:   &x509.Certificate{},
		Reason: x509.Expired,
	}
	diag := client.diagnoseNetworkError(certErr)
	if !strings.Contains(diag, "TLS Certificate Validation Failed") || !strings.Contains(diag, "hardware RTC") {
		t.Errorf("expected clock skew diagnostic for CertificateInvalidError, got: %s", diag)
	}

	// 2. Test x509.UnknownAuthorityError -> reports missing CA certificates
	authErr := x509.UnknownAuthorityError{Cert: &x509.Certificate{}}
	diagAuth := client.diagnoseNetworkError(authErr)
	if !strings.Contains(diagAuth, "Missing CA Certificates") || !strings.Contains(diagAuth, "ca-certificates") {
		t.Errorf("expected missing CA certificates diagnostic, got: %s", diagAuth)
	}

	// 3. Test net.DNSError -> reports DNS failure
	dnsErr := &net.DNSError{Name: "nonexistent.api.provider.test"}
	diagDNS := client.diagnoseNetworkError(dnsErr)
	if !strings.Contains(diagDNS, "DNS Lookup Failed") {
		t.Errorf("expected DNS lookup failure diagnostic, got: %s", diagDNS)
	}
}

func TestBuildTimeFloorFallback(t *testing.T) {
	orig := BuildTime
	defer func() { BuildTime = orig }()

	// Test 1: Valid RFC3339 time
	BuildTime = "2026-06-15T12:00:00Z"
	floor := BuildTimeFloor()
	if floor.Year() != 2026 || floor.Month() != 6 || floor.Day() != 15 {
		t.Errorf("expected 2026-06-15, got %v", floor)
	}

	// Test 2: Empty BuildTime (plain go build without -ldflags)
	BuildTime = ""
	floorEmpty := BuildTimeFloor()
	if floorEmpty.Year() != 2025 || floorEmpty.Month() != 1 || floorEmpty.Day() != 1 {
		t.Errorf("expected fallback 2025-01-01 for empty BuildTime, got %v", floorEmpty)
	}

	// Test 3: Malformed BuildTime
	BuildTime = "corrupted-date-string"
	floorCorrupt := BuildTimeFloor()
	if floorCorrupt.Year() != 2025 || floorCorrupt.Month() != 1 || floorCorrupt.Day() != 1 {
		t.Errorf("expected fallback 2025-01-01 for corrupt BuildTime, got %v", floorCorrupt)
	}
}

func TestStreamOllamaQuirks(t *testing.T) {
	// Simulates Ollama's SSE stream with:
	// 1. Whole tool call in one chunk
	// 2. Missing index field
	// 3. Parallel tool calls with colliding index 0 (must not merge!)
	// 4. finish_reason: "stop" instead of "tool_calls"
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"id":"call_ollama_1","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"delta":{"tool_calls":[{"id":"call_ollama_2","function":{"name":"get_system_status","arguments":"{}"}}]},"finish_reason":"stop"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = true
	cfg.Resolve()

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := client.Complete(context.Background(), CompletionRequest{
		Stream:   true,
		Messages: []Message{{Role: "user", Content: "check status"}},
	}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatalf("expected at least 1 choice")
	}

	tcs := resp.Choices[0].Message.ToolCalls
	t.Logf("assembled tool calls (%d): %+v", len(tcs), tcs)
	if len(tcs) != 2 {
		t.Fatalf("expected 2 distinct tool calls assembled, got %d (parallel calls without index merged!)", len(tcs))
	}

	if tcs[0].Function.Name != "read_file" || tcs[0].Function.Arguments != `{"path":"a.txt"}` {
		t.Errorf("unexpected first tool call: %+v", tcs[0])
	}
	if tcs[1].Function.Name != "get_system_status" || tcs[1].Function.Arguments != "{}" {
		t.Errorf("unexpected second tool call: %+v", tcs[1])
	}
}

func TestStreamOllamaSameNameCollidingIndex(t *testing.T) {
	// Exact case flagged: two same-name calls (two read_file calls)
	// with NO IDs and colliding index 0 (omitted index).
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"function":{"name":"read_file","arguments":"{\"path\":\"file1.txt\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"delta":{"tool_calls":[{"function":{"name":"read_file","arguments":"{\"path\":\"file2.txt\"}"}}]},"finish_reason":"stop"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.BaseURL = server.URL
	cfg.Provider.Stream = true
	cfg.Resolve()

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := client.Complete(context.Background(), CompletionRequest{
		Stream:   true,
		Messages: []Message{{Role: "user", Content: "read file1 and file2"}},
	}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatalf("expected at least 1 choice")
	}

	tcs := resp.Choices[0].Message.ToolCalls
	if len(tcs) != 2 {
		t.Fatalf("expected 2 distinct tool calls, got %d (identical name calls merged!)", len(tcs))
	}

	if tcs[0].Function.Name != "read_file" || tcs[0].Function.Arguments != `{"path":"file1.txt"}` {
		t.Errorf("unexpected first call: %+v", tcs[0])
	}
	if tcs[1].Function.Name != "read_file" || tcs[1].Function.Arguments != `{"path":"file2.txt"}` {
		t.Errorf("unexpected second call: %+v", tcs[1])
	}
	if tcs[0].ID == "" || tcs[1].ID == "" || tcs[0].ID == tcs[1].ID {
		t.Errorf("expected distinct auto-assigned IDs, got id1=%q, id2=%q", tcs[0].ID, tcs[1].ID)
	}
}

func FuzzSSEStreamParser(f *testing.F) {
	seeds := [][]byte{
		[]byte("data: [DONE]\n\n"),
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"),
		[]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_1\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"a.txt\\\"}\"}}]}}]}\n\n"),
		[]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"b.txt\\\"}\"}}]}}]}\n\n"),
		[]byte(": keep-alive comment\n\n"),
		[]byte("invalid non-sse line\n"),
		[]byte("data: malformed-json\n\n"),
		[]byte("data: {\"choices\":[]}\n\n"),
		[]byte("data: {}\n\n"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	cfg := config.DefaultConfig()
	client, err := NewClient(cfg)
	if err != nil {
		f.Fatalf("NewClient: %v", err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, _, _, _ = client.readSSEStream(ctx, bytes.NewReader(data), func(token string) {})
	})
}

func TestSSEStreamRejectsRunawayLineLength(t *testing.T) {
	cfg := config.DefaultConfig()
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Create an oversized SSE line of 300 KB (> 256 KB limit) without newline
	oversized := make([]byte, 300*1024)
	for i := range oversized {
		oversized[i] = 'A'
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, _, _, err = client.readSSEStream(ctx, bytes.NewReader(oversized), func(string) {})
	if err == nil {
		t.Fatalf("expected error for runaway SSE line, got nil")
	}
	if !strings.Contains(err.Error(), "sse line length exceeded limit") {
		t.Errorf("expected line limit error, got: %v", err)
	}
}

func TestClientEmptyAndMalformedResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "empty json response",
			statusCode: 200,
			body:       "{}",
			wantErr:    false,
		},
		{
			name:       "malformed json response",
			statusCode: 200,
			body:       "{invalid-json",
			wantErr:    true,
		},
		{
			name:       "api error payload",
			statusCode: 200,
			body:       `{"error":{"message":"rate limit exceeded"}}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			cfg := config.DefaultConfig()
			cfg.Provider.BaseURL = server.URL
			cfg.Provider.Stream = false
			client, err := NewClient(cfg)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			req := CompletionRequest{
				Messages: []Message{{Role: "user", Content: "hi"}},
			}
			resp, err := client.Complete(context.Background(), req, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Complete() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && resp == nil {
				t.Errorf("expected non-nil response for valid status 200")
			}
		})
	}
}
