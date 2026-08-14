package doubao

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToRequestPayloadPreservesDCMediaRoles(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "seedance-test",
		Prompt: "prompt",
		Metadata: map[string]any{
			"reference_images": []string{"https://example.com/ref-1.png", "https://example.com/ref-2.png"},
			"reference_videos": []string{"https://example.com/ref.mp4"},
			"reference_audios": []string{"https://example.com/ref.mp3"},
		},
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.Len(t, payload.Content, 5)
	assert.Equal(t, "reference_image", payload.Content[0].Role)
	assert.Equal(t, "reference_image", payload.Content[1].Role)
	assert.Equal(t, "reference_video", payload.Content[2].Role)
	assert.Equal(t, "reference_audio", payload.Content[3].Role)
	assert.Equal(t, "text", payload.Content[4].Type)
}

func TestConvertToRequestPayloadKeepsSingleReferenceImageAsReference(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:  "seedance-test",
		Prompt: "prompt",
		Metadata: map[string]any{
			"reference_images": []string{"https://example.com/ref.png"},
		},
	}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(&req)
	require.NoError(t, err)
	require.NotEmpty(t, payload.Content)
	assert.Equal(t, "reference_image", payload.Content[0].Role)
}
