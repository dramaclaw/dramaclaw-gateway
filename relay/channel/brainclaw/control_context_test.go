package brainclaw

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// The frozen control-context vectors, byte-for-byte from the brainclaw
// repository. The acceptance bar for this signer is not "produces something
// plausible" but "reproduces the frozen positive exactly" — that vector is what
// BrainClaw's verifier is itself tested against, so matching it is the same as
// being accepted downstream, without needing BrainClaw in the loop.
const (
	controlContextVectorsPath   = "testdata/control-context-vectors.json"
	controlContextVectorsSHA256 = "20224ff713e31311468f8a9c71062b4f8c2db1d7f799e57390aa05b8185fb1c3"
)

type controlContextVectors struct {
	EnvelopeVersion  string                `json:"envelope_version"`
	SigningKeyID     string                `json:"signing_key_id"`
	SigningKeyHex    string                `json:"signing_key_hex"`
	HeaderValue      string                `json:"header_value"`
	PayloadBase64URL string                `json:"payload_base64url"`
	Payload          ControlContextPayload `json:"payload"`
	Request          struct {
		Method       string `json:"method"`
		EndpointPath string `json:"endpoint_path"`
		BodyUTF8     string `json:"body_utf8"`
		BodySHA256   string `json:"body_sha256_hex"`
	} `json:"request"`
	Algorithm struct {
		HeaderName     string `json:"header_name"`
		MAC            string `json:"mac"`
		Encoding       string `json:"encoding"`
		MaxHeaderBytes int    `json:"max_header_bytes"`
	} `json:"algorithm"`
}

func loadControlContextVectors(t *testing.T) (*controlContextVectors, *ControlContextSigner) {
	t.Helper()
	raw, err := os.ReadFile(controlContextVectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != controlContextVectorsSHA256 {
		t.Fatalf("the local copy of the frozen control-context vectors drifted; copy it "+
			"again from the source repository rather than editing it (got %s)",
			hex.EncodeToString(digest[:]))
	}
	var vectors controlContextVectors
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	key, err := hex.DecodeString(vectors.SigningKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewControlContextSigner(vectors.SigningKeyID, key)
	if err != nil {
		t.Fatal(err)
	}
	return &vectors, signer
}

func TestTheSignerReproducesTheFrozenPositiveExactly(t *testing.T) {
	vectors, signer := loadControlContextVectors(t)
	header, err := signer.Sign(
		vectors.Payload,
		vectors.Request.Method,
		vectors.Request.EndpointPath,
		[]byte(vectors.Request.BodyUTF8),
	)
	if err != nil {
		t.Fatal(err)
	}
	if header != vectors.HeaderValue {
		t.Fatalf("signer output differs from the frozen vector\n got: %s\nwant: %s",
			header, vectors.HeaderValue)
	}
}

// Canonicalisation is the classic place two languages drift. The frozen payload
// encoding must match byte for byte, not merely decode to the same claims.
func TestTheCanonicalPayloadEncodingMatchesTheFrozenVector(t *testing.T) {
	vectors, _ := loadControlContextVectors(t)
	canonical, err := canonicalPayload(vectors.Payload)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64RawURL(canonical)
	if encoded != vectors.PayloadBase64URL {
		t.Fatalf("canonical payload encoding drifted\n got: %s\nwant: %s",
			encoded, vectors.PayloadBase64URL)
	}
}

// The whole point of binding: an envelope must not survive being moved onto a
// different request.
func TestTheSignatureIsBoundToMethodPathAndBody(t *testing.T) {
	vectors, signer := loadControlContextVectors(t)
	base, err := signer.Sign(vectors.Payload, vectors.Request.Method,
		vectors.Request.EndpointPath, []byte(vectors.Request.BodyUTF8))
	if err != nil {
		t.Fatal(err)
	}
	for name, altered := range map[string]struct{ method, path, body string }{
		"method": {"PUT", vectors.Request.EndpointPath, vectors.Request.BodyUTF8},
		"path":   {vectors.Request.Method, "/v1/completions", vectors.Request.BodyUTF8},
		"body":   {vectors.Request.Method, vectors.Request.EndpointPath, vectors.Request.BodyUTF8 + " "},
	} {
		other, err := signer.Sign(vectors.Payload, altered.method, altered.path, []byte(altered.body))
		if err != nil {
			t.Fatal(err)
		}
		if other == base {
			t.Fatalf("changing the %s did not change the signature", name)
		}
	}
}

func TestIllegalPayloadsAreRefusedRatherThanSigned(t *testing.T) {
	vectors, signer := loadControlContextVectors(t)
	for name, mutate := range map[string]func(ControlContextPayload) ControlContextPayload{
		"raw trajectory id": func(p ControlContextPayload) ControlContextPayload { p.TrajectoryGroupID = "trajectory-7"; return p },
		"raw project id":    func(p ControlContextPayload) ControlContextPayload { p.ProjectGroupID = "proj-7"; return p },
		"missing project":   func(p ControlContextPayload) ControlContextPayload { p.ProjectGroupID = ""; return p },
		"negative ordinal":  func(p ControlContextPayload) ControlContextPayload { p.CheckpointOrdinal = -1; return p },
		"ordinal overflow": func(p ControlContextPayload) ControlContextPayload {
			p.CheckpointOrdinal = MaxCheckpointOrdinal + 1
			return p
		},
		"unknown turn kind": func(p ControlContextPayload) ControlContextPayload { p.TurnKind = "batch"; return p },
		"unknown scope":     func(p ControlContextPayload) ControlContextPayload { p.ReplayScopeLimit = "everything"; return p },
		"wrong schema":      func(p ControlContextPayload) ControlContextPayload { p.SchemaVersion = "x/v0"; return p },
	} {
		if _, err := signer.Sign(mutate(vectors.Payload), vectors.Request.Method,
			vectors.Request.EndpointPath, []byte(vectors.Request.BodyUTF8)); err == nil {
			t.Errorf("signed an illegal payload: %s", name)
		}
	}
}

func TestPayloadFromCapabilityCarriesTheClaimsForward(t *testing.T) {
	claims := &CapabilityClaims{
		TrajectoryGroupID: "hmac-sha256:99c6bbfa841348d8",
		ProjectGroupID:    "hmac-sha256:5818b4f4a66dc78a",
		GroupingKeyEpoch:  3,
		TurnKind:          "foreground_user",
		ReplayScopeLimit:  "model_output_only",
	}
	payload := PayloadFromCapability(claims, 11)
	if payload.TrajectoryGroupID != claims.TrajectoryGroupID ||
		payload.ProjectGroupID != claims.ProjectGroupID ||
		payload.GroupingKeyEpoch != 3 ||
		payload.TurnKind != claims.TurnKind ||
		payload.ReplayScopeLimit != claims.ReplayScopeLimit {
		t.Fatalf("claims were not carried forward verbatim: %+v", payload)
	}
	// The ordinal is the one field the capability cannot supply, because a
	// capability spans many requests and an ordinal identifies one.
	if payload.CheckpointOrdinal != 11 {
		t.Fatalf("ordinal not taken from the allocator: %d", payload.CheckpointOrdinal)
	}
	if payload.SchemaVersion != ControlContextPayloadSchema {
		t.Fatalf("schema not set: %q", payload.SchemaVersion)
	}
}

func TestTheAlgorithmConstantsMatchTheFrozenContract(t *testing.T) {
	vectors, _ := loadControlContextVectors(t)
	if vectors.Algorithm.HeaderName != ControlContextHeader {
		t.Fatalf("header name drift: %q", vectors.Algorithm.HeaderName)
	}
	if vectors.EnvelopeVersion != ControlContextEnvelopeVersion {
		t.Fatalf("envelope version drift: %q", vectors.EnvelopeVersion)
	}
	if vectors.Algorithm.MaxHeaderBytes != MaxControlContextHeaderBytes {
		t.Fatalf("size limit drift: %d", vectors.Algorithm.MaxHeaderBytes)
	}
	if vectors.Algorithm.MAC != "HMAC-SHA256" || vectors.Algorithm.Encoding != "base64url without padding" {
		t.Fatalf("algorithm drift: %+v", vectors.Algorithm)
	}
}
