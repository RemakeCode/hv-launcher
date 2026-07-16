package hypervisor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type ModuleSnapshot struct {
	Emulation bool `json:"emulation"`
	KVM       bool `json:"kvm"`
	KVMAMD    bool `json:"kvmAmd"`
}

type JournalSession struct {
	ID         string `json:"id"`
	AppID      string `json:"appId"`
	InstanceID uint64 `json:"instanceId,omitempty"`
	Source     string `json:"source"`
}

type JournalRecord struct {
	Version  int              `json:"version"`
	Phase    string           `json:"phase"`
	Before   ModuleSnapshot   `json:"before"`
	Owned    bool             `json:"owned"`
	Sessions []JournalSession `json:"sessions"`
}

type Journal interface {
	Load() (*JournalRecord, error)
	Write(record JournalRecord) error
	Clear() error
}

type FileJournal struct {
	path string
}

func NewFileJournal(runtimeDir string) *FileJournal {
	return &FileJournal{path: filepath.Join(runtimeDir, "transition-journal.json")}
}

func (j *FileJournal) Load() (*JournalRecord, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record JournalRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (j *FileJournal) Write(record JournalRecord) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return err
	}
	record.Version = 1
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, j.path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(j.path))
}

func (j *FileJournal) Clear() error {
	err := os.Remove(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(j.path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
