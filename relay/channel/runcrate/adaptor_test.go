package runcrate

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
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

func newChatInfo(channelType int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          channelType,
			ChannelBaseUrl:       constant.ChannelBaseURLs[channelType],
			SupportStreamOptions: true,
		},
		RelayMode: relayconstant.RelayModeChatCompletions,
		IsStream:  true,
	}
}

func streamingRequest() *dto.GeneralOpenAIRequest {
	stream := true
	return &dto.GeneralOpenAIRequest{
		Model:         "deepseek-ai/DeepSeek-V4.1-Flash",
		Stream:        &stream,
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
	}
}

func outboundBody(t *testing.T, converted any) map[string]any {
	t.Helper()
	raw, err := common.Marshal(converted)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(raw, &body))
	return body
}

func TestConvertOpenAIRequestKeepsStreamUsageOption(t *testing.T) {
	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertOpenAIRequest(nil, newChatInfo(constant.ChannelTypeRuncrate), streamingRequest())
	require.NoError(t, err)

	body := outboundBody(t, converted)
	assert.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])
}

// The embedded adapter still clears stream_options for other providers, so the
// Runcrate override must not change what they send.
func TestOtherProvidersStillDropStreamOptions(t *testing.T) {
	inherited := &openai.Adaptor{}
	converted, err := inherited.ConvertOpenAIRequest(nil, newChatInfo(constant.ChannelTypeDeepSeek), streamingRequest())
	require.NoError(t, err)

	assert.NotContains(t, outboundBody(t, converted), "stream_options")
}

// A mock upstream returns a usage chunk that differs from any local estimate.
// The handler must use that usage, which is what the request is settled on.
func TestStreamUsageFromUpstreamIsUsedForSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, common.Unmarshal(raw, &received))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[],\"usage\":{\"prompt_tokens\":1234,\"completion_tokens\":56,\"total_tokens\":1290}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	adaptor := &Adaptor{}
	info := newChatInfo(constant.ChannelTypeRuncrate)
	info.ShouldIncludeUsage = true
	info.UpstreamModelName = "deepseek-ai/DeepSeek-V4.1-Flash"
	converted, err := adaptor.ConvertOpenAIRequest(nil, info, streamingRequest())
	require.NoError(t, err)
	payload, err := common.Marshal(converted)
	require.NoError(t, err)

	resp, err := http.Post(upstream.URL, "application/json", bytes.NewReader(payload))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	usage, apiErr := adaptor.DoResponse(c, resp, info)
	require.Nil(t, apiErr)

	assert.Equal(t, map[string]any{"include_usage": true}, received["stream_options"])
	got, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 1234, got.PromptTokens)
	assert.Equal(t, 56, got.CompletionTokens)
}
