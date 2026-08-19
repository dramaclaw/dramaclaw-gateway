package brainclaw

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The frozen contract lives in the brainclaw repository. This is a byte-for-byte
// copy so this repository's CI is self-sufficient — no side-by-side checkout —
// and the pinned digest below is what keeps the two from drifting apart in
// silence. A cross-repository equality check belongs in the integration gate.
const (
	vectorsPath   = "testdata/control-capability-vectors.json"
	sourcePath    = "testdata/SOURCE.json"
	vectorsSHA256 = "d5ed7de1d3ddb51d74c3cf955dac712b105f96e51b1f656d2dce9af603656830"
)

type vectorDocument struct {
	Status                string `json:"status"`
	VerificationClockUnix int64  `json:"verification_clock_unix"`
	SigningKeyB64         string `json:"signing_key_b64"`
	SigningKeyID          string `json:"signing_key_id"`
	NotCoveredBySignature string `json:"not_covered_by_the_signature"`
	Frozen                struct {
		EnvelopeVersion     string `json:"envelope_version"`
		PayloadSchema       string `json:"payload_schema"`
		Issuer              string `json:"issuer"`
		Audience            string `json:"audience"`
		Header              string `json:"header"`
		MaxHeaderBytes      int    `json:"max_header_bytes"`
		MaxTTLSeconds       int    `json:"max_ttl_seconds"`
		MaxClockSkewSeconds int    `json:"max_clock_skew_seconds"`
	} `json:"frozen"`
	Positive struct {
		HeaderValue string                 `json:"header_value"`
		Claims      map[string]interface{} `json:"claims"`
	} `json:"positive"`
	Negative []struct {
		Name        string `json:"name"`
		HeaderValue string `json:"header_value"`
		Why         string `json:"why"`
	} `json:"negative"`
}

func loadVectors(t *testing.T) (*vectorDocument, *CapabilityVerifier) {
	t.Helper()
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != vectorsSHA256 {
		t.Fatalf("the local copy of the frozen vectors has drifted from the pinned digest; "+
			"copy it again from the source repository rather than editing it (got %s)",
			hex.EncodeToString(digest[:]))
	}
	var document vectorDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(document.SigningKeyB64)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewCapabilityVerifier(map[string][]byte{document.SigningKeyID: key})
	if err != nil {
		t.Fatal(err)
	}
	// The vectors carry their own clock so expiry and skew are deterministic.
	verifier.SetClock(func() time.Time { return time.Unix(document.VerificationClockUnix, 0) })
	return &document, verifier
}

func TestFrozenPositiveCapabilityVerifies(t *testing.T) {
	document, verifier := loadVectors(t)
	result := verifier.Verify(document.Positive.HeaderValue)
	if !result.Verified() {
		t.Fatalf("the frozen positive vector was rejected: %s", result.Reason)
	}
	claims := result.Claims
	if claims.TrajectoryGroupID != document.Positive.Claims["trajectory_group_id"] {
		t.Fatalf("trajectory group id mismatch: %q", claims.TrajectoryGroupID)
	}
	if claims.ProjectGroupID != document.Positive.Claims["project_group_id"] {
		t.Fatalf("project group id mismatch: %q", claims.ProjectGroupID)
	}
	if claims.TurnKind != "foreground_user" || claims.ReplayScopeLimit != "model_output_only" {
		t.Fatalf("unexpected scope claims: %+v", claims)
	}
}

func TestEveryFrozenNegativeCapabilityIsRefused(t *testing.T) {
	document, verifier := loadVectors(t)
	if len(document.Negative) < 20 {
		t.Fatalf("the frozen negative set shrank to %d; it is the contract's teeth", len(document.Negative))
	}
	for _, testCase := range document.Negative {
		result := verifier.Verify(testCase.HeaderValue)
		if result.Verified() {
			t.Errorf("accepted a vector that must be refused: %s (%s)", testCase.Name, testCase.Why)
			continue
		}
		if result.Reason == "" {
			t.Errorf("%s was refused without an internal reason code", testCase.Name)
		}
	}
}

// A negative caught by the wrong check tests nothing: a ttl_over_cap rejected as
// "malformed" would pass the suite while leaving the TTL cap unverified.
func TestEachFrozenNegativeFailsForItsOwnReason(t *testing.T) {
	document, verifier := loadVectors(t)
	expected := map[string]string{
		"payload_tampered":          CapabilitySignatureBad,
		"signature_tampered":        CapabilitySignatureBad,
		"wrong_signing_key":         CapabilitySignatureBad,
		"unknown_key_id":            CapabilityKeyUnknown,
		"wrong_audience":            CapabilityAudienceBad,
		"wrong_issuer":              CapabilityClaimsInvalid,
		"wrong_schema_version":      CapabilityClaimsInvalid,
		"expired":                   CapabilityExpired,
		"issued_in_the_future":      CapabilityNotYetValid,
		"ttl_over_cap":              CapabilityTTLTooLong,
		"expires_before_issued":     CapabilityClaimsInvalid,
		"raw_trajectory_identifier": CapabilityClaimsInvalid,
		"raw_project_identifier":    CapabilityClaimsInvalid,
		"illegal_replay_scope":      CapabilityClaimsInvalid,
		"illegal_turn_kind":         CapabilityClaimsInvalid,
		"negative_epoch":            CapabilityClaimsInvalid,
		"additional_property":       CapabilityClaimsInvalid,
		"missing_claim":             CapabilityClaimsInvalid,
		"crlf_injection":            CapabilityMalformed,
		"padded_base64":             CapabilityMalformed,
		"too_few_segments":          CapabilityMalformed,
		"too_many_segments":         CapabilityMalformed,
		"empty":                     CapabilityMissing,
		"oversize":                  CapabilityMalformed,
	}
	for _, testCase := range document.Negative {
		want, known := expected[testCase.Name]
		if !known {
			t.Errorf("frozen vector %q has no expected reason; the contract gained a case "+
				"without the verifier gaining a check", testCase.Name)
			continue
		}
		if got := verifier.Verify(testCase.HeaderValue).Reason; got != want {
			t.Errorf("%s: refused as %q, expected %q", testCase.Name, got, want)
		}
	}
}

func TestVectorProvenanceIsPinned(t *testing.T) {
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		SourceRepository string            `json:"source_repository"`
		SourceRevision   string            `json:"source_revision"`
		ProtocolVersions map[string]string `json:"protocol_versions"`
		Files            map[string]struct {
			SourcePath   string `json:"source_path"`
			SourceSHA256 string `json:"source_sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.SourceRevision) != 40 || source.SourceRepository == "" {
		t.Fatalf("incomplete provenance: %+v", source)
	}
	// Both frozen contracts are copied here, and each pins its own digest.
	for file, wantDigest := range map[string]string{
		"control-capability-vectors.json": vectorsSHA256,
		"control-context-vectors.json":    controlContextVectorsSHA256,
	} {
		entry, present := source.Files[file]
		if !present {
			t.Fatalf("provenance does not describe %s", file)
		}
		if entry.SourceSHA256 != wantDigest {
			t.Fatalf("%s: provenance digest %q disagrees with the pinned one",
				file, entry.SourceSHA256)
		}
		if entry.SourcePath == "" {
			t.Fatalf("%s: provenance has no source path", file)
		}
	}
	if source.ProtocolVersions["control-capability"] != CapabilityPayloadSchema {
		t.Fatalf("provenance pins capability protocol %q, verifier implements %q",
			source.ProtocolVersions["control-capability"], CapabilityPayloadSchema)
	}
	if source.ProtocolVersions["control-context"] != ControlContextPayloadSchema {
		t.Fatalf("provenance pins control-context protocol %q, signer implements %q",
			source.ProtocolVersions["control-context"], ControlContextPayloadSchema)
	}
}

func TestTheVerifierImplementsTheFrozenLimits(t *testing.T) {
	document, _ := loadVectors(t)
	if document.Status != "frozen" {
		t.Fatalf("vectors are not marked frozen: %q", document.Status)
	}
	frozen := document.Frozen
	if frozen.EnvelopeVersion != CapabilityEnvelopeVersion ||
		frozen.PayloadSchema != CapabilityPayloadSchema ||
		frozen.Issuer != CapabilityIssuer ||
		frozen.Audience != CapabilityAudience ||
		frozen.Header != CapabilityHeader {
		t.Fatalf("verifier constants diverge from the frozen contract: %+v", frozen)
	}
	if frozen.MaxHeaderBytes != MaxCapabilityHeaderBytes ||
		frozen.MaxTTLSeconds != MaxCapabilityTTLSeconds ||
		frozen.MaxClockSkewSeconds != MaxCapabilityClockSkew {
		t.Fatalf("verifier limits diverge from the frozen contract: %+v", frozen)
	}
}

// Two capabilities must never be presented at once.
func TestMultipleCapabilityHeadersAreRefused(t *testing.T) {
	document, verifier := loadVectors(t)
	value := document.Positive.HeaderValue
	if result := VerifyCapabilityHeader(verifier, []string{value, value}); result.Verified() ||
		result.Reason != CapabilityMultipleValues {
		t.Fatalf("duplicate capabilities must be refused, got %+v", result)
	}
	if result := VerifyCapabilityHeader(verifier, []string{value}); !result.Verified() {
		t.Fatalf("a single capability regressed: %s", result.Reason)
	}
	if result := VerifyCapabilityHeader(verifier, nil); result.Reason != CapabilityMissing {
		t.Fatalf("an absent capability should be context_missing, got %q", result.Reason)
	}
}
