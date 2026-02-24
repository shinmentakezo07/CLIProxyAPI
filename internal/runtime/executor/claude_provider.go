package executor

import "github.com/router-for-me/CLIProxyAPI/v6/internal/config"

// ClaudeProvider holds Claude-specific provider metadata for the refactored executor split.
type ClaudeProvider struct {
	cfg *config.Config
}

func NewClaudeProvider(cfg *config.Config) *ClaudeProvider {
	return &ClaudeProvider{cfg: cfg}
}

func (p *ClaudeProvider) Identifier() string { return "claude" }
