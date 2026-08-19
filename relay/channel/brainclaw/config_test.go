package brainclaw

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func validKeyring(t *testing.T, dir string) string {
	t.Helper()
	secret := base64.StdEncoding.EncodeToString(make([]byte, 32))
	return writeFile(t, dir, "capability.json",
		`{"schema_version":"`+KeyringSchema+`","keys":{"dc-2026-08":"`+secret+`"}}`, 0o600)
}

func validContextKey(t *testing.T, dir string) string {
	t.Helper()
	return writeFile(t, dir, "context.key", hex.EncodeToString(make([]byte, 32)), 0o600)
}

func noopAllocator(string, string, int64, int64) (int64, error) { return 0, nil }

func TestBothKeyringsLoad(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadCapabilityKeyring(validKeyring(t, dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadControlContextSigner(validContextKey(t, dir), "gw-2026-08"); err != nil {
		t.Fatal(err)
	}
}

// A keyring another local account can read lets it authenticate as DramaClaw
// or mint an identity that later counts as formal evidence.
func TestGroupReadableKeysAreRefused(t *testing.T) {
	dir := t.TempDir()
	secret := base64.StdEncoding.EncodeToString(make([]byte, 32))
	loose := writeFile(t, dir, "loose.json",
		`{"schema_version":"`+KeyringSchema+`","keys":{"k":"`+secret+`"}}`, 0o640)
	if _, err := LoadCapabilityKeyring(loose); err == nil {
		t.Fatal("a group-readable capability keyring was accepted")
	}
	looseKey := writeFile(t, dir, "loose.key", hex.EncodeToString(make([]byte, 32)), 0o644)
	if _, err := LoadControlContextSigner(looseKey, "gw"); err == nil {
		t.Fatal("a world-readable control context key was accepted")
	}
}

func TestMalformedKeyringsAreRefused(t *testing.T) {
	dir := t.TempDir()
	short := base64.StdEncoding.EncodeToString(make([]byte, 31))
	for name, content := range map[string]string{
		"wrong schema":  `{"schema_version":"other/v1","keys":{}}`,
		"no keys":       `{"schema_version":"` + KeyringSchema + `","keys":{}}`,
		"short key":     `{"schema_version":"` + KeyringSchema + `","keys":{"k":"` + short + `"}}`,
		"not base64":    `{"schema_version":"` + KeyringSchema + `","keys":{"k":"not base64!!"}}`,
		"unknown field": `{"schema_version":"` + KeyringSchema + `","keys":{},"trust_all":true}`,
		"unsafe id":     `{"schema_version":"` + KeyringSchema + `","keys":{"../etc":"` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `"}}`,
	} {
		path := writeFile(t, dir, "bad.json", content, 0o600)
		if _, err := LoadCapabilityKeyring(path); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	tooShort := writeFile(t, dir, "short.key", hex.EncodeToString(make([]byte, 16)), 0o600)
	if _, err := LoadControlContextSigner(tooShort, "gw"); err == nil {
		t.Fatal("a short control context key was accepted")
	}
}

// No keys is a normal deployment, not a broken one.
func TestNoConfigurationLeavesTheRuntimeInert(t *testing.T) {
	t.Setenv(CapabilityKeyringEnv, "")
	t.Setenv(ControlContextKeyEnv, "")
	t.Setenv(ControlContextKeyIDEnv, "")
	configured, err := ConfigureFromEnvironment(noopAllocator)
	if err != nil || configured {
		t.Fatalf("an unconfigured gateway must start silently: configured=%v err=%v", configured, err)
	}
	verifier, signer, allocate := runtime()
	if verifier != nil || signer != nil || allocate != nil {
		t.Fatal("runtime was left partly configured")
	}
}

// Half a configuration is a misconfiguration, and starting anyway would serve
// without the attestation the operator asked for.
func TestPartialConfigurationIsFatal(t *testing.T) {
	dir := t.TempDir()
	t.Run("verifier without signer", func(t *testing.T) {
		t.Setenv(CapabilityKeyringEnv, validKeyring(t, dir))
		t.Setenv(ControlContextKeyEnv, "")
		t.Setenv(ControlContextKeyIDEnv, "")
		if _, err := ConfigureFromEnvironment(noopAllocator); err == nil {
			t.Fatal("started with a verifier and no signer")
		}
	})
	t.Run("signer without key id", func(t *testing.T) {
		t.Setenv(CapabilityKeyringEnv, validKeyring(t, dir))
		t.Setenv(ControlContextKeyEnv, validContextKey(t, dir))
		t.Setenv(ControlContextKeyIDEnv, "")
		if _, err := ConfigureFromEnvironment(noopAllocator); err == nil {
			t.Fatal("started with a signer and no key id")
		}
	})
	t.Run("configured without an allocator", func(t *testing.T) {
		t.Setenv(CapabilityKeyringEnv, validKeyring(t, dir))
		t.Setenv(ControlContextKeyEnv, validContextKey(t, dir))
		t.Setenv(ControlContextKeyIDEnv, "gw-2026-08")
		if _, err := ConfigureFromEnvironment(nil); err == nil {
			t.Fatal("started without an ordinal allocator")
		}
	})
	t.Run("broken keyring", func(t *testing.T) {
		t.Setenv(CapabilityKeyringEnv, writeFile(t, dir, "broken.json", "{", 0o600))
		t.Setenv(ControlContextKeyEnv, validContextKey(t, dir))
		t.Setenv(ControlContextKeyIDEnv, "gw-2026-08")
		if _, err := ConfigureFromEnvironment(noopAllocator); err == nil {
			t.Fatal("started with an unreadable keyring")
		}
	})
}

func TestAFullConfigurationActivatesTheRuntime(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(CapabilityKeyringEnv, validKeyring(t, dir))
	t.Setenv(ControlContextKeyEnv, validContextKey(t, dir))
	t.Setenv(ControlContextKeyIDEnv, "gw-2026-08")
	configured, err := ConfigureFromEnvironment(noopAllocator)
	if err != nil || !configured {
		t.Fatalf("a full configuration did not activate: configured=%v err=%v", configured, err)
	}
	verifier, signer, allocate := runtime()
	if verifier == nil || signer == nil || allocate == nil {
		t.Fatal("runtime is incomplete after a successful configuration")
	}
	t.Cleanup(func() { Configure(nil, nil, nil) })
}

// The two legs must not share a file or an environment variable.
func TestTheTwoLegsUseSeparateConfiguration(t *testing.T) {
	if CapabilityKeyringEnv == ControlContextKeyEnv {
		t.Fatal("both legs read the same environment variable")
	}
	dir := t.TempDir()
	capability, context := validKeyring(t, dir), validContextKey(t, dir)
	if capability == context {
		t.Fatal("both legs read the same file")
	}
}
