package openai

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterImageRoutingAndCapability(t *testing.T) {
	adaptor := &Adaptor{ChannelType: constant.ChannelTypeOpenRouter}
	assert.Contains(t, adaptor.GetCapabilities(), "image")
	for _, tc := range []struct {
		name        string
		channelType int
		mode        int
		path        string
		want        string
	}{
		{"OpenRouter generation", constant.ChannelTypeOpenRouter, relayconstant.RelayModeImagesGenerations, "/v1/images/generations", "https://openrouter.ai/api/v1/images"},
		{"OpenRouter edit", constant.ChannelTypeOpenRouter, relayconstant.RelayModeImagesEdits, "/v1/images/edits", "https://openrouter.ai/api/v1/images"},
		{"OpenRouter chat", constant.ChannelTypeOpenRouter, relayconstant.RelayModeChatCompletions, "/v1/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{"OpenAI edit", constant.ChannelTypeOpenAI, relayconstant.RelayModeImagesEdits, "/v1/images/edits", "https://openrouter.ai/api/v1/images/edits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: tc.channelType, ChannelBaseUrl: "https://openrouter.ai/api"},
				RelayMode:   tc.mode, RequestURLPath: tc.path,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOpenRouterImageJSONConversion(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode int
		body string
		want string
	}{
		{
			name: "generation keeps supported explicit zero and false values",
			mode: relayconstant.RelayModeImagesGenerations,
			body: `{"model":"LingShan-G2","prompt":"a cat","n":2,"size":"1024x1024","quality":"high","stream":false,"seed":0,"output_compression":0,"output_format":"png","watermark":false,"response_format":"url","style":"vivid"}`,
			want: `{"model":"openai/gpt-image-2","prompt":"a cat","n":2,"size":"1024x1024","quality":"high","stream":false,"seed":0,"output_compression":0,"output_format":"png","response_format":"url"}`,
		},
		{
			name: "CE reference images and pixel dimensions",
			mode: relayconstant.RelayModeImagesEdits,
			body: `{"model":"LingShan-G2","prompt":"retain the subject","image":["https://example.com/a.png","https://example.com/b.png"],"width":688,"height":1024,"metadata":{"ratio":"2:3","resolution":"1k"},"response_format":"url"}`,
			want: `{"model":"openai/gpt-image-2","prompt":"retain the subject","n":1,"aspect_ratio":"2:3","resolution":"1K","response_format":"url","input_references":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}},{"type":"image_url","image_url":{"url":"https://example.com/b.png"}}]}`,
		},
		{
			name: "explicit inline base64 response format",
			mode: relayconstant.RelayModeImagesGenerations,
			body: `{"model":"LingShan-G2","prompt":"a cat","response_format":"b64_json"}`,
			want: `{"model":"openai/gpt-image-2","prompt":"a cat","n":1,"response_format":"b64_json"}`,
		},
		{
			name: "single reference and automatic ratio",
			mode: relayconstant.RelayModeImagesEdits,
			body: `{"model":"LingShan-G2","prompt":"paint this","image":"https://example.com/a.png","metadata":{"ratio":"auto","resolution":"2k"}}`,
			want: `{"model":"openai/gpt-image-2","prompt":"paint this","n":1,"aspect_ratio":"auto","resolution":"2K","input_references":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}`,
		},
		{
			name: "automatic size preserves requested ratio and resolution",
			mode: relayconstant.RelayModeImagesGenerations,
			body: `{"model":"LingShan-NB-2","prompt":"a cat","size":"auto","metadata":{"ratio":"16:9","resolution":"2k"}}`,
			want: `{"model":"openai/gpt-image-2","prompt":"a cat","n":1,"aspect_ratio":"16:9","resolution":"2K"}`,
		},
		{
			name: "automatic size without metadata uses provider defaults",
			mode: relayconstant.RelayModeImagesGenerations,
			body: `{"model":"LingShan-G2","prompt":"a cat","size":"auto"}`,
			want: `{"model":"openai/gpt-image-2","prompt":"a cat","n":1}`,
		},
		{
			name: "automatic size and ratio pass shared validation",
			mode: relayconstant.RelayModeImagesGenerations,
			body: `{"model":"LingShan-G2","prompt":"a cat","size":" AuTo ","metadata":{"ratio":"auto","resolution":"2k"}}`,
			want: `{"model":"openai/gpt-image-2","prompt":"a cat","n":1,"aspect_ratio":"auto","resolution":"2K"}`,
		},
		{
			name: "explicit pixel size remains authoritative",
			mode: relayconstant.RelayModeImagesGenerations,
			body: `{"model":"LingShan-G2","prompt":"a cat","size":"1536x1024","metadata":{"ratio":"16:9","resolution":"2k"}}`,
			want: `{"model":"openai/gpt-image-2","prompt":"a cat","n":1,"size":"1536x1024"}`,
		},
		{
			name: "structured image references",
			mode: relayconstant.RelayModeImagesEdits,
			body: `{"model":"LingShan-G2","prompt":"paint this","images":[{"image_url":"https://example.com/a.png"}]}`,
			want: `{"model":"openai/gpt-image-2","prompt":"paint this","n":1,"input_references":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}`,
		},
		{
			name: "native image references and provider routing",
			mode: relayconstant.RelayModeImagesEdits,
			body: `{"model":"LingShan-G2","prompt":"paint this","input_references":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2U="}}],"provider":{"only":["google-ai-studio"],"allow_fallbacks":false}}`,
			want: `{"model":"openai/gpt-image-2","prompt":"paint this","n":1,"input_references":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2U="}}],"provider":{"only":["google-ai-studio"],"allow_fallbacks":false}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			request, err := helper.GetAndValidOpenAIImageRequest(c, tc.mode)
			require.NoError(t, err)
			request.Model = "openai/gpt-image-2"
			converted, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter},
				RelayMode:   tc.mode,
			}, *request)
			require.NoError(t, err)
			wire, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(wire))
		})
	}
}

func TestOpenRouterImageRejectsUnsupportedRequests(t *testing.T) {
	for _, body := range []string{
		`{"model":"openai/gpt-image-2","prompt":"edit","image":[]}`,
		`{"model":"openai/gpt-image-2","prompt":"edit","image":[{"unexpected":"value"}]}`,
		`{"model":"openai/gpt-image-2","prompt":"edit","image":[{"type":"video_url","image_url":{"url":"https://example.com/private.png?token=not-a-real-secret"}}]}`,
		`{"model":"openai/gpt-image-2","prompt":"edit","image":"https://example.com/a.png","mask":"https://example.com/mask.png"}`,
		`{"model":"openai/gpt-image-2","prompt":"edit","image":"https://example.com/a.png","n":11}`,
		`{"model":"openai/gpt-image-2","prompt":"edit","image":"https://example.com/a.png","stream":true}`,
		`{"model":"openai/gpt-image-2","prompt":"edit","image":"https://example.com/a.png","response_format":"unsupported"}`,
	} {
		t.Run(body, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			var request dto.ImageRequest
			require.NoError(t, common.Unmarshal([]byte(body), &request))
			_, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter},
				RelayMode:   relayconstant.RelayModeImagesEdits,
			}, request)
			require.Error(t, err)
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
			assert.NotContains(t, err.Error(), "https://")
			assert.NotContains(t, err.Error(), "not-a-real-secret")
		})
	}
}

func TestOpenRouterMultipartImageRetryPreservesOriginalRequest(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "LingShan-G2"))
	require.NoError(t, writer.WriteField("prompt", "paint this"))
	part, err := writer.CreateFormFile("image", "reference.png")
	require.NoError(t, err)
	_, err = io.WriteString(part, "reference image")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	originalContentType := writer.FormDataContentType()
	c.Request.Header.Set("Content-Type", originalContentType)
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	request.Model = "openai/gpt-image-2"
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter},
		RelayMode:   relayconstant.RelayModeImagesEdits,
	}
	adaptor := &Adaptor{}
	first, err := adaptor.ConvertImageRequest(c, info, *request)
	require.NoError(t, err)
	firstWire, err := common.Marshal(first)
	require.NoError(t, err)
	assert.Equal(t, originalContentType, c.Request.Header.Get("Content-Type"))

	second, err := adaptor.ConvertImageRequest(c, info, *request)
	require.NoError(t, err, "a retry must reuse the original multipart reference")
	secondWire, err := common.Marshal(second)
	require.NoError(t, err)
	assert.JSONEq(t, string(firstWire), string(secondWire))

	// The same request may also fall back to an ordinary OpenAI channel.
	info.ChannelType = constant.ChannelTypeOpenAI
	fallback, err := adaptor.ConvertImageRequest(c, info, *request)
	require.NoError(t, err)
	fallbackBody, ok := fallback.(*bytes.Buffer)
	require.True(t, ok)
	replayed := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(fallbackBody.Bytes()))
	replayed.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	require.NoError(t, replayed.ParseMultipartForm(32<<20))
	require.Len(t, replayed.MultipartForm.File["image"], 1)
	file, err := replayed.MultipartForm.File["image"][0].Open()
	require.NoError(t, err)
	defer file.Close()
	image, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, "reference image", string(image))
}

func TestOpenRouterImageGenerationHTTPRoundTrip(t *testing.T) {
	service.InitHttpClient()
	const responseBody = `{"data":[{"url":"https://example.com/generated.png?signature=fixture","media_type":"image/png"}],"expires_at":1710086400}`
	type capturedRequest struct {
		path, contentType string
		body              []byte
		err               error
	}
	received := make(chan capturedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		received <- capturedRequest{r.URL.Path, r.Header.Get("Content-Type"), body, err}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responseBody)
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"LingShan-G2","prompt":"a cat","n":1,"response_format":"url"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
	require.NoError(t, err)
	request.Model = "openai/gpt-image-2"
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: upstream.URL + "/api"},
		RelayMode:   relayconstant.RelayModeImagesGenerations, RequestURLPath: "/v1/images/generations",
	}
	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertImageRequest(c, info, *request)
	require.NoError(t, err)
	wire, err := common.Marshal(converted)
	require.NoError(t, err)
	resp, err := adaptor.DoRequest(c, info, bytes.NewReader(wire))
	require.NoError(t, err)
	httpResp, ok := resp.(*http.Response)
	require.True(t, ok)
	require.Equal(t, http.StatusOK, httpResp.StatusCode)
	_, apiErr := adaptor.DoResponse(c, httpResp, info)
	require.Nil(t, apiErr)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, responseBody, recorder.Body.String())
	got := <-received
	require.NoError(t, got.err)
	assert.Equal(t, "/api/v1/images", got.path)
	assert.Equal(t, "application/json", got.contentType)
	assert.JSONEq(t, `{"model":"openai/gpt-image-2","prompt":"a cat","n":1,"response_format":"url"}`, string(got.body))
}

func TestOpenRouterMultipartImageHTTPRoundTrip(t *testing.T) {
	service.InitHttpClient()
	const responseBody = `{"created":1710000000,"data":[{"url":"https://example.com/edited.png?signature=fixture","media_type":"image/png"}],"expires_at":1710086400,"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`
	type capturedRequest struct {
		path, contentType, authorization string
		body                             []byte
		err                              error
	}
	received := make(chan capturedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		received <- capturedRequest{r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), body, err}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responseBody)
	}))
	defer upstream.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "LingShan-G2"))
	require.NoError(t, writer.WriteField("prompt", "paint the references"))
	require.NoError(t, writer.WriteField("stream", "false"))
	require.NoError(t, writer.WriteField("response_format", "url"))
	// Input order is intentionally reversed: indexed references must keep
	// their declared order rather than a Go map's iteration order.
	for _, input := range []struct{ name, content string }{{"image[1]", "second"}, {"image[0]", "first"}} {
		part, err := writer.CreateFormFile(input.name, "reference.png")
		require.NoError(t, err)
		_, err = io.WriteString(part, input.content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	request.Model = "openai/gpt-image-2"
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: upstream.URL + "/api", ApiKey: "test-openrouter-key"},
		RelayMode:   relayconstant.RelayModeImagesEdits, RequestURLPath: "/v1/images/edits",
	}
	adaptor := &Adaptor{}
	adaptor.Init(info)
	converted, err := adaptor.ConvertImageRequest(c, info, *request)
	require.NoError(t, err)
	wire, err := common.Marshal(converted)
	require.NoError(t, err)
	resp, err := adaptor.DoRequest(c, info, bytes.NewReader(wire))
	require.NoError(t, err)
	httpResp, ok := resp.(*http.Response)
	require.True(t, ok)
	require.Equal(t, http.StatusOK, httpResp.StatusCode)
	usage, apiErr := adaptor.DoResponse(c, httpResp, info)
	require.Nil(t, apiErr)
	parsedUsage, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 3, parsedUsage.PromptTokens)
	assert.Equal(t, 4, parsedUsage.CompletionTokens)
	assert.Equal(t, 7, parsedUsage.TotalTokens)
	assert.JSONEq(t, responseBody, recorder.Body.String())

	got := <-received
	require.NoError(t, got.err)
	assert.Equal(t, "/api/v1/images", got.path)
	assert.Equal(t, "application/json", got.contentType)
	assert.Equal(t, "Bearer test-openrouter-key", got.authorization)
	assert.JSONEq(t, `{"model":"openai/gpt-image-2","prompt":"paint the references","n":1,"stream":false,"response_format":"url","input_references":[{"type":"image_url","image_url":{"url":"data:image/png;base64,`+base64.StdEncoding.EncodeToString([]byte("first"))+`"}},{"type":"image_url","image_url":{"url":"data:image/png;base64,`+base64.StdEncoding.EncodeToString([]byte("second"))+`"}}]}`, string(got.body))
}

func TestOpenRouterSynchronousImageResponseRequiresUsableImage(t *testing.T) {
	adaptor := &Adaptor{}
	for _, mode := range []struct {
		name  string
		value int
	}{
		{"generation", relayconstant.RelayModeImagesGenerations},
		{"edit", relayconstant.RelayModeImagesEdits},
	} {
		t.Run(mode.name, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				body string
				ok   bool
			}{
				{"empty data", `{"data":[]}`, false},
				{"missing data", `{"usage":{"total_tokens":7}}`, false},
				{"non-array data", `{"data":{"url":"https://example.com/image.png"}}`, false},
				{"empty image fields", `{"data":[{},{"url":" ","b64_json":""}]}`, false},
				{"non-string image fields", `{"data":[{"url":123,"b64_json":true}]}`, false},
				{"nonempty URL", `{"data":[{},{"url":"https://example.com/image.png"}]}`, true},
				{"nonempty base64", `{"data":[{"url":"","b64_json":"aW1hZ2U="}]}`, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					c, recorder, resp, info := newImageTestContext(t, tc.body, "application/json", false)
					info.ChannelType = constant.ChannelTypeOpenRouter
					info.RelayMode = mode.value

					_, apiErr := adaptor.DoResponse(c, resp, info)

					if tc.ok {
						require.Nil(t, apiErr)
						assert.JSONEq(t, tc.body, recorder.Body.String())
						return
					}
					require.NotNil(t, apiErr)
					assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
					assert.True(t, types.IsSkipRetryError(apiErr))
					assert.False(t, c.Writer.Written(), "the outer relay must still be able to write the error response")
				})
			}
		})
	}

	t.Run("other channels retain the existing response handling", func(t *testing.T) {
		const body = `{"data":[]}`
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", false)
		info.ChannelType = constant.ChannelTypeOpenAI
		info.RelayMode = relayconstant.RelayModeImagesGenerations

		_, apiErr := adaptor.DoResponse(c, resp, info)

		require.Nil(t, apiErr)
		assert.JSONEq(t, body, recorder.Body.String())
	})
}
