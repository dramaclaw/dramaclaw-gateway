package brainclaw

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestChainCanaryStage2 is the gateway leg of the cross-language chain canary.
// It is skipped unless BRAINCLAW_CHAIN_DIR points at a run directory, so it
// never runs as part of ordinary CI.
func TestChainCanaryStage2(t *testing.T) {
	dir := os.Getenv("BRAINCLAW_CHAIN_DIR")
	if dir == "" {
		t.Skip("set BRAINCLAW_CHAIN_DIR to run the chain canary")
	}
	raw, err := os.ReadFile(dir + "/stage1-minted.json")
	if err != nil {
		t.Fatal(err)
	}
	var minted struct {
		CapabilityKeyB64 string `json:"capability_key_b64"`
		CapabilityKeyID  string `json:"capability_key_id"`
		Rows             []struct {
			TurnID     string `json:"turn_id"`
			Trajectory string `json:"trajectory"`
			Project    string `json:"project"`
			Capability string `json:"capability"`
			Method     string `json:"method"`
			Path       string `json:"path"`
			Body       string `json:"body"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &minted); err != nil {
		t.Fatal(err)
	}

	capKey, _ := base64.StdEncoding.DecodeString(minted.CapabilityKeyB64)
	verifier, err := NewCapabilityVerifier(map[string][]byte{minted.CapabilityKeyID: capKey})
	if err != nil {
		t.Fatal(err)
	}
	contextKey, _ := hex.DecodeString(
		"0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0")
	signer, err := NewControlContextSigner("gw-canary-2026-08", contextKey)
	if err != nil {
		t.Fatal(err)
	}

	// Durable-ordinal stand-in with the same contract as the database one:
	// idempotent per (trajectory, fingerprint), monotonic per trajectory.
	var ordinalMutex sync.Mutex
	assigned := map[string]int64{}
	nextPerTrajectory := map[string]int64{}
	Configure(verifier, signer, func(trajectory, fingerprint string, _, _ int64) (int64, error) {
		ordinalMutex.Lock()
		defer ordinalMutex.Unlock()
		key := trajectory + "\x00" + fingerprint
		if existing, seen := assigned[key]; seen {
			return existing, nil
		}
		ordinal := nextPerTrajectory[trajectory]
		nextPerTrajectory[trajectory] = ordinal + 1
		assigned[key] = ordinal
		return ordinal, nil
	})
	t.Cleanup(func() { Configure(nil, nil, nil) })

	gin.SetMode(gin.TestMode)
	type signedRow struct {
		TurnID           string `json:"turn_id"`
		Trajectory       string `json:"trajectory"`
		Project          string `json:"project"`
		Method           string `json:"method"`
		Path             string `json:"path"`
		Body             string `json:"body"`
		ControlContext   string `json:"control_context"`
		Attested         bool   `json:"attested"`
		LeakedToProvider bool   `json:"leaked_capability_to_provider"`
	}

	// Every row concurrently, to prove the gateway holds no per-request state
	// that could cross between interleaved turns.
	results := make([]signedRow, len(minted.Rows))
	var group sync.WaitGroup
	for index, row := range minted.Rows {
		group.Add(1)
		go func(index int, row struct {
			TurnID     string `json:"turn_id"`
			Trajectory string `json:"trajectory"`
			Project    string `json:"project"`
			Capability string `json:"capability"`
			Method     string `json:"method"`
			Path       string `json:"path"`
			Body       string `json:"body"`
		}) {
			defer group.Done()
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			inbound := httptest.NewRequest(row.Method, row.Path, nil)
			inbound.Header.Set(CapabilityHeader, row.Capability)
			context.Request = inbound
			ConsumeInboundCapability(context)

			outbound := httptest.NewRequest(row.Method, "https://provider.example"+row.Path, nil)
			header, ok := SignOutboundRequest(context, outbound, true,
				row.Method, row.Path, []byte(row.Body), time.Now().Unix())
			results[index] = signedRow{
				TurnID: row.TurnID, Trajectory: row.Trajectory, Project: row.Project,
				Method: row.Method, Path: row.Path, Body: row.Body,
				ControlContext: header, Attested: ok,
				// The capability must never survive onto the provider request.
				LeakedToProvider: outbound.Header.Get(CapabilityHeader) != "" ||
					inbound.Header.Get(CapabilityHeader) != "",
			}
		}(index, row)
	}
	group.Wait()

	out, _ := json.MarshalIndent(map[string]any{
		"control_context_key_hex": hex.EncodeToString(contextKey),
		"control_context_key_id":  "gw-canary-2026-08",
		"rows":                    results,
	}, "", " ")
	if err := os.WriteFile(dir+"/stage2-signed.json", out, 0o600); err != nil {
		t.Fatal(err)
	}
	attested := 0
	for _, row := range results {
		if row.Attested {
			attested++
		}
		if row.LeakedToProvider {
			t.Fatalf("turn %s leaked the capability onward", row.TurnID)
		}
	}
	t.Logf("stage2: %d/%d attested, 0 capability leaks", attested, len(results))
}
