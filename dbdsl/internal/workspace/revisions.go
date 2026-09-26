package workspace

import (
	"dbdsl/internal/dsl"
	"dbdsl/internal/jobs"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

type artifactValue struct {
	Value any
	JSON  bool
	Raw   bool
}

func marshalArtifact(name string, artifact artifactValue) ([]byte, error) {
	switch {
	case artifact.Raw:
		switch value := artifact.Value.(type) {
		case string:
			return []byte(value), nil
		case []byte:
			return value, nil
		}
		return nil, fmt.Errorf("raw artifact %s is not text", name)
	case artifact.JSON:
		return json.MarshalIndent(artifact.Value, "", "  ")
	default:
		return yaml.Marshal(artifact.Value)
	}
}

// revisionStage collects the artifacts of one revision in a private directory.
// Nothing becomes visible in revisions/rev_N until publishRevision renames the
// whole directory under the store lock, after the base revision is confirmed.
// A writer that loses a race therefore never touches the winner's files, and
// a stage that fails leaves no stray files behind.
type revisionStage struct {
	store     *Store
	projectID string
	revision  int
	dir       string
	finalRel  string
	published bool
}

func (s *Store) newRevisionStage(projectID string, baseRevision int) (*revisionStage, error) {
	revision := baseRevision + 1
	parent := s.absoluteWorkspacePath(filepath.ToSlash(filepath.Join(s.projectWorkspaceRel(projectID), "revisions", ".staging")))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(parent, fmt.Sprintf("rev_%06d-", revision))
	if err != nil {
		return nil, err
	}
	// MkdirTemp creates a private directory; a published revision has the
	// same permissions as one written directly.
	if err := os.Chmod(dir, 0o755); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &revisionStage{store: s, projectID: projectID, revision: revision, dir: dir, finalRel: s.projectRevisionRel(projectID, revision)}, nil
}

// write stores the artifacts in the stage and returns their final relative
// paths, which become valid once the stage is published.
func (st *revisionStage) write(artifacts map[string]artifactValue) (map[string]string, error) {
	paths := map[string]string{}
	for name, artifact := range artifacts {
		data, err := marshalArtifact(name, artifact)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", name, err)
		}
		if err := writeAtomic(filepath.Join(st.dir, name), data); err != nil {
			return nil, err
		}
		paths[name] = filepath.ToSlash(filepath.Join(st.finalRel, name))
	}
	return paths, nil
}

// staged returns where a final relative path of this stage can be read before
// it is published.
func (st *revisionStage) staged(finalRel string) string {
	name, err := filepath.Rel(st.finalRel, finalRel)
	if err != nil {
		return st.store.absoluteWorkspacePath(finalRel)
	}
	return filepath.Join(st.dir, name)
}

func (st *revisionStage) discard() {
	if !st.published {
		_ = os.RemoveAll(st.dir)
	}
}

// publishRevision makes the stage the project's next revision. Under the store
// lock it confirms that nobody committed since the stage started, moves the
// stage into place and applies the state change; on a conflict the stage is
// discarded and the winner's revision is left untouched.
func (s *Store) publishRevision(st *revisionStage, apply func(*ProjectState) error) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer st.discard()
	project, ok := s.projects[st.projectID]
	if !ok {
		return 0, ErrNotFound
	}
	if project.CurrentRevision != st.revision-1 {
		return 0, ErrRevisionConflict
	}
	if err := projectMutable(project); err != nil {
		return 0, err
	}
	final := s.absoluteWorkspacePath(st.finalRel)
	// Only a stage that never published can have left this directory; no
	// published revision can be N+1 while the project is still at N.
	if err := os.RemoveAll(final); err != nil {
		return 0, err
	}
	if err := os.Rename(st.dir, final); err != nil {
		return 0, err
	}
	if err := apply(project); err != nil {
		_ = os.Rename(final, st.dir)
		return 0, err
	}
	project.CurrentRevision++
	project.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		return 0, err
	}
	st.published = true
	return project.CurrentRevision, nil
}

// writeAnalysisRevision writes directly into the next revision directory. It
// is only safe while the caller holds the store lock and has confirmed the
// base revision, as commitAnalysisRevision does.
func (s *Store) writeAnalysisRevision(project *ProjectState, artifacts map[string]artifactValue) (map[string]string, error) {
	revisionRel := s.projectRevisionRel(project.ID, project.CurrentRevision+1)
	if err := os.MkdirAll(s.absoluteWorkspacePath(revisionRel), 0o755); err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for name, artifact := range artifacts {
		data, err := marshalArtifact(name, artifact)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", name, err)
		}
		rel := filepath.ToSlash(filepath.Join(revisionRel, name))
		if err := writeAtomic(s.absoluteWorkspacePath(rel), data); err != nil {
			return nil, err
		}
		paths[name] = rel
	}
	return paths, nil
}

// commitAnalysisRevision serializes the revision check, artifact writes, and
// project-state update. This prevents two callers that started from the same
// revision from writing different contents to the winner's revision directory.
func (s *Store) commitAnalysisRevision(projectID string, baseRevision int, artifacts map[string]artifactValue, apply func(*ProjectState, map[string]string) error) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[projectID]
	if !ok {
		return 0, ErrNotFound
	}
	if baseRevision <= 0 || project.CurrentRevision != baseRevision {
		return 0, ErrRevisionConflict
	}
	if err := projectMutable(project); err != nil {
		return 0, err
	}
	if err := os.RemoveAll(s.absoluteWorkspacePath(s.projectRevisionRel(projectID, baseRevision+1))); err != nil {
		return 0, err
	}
	paths, err := s.writeAnalysisRevision(project, artifacts)
	if err != nil {
		return 0, err
	}
	if err := apply(project, paths); err != nil {
		return 0, err
	}
	project.CurrentRevision++
	project.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		return 0, err
	}
	return project.CurrentRevision, nil
}

func invalidateModelAndReview(project *ProjectState) {
	project.ModelGenerated = false
	project.DBMLReady = false
	project.Completed = false
	project.ModelPath = ""
	project.TaskPath = ""
	project.BundlePath = ""
	invalidateModelArtifacts(project)
}

func invalidateModelArtifacts(project *ProjectState) {
	project.ConceptualModelProposalPath = ""
	project.ConceptualModelAcceptedPath = ""
	project.ConceptualModelQAPath = ""
	project.ConceptualModelDiffPath = ""
	project.ConceptualDescriptionPath = ""
	project.LogicalPatchProposalPath = ""
	project.ValidationReportPath = ""
	project.LintReportPath = ""
	project.QualityReportPath = ""
	project.DBMLPath = ""
	project.TraceReportPath = ""
	project.FinalModelAccepted = false
	project.ModelGenerated = false
	project.DBMLReady = false
	project.ModelPath = ""
	project.BundlePath = ""
}

func stageEmitter(emit jobs.StepEmitter) jobs.StepEmitter {
	if emit == nil {
		return func(string, string, int, map[string]any) {}
	}
	return emit
}

func coverageMetadata(counts map[string]int) map[string]any {
	out := make(map[string]any, len(counts))
	for key, value := range counts {
		out[key] = value
	}
	return out
}

func mustAcceptedSourceUnits(s *Store, projectID string) []dsl.SourceUnit {
	artifacts, err := s.SourceUnitArtifacts(projectID)
	if err != nil {
		return nil
	}
	return artifacts.Accepted.SourceUnits
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
