package brainclaw

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
)

const (
	// ControlContextHeader is minted here and read by BrainClaw. An inbound one
	// is always a forgery attempt and is stripped before this runs.
	ControlContextHeader = "X-DramaClaw-Control-Context"

	ControlContextEnvelopeVersion = "v1"
	ControlContextPayloadSchema   = "dramaclaw.brainclaw-context/v1"
	MaxControlContextHeaderBytes  = 4096
	MaxCheckpointOrdinal          = 1<<32 - 1
)

// base64RawURL is shared with the tests so the encoding used by the signer and
// the encoding under test cannot drift apart.
func base64RawURL(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

var safeKeyID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// ControlContextPayload is what BrainClaw reads. Field order is irrelevant on
// the wire because the JSON is canonicalised before encoding, but the signature
// covers the resulting base64 string, never a re-serialised object, so the two
// languages cannot diverge on canonicalisation.
type ControlContextPayload struct {
	SchemaVersion     string `json:"schema_version"`
	EpisodeGroupID    string `json:"episode_group_id"`
	ProjectGroupID    string `json:"project_group_id"`
	GroupingKeyEpoch  int64  `json:"grouping_key_epoch"`
	CheckpointOrdinal int64  `json:"checkpoint_ordinal"`
	TurnKind          string `json:"turn_kind"`
	ReplayScopeLimit  string `json:"replay_scope_limit"`
}

// ControlContextSigner holds the Gateway-to-BrainClaw key. It is deliberately a
// different key from the capability verifier's: the two legs carry different
// claims and must be rotatable independently.
type ControlContextSigner struct {
	keyID string
	key   []byte
}

func NewControlContextSigner(keyID string, key []byte) (*ControlContextSigner, error) {
	if !safeKeyID.MatchString(keyID) {
		return nil, fmt.Errorf("control context signing key id %q is invalid", keyID)
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("control context signing key is too short")
	}
	return &ControlContextSigner{keyID: keyID, key: append([]byte(nil), key...)}, nil
}

// canonicalPayload marshals with Go's encoding/json, which emits object keys in
// struct order. The frozen contract requires sorted keys and no spaces, so the
// payload is re-marshalled through a map to guarantee both regardless of how
// the struct is declared.
func canonicalPayload(payload ControlContextPayload) ([]byte, error) {
	intermediate, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(intermediate, &fields); err != nil {
		return nil, err
	}
	// json.Marshal on a map sorts keys and emits no insignificant whitespace,
	// which is exactly the frozen canonical form.
	return json.Marshal(fields)
}

func validateControlContextPayload(payload ControlContextPayload) error {
	if payload.SchemaVersion != ControlContextPayloadSchema {
		return fmt.Errorf("unexpected control context schema")
	}
	if !opaqueGroupID.MatchString(payload.EpisodeGroupID) ||
		!opaqueGroupID.MatchString(payload.ProjectGroupID) {
		// A caller with no project concept repeats the episode id; BrainClaw
		// never invents a grouping it cannot see, and neither does this.
		return fmt.Errorf("group ids must be opaque")
	}
	if payload.GroupingKeyEpoch < 0 || payload.GroupingKeyEpoch > MaxCheckpointOrdinal {
		return fmt.Errorf("grouping key epoch out of range")
	}
	if payload.CheckpointOrdinal < 0 || payload.CheckpointOrdinal > MaxCheckpointOrdinal {
		return fmt.Errorf("checkpoint ordinal out of range")
	}
	if _, ok := validTurnKinds[payload.TurnKind]; !ok {
		return fmt.Errorf("unexpected turn kind")
	}
	if _, ok := validReplayScope[payload.ReplayScopeLimit]; !ok {
		return fmt.Errorf("unexpected replay scope")
	}
	return nil
}

// Sign binds the payload to this exact request. Method, path and body digest
// are all covered: without that binding a captured envelope could be replayed
// onto a different request and relabel arbitrary traffic into someone else's
// family.
func (signer *ControlContextSigner) Sign(payload ControlContextPayload, method, endpointPath string, body []byte) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("no control context signer configured")
	}
	if err := validateControlContextPayload(payload); err != nil {
		return "", err
	}
	canonical, err := canonicalPayload(payload)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(canonical)
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, signer.key)
	for index, part := range []string{
		method, endpointPath, ControlContextEnvelopeVersion,
		signer.keyID, payloadB64, hex.EncodeToString(digest[:]),
	} {
		if index > 0 {
			mac.Write([]byte{0})
		}
		mac.Write([]byte(part))
	}
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	header := fmt.Sprintf("%s.%s.%s.%s", ControlContextEnvelopeVersion, signer.keyID, payloadB64, signature)
	if len(header) > MaxControlContextHeaderBytes {
		return "", fmt.Errorf("control context header exceeds the frozen size limit")
	}
	return header, nil
}

// PayloadFromCapability derives the Control Context claims from a verified
// capability plus the ordinal this gateway allocated for the request.
//
// The ordinal is the one field the capability cannot supply: it identifies a
// request, and a capability spans many.
func PayloadFromCapability(claims *CapabilityClaims, checkpointOrdinal int64) ControlContextPayload {
	return ControlContextPayload{
		SchemaVersion:     ControlContextPayloadSchema,
		EpisodeGroupID:    claims.EpisodeGroupID,
		ProjectGroupID:    claims.ProjectGroupID,
		GroupingKeyEpoch:  claims.GroupingKeyEpoch,
		CheckpointOrdinal: checkpointOrdinal,
		TurnKind:          claims.TurnKind,
		ReplayScopeLimit:  claims.ReplayScopeLimit,
	}
}
