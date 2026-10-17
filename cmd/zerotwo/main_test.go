package main

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
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"zerotwo/pkg/agent"
	"zerotwo/pkg/config"
	"zerotwo/pkg/provider"
	"zerotwo/pkg/session"
	"zerotwo/pkg/tools"
)

func TestInterruptContextSingleCtrlC(t *testing.T) {
	ctx, cleanup := createInterruptContext()
	defer cleanup()

	// Deliver real OS SIGINT
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("failed to send SIGINT: %v", err)
	}

	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for SIGINT cancellation")
	}
}

func TestInterruptContextCleanup(t *testing.T) {
	ctx, cleanup := createInterruptContext()
	cleanup()

	select {
	case <-ctx.Done():
		// Success: cleanup cancelled the context
	default:
		t.Errorf("cleanup should cancel context")
	}
}

func TestProcessGroupKillOnChildProcess(t *testing.T) {
	// Spawn child shell process in its own process group
	cmd := exec.Command("sh", "-c", "sleep 60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start child command: %v", err)
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("failed to get pgid: %v", err)
	}

	if pgid <= 0 {
		t.Fatalf("expected valid pgid > 0, got %d", pgid)
	}

	// Kill entire process group using negative PID
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		t.Fatalf("failed to kill process group: %v", err)
	}

	// Wait for process termination
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case waitErr := <-done:
		if waitErr == nil {
			t.Fatalf("expected error from killed process, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("process group did not terminate within 2s")
	}
}

func TestBuildRegistryTools(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.AllowShell = true
	mem := agent.NewMemoryStore(t.TempDir())

	registry := buildRegistry(cfg, mem)
	providerTools := registry.AsProviderTools()

	expected := []string{"read_file", "write_file", "patch_file", "list_dir", "get_system_status", "shell", "web_fetch", "http_request", "save_memory", "patch_memory"}
	for _, exp := range expected {
		var found bool
		for _, pt := range providerTools {
			if pt.Function.Name == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tool %q not found in registry (available: %+v)", exp, providerTools)
		}
	}
}

func TestPrintStatusMasking(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.APIKey = "gsk_test1234567890abcdef1234567890abcdef" // gitleaks:allow

	masked := config.MaskKey(cfg.Provider.APIKey)
	if !strings.HasPrefix(masked, "gsk_...") || !strings.HasSuffix(masked, "cdef") {
		t.Errorf("unexpected masked key format: %q", masked)
	}
	if strings.Contains(masked, "test1234567890") {
		t.Errorf("raw key leaked in masked output: %q", masked)
	}
}

func TestREPLCancellationResilience(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			// First prompt: wait for cancel
			select {
			case <-r.Context().Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		// Second prompt: succeed immediately
		resp := provider.CompletionResponse{
			Choices: []provider.Choice{
				{
					Index: 0,
					Message: provider.Message{
						Role:    "assistant",
						Content: "Second prompt reply",
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
	store := agent.NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	loop := agent.NewLoop(cfg, client, registry, sessions, store, false)

	sess := &session.Session{ID: "repl-resilience"}

	// Turn 1: Cancelled by context
	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel1()
	}()
	_, err1 := loop.Run(ctx1, "First prompt to be cancelled", sess, nil)
	if err1 == nil {
		t.Fatalf("expected error on cancelled turn 1")
	}

	// Turn 2: Succeeds
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	reply2, err2 := loop.Run(ctx2, "Second prompt after cancel", sess, nil)
	if err2 != nil {
		t.Fatalf("turn 2 should succeed after turn 1 was cancelled: %v", err2)
	}
	if reply2 != "Second prompt reply" {
		t.Errorf("unexpected reply2: %q", reply2)
	}
}

func TestSecondSIGINTHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	ctx, _ := createInterruptContext()
	fmt.Fprintln(os.Stdout, "READY")
	<-ctx.Done()
	select {}
}

func TestSecondSIGINTForceQuit(t *testing.T) {
	var cmd *exec.Cmd
	if qemu := os.Getenv("QEMU_EXEC"); qemu != "" {
		parts := strings.Fields(qemu)
		args := append(parts[1:], os.Args[0], "-test.run=^TestSecondSIGINTHelperProcess$")
		cmd = exec.Command(parts[0], args...)
	} else {
		cmd = exec.Command(os.Args[0], "-test.run=^TestSecondSIGINTHelperProcess$")
	}
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
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
		t.Fatalf("helper process did not start properly: %v, line: %q", err, line)
	}

	// 1st SIGINT: Graceful cancel
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatalf("1st SIGINT failed: %v", err)
	}

	// Give a moment for the 1st signal to be processed
	time.Sleep(50 * time.Millisecond)

	// 2nd SIGINT: Force exit (os.Exit(130))
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatalf("2nd SIGINT failed: %v", err)
	}

	waitErr := cmd.Wait()
	if waitErr == nil {
		t.Fatalf("expected helper process to exit with non-zero exit code")
	}

	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T: %v", waitErr, waitErr)
	}
	if exitErr.ExitCode() != 130 {
		t.Errorf("expected exit code 130 for force quit, got %d", exitErr.ExitCode())
	}
}

func TestPrintHelpAndStatus(t *testing.T) {
	printHelp()

	cfg := config.DefaultConfig()
	sess := &session.Session{ID: "test-status-sess"}
	printStatus(cfg, sess)
}

func TestRunDryRunExecution(t *testing.T) {
	cfg := config.DefaultConfig()
	tmpDir := t.TempDir()
	store := agent.NewMemoryStore(tmpDir)
	sessions := session.NewManager(tmpDir)
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(1024))
	loop := agent.NewLoop(cfg, nil, registry, sessions, store, false)
	sess := &session.Session{ID: "test-dry-run-sess"}

	runDryRun(cfg, loop, sess, "testing dry run", registry)
	runDryRun(cfg, loop, sess, "", registry)
}

func TestBuildRegistryWithBraveKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.AllowShell = false
	cfg.Tools.BraveAPIKey = "brave_test_key"
	mem := agent.NewMemoryStore(t.TempDir())
	reg := buildRegistry(cfg, mem)
	tools := reg.AsProviderTools()
	var hasSearch, hasShell bool
	for _, tool := range tools {
		if tool.Function.Name == "web_search" {
			hasSearch = true
		}
		if tool.Function.Name == "shell" {
			hasShell = true
		}
	}
	if !hasSearch {
		t.Errorf("expected web_search tool when BraveAPIKey is set")
	}
	if hasShell {
		t.Errorf("shell tool should be omitted when AllowShell is false")
	}
}

type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 'A'
	}
	return len(p), nil
}

func TestReadPipedStdin(t *testing.T) {
	// 1. Normal input
	normalInput := "echo 'hello world'"
	res, err := readPipedStdin(strings.NewReader(normalInput), 10*1024*1024)
	if err != nil {
		t.Fatalf("unexpected error for normal input: %v", err)
	}
	if res != normalInput {
		t.Errorf("expected %q, got %q", normalInput, res)
	}

	// 2. Oversized input (> 10MB limit)
	limit := 10 * 1024 * 1024
	oversizedReader := io.LimitReader(infiniteReader{}, int64(limit+128*1024))
	_, err = readPipedStdin(oversizedReader, limit)
	if err == nil {
		t.Fatalf("expected error for oversized piped input, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded 10 MB limit") {
		t.Errorf("expected limit error message, got: %v", err)
	}
}
