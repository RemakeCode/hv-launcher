package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestVerifierConsumesValidCapabilityOnce(t *testing.T) {
	verifier, secret, now := testVerifier(t, 0x11)
	token := signTestCapability(t, secret, testClaims(now))
	if err := verifier.Consume(token, OperationUMIPApply, "grub:preview-digest"); err != nil {
		t.Fatalf("consume valid capability: %v", err)
	}
	if err := verifier.Consume(token, OperationUMIPApply, "grub:preview-digest"); !errors.Is(err, ErrCapabilityReplay) {
		t.Fatalf("replay returned %v", err)
	}
}

func TestVerifierRejectsInvalidCapabilities(t *testing.T) {
	verifier, secret, now := testVerifier(t, 0x22)
	valid := signTestCapability(t, secret, testClaims(now))
	tampered := valid[:len(valid)-1] + differentBase64Character(valid[len(valid)-1])

	tests := []struct {
		name      string
		token     string
		operation Operation
		binding   string
		want      error
	}{
		{name: "missing", operation: OperationUMIPApply, binding: "grub:preview-digest", want: ErrInvalidCapability},
		{name: "malformed", token: "not-a-capability", operation: OperationUMIPApply, binding: "grub:preview-digest", want: ErrInvalidCapability},
		{name: "tampered", token: tampered, operation: OperationUMIPApply, binding: "grub:preview-digest", want: ErrInvalidCapability},
		{name: "operation mismatch", token: valid, operation: OperationModuleInstall, binding: "grub:preview-digest", want: ErrCapabilityMismatch},
		{name: "binding mismatch", token: valid, operation: OperationUMIPApply, binding: "limine:preview-digest", want: ErrCapabilityMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := verifier.Consume(test.token, test.operation, test.binding); !errors.Is(err, test.want) {
				t.Fatalf("Consume() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestVerifierRejectsExpiredAndInvalidClaims(t *testing.T) {
	verifier, secret, now := testVerifier(t, 0x33)
	tests := []struct {
		name   string
		mutate func(*claims)
		want   error
	}{
		{name: "expired", mutate: func(value *claims) {
			value.IssuedAt = now.Add(-2 * time.Minute).Unix()
			value.ExpiresAt = now.Add(-time.Minute).Unix()
		}, want: ErrExpiredCapability},
		{name: "unknown operation", mutate: func(value *claims) { value.Operation = "arbitrary-command" }, want: ErrInvalidCapability},
		{name: "future issued", mutate: func(value *claims) {
			value.IssuedAt = now.Add(time.Minute).Unix()
			value.ExpiresAt = now.Add(2 * time.Minute).Unix()
		}, want: ErrInvalidCapability},
		{name: "excessive lifetime", mutate: func(value *claims) { value.ExpiresAt = now.Add(2 * time.Minute).Unix() }, want: ErrInvalidCapability},
		{name: "empty binding", mutate: func(value *claims) { value.Binding = "" }, want: ErrInvalidCapability},
		{name: "invalid nonce", mutate: func(value *claims) { value.Nonce = "short" }, want: ErrInvalidCapability},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := testClaims(now)
			test.mutate(&value)
			token := signTestCapability(t, secret, value)
			if err := verifier.Consume(token, OperationUMIPApply, "grub:preview-digest"); !errors.Is(err, test.want) {
				t.Fatalf("Consume() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestVerifierConsumesNonceAtomically(t *testing.T) {
	verifier, secret, now := testVerifier(t, 0x44)
	token := signTestCapability(t, secret, testClaims(now))
	var successes atomic.Int32
	var replays atomic.Int32
	var unexpected atomic.Value
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := verifier.Consume(token, OperationUMIPApply, "grub:preview-digest")
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrCapabilityReplay):
				replays.Add(1)
			default:
				unexpected.Store(err)
			}
		}()
	}
	wait.Wait()
	if value := unexpected.Load(); value != nil {
		t.Fatalf("unexpected concurrent error: %v", value)
	}
	if successes.Load() != 1 || replays.Load() != 31 {
		t.Fatalf("successes = %d, replays = %d", successes.Load(), replays.Load())
	}
}

func TestVerifierRejectsCapabilityAfterSecretRotation(t *testing.T) {
	oldVerifier, oldSecret, now := testVerifier(t, 0x55)
	newVerifier, _, _ := testVerifier(t, 0x66)
	token := signTestCapability(t, oldSecret, testClaims(now))
	if err := oldVerifier.Consume(token, OperationUMIPApply, "grub:preview-digest"); err != nil {
		t.Fatalf("old verifier rejected its capability: %v", err)
	}
	if err := newVerifier.Consume(token, OperationUMIPApply, "grub:preview-digest"); !errors.Is(err, ErrInvalidCapability) {
		t.Fatalf("rotated verifier returned %v", err)
	}
}

func TestVerifierRechecksExpiryBeforeNonceConsumption(t *testing.T) {
	verifier, secret, now := testVerifier(t, 0x77)
	token := signTestCapability(t, secret, testClaims(now))
	calls := 0
	verifier.now = func() time.Time {
		calls++
		if calls == 1 {
			return now
		}
		return now.Add(MaxCapabilityAge)
	}
	if err := verifier.Consume(token, OperationUMIPApply, "grub:preview-digest"); !errors.Is(err, ErrExpiredCapability) {
		t.Fatalf("Consume() error = %v, want %v", err, ErrExpiredCapability)
	}
}

func TestVerifierBoundsConsumedNonceRetention(t *testing.T) {
	verifier, secret, now := testVerifier(t, 0x88)
	for index := range MaxConsumedNonces {
		verifier.consumed[fmt.Sprintf("nonce-%d", index)] = now.Add(MaxCapabilityAge).Unix()
	}
	token := signTestCapability(t, secret, testClaims(now))
	if err := verifier.Consume(token, OperationUMIPApply, "grub:preview-digest"); !errors.Is(err, ErrCapabilityCapacity) {
		t.Fatalf("Consume() error = %v, want %v", err, ErrCapabilityCapacity)
	}
}

func testVerifier(t *testing.T, fill byte) (*Verifier, []byte, time.Time) {
	t.Helper()
	secret := make([]byte, SecretBytes)
	for index := range secret {
		secret[index] = fill
	}
	verifier, err := NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	verifier.now = func() time.Time { return now }
	return verifier, secret, now
}

func testClaims(now time.Time) claims {
	return claims{
		Version: 1, Operation: OperationUMIPApply, Binding: "grub:preview-digest",
		Nonce:    base64.RawURLEncoding.EncodeToString(make([]byte, 24)),
		IssuedAt: now.Unix(), ExpiresAt: now.Add(MaxCapabilityAge).Unix(),
	}
}

func signTestCapability(t *testing.T, secret []byte, value claims) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encodedPayload))
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func differentBase64Character(value byte) string {
	if value == 'A' {
		return "B"
	}
	return "A"
}

func TestLoadEnvironmentClearsValidSecret(t *testing.T) {
	secret := []byte(strings.Repeat("s", SecretBytes))
	t.Setenv(EnvironmentVariable, base64.RawURLEncoding.EncodeToString(secret))
	if _, err := LoadEnvironment(); err != nil {
		t.Fatalf("LoadEnvironment() error: %v", err)
	}
	if _, present := os.LookupEnv(EnvironmentVariable); present {
		t.Fatal("setup secret remained in the Go process environment")
	}
}

func TestLoadEnvironmentClearsMalformedSecret(t *testing.T) {
	t.Setenv(EnvironmentVariable, "malformed")
	if _, err := LoadEnvironment(); err == nil {
		t.Fatal("LoadEnvironment() unexpectedly accepted malformed secret")
	}
	if _, present := os.LookupEnv(EnvironmentVariable); present {
		t.Fatal("malformed setup secret remained in the environment")
	}
}
