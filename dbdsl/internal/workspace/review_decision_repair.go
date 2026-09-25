package workspace

import (
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// repairReviewDecisionPaths fixes projects whose ReviewDecisionsPath was
// redirected to the DB-DSL bundle export by final model acceptance (fixed in
// applyAcceptedBundlePaths). That export has no candidate IDs, so every
// answered review candidate looked open again after finalization. The path is
// pointed back at the newest workbench decision record of the same project.
func (s *Store) repairReviewDecisionPaths() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	repaired := false
	for _, project := range s.projects {
		if project.ReviewDecisionsPath == "" || !isBundleReviewDecisionsExport(s.absoluteWorkspacePath(project.ReviewDecisionsPath)) {
			continue
		}
		candidates, _ := filepath.Glob(filepath.Join(s.projectWorkspaceDir(project.ID), "revisions", "*", "review_decisions.yaml"))
		sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
		for _, candidate := range candidates {
			if !isBundleReviewDecisionsExport(candidate) {
				project.ReviewDecisionsPath = s.relativePath(candidate)
				repaired = true
				break
			}
		}
	}
	if !repaired {
		return nil
	}
	return s.saveLocked()
}

// isBundleReviewDecisionsExport recognizes the DB-DSL v0.5 review_decisions.yaml
// by its review_state section, which workbench decision records never contain.
func isBundleReviewDecisionsExport(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var document map[string]any
	if yaml.Unmarshal(data, &document) != nil {
		return false
	}
	_, exported := document["review_state"]
	return exported
}
