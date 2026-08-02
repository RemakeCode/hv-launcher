package cpuidmodule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hv-launcher/internal/model"
)

func TestVerificationStoreRoundTripsWithoutArtifactIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "cpuid-module-outcome.json")
	store := NewVerificationStore(path)
	wanted := model.ModuleVerificationOutcome{
		State:          model.ModuleVerificationFailed,
		Classification: model.ModuleVerificationKeyRejected,
		Detail:         "Required key not available",
		Remediation:    "Enroll the key",
	}
	if err := store.SaveIfChanged(wanted); err != nil {
		t.Fatal(err)
	}
	got, err := NewVerificationStore(path).Load()
	if err != nil || got != wanted {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"moduleHash", "kernelRelease", "bootIdentity"} {
		if _, present := document[forbidden]; present {
			t.Fatalf("outcome store contains forbidden identity field %q", forbidden)
		}
	}
}

func TestVerificationStoreTreatsMissingAndCorruptStateAsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outcome.json")
	store := NewVerificationStore(path)
	if outcome, err := store.Load(); err != nil || outcome.State != model.ModuleVerificationPending {
		t.Fatalf("missing state = %+v, %v", outcome, err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome, err := store.Load()
	if err == nil || outcome.State != model.ModuleVerificationPending {
		t.Fatalf("corrupt state = %+v, %v", outcome, err)
	}
}

func TestVerificationStoreDoesNotRewriteAnUnchangedOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outcome.json")
	store := NewVerificationStore(path)
	outcome := model.ModuleVerificationOutcome{State: model.ModuleVerificationVerified}
	if err := store.SaveIfChanged(outcome); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveIfChanged(outcome); err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !second.ModTime().Equal(info.ModTime()) {
		t.Fatal("unchanged outcome was rewritten")
	}
}
