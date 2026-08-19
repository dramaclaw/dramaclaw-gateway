package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One capability covers every model call of an agent turn, so the ordinal has
// to come from the request, not the capability.
func TestOrdinalsAdvancePerEpisodeAndAreIdempotentPerRequest(t *testing.T) {
	truncateTables(t)
	const episode = "hmac-sha256:aaaaaaaaaaaaaaaa"

	first, err := AssignCheckpointOrdinal(episode, "sha256:req-1", 1, 100)
	require.NoError(t, err)
	second, err := AssignCheckpointOrdinal(episode, "sha256:req-2", 1, 101)
	require.NoError(t, err)
	assert.Equal(t, int64(0), first)
	assert.Equal(t, int64(1), second)

	// A retry is the same checkpoint. Without this, a retried request would
	// enter the training set twice.
	retry, err := AssignCheckpointOrdinal(episode, "sha256:req-1", 1, 102)
	require.NoError(t, err)
	assert.Equal(t, first, retry, "a retry must not consume a second ordinal")

	// A different episode starts its own sequence.
	other, err := AssignCheckpointOrdinal("hmac-sha256:bbbbbbbbbbbbbbbb", "sha256:req-1", 1, 103)
	require.NoError(t, err)
	assert.Equal(t, int64(0), other)
}

func TestConcurrentAllocationNeverDuplicatesAnOrdinal(t *testing.T) {
	truncateTables(t)
	const episode = "hmac-sha256:cccccccccccccccc"
	const requests = 24

	var group sync.WaitGroup
	results := make([]int64, requests)
	errs := make([]error, requests)
	for index := 0; index < requests; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			results[index], errs[index] = AssignCheckpointOrdinal(
				episode, fingerprintFor(index), 1, int64(200+index))
		}(index)
	}
	group.Wait()

	seen := map[int64][]int{}
	for index, err := range errs {
		// Every request must get an ordinal. An earlier version let the loser of
		// an ordinal race fail outright, which silently discarded that request's
		// evidence under ordinary concurrency — exactly what a durable allocator
		// is for. The retry makes allocation total, not merely duplicate-free.
		require.NoError(t, err, "request %d lost its ordinal to contention", index)
		seen[results[index]] = append(seen[results[index]], index)
	}
	assert.Len(t, seen, requests, "every distinct request must hold its own ordinal")
	for ordinal, holders := range seen {
		assert.Len(t, holders, 1, "ordinal %d was handed to several distinct requests", ordinal)
	}
	// And the numbering is dense: N requests occupy 0..N-1 with no gaps, so a
	// reader cannot mistake a lost allocation for a skipped checkpoint.
	for expected := int64(0); expected < int64(requests); expected++ {
		assert.Contains(t, seen, expected, "ordinal %d is missing from the sequence", expected)
	}
}

func TestSameRequestUnderConcurrencyGetsOneOrdinal(t *testing.T) {
	truncateTables(t)
	const episode = "hmac-sha256:dddddddddddddddd"
	var group sync.WaitGroup
	results := make([]int64, 12)
	errs := make([]error, 12)
	for index := range results {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			results[index], errs[index] = AssignCheckpointOrdinal(episode, "sha256:same", 1, 300)
		}(index)
	}
	group.Wait()

	distinct := map[int64]struct{}{}
	for index, err := range errs {
		require.NoError(t, err, "a concurrent retry of one request failed")
		distinct[results[index]] = struct{}{}
	}
	assert.Len(t, distinct, 1,
		"one request produced several different ordinals: %v", distinct)
}

// The unique index, not the transaction, is what makes multi-replica allocation
// safe. The test harness runs one connection, so assert the constraint exists
// rather than claiming the concurrency test proves cross-process behaviour.
func TestTheUniqueConstraintsExist(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec(
		"INSERT INTO brainclaw_checkpoints(trajectory_group_id,request_fingerprint,checkpoint_ordinal,grouping_key_epoch,created_time) VALUES(?,?,?,?,?)",
		"hmac-sha256:eeeeeeeeeeeeeeee", "sha256:one", 0, 1, 1).Error)

	// Same episode + same fingerprint: one checkpoint, not two.
	assert.Error(t, DB.Exec(
		"INSERT INTO brainclaw_checkpoints(trajectory_group_id,request_fingerprint,checkpoint_ordinal,grouping_key_epoch,created_time) VALUES(?,?,?,?,?)",
		"hmac-sha256:eeeeeeeeeeeeeeee", "sha256:one", 7, 1, 2).Error,
		"a duplicate (episode, fingerprint) must be rejected")

	// Same episode + same ordinal for a different request: two checkpoints
	// sharing a label would silently merge in the training table.
	assert.Error(t, DB.Exec(
		"INSERT INTO brainclaw_checkpoints(trajectory_group_id,request_fingerprint,checkpoint_ordinal,grouping_key_epoch,created_time) VALUES(?,?,?,?,?)",
		"hmac-sha256:eeeeeeeeeeeeeeee", "sha256:two", 0, 1, 3).Error,
		"a duplicate (episode, ordinal) must be rejected")
}

func TestMissingIdentityIsRefusedRatherThanGuessed(t *testing.T) {
	truncateTables(t)
	for _, testCase := range []struct{ episode, fingerprint string }{
		{"", "sha256:req"},
		{"hmac-sha256:ffffffffffffffff", ""},
		{"", ""},
	} {
		_, err := AssignCheckpointOrdinal(testCase.episode, testCase.fingerprint, 1, 400)
		assert.ErrorIs(t, err, ErrCheckpointOrdinalUnavailable)
	}
}

func fingerprintFor(index int) string {
	return "sha256:req-" + string(rune('a'+index%26)) + string(rune('a'+index/26))
}
