package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hv-launcher/internal/auth"
	"hv-launcher/internal/jobs"
	"hv-launcher/internal/umip"
)

type umipEndpointRunner struct {
	mu     sync.Mutex
	calls  []string
	errors []error
}

func (r *umipEndpointRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	index := len(r.calls) - 1
	if index < len(r.errors) {
		return []byte("updater output"), r.errors[index]
	}
	return []byte("updater output"), nil
}

func TestUMIPInspectionReturnsReadOnlyRestartGuidance(t *testing.T) {
	service, root, _, _ := newTestService(t)
	paths := service.options.UMIP.Paths
	configuration := "KERNEL_CMDLINE[default]+=\"quiet clearcpuid=umip\"\n"
	writeServerFile(t, paths.LimineConfiguration, configuration)
	writeServerFile(t, paths.LimineUpdaters[0], "updater")
	if err := os.Chmod(paths.LimineUpdaters[0], 0o755); err != nil {
		t.Fatal(err)
	}
	writeServerFile(t, root+"/cpuinfo", "processor : 0\nvendor_id : AuthenticAMD\ncpu family : 23\nmodel : 49\nmodel name : AMD Ryzen\nflags : umip\n\n")

	response := perform(service.Handler(), http.MethodGet, "/v1/setup/umip", "")
	if response.Code != http.StatusOK {
		t.Fatalf("inspection returned %d: %s", response.Code, response.Body.String())
	}
	var result umip.Inspection
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Selection != umip.SelectionAutomatic || result.Selected != umip.BootloaderLimine ||
		len(result.Candidates) != 1 || result.Candidates[0].State != umip.StateRestartRequired {
		t.Fatalf("unexpected inspection: %+v", result)
	}
	unchanged, err := os.ReadFile(paths.LimineConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != configuration {
		t.Fatalf("inspection changed configuration: %q", unchanged)
	}
}

func TestUMIPInspectionEndpointReportsChoiceAndManualOnlySystems(t *testing.T) {
	t.Run("both supported candidates require a choice", func(t *testing.T) {
		service, _, _, _ := newTestService(t)
		paths := service.options.UMIP.Paths
		writeServerFile(t, paths.LimineConfiguration, "KERNEL_CMDLINE[default]+=quiet\n")
		writeServerFile(t, paths.LimineUpdaters[0], "updater")
		writeServerFile(t, paths.GRUBConfiguration, "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n")
		writeServerFile(t, paths.UpdateGRUB[0], "updater")
		for _, updater := range []string{paths.LimineUpdaters[0], paths.UpdateGRUB[0]} {
			if err := os.Chmod(updater, 0o755); err != nil {
				t.Fatal(err)
			}
		}

		result := inspectUMIPEndpoint(t, service)
		if result.Selection != umip.SelectionChoice || result.Selected != "" || len(result.Candidates) != 2 {
			t.Fatalf("unexpected dual-candidate inspection: %+v", result)
		}
	})

	t.Run("no supported configuration is manual only", func(t *testing.T) {
		service, _, _, _ := newTestService(t)
		result := inspectUMIPEndpoint(t, service)
		if result.Selection != umip.SelectionManualOnly || len(result.Candidates) != 0 || len(result.Manual) != 1 ||
			result.Manual[0].Reason != umip.ReasonUnsupportedLoader {
			t.Fatalf("unexpected no-candidate inspection: %+v", result)
		}
	})

}

func inspectUMIPEndpoint(t *testing.T, service *Service) umip.Inspection {
	t.Helper()
	response := perform(service.Handler(), http.MethodGet, "/v1/setup/umip", "")
	if response.Code != http.StatusOK {
		t.Fatalf("inspection returned %d: %s", response.Code, response.Body.String())
	}
	var result umip.Inspection
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestUMIPApplyRequiresAndConsumesExactCapability(t *testing.T) {
	service, _, _, _ := newTestService(t)
	paths := service.options.UMIP.Paths
	configuration := "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n"
	writeServerFile(t, paths.GRUBConfiguration, configuration)
	writeServerFile(t, paths.UpdateGRUB[0], "updater")
	if err := os.Chmod(paths.UpdateGRUB[0], 0o755); err != nil {
		t.Fatal(err)
	}
	withoutCapability := `{"bootloader":"grub","capability":""}`
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/umip", withoutCapability)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unsigned apply returned %d: %s", response.Code, response.Body.String())
	}
	unchanged, _ := os.ReadFile(paths.GRUBConfiguration)
	if string(unchanged) != configuration {
		t.Fatalf("unsigned apply changed configuration: %q", unchanged)
	}

	capability := signServerCapability(t, auth.OperationUMIPApply, umip.UMIPApplyBinding(umip.BootloaderGRUB))
	request := `{"bootloader":"grub","capability":"` + capability + `"}`
	response = perform(service.Handler(), http.MethodPost, "/v1/setup/umip", request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("authorized apply returned %d: %s", response.Code, response.Body.String())
	}
	var started jobs.JobSnapshot
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	terminal := waitForServerJob(t, service, started.ID)
	if terminal.State != jobs.JobSucceeded {
		t.Fatalf("apply job failed: %+v", terminal)
	}
	updated, _ := os.ReadFile(paths.GRUBConfiguration)
	want := "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet " + umip.FixedArgument + "\"\n"
	if string(updated) != want {
		t.Fatalf("authorized apply wrote %q, want %q", updated, want)
	}

	replay := perform(service.Handler(), http.MethodPost, "/v1/setup/umip", request)
	if replay.Code != http.StatusForbidden {
		t.Fatalf("replayed capability returned %d: %s", replay.Code, replay.Body.String())
	}
}

func TestUMIPApplyEndpointSupportsLimine(t *testing.T) {
	service, _, _, _ := newTestService(t)
	paths := service.options.UMIP.Paths
	configuration := "ESP_PATH=\"/boot\"\nKERNEL_CMDLINE[default]+=quiet\n"
	writeServerFile(t, paths.LimineConfiguration, configuration)
	writeServerFile(t, paths.LimineUpdaters[0], "updater")
	if err := os.Chmod(paths.LimineUpdaters[0], 0o755); err != nil {
		t.Fatal(err)
	}
	capability := signServerCapability(t, auth.OperationUMIPApply, umip.UMIPApplyBinding(umip.BootloaderLimine))
	request := `{"bootloader":"limine","capability":"` + capability + `"}`
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/umip", request)
	terminal := acceptedUMIPJob(t, service, response)
	if terminal.State != jobs.JobSucceeded {
		t.Fatalf("Limine apply failed: %+v", terminal)
	}
	updated, err := os.ReadFile(paths.LimineConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	want := configuration + "KERNEL_CMDLINE[default]+=" + umip.FixedArgument + "\n"
	if string(updated) != want {
		t.Fatalf("Limine apply wrote %q, want %q", updated, want)
	}
}

func TestUMIPApplyEndpointRejectsCallerTargets(t *testing.T) {
	service, _, _, _ := newTestService(t)
	paths := service.options.UMIP.Paths
	configuration := "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet\"\n"
	writeServerFile(t, paths.GRUBConfiguration, configuration)
	writeServerFile(t, paths.UpdateGRUB[0], "updater")
	if err := os.Chmod(paths.UpdateGRUB[0], 0o755); err != nil {
		t.Fatal(err)
	}
	capability := signServerCapability(t, auth.OperationUMIPApply, umip.UMIPApplyBinding(umip.BootloaderGRUB))
	withTarget := `{"bootloader":"grub","capability":"` + capability + `","path":"/tmp/evil"}`
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/umip", withTarget)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("apply accepted caller target: %d %s", response.Code, response.Body.String())
	}
}

func TestUMIPApplyEndpointReportsRollbackAndManualRecovery(t *testing.T) {
	tests := []struct {
		name           string
		errors         []error
		wantError      string
		wantBackup     bool
		wantUpdaterRun int
	}{
		{name: "complete rollback", errors: []error{errors.New("update failed"), nil}, wantError: "rolled back successfully", wantUpdaterRun: 2},
		{name: "failed recovery generation", errors: []error{errors.New("update failed"), errors.New("recovery failed")}, wantError: "recovery updater failed", wantBackup: true, wantUpdaterRun: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _, _, _ := newTestService(t)
			paths := service.options.UMIP.Paths
			configuration := "KERNEL_CMDLINE[default]+=quiet\n"
			writeServerFile(t, paths.LimineConfiguration, configuration)
			writeServerFile(t, paths.LimineUpdaters[0], "updater")
			if err := os.Chmod(paths.LimineUpdaters[0], 0o755); err != nil {
				t.Fatal(err)
			}
			runner := &umipEndpointRunner{errors: test.errors}
			service.options.UMIP.Runner = runner
			capability := signServerCapability(t, auth.OperationUMIPApply, umip.UMIPApplyBinding(umip.BootloaderLimine))
			request := `{"bootloader":"limine","capability":"` + capability + `"}`
			terminal := acceptedUMIPJob(t, service, perform(service.Handler(), http.MethodPost, "/v1/setup/umip", request))
			if terminal.State != jobs.JobFailed || !strings.Contains(terminal.Error, test.wantError) {
				t.Fatalf("unexpected terminal job: %+v", terminal)
			}
			restored, err := os.ReadFile(paths.LimineConfiguration)
			if err != nil || string(restored) != configuration {
				t.Fatalf("source was not restored: %q err=%v", restored, err)
			}
			if len(runner.calls) != test.wantUpdaterRun {
				t.Fatalf("updater calls = %v", runner.calls)
			}
			entries, err := os.ReadDir(paths.RecoveryDirectory)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantBackup != (len(entries) == 1 && filepath.Ext(entries[0].Name()) == ".bak") {
				t.Fatalf("unexpected recovery backups: %v", entries)
			}
		})
	}
}

func acceptedUMIPJob(t *testing.T, service *Service, response interface {
	Result() *http.Response
}) jobs.JobSnapshot {
	t.Helper()
	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusAccepted {
		var body []byte
		body, _ = io.ReadAll(result.Body)
		t.Fatalf("apply returned %d: %s", result.StatusCode, body)
	}
	var started jobs.JobSnapshot
	if err := json.NewDecoder(result.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	return waitForServerJob(t, service, started.ID)
}

func signServerCapability(t *testing.T, operation auth.Operation, binding string) string {
	t.Helper()
	now := time.Now().Unix()
	payload, err := json.Marshal(map[string]any{
		"version": 1, "operation": operation, "binding": binding,
		"nonce":    base64.RawURLEncoding.EncodeToString(bytesOf(0x37, 24)),
		"issuedAt": now, "expiresAt": now + int64(auth.MaxCapabilityAge/time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, testSetupSecret())
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func TestUMIPInspectionIncludesCurrentAndProposedConfiguration(t *testing.T) {
	service, _, _, _ := newTestService(t)
	paths := service.options.UMIP.Paths
	configuration := "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet splash\"\n"
	writeServerFile(t, paths.GRUBConfiguration, configuration)
	writeServerFile(t, paths.UpdateGRUB[0], "updater")
	if err := os.Chmod(paths.UpdateGRUB[0], 0o755); err != nil {
		t.Fatal(err)
	}

	response := perform(service.Handler(), http.MethodGet, "/v1/setup/umip", "")
	if response.Code != http.StatusOK {
		t.Fatalf("inspection returned %d: %s", response.Code, response.Body.String())
	}
	var inspection umip.Inspection
	if err := json.Unmarshal(response.Body.Bytes(), &inspection); err != nil {
		t.Fatal(err)
	}
	if len(inspection.Candidates) != 1 || inspection.Candidates[0].Bootloader != umip.BootloaderGRUB ||
		inspection.Candidates[0].CurrentValue != "quiet splash" ||
		inspection.Candidates[0].ProposedValue != "quiet splash "+umip.FixedArgument {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}

	capability := signServerCapability(t, auth.OperationUMIPApply, umip.UMIPApplyBinding(umip.BootloaderGRUB))
	unknownField := perform(service.Handler(), http.MethodPost, "/v1/setup/umip", `{"bootloader":"grub","capability":"`+capability+`","path":"/tmp/evil"}`)
	if unknownField.Code != http.StatusBadRequest {
		t.Fatalf("apply accepted caller path: %d %s", unknownField.Code, unknownField.Body.String())
	}
	unsupported := perform(service.Handler(), http.MethodPost, "/v1/setup/umip", `{"bootloader":"systemd-boot","capability":"invalid"}`)
	if unsupported.Code != http.StatusBadRequest {
		t.Fatalf("apply accepted unsupported bootloader: %d %s", unsupported.Code, unsupported.Body.String())
	}
}
