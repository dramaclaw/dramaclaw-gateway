package brainclaw

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Counters for the evidence plane, by reason code.
//
// The reasons already existed as constants and were already computed on every
// request — but only ever as a local decision, so in production a chain that
// silently stopped producing evidence looked exactly like one that had no
// traffic. There was no way to tell an issuer that stopped issuing from a
// signature that stopped verifying from a key that had drifted.
//
// Names and counts only. A reason code carries no capability, no signature, no
// project and no trajectory, which is what makes it safe to log and to scrape;
// anything that would identify a request belongs in the ledger, behind the
// authorisations that govern it.
type reasonCounters struct {
	mutex  sync.Mutex
	counts map[string]*int64
}

var (
	capabilityOutcomes = &reasonCounters{counts: map[string]*int64{}}
	signingOutcomes    = &reasonCounters{counts: map[string]*int64{}}
	ordinalOutcomes    = &reasonCounters{counts: map[string]*int64{}}
)

func (counters *reasonCounters) observe(reason string) {
	if reason == "" {
		reason = "unspecified"
	}
	counters.mutex.Lock()
	counter, ok := counters.counts[reason]
	if !ok {
		counter = new(int64)
		counters.counts[reason] = counter
	}
	counters.mutex.Unlock()
	atomic.AddInt64(counter, 1)
}

func (counters *reasonCounters) snapshot() map[string]int64 {
	counters.mutex.Lock()
	defer counters.mutex.Unlock()
	out := make(map[string]int64, len(counters.counts))
	for reason, counter := range counters.counts {
		out[reason] = atomic.LoadInt64(counter)
	}
	return out
}

// ObserveCapability records the outcome of one capability verification.
func ObserveCapability(result CapabilityResult) {
	if result.Verified() {
		capabilityOutcomes.observe("verified")
		return
	}
	capabilityOutcomes.observe(result.Reason)
}

// ObserveSigning records whether a Control Context was signed for a request.
func ObserveSigning(err error) {
	if err != nil {
		signingOutcomes.observe("failure")
		return
	}
	signingOutcomes.observe("success")
}

// ObserveOrdinal records how a checkpoint ordinal was resolved. A conflict that
// the retry absorbed is still reported: it is the signal that contention is
// rising, and it stops being harmless the moment the retry budget runs out.
func ObserveOrdinal(outcome string) { ordinalOutcomes.observe(outcome) }

// EvidencePlaneCounters is the snapshot a scrape or a periodic log reports.
func EvidencePlaneCounters() map[string]map[string]int64 {
	return map[string]map[string]int64{
		"capability": capabilityOutcomes.snapshot(),
		"signing":    signingOutcomes.snapshot(),
		"ordinal":    ordinalOutcomes.snapshot(),
	}
}

// Reasons that must stay at zero. A non-zero value here is not a degraded
// service — it means evidence is being produced that cannot be trusted, or an
// identity is being resolved by a key nobody expected. Rollout stops.
var haltingReasons = map[string]bool{
	CapabilitySignatureBad: true,
	CapabilityKeyUnknown:   true,
	CapabilityAudienceBad:  true,
	"binding_invalid":      true,
	"conflict":             true,
	"failure":              true,
}

// HaltingCounts returns the non-zero counters that should stop a rollout.
func HaltingCounts() map[string]int64 {
	halting := map[string]int64{}
	for _, group := range EvidencePlaneCounters() {
		for reason, count := range group {
			if haltingReasons[reason] && count > 0 {
				halting[reason] = count
			}
		}
	}
	return halting
}

// EvidenceReportingPeriodEnv overrides how often the counters are logged.
const EvidenceReportingPeriodEnv = "BRAINCLAW_EVIDENCE_REPORT_SECONDS"

// EvidenceReportingPeriod is the configured period, defaulting to five minutes.
//
// Configurable because the useful cadence is not the same across stages: a
// canary wants to see the counters within a run, a 24-hour soak does not want
// them every minute.
func EvidenceReportingPeriod() time.Duration {
	raw := strings.TrimSpace(os.Getenv(EvidenceReportingPeriodEnv))
	if raw == "" {
		return 5 * time.Minute
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}

// StartEvidencePlaneReporting logs the counters on a fixed period.
//
// A periodic log rather than only a scrape endpoint: this has to be readable in
// a deployment that has no Prometheus in front of it, and the first rollouts
// are exactly where that is most likely to be true.
func StartEvidencePlaneReporting(period time.Duration) {
	if period <= 0 {
		period = 5 * time.Minute
	}
	go func() {
		for range time.Tick(period) {
			for group, counts := range EvidencePlaneCounters() {
				if len(counts) == 0 {
					continue
				}
				reasons := make([]string, 0, len(counts))
				for reason := range counts {
					reasons = append(reasons, reason)
				}
				sort.Strings(reasons)
				for _, reason := range reasons {
					common.SysLog("brainclaw evidence " + group + " " + reason + "=" +
						strconv.FormatInt(counts[reason], 10))
				}
			}
			if halting := HaltingCounts(); len(halting) > 0 {
				for reason, count := range halting {
					common.SysError("brainclaw evidence HALT " + reason + "=" +
						strconv.FormatInt(count, 10) + " — stop increasing traffic")
				}
			}
		}
	}()
}
