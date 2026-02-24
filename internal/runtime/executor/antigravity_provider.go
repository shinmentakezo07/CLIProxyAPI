package executor

import "github.com/router-for-me/CLIProxyAPI/v6/internal/config"

// AntigravityProvider holds Antigravity-specific provider metadata for the refactored executor split.
type AntigravityProvider struct {
	cfg *config.Config
}

func NewAntigravityProvider(cfg *config.Config) *AntigravityProvider {
	return &AntigravityProvider{cfg: cfg}
}

func (p *AntigravityProvider) Identifier() string { return antigravityAuthType }
