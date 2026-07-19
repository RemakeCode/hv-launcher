package proton

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Preflight struct {
	FileName        string        `json:"fileName"`
	Compression     Compression   `json:"compression"`
	CompressedBytes int64         `json:"compressedBytes"`
	Destinations    []Destination `json:"destinations"`
}

type ProgressFunc func(phase string, progress int, message string)

type InstallResult struct {
	ToolName      string `json:"toolName"`
	DestinationID string `json:"destinationId"`
	SHA256        string `json:"sha256"`
	RestartSteam  bool   `json:"restartSteam"`
}

type Installer struct {
	UserHome string
	Limits   Limits

	beforeStageValidation func(string) error
	rename                func(string, string) error
}

func NewInstaller(userHome string) *Installer {
	return &Installer{UserHome: userHome, Limits: DefaultLimits(), rename: os.Rename}
}

func (i *Installer) PreflightPath(archivePath string) (Preflight, error) {
	archive, err := i.openArchive(archivePath)
	if err != nil {
		return Preflight{}, err
	}
	defer archive.Close()
	info, err := archive.Stat()
	if err != nil {
		return Preflight{}, fmt.Errorf("inspect selected archive: %w", err)
	}
	compression, err := detectCompression(archive)
	if err != nil {
		return Preflight{}, err
	}
	return Preflight{
		FileName: filepath.Base(archivePath), Compression: compression, CompressedBytes: info.Size(),
		Destinations: DiscoverDestinations(i.UserHome),
	}, nil
}

func (i *Installer) Install(ctx context.Context, archivePath, destinationID string, progress ProgressFunc) (InstallResult, error) {
	report(progress, "opening-archive", 5, "Opening the selected Proton archive")
	destination, err := ValidateDestination(i.UserHome, destinationID)
	if err != nil {
		return InstallResult{}, err
	}
	limits := i.limits()
	if err := validateLimits(limits); err != nil {
		return InstallResult{}, err
	}
	archive, err := i.openArchive(archivePath)
	if err != nil {
		return InstallResult{}, err
	}
	defer archive.Close()
	archiveInfo, err := archive.Stat()
	if err != nil {
		return InstallResult{}, fmt.Errorf("inspect selected archive: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}

	compatibilityTools, err := i.ensureCompatibilityTools(destination.compatibilityTools)
	if err != nil {
		return InstallResult{}, err
	}
	staging, err := os.MkdirTemp(compatibilityTools, ".hv-launcher-stage-")
	if err != nil {
		return InstallResult{}, fmt.Errorf("create installation staging directory: %w", err)
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		_ = os.RemoveAll(staging)
		return InstallResult{}, fmt.Errorf("secure installation staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	report(progress, "extracting-and-validating", 20, "Safely extracting and validating the archive")
	trackedArchive := &compressedProgressReader{reader: archive, total: archiveInfo.Size(), progress: progress, last: 20}
	extracted, err := extractArchive(ctx, trackedArchive, staging, limits)
	if err != nil {
		return InstallResult{}, err
	}
	finalPath := filepath.Join(compatibilityTools, extracted.ToolRoot)
	if _, err := os.Lstat(finalPath); err == nil {
		return InstallResult{}, fmt.Errorf("compatibility tool %q already exists", extracted.ToolRoot)
	} else if !os.IsNotExist(err) {
		return InstallResult{}, fmt.Errorf("inspect final compatibility tool: %w", err)
	}
	stagedRoot := filepath.Join(staging, extracted.ToolRoot)
	if i.beforeStageValidation != nil {
		if err := i.beforeStageValidation(stagedRoot); err != nil {
			return InstallResult{}, err
		}
	}
	report(progress, "validating-staged-tool", 75, "Validating the staged Proton tool")
	if _, err := InspectInstalledWithLimits(stagedRoot, limits); err != nil {
		return InstallResult{}, fmt.Errorf("validate staged compatibility tool: %w", err)
	}
	if _, err := os.Lstat(finalPath); err == nil {
		return InstallResult{}, fmt.Errorf("compatibility tool %q appeared during installation", extracted.ToolRoot)
	} else if !os.IsNotExist(err) {
		return InstallResult{}, fmt.Errorf("recheck final compatibility tool: %w", err)
	}
	rename := i.rename
	if rename == nil {
		rename = os.Rename
	}
	report(progress, "committing", 90, "Making the installed Proton tool available to Steam")
	if err := rename(stagedRoot, finalPath); err != nil {
		return InstallResult{}, fmt.Errorf("commit compatibility tool: %w", err)
	}
	return InstallResult{ToolName: extracted.ToolRoot, DestinationID: destination.ID, SHA256: extracted.SHA256, RestartSteam: true}, nil
}

type compressedProgressReader struct {
	reader   io.Reader
	total    int64
	read     int64
	progress ProgressFunc
	last     int
}

func (r *compressedProgressReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.read += int64(count)
	if r.progress != nil && r.total > 0 {
		percent := 20 + int(r.read*50/r.total)
		if percent > 70 {
			percent = 70
		}
		if percent >= r.last+2 {
			r.last = percent
			r.progress("extracting-and-validating", percent, "")
		}
	}
	return count, err
}

func report(progress ProgressFunc, phase string, percent int, message string) {
	if progress != nil {
		progress(phase, percent, message)
	}
}

func (i *Installer) limits() Limits {
	if i.Limits.MaxCompressedBytes == 0 {
		return DefaultLimits()
	}
	return i.Limits
}

func (i *Installer) openArchive(archivePath string) (*os.File, error) {
	if !supportedArchiveSuffix(archivePath) {
		return nil, fmt.Errorf("archive must end in .tar.gz, .tgz, or .tar.xz")
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open selected archive: %w", err)
	}
	info, err := archive.Stat()
	if err != nil {
		archive.Close()
		return nil, fmt.Errorf("inspect selected archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		archive.Close()
		return nil, fmt.Errorf("selected archive is not a regular file")
	}
	if info.Size() > i.limits().MaxCompressedBytes {
		archive.Close()
		return nil, limitError("compressed-bytes", fmt.Sprintf("archive is %d bytes; maximum is %d", info.Size(), i.limits().MaxCompressedBytes))
	}
	return archive, nil
}

func detectCompression(archive io.ReadSeeker) (Compression, error) {
	var magic [6]byte
	if _, err := io.ReadFull(archive, magic[:]); err != nil {
		return "", validationError(ErrorUnsupportedFormat, "", "", "archive is too short to identify", err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", validationError(ErrorArchiveIO, "", "", "rewind archive", err)
	}
	if magic[0] == 0x1f && magic[1] == 0x8b {
		return CompressionGzip, nil
	}
	if string(magic[:]) == "\xfd7zXZ\x00" {
		return CompressionXZ, nil
	}
	return "", validationError(ErrorUnsupportedFormat, "", "", "content is neither gzip nor xz", nil)
}

func (i *Installer) ensureCompatibilityTools(directory string) (string, error) {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		if err := os.Mkdir(directory, 0o755); err != nil {
			return "", fmt.Errorf("create compatibilitytools.d: %w", err)
		}
		return directory, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect compatibilitytools.d: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("compatibilitytools.d must be a directory, not a symbolic link")
	}
	return directory, nil
}

func supportedArchiveSuffix(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar.xz")
}

type pendingLink struct {
	name           string
	linkName       string
	resolvedTarget string
	kind           entryKind
}

func extractArchive(ctx context.Context, archive io.Reader, staging string, limits Limits) (Inspection, error) {
	hash := sha256.New()
	format, decoded, closer, err := openCompressedStream(io.TeeReader(archive, hash))
	if err != nil {
		return Inspection{}, err
	}
	if closer != nil {
		defer closer.Close()
	}
	limited := &io.LimitedReader{R: decoded, N: limits.MaxExpandedBytes + 1}
	tarReader := tar.NewReader(limited)
	state := archiveState{limits: limits, entries: make(map[string]archiveEntry)}
	links := make([]pendingLink, 0)

	for {
		if err := ctx.Err(); err != nil {
			return Inspection{}, err
		}
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if limited.N == 0 {
				return Inspection{}, limitError("expanded-bytes", fmt.Sprintf("archive expands beyond %d bytes", limits.MaxExpandedBytes))
			}
			return Inspection{}, validationError(ErrorMalformedArchive, "", "", "read tar header during extraction", err)
		}
		state.entryCount++
		if state.entryCount > limits.MaxEntries {
			return Inspection{}, limitError("entry-count", fmt.Sprintf("archive has more than %d entries", limits.MaxEntries))
		}
		name, err := normalizeEntryPath(header.Name, limits)
		if err != nil {
			return Inspection{}, err
		}
		kind, err := supportedEntryKind(header.Typeflag, name)
		if err != nil {
			return Inspection{}, err
		}
		if err := state.recordPath(name, kind, header); err != nil {
			return Inspection{}, err
		}
		destination := filepath.Join(staging, filepath.FromSlash(name))
		switch kind {
		case entryDirectory:
			if err := os.MkdirAll(destination, safeDirectoryMode(header.Mode)); err != nil {
				return Inspection{}, fmt.Errorf("create directory %q: %w", name, err)
			}
			if err := os.Chmod(destination, safeDirectoryMode(header.Mode)); err != nil {
				return Inspection{}, fmt.Errorf("set directory mode %q: %w", name, err)
			}
		case entryRegular:
			if header.Size < 0 || header.Size > limits.MaxExpandedBytes-state.regularBytes {
				return Inspection{}, limitError("expanded-bytes", fmt.Sprintf("regular files expand beyond %d bytes", limits.MaxExpandedBytes))
			}
			state.regularBytes += header.Size
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return Inspection{}, fmt.Errorf("create parent directory for %q: %w", name, err)
			}
			file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return Inspection{}, fmt.Errorf("create regular file %q: %w", name, err)
			}
			var manifest bytes.Buffer
			writer := io.Writer(file)
			if name == path.Join(state.root, "compatibilitytool.vdf") {
				if header.Size > limits.MaxManifestBytes {
					file.Close()
					return Inspection{}, limitError("manifest-bytes", fmt.Sprintf("%s is %d bytes; maximum is %d", name, header.Size, limits.MaxManifestBytes))
				}
				writer = io.MultiWriter(file, &manifest)
			}
			_, copyErr := io.CopyN(writer, tarReader, header.Size)
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil {
				return Inspection{}, fmt.Errorf("write regular file %q: %w", name, errors.Join(copyErr, closeErr))
			}
			if manifest.Len() > 0 || name == path.Join(state.root, "compatibilitytool.vdf") {
				state.manifest = append([]byte(nil), manifest.Bytes()...)
			}
			if err := os.Chmod(destination, safeRegularMode(header.Mode)); err != nil {
				return Inspection{}, fmt.Errorf("set regular file mode %q: %w", name, err)
			}
		case entrySymlink, entryHardLink:
			entry := state.entries[name]
			links = append(links, pendingLink{name: name, linkName: header.Linkname, resolvedTarget: entry.resolvedTarget, kind: kind})
		}
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return Inspection{}, validationError(ErrorMalformedArchive, "", "", "finish compressed stream during extraction", err)
	}
	if limited.N == 0 {
		return Inspection{}, limitError("expanded-bytes", fmt.Sprintf("archive expands beyond %d bytes", limits.MaxExpandedBytes))
	}
	if err := state.validateLinks(); err != nil {
		return Inspection{}, err
	}
	inspection, err := state.validateLayout()
	if err != nil {
		return Inspection{}, err
	}
	if err := createArchiveLinks(staging, links); err != nil {
		return Inspection{}, err
	}
	inspection.Compression = format
	inspection.SHA256 = hex.EncodeToString(hash.Sum(nil))
	inspection.ExpandedBytes = limits.MaxExpandedBytes + 1 - limited.N
	inspection.EntryCount = state.entryCount
	inspection.RegularBytes = state.regularBytes
	return inspection, nil
}

func safeDirectoryMode(mode int64) os.FileMode {
	return os.FileMode(mode&0o777) | 0o700
}

func safeRegularMode(mode int64) os.FileMode {
	return os.FileMode(mode&0o777) | 0o600
}

func createArchiveLinks(staging string, links []pendingLink) error {
	pending := append([]pendingLink(nil), links...)
	for len(pending) > 0 {
		progress := false
		next := pending[:0]
		for _, link := range pending {
			if link.kind != entryHardLink {
				next = append(next, link)
				continue
			}
			target := filepath.Join(staging, filepath.FromSlash(link.resolvedTarget))
			if _, err := os.Stat(target); err != nil {
				next = append(next, link)
				continue
			}
			destination := filepath.Join(staging, filepath.FromSlash(link.name))
			if err := os.Link(target, destination); err != nil {
				return fmt.Errorf("create hard link %q: %w", link.name, err)
			}
			progress = true
		}
		pending = next
		if !progress {
			break
		}
	}
	for _, link := range pending {
		if link.kind == entryHardLink {
			return fmt.Errorf("create hard link %q: target is unavailable", link.name)
		}
	}
	for _, link := range links {
		if link.kind != entrySymlink {
			continue
		}
		destination := filepath.Join(staging, filepath.FromSlash(link.name))
		if err := os.Symlink(link.linkName, destination); err != nil {
			return fmt.Errorf("create symbolic link %q: %w", link.name, err)
		}
	}
	return nil
}
