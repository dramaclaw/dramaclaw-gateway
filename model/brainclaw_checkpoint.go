package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BrainClawCheckpoint assigns each distinct request within one episode a stable
// ordinal.
//
// The ordinal cannot live in the capability: one capability covers every model
// call an agent turn makes, so a value fixed at issue time would label all of
// them identically. Nor can it live in a process counter — this gateway runs as
// several replicas and restarts, and an ordinal that resets or forks per process
// is worse than none, because it looks authoritative. Nor can it be a truncated
// hash: collisions there would silently merge two distinct checkpoints into one
// training row.
//
// So it is allocated in the database, transactionally, keyed on the request
// itself. A retry of the same request returns the ordinal it already has —
// idempotent by construction rather than by convention — while a genuinely new
// request gets the next one. Out-of-order and concurrent arrival are expected
// and fine: the ordinal is a label, not an ordering guarantee.
type BrainClawCheckpoint struct {
	Id                 int64  `json:"id" gorm:"primaryKey"`
	EpisodeGroupId     string `json:"episode_group_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_bc_episode_fingerprint,priority:1;uniqueIndex:idx_bc_episode_ordinal,priority:1"`
	RequestFingerprint string `json:"request_fingerprint" gorm:"type:varchar(80);not null;uniqueIndex:idx_bc_episode_fingerprint,priority:2"`
	CheckpointOrdinal  int64  `json:"checkpoint_ordinal" gorm:"not null;uniqueIndex:idx_bc_episode_ordinal,priority:2"`
	GroupingKeyEpoch   int64  `json:"grouping_key_epoch" gorm:"not null"`
	CreatedTime        int64  `json:"created_time" gorm:"bigint;not null"`
}

func (BrainClawCheckpoint) TableName() string { return "brainclaw_checkpoints" }

// ErrCheckpointOrdinalUnavailable means the ordinal could not be established.
// Callers must degrade to diagnostic evidence and keep serving; a routing
// decision must never depend on evidence bookkeeping.
var ErrCheckpointOrdinalUnavailable = errors.New("brainclaw checkpoint ordinal unavailable")

// maxOrdinalAllocationAttempts bounds the retry below. Contention is between
// distinct requests of one episode, so the loser only has to step past whoever
// won; a handful of attempts covers far more concurrency than one episode ever
// sees, and a bound means a pathological case degrades to diagnostic instead of
// spinning.
const maxOrdinalAllocationAttempts = 8

// AssignCheckpointOrdinal returns the ordinal for one (episode, request) pair.
//
// Idempotent: the same fingerprint always yields the same ordinal, so a retried
// request does not consume a second slot and does not appear as two checkpoints.
//
// Two different unique constraints can reject an insert, and they mean opposite
// things:
//
//   - (episode, fingerprint) — this exact request already has an ordinal, so
//     read it and return; that is the idempotent path.
//   - (episode, ordinal) — a *different* request took the number we picked, so
//     pick the next one and try again.
//
// An earlier version handled only the first and treated the second as a failure,
// which threw away the losing request's evidence entirely under ordinary
// concurrency — the one case the durable allocator exists to survive.
func AssignCheckpointOrdinal(episodeGroupId, requestFingerprint string, groupingKeyEpoch, now int64) (int64, error) {
	if episodeGroupId == "" || requestFingerprint == "" {
		return 0, ErrCheckpointOrdinalUnavailable
	}
	var lastErr error
	for attempt := 0; attempt < maxOrdinalAllocationAttempts; attempt++ {
		ordinal, done, err := tryAssignCheckpointOrdinal(
			episodeGroupId, requestFingerprint, groupingKeyEpoch, now)
		if err != nil {
			lastErr = err
			break
		}
		if done {
			return ordinal, nil
		}
		// Lost the ordinal race; re-read the maximum and step past the winner.
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("exhausted %d ordinal allocation attempts", maxOrdinalAllocationAttempts)
	}
	return 0, fmt.Errorf("%w: %v", ErrCheckpointOrdinalUnavailable, lastErr)
}

// tryAssignCheckpointOrdinal performs one attempt. done=false means the ordinal
// was taken by another request and the caller should try again.
func tryAssignCheckpointOrdinal(
	episodeGroupId, requestFingerprint string, groupingKeyEpoch, now int64,
) (ordinal int64, done bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var existing BrainClawCheckpoint
		lookup := tx.Where("episode_group_id = ? AND request_fingerprint = ?",
			episodeGroupId, requestFingerprint).First(&existing)
		if lookup.Error == nil {
			ordinal, done = existing.CheckpointOrdinal, true
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		var maximum *int64
		if scanErr := tx.Model(&BrainClawCheckpoint{}).
			Where("episode_group_id = ?", episodeGroupId).
			Select("MAX(checkpoint_ordinal)").Scan(&maximum).Error; scanErr != nil {
			return scanErr
		}
		next := int64(0)
		if maximum != nil {
			next = *maximum + 1
		}
		record := BrainClawCheckpoint{
			EpisodeGroupId:     episodeGroupId,
			RequestFingerprint: requestFingerprint,
			CheckpointOrdinal:  next,
			GroupingKeyEpoch:   groupingKeyEpoch,
			CreatedTime:        now,
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			// Which constraint rejected it decides what happens next, and the
			// row itself is the only reliable way to tell them apart.
			var winner BrainClawCheckpoint
			byFingerprint := tx.Where("episode_group_id = ? AND request_fingerprint = ?",
				episodeGroupId, requestFingerprint).First(&winner)
			if byFingerprint.Error == nil {
				// Same request, already numbered: idempotent.
				ordinal, done = winner.CheckpointOrdinal, true
				return nil
			}
			if !errors.Is(byFingerprint.Error, gorm.ErrRecordNotFound) {
				return byFingerprint.Error
			}
			// A different request holds this ordinal. Retry with the next one.
			done = false
			return nil
		}
		ordinal, done = next, true
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return ordinal, done, nil
}
