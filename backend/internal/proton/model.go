package proton

import "fmt"

const (
	DefaultMaxCompressedBytes int64 = 4 << 30 // 4 GiB
	DefaultMaxExpandedBytes   int64 = 8 << 30 // 8 GiB
	DefaultMaxEntries               = 50_000  // 50,000 entries
	DefaultMaxManifestBytes   int64 = 1 << 20 // 1 MiB
	DefaultMaxPathBytes             = 4_096   // 4 KiB
)

// Limits bounds every resource consumed while preflighting or installing an
// archive. Smaller values can be supplied by tests without changing defaults.
type Limits struct {
	MaxCompressedBytes int64
	MaxExpandedBytes   int64
	MaxEntries         int
	MaxManifestBytes   int64
	MaxPathBytes       int
}

func DefaultLimits() Limits {
	return Limits{
		MaxCompressedBytes: DefaultMaxCompressedBytes,
		MaxExpandedBytes:   DefaultMaxExpandedBytes,
		MaxEntries:         DefaultMaxEntries,
		MaxManifestBytes:   DefaultMaxManifestBytes,
		MaxPathBytes:       DefaultMaxPathBytes,
	}
}

type Compression string

const (
	CompressionGzip Compression = "gzip"
	CompressionXZ   Compression = "xz"
)

type Check struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Inspection is the structured, read-only result of validating one archive.
type Inspection struct {
	Compression      Compression `json:"compression"`
	SHA256           string      `json:"sha256"`
	ToolRoot         string      `json:"toolRoot"`
	ManifestTool     string      `json:"manifestTool"`
	Payload          string      `json:"payload"`
	EntryCount       int         `json:"entryCount"`
	ExpandedBytes    int64       `json:"expandedBytes"`
	RegularBytes     int64       `json:"regularBytes"`
	EmptyDirectories int         `json:"emptyDirectories"`
	SymbolicLinks    int         `json:"symbolicLinks"`
	HardLinks        int         `json:"hardLinks"`
	Checks           []Check     `json:"checks"`
}

type ErrorCode string

const (
	ErrorInvalidLimits     ErrorCode = "invalid-limits"
	ErrorArchiveIO         ErrorCode = "archive-io"
	ErrorUnsupportedFormat ErrorCode = "unsupported-format"
	ErrorMalformedArchive  ErrorCode = "malformed-archive"
	ErrorResourceLimit     ErrorCode = "resource-limit"
	ErrorUnsafePath        ErrorCode = "unsafe-path"
	ErrorPathConflict      ErrorCode = "path-conflict"
	ErrorUnsupportedEntry  ErrorCode = "unsupported-entry"
	ErrorUnsafeLink        ErrorCode = "unsafe-link"
	ErrorInvalidLayout     ErrorCode = "invalid-layout"
	ErrorInvalidManifest   ErrorCode = "invalid-manifest"
)

// ValidationError gives callers a stable category plus the archive path or
// named limit responsible for a validation failure.
type ValidationError struct {
	Code   ErrorCode
	Path   string
	Limit  string
	Detail string
	Err    error
}

func (e *ValidationError) Error() string {
	message := string(e.Code)
	if e.Path != "" {
		message += fmt.Sprintf(" at %q", e.Path)
	}
	if e.Limit != "" {
		message += fmt.Sprintf(" (%s)", e.Limit)
	}
	if e.Detail != "" {
		message += ": " + e.Detail
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *ValidationError) Unwrap() error { return e.Err }
