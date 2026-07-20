package cpuidmodule

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const sourceWarning = "HV Launcher cannot verify this archive's origin. DKMS will execute its Makefile as root."

type Inspector struct {
	Limits Limits
}

func NewInspector() *Inspector { return &Inspector{Limits: DefaultLimits()} }

// PreflightPath performs only the fast checks needed before the user confirms
// installation. Full archive validation belongs to the installation job.
func (i *Inspector) PreflightPath(selectedPath string) (ArchivePreflight, error) {
	file, size, err := openSelectedFile(selectedPath)
	if err != nil {
		return ArchivePreflight{}, err
	}
	defer file.Close()
	if err := validateLimits(i.Limits); err != nil {
		return ArchivePreflight{}, err
	}
	if size <= 0 || size > i.Limits.MaxCompressedBytes {
		return ArchivePreflight{}, fmt.Errorf("%w: compressed archive size is %d bytes", ErrResourceLimit, size)
	}
	var signature [4]byte
	if _, err := file.ReadAt(signature[:], 0); err != nil ||
		(string(signature[:]) != "PK\x03\x04" && string(signature[:]) != "PK\x05\x06" && string(signature[:]) != "PK\x07\x08") {
		return ArchivePreflight{}, fmt.Errorf("%w: selected file is not a ZIP archive", ErrInvalidArchive)
	}
	return ArchivePreflight{
		FileName: filepath.Base(selectedPath), CompressedBytes: size, Warning: sourceWarning,
	}, nil
}

func (i *Inspector) ValidatePath(selectedPath string) (Inspection, error) {
	file, size, err := openSelectedFile(selectedPath)
	if err != nil {
		return Inspection{}, err
	}
	defer file.Close()
	return i.validateOpen(file, filepath.Base(selectedPath), size)
}

func openSelectedFile(selectedPath string) (*os.File, int64, error) {
	if selectedPath == "" || !filepath.IsAbs(selectedPath) || len(selectedPath) > MaxSelectedPathBytes || strings.ContainsRune(selectedPath, '\x00') {
		return nil, 0, fmt.Errorf("%w: a bounded absolute ZIP path is required", ErrInvalidArchive)
	}
	file, err := os.Open(selectedPath)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: open selected archive: %v", ErrInvalidArchive, err)
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		file.Close()
		return nil, 0, fmt.Errorf("%w: selected path must resolve to a readable regular file", ErrInvalidArchive)
	}
	return file, opened.Size(), nil
}

func (i *Inspector) validateOpen(file *os.File, fileName string, size int64) (Inspection, error) {
	limits := i.Limits
	if err := validateLimits(limits); err != nil {
		return Inspection{}, err
	}
	if size <= 0 || size > limits.MaxCompressedBytes {
		return Inspection{}, fmt.Errorf("%w: compressed archive size is %d bytes", ErrResourceLimit, size)
	}
	digest, err := digestOpenFile(file, size)
	if err != nil {
		return Inspection{}, err
	}
	archive, err := zip.NewReader(file, size)
	if err != nil {
		return Inspection{}, fmt.Errorf("%w: open ZIP: %v", ErrInvalidArchive, err)
	}
	if len(archive.File) == 0 || len(archive.File) > limits.MaxEntries {
		return Inspection{}, fmt.Errorf("%w: archive contains %d entries", ErrResourceLimit, len(archive.File))
	}

	seen := make(map[string]bool, len(archive.File))
	regular := make(map[string]bool, len(archive.File))
	records := make([]sourceRecord, 0, len(archive.File))
	var expanded int64
	var dkmsConfig []byte
	for _, entry := range archive.File {
		name, directory, err := normalizeZIPPath(entry, limits)
		if err != nil {
			return Inspection{}, err
		}
		if seen[name] {
			return Inspection{}, fmt.Errorf("%w: duplicate path %q", ErrUnsafeEntry, name)
		}
		seen[name] = true
		for ancestor := path.Dir(name); ancestor != "."; ancestor = path.Dir(ancestor) {
			if regular[ancestor] {
				return Inspection{}, fmt.Errorf("%w: %q has a regular-file ancestor", ErrUnsafeEntry, name)
			}
		}
		if directory {
			continue
		}
		for existing := range seen {
			if strings.HasPrefix(existing, name+"/") {
				return Inspection{}, fmt.Errorf("%w: regular path %q conflicts with a child", ErrUnsafeEntry, name)
			}
		}
		regular[name] = true
		if entry.UncompressedSize64 > uint64(limits.MaxFileBytes) ||
			entry.UncompressedSize64 > uint64(limits.MaxExpandedBytes-expanded) {
			return Inspection{}, fmt.Errorf("%w: %q is too large", ErrResourceLimit, name)
		}
		content, digest, err := readZIPFile(entry, limits.MaxFileBytes)
		if err != nil {
			return Inspection{}, fmt.Errorf("%w: read %q: %v", ErrInvalidArchive, name, err)
		}
		expanded += int64(len(content))
		records = append(records, sourceRecord{path: name, size: int64(len(content)), digest: digest})
		if name == "dkms.conf" {
			if int64(len(content)) > limits.MaxDKMSConfigBytes {
				return Inspection{}, fmt.Errorf("%w: dkms.conf is too large", ErrResourceLimit)
			}
			dkmsConfig = content
		}
	}
	for _, required := range requiredFiles {
		if !regular[required] {
			return Inspection{}, fmt.Errorf("%w: required regular file %q is missing", ErrInvalidArchive, required)
		}
	}
	identity, err := ParseDKMSConfig(dkmsConfig)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{
		FileName: fileName, SHA256: digest,
		SourceDigest: normalizedSourceDigest(records), Identity: identity,
		EntryCount: len(archive.File), ExpandedBytes: expanded,
		RequiredFiles: append([]string(nil), requiredFiles...), Warning: sourceWarning,
	}, nil
}

func digestOpenFile(file *os.File, size int64) (string, error) {
	hash := sha256.New()
	written, err := io.Copy(hash, io.NewSectionReader(file, 0, size))
	if err != nil || written != size {
		return "", fmt.Errorf("%w: hash selected archive", ErrInvalidArchive)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validateLimits(limits Limits) error {
	if limits.MaxCompressedBytes <= 0 || limits.MaxExpandedBytes <= 0 || limits.MaxFileBytes <= 0 ||
		limits.MaxDKMSConfigBytes <= 0 || limits.MaxEntries <= 0 || limits.MaxPathBytes <= 0 {
		return errors.New("all CPUID module inspection limits must be positive")
	}
	return nil
}

func normalizeZIPPath(entry *zip.File, limits Limits) (string, bool, error) {
	name := entry.Name
	if !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") || len(name) > limits.MaxPathBytes {
		return "", false, fmt.Errorf("%w: invalid ZIP path", ErrUnsafeEntry)
	}
	if path.IsAbs(name) {
		return "", false, fmt.Errorf("%w: absolute path %q", ErrUnsafeEntry, name)
	}
	directory := strings.HasSuffix(name, "/")
	clean := path.Clean(strings.TrimSuffix(name, "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != strings.TrimSuffix(name, "/") {
		return "", false, fmt.Errorf("%w: non-normal path %q", ErrUnsafeEntry, name)
	}
	mode := entry.Mode()
	if directory {
		if !mode.IsDir() {
			return "", false, fmt.Errorf("%w: ambiguous directory %q", ErrUnsafeEntry, name)
		}
		return clean, true, nil
	}
	if !mode.IsRegular() {
		return "", false, fmt.Errorf("%w: links and special entries are not supported at %q", ErrUnsafeEntry, name)
	}
	if entry.Flags&0x1 != 0 {
		return "", false, fmt.Errorf("%w: encrypted entry %q is unsupported", ErrUnsafeEntry, name)
	}
	return clean, false, nil
}

func readZIPFile(entry *zip.File, maximum int64) ([]byte, [sha256.Size]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	if int64(len(content)) > maximum || uint64(len(content)) != entry.UncompressedSize64 {
		return nil, [sha256.Size]byte{}, errors.New("expanded entry size is invalid")
	}
	if err := reader.Close(); err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	return content, sha256.Sum256(content), nil
}
