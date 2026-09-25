package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dbdsl/internal/dsl"

	"gopkg.in/yaml.v3"
)

type FreezeStage string

const (
	FreezeStagePreparation FreezeStage = "preparation"
	FreezeStageAIInternal  FreezeStage = "ai-internal"
	FreezeStageHumanSigned FreezeStage = "human-signed"
	FreezeReadyStatus                  = "ready_for_expert_signoff"
	FreezeAIStatus                     = "internally_frozen_ai_review"
	FreezeHumanStatus                  = "human_expert_frozen"
)

type FreezeArtifact struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

type FreezeDomain struct {
	ID                string           `yaml:"id"`
	Status            string           `yaml:"status"`
	Task              FreezeArtifact   `yaml:"task"`
	Reference         FreezeArtifact   `yaml:"reference"`
	Ledger            FreezeArtifact   `yaml:"obligation_ledger"`
	Audit             FreezeArtifact   `yaml:"audit"`
	Signoff           FreezeArtifact   `yaml:"signoff"`
	EvidenceArtifacts []FreezeArtifact `yaml:"evidence_artifacts"`
}

type FreezeManifest struct {
	SchemaVersion       int              `yaml:"schema_version"`
	PackageID           string           `yaml:"package_id"`
	PreparedAt          string           `yaml:"prepared_at"`
	Status              string           `yaml:"status"`
	WorkspaceRoot       string           `yaml:"workspace_root"`
	Rubric              FreezeArtifact   `yaml:"rubric"`
	SupportingDocuments []FreezeArtifact `yaml:"supporting_documents"`
	Domains             []FreezeDomain   `yaml:"domains"`
}

type CandidateManifest struct {
	SchemaVersion       int              `yaml:"schema_version"`
	Domain              string           `yaml:"domain"`
	Revision            string           `yaml:"revision"`
	Status              string           `yaml:"status"`
	ReviewAuthority     string           `yaml:"review_authority"`
	HumanExpertReviewed *bool            `yaml:"human_expert_reviewed"`
	Artifacts           []FreezeArtifact `yaml:"artifacts"`
}

type RubricDimension struct {
	ID     string `yaml:"id"`
	Label  string `yaml:"label"`
	Weight int    `yaml:"weight"`
}

type FreezeRubric struct {
	SchemaVersion int               `yaml:"schema_version"`
	ID            string            `yaml:"id"`
	Purpose       string            `yaml:"purpose"`
	Scale         map[int]string    `yaml:"scale"`
	Dimensions    []RubricDimension `yaml:"dimensions"`
	CriticalError []struct {
		ID          string `yaml:"id"`
		Description string `yaml:"description"`
	} `yaml:"critical_error_taxonomy"`
	DecisionRule map[string]string `yaml:"decision_rule"`
}

type CoverageRange struct {
	ID          string   `yaml:"id"`
	LineStart   int      `yaml:"line_start"`
	LineEnd     int      `yaml:"line_end"`
	Disposition string   `yaml:"disposition"`
	Obligations []string `yaml:"obligations"`
	Rationale   string   `yaml:"rationale"`
}

type SemanticObligation struct {
	ID                     string   `yaml:"id"`
	Statement              string   `yaml:"statement"`
	Category               string   `yaml:"category"`
	Severity               string   `yaml:"severity"`
	SourceRanges           []string `yaml:"source_ranges"`
	ExpectedRealization    string   `yaml:"expected_realization"`
	AcceptableAlternatives []string `yaml:"acceptable_alternatives"`
	ReferenceElements      []string `yaml:"reference_elements"`
	Status                 string   `yaml:"status"`
}

type ObligationLedger struct {
	SchemaVersion      int                  `yaml:"schema_version"`
	Domain             string               `yaml:"domain"`
	ExtractionProtocol string               `yaml:"extraction_protocol"`
	Source             FreezeArtifact       `yaml:"source"`
	CoveragePolicy     string               `yaml:"coverage_policy"`
	Coverage           []CoverageRange      `yaml:"coverage"`
	Obligations        []SemanticObligation `yaml:"obligations"`
}

type AuditDimension struct {
	ID       string   `yaml:"id"`
	Score    int      `yaml:"score"`
	Evidence []string `yaml:"evidence"`
	Notes    string   `yaml:"notes"`
}

type AuditFinding struct {
	ID          string `yaml:"id"`
	Severity    string `yaml:"severity"`
	Disposition string `yaml:"disposition"`
	Summary     string `yaml:"summary"`
}

type ReferenceAudit struct {
	SchemaVersion      int              `yaml:"schema_version"`
	Domain             string           `yaml:"domain"`
	RubricID           string           `yaml:"rubric_id"`
	AssessmentKind     string           `yaml:"assessment_kind"`
	Candidate          FreezeArtifact   `yaml:"candidate"`
	Dimensions         []AuditDimension `yaml:"dimensions"`
	WeightedScore      float64          `yaml:"weighted_score"`
	Findings           []AuditFinding   `yaml:"findings"`
	UnresolvedCritical int              `yaml:"unresolved_critical"`
	Recommendation     string           `yaml:"recommendation"`
}

type ExpertSignoff struct {
	SchemaVersion            int      `yaml:"schema_version"`
	Domain                   string   `yaml:"domain"`
	Status                   string   `yaml:"status"`
	ReviewerKind             *string  `yaml:"reviewer_kind"`
	ReviewerIdentity         *string  `yaml:"reviewer_identity"`
	HumanExpertReviewed      *bool    `yaml:"human_expert_reviewed"`
	ReviewerName             *string  `yaml:"reviewer_name"`
	ReviewerAffiliation      *string  `yaml:"reviewer_affiliation"`
	ReviewedAt               *string  `yaml:"reviewed_at"`
	Verdict                  *string  `yaml:"verdict"`
	ConfirmedReferenceSHA256 *string  `yaml:"confirmed_reference_sha256"`
	AcceptedExceptions       []string `yaml:"accepted_exceptions"`
	Notes                    *string  `yaml:"notes"`
}

type FreezeCheckResult struct {
	PackageID      string
	Domains        int
	Artifacts      int
	NonEmptyLines  int
	CoveredLines   int
	Obligations    int
	WeightedScores map[string]float64
}

// CheckFreezePackageWithStage verifies preparation, transparent AI-internal,
// or real human-signed freeze packages without changing any artifact.
func CheckFreezePackageWithStage(manifestPath string, stage FreezeStage) (FreezeCheckResult, error) {
	expectedStatus, err := freezeStatusForStage(stage)
	if err != nil {
		return FreezeCheckResult{}, err
	}
	var manifest FreezeManifest
	if err := decodeYAMLStrict(manifestPath, &manifest); err != nil {
		return FreezeCheckResult{}, fmt.Errorf("manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return FreezeCheckResult{}, fmt.Errorf("manifest: unsupported schema_version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.PackageID) == "" {
		return FreezeCheckResult{}, fmt.Errorf("manifest: package_id is required")
	}
	if manifest.Status != expectedStatus {
		return FreezeCheckResult{}, fmt.Errorf("manifest: status must be %q for stage %q, got %q", expectedStatus, stage, manifest.Status)
	}
	manifestDir, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return FreezeCheckResult{}, err
	}
	if strings.TrimSpace(manifest.WorkspaceRoot) == "" || filepath.IsAbs(manifest.WorkspaceRoot) {
		return FreezeCheckResult{}, fmt.Errorf("manifest: workspace_root must be a non-empty relative path")
	}
	workspaceRoot, err := filepath.Abs(filepath.Join(manifestDir, manifest.WorkspaceRoot))
	if err != nil {
		return FreezeCheckResult{}, fmt.Errorf("manifest: workspace_root: %w", err)
	}
	manifestRelative, err := filepath.Rel(workspaceRoot, manifestDir)
	if err != nil || manifestRelative == ".." || strings.HasPrefix(manifestRelative, ".."+string(filepath.Separator)) {
		return FreezeCheckResult{}, fmt.Errorf("manifest: package must reside inside workspace_root")
	}

	result := FreezeCheckResult{PackageID: manifest.PackageID, WeightedScores: map[string]float64{}}
	rubricPath, err := verifyArtifact(workspaceRoot, manifest.Rubric)
	if err != nil {
		return result, fmt.Errorf("rubric: %w", err)
	}
	result.Artifacts++
	var rubric FreezeRubric
	if err := decodeYAMLStrict(rubricPath, &rubric); err != nil {
		return result, fmt.Errorf("rubric: %w", err)
	}
	if err := checkRubric(rubric); err != nil {
		return result, err
	}
	for _, artifact := range manifest.SupportingDocuments {
		if _, err := verifyArtifact(workspaceRoot, artifact); err != nil {
			return result, fmt.Errorf("supporting document: %w", err)
		}
		result.Artifacts++
	}

	seenDomains := map[string]bool{}
	for _, domain := range manifest.Domains {
		if domain.ID == "" || seenDomains[domain.ID] {
			return result, fmt.Errorf("manifest: domain id %q is empty or duplicated", domain.ID)
		}
		seenDomains[domain.ID] = true
		if domain.Status != expectedStatus {
			return result, fmt.Errorf("domain %s: status must be %q", domain.ID, expectedStatus)
		}

		taskPath, err := verifyArtifact(workspaceRoot, domain.Task)
		if err != nil {
			return result, fmt.Errorf("domain %s task: %w", domain.ID, err)
		}
		referencePath, err := verifyArtifact(workspaceRoot, domain.Reference)
		if err != nil {
			return result, fmt.Errorf("domain %s reference: %w", domain.ID, err)
		}
		ledgerPath, err := verifyArtifact(workspaceRoot, domain.Ledger)
		if err != nil {
			return result, fmt.Errorf("domain %s ledger: %w", domain.ID, err)
		}
		auditPath, err := verifyArtifact(workspaceRoot, domain.Audit)
		if err != nil {
			return result, fmt.Errorf("domain %s audit: %w", domain.ID, err)
		}
		signoffPath, err := verifyArtifact(workspaceRoot, domain.Signoff)
		if err != nil {
			return result, fmt.Errorf("domain %s signoff: %w", domain.ID, err)
		}
		result.Artifacts += 5
		for _, artifact := range domain.EvidenceArtifacts {
			artifactPath, err := verifyArtifact(workspaceRoot, artifact)
			if err != nil {
				return result, fmt.Errorf("domain %s evidence artifact: %w", domain.ID, err)
			}
			result.Artifacts++
			if filepath.Base(artifactPath) == "candidate_manifest.yaml" {
				count, err := checkCandidateManifest(artifactPath, domain.ID)
				if err != nil {
					return result, fmt.Errorf("domain %s candidate manifest: %w", domain.ID, err)
				}
				result.Artifacts += count
			}
		}

		var ledger ObligationLedger
		if err := decodeYAMLStrict(ledgerPath, &ledger); err != nil {
			return result, fmt.Errorf("domain %s ledger: %w", domain.ID, err)
		}
		lineCount, covered, obligations, err := checkLedger(domain.ID, taskPath, domain.Task, ledger)
		if err != nil {
			return result, err
		}
		result.NonEmptyLines += lineCount
		result.CoveredLines += covered
		result.Obligations += obligations
		if err := checkReferenceElements(referencePath, ledger); err != nil {
			return result, fmt.Errorf("domain %s ledger: %w", domain.ID, err)
		}

		var audit ReferenceAudit
		if err := decodeYAMLStrict(auditPath, &audit); err != nil {
			return result, fmt.Errorf("domain %s audit: %w", domain.ID, err)
		}
		if err := checkAudit(domain.ID, referencePath, domain.Reference, rubric, audit, stage, expectedStatus); err != nil {
			return result, err
		}
		result.WeightedScores[domain.ID] = audit.WeightedScore

		var signoff ExpertSignoff
		if err := decodeYAMLStrict(signoffPath, &signoff); err != nil {
			return result, fmt.Errorf("domain %s signoff: %w", domain.ID, err)
		}
		if err := checkSignoff(domain.ID, domain.Reference.SHA256, signoff, stage); err != nil {
			return result, err
		}
		result.Domains++
	}
	if result.Domains == 0 {
		return result, fmt.Errorf("manifest: at least one domain is required")
	}
	return result, nil
}

func freezeStatusForStage(stage FreezeStage) (string, error) {
	switch stage {
	case FreezeStagePreparation:
		return FreezeReadyStatus, nil
	case FreezeStageAIInternal:
		return FreezeAIStatus, nil
	case FreezeStageHumanSigned:
		return FreezeHumanStatus, nil
	default:
		return "", fmt.Errorf("unknown freeze stage %q", stage)
	}
}

func decodeYAMLStrict(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func secureJoin(root, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be a non-empty relative path")
	}
	joined := filepath.Clean(filepath.Join(root, rel))
	relative, err := filepath.Rel(root, joined)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace root: %s", rel)
	}
	return joined, nil
}

func verifyArtifact(root string, artifact FreezeArtifact) (string, error) {
	path, err := secureJoin(root, artifact.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	actual := "sha256:" + hex.EncodeToString(sum[:])
	if artifact.SHA256 != actual {
		return "", fmt.Errorf("hash mismatch for %s: expected %s, got %s", artifact.Path, artifact.SHA256, actual)
	}
	return path, nil
}

func checkCandidateManifest(path, domainID string) (int, error) {
	var manifest CandidateManifest
	if err := decodeYAMLStrict(path, &manifest); err != nil {
		return 0, err
	}
	if manifest.SchemaVersion != 1 || manifest.Domain != domainID || strings.TrimSpace(manifest.Revision) == "" {
		return 0, fmt.Errorf("schema/domain/revision mismatch")
	}
	if manifest.Status != FreezeAIStatus || manifest.ReviewAuthority != "ai" || manifest.HumanExpertReviewed == nil || *manifest.HumanExpertReviewed {
		return 0, fmt.Errorf("candidate must transparently identify the internal AI review state")
	}
	if len(manifest.Artifacts) == 0 {
		return 0, fmt.Errorf("artifacts must not be empty")
	}

	root := filepath.Dir(path)
	seen := map[string]bool{}
	hasModel := false
	for _, artifact := range manifest.Artifacts {
		clean := filepath.Clean(artifact.Path)
		if seen[clean] {
			return 0, fmt.Errorf("duplicated artifact %q", artifact.Path)
		}
		seen[clean] = true
		if filepath.Base(clean) == "db_model.dsl.yaml" {
			hasModel = true
		}
		if _, err := verifyArtifact(root, artifact); err != nil {
			return 0, err
		}
	}
	if !hasModel {
		return 0, fmt.Errorf("candidate model artifact is required")
	}
	return len(manifest.Artifacts), nil
}

func checkRubric(rubric FreezeRubric) error {
	if rubric.SchemaVersion != 1 || strings.TrimSpace(rubric.ID) == "" {
		return fmt.Errorf("rubric: schema_version 1 and id are required")
	}
	weight, seen := 0, map[string]bool{}
	for _, dimension := range rubric.Dimensions {
		if dimension.ID == "" || seen[dimension.ID] || dimension.Weight <= 0 {
			return fmt.Errorf("rubric: invalid or duplicated dimension %q", dimension.ID)
		}
		seen[dimension.ID] = true
		weight += dimension.Weight
	}
	if weight != 100 {
		return fmt.Errorf("rubric: dimension weights total %d, want 100", weight)
	}
	for score := 0; score <= 4; score++ {
		if strings.TrimSpace(rubric.Scale[score]) == "" {
			return fmt.Errorf("rubric: scale description for score %d is required", score)
		}
	}
	if len(rubric.CriticalError) == 0 {
		return fmt.Errorf("rubric: critical_error_taxonomy must not be empty")
	}
	return nil
}

func checkLedger(domainID, taskPath string, taskArtifact FreezeArtifact, ledger ObligationLedger) (int, int, int, error) {
	if ledger.SchemaVersion != 1 || ledger.Domain != domainID || ledger.CoveragePolicy != "every_non_empty_line" {
		return 0, 0, 0, fmt.Errorf("domain %s ledger: schema/domain/coverage policy mismatch", domainID)
	}
	if ledger.Source.Path != taskArtifact.Path || ledger.Source.SHA256 != taskArtifact.SHA256 {
		return 0, 0, 0, fmt.Errorf("domain %s ledger: source identity does not match manifest task", domainID)
	}
	taskData, err := os.ReadFile(taskPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("domain %s ledger: read task: %w", domainID, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(taskData), "\r\n", "\n"), "\n")
	rangeIDs, obligationIDs := map[string]bool{}, map[string]bool{}
	for _, obligation := range ledger.Obligations {
		if obligation.ID == "" || obligationIDs[obligation.ID] {
			return 0, 0, 0, fmt.Errorf("domain %s ledger: invalid or duplicated obligation %q", domainID, obligation.ID)
		}
		obligationIDs[obligation.ID] = true
		if strings.TrimSpace(obligation.Statement) == "" || strings.TrimSpace(obligation.ExpectedRealization) == "" || len(obligation.SourceRanges) == 0 {
			return 0, 0, 0, fmt.Errorf("domain %s ledger: obligation %s is incomplete", domainID, obligation.ID)
		}
		switch obligation.Status {
		case "realized", "justified_non_model", "needs_correction":
		default:
			return 0, 0, 0, fmt.Errorf("domain %s ledger: obligation %s has invalid status %q", domainID, obligation.ID, obligation.Status)
		}
	}
	covered := make([]bool, len(lines)+1)
	validDisposition := map[string]bool{"modeled": true, "derived": true, "application_only": true, "ui_only": true, "example": true, "metadata": true, "out_of_scope": true}
	for _, item := range ledger.Coverage {
		if item.ID == "" || rangeIDs[item.ID] || item.LineStart < 1 || item.LineEnd < item.LineStart || item.LineEnd > len(lines) {
			return 0, 0, 0, fmt.Errorf("domain %s ledger: invalid or duplicated coverage range %q", domainID, item.ID)
		}
		rangeIDs[item.ID] = true
		if !validDisposition[item.Disposition] || strings.TrimSpace(item.Rationale) == "" {
			return 0, 0, 0, fmt.Errorf("domain %s ledger: range %s has invalid disposition or empty rationale", domainID, item.ID)
		}
		if (item.Disposition == "modeled" || item.Disposition == "derived") && len(item.Obligations) == 0 {
			return 0, 0, 0, fmt.Errorf("domain %s ledger: range %s requires an obligation", domainID, item.ID)
		}
		for _, id := range item.Obligations {
			if !obligationIDs[id] {
				return 0, 0, 0, fmt.Errorf("domain %s ledger: range %s references unknown obligation %s", domainID, item.ID, id)
			}
		}
		for line := item.LineStart; line <= item.LineEnd; line++ {
			if strings.TrimSpace(lines[line-1]) != "" {
				covered[line] = true
			}
		}
	}
	for _, obligation := range ledger.Obligations {
		for _, id := range obligation.SourceRanges {
			if !rangeIDs[id] {
				return 0, 0, 0, fmt.Errorf("domain %s ledger: obligation %s references unknown range %s", domainID, obligation.ID, id)
			}
		}
	}
	nonEmpty, coveredCount := 0, 0
	var missing []int
	for line, content := range lines {
		if strings.TrimSpace(content) == "" {
			continue
		}
		nonEmpty++
		if covered[line+1] {
			coveredCount++
		} else {
			missing = append(missing, line+1)
		}
	}
	if len(missing) > 0 {
		sort.Ints(missing)
		return nonEmpty, coveredCount, len(ledger.Obligations), fmt.Errorf("domain %s ledger: uncovered non-empty task lines %v", domainID, missing)
	}
	return nonEmpty, coveredCount, len(ledger.Obligations), nil
}

func checkReferenceElements(referencePath string, ledger ObligationLedger) error {
	document, err := dsl.LoadDocument(referencePath)
	if err != nil {
		return fmt.Errorf("load reference model: %w", err)
	}
	elements := map[string]bool{
		"constraints": true, "file_specs": true, "entities": true, "relationships": true,
		"state_machines": true, "derived_views": true, "import_specs": true,
	}
	for _, entity := range document.Entities {
		elements[entity.ID] = true
		for _, attribute := range entity.Attributes {
			elements[entity.ID+"."+attribute.ID] = true
		}
	}
	for _, relationship := range document.Relationships {
		elements[relationship.ID] = true
	}
	for _, constraint := range document.Constraints {
		elements[constraint.ID] = true
	}
	for _, machine := range document.StateMachines {
		elements[machine.ID] = true
	}
	for _, view := range document.DerivedViews {
		elements[view.ID] = true
	}
	for _, spec := range document.ImportSpecs {
		elements[spec.ID] = true
	}
	for _, spec := range document.FileSpecs {
		elements[spec.ID] = true
	}
	for _, obligation := range ledger.Obligations {
		if obligation.Status == "realized" && len(obligation.ReferenceElements) == 0 {
			return fmt.Errorf("realized obligation %s has no reference_elements", obligation.ID)
		}
		for _, element := range obligation.ReferenceElements {
			if !elements[element] {
				return fmt.Errorf("obligation %s references missing model element %q", obligation.ID, element)
			}
		}
	}
	return nil
}

func checkAudit(domainID, referencePath string, referenceArtifact FreezeArtifact, rubric FreezeRubric, audit ReferenceAudit, stage FreezeStage, expectedStatus string) error {
	if audit.SchemaVersion != 1 || audit.Domain != domainID || audit.RubricID != rubric.ID {
		return fmt.Errorf("domain %s audit: schema/domain/rubric mismatch", domainID)
	}
	expectedAssessment := map[FreezeStage]string{
		FreezeStagePreparation: "ai_assisted_pre_review",
		FreezeStageAIInternal:  "ai_simulated_expert_review",
		FreezeStageHumanSigned: "human_expert_review",
	}[stage]
	if audit.AssessmentKind != expectedAssessment {
		return fmt.Errorf("domain %s audit: assessment_kind must be %q for stage %q", domainID, expectedAssessment, stage)
	}
	if audit.Candidate.Path != referenceArtifact.Path || audit.Candidate.SHA256 != referenceArtifact.SHA256 {
		return fmt.Errorf("domain %s audit: candidate identity does not match manifest reference", domainID)
	}
	if _, err := os.Stat(referencePath); err != nil {
		return fmt.Errorf("domain %s audit: reference unavailable: %w", domainID, err)
	}
	weights := map[string]int{}
	for _, dimension := range rubric.Dimensions {
		weights[dimension.ID] = dimension.Weight
	}
	seen, weighted := map[string]bool{}, 0.0
	for _, dimension := range audit.Dimensions {
		weight, ok := weights[dimension.ID]
		if !ok || seen[dimension.ID] || dimension.Score < 0 || dimension.Score > 4 || len(dimension.Evidence) == 0 {
			return fmt.Errorf("domain %s audit: invalid dimension %q", domainID, dimension.ID)
		}
		seen[dimension.ID] = true
		weighted += float64(weight*dimension.Score) / 4.0
	}
	if len(seen) != len(weights) {
		return fmt.Errorf("domain %s audit: expected %d rubric dimensions, got %d", domainID, len(weights), len(seen))
	}
	if abs(weighted-audit.WeightedScore) > 0.0001 {
		return fmt.Errorf("domain %s audit: weighted_score %.2f does not match calculated %.2f", domainID, audit.WeightedScore, weighted)
	}
	unresolved := 0
	for _, finding := range audit.Findings {
		if finding.Severity == "critical" && finding.Disposition == "unresolved" {
			unresolved++
		}
	}
	if audit.UnresolvedCritical != unresolved || unresolved != 0 {
		return fmt.Errorf("domain %s audit: unresolved critical findings=%d", domainID, unresolved)
	}
	if audit.Recommendation != expectedStatus {
		return fmt.Errorf("domain %s audit: recommendation must be %q", domainID, expectedStatus)
	}
	return nil
}

func checkSignoff(domainID, referenceSHA string, signoff ExpertSignoff, stage FreezeStage) error {
	if signoff.SchemaVersion != 1 || signoff.Domain != domainID {
		return fmt.Errorf("domain %s signoff: schema/domain mismatch", domainID)
	}
	switch stage {
	case FreezeStagePreparation:
		return checkUnsignedSignoff(domainID, signoff)
	case FreezeStageAIInternal:
		if signoff.Status != FreezeAIStatus || value(signoff.ReviewerKind) != "ai" || value(signoff.ReviewerIdentity) != "ai_simulated_database_expert" {
			return fmt.Errorf("domain %s signoff: AI-internal identity/status mismatch", domainID)
		}
		if signoff.HumanExpertReviewed == nil || *signoff.HumanExpertReviewed || signoff.ReviewerName != nil || signoff.ReviewerAffiliation != nil {
			return fmt.Errorf("domain %s signoff: AI review must not populate or claim human reviewer fields", domainID)
		}
		if value(signoff.Verdict) != "internally_approved" || value(signoff.ReviewedAt) == "" || value(signoff.ConfirmedReferenceSHA256) != referenceSHA {
			return fmt.Errorf("domain %s signoff: incomplete AI approval or candidate hash mismatch", domainID)
		}
		return nil
	case FreezeStageHumanSigned:
		if signoff.Status != FreezeHumanStatus || value(signoff.ReviewerKind) != "human" || signoff.HumanExpertReviewed == nil || !*signoff.HumanExpertReviewed {
			return fmt.Errorf("domain %s signoff: human-signed stage requires an explicit human reviewer", domainID)
		}
		if value(signoff.ReviewerName) == "" || value(signoff.ReviewerAffiliation) == "" || value(signoff.ReviewedAt) == "" || value(signoff.Verdict) != "approve" || value(signoff.ConfirmedReferenceSHA256) != referenceSHA {
			return fmt.Errorf("domain %s signoff: incomplete human approval or candidate hash mismatch", domainID)
		}
		return nil
	default:
		return fmt.Errorf("domain %s signoff: unknown stage %q", domainID, stage)
	}
}

func checkUnsignedSignoff(domainID string, signoff ExpertSignoff) error {
	if signoff.SchemaVersion != 1 || signoff.Domain != domainID || signoff.Status != "unsigned" {
		return fmt.Errorf("domain %s signoff: expected an unsigned schema_version 1 form", domainID)
	}
	if signoff.ReviewerKind != nil || signoff.ReviewerIdentity != nil || signoff.HumanExpertReviewed != nil || signoff.ReviewerName != nil || signoff.ReviewerAffiliation != nil || signoff.ReviewedAt != nil || signoff.Verdict != nil || signoff.ConfirmedReferenceSHA256 != nil || signoff.Notes != nil || len(signoff.AcceptedExceptions) != 0 {
		return fmt.Errorf("domain %s signoff: human-only fields must remain blank before expert review", domainID)
	}
	return nil
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return strings.TrimSpace(*pointer)
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
