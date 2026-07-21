package proton

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/ulikunitz/xz"
)

const fixtureRoot = "GE-Proton11-1-LinUwUx"

const fixtureManifest = `
"compatibilitytools"
{
    "compat_tools"
    {
        "GE-Proton11-1-LinUwUx"
        {
            "install_path" "."
            "display_name" "GE-Proton11-1-LinUwUx"
            "from_oslist" "windows"
            "to_oslist" "linux"
        }
    }
}
`

type fixtureEntry struct {
	name     string
	typeFlag byte
	mode     int64
	linkName string
	data     []byte
}

func TestArchiveValidationAcceptsGzipAndXZFixtures(t *testing.T) {
	for _, compression := range []Compression{CompressionGzip, CompressionXZ} {
		t.Run(string(compression), func(t *testing.T) {
			archive := buildArchive(t, compression, validFixture(fixtureRoot))
			inspection, err := validateFixtureArchive(t, archive, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}

			digest := sha256.Sum256(archive)
			if inspection.Compression != compression {
				t.Fatalf("compression = %q, want %q", inspection.Compression, compression)
			}
			if inspection.SHA256 != hex.EncodeToString(digest[:]) {
				t.Fatalf("SHA-256 = %q, want %q", inspection.SHA256, hex.EncodeToString(digest[:]))
			}
			if inspection.ToolRoot != fixtureRoot || inspection.ManifestTool != fixtureRoot {
				t.Fatalf("unexpected tool identity: %+v", inspection)
			}
			if inspection.Payload != "files" {
				t.Fatalf("payload = %q, want files", inspection.Payload)
			}
			if inspection.EmptyDirectories != 1 || inspection.SymbolicLinks != 1 || inspection.HardLinks != 1 {
				t.Fatalf("fixture entries were not accounted for: %+v", inspection)
			}
			if inspection.EntryCount != len(validFixture(fixtureRoot)) || inspection.ExpandedBytes <= inspection.RegularBytes {
				t.Fatalf("unexpected archive accounting: %+v", inspection)
			}
			if len(inspection.Checks) != 7 {
				t.Fatalf("checks = %v", inspection.Checks)
			}
			for _, check := range inspection.Checks {
				if !check.OK {
					t.Fatalf("successful inspection returned a failing check: %+v", check)
				}
			}
		})
	}
}

func TestArchiveValidationAcceptsRecognizedPayloadVariants(t *testing.T) {
	tests := []struct {
		name    string
		entries []fixtureEntry
		want    string
	}{
		{
			name: "nested files imply directory",
			entries: append(requiredFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/files/lib/wine", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("wine"),
			}),
			want: "files",
		},
		{
			name: "proton dist tar",
			entries: append(requiredFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/proton_dist.tar", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("payload"),
			}),
			want: "proton_dist.tar",
		},
		{
			name: "compressed proton dist tar",
			entries: append(requiredFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/proton_dist.tar.gz", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("payload"),
			}),
			want: "proton_dist.tar.gz",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inspection, err := validateFixtureArchive(t, buildArchive(t, CompressionGzip, test.entries), DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			if inspection.Payload != test.want {
				t.Fatalf("payload = %q, want %q", inspection.Payload, test.want)
			}
		})
	}
}

func TestArchiveValidationAcceptsMinimalCompatibilityManifest(t *testing.T) {
	entries := replaceEntry(validFixture(fixtureRoot), fixtureEntry{
		name:     fixtureRoot + "/compatibilitytool.vdf",
		typeFlag: tar.TypeReg,
		mode:     0o644,
		data: []byte(`
"compatibilitytools"
{
    "compat_tools"
    {
        "LinUwUx Build" { "install_path" "." }
    }
}
`),
	})

	inspection, err := validateFixtureArchive(t, buildArchive(t, CompressionGzip, entries), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.ManifestTool != "LinUwUx Build" {
		t.Fatalf("manifest tool = %q", inspection.ManifestTool)
	}
}

func TestArchiveValidationAcceptsSafeDanglingSymlink(t *testing.T) {
	entries := append(validFixture(fixtureRoot), fixtureEntry{
		name:     fixtureRoot + "/files/bin/future-link",
		typeFlag: tar.TypeSymlink,
		mode:     0o777,
		linkName: "not-created-yet",
	})

	inspection, err := validateFixtureArchive(t, buildArchive(t, CompressionXZ, entries), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.SymbolicLinks != 2 {
		t.Fatalf("symbolic links = %d, want 2", inspection.SymbolicLinks)
	}
}

func TestArchiveValidationRejectsUnsafeOrIncompleteFixtures(t *testing.T) {
	tests := []struct {
		name    string
		entries []fixtureEntry
		code    ErrorCode
	}{
		{
			name:    "ordinary proton sunset slr",
			entries: validFixture("proton-sunset-slr"),
			code:    ErrorInvalidLayout,
		},
		{
			name:    "missing compatibility manifest",
			entries: removeEntry(validFixture(fixtureRoot), fixtureRoot+"/compatibilitytool.vdf"),
			code:    ErrorInvalidLayout,
		},
		{
			name:    "missing launcher",
			entries: removeEntry(validFixture(fixtureRoot), fixtureRoot+"/proton"),
			code:    ErrorInvalidLayout,
		},
		{
			name:    "launcher is not executable",
			entries: replaceEntry(validFixture(fixtureRoot), fixtureEntry{name: fixtureRoot + "/proton", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("launcher")}),
			code:    ErrorInvalidLayout,
		},
		{
			name:    "missing tool manifest",
			entries: removeEntry(validFixture(fixtureRoot), fixtureRoot+"/toolmanifest.vdf"),
			code:    ErrorInvalidLayout,
		},
		{
			name:    "missing version",
			entries: removeEntry(validFixture(fixtureRoot), fixtureRoot+"/version"),
			code:    ErrorInvalidLayout,
		},
		{
			name:    "missing payload",
			entries: removePrefix(validFixture(fixtureRoot), fixtureRoot+"/files"),
			code:    ErrorInvalidLayout,
		},
		{
			name: "invalid manifest",
			entries: replaceEntry(validFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/compatibilitytool.vdf", typeFlag: tar.TypeReg, mode: 0o644, data: []byte(`"compatibilitytools" {}`),
			}),
			code: ErrorInvalidManifest,
		},
		{
			name: "conflicting duplicate entry",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/proton", typeFlag: tar.TypeDir, mode: 0o755,
			}),
			code: ErrorPathConflict,
		},
		{
			name: "ancestor path conflict",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/files", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("not a directory"),
			}),
			code: ErrorPathConflict,
		},
		{
			name: "traversal path",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/../escape", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("escape"),
			}),
			code: ErrorUnsafePath,
		},
		{
			name: "absolute path",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: "/tmp/escape", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("escape"),
			}),
			code: ErrorUnsafePath,
		},
		{
			name: "unsafe symbolic link",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/files/escape", typeFlag: tar.TypeSymlink, mode: 0o777, linkName: "../../../outside",
			}),
			code: ErrorUnsafeLink,
		},
		{
			name: "unsafe hard link",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/files/escape", typeFlag: tar.TypeLink, mode: 0o644, linkName: "outside",
			}),
			code: ErrorUnsafeLink,
		},
		{
			name: "special file",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: fixtureRoot + "/files/fifo", typeFlag: tar.TypeFifo, mode: 0o644,
			}),
			code: ErrorUnsupportedEntry,
		},
		{
			name: "second top-level root",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: "Other-LinUwUx", typeFlag: tar.TypeDir, mode: 0o755,
			}),
			code: ErrorInvalidLayout,
		},
		{
			name: "unrelated top-level file",
			entries: append(validFixture(fixtureRoot), fixtureEntry{
				name: "README", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("unrelated"),
			}),
			code: ErrorInvalidLayout,
		},
	}

	for _, compression := range []Compression{CompressionGzip, CompressionXZ} {
		for _, test := range tests {
			t.Run(string(compression)+"/"+test.name, func(t *testing.T) {
				_, err := validateFixtureArchive(t, buildArchive(t, compression, test.entries), DefaultLimits())
				assertValidationError(t, err, test.code, "")
			})
		}
	}
}

func TestArchiveValidationEnforcesEveryResourceLimit(t *testing.T) {
	archive := buildArchive(t, CompressionGzip, validFixture(fixtureRoot))
	tests := []struct {
		name   string
		limits Limits
		limit  string
	}{
		{
			name: "compressed bytes",
			limits: withLimits(func(limits *Limits) {
				limits.MaxCompressedBytes = int64(len(archive) - 1)
			}),
			limit: "compressed-bytes",
		},
		{
			name: "expanded bytes",
			limits: withLimits(func(limits *Limits) {
				limits.MaxExpandedBytes = 1_024
			}),
			limit: "expanded-bytes",
		},
		{
			name: "entry count",
			limits: withLimits(func(limits *Limits) {
				limits.MaxEntries = 2
			}),
			limit: "entry-count",
		},
		{
			name: "manifest bytes",
			limits: withLimits(func(limits *Limits) {
				limits.MaxManifestBytes = int64(len(fixtureManifest) - 1)
			}),
			limit: "manifest-bytes",
		},
		{
			name: "path bytes",
			limits: withLimits(func(limits *Limits) {
				limits.MaxPathBytes = len(fixtureRoot) - 1
			}),
			limit: "path-bytes",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateFixtureArchive(t, archive, test.limits)
			assertValidationError(t, err, ErrorResourceLimit, test.limit)
		})
	}
}

func TestArchiveValidationRejectsUnsupportedAndMalformedCompression(t *testing.T) {
	_, err := validateFixtureArchive(t, []byte("not an archive"), DefaultLimits())
	assertValidationError(t, err, ErrorUnsupportedFormat, "")

	truncated := buildArchive(t, CompressionXZ, validFixture(fixtureRoot))
	truncated = truncated[:len(truncated)-8]
	_, err = validateFixtureArchive(t, truncated, DefaultLimits())
	assertValidationError(t, err, ErrorMalformedArchive, "")
}

func TestArchiveValidationRejectsInvalidLimits(t *testing.T) {
	_, err := validateFixtureArchive(t, []byte("unused"), Limits{})
	assertValidationError(t, err, ErrorInvalidLimits, "")
}

func validateFixtureArchive(t *testing.T, archive []byte, limits Limits) (Inspection, error) {
	t.Helper()
	if err := validateLimits(limits); err != nil {
		return Inspection{}, err
	}
	if int64(len(archive)) > limits.MaxCompressedBytes {
		return Inspection{}, limitError("compressed-bytes", "archive exceeds the compressed-size limit")
	}
	return extractArchive(context.Background(), bytes.NewReader(archive), t.TempDir(), limits)
}

func requiredFixture(root string) []fixtureEntry {
	manifest := []byte(fixtureManifest)
	if root != fixtureRoot {
		manifest = []byte(`
"compatibilitytools" { "compat_tools" { "Proton-Sunset" {
"install_path" "." "display_name" "Proton Sunset" "from_oslist" "windows" "to_oslist" "linux"
} } }
`)
	}
	return []fixtureEntry{
		{name: root, typeFlag: tar.TypeDir, mode: 0o755},
		{name: root + "/compatibilitytool.vdf", typeFlag: tar.TypeReg, mode: 0o644, data: manifest},
		{name: root + "/proton", typeFlag: tar.TypeReg, mode: 0o755, data: []byte("launcher")},
		{name: root + "/toolmanifest.vdf", typeFlag: tar.TypeReg, mode: 0o644, data: []byte(`"manifest" {}`)},
		{name: root + "/version", typeFlag: tar.TypeReg, mode: 0o644, data: []byte("11-1\n")},
	}
}

func validFixture(root string) []fixtureEntry {
	return append(requiredFixture(root),
		fixtureEntry{name: root + "/files", typeFlag: tar.TypeDir, mode: 0o755},
		fixtureEntry{name: root + "/files/empty", typeFlag: tar.TypeDir, mode: 0o755},
		fixtureEntry{name: root + "/files/bin/tool", typeFlag: tar.TypeReg, mode: 0o755, data: []byte("tool")},
		fixtureEntry{name: root + "/files/bin/tool-link", typeFlag: tar.TypeSymlink, mode: 0o777, linkName: "tool"},
		fixtureEntry{name: root + "/files/bin/tool-hard", typeFlag: tar.TypeLink, mode: 0o755, linkName: root + "/files/bin/tool"},
	)
}

func buildArchive(t *testing.T, compression Compression, entries []fixtureEntry) []byte {
	t.Helper()
	var tarData bytes.Buffer
	tarWriter := tar.NewWriter(&tarData)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Typeflag: entry.typeFlag,
			Mode:     entry.mode,
			Linkname: entry.linkName,
		}
		if entry.typeFlag == tar.TypeReg || entry.typeFlag == tar.TypeRegA {
			header.Size = int64(len(entry.data))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write fixture header %q: %v", entry.name, err)
		}
		if len(entry.data) > 0 {
			if _, err := tarWriter.Write(entry.data); err != nil {
				t.Fatalf("write fixture data %q: %v", entry.name, err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}

	var compressed bytes.Buffer
	var writer io.WriteCloser
	var err error
	switch compression {
	case CompressionGzip:
		writer = gzip.NewWriter(&compressed)
	case CompressionXZ:
		writer, err = xz.NewWriter(&compressed)
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported fixture compression %q", compression)
	}
	if _, err := writer.Write(tarData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func removeEntry(entries []fixtureEntry, name string) []fixtureEntry {
	result := make([]fixtureEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.name != name {
			result = append(result, entry)
		}
	}
	return result
}

func removePrefix(entries []fixtureEntry, prefix string) []fixtureEntry {
	result := make([]fixtureEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.name != prefix && !bytes.HasPrefix([]byte(entry.name), []byte(prefix+"/")) {
			result = append(result, entry)
		}
	}
	return result
}

func replaceEntry(entries []fixtureEntry, replacement fixtureEntry) []fixtureEntry {
	result := append([]fixtureEntry(nil), entries...)
	for index, entry := range result {
		if entry.name == replacement.name {
			result[index] = replacement
			return result
		}
	}
	return append(result, replacement)
}

func withLimits(change func(*Limits)) Limits {
	limits := DefaultLimits()
	change(&limits)
	return limits
}

func assertValidationError(t *testing.T, err error, code ErrorCode, limit string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error", code)
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error %T is not a ValidationError: %v", err, err)
	}
	if validation.Code != code {
		t.Fatalf("error code = %q, want %q: %v", validation.Code, code, validation)
	}
	if limit != "" && validation.Limit != limit {
		t.Fatalf("limit = %q, want %q: %v", validation.Limit, limit, validation)
	}
}
