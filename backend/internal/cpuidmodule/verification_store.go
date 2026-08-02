package cpuidmodule

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"hv-launcher/internal/model"
)

const (
	VerificationStoreVersion = 1
	MaxOutcomeDetail         = 4 << 10
	MaxOutcomeRemedy         = 2 << 10
)

type persistedVerification struct {
	Version        int                                    `json:"version"`
	State          model.ModuleVerificationState          `json:"state"`
	Classification model.ModuleVerificationClassification `json:"classification,omitempty"`
	Detail         string                                 `json:"detail,omitempty"`
	Remediation    string                                 `json:"remediation,omitempty"`
}

// FileVerificationStore deliberately contains no module or kernel identity. The
// live inspector remains authoritative for whether a compatible module exists.
type FileVerificationStore struct {
	mu   sync.Mutex
	path string
}

func NewVerificationStore(path string) *FileVerificationStore {
	return &FileVerificationStore{path: path}
}

func (s *FileVerificationStore) Load() (model.ModuleVerificationOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *FileVerificationStore) SaveIfChanged(outcome model.ModuleVerificationOutcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	outcome = normalizeVerificationOutcome(outcome)
	current, err := s.loadLocked()
	if err == nil && current == outcome {
		if _, statErr := os.Stat(s.path); statErr == nil {
			return nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}

	document := persistedVerification{
		Version: VerificationStoreVersion, State: outcome.State,
		Classification: outcome.Classification, Detail: outcome.Detail,
		Remediation: outcome.Remediation,
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	defer os.Remove(tmp)

	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	file, err := os.OpenFile(tmp, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *FileVerificationStore) loadLocked() (model.ModuleVerificationOutcome, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return pendingVerification(), nil
	}
	if err != nil {
		return pendingVerification(), err
	}
	var document persistedVerification
	if err := json.Unmarshal(data, &document); err != nil {
		return pendingVerification(), fmt.Errorf("decode module verification state: %w", err)
	}
	if document.Version != VerificationStoreVersion || !validState(document.State) {
		return pendingVerification(), errors.New("module verification state is missing a supported version or state")
	}
	return normalizeVerificationOutcome(model.ModuleVerificationOutcome{
		State: document.State, Classification: document.Classification,
		Detail: document.Detail, Remediation: document.Remediation,
	}), nil
}

func pendingVerification() model.ModuleVerificationOutcome {
	return model.ModuleVerificationOutcome{State: model.ModuleVerificationPending}
}

func validState(state model.ModuleVerificationState) bool {
	return state == model.ModuleVerificationPending || state == model.ModuleVerificationVerified || state == model.ModuleVerificationFailed
}

func normalizeVerificationOutcome(outcome model.ModuleVerificationOutcome) model.ModuleVerificationOutcome {
	if !validState(outcome.State) {
		outcome.State = model.ModuleVerificationPending
	}
	outcome.Detail = boundedText(outcome.Detail, MaxOutcomeDetail)
	outcome.Remediation = boundedText(outcome.Remediation, MaxOutcomeRemedy)
	return outcome
}

func boundedText(value string, maximum int) string {
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' || unicode.IsPrint(character) {
			return character
		}
		return -1
	}, value)
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")
	value = strings.TrimSpace(value)
	for len(value) > maximum {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
