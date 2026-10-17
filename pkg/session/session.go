package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"zerotwo/pkg/config"
	"zerotwo/pkg/provider"
)

type Session struct {
	ID       string             `json:"id"`
	Messages []provider.Message `json:"messages"`
	Tainted  bool               `json:"tainted,omitempty"`
}

type Manager struct {
	sessionsDir string
}

func NewManager(workspace string) *Manager {
	dir := filepath.Join(workspace, "sessions")
	_ = os.MkdirAll(dir, 0700)
	config.CheckDirPermissions(dir, 0700)
	return &Manager{sessionsDir: dir}
}

func (m *Manager) path(id string) string {
	// Sanitize session ID to avoid path traversal
	safeID := filepath.Base(id)
	if safeID == "" || safeID == "." || safeID == "/" {
		safeID = "default"
	}
	if !strings.HasSuffix(safeID, ".json") {
		safeID += ".json"
	}
	return filepath.Join(m.sessionsDir, safeID)
}

func (m *Manager) Load(id string) (*Session, error) {
	p := m.path(id)
	data, err := os.ReadFile(p) // #nosec G304 -- local session file read
	if os.IsNotExist(err) {
		return &Session{ID: id, Messages: nil}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}

	config.CheckFilePermissions(p, 0600)

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("parsing session: %w", err)
	}
	sess.ID = id
	return &sess, nil
}

func (m *Manager) Save(sess *Session) error {
	p := m.path(sess.ID)
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling session: %w", err)
	}
	return config.AtomicWriteFile(p, data, 0600)
}

func DefaultSessionID() string {
	return "default"
}
