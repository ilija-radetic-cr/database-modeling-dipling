package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitReturnsAfterTerminalStateIsPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	m := NewPersistentManager(path)
	job := m.StartWithRevision("project_1", "source_units", 4, nil, func(_ string, _ string, _ int, _ StepEmitter) (int, []string, error) {
		return 5, []string{"source_units"}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := m.Wait(ctx, job.ID)
	if err != nil || got.Status != StatusCompleted {
		t.Fatalf("wait: job=%+v err=%v", got, err)
	}
	reloaded := NewPersistentManager(path)
	persisted, ok := reloaded.Get(job.ID)
	if !ok || persisted.Status != StatusCompleted || persisted.OutputRevision != 5 {
		t.Fatalf("terminal state was not persisted before Wait returned: %+v found=%v", persisted, ok)
	}
}

func TestManagerPersistsCompletedTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	m := NewPersistentManager(path)
	job := m.StartWithRevision("project_1", "source_units", 4, []string{"extract"}, func(_ string, _ string, _ int, emit StepEmitter) (int, []string, error) {
		emit("extract", "Extracting.", 45, nil)
		return 5, []string{"source_units"}, nil
	})
	waitForStatus(t, m, job.ID, StatusCompleted)

	reloaded := NewPersistentManager(path)
	got, ok := reloaded.Get(job.ID)
	if !ok || got.Status != StatusCompleted || got.InputRevision != 4 || got.OutputRevision != 5 {
		t.Fatalf("unexpected reloaded job: %#v, found=%v", got, ok)
	}
	_, replay, ok := reloaded.Subscribe(job.ID)
	if !ok || len(replay) < 3 {
		t.Fatalf("expected persisted timeline, got %d events", len(replay))
	}
}

func TestManagerFailureCanRetryWithoutMutatingFirstJob(t *testing.T) {
	m := NewPersistentManager("")
	attempt := 0
	seenRevisions := make(chan int, 2)
	runner := func(_ string, _ string, inputRevision int, _ StepEmitter) (int, []string, error) {
		seenRevisions <- inputRevision
		attempt++
		if attempt == 1 {
			return 0, nil, errors.New("schema mismatch")
		}
		return 3, []string{"requirement_atoms"}, nil
	}
	first := m.StartWithRevision("project_1", "requirement_atoms", 2, nil, runner)
	waitForStatus(t, m, first.ID, StatusFailed)
	second, err := m.Retry(first.ID, 7)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	waitForStatus(t, m, second.ID, StatusCompleted)
	original, _ := m.Get(first.ID)
	if original.Status != StatusFailed || second.ID == first.ID {
		t.Fatalf("retry must create a distinct job; first=%#v second=%#v", original, second)
	}
	firstRevision, secondRevision := <-seenRevisions, <-seenRevisions
	if firstRevision != 2 || secondRevision != 7 {
		t.Fatalf("runner did not receive each job's declared input revision: %d, %d", firstRevision, secondRevision)
	}
}

func TestManagerMarksInflightJobInterruptedOnRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	m := NewPersistentManager(path)
	block := make(chan struct{})
	job := m.StartWithRevision("project_1", "logical_model", 8, nil, func(_ string, _ string, _ int, _ StepEmitter) (int, []string, error) {
		<-block
		return 9, nil, nil
	})
	waitForStatus(t, m, job.ID, StatusRunning)
	reloaded := NewPersistentManager(path)
	got, ok := reloaded.Get(job.ID)
	close(block)
	waitForStatus(t, m, job.ID, StatusCompleted)
	if !ok || got.Status != StatusInterrupted {
		t.Fatalf("expected interrupted recovery state, got %#v", got)
	}
}

func waitForStatus(t *testing.T, manager *Manager, id string, want Status) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := manager.Get(id); ok && job.Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, _ := manager.Get(id)
	t.Fatalf("job %s did not reach %s; got %#v", id, want, job)
}
