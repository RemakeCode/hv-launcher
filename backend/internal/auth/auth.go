package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	SecretBytes         = 32
	MaxBindingBytes     = 4_096
	MaxCapabilityAge    = 60 * time.Second
	MaxConsumedNonces   = 4_096
	maxTokenBytes       = 8 << 10
	clockSkew           = 5 * time.Second
	EnvironmentVariable = "HV_LAUNCHER_SETUP_SECRET"
)

type Operation string

const (
	OperationUMIPApply     Operation = "umip-apply"
	OperationModuleInstall Operation = "module-install"
)

var (
	ErrInvalidCapability  = errors.New("invalid setup capability")
	ErrExpiredCapability  = errors.New("setup capability has expired")
	ErrCapabilityMismatch = errors.New("setup capability does not match the requested operation")
	ErrCapabilityReplay   = errors.New("setup capability has already been used")
	ErrCapabilityCapacity = errors.New("setup capability replay protection is at capacity")
)

type claims struct {
	Version   int       `json:"version"`
	Operation Operation `json:"operation"`
	Binding   string    `json:"binding"`
	Nonce     string    `json:"nonce"`
	IssuedAt  int64     `json:"issuedAt"`
	ExpiresAt int64     `json:"expiresAt"`
}

type Verifier struct {
	secret   []byte
	now      func() time.Time
	mu       sync.Mutex
	consumed map[string]int64
}

func NewVerifier(secret []byte) (*Verifier, error) {
	if len(secret) != SecretBytes {
		return nil, fmt.Errorf("setup capability secret must be %d bytes", SecretBytes)
	}

	return &Verifier{
		secret: append([]byte(nil), secret...), now: time.Now, consumed: make(map[string]int64),
	}, nil
}

// Consume verifies a capability's signature and exact binding, then records
// its nonce atomically before the caller begins the privileged operation.
func (v *Verifier) Consume(token string, operation Operation, binding string) error {
	parsed, err := v.verify(token)
	if err != nil {
		return err
	}
	if parsed.Operation != operation || !hmac.Equal([]byte(parsed.Binding), []byte(binding)) {
		return ErrCapabilityMismatch
	}

	now := v.now().Unix()
	v.mu.Lock()
	defer v.mu.Unlock()
	for nonce, expiresAt := range v.consumed {
		if expiresAt <= now {
			delete(v.consumed, nonce)
		}
	}

	if parsed.ExpiresAt <= now {
		return ErrExpiredCapability
	}
	if _, used := v.consumed[parsed.Nonce]; used {
		return ErrCapabilityReplay
	}
	if len(v.consumed) >= MaxConsumedNonces {
		return ErrCapabilityCapacity
	}

	v.consumed[parsed.Nonce] = parsed.ExpiresAt
	return nil
}

func (v *Verifier) verify(token string) (claims, error) {
	if token == "" || len(token) > maxTokenBytes {
		return claims{}, ErrInvalidCapability
	}

	encodedPayload, encodedSignature, found := strings.Cut(token, ".")
	if !found || encodedPayload == "" || encodedSignature == "" || strings.Contains(encodedSignature, ".") {
		return claims{}, ErrInvalidCapability
	}
	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return claims{}, ErrInvalidCapability
	}
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || len(signature) != sha256.Size {
		return claims{}, ErrInvalidCapability
	}

	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write([]byte(encodedPayload))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return claims{}, ErrInvalidCapability
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var parsed claims
	if err := decoder.Decode(&parsed); err != nil {
		return claims{}, ErrInvalidCapability
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return claims{}, ErrInvalidCapability
	}

	if err := validateClaims(parsed, v.now()); err != nil {
		return claims{}, err
	}
	return parsed, nil
}

func validateClaims(parsed claims, now time.Time) error {
	if parsed.Version != 1 || !validOperation(parsed.Operation) ||
		parsed.Binding == "" || len(parsed.Binding) > MaxBindingBytes || !utf8.ValidString(parsed.Binding) ||
		strings.ContainsRune(parsed.Binding, '\x00') || !validNonce(parsed.Nonce) {
		return ErrInvalidCapability
	}

	issuedAt := time.Unix(parsed.IssuedAt, 0)
	expiresAt := time.Unix(parsed.ExpiresAt, 0)
	if !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > MaxCapabilityAge || issuedAt.After(now.Add(clockSkew)) {
		return ErrInvalidCapability
	}
	if !expiresAt.After(now) {
		return ErrExpiredCapability
	}
	return nil
}

func validOperation(operation Operation) bool {
	switch operation {
	case OperationUMIPApply, OperationModuleInstall:
		return true
	default:
		return false
	}
}

func validNonce(nonce string) bool {
	if len(nonce) < 16 || len(nonce) > 128 {
		return false
	}

	decoded, err := base64.RawURLEncoding.DecodeString(nonce)
	return err == nil && len(decoded) >= 16
}

// LoadEnvironment removes the inherited secret before validating it so no
// child process started by the Go service can inherit the value.
func LoadEnvironment() (*Verifier, error) {
	encoded, present := os.LookupEnv(EnvironmentVariable)
	if err := os.Unsetenv(EnvironmentVariable); err != nil {
		return nil, fmt.Errorf("clear setup capability secret: %w", err)
	}

	if !present || encoded == "" {
		return nil, errors.New("setup capability secret is required")
	}
	secret, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("setup capability secret is malformed")
	}

	verifier, err := NewVerifier(secret)
	clear(secret)
	if err != nil {
		return nil, err
	}

	return verifier, nil
}
