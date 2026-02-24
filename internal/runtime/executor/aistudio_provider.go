package executor

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// AIStudioProvider holds AI Studio provider metadata for the refactored executor split.
type AIStudioProvider struct {
	cfg      *config.Config
	provider string
}

func NewAIStudioProvider(cfg *config.Config, provider string) *AIStudioProvider {
	return &AIStudioProvider{cfg: cfg, provider: strings.ToLower(provider)}
}

func (p *AIStudioProvider) Identifier() string { return "aistudio" }
