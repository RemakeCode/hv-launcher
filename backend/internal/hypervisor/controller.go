package hypervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"hv-launcher/internal/model"
)

type State string

const (
	StateIdle                 State = "idle"
	StateSwitchingToEmulation State = "switching-to-emulation"
	StateEmulationActive      State = "emulation-active"
	StateRestoringKVM         State = "restoring-kvm"
	StateRecoveryRequired     State = "recovery-required"
)

var ErrKVMBusy = errors.New("KVM is busy")
var ErrRecoveryRequired = errors.New("module ownership is ambiguous; recovery is required")

type Options struct {
	Runner        CommandRunner
	Modules       ModuleState
	Journal       Journal
	KernelRelease string
	EffectiveUID  func() int
	Logger        *slog.Logger
}

type Controller struct {
	mu       sync.Mutex
	options  Options
	state    State
	owned    bool
	before   ModuleSnapshot
	sessions map[string]model.Session
}

func New(options Options) (*Controller, error) {
	if options.Runner == nil || options.Modules == nil || options.Journal == nil {
		return nil, errors.New("runner, module state, and journal are required")
	}

	if options.EffectiveUID == nil {
		options.EffectiveUID = os.Geteuid
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Controller{options: options, state: StateIdle, sessions: map[string]model.Session{}}, nil
}

func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Controller) Sessions() []model.Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionListLocked()
}

func (c *Controller) StartSession(ctx context.Context, appID, source string) (model.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.options.Logger.Info("session start requested", "app_id", appID, "source", source, "state", c.state, "active_sessions", len(c.sessions))
	if c.state == StateRecoveryRequired {
		c.options.Logger.Error("session start rejected", "app_id", appID, "error", ErrRecoveryRequired)
		return model.Session{}, ErrRecoveryRequired
	}

	session := model.Session{ID: newSessionID(), AppID: appID, Source: source}
	if len(c.sessions) == 0 {
		if c.options.Modules.Loaded("cpuid_fault_emulation") && !c.owned {
			c.state = StateEmulationActive
		} else if err := c.activateLocked(ctx, session); err != nil {
			c.options.Logger.Error("session activation failed", "app_id", appID, "error", err)
			return model.Session{}, err
		}
	}

	c.sessions[session.ID] = session
	if c.owned {
		if err := c.writeJournalLocked("active"); err != nil {
			c.state = StateRecoveryRequired
			c.options.Logger.Error("failed to persist active session", "app_id", appID, "session_id", session.ID, "error", err)
			return model.Session{}, fmt.Errorf("persist active session: %w", err)
		}
	}

	c.options.Logger.Info("session started", "app_id", appID, "session_id", session.ID, "state", c.state, "active_sessions", len(c.sessions))
	return session, nil
}

func (c *Controller) EndSession(ctx context.Context, sessionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.sessions[sessionID]; !exists {
		return nil
	}

	appID := c.sessions[sessionID].AppID
	delete(c.sessions, sessionID)
	c.options.Logger.Info("session ended", "app_id", appID, "session_id", sessionID, "remaining_sessions", len(c.sessions))
	if len(c.sessions) > 0 {
		if c.owned {
			return c.writeJournalLocked("active")
		}
		return nil
	}

	if !c.owned {
		c.state = StateIdle
		c.options.Logger.Info("controller returned to idle", "reason", "unowned session ended")
		return nil
	}
	return c.restoreLocked(ctx)
}

func (c *Controller) ObserveLifetime(ctx context.Context, request model.LifetimeRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if request.Running {
		for id, session := range c.sessions {
			if session.AppID == request.AppID && session.InstanceID == 0 {
				session.InstanceID = request.InstanceID
				c.sessions[id] = session
				c.options.Logger.Info("Steam lifetime attached to session", "app_id", request.AppID, "session_id", id, "instance_id", request.InstanceID)
				if c.owned {
					return c.writeJournalLocked("active")
				}
				return nil
			}
		}
		return nil
	}

	for id, session := range c.sessions {
		if session.AppID == request.AppID && (request.InstanceID == 0 || session.InstanceID == request.InstanceID) {
			delete(c.sessions, id)
			c.options.Logger.Info("Steam lifetime closed session", "app_id", request.AppID, "session_id", id, "instance_id", request.InstanceID)
		}
	}

	if len(c.sessions) > 0 {
		if c.owned {
			return c.writeJournalLocked("active")
		}
		return nil
	}
	if c.owned {
		return c.restoreLocked(ctx)
	}

	if c.state == StateEmulationActive {
		c.state = StateIdle
		c.options.Logger.Info("controller returned to idle", "reason", "unowned Steam lifetime ended")
	}
	return nil
}

func (c *Controller) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.owned {
		return nil
	}

	c.options.Logger.Info("backend shutdown restoring owned module state", "active_sessions", len(c.sessions))
	c.sessions = map[string]model.Session{}
	return c.restoreLocked(ctx)
}

func (c *Controller) Reconcile(ctx context.Context, running map[string]bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	record, err := c.options.Journal.Load()
	if err != nil {
		c.state = StateRecoveryRequired
		c.options.Logger.Error("failed to load transition journal", "error", err)
		return err
	}

	if record == nil {
		if c.options.Modules.Loaded("cpuid_fault_emulation") {
			c.state = StateEmulationActive
			c.options.Logger.Info("reconciled pre-existing emulation module", "owned", false)
		} else {
			c.options.Logger.Info("no prior transition to reconcile", "state", c.state)
		}
		return nil
	}

	actual := c.snapshot()
	if actual == record.Before {
		c.state = StateIdle
		c.options.Logger.Info("clearing completed transition journal", "phase", record.Phase)
		return c.options.Journal.Clear()
	}
	if !record.Owned {
		c.state = StateRecoveryRequired
		c.options.Logger.Error("transition recovery requires manual action", "phase", record.Phase, "error", ErrRecoveryRequired)
		return ErrRecoveryRequired
	}

	c.before, c.owned = record.Before, true
	if actual.Emulation {
		for _, session := range record.Sessions {
			if running[session.AppID] {
				c.sessions[session.ID] = model.Session{ID: session.ID, AppID: session.AppID, InstanceID: session.InstanceID, Source: session.Source}
			}
		}
		if len(c.sessions) > 0 {
			c.state = StateEmulationActive
			return c.writeJournalLocked("active")
		}
		return c.restoreLocked(ctx)
	}
	// The durable owned record proves these partial mutations belong to us, so a
	// crash before emulation loaded can be rolled back to the recorded snapshot.
	return c.restoreLocked(ctx)
}

func (c *Controller) activateLocked(ctx context.Context, pending model.Session) error {
	if c.options.EffectiveUID() != 0 {
		return errors.New("hypervisor transition requires root")
	}

	if _, err := c.options.Runner.LookPath("modprobe"); err != nil {
		return errors.New("modprobe is unavailable")
	}
	output, err := c.options.Runner.Run(ctx, "modinfo", "-F", "vermagic", "cpuid_fault_emulation")
	if err != nil {
		return fmt.Errorf("cpuid_fault_emulation is not installed: %w", err)
	}

	vermagic := strings.TrimSpace(string(output))
	if c.options.KernelRelease != "" && vermagic != c.options.KernelRelease && !strings.HasPrefix(vermagic, c.options.KernelRelease+" ") {
		return fmt.Errorf("cpuid_fault_emulation does not match kernel %s", c.options.KernelRelease)
	}
	if c.options.Modules.RefCount("kvm_amd") > 0 {
		return ErrKVMBusy
	}

	c.before = c.snapshot()
	c.owned = true
	c.state = StateSwitchingToEmulation
	c.options.Logger.Info("activating CPUID fault emulation", "app_id", pending.AppID, "kvm_loaded", c.before.KVM, "kvm_amd_loaded", c.before.KVMAMD, "emulation_loaded", c.before.Emulation)
	if err := c.options.Journal.Write(JournalRecord{Version: 1, Phase: string(c.state), Before: c.before, Owned: true, Sessions: []JournalSession{journalSession(pending)}}); err != nil {
		c.owned = false
		c.state = StateIdle
		return fmt.Errorf("write transition journal: %w", err)
	}

	if c.before.KVMAMD {
		if err := c.moduleCommand(ctx, false, "kvm_amd"); err != nil {
			return c.activationFailureLocked(ctx, "remove kvm_amd", err)
		}
	}
	if c.before.KVM {
		if err := c.moduleCommand(ctx, false, "kvm"); err != nil {
			return c.activationFailureLocked(ctx, "remove kvm", err)
		}
	}
	if err := c.moduleCommand(ctx, true, "cpuid_fault_emulation"); err != nil {
		return c.activationFailureLocked(ctx, "load cpuid_fault_emulation", err)
	}
	c.state = StateEmulationActive
	c.options.Logger.Info("CPUID fault emulation active", "app_id", pending.AppID)
	return nil
}

func (c *Controller) activationFailureLocked(ctx context.Context, step string, cause error) error {
	c.options.Logger.Error("activation step failed; rolling back", "step", step, "error", cause)
	rollbackErr := c.restoreModulesLocked(ctx)
	if rollbackErr != nil {
		c.state = StateRecoveryRequired
		c.options.Logger.Error("activation rollback failed", "step", step, "error", rollbackErr)
		return fmt.Errorf("%s: %w; rollback failed: %v", step, cause, rollbackErr)
	}

	c.owned = false
	c.state = StateIdle
	if err := c.options.Journal.Clear(); err != nil {
		c.state = StateRecoveryRequired
		return fmt.Errorf("%s: %w; clearing rollback journal: %v", step, cause, err)
	}
	c.options.Logger.Info("activation rollback complete", "failed_step", step, "state", c.state)
	return fmt.Errorf("%s: %w", step, cause)
}

func (c *Controller) restoreLocked(ctx context.Context) error {
	c.state = StateRestoringKVM
	c.options.Logger.Info("restoring KVM module state", "active_sessions", len(c.sessions))
	if err := c.writeJournalLocked(string(c.state)); err != nil {
		c.state = StateRecoveryRequired
		return err
	}

	if err := c.restoreModulesLocked(ctx); err != nil {
		c.state = StateRecoveryRequired
		return err
	}
	if err := c.options.Journal.Clear(); err != nil {
		c.state = StateRecoveryRequired
		return err
	}

	c.owned = false
	c.state = StateIdle
	c.options.Logger.Info("KVM module state restored", "state", c.state)
	return nil
}

func (c *Controller) restoreModulesLocked(ctx context.Context) error {
	if !c.before.Emulation && c.options.Modules.Loaded("cpuid_fault_emulation") {
		if err := c.moduleCommand(ctx, false, "cpuid_fault_emulation"); err != nil {
			return fmt.Errorf("remove cpuid_fault_emulation: %w", err)
		}
	}
	if c.before.KVMAMD && !c.options.Modules.Loaded("kvm_amd") {
		if err := c.moduleCommand(ctx, true, "kvm_amd"); err != nil {
			return fmt.Errorf("restore kvm_amd: %w", err)
		}
	}
	if c.before.KVM && !c.options.Modules.Loaded("kvm") {
		if err := c.moduleCommand(ctx, true, "kvm"); err != nil {
			return fmt.Errorf("restore kvm: %w", err)
		}
	}
	return nil
}

func (c *Controller) moduleCommand(ctx context.Context, load bool, name string) error {
	args := []string{name}
	operation := "load"
	if !load {
		args = []string{"-r", name}
		operation = "remove"
	}

	c.options.Logger.Info("running module transition", "operation", operation, "module", name)
	output, err := c.options.Runner.Run(ctx, "modprobe", args...)
	if err != nil {
		return fmt.Errorf("modprobe %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}

	if c.options.Modules.Loaded(name) != load {
		return fmt.Errorf("module %s verification failed (loaded=%v)", name, c.options.Modules.Loaded(name))
	}
	c.options.Logger.Info("module transition complete", "operation", operation, "module", name)
	return nil
}

func (c *Controller) snapshot() ModuleSnapshot {
	return ModuleSnapshot{
		Emulation: c.options.Modules.Loaded("cpuid_fault_emulation"),
		KVM:       c.options.Modules.Loaded("kvm"),
		KVMAMD:    c.options.Modules.Loaded("kvm_amd"),
	}
}

func (c *Controller) writeJournalLocked(phase string) error {
	return c.options.Journal.Write(JournalRecord{Version: 1, Phase: phase, Before: c.before, Owned: c.owned, Sessions: c.journalSessionsLocked()})
}

func (c *Controller) journalSessionsLocked() []JournalSession {
	result := make([]JournalSession, 0, len(c.sessions))
	for _, session := range c.sessions {
		result = append(result, journalSession(session))
	}

	return result
}

func (c *Controller) sessionListLocked() []model.Session {
	result := make([]model.Session, 0, len(c.sessions))
	for _, session := range c.sessions {
		result = append(result, session)
	}

	return result
}

func journalSession(session model.Session) JournalSession {
	return JournalSession{ID: session.ID, AppID: session.AppID, InstanceID: session.InstanceID, Source: session.Source}
}

func newSessionID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}

	return hex.EncodeToString(buffer)
}
