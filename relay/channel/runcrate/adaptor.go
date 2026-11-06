package runcrate

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
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

// ConvertOpenAIRequest keeps stream_options for Runcrate only. The inherited
// converter clears stream_options for every channel except OpenAI and Azure, so
// an explicit include_usage would never reach the upstream, and the relay would
// then fall back to a local token estimate for settlement instead of the usage
// Runcrate reports. Other providers keep the inherited behaviour.
func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	var streamOptions *dto.StreamOptions
	if request != nil {
		streamOptions = request.StreamOptions
	}
	converted, err := a.Adaptor.ConvertOpenAIRequest(c, info, request)
	if err != nil {
		return nil, err
	}
	if request != nil {
		request.StreamOptions = streamOptions
	}
	return converted, nil
}

// GetCapabilities declares text because the model list is discovered upstream
// and cannot be used to infer capabilities.
func (a *Adaptor) GetCapabilities() []string {
	return []string{"text"}
}

func (a *Adaptor) GetBaseURLPolicy() (requiresBaseURL, supportsBaseURLOverride bool) {
	return false, true
}
