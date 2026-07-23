package hypervisor

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"hv-launcher/internal/model"
)

type fakeHost struct {
	mu       sync.Mutex
	loaded   map[string]bool
	refCount map[string]int
	calls    []string
	failOnce string
	failed   bool
	lookErr  error
}

func TestNewSessionIDReturnsRandomSourceFailure(t *testing.T) {
	if _, err := newSessionID(strings.NewReader("short")); err == nil {
		t.Fatal("newSessionID accepted insufficient randomness")
	}
}

type memoryJournal struct {
	record *JournalRecord
	writes int
	clears int
	err    error
}

func newFakeHost() *fakeHost {
	return &fakeHost{loaded: map[string]bool{"kvm": true, "kvm_amd": true}, refCount: map[string]int{}}
}

func (h *fakeHost) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	call := name + " " + strings.Join(args, " ")
	h.calls = append(h.calls, call)
	if name == "modinfo" {
		return []byte("6.18.0 SMP preempt\n"), nil
	}
	if h.failOnce == strings.Join(args, " ") && !h.failed {
		h.failed = true
		return []byte("injected failure"), errors.New("injected failure")
	}
	if name == "modprobe" {
		if len(args) == 2 && args[0] == "-r" {
			h.loaded[args[1]] = false
		} else if len(args) == 1 {
			h.loaded[args[0]] = true
			if args[0] == "kvm_amd" {
				h.loaded["kvm"] = true
			}
		}
	}
	return nil, nil
}

func (h *fakeHost) LookPath(string) (string, error) {
	if h.lookErr != nil {
		return "", h.lookErr
	}
	return "/sbin/modprobe", nil
}

func (h *fakeHost) Loaded(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.loaded[name]
}

func (h *fakeHost) RefCount(name string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.refCount[name]
}

func (h *fakeHost) snapshot() ModuleSnapshot {
	return ModuleSnapshot{Emulation: h.Loaded("cpuid_fault_emulation"), KVM: h.Loaded("kvm"), KVMAMD: h.Loaded("kvm_amd")}
}

func (j *memoryJournal) Load() (*JournalRecord, error) { return j.record, j.err }
func (j *memoryJournal) Write(record JournalRecord) error {
	if j.err != nil {
		return j.err
	}
	copy := record
	j.record = &copy
	j.writes++
	return nil
}
func (j *memoryJournal) Clear() error {
	if j.err != nil {
		return j.err
	}
	j.record = nil
	j.clears++
	return nil
}

func testController(t *testing.T, host *fakeHost, journal Journal) *Controller {
	t.Helper()
	controller, err := New(Options{Runner: host, Modules: host, Journal: journal, KernelRelease: "6.18.0", EffectiveUID: func() int { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

func TestActivationAndFinalSessionRestorationOrdering(t *testing.T) {
	host := newFakeHost()
	journal := &memoryJournal{}
	controller := testController(t, host, journal)
	first, err := controller.StartSession(context.Background(), "10", "wrapper")
	if err != nil {
		t.Fatal(err)
	}
	second, err := controller.StartSession(context.Background(), "20", "wrapper")
	if err != nil {
		t.Fatal(err)
	}
	if !host.Loaded("cpuid_fault_emulation") || host.Loaded("kvm") || host.Loaded("kvm_amd") {
		t.Fatalf("activation state is wrong: %+v", host.snapshot())
	}
	if err := controller.EndSession(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if !host.Loaded("cpuid_fault_emulation") {
		t.Fatal("emulation was removed while another session remained")
	}
	if err := controller.EndSession(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	if got := host.snapshot(); got != (ModuleSnapshot{KVM: true, KVMAMD: true}) {
		t.Fatalf("original modules were not restored: %+v", got)
	}
	expected := []string{
		"modinfo -F vermagic cpuid_fault_emulation", "modprobe -r kvm_amd", "modprobe -r kvm",
		"modprobe cpuid_fault_emulation", "modprobe -r cpuid_fault_emulation", "modprobe kvm_amd",
	}
	if !reflect.DeepEqual(host.calls, expected) {
		t.Fatalf("\ngot:  %v\nwant: %v", host.calls, expected)
	}
	if journal.record != nil || controller.State() != StateIdle {
		t.Fatalf("transition did not finish cleanly: %+v, %s", journal.record, controller.State())
	}
}

func TestSessionActivationFailureIsLoggedWithCause(t *testing.T) {
	host := newFakeHost()
	var output bytes.Buffer
	controller, err := New(Options{
		Runner: host, Modules: host, Journal: &memoryJournal{}, KernelRelease: "6.18.0",
		EffectiveUID: func() int { return 1000 },
		Logger:       slog.New(slog.NewTextHandler(&output, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.StartSession(context.Background(), "10", "wrapper"); err == nil {
		t.Fatal("expected activation failure")
	}
	logs := output.String()
	if !strings.Contains(logs, "session activation failed") || !strings.Contains(logs, "hypervisor transition requires root") {
		t.Fatalf("activation cause was not logged: %s", logs)
	}
}

func TestActivationRollsBackEveryFailedOperation(t *testing.T) {
	for _, operation := range []string{"-r kvm_amd", "-r kvm", "cpuid_fault_emulation"} {
		t.Run(operation, func(t *testing.T) {
			host := newFakeHost()
			host.failOnce = operation
			journal := &memoryJournal{}
			controller := testController(t, host, journal)
			if _, err := controller.StartSession(context.Background(), "10", "wrapper"); err == nil {
				t.Fatal("expected activation failure")
			}
			if got := host.snapshot(); got != (ModuleSnapshot{KVM: true, KVMAMD: true}) {
				t.Fatalf("rollback did not restore original state: %+v", got)
			}
			if controller.State() != StateIdle || journal.record != nil {
				t.Fatalf("rollback state is %s, journal=%+v", controller.State(), journal.record)
			}
		})
	}
}

func TestRollbackFailureRequiresRecovery(t *testing.T) {
	host := newFakeHost()
	host.failOnce = "kvm_amd"
	controller := testController(t, host, &memoryJournal{})
	session, err := controller.StartSession(context.Background(), "10", "wrapper")
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.EndSession(context.Background(), session.ID); err == nil {
		t.Fatal("expected restore failure")
	}
	if controller.State() != StateRecoveryRequired {
		t.Fatalf("got %s", controller.State())
	}
}

func TestRestorationFailureAtEveryModuleOperationRequiresRecovery(t *testing.T) {
	tests := []struct {
		name      string
		initial   map[string]bool
		operation string
	}{
		{"remove emulation", map[string]bool{"kvm": true, "kvm_amd": true}, "-r cpuid_fault_emulation"},
		{"restore kvm amd", map[string]bool{"kvm": true, "kvm_amd": true}, "kvm_amd"},
		{"restore kvm", map[string]bool{"kvm": true}, "kvm"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := newFakeHost()
			host.loaded = test.initial
			journal := &memoryJournal{}
			controller := testController(t, host, journal)
			session, err := controller.StartSession(context.Background(), "10", "wrapper")
			if err != nil {
				t.Fatal(err)
			}
			host.failOnce = test.operation
			host.failed = false
			if err := controller.EndSession(context.Background(), session.ID); err == nil {
				t.Fatal("expected restoration failure")
			}
			if controller.State() != StateRecoveryRequired || journal.record == nil {
				t.Fatalf("got state=%s journal=%+v", controller.State(), journal.record)
			}
		})
	}
}

func TestPreflightRejectsNonRootMissingToolsAndBusyKVM(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*fakeHost, *Options)
		wanted error
	}{
		{"non-root", func(_ *fakeHost, o *Options) { o.EffectiveUID = func() int { return 1000 } }, nil},
		{"missing modprobe", func(h *fakeHost, _ *Options) { h.lookErr = errors.New("missing") }, nil},
		{"busy KVM", func(h *fakeHost, _ *Options) { h.refCount["kvm_amd"] = 1 }, ErrKVMBusy},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := newFakeHost()
			journal := &memoryJournal{}
			options := Options{Runner: host, Modules: host, Journal: journal, KernelRelease: "6.18.0", EffectiveUID: func() int { return 0 }}
			test.setup(host, &options)
			controller, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			_, err = controller.StartSession(context.Background(), "10", "wrapper")
			if err == nil || test.wanted != nil && !errors.Is(err, test.wanted) {
				t.Fatalf("got %v", err)
			}
			if journal.writes != 0 {
				t.Fatal("preflight failure wrote ownership journal")
			}
		})
	}
}

func TestConcurrentSessionsCauseOneTransition(t *testing.T) {
	host := newFakeHost()
	controller := testController(t, host, &memoryJournal{})
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func(id int) {
			defer wait.Done()
			_, err := controller.StartSession(context.Background(), string(rune('A'+id)), "wrapper")
			errorsFound <- err
		}(index)
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(controller.Sessions()) != 8 {
		t.Fatalf("got %d sessions", len(controller.Sessions()))
	}
	count := 0
	for _, call := range host.calls {
		if call == "modprobe cpuid_fault_emulation" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("activation happened %d times: %v", count, host.calls)
	}
}

func TestPreexistingEmulationIsNeverOwnedOrUnloaded(t *testing.T) {
	host := newFakeHost()
	host.loaded = map[string]bool{"cpuid_fault_emulation": true}
	journal := &memoryJournal{}
	controller := testController(t, host, journal)
	session, err := controller.StartSession(context.Background(), "10", "wrapper")
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.EndSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	if !host.Loaded("cpuid_fault_emulation") || len(host.calls) != 0 || journal.writes != 0 {
		t.Fatalf("external emulation was mutated: calls=%v writes=%d", host.calls, journal.writes)
	}
}

func TestLifetimeStopClosesOnlyMatchingOrphan(t *testing.T) {
	host := newFakeHost()
	controller := testController(t, host, &memoryJournal{})
	first, _ := controller.StartSession(context.Background(), "10", "wrapper")
	second, _ := controller.StartSession(context.Background(), "20", "wrapper")
	_ = controller.ObserveLifetime(context.Background(), lifetime("10", 100, true))
	_ = controller.ObserveLifetime(context.Background(), lifetime("20", 200, true))
	if err := controller.ObserveLifetime(context.Background(), lifetime("10", 100, false)); err != nil {
		t.Fatal(err)
	}
	if len(controller.Sessions()) != 1 || !host.Loaded("cpuid_fault_emulation") {
		t.Fatalf("wrong orphan closure: %+v", controller.Sessions())
	}
	if err := controller.EndSession(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	if err := controller.EndSession(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileAdoptsRunningSessionOrRestoresOwnedState(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(map[bool]string{true: "adopt", false: "restore"}[running], func(t *testing.T) {
			host := newFakeHost()
			host.loaded = map[string]bool{"cpuid_fault_emulation": true}
			journal := &memoryJournal{record: &JournalRecord{Owned: true, Before: ModuleSnapshot{KVM: true, KVMAMD: true}, Sessions: []JournalSession{{ID: "session", AppID: "10", Source: "wrapper"}}}}
			controller := testController(t, host, journal)
			if err := controller.Reconcile(context.Background(), map[string]bool{"10": running}); err != nil {
				t.Fatal(err)
			}
			if running {
				if controller.State() != StateEmulationActive || len(controller.Sessions()) != 1 {
					t.Fatalf("session was not adopted")
				}
			} else if controller.State() != StateIdle || host.snapshot() != (ModuleSnapshot{KVM: true, KVMAMD: true}) {
				t.Fatalf("owned state was not restored: %s %+v", controller.State(), host.snapshot())
			}
		})
	}
}

func TestAmbiguousJournalIsNonDestructive(t *testing.T) {
	host := newFakeHost()
	host.loaded = map[string]bool{"cpuid_fault_emulation": true}
	journal := &memoryJournal{record: &JournalRecord{Owned: false, Before: ModuleSnapshot{KVM: true, KVMAMD: true}}}
	controller := testController(t, host, journal)
	if err := controller.Reconcile(context.Background(), nil); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("got %v", err)
	}
	if controller.State() != StateRecoveryRequired || len(host.calls) != 0 {
		t.Fatalf("ambiguous recovery mutated state: %s %v", controller.State(), host.calls)
	}
}

func TestReconcileEveryJournaledCrashPoint(t *testing.T) {
	before := ModuleSnapshot{KVM: true, KVMAMD: true}
	crashPoints := []struct {
		name    string
		actual  map[string]bool
		running bool
		state   State
	}{
		{"journal written before mutation", map[string]bool{"kvm": true, "kvm_amd": true}, false, StateIdle},
		{"kvm amd removed", map[string]bool{"kvm": true}, false, StateIdle},
		{"all kvm removed", map[string]bool{}, false, StateIdle},
		{"emulation loaded with running game", map[string]bool{"cpuid_fault_emulation": true}, true, StateEmulationActive},
		{"emulation loaded without running game", map[string]bool{"cpuid_fault_emulation": true}, false, StateIdle},
		{"restoration removed emulation", map[string]bool{}, false, StateIdle},
		{"restoration complete before clear", map[string]bool{"kvm": true, "kvm_amd": true}, false, StateIdle},
	}
	for _, crash := range crashPoints {
		t.Run(crash.name, func(t *testing.T) {
			host := newFakeHost()
			host.loaded = crash.actual
			journal := &memoryJournal{record: &JournalRecord{
				Owned: true, Before: before, Phase: "interrupted",
				Sessions: []JournalSession{{ID: "session", AppID: "10", Source: "wrapper"}},
			}}
			controller := testController(t, host, journal)
			if err := controller.Reconcile(context.Background(), map[string]bool{"10": crash.running}); err != nil {
				t.Fatal(err)
			}
			if controller.State() != crash.state {
				t.Fatalf("got state %s, want %s", controller.State(), crash.state)
			}
			if crash.state == StateIdle {
				if host.snapshot() != before || journal.record != nil {
					t.Fatalf("did not restore cleanly: modules=%+v journal=%+v", host.snapshot(), journal.record)
				}
			} else if len(controller.Sessions()) != 1 || journal.record == nil {
				t.Fatalf("running state was not adopted")
			}
		})
	}
}

func TestFileJournalRoundTripAndClear(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime")
	journal := NewFileJournal(dir)
	wanted := JournalRecord{Phase: "active", Owned: true, Before: ModuleSnapshot{KVM: true}, Sessions: []JournalSession{{ID: "x", AppID: "10"}}}
	if err := journal.Write(wanted); err != nil {
		t.Fatal(err)
	}
	loaded, err := journal.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.Phase != wanted.Phase || len(loaded.Sessions) != 1 {
		t.Fatalf("unexpected journal: %+v", loaded)
	}
	if info, err := os.Stat(filepath.Join(dir, "transition-journal.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal permissions: %v, %v", info, err)
	}
	if err := journal.Clear(); err != nil {
		t.Fatal(err)
	}
	if loaded, err := journal.Load(); err != nil || loaded != nil {
		t.Fatalf("journal remains: %+v, %v", loaded, err)
	}
}

func lifetime(appID string, instance uint64, running bool) model.LifetimeRequest {
	return model.LifetimeRequest{AppID: appID, InstanceID: instance, Running: running}
}
