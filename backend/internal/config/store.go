package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"hv-launcher/internal/model"
)

const CurrentVersion = 1

const applicationDir = "hv-launcher"

type Store struct {
	mu   sync.RWMutex
	path string
	doc  model.ConfigDocument
}

func DataDir(userHome, xdgDataHome string) (string, error) {
	if xdgDataHome != "" {
		if !filepath.IsAbs(xdgDataHome) {
			return "", errors.New("XDG_DATA_HOME must be an absolute path")
		}
		return filepath.Join(xdgDataHome, applicationDir), nil
	}
	if userHome == "" || !filepath.IsAbs(userHome) {
		return "", errors.New("user home must be an absolute path")
	}
	return filepath.Join(userHome, ".local", "share", applicationDir), nil
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dir, "config.json"),
		doc:  model.ConfigDocument{Version: CurrentVersion, Games: map[string]model.ManagedGame{}},
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.doc); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	changed, err := s.migrate()
	if err != nil {
		return nil, err
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, fmt.Errorf("persist upgraded config: %w", err)
		}
	}
	return s, nil
}

func (s *Store) migrate() (bool, error) {
	if s.doc.Version > CurrentVersion {
		return false, fmt.Errorf("config version %d is newer than supported version %d", s.doc.Version, CurrentVersion)
	}
	changed := false
	if s.doc.Games == nil {
		s.doc.Games = map[string]model.ManagedGame{}
		changed = true
	}
	if s.doc.Version == 0 {
		s.doc.Version = CurrentVersion
		changed = true
	}
	return changed, nil
}

func (s *Store) Snapshot() model.ConfigDocument {
	s.mu.RLock()
	defer s.mu.RUnlock()
	games := make(map[string]model.ManagedGame, len(s.doc.Games))
	for id, game := range s.doc.Games {
		games[id] = game
	}
	return model.ConfigDocument{Version: s.doc.Version, Games: games}
}

func (s *Store) Game(appID string) (model.ManagedGame, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	game, ok := s.doc.Games[appID]
	return game, ok
}

func (s *Store) PutGame(game model.ManagedGame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.doc.Games[game.AppID]
	s.doc.Games[game.AppID] = game
	if err := s.saveLocked(); err != nil {
		if existed {
			s.doc.Games[game.AppID] = previous
		} else {
			delete(s.doc.Games, game.AppID)
		}
		return err
	}
	return nil
}

func (s *Store) DeleteGame(appID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.doc.Games[appID]
	delete(s.doc.Games, appID)
	if err := s.saveLocked(); err != nil {
		if existed {
			s.doc.Games[appID] = previous
		}
		return err
	}
	return nil
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.doc, "", "  ")
	if err != nil {
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
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
