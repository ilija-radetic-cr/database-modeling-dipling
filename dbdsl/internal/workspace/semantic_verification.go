package workspace

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
	"dbdsl/internal/jobs"
	"dbdsl/internal/llmpipeline"
	"dbdsl/internal/quality"
)

type ObligationRealization struct {
	ObligationID  string   `json:"obligation_id"`
	Status        string   `json:"status"`
	ModelElements []string `json:"model_elements"`
	Enforcement   string   `json:"enforcement"`
	Rationale     string   `json:"rationale"`
}

type SemanticIssue struct {
	ID            string   `json:"id"`
	Severity      string   `json:"severity"`
	Code          string   `json:"code"`
	Kind          string   `json:"kind"`
	ObligationID  string   `json:"obligation_id,omitempty"`
	Message       string   `json:"message"`
	ModelElements []string `json:"model_elements,omitempty"`
	Blocking      bool     `json:"blocking"`
}

type SemanticVerificationReport struct {
	Version             string                  `json:"version"`
	PipelineVersion     string                  `json:"pipeline_version"`
	OK                  bool                    `json:"ok"`
	ModelSHA256         string                  `json:"model_sha256"`
	ObligationsTotal    int                     `json:"obligations_total"`
	ObligationsRequired int                     `json:"obligations_required"`
	ObligationsRealized int                     `json:"obligations_realized"`
	BlockingIssues      int                     `json:"blocking_issues"`
	Realizations        []ObligationRealization `json:"realizations"`
	Issues              []SemanticIssue         `json:"issues"`
}

func (s *Store) RunSemanticVerification(projectID string, baseRevision int, emit jobs.StepEmitter) (int, []string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return 0, nil, ErrRevisionConflict
	}
	if !project.ModelGenerated || project.ModelPath == "" {
		return 0, nil, ErrModelNotGenerated
	}
	if project.DesignObligationsPath == "" {
		return 0, nil, errors.New("design obligations are not ready; rerun the v0.7 requirement stage")
	}
	emit = stageEmitter(emit)
	emit("map_obligations", "Mapping design obligations to model evidence.", 25, nil)
	bundle, err := dsl.LoadV05Bundle(project.ModelPath)
	if err != nil {
		return 0, nil, err
	}
	obligations, err := s.DesignObligations(projectID)
	if err != nil {
		return 0, nil, err
	}
	modelBytes, err := os.ReadFile(project.ModelPath)
	if err != nil {
		return 0, nil, err
	}
	report := verifySemanticObligations(*bundle.Document, obligations.Accepted.DesignObligations, sha256Hash(modelBytes))
	emit("verify_obligations", "Checking coverage, weak typing and fabricated fallback fields.", 70, map[string]any{
		"obligations": report.ObligationsTotal, "blocking_issues": report.BlockingIssues,
	})
	invariantReport := renderInvariantReport(report)
	qualityReport := quality.BuildReport(project.ModelPath, false)
	qualityReport.Summary.SemanticVerificationStatus = map[bool]string{true: "passed", false: "blocked"}[report.OK]
	qualityReport.Summary.SemanticBlockingIssues = report.BlockingIssues
	qualityReport.Summary.BlockingIssues += report.BlockingIssues
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"obligation_realizations.json":      {Value: report.Realizations, JSON: true},
		"semantic_verification_report.json": {Value: report, JSON: true},
		"invariant_report.md":               {Value: invariantReport, Raw: true},
		"quality_report.json":               {Value: qualityReport, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.ObligationRealizationsPath = paths["obligation_realizations.json"]
		current.SemanticVerificationPath = paths["semantic_verification_report.json"]
		current.InvariantReportPath = paths["invariant_report.md"]
		current.QualityReportPath = paths["quality_report.json"]
		current.FinalModelAccepted = false
		current.DBMLReady = false
		current.DBMLPath = ""
		current.TraceReportPath = ""
		if report.OK {
			current.LifecycleStatus = "semantic_review"
			current.LastActivity = "Semantic obligation verification passed; final human review is required."
		} else {
			current.LifecycleStatus = "semantic_repair_required"
			current.LastActivity = "Semantic obligation verification found blocking issues."
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"obligation_realizations", "semantic_verification", "invariant_report"}, nil
}

func (s *Store) SemanticVerification(projectID string) (SemanticVerificationReport, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return SemanticVerificationReport{}, ErrNotFound
	}
	if project.SemanticVerificationPath == "" {
		return SemanticVerificationReport{}, ErrNotFound
	}
	var report SemanticVerificationReport
	if err := readJSON(s.absoluteWorkspacePath(project.SemanticVerificationPath), &report); err != nil {
		return report, err
	}
	return report, nil
}

func verifySemanticObligations(model dsl.Document, obligations []llmpipeline.DesignObligation, modelHash string) SemanticVerificationReport {
	report := SemanticVerificationReport{Version: "1", PipelineVersion: "0.7", ModelSHA256: modelHash, ObligationsTotal: len(obligations)}
	byAtom := map[string][]string{}
	addEvidence := func(element string, evidence dsl.Evidence) {
		for _, atomID := range evidence.RequirementAtoms {
			byAtom[atomID] = append(byAtom[atomID], element)
		}
	}
	for _, entity := range model.Entities {
		addEvidence("entity:"+entity.ID, entity.Evidence)
		for _, attribute := range entity.Attributes {
			addEvidence("field:"+entity.ID+"."+attribute.ID, attribute.Evidence)
			if strings.Contains(strings.ToLower(attribute.Description), "fallback name attribute") {
				report.Issues = append(report.Issues, semanticIssue("fabricated_fallback_attribute", "critical", "fabricated_model_content", "Fallback attribute has no source-backed semantics.", []string{"field:" + entity.ID + "." + attribute.ID}, ""))
			}
		}
		attributeIDs := map[string]bool{}
		for _, attribute := range entity.Attributes {
			attributeIDs[attribute.ID] = true
		}
		if attributeIDs["fact_kind"] && attributeIDs["fact_value"] {
			report.Issues = append(report.Issues, semanticIssue("weakly_typed_fact_container", "high", "weakly_typed_model", "Generic fact_kind/fact_value storage hides domain semantics and prevents precise constraint verification.", []string{"entity:" + entity.ID}, ""))
		}
	}
	for _, relationship := range model.Relationships {
		addEvidence("relationship:"+relationship.ID, relationship.Evidence)
	}
	for _, constraint := range model.Constraints {
		addEvidence("constraint:"+constraint.ID, constraint.Evidence)
	}
	for _, stateMachine := range model.StateMachines {
		addEvidence("state_machine:"+stateMachine.ID, stateMachine.Evidence)
	}
	for _, view := range model.DerivedViews {
		addEvidence("derived_view:"+view.ID, view.Evidence)
	}
	for _, spec := range model.FileSpecs {
		addEvidence("file_spec:"+spec.ID, spec.Evidence)
	}
	for _, spec := range model.ImportSpecs {
		addEvidence("import_spec:"+spec.ID, spec.Evidence)
	}
	for _, obligation := range obligations {
		if obligation.Status == "not_required" || obligation.Persistence == "not_required" {
			report.Realizations = append(report.Realizations, ObligationRealization{ObligationID: obligation.ID, Status: "not_required", Enforcement: "excluded", Rationale: obligation.Rationale})
			continue
		}
		report.ObligationsRequired++
		elements := []string{}
		for _, atomID := range obligation.RequirementAtoms {
			elements = append(elements, byAtom[atomID]...)
		}
		elements = uniqueSorted(elements)
		compatible := compatibleRealizations(obligation.Kind, elements)
		if len(elements) == 0 || len(compatible) == 0 {
			rationale := "No model element cites the obligation's requirement atom."
			code := "unrealized_design_obligation"
			if len(elements) > 0 {
				rationale = "Evidence is cited, but no compatible model construct realizes the obligation kind " + obligation.Kind + "."
				code = "evidence_without_semantic_realization"
			}
			report.Realizations = append(report.Realizations, ObligationRealization{ObligationID: obligation.ID, Status: "missing", ModelElements: elements, Enforcement: "none", Rationale: rationale})
			report.Issues = append(report.Issues, semanticIssue("missing_"+obligation.ID, riskSeverity(obligation.Risk), code, obligation.Statement, elements, obligation.ID))
			continue
		}
		report.ObligationsRealized++
		status := "represented"
		if obligation.Persistence == "derived" || obligation.Kind == "derived_view" {
			status = "derived"
		}
		report.Realizations = append(report.Realizations, ObligationRealization{ObligationID: obligation.ID, Status: status, ModelElements: compatible, Enforcement: obligation.VerificationTarget, Rationale: obligation.Rationale})
	}
	for i := range report.Issues {
		report.Issues[i].ID = fmt.Sprintf("SEM-%03d", i+1)
		if report.Issues[i].Blocking {
			report.BlockingIssues++
		}
	}
	sort.Slice(report.Realizations, func(i, j int) bool { return report.Realizations[i].ObligationID < report.Realizations[j].ObligationID })
	report.OK = report.BlockingIssues == 0 && report.ObligationsRealized == report.ObligationsRequired
	return report
}

func compatibleRealizations(kind string, elements []string) []string {
	allowedPrefixes := map[string][]string{
		"identity":           {"field:", "constraint:"},
		"key":                {"field:", "constraint:"},
		"attribute":          {"field:", "entity:"},
		"relationship":       {"relationship:"},
		"cardinality":        {"relationship:", "constraint:"},
		"ownership":          {"relationship:", "file_spec:"},
		"lifecycle":          {"state_machine:", "field:", "constraint:", "entity:"},
		"event_history":      {"entity:", "state_machine:", "field:"},
		"generated_snapshot": {"entity:", "field:"},
		"derived_view":       {"derived_view:"},
		"invariant":          {"constraint:", "field:", "state_machine:"},
		"security":           {"constraint:", "field:", "entity:"},
		"completeness":       {"constraint:", "field:", "relationship:"},
	}
	prefixes := allowedPrefixes[kind]
	if len(prefixes) == 0 {
		return elements
	}
	out := []string{}
	for _, element := range elements {
		for _, prefix := range prefixes {
			if strings.HasPrefix(element, prefix) {
				out = append(out, element)
				break
			}
		}
	}
	return uniqueSorted(out)
}

func semanticIssue(_ string, severity, code, message string, elements []string, obligationID string) SemanticIssue {
	return SemanticIssue{Severity: severity, Code: code, Kind: "semantic", ObligationID: obligationID, Message: message, ModelElements: elements, Blocking: severity == "critical" || severity == "high"}
}

func riskSeverity(risk string) string {
	if risk == "low" {
		return "medium"
	}
	return "high"
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

func renderInvariantReport(report SemanticVerificationReport) string {
	var out strings.Builder
	out.WriteString("# Invariant and design-obligation report\n\n")
	out.WriteString(fmt.Sprintf("Pipeline: v%s  \nModel SHA-256: `%s`  \nStatus: **%s**\n\n", report.PipelineVersion, report.ModelSHA256, map[bool]string{true: "PASS", false: "BLOCKED"}[report.OK]))
	out.WriteString(fmt.Sprintf("Required obligations: %d; realized: %d; blocking issues: %d.\n\n", report.ObligationsRequired, report.ObligationsRealized, report.BlockingIssues))
	out.WriteString("| Obligation | Status | Enforcement | Model elements |\n|---|---|---|---|\n")
	for _, realization := range report.Realizations {
		out.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", realization.ObligationID, realization.Status, realization.Enforcement, strings.Join(realization.ModelElements, ", ")))
	}
	if len(report.Issues) > 0 {
		out.WriteString("\n## Issues\n\n")
		for _, issue := range report.Issues {
			out.WriteString(fmt.Sprintf("- **%s / %s**: %s\n", issue.Severity, issue.Code, issue.Message))
		}
	}
	return out.String()
}
