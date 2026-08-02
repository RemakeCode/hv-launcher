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
	"unicode"
	"unicode/utf8"

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

const (
	maxActivationDiagnostic      = 4 << 10
	keyRejectedRemediation       = "Sign the generated kernel module and enroll or trust its certificate through your distribution's MOK/key mechanism, then test the module again."
	genericActivationRemediation = "Retry the module test or managed game; inspect the bounded activation detail if the failure persists."
)

type Options struct {
	Runner            CommandRunner
	Modules           ModuleState
	Journal           Journal
	KernelRelease     string
	EffectiveUID      func() int
	Logger            *slog.Logger
	VerificationStore model.ModuleVerificationStore
	ActivationFailure func(model.ManagedActivationFailure)
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

// TestModule performs the fixed, session-free CPUID load transaction used by
// installation and the readiness UI. A returned outcome is authoritative for
// the attempted load even when restoration subsequently requires recovery.
func (c *Controller) TestModule(ctx context.Context) (model.ModuleVerificationOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.testModuleLocked(ctx)
}

func (c *Controller) StartSession(ctx context.Context, appID, source string) (model.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.options.Logger.Info("session start requested", "app_id", appID, "source", source, "state", c.state, "active_sessions", len(c.sessions))
	if c.state == StateRecoveryRequired {
		c.options.Logger.Error("session start rejected", "app_id", appID, "error", ErrRecoveryRequired)
		return model.Session{}, ErrRecoveryRequired
	}

	sessionID, err := newSessionID(rand.Reader)
	if err != nil {
		c.options.Logger.Error("session start rejected", "app_id", appID, "error", err)
		return model.Session{}, err
	}

	session := model.Session{ID: sessionID, AppID: appID, Source: source}
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
		return c.blockedActivationLocked(pending.AppID, errors.New("hypervisor transition requires root"))
	}

	if _, err := c.options.Runner.LookPath("modprobe"); err != nil {
		return c.blockedActivationLocked(pending.AppID, errors.New("modprobe is unavailable"))
	}
	output, err := c.options.Runner.Run(ctx, "modinfo", "-F", "vermagic", "cpuid_fault_emulation")
	if err != nil {
		return c.blockedActivationLocked(pending.AppID, fmt.Errorf("cpuid_fault_emulation is not installed: %w", err))
	}

	vermagic := strings.TrimSpace(string(output))
	if c.options.KernelRelease != "" && vermagic != c.options.KernelRelease && !strings.HasPrefix(vermagic, c.options.KernelRelease+" ") {
		return c.blockedActivationLocked(pending.AppID, fmt.Errorf("cpuid_fault_emulation does not match kernel %s", c.options.KernelRelease))
	}
	if c.options.Modules.RefCount("kvm_amd") > 0 {
		return c.blockedActivationLocked(pending.AppID, ErrKVMBusy)
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
			return c.activationFailureLocked(ctx, "remove kvm_amd", err, pending.AppID)
		}
	}
	if c.before.KVM {
		if err := c.moduleCommand(ctx, false, "kvm"); err != nil {
			return c.activationFailureLocked(ctx, "remove kvm", err, pending.AppID)
		}
	}
	if err := c.moduleCommand(ctx, true, "cpuid_fault_emulation"); err != nil {
		return c.activationFailureLocked(ctx, "load cpuid_fault_emulation", err, pending.AppID)
	}
	c.recordVerificationLocked(model.ModuleVerificationOutcome{State: model.ModuleVerificationVerified})
	c.state = StateEmulationActive
	c.options.Logger.Info("CPUID fault emulation active", "app_id", pending.AppID)
	return nil
}

func (c *Controller) activationFailureLocked(ctx context.Context, step string, cause error, appID string) error {
	c.options.Logger.Error("activation step failed; rolling back", "step", step, "error", cause)
	var outcome model.ModuleVerificationOutcome
	if step == "load cpuid_fault_emulation" {
		outcome = verificationFailure(cause)
		c.recordVerificationLocked(outcome)
	} else {
		outcome = model.ModuleVerificationOutcome{State: model.ModuleVerificationPending, Classification: model.ModuleVerificationBlocked, Detail: sanitizeActivationText(cause.Error())}
	}
	rollbackErr := c.restoreModulesLocked(ctx)
	if rollbackErr != nil {
		c.state = StateRecoveryRequired
		c.options.Logger.Error("activation rollback failed", "step", step, "error", rollbackErr)
		if appID != "" {
			c.publishActivationFailureLocked(appID, outcome)
		}
		return fmt.Errorf("%s: %w; rollback failed: %v", step, cause, rollbackErr)
	}

	c.owned = false
	c.state = StateIdle
	if err := c.options.Journal.Clear(); err != nil {
		c.state = StateRecoveryRequired
		if appID != "" {
			c.publishActivationFailureLocked(appID, outcome)
		}
		return fmt.Errorf("%s: %w; clearing rollback journal: %v", step, cause, err)
	}
	if appID != "" {
		c.publishActivationFailureLocked(appID, outcome)
	}
	c.options.Logger.Info("activation rollback complete", "failed_step", step, "state", c.state)
	return fmt.Errorf("%s: %w", step, cause)
}

func (c *Controller) blockedActivationLocked(appID string, cause error) error {
	outcome := model.ModuleVerificationOutcome{
		State: model.ModuleVerificationPending, Classification: model.ModuleVerificationBlocked,
		Detail: sanitizeActivationText(cause.Error()),
	}
	c.publishActivationFailureLocked(appID, outcome)
	return cause
}

func (c *Controller) testModuleLocked(ctx context.Context) (model.ModuleVerificationOutcome, error) {
	if c.state == StateRecoveryRequired {
		return c.blockedModuleTestLocked("Module ownership recovery is required before testing the CPUID module.")
	}
	if len(c.sessions) > 0 || c.owned || c.state != StateIdle {
		return c.blockedModuleTestLocked("The hypervisor manager is busy with a managed game or transition.")
	}
	if c.options.Modules.Loaded("cpuid_fault_emulation") {
		return c.blockedModuleTestLocked("cpuid_fault_emulation is already loaded outside controller ownership.")
	}
	if c.options.EffectiveUID() != 0 {
		return c.blockedModuleTestLocked("Testing the CPUID module requires root.")
	}
	if _, err := c.options.Runner.LookPath("modprobe"); err != nil {
		return c.blockedModuleTestLocked("modprobe is unavailable.")
	}
	output, err := c.options.Runner.Run(ctx, "modinfo", "-F", "vermagic", "cpuid_fault_emulation")
	if err != nil {
		return c.blockedModuleTestLocked("cpuid_fault_emulation is not installed for the running kernel.")
	}
	vermagic := strings.TrimSpace(string(output))
	if c.options.KernelRelease != "" && vermagic != c.options.KernelRelease && !strings.HasPrefix(vermagic, c.options.KernelRelease+" ") {
		return c.blockedModuleTestLocked(fmt.Sprintf("cpuid_fault_emulation does not match kernel %s.", c.options.KernelRelease))
	}
	if c.options.Modules.RefCount("kvm_amd") > 0 {
		return c.blockedModuleTestLocked("KVM is busy; stop active virtual machines before testing the module.")
	}

	c.before = c.snapshot()
	c.owned = true
	c.state = StateSwitchingToEmulation
	if err := c.options.Journal.Write(JournalRecord{Version: 1, Phase: string(c.state), Before: c.before, Owned: true}); err != nil {
		c.owned = false
		c.state = StateIdle
		return c.blockedModuleTestLocked("The module test could not start because its transition journal could not be written.")
	}
	if c.before.KVMAMD {
		if err := c.moduleCommand(ctx, false, "kvm_amd"); err != nil {
			detail := sanitizeActivationText(err.Error())
			return c.blockedModuleTestOutcomeLocked(detail), c.activationFailureLocked(ctx, "remove kvm_amd", err, "")
		}
	}
	if c.before.KVM {
		if err := c.moduleCommand(ctx, false, "kvm"); err != nil {
			detail := sanitizeActivationText(err.Error())
			return c.blockedModuleTestOutcomeLocked(detail), c.activationFailureLocked(ctx, "remove kvm", err, "")
		}
	}

	if err := c.moduleCommand(ctx, true, "cpuid_fault_emulation"); err != nil {
		outcome := verificationFailure(err)
		return outcome, c.activationFailureLocked(ctx, "load cpuid_fault_emulation", err, "")
	}
	c.recordVerificationLocked(model.ModuleVerificationOutcome{State: model.ModuleVerificationVerified})
	if err := c.restoreLocked(ctx); err != nil {
		return model.ModuleVerificationOutcome{State: model.ModuleVerificationVerified}, err
	}
	return model.ModuleVerificationOutcome{State: model.ModuleVerificationVerified}, nil
}

func (c *Controller) blockedModuleTestLocked(detail string) (model.ModuleVerificationOutcome, error) {
	outcome := c.blockedModuleTestOutcomeLocked(detail)
	return outcome, errors.New(outcome.Detail)
}

func (c *Controller) blockedModuleTestOutcomeLocked(detail string) model.ModuleVerificationOutcome {
	state := model.ModuleVerificationPending
	if c.options.VerificationStore != nil {
		previous, err := c.options.VerificationStore.Load()
		if err != nil {
			c.options.Logger.Warn("previous module verification state could not be read for blocked test", "error", err)
		} else if previous.State == model.ModuleVerificationVerified || previous.State == model.ModuleVerificationFailed {
			state = previous.State
		}
	}
	return model.ModuleVerificationOutcome{
		State: state, Classification: model.ModuleVerificationBlocked,
		Detail: sanitizeActivationText(detail),
	}
}

func (c *Controller) recordVerificationLocked(outcome model.ModuleVerificationOutcome) {
	if c.options.VerificationStore == nil {
		return
	}
	if err := c.options.VerificationStore.SaveIfChanged(outcome); err != nil {
		c.options.Logger.Warn("module verification result could not be persisted", "error", err)
	}
}

func verificationFailure(cause error) model.ModuleVerificationOutcome {
	detail := sanitizeActivationText(cause.Error())
	if containsKeyRejection(detail) {
		return model.ModuleVerificationOutcome{
			State: model.ModuleVerificationFailed, Classification: model.ModuleVerificationKeyRejected,
			Detail: detail, Remediation: keyRejectedRemediation,
		}
	}
	return model.ModuleVerificationOutcome{
		State: model.ModuleVerificationFailed, Classification: model.ModuleVerificationGenericFailure,
		Detail: detail, Remediation: genericActivationRemediation,
	}
}

func containsKeyRejection(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, strings.ToLower("Required key not available")) || strings.Contains(lower, strings.ToLower("Key was rejected by service"))
}

func sanitizeActivationText(value string) string {
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' || unicode.IsPrint(character) {
			return character
		}
		return -1
	}, value)
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if len(value) > maxActivationDiagnostic {
		for len(value) > maxActivationDiagnostic {
			_, size := utf8.DecodeLastRuneInString(value)
			value = value[:len(value)-size]
		}
	}
	return value
}

func (c *Controller) publishActivationFailureLocked(appID string, outcome model.ModuleVerificationOutcome) {
	if c.options.ActivationFailure == nil || appID == "" {
		return
	}
	summary := "CPUID module activation failed"
	if outcome.Classification == model.ModuleVerificationKeyRejected {
		summary = "The kernel rejected the CPUID module signing key"
	}
	c.options.ActivationFailure(model.ManagedActivationFailure{
		AppID: appID, State: outcome.State, Classification: outcome.Classification,
		Summary: summary, Detail: outcome.Detail, Remediation: outcome.Remediation,
	})
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
		detail := sanitizeActivationText(string(output))
		if detail == "" {
			detail = sanitizeActivationText(err.Error())
		}
		return fmt.Errorf("modprobe %s: %s: %w", strings.Join(args, " "), detail, err)
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

func newSessionID(random io.Reader) (string, error) {
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(random, buffer); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}

	return hex.EncodeToString(buffer), nil
}
