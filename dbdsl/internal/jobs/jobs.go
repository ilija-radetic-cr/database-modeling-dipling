package jobs

import (
	"encoding/json"
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
}

type StepEmitter func(step string, message string, progress int, metadata map[string]any)

type Runner func(projectID string, jobType string, emit StepEmitter) (revision int, updated []string, err error)

type jobState struct {
	job         Job
	projectID   string
	events      []Event
	subscribers map[chan Event]bool
	steps       []string
	runner      Runner
}

func NewManager(applyResult func(projectID string, jobType string) (int, []string, error)) *Manager {
	return NewPersistentManager(applyResult, "")
}

// NewPersistentManager preserves job summaries and event timelines across
// workbench restarts. In-flight work is marked interrupted during recovery;
// stage artifacts remain untouched and the user can retry against the latest
// project revision.
func NewPersistentManager(applyResult func(projectID string, jobType string) (int, []string, error), statePath string) *Manager {
	m := &Manager{
		jobs:        map[string]*jobState{},
		applyResult: applyResult,
		statePath:   statePath,
	}
	m.load()
	return m
}

func (m *Manager) Start(projectID, jobType string, steps []string) Job {
	return m.StartWithRevision(projectID, jobType, 0, steps, nil)
}

func (m *Manager) StartWithRunner(projectID, jobType string, steps []string, runner Runner) Job {
	return m.StartWithRevision(projectID, jobType, 0, steps, runner)
}

func (m *Manager) StartWithRevision(projectID, jobType string, inputRevision int, steps []string, runner Runner) Job {
	now := time.Now()
	m.mu.Lock()
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

	go m.run(id, steps, runner)
	return job
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
	return m.StartWithRevision(projectID, jobType, inputRevision, steps, runner), nil
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

func (m *Manager) HasActiveProject(projectID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.jobs {
		if state.projectID == projectID && !terminal(state.job.Status) {
			return true
		}
	}
	return false
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

func (m *Manager) run(id string, steps []string, runner Runner) {
	m.update(id, StatusRunning, "", "Job started.", 2, 0, nil, nil)
	if runner != nil {
		m.mu.Lock()
		state := m.jobs[id]
		projectID := state.projectID
		jobType := state.job.Type
		m.mu.Unlock()

		emit := func(step string, message string, progress int, metadata map[string]any) {
			m.update(id, StatusRunning, step, message, progress, 0, nil, metadata)
		}
		revision, updated, err := runner(projectID, jobType, emit)
		if err != nil {
			m.update(id, StatusFailed, "", err.Error(), 100, 0, nil, nil)
			return
		}
		m.update(id, StatusCompleted, "", "Job completed.", 100, revision, updated, nil)
		return
	}
	if len(steps) == 0 {
		steps = []string{"run"}
	}
	for i, step := range steps {
		progress := 10 + int(float64(i)/float64(len(steps))*75)
		m.update(id, StatusRunning, step, humanStep(step), progress, 0, nil, nil)
		time.Sleep(180 * time.Millisecond)
	}

	m.mu.Lock()
	state := m.jobs[id]
	projectID := state.projectID
	jobType := state.job.Type
	m.mu.Unlock()

	revision, updated, err := m.applyResult(projectID, jobType)
	if err != nil {
		m.update(id, StatusFailed, "", err.Error(), 100, 0, nil, nil)
		return
	}
	m.update(id, StatusCompleted, "", "Job completed.", 100, revision, updated, nil)
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
		m.jobs[job.ID] = &jobState{job: job, projectID: job.ProjectID, events: eventsByJob[job.ID], subscribers: map[chan Event]bool{}}
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

func humanStep(step string) string {
	switch step {
	case "load_extracted_resources":
		return "Loading extracted resources."
	case "write_source_manifest":
		return "Writing source manifest."
	case "segment_source_sentences":
		return "Segmenting source sentences."
	case "validate_source_fidelity":
		return "Validating source fidelity."
	case "write_combined_document":
		return "Writing combined source document."
	case "detect_examples":
		return "Detecting structured examples."
	case "create_source_units":
		return "Creating source units."
	case "extract_requirements":
		return "Extracting requirement atoms."
	case "build_functional_crud":
		return "Building functional and CRUD analysis."
	case "create_review_candidates":
		return "Creating review questions."
	case "apply_review_decision":
		return "Applying review decision."
	case "refresh_analysis":
		return "Refreshing affected analysis artifacts."
	case "validate":
		return "Running validation."
	case "lint":
		return "Running lint checks."
	case "build_model_graph":
		return "Building model graph."
	case "build_trace_index":
		return "Building trace index."
	case "generate_dbml":
		return "Generating DBML."
	default:
		return "Running " + step + "."
	}
}
