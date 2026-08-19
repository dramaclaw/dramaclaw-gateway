package brainclaw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Three of this package's additions run on every request, not only on
// BrainClaw traffic: the capability is consumed at the edge for all callers,
// the identity headers are stripped from every upstream request, and the
// counters increment regardless. A deployment with no keys, no BrainClaw
// channel and no interest in any of it must be unable to tell.

func newRequestContext(t *testing.T, headers map[string]string) (*gin.Context, *http.Request) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	return context, request
}

func TestAnUnconfiguredGatewayAcceptsARequestWithNoCapability(t *testing.T) {
	Configure(nil, nil, nil)
	context, _ := newRequestContext(t, nil)

	result := ConsumeInboundCapability(context)
	if result.Verified() {
		t.Fatalf("nothing was signed, so nothing can verify")
	}
	if result.Reason != CapabilityMissing {
		t.Fatalf("an ordinary request must read as missing, not as a fault: %q", result.Reason)
	}
}

func TestAnUnconfiguredGatewayDoesNotRejectACapabilityItCannotCheck(t *testing.T) {
	// A deployment that has never been given keys may still receive a header,
	// from a caller upgraded ahead of it. Refusing the request would take a
	// working deployment down for a header it was never asked to understand.
	Configure(nil, nil, nil)
	context, _ := newRequestContext(t, map[string]string{
		CapabilityHeader: "v1.some-key.payload.signature",
	})

	result := ConsumeInboundCapability(context)
	if result.Verified() {
		t.Fatalf("an unconfigured gateway must not claim to have verified anything")
	}
	if context.IsAborted() {
		t.Fatalf("the request must be served, not aborted")
	}
}

func TestTheCapabilityHeaderIsRemovedEvenWhenItCannotBeVerified(t *testing.T) {
	// Otherwise an internal header reaches a provider on exactly the
	// deployments least equipped to notice.
	Configure(nil, nil, nil)
	context, request := newRequestContext(t, map[string]string{
		CapabilityHeader: "v1.some-key.payload.signature",
	})

	ConsumeInboundCapability(context)
	if request.Header.Get(CapabilityHeader) != "" {
		t.Fatalf("the capability header survived into the upstream request")
	}
}

func TestCountersDoNotRequireConfiguration(t *testing.T) {
	// The counters are the one part that runs on every deployment. They must
	// never be the reason one fails to start.
	Configure(nil, nil, nil)
	context, _ := newRequestContext(t, nil)
	ConsumeInboundCapability(context)

	if len(EvidencePlaneCounters()) == 0 {
		t.Fatalf("counters should exist even with nothing configured")
	}
}

func TestAnUnconfiguredGatewayReportsNothingHalting(t *testing.T) {
	// A deployment with no evidence plane must not look like one in trouble:
	// an operator who sees a halt on an unconfigured gateway learns to ignore
	// halts.
	Configure(nil, nil, nil)
	for i := 0; i < 5; i++ {
		context, _ := newRequestContext(t, nil)
		ConsumeInboundCapability(context)
	}
	for reason, count := range HaltingCounts() {
		if reason == CapabilityMissing {
			t.Fatalf("a missing capability must never halt a rollout (count %d)", count)
		}
	}
}
