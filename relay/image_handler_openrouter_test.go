package relay

import (
	"io"
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
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterImageEditConvertsWithRequestBodyPassthroughEnabled(t *testing.T) {
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })

	for _, tc := range []struct {
		name    string
		global  bool
		channel bool
	}{
		{name: "global passthrough", global: true},
		{name: "channel passthrough", channel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.PassThroughRequestEnabled = tc.global
			type upstreamRequest struct {
				path, contentType, body string
				err                     error
			}
			received := make(chan upstreamRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				received <- upstreamRequest{r.URL.Path, r.Header.Get("Content-Type"), string(body), err}
				// Keep this request-path test independent of quota and storage setup.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"message":"fixture response","type":"invalid_request_error"}}`)
			}))
			t.Cleanup(upstream.Close)

			const path = "/v1/images/edits"
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(
				`{"model":"ce-image","prompt":"edit the reference","n":1,"image":["https://example.com/reference.png"]}`,
			))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenRouter)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL+"/api")
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.channel})
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "ce-image")
			c.Set("model_mapping", `{"ce-image":"openai/gpt-image-2"}`)
			request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
			require.NoError(t, err)

			apiErr := ImageHelper(c, &relaycommon.RelayInfo{
				OriginModelName: "ce-image",
				Request:         request,
				RelayMode:       relayconstant.RelayModeImagesEdits,
				RequestURLPath:  path,
			})
			select {
			case got := <-received:
				require.NoError(t, got.err)
				assert.Equal(t, "/api/v1/images", got.path)
				assert.Equal(t, "application/json", got.contentType)
				assert.JSONEq(t, `{"model":"openai/gpt-image-2","prompt":"edit the reference","n":1,"input_references":[{"type":"image_url","image_url":{"url":"https://example.com/reference.png"}}]}`, got.body)
			default:
				t.Fatalf("upstream did not receive the edit request: %v", apiErr)
			}
		})
	}
}
