package proton

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var ErrSelectionUnavailable = errors.New("selection is missing or expired; select the archive again")

type SelectionRecord struct {
	ID        string    `json:"selectionId"`
	Path      string    `json:"-"`
	Preflight Preflight `json:"preflight"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type SelectionStore struct {
	mu      sync.Mutex
	records map[string]SelectionRecord
	now     func() time.Time
	ttl     time.Duration
	max     int
}

func NewSelectionStore() *SelectionStore {
	return &SelectionStore{records: make(map[string]SelectionRecord), now: time.Now, ttl: 10 * time.Minute, max: 64}
}

func (s *SelectionStore) Put(path string, preflight Preflight) (SelectionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	if len(s.records) >= s.max {
		return SelectionRecord{}, errors.New("too many pending Proton selections")
	}
	idBytes := make([]byte, 24)
	if _, err := rand.Read(idBytes); err != nil {
		return SelectionRecord{}, err
	}
	record := SelectionRecord{
		ID: base64.RawURLEncoding.EncodeToString(idBytes), Path: path, Preflight: preflight, ExpiresAt: s.now().Add(s.ttl),
	}
	s.records[record.ID] = record
	return record, nil
}

func (s *SelectionStore) Get(id string) (SelectionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	record, ok := s.records[id]
	if !ok {
		return SelectionRecord{}, ErrSelectionUnavailable
	}
	return record, nil
}

func (s *SelectionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
}

func (s *SelectionStore) pruneLocked() {
	now := s.now()
	for id, record := range s.records {
		if !record.ExpiresAt.After(now) {
			delete(s.records, id)
		}
	}
}
