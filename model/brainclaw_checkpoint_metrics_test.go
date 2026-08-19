package model

import "testing"

// The hook exists because the caller only ever sees the final outcome. A
// conflict the retry absorbs returns success, so counting return values reports
// contention as a clean assignment — and the operator learns nothing until the
// retry budget is exhausted, which is exactly when it is too late.

func TestAnAbsorbedConflictIsReportedAlongsideItsSuccess(t *testing.T) {
	var seen []string
	ObserveOrdinalAttempt = func(outcome string) { seen = append(seen, outcome) }
	defer func() { ObserveOrdinalAttempt = nil }()

	observeOrdinalAttempt("storage_contention")
	observeOrdinalAttempt("conflict_absorbed")
	observeOrdinalAttempt("assigned")

	if len(seen) != 3 || seen[2] != "assigned" {
		t.Fatalf("expected the contention and the eventual success, got %v", seen)
	}
	if seen[0] != "storage_contention" {
		t.Fatalf("the absorbed contention must be reported, got %v", seen)
	}
}

func TestTheHookIsOptional(t *testing.T) {
	// A gateway with no evidence plane configured still allocates ordinals, so
	// an unset hook must not panic.
	ObserveOrdinalAttempt = nil
	observeOrdinalAttempt("assigned")
}

func TestAnEmptyIdentityIsRejectedBeforeAnyAttempt(t *testing.T) {
	var seen []string
	ObserveOrdinalAttempt = func(outcome string) { seen = append(seen, outcome) }
	defer func() { ObserveOrdinalAttempt = nil }()

	if _, err := AssignCheckpointOrdinal("", "fingerprint", 1, 0); err == nil {
		t.Fatalf("an empty trajectory must not be assigned an ordinal")
	}
	if len(seen) != 0 {
		t.Fatalf("a rejected identity is not an allocation attempt, got %v", seen)
	}
}
