package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCheckFreezePackage(t *testing.T) {
	manifestPath, taskPath := writeFreezeFixture(t)
	result, err := CheckFreezePackage(manifestPath)
	if err != nil {
		t.Fatalf("CheckFreezePackage failed: %v", err)
	}
	if result.Domains != 1 || result.CoveredLines != 2 || result.NonEmptyLines != 2 || result.Obligations != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}

	if err := os.WriteFile(taskPath, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckFreezePackage(manifestPath); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
}

func TestCheckLedgerRejectsUncoveredNonEmptyLine(t *testing.T) {
	taskPath := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(taskPath, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact := FreezeArtifact{Path: "task.md", SHA256: fileHash(t, taskPath)}
	ledger := ObligationLedger{
		SchemaVersion: 1, Domain: "sample", CoveragePolicy: "every_non_empty_line", Source: artifact,
		Coverage:    []CoverageRange{{ID: "R1", LineStart: 1, LineEnd: 1, Disposition: "modeled", Obligations: []string{"O1"}, Rationale: "first"}},
		Obligations: []SemanticObligation{{ID: "O1", Statement: "one", ExpectedRealization: "one", SourceRanges: []string{"R1"}, Status: "realized"}},
	}
	_, _, _, err := checkLedger("sample", taskPath, artifact, ledger)
	if err == nil || !strings.Contains(err.Error(), "uncovered") {
		t.Fatalf("expected uncovered-line error, got %v", err)
	}
}

func TestCheckAuditRejectsUnresolvedCriticalFinding(t *testing.T) {
	referencePath := filepath.Join(t.TempDir(), "model.yaml")
	if err := os.WriteFile(referencePath, []byte("model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact := FreezeArtifact{Path: "model.yaml", SHA256: fileHash(t, referencePath)}
	rubric := FreezeRubric{ID: "r1", Dimensions: []RubricDimension{{ID: "d1", Weight: 100}}}
	audit := ReferenceAudit{
		SchemaVersion: 1, Domain: "sample", RubricID: "r1", AssessmentKind: "ai_assisted_pre_review",
		Candidate: artifact, Dimensions: []AuditDimension{{ID: "d1", Score: 4, Evidence: []string{"e"}}}, WeightedScore: 100,
		Findings:           []AuditFinding{{ID: "F1", Severity: "critical", Disposition: "unresolved", Summary: "broken"}},
		UnresolvedCritical: 1, Recommendation: FreezeReadyStatus,
	}
	if err := checkAudit("sample", referencePath, artifact, rubric, audit, FreezeStagePreparation, FreezeReadyStatus); err == nil || !strings.Contains(err.Error(), "unresolved critical") {
		t.Fatalf("expected unresolved-critical error, got %v", err)
	}
}

func TestSecureJoinRejectsTraversal(t *testing.T) {
	if _, err := secureJoin(t.TempDir(), "../outside"); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestCheckSignoffSeparatesAIAndHumanAuthority(t *testing.T) {
	falseValue, trueValue := false, true
	ai, identity, reviewedAt := "ai", "ai_simulated_database_expert", "2026-09-14T16:40:00+02:00"
	internalVerdict, referenceHash := "internally_approved", "sha256:abc"
	signoff := ExpertSignoff{
		SchemaVersion: 1, Domain: "sample", Status: FreezeAIStatus,
		ReviewerKind: &ai, ReviewerIdentity: &identity, HumanExpertReviewed: &falseValue,
		ReviewedAt: &reviewedAt, Verdict: &internalVerdict, ConfirmedReferenceSHA256: &referenceHash,
	}
	if err := checkSignoff("sample", referenceHash, signoff, FreezeStageAIInternal); err != nil {
		t.Fatalf("valid AI signoff rejected: %v", err)
	}
	signoff.HumanExpertReviewed = &trueValue
	if err := checkSignoff("sample", referenceHash, signoff, FreezeStageAIInternal); err == nil {
		t.Fatal("AI signoff must not claim human review")
	}
	if err := checkSignoff("sample", referenceHash, signoff, FreezeStageHumanSigned); err == nil {
		t.Fatal("AI identity must not pass the human-signed stage")
	}
}

func TestCheckHumanSignoffRequiresMatchingHash(t *testing.T) {
	trueValue := true
	human, identity, name, affiliation := "human", "reviewer-1", "Reviewer", "Faculty"
	reviewedAt, verdict, wrongHash := "2026-09-14", "approve", "sha256:wrong"
	signoff := ExpertSignoff{
		SchemaVersion: 1, Domain: "sample", Status: FreezeHumanStatus,
		ReviewerKind: &human, ReviewerIdentity: &identity, HumanExpertReviewed: &trueValue,
		ReviewerName: &name, ReviewerAffiliation: &affiliation, ReviewedAt: &reviewedAt,
		Verdict: &verdict, ConfirmedReferenceSHA256: &wrongHash,
	}
	if err := checkSignoff("sample", "sha256:expected", signoff, FreezeStageHumanSigned); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
}

func TestCheckReferenceElementsRejectsMissingElement(t *testing.T) {
	modelPath := filepath.Join(t.TempDir(), "model.yaml")
	if err := os.WriteFile(modelPath, []byte("entities:\n- id: Present\n  attributes: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger := ObligationLedger{Obligations: []SemanticObligation{{ID: "O1", Status: "realized", ReferenceElements: []string{"Missing"}}}}
	if err := checkReferenceElements(modelPath, ledger); err == nil || !strings.Contains(err.Error(), "missing model element") {
		t.Fatalf("expected missing-element error, got %v", err)
	}
}

func TestCheckCandidateManifestVerifiesNestedArtifacts(t *testing.T) {
	root := t.TempDir()
	falseValue := false
	modelPath := filepath.Join(root, "db_model.dsl.yaml")
	if err := os.WriteFile(modelPath, []byte("entities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "candidate_manifest.yaml")
	manifest := CandidateManifest{
		SchemaVersion: 1, Domain: "sample", Revision: "v2", Status: FreezeAIStatus,
		ReviewAuthority: "ai", HumanExpertReviewed: &falseValue,
		Artifacts: []FreezeArtifact{{Path: "db_model.dsl.yaml", SHA256: fileHash(t, modelPath)}},
	}
	writeYAML(t, manifestPath, manifest)
	if count, err := checkCandidateManifest(manifestPath, "sample"); err != nil || count != 1 {
		t.Fatalf("valid candidate manifest rejected: count=%d err=%v", count, err)
	}
	if err := os.WriteFile(modelPath, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := checkCandidateManifest(manifestPath, "sample"); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected nested hash mismatch, got %v", err)
	}
}

func writeFreezeFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	packageDir := filepath.Join(root, "evaluation", "reference_freeze", "fixture")
	domainDir := filepath.Join(packageDir, "sample")
	if err := os.MkdirAll(domainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(root, "task.md")
	referencePath := filepath.Join(root, "model.yaml")
	if err := os.WriteFile(taskPath, []byte("alpha\n\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(referencePath, []byte("entities:\n- id: Model\n  attributes: []\nrelationships: []\nconstraints: []\nstate_machines: []\nderived_views: []\nimport_specs: []\nfile_specs: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskArtifact := FreezeArtifact{Path: "task.md", SHA256: fileHash(t, taskPath)}
	referenceArtifact := FreezeArtifact{Path: "model.yaml", SHA256: fileHash(t, referencePath)}

	rubric := FreezeRubric{
		SchemaVersion: 1, ID: "r1", Purpose: "test", Scale: map[int]string{0: "zero", 1: "one", 2: "two", 3: "three", 4: "four"},
		Dimensions: []RubricDimension{{ID: "d1", Label: "dimension", Weight: 100}},
	}
	rubric.CriticalError = append(rubric.CriticalError, struct {
		ID          string `yaml:"id"`
		Description string `yaml:"description"`
	}{ID: "CE-1", Description: "critical"})
	rubricPath := filepath.Join(packageDir, "rubric.yaml")
	writeYAML(t, rubricPath, rubric)

	ledger := ObligationLedger{
		SchemaVersion: 1, Domain: "sample", ExtractionProtocol: "source_first", Source: taskArtifact, CoveragePolicy: "every_non_empty_line",
		Coverage:    []CoverageRange{{ID: "R1", LineStart: 1, LineEnd: 3, Disposition: "modeled", Obligations: []string{"O1"}, Rationale: "all"}},
		Obligations: []SemanticObligation{{ID: "O1", Statement: "facts", Category: "test", Severity: "major", SourceRanges: []string{"R1"}, ExpectedRealization: "model", ReferenceElements: []string{"Model"}, Status: "realized"}},
	}
	ledgerPath := filepath.Join(domainDir, "ledger.yaml")
	writeYAML(t, ledgerPath, ledger)

	audit := ReferenceAudit{
		SchemaVersion: 1, Domain: "sample", RubricID: "r1", AssessmentKind: "ai_assisted_pre_review", Candidate: referenceArtifact,
		Dimensions: []AuditDimension{{ID: "d1", Score: 4, Evidence: []string{"checked"}, Notes: "ok"}}, WeightedScore: 100,
		Findings: []AuditFinding{}, UnresolvedCritical: 0, Recommendation: FreezeReadyStatus,
	}
	auditPath := filepath.Join(domainDir, "audit.yaml")
	writeYAML(t, auditPath, audit)
	signoffPath := filepath.Join(domainDir, "signoff.yaml")
	writeYAML(t, signoffPath, ExpertSignoff{SchemaVersion: 1, Domain: "sample", Status: "unsigned", AcceptedExceptions: []string{}})

	manifest := FreezeManifest{
		SchemaVersion: 1, PackageID: "fixture", PreparedAt: "2026-09-14", Status: FreezeReadyStatus, WorkspaceRoot: "../../..",
		Rubric: FreezeArtifact{Path: "evaluation/reference_freeze/fixture/rubric.yaml", SHA256: fileHash(t, rubricPath)},
		Domains: []FreezeDomain{{
			ID: "sample", Status: FreezeReadyStatus, Task: taskArtifact, Reference: referenceArtifact,
			Ledger:  FreezeArtifact{Path: "evaluation/reference_freeze/fixture/sample/ledger.yaml", SHA256: fileHash(t, ledgerPath)},
			Audit:   FreezeArtifact{Path: "evaluation/reference_freeze/fixture/sample/audit.yaml", SHA256: fileHash(t, auditPath)},
			Signoff: FreezeArtifact{Path: "evaluation/reference_freeze/fixture/sample/signoff.yaml", SHA256: fileHash(t, signoffPath)},
		}},
	}
	manifestPath := filepath.Join(packageDir, "freeze_manifest.yaml")
	writeYAML(t, manifestPath, manifest)
	return manifestPath, taskPath
}

func writeYAML(t *testing.T, path string, value any) {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
