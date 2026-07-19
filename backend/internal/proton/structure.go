package proton

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type structureResult struct {
	manifestTool string
	payload      string
	checks       []Check
}

// InstalledTool is the read-only result of validating an installed Steam
// compatibility tool directory.
type InstalledTool struct {
	Name         string  `json:"name"`
	ManifestTool string  `json:"manifestTool"`
	Payload      string  `json:"payload"`
	Checks       []Check `json:"checks"`
}

func validateStructure(root string, entries map[string]archiveEntry, manifest []byte, filesPayload bool) (structureResult, error) {
	if root == "" {
		return structureResult{}, validationError(ErrorInvalidLayout, "", "", "tool root is missing", nil)
	}
	if !strings.Contains(strings.ToLower(root), "linuwux") {
		return structureResult{}, validationError(ErrorInvalidLayout, root, "", "tool root does not identify as LinUwUx", nil)
	}

	for _, relative := range []string{"compatibilitytool.vdf", "toolmanifest.vdf", "version"} {
		name := path.Join(root, relative)
		if entry, ok := entries[name]; !ok || entry.kind != entryRegular {
			return structureResult{}, validationError(ErrorInvalidLayout, name, "", "required regular file is missing", nil)
		}
	}
	launcher := path.Join(root, "proton")
	launcherEntry, ok := entries[launcher]
	if !ok || launcherEntry.kind != entryRegular {
		return structureResult{}, validationError(ErrorInvalidLayout, launcher, "", "required regular launcher is missing", nil)
	}
	if launcherEntry.mode&0o111 == 0 {
		return structureResult{}, validationError(ErrorInvalidLayout, launcher, "", "launcher is not executable", nil)
	}

	manifestTool, err := parseProtonManifest(manifest)
	if err != nil {
		return structureResult{}, validationError(ErrorInvalidManifest, path.Join(root, "compatibilitytool.vdf"), "", "manifest does not declare a root-installed Steam compatibility tool", err)
	}
	payload := ""
	if filesPayload {
		payload = "files"
	} else {
		for _, candidate := range []string{"proton_dist.tar.gz", "proton_dist.tar"} {
			name := path.Join(root, candidate)
			if entry, ok := entries[name]; ok && entry.kind == entryRegular {
				payload = candidate
				break
			}
		}
	}
	if payload == "" {
		return structureResult{}, validationError(ErrorInvalidLayout, root, "", "recognized Proton payload is missing", nil)
	}

	return structureResult{
		manifestTool: manifestTool,
		payload:      payload,
		checks: []Check{
			{ID: "single-root", OK: true, Detail: root},
			{ID: "linuwux-identity", OK: true, Detail: "tool root identifies as LinUwUx"},
			{ID: "compatibility-manifest", OK: true, Detail: manifestTool},
			{ID: "proton-launcher", OK: true, Detail: "executable proton launcher"},
			{ID: "tool-manifest", OK: true, Detail: "toolmanifest.vdf"},
			{ID: "version", OK: true, Detail: "version"},
			{ID: "payload", OK: true, Detail: payload},
		},
	}, nil
}

// InspectInstalled validates one installed compatibility tool without changing
// it or following symbolic links for any required structural entry.
func InspectInstalled(rootPath string) (InstalledTool, error) {
	return InspectInstalledWithLimits(rootPath, DefaultLimits())
}

func InspectInstalledWithLimits(rootPath string, limits Limits) (InstalledTool, error) {
	if err := validateLimits(limits); err != nil {
		return InstalledTool{}, err
	}
	rootPath = filepath.Clean(rootPath)
	rootInfo, err := os.Lstat(rootPath)
	if err != nil {
		return InstalledTool{}, validationError(ErrorArchiveIO, rootPath, "", "inspect installed tool root", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return InstalledTool{}, validationError(ErrorInvalidLayout, rootPath, "", "installed tool root must be a directory, not a symbolic link", nil)
	}
	root := filepath.Base(rootPath)
	entries := make(map[string]archiveEntry)
	for _, relative := range []string{
		"compatibilitytool.vdf", "proton", "toolmanifest.vdf", "version",
		"proton_dist.tar.gz", "proton_dist.tar", "files",
	} {
		entryPath := filepath.Join(rootPath, filepath.FromSlash(relative))
		info, statErr := os.Lstat(entryPath)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return InstalledTool{}, validationError(ErrorArchiveIO, path.Join(root, relative), "", "inspect installed tool entry", statErr)
		}
		entries[path.Join(root, relative)] = archiveEntry{kind: installedEntryKind(info), mode: int64(info.Mode().Perm())}
	}

	manifestPath := filepath.Join(rootPath, "compatibilitytool.vdf")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return InstalledTool{}, validationError(ErrorInvalidLayout, path.Join(root, "compatibilitytool.vdf"), "", "required regular file is missing", nil)
		}
		return InstalledTool{}, validationError(ErrorArchiveIO, manifestPath, "", "inspect compatibility manifest", err)
	}
	if !manifestInfo.Mode().IsRegular() {
		return InstalledTool{}, validationError(ErrorInvalidLayout, path.Join(root, "compatibilitytool.vdf"), "", "required regular file is missing", nil)
	}
	if manifestInfo.Size() > limits.MaxManifestBytes {
		return InstalledTool{}, limitErrorAt("manifest-bytes", path.Join(root, "compatibilitytool.vdf"), fmt.Sprintf("manifest is %d bytes; maximum is %d", manifestInfo.Size(), limits.MaxManifestBytes))
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return InstalledTool{}, validationError(ErrorArchiveIO, manifestPath, "", "read compatibility manifest", err)
	}

	filesEntry, filesPresent := entries[path.Join(root, "files")]
	structure, err := validateStructure(root, entries, manifest, filesPresent && filesEntry.kind == entryDirectory)
	if err != nil {
		return InstalledTool{}, err
	}
	return InstalledTool{Name: root, ManifestTool: structure.manifestTool, Payload: structure.payload, Checks: structure.checks}, nil
}

func installedEntryKind(info os.FileInfo) entryKind {
	switch {
	case info.Mode().IsRegular():
		return entryRegular
	case info.IsDir():
		return entryDirectory
	case info.Mode()&os.ModeSymlink != 0:
		return entrySymlink
	default:
		return entryKind("unsupported")
	}
}
