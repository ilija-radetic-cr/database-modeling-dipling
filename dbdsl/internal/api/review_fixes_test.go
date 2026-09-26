package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F01: gate decisions wait for the project: while a job or another change
// holds it, acceptance is refused instead of racing the writer.
func TestModelAcceptanceIsRefusedWhileProjectIsBusy(t *testing.T) {
	server := newTestServer(t)
	project, err := server.store.CreateProject("Busy", "", "en", "busy")
	if err != nil {
		t.Fatal(err)
	}
	release, err := server.jobs.Reserve(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"base_revision":%d}`, project.CurrentRevision)
	for _, path := range []string{"model-acceptance", "complete"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+project.ID+"/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "project_busy") {
			t.Fatalf("%s on a busy project: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	release()
	if after, _ := server.store.Project(project.ID); after.CurrentRevision != project.CurrentRevision {
		t.Fatalf("busy project changed: %d -> %d", project.CurrentRevision, after.CurrentRevision)
	}
}
