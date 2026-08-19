// Package brainclaw verifies the short-lived capability DramaClaw issues for
// one agent turn, and mints the request-bound Control Context BrainClaw reads.
//
// Two credentials on two legs, deliberately not the same thing:
//
//	DramaClaw --capability--> Gateway --control context--> BrainClaw
//
// The capability's signature covers only itself. One capability spans every
// model call an agent turn makes, and those requests do not exist when it is
// issued, so it cannot be bound to any one of them. It is a bearer token whose
// replay window is its TTL — which is why the TTL is hard-capped, the audience
// is explicit, and this package strips the header the moment it is consumed.
//
// Request binding happens one hop later: the Control Context is minted from the
// final outbound bytes, after channel selection, body conversion and every
// header override.
package brainclaw

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// CapabilityHeader is inbound only. It is never forwarded.
	CapabilityHeader = "X-DramaClaw-Control-Capability"

	CapabilityEnvelopeVersion = "v1"
	CapabilityPayloadSchema   = "dramaclaw.control-capability/v1"
	CapabilityIssuer          = "dramaclaw"
	CapabilityAudience        = "dramaclaw-gateway"

	MaxCapabilityHeaderBytes = 4096
	MaxCapabilityTTLSeconds  = 900
	MaxCapabilityClockSkew   = 60
)

// Refusal reasons. These are internal: the caller is never told which check
// failed, because a per-reason response would be a signature oracle.
const (
	CapabilityMissing        = "capability_missing"
	CapabilityMultipleValues = "capability_multiple_values"
	CapabilityMalformed      = "capability_malformed"
	CapabilitySignatureBad   = "capability_signature_invalid"
	CapabilityKeyUnknown     = "capability_key_unknown"
	CapabilityAudienceBad    = "capability_audience_invalid"
	CapabilityExpired        = "capability_expired"
	CapabilityNotYetValid    = "capability_not_yet_valid"
	CapabilityTTLTooLong     = "capability_ttl_too_long"
	CapabilityClaimsInvalid  = "capability_claims_invalid"
)

var (
	opaqueGroupID    = regexp.MustCompile(`^hmac-sha256:[0-9a-f]{16}$`)
	safeIdentifier   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	validTurnKinds   = map[string]struct{}{"foreground_user": {}, "internal_maintenance": {}}
	validReplayScope = map[string]struct{}{"none": {}, "model_output_only": {}}
)

// CapabilityClaims is the decoded payload. Every field is required; the decoder
// refuses unknown fields, so a claim set that is not exactly this is rejected
// rather than partially honoured.
type CapabilityClaims struct {
	SchemaVersion    string `json:"schema_version"`
	Issuer           string `json:"issuer"`
	Audience         string `json:"audience"`
	KeyID            string `json:"key_id"`
	TurnID           string `json:"turn_id"`
	EpisodeGroupID   string `json:"episode_group_id"`
	ProjectGroupID   string `json:"project_group_id"`
	GroupingKeyEpoch int64  `json:"grouping_key_epoch"`
	TurnKind         string `json:"turn_kind"`
	ReplayScopeLimit string `json:"replay_scope_limit"`
	IssuedAt         int64  `json:"issued_at"`
	ExpiresAt        int64  `json:"expires_at"`
	Nonce            string `json:"nonce"`
}

type CapabilityResult struct {
	Claims *CapabilityClaims
	Reason string
}

func (result CapabilityResult) Verified() bool { return result.Claims != nil }

// CapabilityVerifier holds the DramaClaw-to-Gateway signing keys. This key is
// distinct from the Gateway-to-BrainClaw one; sharing them would collapse two
// trust legs into one.
type CapabilityVerifier struct {
	keys map[string][]byte
	now  func() time.Time
}

func NewCapabilityVerifier(keys map[string][]byte) (*CapabilityVerifier, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("capability verifier requires at least one signing key")
	}
	copied := make(map[string][]byte, len(keys))
	for id, key := range keys {
		if !safeIdentifier.MatchString(id) || len(key) < 32 {
			return nil, fmt.Errorf("capability signing key %q is invalid", id)
		}
		copied[id] = append([]byte(nil), key...)
	}
	return &CapabilityVerifier{keys: copied, now: time.Now}, nil
}

// SetClock is a test seam. The frozen vectors carry their own verification
// clock, so expiry and skew can be exercised deterministically.
func (verifier *CapabilityVerifier) SetClock(now func() time.Time) { verifier.now = now }

func base64URLDecode(value string) ([]byte, error) {
	// Unpadded only. The encoding is canonical: one token, one spelling. A
	// padded variant is a different string with the same meaning, which is
	// exactly the ambiguity a frozen contract exists to remove.
	if strings.HasSuffix(value, "=") {
		return nil, fmt.Errorf("padded base64url")
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func signCapability(key []byte, version, keyID, payloadB64 string) string {
	mac := hmac.New(sha256.New, key)
	for index, part := range []string{version, keyID, payloadB64} {
		if index > 0 {
			mac.Write([]byte{0})
		}
		mac.Write([]byte(part))
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Verify decodes and checks one capability header value.
func (verifier *CapabilityVerifier) Verify(header string) CapabilityResult {
	if verifier == nil {
		return CapabilityResult{Reason: CapabilityMissing}
	}
	if strings.TrimSpace(header) == "" {
		return CapabilityResult{Reason: CapabilityMissing}
	}
	if len(header) > MaxCapabilityHeaderBytes {
		return CapabilityResult{Reason: CapabilityMalformed}
	}
	// Checked before splitting: a CR or LF is an illegal header value, not a
	// malformed envelope, and must never be reinterpreted as one.
	if strings.ContainsAny(header, "\r\n") {
		return CapabilityResult{Reason: CapabilityMalformed}
	}
	parts := strings.Split(header, ".")
	if len(parts) != 4 {
		return CapabilityResult{Reason: CapabilityMalformed}
	}
	version, keyID, payloadB64, signature := parts[0], parts[1], parts[2], parts[3]
	if version != CapabilityEnvelopeVersion || !safeIdentifier.MatchString(keyID) {
		return CapabilityResult{Reason: CapabilityMalformed}
	}
	if strings.HasSuffix(payloadB64, "=") || strings.HasSuffix(signature, "=") {
		return CapabilityResult{Reason: CapabilityMalformed}
	}
	key, known := verifier.keys[keyID]
	if !known {
		return CapabilityResult{Reason: CapabilityKeyUnknown}
	}
	expected := signCapability(key, version, keyID, payloadB64)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return CapabilityResult{Reason: CapabilitySignatureBad}
	}
	raw, err := base64URLDecode(payloadB64)
	if err != nil {
		return CapabilityResult{Reason: CapabilityMalformed}
	}
	var claims CapabilityClaims
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil {
		return CapabilityResult{Reason: CapabilityClaimsInvalid}
	}
	if decoder.More() {
		return CapabilityResult{Reason: CapabilityClaimsInvalid}
	}
	if reason := validateCapabilityClaims(claims, keyID); reason != "" {
		return CapabilityResult{Reason: reason}
	}
	now := verifier.now().Unix()
	if now >= claims.ExpiresAt {
		return CapabilityResult{Reason: CapabilityExpired}
	}
	if claims.IssuedAt-now > MaxCapabilityClockSkew {
		return CapabilityResult{Reason: CapabilityNotYetValid}
	}
	return CapabilityResult{Claims: &claims}
}

func validateCapabilityClaims(claims CapabilityClaims, envelopeKeyID string) string {
	if claims.SchemaVersion != CapabilityPayloadSchema {
		return CapabilityClaimsInvalid
	}
	if claims.Issuer != CapabilityIssuer {
		return CapabilityClaimsInvalid
	}
	if claims.Audience != CapabilityAudience {
		// A correctly signed capability meant for someone else. Distinct from a
		// bad signature so the ledger can tell misrouting from forgery.
		return CapabilityAudienceBad
	}
	// The envelope's key id and the signed one must agree, or the signed claims
	// describe a different envelope than the one presented.
	if claims.KeyID != envelopeKeyID {
		return CapabilityClaimsInvalid
	}
	if !safeIdentifier.MatchString(claims.TurnID) || !safeIdentifier.MatchString(claims.Nonce) {
		return CapabilityClaimsInvalid
	}
	if !opaqueGroupID.MatchString(claims.EpisodeGroupID) ||
		!opaqueGroupID.MatchString(claims.ProjectGroupID) {
		// A raw identifier arriving through an opaque field would be signed and
		// stored verbatim, silently defeating the pseudonymisation.
		return CapabilityClaimsInvalid
	}
	if claims.GroupingKeyEpoch < 0 || claims.GroupingKeyEpoch > 1<<32-1 {
		return CapabilityClaimsInvalid
	}
	if _, ok := validTurnKinds[claims.TurnKind]; !ok {
		return CapabilityClaimsInvalid
	}
	if _, ok := validReplayScope[claims.ReplayScopeLimit]; !ok {
		return CapabilityClaimsInvalid
	}
	if claims.IssuedAt < 0 || claims.ExpiresAt <= claims.IssuedAt {
		return CapabilityClaimsInvalid
	}
	if claims.ExpiresAt-claims.IssuedAt > MaxCapabilityTTLSeconds {
		return CapabilityTTLTooLong
	}
	return ""
}

// VerifyCapabilityHeader enforces the single-value rule before verifying.
// Header.Get returns only the first of several, so without this a caller could
// present two capabilities and have the gateway act on one while something else
// reads another.
func VerifyCapabilityHeader(verifier *CapabilityVerifier, values []string) CapabilityResult {
	if len(values) > 1 {
		return CapabilityResult{Reason: CapabilityMultipleValues}
	}
	if len(values) == 0 {
		return CapabilityResult{Reason: CapabilityMissing}
	}
	return verifier.Verify(values[0])
}
