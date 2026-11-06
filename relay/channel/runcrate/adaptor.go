package runcrate

import (
	"github.com/QuantumNous/new-api/relay/channel/openai"
)

// Adaptor reuses the OpenAI-compatible text path. Runcrate serves
// /v1/chat/completions and /v1/models at https://api.runcrate.ai, so the
// default Base URL must not include /v1: the shared adapter appends the full
// request path.
type Adaptor struct {
	openai.Adaptor
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

// GetCapabilities declares text because the model list is discovered upstream
// and cannot be used to infer capabilities.
func (a *Adaptor) GetCapabilities() []string {
	return []string{"text"}
}

func (a *Adaptor) GetBaseURLPolicy() (requiresBaseURL, supportsBaseURLOverride bool) {
	return false, true
}
