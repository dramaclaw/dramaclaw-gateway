package runcrate

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestURLsUseDefaultBaseURLWithoutDuplicatingV1(t *testing.T) {
	adaptor := &Adaptor{}
	baseURL := constant.ChannelBaseURLs[constant.ChannelTypeRuncrate]
	tests := []struct {
		name      string
		path      string
		relayMode int
		want      string
	}{
		{"chat", "/v1/chat/completions", relayconstant.RelayModeChatCompletions, "https://api.runcrate.ai/v1/chat/completions"},
		{"embeddings", "/v1/embeddings", relayconstant.RelayModeEmbeddings, "https://api.runcrate.ai/v1/embeddings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    constant.ChannelTypeRuncrate,
					ChannelBaseUrl: baseURL,
				},
				RequestURLPath: tt.path,
				RelayMode:      tt.relayMode,
			}
			got, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAdaptorMetadata(t *testing.T) {
	adaptor := &Adaptor{}
	assert.Equal(t, "runcrate", adaptor.GetChannelName())
	assert.Empty(t, adaptor.GetModelList())
	assert.Equal(t, []string{"text"}, adaptor.GetCapabilities())
	requires, overrides := adaptor.GetBaseURLPolicy()
	assert.False(t, requires)
	assert.True(t, overrides)
}
