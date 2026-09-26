package jobs

import (
	"errors"
	"path/filepath"
	"testing"
)

// A reservation and a single-flight job exclude each other on one project and
// leave other projects free.
func TestReservationAndSingleFlightExcludeEachOther(t *testing.T) {
	m := NewPersistentManager(filepath.Join(t.TempDir(), "jobs.json"))
	release, err := m.Reserve("project_1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Reserve("project_1"); !errors.Is(err, ErrProjectBusy) {
		t.Fatalf("second reservation = %v, want ErrProjectBusy", err)
	}
	noop := func(string, string, int, StepEmitter) (int, []string, error) { return 2, nil, nil }
	if _, err := m.StartSingleFlightWithRevision("project_1", "quality", 1, nil, noop); !errors.Is(err, ErrProjectBusy) {
		t.Fatalf("job started on a reserved project: %v", err)
	}
	other, err := m.Reserve("project_2")
	if err != nil {
		t.Fatalf("another project was blocked: %v", err)
	}
	other()
	release()
	release() // releasing twice is harmless

	block := make(chan struct{})
	job, err := m.StartSingleFlightWithRevision("project_1", "quality", 1, nil, func(string, string, int, StepEmitter) (int, []string, error) {
		<-block
		return 2, nil, nil
	})
	if err != nil {
		t.Fatalf("job after release: %v", err)
	}
	if _, err := m.Reserve("project_1"); !errors.Is(err, ErrProjectBusy) {
		t.Fatalf("reservation during an active job = %v, want ErrProjectBusy", err)
	}
	close(block)
	waitForStatus(t, m, job.ID, StatusCompleted)
	release, err = m.Reserve("project_1")
	if err != nil {
		t.Fatalf("reservation after the job finished: %v", err)
	}
	release()
}
