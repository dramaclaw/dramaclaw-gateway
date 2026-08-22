package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyCodexTurnAuthorizationPromotesAndScrubsTurnFields(t *testing.T) {
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+codexTurnCredentialPlaceholder)
	header.Set(codexTurnMetadataHeader, `{
		"turn_id":"turn-1",
		"dramaclaw_gateway_api_key":"sk-turn-secret",
		"dramaclaw_control_context_capability":"signed-capability"
	}`)

	applyCodexTurnAuthorization(header)

	require.Equal(t, "Bearer sk-turn-secret", header.Get("Authorization"))
	require.Equal(t, "signed-capability", header.Get(codexControlCapabilityHeader))
	metadata := make(map[string]any)
	require.NoError(t, json.Unmarshal([]byte(header.Get(codexTurnMetadataHeader)), &metadata))
	require.Equal(t, "turn-1", metadata["turn_id"])
	require.NotContains(t, metadata, codexGatewayAPIKeyMetadata)
	require.NotContains(t, metadata, codexControlCapabilityMetadata)
}

func TestApplyCodexTurnAuthorizationDoesNotOverrideNormalBearer(t *testing.T) {
	header := make(http.Header)
	header.Set("Authorization", "Bearer ordinary-token")
	header.Set(codexTurnMetadataHeader, `{"dramaclaw_gateway_api_key":"other-token"}`)

	applyCodexTurnAuthorization(header)

	require.Equal(t, "Bearer ordinary-token", header.Get("Authorization"))
	require.Contains(t, header.Get(codexTurnMetadataHeader), "other-token")
}

func TestApplyCodexTurnAuthorizationFailsClosed(t *testing.T) {
	tests := map[string]string{
		"missing key": `{"turn_id":"turn-1"}`,
		"wrong type":  `{"dramaclaw_gateway_api_key":42}`,
		"malformed":   `{`,
		"oversized":   strings.Repeat("x", maxCodexTurnMetadataBytes+1),
	}
	for name, metadata := range tests {
		t.Run(name, func(t *testing.T) {
			header := make(http.Header)
			header.Set("Authorization", "Bearer "+codexTurnCredentialPlaceholder)
			header.Set(codexTurnMetadataHeader, metadata)

			applyCodexTurnAuthorization(header)

			require.Equal(
				t,
				"Bearer "+codexTurnCredentialPlaceholder,
				header.Get("Authorization"),
			)
		})
	}
}
