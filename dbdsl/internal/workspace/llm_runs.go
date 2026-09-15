package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dbdsl/internal/llmpipeline"
)

type LLMRunSummary struct {
	ID string `json:"id"`
	llmpipeline.RunSummary
}

type LLMRunDetails struct {
	Summary    LLMRunSummary `json:"summary"`
	Request    any           `json:"request,omitempty"`
	Parsed     any           `json:"parsed_response,omitempty"`
	Validation any           `json:"validation,omitempty"`
	Raw        string        `json:"raw_response,omitempty"`
}

func (s *Store) LLMRuns(projectID string) ([]LLMRunSummary, error) {
	if _, ok := s.Project(projectID); !ok {
		return nil, ErrNotFound
	}
	root := filepath.Join(s.projectWorkspaceDir(projectID), "llm_runs")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []LLMRunSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]LLMRunSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var summary llmpipeline.RunSummary
		if readJSON(filepath.Join(root, entry.Name(), "run.json"), &summary) != nil {
			continue
		}
		items = append(items, LLMRunSummary{ID: entry.Name(), RunSummary: summary})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	return items, nil
}

func (s *Store) LLMRun(projectID, runID string, includeRaw bool) (LLMRunDetails, error) {
	if strings.TrimSpace(runID) == "" || filepath.Base(runID) != runID || strings.Contains(runID, "..") {
		return LLMRunDetails{}, errors.New("invalid run id")
	}
	items, err := s.LLMRuns(projectID)
	if err != nil {
		return LLMRunDetails{}, err
	}
	var selected *LLMRunSummary
	for i := range items {
		if items[i].ID == runID {
			selected = &items[i]
			break
		}
	}
	if selected == nil {
		return LLMRunDetails{}, ErrNotFound
	}
	details := LLMRunDetails{Summary: *selected}
	runDir := filepath.Join(s.projectWorkspaceDir(projectID), "llm_runs", runID)
	details.Request = readJSONValue(filepath.Join(runDir, "request.json"))
	details.Parsed = readJSONValue(filepath.Join(runDir, "response.parsed.json"))
	details.Validation = readJSONValue(filepath.Join(runDir, "validation_report.json"))
	if includeRaw {
		details.Raw = readString(filepath.Join(runDir, "response.raw.txt"))
	}
	return details, nil
}

func readJSONValue(path string) any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	return value
}
