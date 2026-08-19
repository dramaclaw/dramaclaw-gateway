package brainclaw

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// ContextKeyCapabilityClaims holds the verified claims for the current request.
// Downstream code reads them from here; the raw header is consumed once, at the
// edge, and never re-parsed.
const ContextKeyCapabilityClaims = "brainclaw_capability_claims"

// ContextKeyCapabilityReason records why no claims are present. It is internal
// telemetry: the caller is never told which check failed, because a per-reason
// response would be a signature oracle.
const ContextKeyCapabilityReason = "brainclaw_capability_reason"

var (
	runtimeMutex         sync.RWMutex
	capabilityVerifier   *CapabilityVerifier
	controlContextSigner *ControlContextSigner
	ordinalAllocator     func(episodeGroupID, requestFingerprint string, epoch, now int64) (int64, error)
)

// Configure installs the runtime. Both legs are optional and independent: a
// gateway with no capability verifier simply never produces formal evidence,
// and one with no signer never mints a Control Context. Neither absence may
// affect serving.
func Configure(verifier *CapabilityVerifier, signer *ControlContextSigner,
	allocator func(string, string, int64, int64) (int64, error)) {
	runtimeMutex.Lock()
	defer runtimeMutex.Unlock()
	capabilityVerifier = verifier
	controlContextSigner = signer
	ordinalAllocator = allocator
}

func runtime() (*CapabilityVerifier, *ControlContextSigner, func(string, string, int64, int64) (int64, error)) {
	runtimeMutex.RLock()
	defer runtimeMutex.RUnlock()
	return capabilityVerifier, controlContextSigner, ordinalAllocator
}

// ConsumeInboundCapability verifies the capability and removes it from the
// inbound request.
//
// Deliberately at the edge rather than at the signing point: the header must
// stop existing as early as possible so no later code path — logging, tracing,
// passthrough rules, a future adaptor — can copy it onward. The claims travel
// on the gin context instead, already verified, so nothing downstream re-parses
// an attacker-controlled string.
func ConsumeInboundCapability(c *gin.Context) CapabilityResult {
	if c == nil || c.Request == nil {
		return CapabilityResult{Reason: CapabilityMissing}
	}
	values := c.Request.Header.Values(CapabilityHeader)
	// Delete before verifying, so an early return cannot leave it in place.
	c.Request.Header.Del(CapabilityHeader)

	verifier, _, _ := runtime()
	result := VerifyCapabilityHeader(verifier, values)
	if result.Verified() {
		c.Set(ContextKeyCapabilityClaims, result.Claims)
	} else {
		c.Set(ContextKeyCapabilityReason, result.Reason)
	}
	return result
}

// VerifiedClaims returns the claims established at the edge, if any.
func VerifiedClaims(c *gin.Context) *CapabilityClaims {
	if c == nil {
		return nil
	}
	value, present := c.Get(ContextKeyCapabilityClaims)
	if !present {
		return nil
	}
	claims, ok := value.(*CapabilityClaims)
	if !ok {
		return nil
	}
	return claims
}

// RequestFingerprint identifies one checkpoint: the exact bytes this gateway is
// about to send, plus where it is sending them. A retry of the same request
// yields the same fingerprint and therefore the same ordinal.
func RequestFingerprint(method, endpointPath string, body []byte) string {
	digest := sha256.New()
	for index, part := range [][]byte{[]byte(method), []byte(endpointPath), body} {
		if index > 0 {
			digest.Write([]byte{0})
		}
		digest.Write(part)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

// SignOutboundRequest mints the Control Context for a final outbound request.
//
// Every condition below must hold. None of them is a serving requirement: when
// any fails the request proceeds unchanged and simply carries no formal
// identity, because evidence collection must never be able to fail a user's
// request.
func SignOutboundRequest(c *gin.Context, request *http.Request, enabled bool,
	method, endpointPath string, body []byte, now int64) (string, bool) {
	if request == nil || !enabled {
		return "", false
	}
	claims := VerifiedClaims(c)
	if claims == nil {
		return "", false
	}
	// A caller that forbade replay is not offering training evidence, so there
	// is nothing to attest.
	if claims.ReplayScopeLimit == "none" {
		return "", false
	}
	_, signer, allocate := runtime()
	if signer == nil || allocate == nil {
		return "", false
	}
	ordinal, err := allocate(claims.TrajectoryGroupID,
		RequestFingerprint(method, endpointPath, body), claims.GroupingKeyEpoch, now)
	if err != nil {
		// The ordinal is durable state; if it cannot be established, an
		// unnumbered checkpoint would be worse than none.
		return "", false
	}
	header, err := signer.Sign(PayloadFromCapability(claims, ordinal), method, endpointPath, body)
	if err != nil {
		return "", false
	}
	request.Header.Set(ControlContextHeader, header)
	return header, true
}
