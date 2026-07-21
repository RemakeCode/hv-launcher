package proton

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ulikunitz/xz"
)

type entryKind string

const (
	entryDirectory entryKind = "directory"
	entryRegular   entryKind = "regular"
	entrySymlink   entryKind = "symlink"
	entryHardLink  entryKind = "hard-link"
)

type archiveEntry struct {
	kind           entryKind
	mode           int64
	resolvedTarget string
}

type archiveState struct {
	limits       Limits
	root         string
	entries      map[string]archiveEntry
	manifest     []byte
	entryCount   int
	regularBytes int64
	symlinks     int
	hardLinks    int
}

func validateLimits(limits Limits) error {
	if limits.MaxCompressedBytes <= 0 || limits.MaxExpandedBytes <= 0 || limits.MaxExpandedBytes == math.MaxInt64 ||
		limits.MaxEntries <= 0 || limits.MaxManifestBytes <= 0 || limits.MaxPathBytes <= 0 {
		return validationError(ErrorInvalidLimits, "", "", "all archive limits must be positive and bounded", nil)
	}
	return nil
}

func openCompressedStream(reader io.Reader) (Compression, io.Reader, io.Closer, error) {
	buffered := bufio.NewReader(reader)
	magic, err := buffered.Peek(6)
	if err != nil {
		return "", nil, nil, validationError(ErrorUnsupportedFormat, "", "", "archive is too short to identify", err)
	}
	if magic[0] == 0x1f && magic[1] == 0x8b {
		stream, err := gzip.NewReader(buffered)
		if err != nil {
			return "", nil, nil, validationError(ErrorMalformedArchive, "", "", "open gzip stream", err)
		}
		return CompressionGzip, stream, stream, nil
	}
	if string(magic) == "\xfd7zXZ\x00" {
		stream, err := xz.NewReader(buffered)
		if err != nil {
			return "", nil, nil, validationError(ErrorMalformedArchive, "", "", "open xz stream", err)
		}
		return CompressionXZ, stream, nil, nil
	}
	return "", nil, nil, validationError(ErrorUnsupportedFormat, "", "", "content is neither gzip nor xz", nil)
}

func (s *archiveState) recordPath(name string, kind entryKind, header *tar.Header) error {
	components := strings.Split(name, "/")
	root := components[0]
	if s.root == "" {
		s.root = root
	} else if root != s.root {
		return validationError(ErrorInvalidLayout, name, "", fmt.Sprintf("archive has multiple top-level roots %q and %q", s.root, root), nil)
	}
	if len(components) == 1 && kind != entryDirectory {
		return validationError(ErrorInvalidLayout, name, "", "unrelated top-level files are not allowed", nil)
	}

	if existing, ok := s.entries[name]; ok {
		if existing.kind == entryDirectory && kind == entryDirectory {
			return nil
		}
		return validationError(ErrorPathConflict, name, "", fmt.Sprintf("duplicate path uses %s and %s entries", existing.kind, kind), nil)
	}
	for ancestor := path.Dir(name); ancestor != "." && ancestor != "/"; ancestor = path.Dir(ancestor) {
		if existing, ok := s.entries[ancestor]; ok && existing.kind != entryDirectory {
			return validationError(ErrorPathConflict, name, "", fmt.Sprintf("ancestor %q is a %s", ancestor, existing.kind), nil)
		}
	}
	if kind != entryDirectory {
		prefix := name + "/"
		for existing := range s.entries {
			if strings.HasPrefix(existing, prefix) {
				return validationError(ErrorPathConflict, name, "", fmt.Sprintf("path conflicts with existing child %q", existing), nil)
			}
		}
	}

	record := archiveEntry{kind: kind, mode: header.Mode}
	if kind == entrySymlink || kind == entryHardLink {
		target, err := resolveLinkTarget(name, header.Linkname, s.root, kind, s.limits)
		if err != nil {
			return err
		}
		record.resolvedTarget = target
		if kind == entrySymlink {
			s.symlinks++
		} else {
			s.hardLinks++
		}
	}
	s.entries[name] = record
	return nil
}

func supportedEntryKind(typeFlag byte, name string) (entryKind, error) {
	switch typeFlag {
	case tar.TypeDir:
		return entryDirectory, nil
	case tar.TypeReg, tar.TypeRegA:
		return entryRegular, nil
	case tar.TypeSymlink:
		return entrySymlink, nil
	case tar.TypeLink:
		return entryHardLink, nil
	default:
		return "", validationError(ErrorUnsupportedEntry, name, "", fmt.Sprintf("tar entry type %d is not supported", typeFlag), nil)
	}
}

func normalizeEntryPath(name string, limits Limits) (string, error) {
	if !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') {
		return "", validationError(ErrorUnsafePath, name, "", "path is not valid NUL-free UTF-8", nil)
	}
	if len(name) > limits.MaxPathBytes {
		return "", limitErrorAt("path-bytes", name, fmt.Sprintf("path is %d bytes; maximum is %d", len(name), limits.MaxPathBytes))
	}
	if path.IsAbs(name) {
		return "", validationError(ErrorUnsafePath, name, "", "absolute paths are not allowed", nil)
	}
	for _, component := range strings.Split(name, "/") {
		if component == ".." {
			return "", validationError(ErrorUnsafePath, name, "", "traversal components are not allowed", nil)
		}
	}
	clean := path.Clean(name)
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", validationError(ErrorUnsafePath, name, "", "path does not name an entry inside the tool root", nil)
	}
	return clean, nil
}

func resolveLinkTarget(name, linkName, root string, kind entryKind, limits Limits) (string, error) {
	if !utf8.ValidString(linkName) || strings.ContainsRune(linkName, '\x00') {
		return "", validationError(ErrorUnsafeLink, name, "", "link target is not valid NUL-free UTF-8", nil)
	}
	if len(linkName) > limits.MaxPathBytes {
		return "", limitErrorAt("path-bytes", name, fmt.Sprintf("link target is %d bytes; maximum is %d", len(linkName), limits.MaxPathBytes))
	}
	if path.IsAbs(linkName) {
		return "", validationError(ErrorUnsafeLink, name, "", "absolute link targets are not allowed", nil)
	}

	var resolved string
	if kind == entrySymlink {
		resolved = path.Clean(path.Join(path.Dir(name), linkName))
	} else {
		resolved = path.Clean(linkName)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+"/") {
		return "", validationError(ErrorUnsafeLink, name, "", fmt.Sprintf("link target %q escapes tool root %q", linkName, root), nil)
	}
	return resolved, nil
}

func (s *archiveState) validateLinks() error {
	for name, entry := range s.entries {
		if entry.kind == entryHardLink && !s.hardLinkTargetsRegular(entry.resolvedTarget, map[string]bool{name: true}) {
			return validationError(ErrorUnsafeLink, name, "", fmt.Sprintf("hard-link target %q is not a regular archive entry", entry.resolvedTarget), nil)
		}
	}
	return nil
}

func (s *archiveState) hardLinkTargetsRegular(name string, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	entry, ok := s.entries[name]
	if !ok {
		return false
	}
	if entry.kind == entryRegular {
		return true
	}
	if entry.kind != entryHardLink {
		return false
	}
	return s.hardLinkTargetsRegular(entry.resolvedTarget, seen)
}

func (s *archiveState) validateLayout() (Inspection, error) {
	if s.root == "" {
		return Inspection{}, validationError(ErrorInvalidLayout, "", "", "tar archive contains no entries", nil)
	}
	files := path.Join(s.root, "files")
	filesPayload := false
	if entry, ok := s.entries[files]; ok && entry.kind == entryDirectory {
		filesPayload = true
	} else {
		for name := range s.entries {
			if strings.HasPrefix(name, files+"/") {
				filesPayload = true
				break
			}
		}
	}
	structure, err := validateStructure(s.root, s.entries, s.manifest, filesPayload)
	if err != nil {
		return Inspection{}, err
	}

	return Inspection{
		ToolRoot:         s.root,
		ManifestTool:     structure.manifestTool,
		Payload:          structure.payload,
		EmptyDirectories: s.emptyDirectoryCount(),
		SymbolicLinks:    s.symlinks,
		HardLinks:        s.hardLinks,
		Checks:           structure.checks,
	}, nil
}

func (s *archiveState) emptyDirectoryCount() int {
	count := 0
	for name, entry := range s.entries {
		if entry.kind != entryDirectory {
			continue
		}
		prefix := name + "/"
		empty := true
		for candidate := range s.entries {
			if strings.HasPrefix(candidate, prefix) {
				empty = false
				break
			}
		}
		if empty {
			count++
		}
	}
	return count
}

func validationError(code ErrorCode, name, limit, detail string, err error) error {
	return &ValidationError{Code: code, Path: name, Limit: limit, Detail: detail, Err: err}
}

func limitError(limit, detail string) error {
	return validationError(ErrorResourceLimit, "", limit, detail, nil)
}

func limitErrorAt(limit, name, detail string) error {
	return validationError(ErrorResourceLimit, name, limit, detail, nil)
}
