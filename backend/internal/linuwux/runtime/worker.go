package runtime

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

	"hv-launcher/internal/model"
)

const maxWorkerRequestBytes = 4 << 10

type workerOperation string

const (
	workerInspect workerOperation = "inspect"
	workerLatest  workerOperation = "latest"
	workerInstall workerOperation = "install"
	workerRemove  workerOperation = "remove"
)

type workerRequest struct {
	Operation workerOperation `json:"operation"`
	UserHome  string          `json:"userHome"`
}

type workerResponse struct {
	Status   *model.RuntimeStatus        `json:"status,omitempty"`
	Release  *model.RuntimeRelease       `json:"release,omitempty"`
	Progress *Progress                   `json:"progress,omitempty"`
	Result   *model.RuntimeInstallResult `json:"result,omitempty"`
	Error    string                      `json:"error,omitempty"`
}

type WorkerClient struct {
	Executable string
	UserHome   string
	UID        int
	GID        int
}

func NewWorkerClient(executable, userHome string, uid, gid int) (*WorkerClient, error) {
	if executable == "" || !filepath.IsAbs(executable) {
		return nil, errors.New("LinUwUx runtime worker executable must be an absolute path")
	}
	if userHome == "" || !filepath.IsAbs(userHome) {
		return nil, errors.New("Decky user home must be an absolute path")
	}
	if uid <= 0 || gid <= 0 {
		return nil, errors.New("LinUwUx runtime worker requires an unprivileged Decky user")
	}
	return &WorkerClient{Executable: executable, UserHome: userHome, UID: uid, GID: gid}, nil
}

func (c *WorkerClient) Inspect(ctx context.Context) (model.RuntimeStatus, error) {
	response, err := c.run(ctx, workerInspect, nil)
	if err != nil {
		return model.RuntimeStatus{}, err
	}
	if response.Status == nil {
		return model.RuntimeStatus{}, errors.New("LinUwUx runtime worker returned no runtime status")
	}
	return *response.Status, nil
}

func (c *WorkerClient) Latest(ctx context.Context) (model.RuntimeRelease, error) {
	response, err := c.run(ctx, workerLatest, nil)
	if err != nil {
		return model.RuntimeRelease{}, err
	}
	if response.Release == nil {
		return model.RuntimeRelease{}, errors.New("LinUwUx runtime worker returned no release")
	}
	return *response.Release, nil
}

func (c *WorkerClient) Install(ctx context.Context, progress ProgressFunc) (model.RuntimeInstallResult, error) {
	response, err := c.run(ctx, workerInstall, progress)
	if err != nil {
		return model.RuntimeInstallResult{}, err
	}
	if response.Result == nil {
		return model.RuntimeInstallResult{}, errors.New("LinUwUx runtime worker returned no installation result")
	}
	return *response.Result, nil
}

func (c *WorkerClient) Remove(ctx context.Context) error {
	_, err := c.run(ctx, workerRemove, nil)
	return err
}

func (c *WorkerClient) run(ctx context.Context, operation workerOperation, progress ProgressFunc) (workerResponse, error) {
	payload, err := json.Marshal(workerRequest{Operation: operation, UserHome: c.UserHome})
	if err != nil {
		return workerResponse{}, err
	}
	command := c.command(ctx)
	command.Stdin = bytes.NewReader(payload)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return workerResponse{}, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return workerResponse{}, fmt.Errorf("start LinUwUx runtime worker: %w", err)
	}

	decoder := json.NewDecoder(stdout)
	decoder.DisallowUnknownFields()
	var final workerResponse
	for {
		var response workerResponse
		if err := decoder.Decode(&response); err != nil {
			if !errors.Is(err, io.EOF) {
				stopWorker(command, stdout)
				return workerResponse{}, fmt.Errorf("decode LinUwUx runtime worker response: %w", err)
			}
			break
		}
		if response.Progress != nil {
			if progress != nil {
				progress(*response.Progress)
			}
			continue
		}
		final = response
	}
	if err := command.Wait(); err != nil {
		if stderr.Len() > 0 {
			return workerResponse{}, fmt.Errorf("LinUwUx runtime worker failed: %s", sanitized(stderr.String()))
		}
		return workerResponse{}, fmt.Errorf("LinUwUx runtime worker failed: %w", err)
	}
	if final.Error != "" {
		return workerResponse{}, errors.New(final.Error)
	}
	return final, nil
}

func (c *WorkerClient) command(ctx context.Context) *exec.Cmd {
	command := exec.CommandContext(ctx, c.Executable, WorkerCommand)
	command.Env = []string{"HOME=" + c.UserHome, "PATH=/usr/local/bin:/usr/bin:/bin"}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: uint32(c.UID), Gid: uint32(c.GID), Groups: []uint32{uint32(c.GID)},
	}}
	return command
}

func RunWorker(ctx context.Context, input io.Reader, output io.Writer) error {
	payload, err := io.ReadAll(io.LimitReader(input, maxWorkerRequestBytes+1))
	if err != nil {
		return writeWorkerResponse(output, workerResponse{Error: sanitized(err.Error())})
	}
	if len(payload) > maxWorkerRequestBytes {
		return writeWorkerResponse(output, workerResponse{Error: "LinUwUx runtime worker request is too large"})
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request workerRequest
	if err := decoder.Decode(&request); err != nil {
		return writeWorkerResponse(output, workerResponse{Error: "decode LinUwUx runtime worker request: " + sanitized(err.Error())})
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return writeWorkerResponse(output, workerResponse{Error: "decode LinUwUx runtime worker request: trailing data"})
	}
	if request.UserHome == "" || !filepath.IsAbs(request.UserHome) {
		return writeWorkerResponse(output, workerResponse{Error: "LinUwUx runtime worker requires an absolute user home"})
	}
	manager, err := NewLocalManager(request.UserHome)
	if err != nil {
		return writeWorkerResponse(output, workerResponse{Error: sanitized(err.Error())})
	}

	switch request.Operation {
	case workerInspect:
		status, err := manager.Inspect(ctx)
		if err != nil {
			return writeWorkerResponse(output, workerResponse{Error: sanitized(err.Error())})
		}
		return writeWorkerResponse(output, workerResponse{Status: &status})
	case workerLatest:
		release, err := manager.Latest(ctx)
		if err != nil {
			return writeWorkerResponse(output, workerResponse{Error: sanitized(err.Error())})
		}
		return writeWorkerResponse(output, workerResponse{Release: &release})
	case workerInstall:
		result, err := manager.Install(ctx, func(update Progress) {
			_ = writeWorkerResponse(output, workerResponse{Progress: &update})
		})
		if err != nil {
			return writeWorkerResponse(output, workerResponse{Error: sanitized(err.Error())})
		}
		return writeWorkerResponse(output, workerResponse{Result: &result})
	case workerRemove:
		if err := manager.Remove(ctx); err != nil {
			return writeWorkerResponse(output, workerResponse{Error: sanitized(err.Error())})
		}
		return writeWorkerResponse(output, workerResponse{})
	default:
		return writeWorkerResponse(output, workerResponse{Error: "unknown LinUwUx runtime worker operation"})
	}
}

func writeWorkerResponse(output io.Writer, response workerResponse) error {
	return json.NewEncoder(output).Encode(response)
}

func stopWorker(command *exec.Cmd, stdout io.Closer) {
	_ = stdout.Close()
	if command.Process != nil {
		_ = command.Process.Kill()
	}
	_ = command.Wait()
}
