package config

import (
	"path/filepath"

	"github.com/derailed/k9s/internal/client"
)

const (
	defaultMaxHistory = 20
)

// K9sHistoryDir manages K9s history files.
var K9sHistoryDir = filepath.Join(K9sHome(), "history")

// History tracks history configuration options.
type History struct {
	MaxHistory int `yaml:"maxHistory"`
}

// NewHistory creates a new history configuration.
func NewHistory() *History {
	return &History{
		MaxHistory: defaultMaxHistory,
	}
}

// Validate a history configuration.
func (h *History) Validate(client client.Connection, settings KubeSettings) {
	if h.MaxHistory <= 0 {
		h.MaxHistory = defaultMaxHistory
	}
}
