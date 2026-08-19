package brainclaw

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Two keyrings, deliberately separate files with separate environment
// variables. They sit on different trust legs — DramaClaw→Gateway and
// Gateway→BrainClaw — carry different claims, and must be rotatable
// independently. One file holding both would make that impossible and would
// let a compromise of either leg forge the other.
const (
	CapabilityKeyringEnv   = "BRAINCLAW_CAPABILITY_KEYRING_FILE"
	ControlContextKeyEnv   = "BRAINCLAW_CONTROL_CONTEXT_KEY_FILE"
	ControlContextKeyIDEnv = "BRAINCLAW_CONTROL_CONTEXT_KEY_ID"
	KeyringSchema          = "brainclaw.control-context-keyring/v1"
	minKeyBytes            = 32
)

type keyringFile struct {
	SchemaVersion string            `json:"schema_version"`
	Keys          map[string]string `json:"keys"`
}

// readOwnerOnly refuses a key file any other local account can read. A keyring
// readable by another account lets it mint an identity that later counts as
// formal statistical evidence, or authenticate as DramaClaw.
func readOwnerOnly(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file, not a symlink", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s must be owner-only (found mode %o)", path, info.Mode().Perm())
	}
	return os.ReadFile(path)
}

func decodeKey(encoded string) ([]byte, error) {
	if secret, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return secret, nil
	}
	secret, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("secret must be base64")
	}
	return secret, nil
}

// LoadCapabilityKeyring reads the DramaClaw→Gateway signing keys.
func LoadCapabilityKeyring(path string) (*CapabilityVerifier, error) {
	raw, err := readOwnerOnly(path)
	if err != nil {
		return nil, fmt.Errorf("capability keyring: %w", err)
	}
	var file keyringFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("capability keyring: %w", err)
	}
	if file.SchemaVersion != KeyringSchema {
		return nil, fmt.Errorf("capability keyring schema %q is not %s", file.SchemaVersion, KeyringSchema)
	}
	keys := make(map[string][]byte, len(file.Keys))
	for id, encoded := range file.Keys {
		secret, decodeErr := decodeKey(encoded)
		if decodeErr != nil {
			// The id is an operator-chosen label; the secret never reaches the
			// message.
			return nil, fmt.Errorf("capability keyring key %q: %w", id, decodeErr)
		}
		if len(secret) < minKeyBytes {
			return nil, fmt.Errorf("capability keyring key %q is shorter than %d bytes", id, minKeyBytes)
		}
		keys[id] = secret
	}
	return NewCapabilityVerifier(keys)
}

// LoadControlContextSigner reads the Gateway→BrainClaw signing key. A single
// key rather than a ring: this side signs, and signing with an ambiguous key
// would produce envelopes BrainClaw cannot attribute.
func LoadControlContextSigner(path, keyID string) (*ControlContextSigner, error) {
	raw, err := readOwnerOnly(path)
	if err != nil {
		return nil, fmt.Errorf("control context key: %w", err)
	}
	trimmed := strings.TrimSpace(string(raw))
	key, err := hex.DecodeString(trimmed)
	if err != nil {
		if key, err = decodeKey(trimmed); err != nil {
			return nil, fmt.Errorf("control context key must be hex or base64")
		}
	}
	if len(key) < minKeyBytes {
		return nil, fmt.Errorf("control context key is shorter than %d bytes", minKeyBytes)
	}
	return NewControlContextSigner(keyID, key)
}

// ConfigureFromEnvironment wires the runtime at startup.
//
// Every part is optional and independent, and absence is never fatal: a gateway
// with no keys serves exactly as before and simply produces no formal evidence.
// A key that is *present but broken* is fatal, because silently serving without
// the attestation an operator explicitly configured is the failure mode this
// whole contract exists to prevent.
func ConfigureFromEnvironment(
	allocator func(episodeGroupID, requestFingerprint string, epoch, now int64) (int64, error),
) (configured bool, err error) {
	capabilityPath := strings.TrimSpace(os.Getenv(CapabilityKeyringEnv))
	contextPath := strings.TrimSpace(os.Getenv(ControlContextKeyEnv))
	contextKeyID := strings.TrimSpace(os.Getenv(ControlContextKeyIDEnv))

	if capabilityPath == "" && contextPath == "" {
		Configure(nil, nil, nil)
		return false, nil
	}
	// Half a configuration is a misconfiguration: a verifier with no signer
	// verifies capabilities and then throws the result away, and a signer with
	// no verifier has nothing to sign about.
	if capabilityPath == "" || contextPath == "" {
		return false, fmt.Errorf(
			"BrainClaw evidence needs both %s and %s, or neither",
			CapabilityKeyringEnv, ControlContextKeyEnv)
	}
	if contextKeyID == "" {
		return false, fmt.Errorf("%s is required alongside %s", ControlContextKeyIDEnv, ControlContextKeyEnv)
	}
	verifier, err := LoadCapabilityKeyring(capabilityPath)
	if err != nil {
		return false, err
	}
	signer, err := LoadControlContextSigner(contextPath, contextKeyID)
	if err != nil {
		return false, err
	}
	if allocator == nil {
		return false, fmt.Errorf("BrainClaw evidence requires a checkpoint ordinal allocator")
	}
	Configure(verifier, signer, allocator)
	return true, nil
}
