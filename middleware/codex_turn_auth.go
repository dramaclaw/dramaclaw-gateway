package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
)

const (
	codexTurnCredentialPlaceholder = "dramaclaw-codex-per-turn-placeholder"
	codexTurnMetadataHeader        = "x-codex-turn-metadata"
	codexGatewayAPIKeyMetadata     = "dramaclaw_gateway_api_key"
	codexControlCapabilityMetadata = "dramaclaw_control_context_capability"
	codexControlCapabilityHeader   = "X-DramaClaw-Control-Capability"
	maxCodexTurnMetadataBytes      = 64 << 10
)

// applyCodexTurnAuthorization adapts Codex's turn-local metadata to NewAPI's
// normal token authentication. Codex keeps a shared App Server and therefore
// cannot keep an organization's token in the process or thread configuration.
// DramaClaw supplies it on turn/start; Codex projects it into this request
// header. The credential is removed before the request reaches relay code.
func applyCodexTurnAuthorization(header http.Header) {
	rawAuthorization, ok := authorizationToken(header.Get("Authorization"))
	if !ok || rawAuthorization != codexTurnCredentialPlaceholder {
		return
	}

	rawMetadata := strings.TrimSpace(header.Get(codexTurnMetadataHeader))
	if rawMetadata == "" || len(rawMetadata) > maxCodexTurnMetadataBytes {
		return
	}
	metadata := make(map[string]any)
	if err := json.Unmarshal([]byte(rawMetadata), &metadata); err != nil {
		return
	}
	gatewayAPIKey, _ := metadata[codexGatewayAPIKeyMetadata].(string)
	gatewayAPIKey = strings.TrimSpace(gatewayAPIKey)
	if gatewayAPIKey == "" {
		return
	}

	header.Set("Authorization", "Bearer "+gatewayAPIKey)
	if capability, _ := metadata[codexControlCapabilityMetadata].(string); strings.TrimSpace(capability) != "" {
		header.Set(codexControlCapabilityHeader, strings.TrimSpace(capability))
	}

	delete(metadata, codexGatewayAPIKeyMetadata)
	delete(metadata, codexControlCapabilityMetadata)
	if scrubbed, err := json.Marshal(metadata); err == nil {
		header.Set(codexTurnMetadataHeader, string(scrubbed))
	} else {
		header.Del(codexTurnMetadataHeader)
	}
}
