package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Provider ProviderConfig `json:"provider"`
	Agent    AgentConfig    `json:"agent"`
	Tools    ToolsConfig    `json:"tools"`
	Timeouts TimeoutsConfig `json:"timeouts"`
}

type ProviderConfig struct {
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	APIKeyEnv  string `json:"api_key_env"`
	Model      string `json:"model"`
	Stream     bool   `json:"stream"`
	CACertFile string `json:"ca_cert_file,omitempty"`

	// Privacy: SendMemory defaults to true ONLY for localhost/LAN endpoints, false for public cloud.
	SendMemory     *bool    `json:"send_memory,omitempty"`
	RedactSections []string `json:"redact_sections,omitempty"`
}

type AgentConfig struct {
	Name          string  `json:"name"`
	Workspace     string  `json:"workspace"`
	MaxIterations int     `json:"max_iterations"`
	ContextWindow int     `json:"context_window"`
	MaxTokens     int     `json:"max_tokens"`
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"top_p"`
}

type ToolsConfig struct {
	ConfirmTools      []string `json:"confirm_tools"`
	AllowShell        bool     `json:"allow_shell"`
	BlockedCommands   []string `json:"blocked_commands,omitempty"`
	MaxToolOutputSize int      `json:"max_tool_output_size"` // in bytes, default 4096
	BraveAPIKey       string   `json:"brave_api_key,omitempty"`
	BraveAPIKeyEnv    string   `json:"brave_api_key_env,omitempty"`
	MaxSearchResults  int      `json:"max_search_results"`
}

type TimeoutsConfig struct {
	DialSeconds           int `json:"dial_seconds"`
	TLSHandshakeSeconds   int `json:"tls_handshake_seconds"`
	ResponseHeaderSeconds int `json:"response_header_seconds"`
	TotalSeconds          int `json:"total_seconds"`
	IdleSeconds           int `json:"idle_seconds"` // for streaming SSE idle detection
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return &Config{
		Provider: ProviderConfig{
			BaseURL:    "https://api.groq.com/openai/v1",
			APIKeyEnv:  "GROQ_API_KEY", // #nosec G101 -- environment variable key name
			Model:      "",             // No hardcoded model name in code
			Stream:     true,
			SendMemory: nil, // Auto-resolved based on BaseURL
			RedactSections: []string{
				"Key Relationships",
				"Personal Context",
			},
		},
		Agent: AgentConfig{
			Name:          "ZeroTwo",
			Workspace:     filepath.Join(home, ".config", "zerotwo", "workspace"),
			MaxIterations: 15,
			ContextWindow: 16384,
			MaxTokens:     2048,
			Temperature:   0.7,
			TopP:          0.8,
		},
		Tools: ToolsConfig{
			ConfirmTools:      []string{"shell", "write_file", "patch_file"},
			AllowShell:        true,
			MaxToolOutputSize: 4096,
			MaxSearchResults:  5,
		},
		Timeouts: TimeoutsConfig{
			DialSeconds:           10,
			TLSHandshakeSeconds:   10,
			ResponseHeaderSeconds: 30,  // cloud default
			TotalSeconds:          120, // cloud default
			IdleSeconds:           45,  // streaming idle timeout
		},
	}
}

func ConfigDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ".config/zerotwo"
	}
	return filepath.Join(home, ".config", "zerotwo")
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

func (c *Config) Resolve() {
	// 1. Resolve API keys from env
	if c.Provider.APIKey == "" && c.Provider.APIKeyEnv != "" {
		c.Provider.APIKey = os.Getenv(c.Provider.APIKeyEnv)
	}
	if c.Provider.APIKey == "" {
		// Fallbacks
		if key := os.Getenv("ZEROTWO_API_KEY"); key != "" {
			c.Provider.APIKey = key
		} else if key := os.Getenv("OPENAI_API_KEY"); key != "" {
			c.Provider.APIKey = key
		}
	}

	if c.Tools.BraveAPIKey == "" && c.Tools.BraveAPIKeyEnv != "" {
		c.Tools.BraveAPIKey = os.Getenv(c.Tools.BraveAPIKeyEnv)
	}

	// 2. Resolve timeout defaults for LAN / localhost vs Cloud
	isLocal := IsLocalOrLAN(c.Provider.BaseURL)
	if isLocal {
		if c.Timeouts.ResponseHeaderSeconds == 30 {
			c.Timeouts.ResponseHeaderSeconds = 600 // 10 minutes for slow CPU LAN inference
		}
		if c.Timeouts.TotalSeconds == 120 {
			c.Timeouts.TotalSeconds = 900 // 15 minutes total
		}
		if c.Timeouts.IdleSeconds == 45 {
			c.Timeouts.IdleSeconds = 120 // 2 minutes between tokens
		}
	}

	// 3. Resolve SendMemory privacy default
	if c.Provider.SendMemory == nil {
		send := isLocal // true for local/LAN, false for public cloud
		c.Provider.SendMemory = &send
	}

	if c.Tools.MaxToolOutputSize <= 0 {
		c.Tools.MaxToolOutputSize = 4096
	}

	// 4. Expand tilde in paths
	c.Agent.Workspace = ExpandPath(c.Agent.Workspace)
	if c.Provider.CACertFile != "" {
		c.Provider.CACertFile = ExpandPath(c.Provider.CACertFile)
	}
}

// ExpandPath expands leading ~ or ~/ to the user's home directory.
func ExpandPath(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// IsLocalOrLAN checks if a URL points to localhost, loopback, or private RFC1918 LAN.
func IsLocalOrLAN(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostname := u.Hostname()
	if hostname == "localhost" || strings.HasSuffix(hostname, ".local") {
		return true
	}
	ip := net.ParseIP(hostname)
	if ip == nil {
		// Try resolving host
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}

func Load() (*Config, error) {
	cfg := DefaultConfig()
	path := ConfigPath()

	data, err := os.ReadFile(path) // #nosec G304 -- local config file read
	if os.IsNotExist(err) {
		cfg.Resolve()
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	CheckFilePermissions(path, 0600)

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.Resolve()
	return cfg, nil
}

func Save(cfg *Config) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	CheckDirPermissions(dir, 0700)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	return AtomicWriteFile(path, data, 0600)
}

func CheckDirPermissions(path string, expected os.FileMode) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		fmt.Fprintf(os.Stderr, "SECURITY WARNING: Directory %s has loose permissions (%04o). Expected %04o\n", path, perm, expected)
		return false
	}
	return true
}

func CheckFilePermissions(path string, expected os.FileMode) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		fmt.Fprintf(os.Stderr, "SECURITY WARNING: File %s has loose permissions (%04o). Expected %04o\n", path, perm, expected)
		return false
	}
	return true
}

// AtomicWriteFile writes data to a temporary file in the same directory, fsyncs it and its parent directory, and renames it.
func AtomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("rename to target %s: %w", filename, err)
	}
	tmpName = "" // disarm defer remove

	// Fsync parent directory
	if df, err := os.Open(dir); err == nil { // #nosec G304 -- directory sync
		_ = df.Sync()
		_ = df.Close()
	}

	return nil
}

func MaskKey(key string) string {
	if key == "" {
		return "(none)"
	}
	if len(key) <= 8 {
		return "********"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// SanitizeChildEnv filters out all environment variables except a strict allowlist of safe system variables,
// ensuring no API keys, tokens, database URLs, SSH auth socks, or cloud credentials leak to child commands.
func SanitizeChildEnv(environ []string) []string {
	safeAllowlist := map[string]bool{
		"PATH":     true,
		"HOME":     true,
		"LANG":     true,
		"LC_ALL":   true,
		"LC_CTYPE": true,
		"TERM":     true,
		"TZ":       true,
		"USER":     true,
		"LOGNAME":  true,
		"SHELL":    true,
		"TMPDIR":   true,
		"PWD":      true,
	}

	var clean []string
	for _, env := range environ {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 0 {
			continue
		}
		key := strings.ToUpper(parts[0])
		if safeAllowlist[key] {
			clean = append(clean, env)
		}
	}
	return clean
}
