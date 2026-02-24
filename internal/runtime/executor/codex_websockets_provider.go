package executor

import "github.com/router-for-me/CLIProxyAPI/v6/internal/config"

// CodexWebsocketsProvider holds Codex websocket provider metadata for refactored executors.
type CodexWebsocketsProvider struct {
	cfg *config.Config
}

func NewCodexWebsocketsProvider(cfg *config.Config) *CodexWebsocketsProvider {
	return &CodexWebsocketsProvider{cfg: cfg}
}

func (p *CodexWebsocketsProvider) Identifier() string { return "codex" }
