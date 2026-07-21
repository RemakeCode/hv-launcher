package cpuidmodule

import (
	"errors"
)

const (
	MaxCompressedBytes   int64 = 16 << 20 // 16 MiB
	MaxExpandedBytes     int64 = 64 << 20 // 64 MiB
	MaxFileBytes         int64 = 16 << 20 // 16 MiB
	MaxDKMSConfigBytes   int64 = 64 << 10 // 64 KiB
	MaxEntries                 = 256
	MaxPathBytes               = 512
	MaxSelectedPathBytes       = 4_000
)

var (
	ErrInvalidArchive    = errors.New("invalid CPUID module archive")
	ErrResourceLimit     = errors.New("CPUID module archive exceeds a resource limit")
	ErrUnsafeEntry       = errors.New("CPUID module archive contains an unsafe entry")
	ErrInvalidDKMSConfig = errors.New("invalid static dkms.conf")
)

var requiredFiles = []string{
	"dkms.conf",
	"Makefile",
}

type Limits struct {
	MaxCompressedBytes int64
	MaxExpandedBytes   int64
	MaxFileBytes       int64
	MaxDKMSConfigBytes int64
	MaxEntries         int
	MaxPathBytes       int
}

func DefaultLimits() Limits {
	return Limits{
		MaxCompressedBytes: MaxCompressedBytes,
		MaxExpandedBytes:   MaxExpandedBytes,
		MaxFileBytes:       MaxFileBytes,
		MaxDKMSConfigBytes: MaxDKMSConfigBytes,
		MaxEntries:         MaxEntries,
		MaxPathBytes:       MaxPathBytes,
	}
}

type Identity struct {
	PackageName      string `json:"packageName"`
	PackageVersion   string `json:"packageVersion"`
	BuiltModuleName  string `json:"builtModuleName"`
	Destination      string `json:"destination"`
	AutomaticInstall bool   `json:"automaticInstall"`
}

type Inspection struct {
	FileName      string   `json:"fileName"`
	Identity      Identity `json:"identity"`
	EntryCount    int      `json:"entryCount"`
	ExpandedBytes int64    `json:"expandedBytes"`
	RequiredFiles []string `json:"requiredFiles"`
	Warning       string   `json:"warning"`
}

type PreflightCheck struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type Preflight struct {
	Ready            bool             `json:"ready"`
	KernelRelease    string           `json:"kernelRelease"`
	BuildRoot        string           `json:"buildRoot,omitempty"`
	Toolchain        string           `json:"toolchain,omitempty"`
	PackageManager   string           `json:"packageManager,omitempty"`
	DistributionID   string           `json:"distributionId,omitempty"`
	Lockdown         string           `json:"lockdown"`
	Signing          SigningEvidence  `json:"signing"`
	ControllerState  string           `json:"controllerState"`
	DKMSRegistered   bool             `json:"dkmsRegistered"`
	RegisteredSource string           `json:"registeredSource,omitempty"`
	Checks           []PreflightCheck `json:"checks"`
}

type SigningEvidence struct {
	ModuleSigningEnabled bool   `json:"moduleSigningEnabled"`
	SignatureForced      bool   `json:"signatureForced"`
	TrustedKeysSetting   string `json:"trustedKeysSetting,omitempty"`
}

type ArchivePreflight struct {
	FileName        string    `json:"fileName"`
	CompressedBytes int64     `json:"compressedBytes"`
	Warning         string    `json:"warning"`
	System          Preflight `json:"system"`
}
