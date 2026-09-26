package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Status string

const (
	StatusQueued      Status = "queued"
	StatusRunning     Status = "running"
	StatusRetrying    Status = "retrying"
	StatusWaiting     Status = "waiting_for_review"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusCancelled   Status = "cancelled"
	StatusSuperseded  Status = "superseded"
	StatusInterrupted Status = "interrupted"
)

type Event struct {
	JobID     string         `json:"job_id"`
	Type      string         `json:"type"`
	Status    Status         `json:"status"`
	Step      string         `json:"step,omitempty"`
	Message   string         `json:"message,omitempty"`
	Progress  int            `json:"progress"`
	Revision  int            `json:"project_revision,omitempty"`
	Updated   []string       `json:"updated,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type Job struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	Type           string     `json:"type"`
	Stage          string     `json:"stage"`
	Status         Status     `json:"status"`
	EventsURL      string     `json:"events_url"`
	InputRevision  int        `json:"input_revision,omitempty"`
	OutputRevision int        `json:"output_revision,omitempty"`
	Attempt        int        `json:"attempt"`
	Progress       int        `json:"progress"`
	Message        string     `json:"message,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type Manager struct {
	mu          sync.Mutex
	seq         int
	jobs        map[string]*jobState
	applyResult func(projectID string, jobType string) (revision int, updated []string, err error)
	statePath   string
	// reserved marks projects held by a synchronous change (an acceptance, a
	// review decision). Jobs and reservations exclude each other, so one
	// project never has two writers at once.
	reserved map[string]bool
}

type StepEmitter func(step string, message string, progress int, metadata map[string]any)

type Runner func(projectID string, jobType string, inputRevision int, emit StepEmitter) (revision int, updated []string, err error)

type jobState struct {
	job         Job
	projectID   string
	events      []Event
	subscribers map[chan Event]bool
	steps       []string
	runner      Runner
	done        chan struct{}
	doneClosed  bool
}

var ErrProjectBusy = errors.New("another project job is active")

// NewPersistentManager preserves job summaries and event timelines across
// workbench restarts. In-flight work is marked interrupted during recovery;
// stage artifacts remain untouched and the user can retry against the latest
// project revision.
func NewPersistentManager(statePath string) *Manager {
	m := &Manager{
		jobs:      map[string]*jobState{},
		statePath: statePath,
	}
	m.load()
	return m
}

func (m *Manager) StartWithRevision(projectID, jobType string, inputRevision int, steps []string, runner Runner) Job {
	job, _ := m.startWithRevision(projectID, jobType, inputRevision, steps, runner, false)
	return job
}

// StartSingleFlightWithRevision atomically checks for active work and reserves
// the project before launching the runner.
func (m *Manager) StartSingleFlightWithRevision(projectID, jobType string, inputRevision int, steps []string, runner Runner) (Job, error) {
	return m.startWithRevision(projectID, jobType, inputRevision, steps, runner, true)
}

func (m *Manager) startWithRevision(projectID, jobType string, inputRevision int, steps []string, runner Runner, singleFlight bool) (Job, error) {
	now := time.Now()
	m.mu.Lock()
	if singleFlight && m.busyLocked(projectID) {
		m.mu.Unlock()
		return Job{}, ErrProjectBusy
	}
	m.seq++
	id := fmt.Sprintf("job_%06d", m.seq)
	state := &jobState{
		projectID: projectID,
		job: Job{
			ID: id, ProjectID: projectID, Type: jobType, Stage: jobType,
			Status: StatusQueued, EventsURL: fmt.Sprintf("/api/v1/projects/%s/jobs/%s/events", projectID, id),
			InputRevision: inputRevision, Attempt: 1, Progress: 0, Message: "Job queued.", CreatedAt: now, UpdatedAt: now,
		},
		subscribers: map[chan Event]bool{},
		steps:       append([]string(nil), steps...),
		runner:      runner,
		done:        make(chan struct{}),
	}
	m.jobs[id] = state
	m.appendLocked(state, Event{
		JobID:     id,
		Type:      jobType,
		Status:    StatusQueued,
		Progress:  0,
		Message:   "Job queued.",
		CreatedAt: now,
	})
	m.persistLocked()
	job := state.job
	m.mu.Unlock()

	go m.run(id, runner)
	return job, nil
}

// Wait blocks on the job lifecycle rather than polling. A returned terminal
// job is synchronized after its final persistent-state write.
func (m *Manager) Wait(ctx context.Context, id string) (Job, error) {
	m.mu.Lock()
	state, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("job not found")
	}
	if terminal(state.job.Status) {
		job := state.job
		m.mu.Unlock()
		return job, nil
	}
	done := state.done
	m.mu.Unlock()

	select {
	case <-ctx.Done():
		return Job{}, ctx.Err()
	case <-done:
		m.mu.Lock()
		defer m.mu.Unlock()
		state, ok := m.jobs[id]
		if !ok {
			return Job{}, fmt.Errorf("job not found")
		}
		return state.job, nil
	}
}

func (m *Manager) List(projectID string) []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Job, 0)
	for _, state := range m.jobs {
		if projectID == "" || state.projectID == projectID {
			items = append(items, state.job)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items
}

func (m *Manager) Retry(id string, inputRevision int) (Job, error) {
	m.mu.Lock()
	state, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("job not found")
	}
	if state.job.Status == StatusQueued || state.job.Status == StatusRunning || state.job.Status == StatusRetrying {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("job is still active")
	}
	if state.runner == nil {
		m.mu.Unlock()
		return Job{}, fmt.Errorf("runner is unavailable after restart; rerun the stage from the project pipeline")
	}
	projectID, jobType := state.projectID, state.job.Type
	steps, runner := append([]string(nil), state.steps...), state.runner
	m.mu.Unlock()
	return m.startWithRevision(projectID, jobType, inputRevision, steps, runner, true)
}

func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return state.job, true
}

// HasActiveProject reports whether the project has a running job or is held by
// a synchronous change.
func (m *Manager) HasActiveProject(projectID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busyLocked(projectID)
}

func (m *Manager) busyLocked(projectID string) bool {
	if m.reserved[projectID] {
		return true
	}
	for _, state := range m.jobs {
		if state.projectID == projectID && !terminal(state.job.Status) {
			return true
		}
	}
	return false
}

// Reserve holds the project for one synchronous change. It fails with
// ErrProjectBusy while a job runs or another change holds the project; the
// returned release must be called when the change is done.
func (m *Manager) Reserve(projectID string) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busyLocked(projectID) {
		return nil, ErrProjectBusy
	}
	if m.reserved == nil {
		m.reserved = map[string]bool{}
	}
	m.reserved[projectID] = true
	released := false
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !released {
			delete(m.reserved, projectID)
			released = true
		}
	}, nil
}

func (m *Manager) ForgetProject(projectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, state := range m.jobs {
		if state.projectID != projectID {
			continue
		}
		for subscriber := range state.subscribers {
			close(subscriber)
		}
		if !state.doneClosed {
			close(state.done)
			state.doneClosed = true
		}
		delete(m.jobs, id)
	}
	m.persistLocked()
}

func (m *Manager) Subscribe(id string) (<-chan Event, []Event, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.jobs[id]
	if !ok {
		return nil, nil, false
	}
	ch := make(chan Event, 16)
	replay := append([]Event(nil), state.events...)
	if !terminal(state.job.Status) {
		state.subscribers[ch] = true
	}
	return ch, replay, true
}

func (m *Manager) Unsubscribe(id string, ch <-chan Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.jobs[id]
	if !ok {
		return
	}
	for sub := range state.subscribers {
		if (<-chan Event)(sub) == ch {
			delete(state.subscribers, sub)
			close(sub)
			return
		}
	}
}

func (m *Manager) run(id string, runner Runner) {
	m.update(id, StatusRunning, "", "Job started.", 2, 0, nil, nil)
	if runner != nil {
		m.mu.Lock()
		state := m.jobs[id]
		projectID := state.projectID
		jobType := state.job.Type
		inputRevision := state.job.InputRevision
		m.mu.Unlock()

		emit := func(step string, message string, progress int, metadata map[string]any) {
			m.update(id, StatusRunning, step, message, progress, 0, nil, metadata)
		}
		revision, updated, err := runner(projectID, jobType, inputRevision, emit)
		if err != nil {
			m.update(id, StatusFailed, "", err.Error(), 100, 0, nil, nil)
			return
		}
		m.update(id, StatusCompleted, "", "Job completed.", 100, revision, updated, nil)
		return
	}
	m.update(id, StatusFailed, "", "job has no runner", 100, 0, nil, nil)
}

func (m *Manager) update(id string, status Status, step string, message string, progress int, revision int, updated []string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.jobs[id]
	if !ok {
		return
	}
	state.job.Status = status
	state.job.UpdatedAt = time.Now()
	state.job.Progress = progress
	state.job.Message = message
	if status == StatusRunning && state.job.StartedAt == nil {
		started := state.job.UpdatedAt
		state.job.StartedAt = &started
	}
	if status == StatusFailed {
		state.job.Error = message
	}
	if status == StatusCompleted {
		state.job.OutputRevision = revision
	}
	if terminal(status) {
		completed := state.job.UpdatedAt
		state.job.CompletedAt = &completed
	}
	m.appendLocked(state, Event{
		JobID:     id,
		Type:      state.job.Type,
		Status:    status,
		Step:      step,
		Message:   message,
		Progress:  progress,
		Revision:  revision,
		Updated:   updated,
		Metadata:  metadata,
		CreatedAt: state.job.UpdatedAt,
	})
	if terminal(status) {
		for ch := range state.subscribers {
			close(ch)
			delete(state.subscribers, ch)
		}
	}
	m.persistLocked()
	if terminal(status) && !state.doneClosed {
		close(state.done)
		state.doneClosed = true
	}
}

func (m *Manager) appendLocked(state *jobState, event Event) {
	state.events = append(state.events, event)
	for ch := range state.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

type persistedJobs struct {
	Version int     `json:"version"`
	Jobs    []Job   `json:"jobs"`
	Events  []Event `json:"events"`
}

func (m *Manager) load() {
	if m.statePath == "" {
		return
	}
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		return
	}
	var saved persistedJobs
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	eventsByJob := map[string][]Event{}
	for _, event := range saved.Events {
		eventsByJob[event.JobID] = append(eventsByJob[event.JobID], event)
	}
	now := time.Now()
	for _, job := range saved.Jobs {
		if !terminal(job.Status) {
			job.Status = StatusInterrupted
			job.Progress = 100
			job.Message = "Job was interrupted by a workbench restart. Retry the stage against the latest revision."
			job.Error = job.Message
			job.UpdatedAt = now
			job.CompletedAt = &now
			eventsByJob[job.ID] = append(eventsByJob[job.ID], Event{JobID: job.ID, Type: job.Type, Status: StatusInterrupted, Message: job.Message, Progress: 100, CreatedAt: now})
		}
		done := make(chan struct{})
		close(done)
		m.jobs[job.ID] = &jobState{job: job, projectID: job.ProjectID, events: eventsByJob[job.ID], subscribers: map[chan Event]bool{}, done: done, doneClosed: true}
		var seq int
		_, _ = fmt.Sscanf(job.ID, "job_%d", &seq)
		if seq > m.seq {
			m.seq = seq
		}
	}
	m.persistLocked()
}

func (m *Manager) persistLocked() {
	if m.statePath == "" {
		return
	}
	saved := persistedJobs{Version: 1}
	for _, state := range m.jobs {
		saved.Jobs = append(saved.Jobs, state.job)
		saved.Events = append(saved.Events, state.events...)
	}
	sort.Slice(saved.Jobs, func(i, j int) bool { return saved.Jobs[i].CreatedAt.Before(saved.Jobs[j].CreatedAt) })
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(m.statePath), 0o755) != nil {
		return
	}
	tmp := m.statePath + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, m.statePath)
}

func terminal(status Status) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusSuperseded, StatusInterrupted:
		return true
	default:
		return false
	}
}
