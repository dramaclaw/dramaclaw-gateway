package channel

import (
	"net/http"
	"testing"
)

// Only this gateway may mint a Control Context, and the capability is
// inbound-only. Both statements are enforced here rather than assumed, because
// Header Override writes headers with an explicit Set and would otherwise
// inject a static, body-unbound value that looks authentic downstream.
func TestBrainClawIdentityHeadersNeverReachAProvider(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://provider.example/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate both a caller-supplied forgery and a misconfigured channel
	// Header Override, which is the case the passthrough skip list cannot see.
	request.Header.Set(BrainClawCapabilityHeader, "cap.forged")
	request.Header.Set(BrainClawControlContextHeader, "v1.forged.payload.signature")
	request.Header.Set("Authorization", "Bearer upstream-key")

	stripBrainClawIdentityHeaders(request)

	if got := request.Header.Get(BrainClawCapabilityHeader); got != "" {
		t.Fatalf("capability leaked to the provider: %q", got)
	}
	if got := request.Header.Get(BrainClawControlContextHeader); got != "" {
		t.Fatalf("a forged control context survived: %q", got)
	}
	if request.Header.Get("Authorization") == "" {
		t.Fatal("stripping must not disturb unrelated headers")
	}
}

// A wildcard passthrough rule must not carry either header either.
func TestBrainClawIdentityHeadersAreExcludedFromWildcardPassthrough(t *testing.T) {
	for _, name := range []string{brainclawCapabilityHeaderLower, brainclawControlContextHeaderLower} {
		if _, skipped := passthroughSkipHeaderNamesLower[name]; !skipped {
			t.Fatalf("%q is not in the passthrough skip list", name)
		}
	}
}

// A nil request is reachable on error paths; stripping must not panic there.
func TestStripBrainClawIdentityHeadersToleratesNil(t *testing.T) {
	stripBrainClawIdentityHeaders(nil)
}
