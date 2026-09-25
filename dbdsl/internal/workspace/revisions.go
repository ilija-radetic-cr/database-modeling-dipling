package workspace

import (
	"dbdsl/internal/dsl"
	"dbdsl/internal/jobs"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type artifactValue struct {
	Value any
	JSON  bool
	Raw   bool
}

func (s *Store) writeAnalysisRevision(project *ProjectState, artifacts map[string]artifactValue) (map[string]string, error) {
	revisionRel := s.projectRevisionRel(project.ID, project.CurrentRevision+1)
	if err := os.MkdirAll(s.absoluteWorkspacePath(revisionRel), 0o755); err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for name, artifact := range artifacts {
		var data []byte
		var err error
		if artifact.Raw {
			text, ok := artifact.Value.(string)
			if !ok {
				return nil, fmt.Errorf("raw artifact %s is not text", name)
			}
			data = []byte(text)
		} else if artifact.JSON {
			data, err = json.MarshalIndent(artifact.Value, "", "  ")
		} else {
			data, err = yaml.Marshal(artifact.Value)
		}
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
