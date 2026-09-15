package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/dsl"
)

func TestLintV02CleanModel(t *testing.T) {
	modelPath := writeLintFixture(t, baseLintReviewedSource(), baseLintModelV02())

	result := LintFile(modelPath)
	if len(result.Issues) != 0 {
		t.Fatalf("expected no lint issues, got:\n%s", formatIssues(result.Issues))
	}
}

func TestLintRejectsNonV02Model(t *testing.T) {
	model := strings.Replace(baseLintModelV02(), `version: "0.2"`, `version: "0.1"`, 1)
	modelPath := writeLintFixture(t, baseLintReviewedSource(), model)

	result := LintFile(modelPath)
	assertHasIssue(t, result, "DBDSL_V02_VERSION")
}

func TestLintStateMachineNeedsCandidateOrReview(t *testing.T) {
	source := strings.Replace(baseLintReviewedSource(), "state_machine_candidate", "attribute_candidate", 1)
	source = strings.Replace(source, "state_machines:", "attributes:", 1)
	modelPath := writeLintFixture(t, source, baseLintModelV02())

	result := LintFile(modelPath)
	assertHasIssue(t, result, "DBDSL_V02_SM001")
}

func TestLintDerivedViewNeedsDataShape(t *testing.T) {
	source := strings.Replace(baseLintReviewedSource(), "derived_view_candidate", "scope_boundary", 1)
	source = strings.Replace(source, "derived_views:", "entities:", 1)
	model := strings.Replace(baseLintModelV02(), "metrics: [user_count]", "metrics: []", 1)
	modelPath := writeLintFixture(t, source, model)

	result := LintFile(modelPath)
	assertHasIssue(t, result, "DBDSL_V02_DV001")
	assertHasIssue(t, result, "DBDSL_V02_DV002")
}

func TestLintFileSpecNeedsFileEvidence(t *testing.T) {
	source := strings.Replace(baseLintReviewedSource(), "file_spec_candidate", "attribute_candidate", 1)
	source = strings.Replace(source, "file_specs:", "attributes:", 1)
	model := strings.Replace(baseLintModelV02(), "type: file_path", "type: url", 1)
	model = strings.Replace(model, "storage: path", "storage: url", 1)
	modelPath := writeLintFixture(t, source, model)

	result := LintFile(modelPath)
	assertHasIssue(t, result, "DBDSL_V02_FS001")
	assertHasIssue(t, result, "DBDSL_V02_FS003")
}

func TestLintCheckConstraintNeedsSpecificEvidence(t *testing.T) {
	source := strings.Replace(baseLintReviewedSource(), "constraint_candidate", "attribute_candidate", 1)
	source = strings.Replace(source, "constraints:", "attributes:", 1)
	modelPath := writeLintFixture(t, source, baseLintModelV02())

	result := LintFile(modelPath)
	assertHasIssue(t, result, "DBDSL_V02_CK001")
}

func TestLintIncludedFragmentNeedsDerivedCandidates(t *testing.T) {
	source := strings.Replace(baseLintReviewedSource(), "derived_candidates:\n      state_machines:\n        - name: UserStatusFlow", "derived_candidates: {}", 1)
	modelPath := writeLintFixture(t, source, baseLintModelV02())

	result := LintFile(modelPath)
	assertHasIssue(t, result, "DBDSL_V02_SRC001")
}

func TestLintV05ReportsNativeWarnings(t *testing.T) {
	result := LintV05(&dsl.V05Bundle{
		Document: &dsl.Document{
			DSL:   dsl.DSLMeta{Name: "DB-DSL", Version: "0.5"},
			Model: dsl.ModelInfo{ID: "test_v05", Name: "Test v0.5"},
			Entities: []dsl.Entity{
				{
					ID:        "Product",
					Label:     "Product",
					TableName: "products",
					Kind:      "regular",
					Attributes: []dsl.Attribute{
						{ID: "image_path", Type: "file_path"},
					},
				},
			},
			DerivedViews: []dsl.DerivedView{
				{ID: "ProductSearch", Label: "Product search", Kind: "projection", Sources: []string{"Product"}, Persistence: "virtual"},
			},
			FileSpecs: []dsl.FileSpec{
				{ID: "ProductImageFile", Owner: "Product", Field: "image_path", Storage: "path", AllowedExtensions: []string{"jpg"}},
			},
		},
		RequirementAtoms: &dsl.V05RequirementAtomsFile{},
		ReviewDecisions:  &dsl.V05ReviewDecisionsFile{},
		CRUDMatrix:       &dsl.V05CRUDMatrixFile{},
	})

	if result.Version != VersionV05 {
		t.Fatalf("expected lint version %s, got %s", VersionV05, result.Version)
	}
	assertHasIssue(t, result, "DBDSL_V05_DV002")
	assertHasIssue(t, result, "DBDSL_V05_FS004")
}

func writeLintFixture(t *testing.T, sourceYAML, modelYAML string) string {
	t.Helper()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "reviewed_source_fragments.yaml")
	modelPath := filepath.Join(dir, "db_model.dsl.yaml")

	if err := os.WriteFile(sourcePath, []byte(sourceYAML), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	modelYAML = strings.ReplaceAll(modelYAML, "__SOURCE_PATH__", sourcePath)
	if err := os.WriteFile(modelPath, []byte(modelYAML), 0o600); err != nil {
		t.Fatalf("write model fixture: %v", err)
	}

	return modelPath
}

func assertHasIssue(t *testing.T, result Result, code string) {
	t.Helper()

	for _, issue := range result.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected lint issue %s, got:\n%s", code, formatIssues(result.Issues))
}

func formatIssues(issues []Issue) string {
	var lines []string
	for _, issue := range issues {
		lines = append(lines, issue.Severity+" "+issue.Code+" "+issue.Element+": "+issue.Message)
	}
	return strings.Join(lines, "\n")
}

func baseLintReviewedSource() string {
	return `
review_state:
  status: simulated_review_complete
  all_required_reviews_resolved: true
  unresolved_requires_review_flags: 0
fragments:
  - id: PH-SM-001
    types: [state_machine_candidate]
    decision: include
    phase_2_eligibility: eligible
    derived_candidates:
      state_machines:
        - name: UserStatusFlow
    evidence:
      support_level: explicit
      confidence: high
  - id: PH-DV-001
    types: [derived_view_candidate]
    decision: include
    phase_2_eligibility: eligible
    derived_candidates:
      derived_views:
        - name: UserStats
    evidence:
      support_level: explicit
      confidence: high
  - id: PH-FS-001
    types: [file_spec_candidate]
    decision: include
    phase_2_eligibility: eligible
    derived_candidates:
      file_specs:
        - name: UserProfileImageFile
    evidence:
      support_level: explicit
      confidence: high
  - id: PH-CK-001
    types: [constraint_candidate]
    decision: include
    phase_2_eligibility: eligible
    derived_candidates:
      constraints:
        - owner: UserAccount
          type: check
    evidence:
      support_level: explicit
      confidence: high
review_items:
  - id: PH-REV-001
    decision:
      status: accepted
      selected_option: accepted_option
`
}

func baseLintModelV02() string {
	return `
dsl:
  name: "DB-DSL"
  version: "0.2"
model:
  id: lint_model_v02
  name: Lint Model v0.2
  status: draft_model
  description: Test lint model.
source:
  reviewed_fragments_file: "__SOURCE_PATH__"
  review_state: simulated_review_complete
  accepted_review_decisions:
    - review_id: PH-REV-001
      selected_option: accepted_option
entities:
  - id: UserAccount
    label: User account
    description: User account.
    table_name: user_accounts
    kind: regular
    evidence:
      fragments: [PH-SM-001]
      review_decisions: []
      support_level: explicit
      confidence: high
    attributes:
      - id: status
        label: Status
        description: Account status.
        type: string
        required: true
        enum_values: [pending, active, disabled]
        evidence:
          fragments: [PH-SM-001]
          review_decisions: []
          support_level: explicit
          confidence: high
      - id: profile_image_path
        label: Profile image path
        description: Profile image path.
        type: file_path
        required: false
        evidence:
          fragments: [PH-FS-001]
          review_decisions: []
          support_level: explicit
          confidence: high
relationships: []
constraints:
  - id: user_status_not_blank
    type: check
    owner: UserAccount
    expression: "status != ''"
    description: Status cannot be blank.
    evidence:
      fragments: [PH-CK-001]
      review_decisions: []
      support_level: explicit
      confidence: high
import_specs: []
state_machines:
  - id: UserStatusFlow
    owner: UserAccount
    field: status
    states: [pending, active, disabled]
    initial: pending
    terminal: [disabled]
    transitions:
      - from: pending
        to: active
      - from: active
        to: disabled
    evidence:
      fragments: [PH-SM-001]
      review_decisions: []
      support_level: explicit
      confidence: high
derived_views:
  - id: UserStats
    label: User stats
    description: User aggregate stats.
    kind: aggregate
    sources: [UserAccount]
    persistence: virtual
    metrics: [user_count]
    evidence:
      fragments: [PH-DV-001]
      review_decisions: []
      support_level: explicit
      confidence: high
file_specs:
  - id: UserProfileImageFile
    owner: UserAccount
    field: profile_image_path
    allowed_extensions: [.jpg, .png]
    max_size_mb: 5
    storage: path
    evidence:
      fragments: [PH-FS-001]
      review_decisions: []
      support_level: explicit
      confidence: high
`
}
