package jobs

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCoordinatorSerializesMutationsAndRetainsTerminalSnapshot(t *testing.T) {
	coordinator := NewCoordinator()
	release := make(chan struct{})
	started, err := coordinator.Start("proton-install", "validating", func(job *Job) (any, error) {
		job.Update("installing", 50)
		<-release
		return map[string]bool{"restartSteam": true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Start("umip-apply", "starting", func(*Job) (any, error) { return nil, nil }); !errors.Is(err, ErrBusy) {
		t.Fatalf("second mutation error = %v", err)
	}
	close(release)
	snapshot := waitForJob(t, coordinator, started.ID)
	if snapshot.State != JobSucceeded || snapshot.Result == nil || coordinator.Active().Active {
		t.Fatalf("unexpected terminal snapshot: %+v", snapshot)
	}
}

func TestSlowSubscriberCannotBlockOrCancelMutation(t *testing.T) {
	coordinator := NewCoordinator()
	_, cancel := coordinator.Subscribe()
	defer cancel()
	done := make(chan struct{})
	started, err := coordinator.Start("module-install", "starting", func(job *Job) (any, error) {
		for index := 0; index < 1_000; index++ {
			job.Update("building", index%101)
		}
		close(done)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked mutation")
	}
	if snapshot := waitForJob(t, coordinator, started.ID); snapshot.State != JobSucceeded {
		t.Fatalf("job was cancelled: %+v", snapshot)
	}
}

func TestJobOutputIsSanitizedAndBounded(t *testing.T) {
	coordinator := NewCoordinator()
	started, err := coordinator.Start("module-install", "starting", func(job *Job) (any, error) {
		for index := 0; index < 300; index++ {
			job.Output(strings.Repeat("x", 700) + "\x00\nsecret-next-line")
		}
		return nil, errors.New(strings.Repeat("failure ", 300))
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := waitForJob(t, coordinator, started.ID)
	if snapshot.State != JobFailed || len(snapshot.Output) > maxJobOutputLines || outputSize(snapshot.Output) > maxJobOutputBytes || len(snapshot.Error) > 1024 {
		t.Fatalf("output was not bounded: lines=%d bytes=%d error=%d", len(snapshot.Output), outputSize(snapshot.Output), len(snapshot.Error))
	}
	for _, line := range snapshot.Output {
		if strings.ContainsAny(line, "\x00\r\n") || len(line) > maxJobOutputLine {
			t.Fatalf("output was not sanitized: %q", line)
		}
	}
}

func waitForJob(t *testing.T, coordinator *Coordinator, id string) JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, ok := coordinator.Get(id)
		if ok && snapshot.State != JobRunning {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
	return JobSnapshot{}
}
