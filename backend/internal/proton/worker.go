package proton

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	WorkerCommand         = "proton-worker"
	maxWorkerRequestBytes = 16 << 10
)

// Operator is the user-scoped Proton work used by the HTTP service.
// Production delegates it to WorkerClient; tests may use Installer directly.
type Operator interface {
	PreflightPath(string) (Preflight, error)
	Install(context.Context, string, string, ProgressFunc) (InstallResult, error)
}

type workerOperation string

const (
	workerPreflight workerOperation = "preflight"
	workerInstall   workerOperation = "install"
)

type workerRequest struct {
	Operation     workerOperation `json:"operation"`
	UserHome      string          `json:"userHome"`
	ArchivePath   string          `json:"archivePath"`
	DestinationID string          `json:"destinationId,omitempty"`
}

type workerProgress struct {
	Phase    string `json:"phase"`
	Progress int    `json:"progress"`
	Message  string `json:"message"`
}

type workerResponse struct {
	Preflight *Preflight      `json:"preflight,omitempty"`
	Progress  *workerProgress `json:"progress,omitempty"`
	Result    *InstallResult  `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// WorkerClient invokes the fixed worker mode of the current executable with
// Decky-user credentials. It never forwards the root service environment.
type WorkerClient struct {
	Executable string
	UserHome   string
	UID        int
	GID        int
}

func NewWorkerClient(executable, userHome string, uid, gid int) (*WorkerClient, error) {
	if executable == "" || !filepath.IsAbs(executable) {
		return nil, errors.New("Proton worker executable must be an absolute path")
	}
	if userHome == "" || !filepath.IsAbs(userHome) {
		return nil, errors.New("Decky user home must be an absolute path")
	}
	if uid <= 0 || gid <= 0 {
		return nil, errors.New("Proton worker requires an unprivileged Decky user")
	}
	return &WorkerClient{Executable: executable, UserHome: userHome, UID: uid, GID: gid}, nil
}

func (c *WorkerClient) PreflightPath(archivePath string) (Preflight, error) {
	response, err := c.run(context.Background(), workerRequest{
		Operation: workerPreflight, UserHome: c.UserHome, ArchivePath: archivePath,
	}, nil)
	if err != nil {
		return Preflight{}, err
	}
	if response.Preflight == nil {
		return Preflight{}, errors.New("Proton worker returned no preflight result")
	}
	return *response.Preflight, nil
}

func (c *WorkerClient) Install(ctx context.Context, archivePath, destinationID string, progress ProgressFunc) (InstallResult, error) {
	response, err := c.run(ctx, workerRequest{
		Operation: workerInstall, UserHome: c.UserHome, ArchivePath: archivePath,
		DestinationID: destinationID,
	}, progress)
	if err != nil {
		return InstallResult{}, err
	}
	if response.Result == nil {
		return InstallResult{}, errors.New("Proton worker returned no installation result")
	}
	return *response.Result, nil
}

func (c *WorkerClient) run(ctx context.Context, request workerRequest, progress ProgressFunc) (workerResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return workerResponse{}, fmt.Errorf("encode Proton worker request: %w", err)
	}
	command := c.command(ctx)
	command.Stdin = bytes.NewReader(payload)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return workerResponse{}, fmt.Errorf("open Proton worker output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return workerResponse{}, fmt.Errorf("start Proton worker: %w", err)
	}
	decoder := json.NewDecoder(stdout)
	decoder.DisallowUnknownFields()
	var final workerResponse
	for {
		var response workerResponse
		if err := decoder.Decode(&response); err != nil {
			if !errors.Is(err, io.EOF) {
				_ = command.Wait()
				return workerResponse{}, fmt.Errorf("decode Proton worker response: %w", err)
			}
			break
		}
		if response.Progress != nil {
			if progress != nil {
				progress(response.Progress.Phase, response.Progress.Progress, response.Progress.Message)
			}
			continue
		}
		final = response
	}
	if err := command.Wait(); err != nil {
		if stderr.Len() > 0 {
			return workerResponse{}, fmt.Errorf("Proton worker failed: %s", bytes.TrimSpace(stderr.Bytes()))
		}
		return workerResponse{}, fmt.Errorf("Proton worker failed: %w", err)
	}
	if final.Error != "" {
		return workerResponse{}, errors.New(final.Error)
	}
	return final, nil
}

func (c *WorkerClient) command(ctx context.Context) *exec.Cmd {
	command := exec.CommandContext(ctx, c.Executable, WorkerCommand)
	command.Env = []string{"HOME=" + c.UserHome}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: uint32(c.UID), Gid: uint32(c.GID), Groups: []uint32{uint32(c.GID)},
	}}
	return command
}

// RunWorker handles one bounded request. It is invoked only by WorkerCommand
// and performs no operation other than the two typed Proton actions.
func RunWorker(ctx context.Context, input io.Reader, output io.Writer) error {
	payload, err := io.ReadAll(io.LimitReader(input, maxWorkerRequestBytes+1))
	if err != nil {
		return writeWorkerResponse(output, workerResponse{Error: "read Proton worker request: " + err.Error()})
	}
	if len(payload) > maxWorkerRequestBytes {
		return writeWorkerResponse(output, workerResponse{Error: "Proton worker request is too large"})
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request workerRequest
	if err := decoder.Decode(&request); err != nil {
		return writeWorkerResponse(output, workerResponse{Error: "decode Proton worker request: " + err.Error()})
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return writeWorkerResponse(output, workerResponse{Error: "decode Proton worker request: trailing data"})
	}
	if request.UserHome == "" || !filepath.IsAbs(request.UserHome) ||
		request.ArchivePath == "" || !filepath.IsAbs(request.ArchivePath) {
		return writeWorkerResponse(output, workerResponse{Error: "Proton worker requires absolute user-home and archive paths"})
	}
	installer := NewInstaller(request.UserHome)
	switch request.Operation {
	case workerPreflight:
		if request.DestinationID != "" {
			return writeWorkerResponse(output, workerResponse{Error: "preflight request contains installation fields"})
		}
		preflight, err := installer.PreflightPath(request.ArchivePath)
		if err != nil {
			return writeWorkerResponse(output, workerResponse{Error: err.Error()})
		}
		return writeWorkerResponse(output, workerResponse{Preflight: &preflight})
	case workerInstall:
		if request.DestinationID == "" {
			return writeWorkerResponse(output, workerResponse{Error: "installation request is incomplete"})
		}
		progress := func(phase string, percent int, message string) {
			_ = writeWorkerResponse(output, workerResponse{Progress: &workerProgress{
				Phase: phase, Progress: percent, Message: message,
			}})
		}
		result, err := installer.Install(ctx, request.ArchivePath, request.DestinationID, progress)
		if err != nil {
			return writeWorkerResponse(output, workerResponse{Error: err.Error()})
		}
		return writeWorkerResponse(output, workerResponse{Result: &result})
	default:
		return writeWorkerResponse(output, workerResponse{Error: "unknown Proton worker operation"})
	}
}

func writeWorkerResponse(output io.Writer, response workerResponse) error {
	return json.NewEncoder(output).Encode(response)
}
