package runtime

import (
	"context"
	"time"

	"hv-launcher/internal/model"
)

const (
	Repository       = "brcly/linuwux-runtime"
	RepositoryURL    = "https://github.com/brcly/linuwux-runtime"
	LatestReleaseURL = "https://api.github.com/repos/brcly/linuwux-runtime/releases/latest"
	LibraryAsset     = "liblinuwux.so"
	WrapperAsset     = "linuwux.sh"
	ChecksumsAsset   = "SHA256SUMS"
	WorkerCommand    = "linuwux-runtime-worker"
	SupportedGOARCH  = "amd64"

	maxReleaseBytes  int64 = 1 << 20
	maxLibraryBytes  int64 = 64 << 20
	maxWrapperBytes  int64 = 1 << 20
	maxChecksumBytes int64 = 1 << 20
	maxVersionBytes        = 4 << 10
)

type Progress struct {
	Phase      string `json:"phase"`
	Progress   int    `json:"progress"`
	Message    string `json:"message"`
	ReleaseTag string `json:"releaseTag,omitempty"`
	Asset      string `json:"asset,omitempty"`
}

type ProgressFunc func(Progress)

type Manager interface {
	Inspect(context.Context) (model.RuntimeStatus, error)
	Latest(context.Context) (model.RuntimeRelease, error)
	Install(context.Context, ProgressFunc) (model.RuntimeInstallResult, error)
	Remove(context.Context) error
}

type HTTPClient interface {
	Do(*httpRequest) (*httpResponse, error)
}

// The small HTTP aliases keep the public manager contract testable without
// exposing unrestricted URLs to callers.
type httpRequest struct {
	Context context.Context
	URL     string
	Header  map[string]string
	Maximum int64
}

type httpResponse struct {
	StatusCode int
	Body       []byte
}

type commandRunner interface {
	Version(context.Context, string, string) ([]byte, error)
}

type localManagerOptions struct {
	GOARCH  string
	Client  HTTPClient
	Runner  commandRunner
	Timeout time.Duration
}
