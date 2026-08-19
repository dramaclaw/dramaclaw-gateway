package brainclaw

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func testRuntime(t *testing.T) (*CapabilityClaims, string) {
	t.Helper()
	capRaw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var capDoc vectorDocument
	if err := json.Unmarshal(capRaw, &capDoc); err != nil {
		t.Fatal(err)
	}
	capKey, _ := base64.StdEncoding.DecodeString(capDoc.SigningKeyB64)
	verifier, err := NewCapabilityVerifier(map[string][]byte{capDoc.SigningKeyID: capKey})
	if err != nil {
		t.Fatal(err)
	}
	verifier.SetClock(func() time.Time { return time.Unix(capDoc.VerificationClockUnix, 0) })

	ccRaw, err := os.ReadFile(controlContextVectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var ccDoc controlContextVectors
	if err := json.Unmarshal(ccRaw, &ccDoc); err != nil {
		t.Fatal(err)
	}
	ccKey, _ := hex.DecodeString(ccDoc.SigningKeyHex)
	signer, err := NewControlContextSigner(ccDoc.SigningKeyID, ccKey)
	if err != nil {
		t.Fatal(err)
	}

	ordinals := map[string]int64{}
	Configure(verifier, signer, func(trajectory, fingerprint string, _, _ int64) (int64, error) {
		key := trajectory + "\x00" + fingerprint
		if existing, seen := ordinals[key]; seen {
			return existing, nil
		}
		next := int64(len(ordinals))
		ordinals[key] = next
		return next, nil
	})
	t.Cleanup(func() { Configure(nil, nil, nil) })

	result := verifier.Verify(capDoc.Positive.HeaderValue)
	if !result.Verified() {
		t.Fatalf("test fixture capability did not verify: %s", result.Reason)
	}
	return result.Claims, capDoc.Positive.HeaderValue
}

func newContext(t *testing.T, header string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if header != "" {
		request.Header.Set(CapabilityHeader, header)
	}
	context.Request = request
	return context
}

// The capability must stop existing at the edge, verified or not.
func TestTheCapabilityIsRemovedFromTheInboundRequest(t *testing.T) {
	_, header := testRuntime(t)
	for name, presented := range map[string]string{
		"valid":   header,
		"forged":  "v1.dc-capability-2026-08.eyJhIjoxfQ.AAAA",
		"garbage": "not-an-envelope",
	} {
		context := newContext(t, presented)
		ConsumeInboundCapability(context)
		if left := context.Request.Header.Get(CapabilityHeader); left != "" {
			t.Fatalf("%s capability survived at the edge: %q", name, left)
		}
	}
}

func TestOnlyAVerifiedCapabilityReachesTheContext(t *testing.T) {
	_, header := testRuntime(t)
	valid := newContext(t, header)
	ConsumeInboundCapability(valid)
	if VerifiedClaims(valid) == nil {
		t.Fatal("a valid capability produced no claims")
	}

	forged := newContext(t, "v1.dc-capability-2026-08.eyJhIjoxfQ.AAAA")
	ConsumeInboundCapability(forged)
	if VerifiedClaims(forged) != nil {
		t.Fatal("a forged capability produced claims")
	}
	if reason, _ := forged.Get(ContextKeyCapabilityReason); reason == "" || reason == nil {
		t.Fatal("no internal reason was recorded for the refusal")
	}
}

// Duplicate presentation must not be resolved by taking the first.
func TestDuplicateCapabilitiesAreRefusedAtTheEdge(t *testing.T) {
	_, header := testRuntime(t)
	context := newContext(t, header)
	context.Request.Header.Add(CapabilityHeader, header)
	ConsumeInboundCapability(context)
	if VerifiedClaims(context) != nil {
		t.Fatal("two capabilities were accepted")
	}
	if left := context.Request.Header.Values(CapabilityHeader); len(left) != 0 {
		t.Fatalf("duplicates survived: %v", left)
	}
}

const outboundBody = `{"model":"BCI-gpt","messages":[{"role":"user","content":"hi"}]}`

func signedOutbound(t *testing.T, context *gin.Context, enabled bool, body string) (*http.Request, bool) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/chat/completions", nil)
	_, ok := SignOutboundRequest(context, request, enabled, http.MethodPost,
		"/v1/chat/completions", []byte(body), 1787000010)
	return request, ok
}

func TestASignedRequestCarriesAVerifiableControlContext(t *testing.T) {
	claims, header := testRuntime(t)
	context := newContext(t, header)
	ConsumeInboundCapability(context)

	request, ok := signedOutbound(t, context, true, outboundBody)
	if !ok {
		t.Fatal("a verified capability on an enabled channel produced no Control Context")
	}
	minted := request.Header.Get(ControlContextHeader)
	if minted == "" {
		t.Fatal("no header was set on the outbound request")
	}

	// The minted envelope must carry the capability's identity forward.
	parts := splitEnvelope(t, minted)
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	var payload ControlContextPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TrajectoryGroupID != claims.TrajectoryGroupID ||
		payload.ProjectGroupID != claims.ProjectGroupID ||
		payload.GroupingKeyEpoch != claims.GroupingKeyEpoch {
		t.Fatalf("identity was not carried forward: %+v", payload)
	}
}

// Every gate is a refusal to attest, never a refusal to serve.
func TestEachGateSuppressesAttestationWithoutAffectingTheRequest(t *testing.T) {
	_, header := testRuntime(t)

	t.Run("channel not opted in", func(t *testing.T) {
		context := newContext(t, header)
		ConsumeInboundCapability(context)
		request, ok := signedOutbound(t, context, false, outboundBody)
		if ok || request.Header.Get(ControlContextHeader) != "" {
			t.Fatal("a channel that did not opt in was signed")
		}
	})

	t.Run("no capability", func(t *testing.T) {
		context := newContext(t, "")
		ConsumeInboundCapability(context)
		request, ok := signedOutbound(t, context, true, outboundBody)
		if ok || request.Header.Get(ControlContextHeader) != "" {
			t.Fatal("an unattested request was signed")
		}
	})

	t.Run("replay scope none", func(t *testing.T) {
		context := newContext(t, header)
		ConsumeInboundCapability(context)
		claims := VerifiedClaims(context)
		claims.ReplayScopeLimit = "none"
		request, ok := signedOutbound(t, context, true, outboundBody)
		if ok || request.Header.Get(ControlContextHeader) != "" {
			t.Fatal("a caller that forbade replay was still attested")
		}
	})

	t.Run("ordinal unavailable", func(t *testing.T) {
		context := newContext(t, header)
		ConsumeInboundCapability(context)
		verifier, signer, _ := runtime()
		Configure(verifier, signer, func(string, string, int64, int64) (int64, error) {
			return 0, os.ErrClosed
		})
		request, ok := signedOutbound(t, context, true, outboundBody)
		if ok || request.Header.Get(ControlContextHeader) != "" {
			t.Fatal("an unnumbered checkpoint was attested")
		}
	})

	t.Run("no signer configured", func(t *testing.T) {
		context := newContext(t, header)
		ConsumeInboundCapability(context)
		verifier, _, allocate := runtime()
		Configure(verifier, nil, allocate)
		request, ok := signedOutbound(t, context, true, outboundBody)
		if ok || request.Header.Get(ControlContextHeader) != "" {
			t.Fatal("signed without a signer")
		}
	})
}

// A retry is the same checkpoint, so the same body must reuse its ordinal while
// a different body must not.
func TestTheFingerprintDistinguishesRequestsAndSurvivesRetries(t *testing.T) {
	first := RequestFingerprint("POST", "/v1/chat/completions", []byte(outboundBody))
	same := RequestFingerprint("POST", "/v1/chat/completions", []byte(outboundBody))
	if first != same {
		t.Fatal("the same request produced two fingerprints")
	}
	for name, other := range map[string]string{
		"body":   RequestFingerprint("POST", "/v1/chat/completions", []byte(outboundBody+" ")),
		"path":   RequestFingerprint("POST", "/v1/completions", []byte(outboundBody)),
		"method": RequestFingerprint("PUT", "/v1/chat/completions", []byte(outboundBody)),
	} {
		if other == first {
			t.Fatalf("a different %s produced the same fingerprint", name)
		}
	}
}

func splitEnvelope(t *testing.T, header string) []string {
	t.Helper()
	parts := make([]string, 0, 4)
	current := ""
	for _, character := range header {
		if character == '.' {
			parts = append(parts, current)
			current = ""
			continue
		}
		current += string(character)
	}
	parts = append(parts, current)
	if len(parts) != 4 {
		t.Fatalf("malformed envelope: %s", header)
	}
	return parts
}
