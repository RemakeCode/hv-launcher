package jobs

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxRetainedJobs    = 64
	maxJobOutputLines  = 100
	maxJobOutputBytes  = 16 << 10
	maxJobOutputLine   = 512
	setupSubscriberCap = 16
)

var ErrBusy = errors.New("another setup operation is already running")

type JobState string

const (
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
)

type JobSnapshot struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	State      JobState   `json:"state"`
	Phase      string     `json:"phase"`
	Progress   int        `json:"progress"`
	Output     []string   `json:"output"`
	Result     any        `json:"result,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type ActiveJob struct {
	Active bool         `json:"active"`
	Job    *JobSnapshot `json:"job,omitempty"`
}

type JobEvent struct {
	Type string      `json:"type"`
	Job  JobSnapshot `json:"job"`
}

type JobWork func(*Job) (any, error)

type Coordinator struct {
	mu          sync.Mutex
	jobs        map[string]*JobSnapshot
	order       []string
	active      string
	subscribers map[uint64]chan JobEvent
	nextSub     uint64
	now         func() time.Time
}

type Job struct {
	coordinator *Coordinator
	id          string
}

func NewCoordinator() *Coordinator {
	return &Coordinator{
		jobs: make(map[string]*JobSnapshot), subscribers: make(map[uint64]chan JobEvent), now: time.Now,
	}
}

func (c *Coordinator) Start(kind, phase string, work JobWork) (JobSnapshot, error) {
	c.mu.Lock()
	if c.active != "" {
		c.mu.Unlock()
		return JobSnapshot{}, ErrBusy
	}

	idBytes := make([]byte, 18)
	if _, err := rand.Read(idBytes); err != nil {
		c.mu.Unlock()
		return JobSnapshot{}, err
	}

	id := base64.RawURLEncoding.EncodeToString(idBytes)
	snapshot := &JobSnapshot{ID: id, Kind: kind, State: JobRunning, Phase: phase, Progress: 0, StartedAt: c.now()}
	c.jobs[id] = snapshot
	c.order = append(c.order, id)
	c.active = id
	c.pruneLocked()
	event := c.eventLocked(snapshot)
	initial := cloneJob(snapshot)
	c.mu.Unlock()
	c.publish(event)

	go func() {
		job := &Job{coordinator: c, id: id}
		result, err := runJobWork(job, work)
		c.finish(id, result, err)
	}()
	return initial, nil
}

func runJobWork(job *Job, work JobWork) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.New("setup operation failed unexpectedly")
		}
	}()
	return work(job)
}

func (j *Job) Update(phase string, progress int) {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	c := j.coordinator
	c.mu.Lock()
	snapshot, ok := c.jobs[j.id]
	if !ok || snapshot.State != JobRunning {
		c.mu.Unlock()
		return
	}

	snapshot.Phase = sanitizeText(phase, 128)
	snapshot.Progress = progress
	event := c.eventLocked(snapshot)
	c.mu.Unlock()
	c.publish(event)
}

func (j *Job) Output(line string) {
	c := j.coordinator
	line = sanitizeText(line, maxJobOutputLine)
	if line == "" {
		return
	}

	c.mu.Lock()
	snapshot, ok := c.jobs[j.id]
	if !ok || snapshot.State != JobRunning {
		c.mu.Unlock()
		return
	}

	snapshot.Output = append(snapshot.Output, line)
	for len(snapshot.Output) > maxJobOutputLines || outputSize(snapshot.Output) > maxJobOutputBytes {
		snapshot.Output = snapshot.Output[1:]
	}
	event := c.eventLocked(snapshot)
	c.mu.Unlock()
	c.publish(event)
}

func (c *Coordinator) finish(id string, result any, workErr error) {
	c.mu.Lock()
	snapshot, ok := c.jobs[id]
	if !ok {
		c.mu.Unlock()
		return
	}

	finished := c.now()
	snapshot.FinishedAt = &finished
	snapshot.Progress = 100
	if workErr != nil {
		snapshot.State = JobFailed
		snapshot.Phase = "failed"
		snapshot.Error = sanitizeText(workErr.Error(), 1024)
	} else {
		snapshot.State = JobSucceeded
		snapshot.Phase = "complete"
		snapshot.Result = result
	}
	if c.active == id {
		c.active = ""
	}

	event := c.eventLocked(snapshot)
	c.mu.Unlock()
	c.publish(event)
}

func (c *Coordinator) Active() ActiveJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == "" {
		return ActiveJob{Active: false}
	}

	snapshot := cloneJob(c.jobs[c.active])
	return ActiveJob{Active: true, Job: &snapshot}
}

func (c *Coordinator) Get(id string) (JobSnapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot, ok := c.jobs[id]
	if !ok {
		return JobSnapshot{}, false
	}

	return cloneJob(snapshot), true
}

func (c *Coordinator) Subscribe() (<-chan JobEvent, func()) {
	c.mu.Lock()
	id := c.nextSub
	c.nextSub++
	channel := make(chan JobEvent, setupSubscriberCap)
	c.subscribers[id] = channel
	c.mu.Unlock()

	var once sync.Once
	return channel, func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subscribers, id)
			c.mu.Unlock()
		})
	}
}

func (c *Coordinator) publish(event JobEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, subscriber := range c.subscribers {
		select {
		case subscriber <- event:
		default:
			// Snapshots are authoritative. Losing intermediate events is safer
			// than allowing a browser client to block a root mutation.
		}
	}
}

func (c *Coordinator) eventLocked(snapshot *JobSnapshot) JobEvent {
	return JobEvent{Type: "setup-job", Job: cloneJob(snapshot)}
}

func (c *Coordinator) pruneLocked() {
	for len(c.order) > maxRetainedJobs {
		oldest := c.order[0]
		if oldest == c.active {
			return
		}
		delete(c.jobs, oldest)
		c.order = c.order[1:]
	}
}

func cloneJob(snapshot *JobSnapshot) JobSnapshot {
	cloned := *snapshot
	cloned.Output = append([]string(nil), snapshot.Output...)
	if snapshot.FinishedAt != nil {
		finished := *snapshot.FinishedAt
		cloned.FinishedAt = &finished
	}

	return cloned
}

func outputSize(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line)
	}

	return total
}

func sanitizeText(value string, maximum int) string {
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' || unicode.IsPrint(character) {
			return character
		}
		return -1
	}, value)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.TrimSpace(value)
	for len(value) > maximum {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
