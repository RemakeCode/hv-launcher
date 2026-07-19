package proton

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWorkerClientUsesDeckyCredentialsAndMinimalEnvironment(t *testing.T) {
	client, err := NewWorkerClient("/opt/hv-launcher", "/home/deck", 1000, 1000)
	if err != nil {
		t.Fatal(err)
	}
	command := client.command(context.Background())
	if len(command.Args) != 2 || command.Args[1] != WorkerCommand {
		t.Fatalf("unexpected worker command: %v", command.Args)
	}
	credential := command.SysProcAttr.Credential
	if credential == nil || credential.Uid != 1000 || credential.Gid != 1000 ||
		len(credential.Groups) != 1 || credential.Groups[0] != 1000 {
		t.Fatalf("unexpected worker credentials: %+v", credential)
	}
	if len(command.Env) != 1 || command.Env[0] != "HOME=/home/deck" {
		t.Fatalf("worker inherited an unexpected environment: %v", command.Env)
	}
}

func TestWorkerRejectsPrivilegedDeckyIdentity(t *testing.T) {
	if _, err := NewWorkerClient("/opt/hv-launcher", "/root", 0, 0); err == nil {
		t.Fatal("root Proton worker identity was accepted")
	}
}

func TestWorkerPreflightsAndInstallsAsCurrentUser(t *testing.T) {
	home, steamRoot := installerSteamHome(t)
	archivePath := writeInstallerArchive(t, CompressionXZ, validFixture(fixtureRoot))

	preflight := runWorkerRequest(t, workerRequest{
		Operation: workerPreflight, UserHome: home, ArchivePath: archivePath,
	})
	if preflight.Error != "" || preflight.Preflight == nil || preflight.Preflight.CompressedBytes <= 0 {
		t.Fatalf("unexpected preflight response: %+v", preflight)
	}

	installation := runWorkerRequest(t, workerRequest{
		Operation: workerInstall, UserHome: home, ArchivePath: archivePath,
		DestinationID: "native",
	})
	if installation.Error != "" || installation.Result == nil || installation.Result.ToolName != fixtureRoot {
		t.Fatalf("unexpected installation response: %+v", installation)
	}
	finalRoot := filepath.Join(steamRoot, "compatibilitytools.d", fixtureRoot)
	info, err := os.Stat(finalRoot)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() {
		t.Fatalf("installed root owner = %d:%d, want %d:%d", stat.Uid, stat.Gid, os.Geteuid(), os.Getegid())
	}
}

func runWorkerRequest(t *testing.T, request workerRequest) workerResponse {
	t.Helper()
	var input bytes.Buffer
	if err := json.NewEncoder(&input).Encode(request); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunWorker(context.Background(), &input, &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var response workerResponse
	for {
		if err := decoder.Decode(&response); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
	}
	return response
}
