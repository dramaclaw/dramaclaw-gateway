package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterImageStreamRejectsIncompleteResult(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	const partial = `data: {"type":"image_generation.partial_image","b64_json":"preview"}`
	const failure = `data: {"type":"error","error":{"message":"Generation failed","code":"server_error"}}`
	const completed = `data: {"type":"image_generation.completed","b64_json":"final"}`
	for _, tc := range []struct {
		name   string
		chunks []string
	}{
		{"upstream error after preview", []string{partial, failure, "data: [DONE]"}},
		{"EOF after preview", []string{partial}},
		{"DONE without a completed image", []string{"data: [DONE]"}},
		{"error followed by completed image", []string{failure, completed, "data: [DONE]"}},
		{"error after completed image", []string{completed, failure, "data: [DONE]"}},
		{"completed event without image data", []string{`data: {"type":"image_generation.completed"}`, "data: [DONE]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, resp, info := newImageTestContext(t, strings.Join(tc.chunks, "\n\n")+"\n\n", "text/event-stream", true)
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.RelayMode = relayconstant.RelayModeImagesGenerations
			info.PriceData.UsePrice = true
			info.PriceData.AddOtherRatio("n", 2)

			_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

			require.NotNil(t, apiErr, "failure must reach the controller's existing refund branch")
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr), "an emitted SSE response must not start another generation")
		})
	}
}

func TestOpenRouterImageStreamCompletedResultPreservesDataAndCount(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"image_generation.partial_image","b64_json":"preview"}`,
		`data: {"type":"image_generation.completed","b64_json":"aW1hZ2U=","media_type":"image/png","usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)
	info.ChannelType = constant.ChannelTypeOpenRouter
	info.RelayMode = relayconstant.RelayModeImagesGenerations
	info.PriceData.UsePrice = true
	info.PriceData.AddOtherRatio("n", 2)

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.Nil(t, apiErr)
	actualUsage, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 3, actualUsage.PromptTokens)
	assert.Equal(t, 4, actualUsage.CompletionTokens)
	assert.Equal(t, 7, actualUsage.TotalTokens)
	assert.Equal(t, 1.0, info.PriceData.OtherRatios()["n"], "OpenRouter may return fewer images than requested")
	assert.Contains(t, recorder.Body.String(), `"b64_json":"aW1hZ2U="`)
	assert.Contains(t, recorder.Body.String(), `"media_type":"image/png"`)
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

type openRouterImageFailedWriter struct {
	gin.ResponseWriter
}

func (w *openRouterImageFailedWriter) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "final-image") {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseWriter.Write(data)
}

func (w *openRouterImageFailedWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func TestOpenRouterImageStreamWriteFailureReturnsRefundableError(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"final-image\"}\n\ndata: [DONE]\n\n"
	c, _, resp, info := newImageTestContext(t, body, "text/event-stream", true)
	c.Writer = &openRouterImageFailedWriter{ResponseWriter: c.Writer}
	info.ChannelType = constant.ChannelTypeOpenRouter
	info.RelayMode = relayconstant.RelayModeImagesGenerations

	_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
}

func TestOpenRouterImageStreamCancellationReturnsRefundableError(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"preview\"}\n\n"
	c, _, resp, info := newDisconnectingImageStream(t, body, "preview")
	info.ChannelType = constant.ChannelTypeOpenRouter
	info.RelayMode = relayconstant.RelayModeImagesGenerations
	info.PriceData.UsePrice = true
	info.PriceData.AddOtherRatio("n", 2)

	_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
}

func TestOpenRouterImageJSONResultRequiresImage(t *testing.T) {
	for _, body := range []string{
		`{"data":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
		`{"data":[{}]}`,
		`{"data":[{"b64_json":""}]}`,
	} {
		for _, stream := range []bool{false, true} {
			c, _, resp, info := newImageTestContext(t, body, "application/json", stream)
			info.ChannelType = constant.ChannelTypeOpenRouter
			info.RelayMode = relayconstant.RelayModeImagesGenerations

			_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

			require.NotNil(t, apiErr, "stream=%v response=%s", stream, body)
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
		}
	}
}

func TestOpenRouterImageJSONFallbackPreservesResult(t *testing.T) {
	const body = `{"data":[{"b64_json":"aW1hZ2U=","media_type":"image/png"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`
	c, recorder, resp, info := newImageTestContext(t, body, "application/json", true)
	info.ChannelType = constant.ChannelTypeOpenRouter
	info.RelayMode = relayconstant.RelayModeImagesGenerations
	info.PriceData.UsePrice = true
	info.PriceData.AddOtherRatio("n", 2)

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.Nil(t, apiErr)
	actualUsage, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 7, actualUsage.TotalTokens)
	assert.Equal(t, 1.0, info.PriceData.OtherRatios()["n"])
	assert.Contains(t, recorder.Body.String(), `"b64_json":"aW1hZ2U="`)
	assert.Contains(t, recorder.Body.String(), `"media_type":"image/png"`)
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}
