package brainclaw

import "testing"

// The counters exist because the reasons were already computed and then thrown
// away: in production a chain that had silently stopped producing evidence was
// indistinguishable from one with no traffic.

func TestVerifiedAndRefusedCapabilitiesAreCountedSeparately(t *testing.T) {
	before := EvidencePlaneCounters()["capability"]
	ObserveCapability(CapabilityResult{Claims: &CapabilityClaims{}})
	ObserveCapability(CapabilityResult{Reason: CapabilitySignatureBad})
	after := EvidencePlaneCounters()["capability"]

	if after["verified"] != before["verified"]+1 {
		t.Fatalf("a verified capability was not counted")
	}
	if after[CapabilitySignatureBad] != before[CapabilitySignatureBad]+1 {
		t.Fatalf("a refusal was not counted under its own reason")
	}
}

func TestAnEmptyReasonIsNamedRatherThanDropped(t *testing.T) {
	// A blank key would vanish into the map and the count would be lost, which
	// is the failure the counters are meant to make impossible.
	before := EvidencePlaneCounters()["capability"]["unspecified"]
	ObserveCapability(CapabilityResult{})
	if EvidencePlaneCounters()["capability"]["unspecified"] != before+1 {
		t.Fatalf("an unnamed refusal was not counted")
	}
}

func TestSignatureFailuresHaltARollout(t *testing.T) {
	ObserveCapability(CapabilityResult{Reason: CapabilityKeyUnknown})
	if HaltingCounts()[CapabilityKeyUnknown] == 0 {
		t.Fatalf("an unknown key must halt a rollout: it means an identity was " +
			"resolved by a key nobody configured")
	}
}

func TestAnAbsorbedOrdinalConflictIsStillReported(t *testing.T) {
	ObserveOrdinal("conflict")
	if HaltingCounts()["conflict"] == 0 {
		t.Fatalf("a conflict is harmless only until the retry budget runs out; " +
			"it has to be visible while it is still being absorbed")
	}
}

func TestOrdinaryOperationLeavesNothingHalting(t *testing.T) {
	// Deliberately last: it asserts on a fresh reading of only the benign
	// reasons, so it documents that "verified" and "assigned" are not halting.
	counters := EvidencePlaneCounters()
	for _, benign := range []string{"verified", "assigned", "success"} {
		if haltingReasons[benign] {
			t.Fatalf("%q must not halt a rollout", benign)
		}
	}
	if len(counters) != 3 {
		t.Fatalf("expected capability, signing and ordinal groups, got %d", len(counters))
	}
}
