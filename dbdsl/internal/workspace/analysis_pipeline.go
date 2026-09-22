package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
	"dbdsl/internal/jobs"
	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"

	"gopkg.in/yaml.v3"
)

type AnalysisStageOptions struct {
	BaseRevision    int
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	OnProgress      jobs.StepEmitter
}

type CRUDMatrixArtifact struct {
	Document       map[string]any          `yaml:"document" json:"document"`
	Notation       map[string]string       `yaml:"notation" json:"notation"`
	Actors         []dsl.CRUDActor         `yaml:"actors" json:"actors"`
	Operations     []AcceptedCRUDOperation `yaml:"operations" json:"operations"`
	CoverageChecks []map[string]any        `yaml:"coverage_checks" json:"coverage_checks"`
}

type AcceptedCRUDOperation struct {
	ID                string   `yaml:"id" json:"id"`
	Label             string   `yaml:"label" json:"label"`
	FunctionalArea    string   `yaml:"functional_area" json:"functional_area_id"`
	FunctionalPattern string   `yaml:"functional_pattern" json:"functional_pattern"`
	Actor             string   `yaml:"actor" json:"actor_id"`
	SourceAtoms       []string `yaml:"source_atoms" json:"requirement_atoms"`
	SourceUnits       []string `yaml:"source_units" json:"source_units"`
	Creates           []string `yaml:"creates" json:"creates"`
	Reads             []string `yaml:"reads" json:"reads"`
	Updates           []string `yaml:"updates" json:"updates"`
	Deletes           []string `yaml:"deletes" json:"deletes"`
	PersistentData    []string `yaml:"persistent_data" json:"persistent_data"`
	Outcome           string   `yaml:"outcome" json:"outcome"`
	RequiresReview    bool     `yaml:"requires_review" json:"requires_review"`
	Warnings          []string `yaml:"warnings" json:"warnings"`
}

type DesignObligationArtifacts struct {
	Proposal llmpipeline.DesignObligationsFile `json:"proposal"`
	Accepted llmpipeline.DesignObligationsFile `json:"accepted"`
	QA       llmpipeline.DesignObligationQA    `json:"qa"`
}

func (s *Store) GenerateRequirementAtoms(ctx context.Context, client llm.Client, projectID string, opts AnalysisStageOptions) (int, []string, error) {
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "requirement_atoms", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	project, units, err := s.analysisStageInputs(projectID, opts.BaseRevision)
	if err != nil {
		return 0, nil, err
	}
	if client == nil {
		return 0, nil, errors.New("LLM client is required for requirement extraction")
	}
	chunkUnits := len(units)
	if chunkUnits > 40 {
		chunkUnits = 40
	}
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "requirement_atoms", opts.MaxOutputTokens, stageBudgetInput{Units: chunkUnits})
	controls := s.resolveLLMExecutionControls(projectID)
	emit := stageEmitter(opts.OnProgress)
	emit("extract_requirement_atoms", "Extracting atomic requirements.", 20, map[string]any{"source_units": len(units)})
	proposal, qa, err := llmpipeline.RunRequirementAtomExtraction(ctx, client, llmpipeline.RequirementAtomStageOptions{
		OutDir: s.projectWorkspaceDir(projectID), SourceUnits: units, Model: opts.Model,
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		MaxParallelism: controls.MaxParallelism, PromptVersion: controls.PromptVersion,
	})
	if err != nil {
		return 0, nil, err
	}
	accepted := buildRequirementAtomsArtifact(project, proposal)
	obligations, obligationQA := llmpipeline.DeriveDesignObligations(proposal.RequirementAtoms)
	obligations.Document["id"] = project.ID + "_design_obligations"
	obligations.Document["title"] = project.Name + " design obligations"
	obligations.Document["requirement_atoms_file"] = "requirement_atoms.yaml"
	if !obligationQA.OK {
		return 0, nil, fmt.Errorf("design-obligation QA failed: %s", strings.Join(obligationQA.Errors, "; "))
	}
	emit("validate_requirement_atoms", "Validating source references and review signals.", 70, coverageMetadata(qa.Coverage))
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"requirement_atoms.proposed.json":  {Value: proposal, JSON: true},
		"requirement_atoms.yaml":           {Value: accepted},
		"requirement_atom_qa.json":         {Value: qa, JSON: true},
		"design_obligations.proposed.json": {Value: obligations, JSON: true},
		"design_obligations.yaml":          {Value: obligations},
		"design_obligation_qa.json":        {Value: obligationQA, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.RequirementAtomsProposalPath = paths["requirement_atoms.proposed.json"]
		current.RequirementAtomsPath = paths["requirement_atoms.yaml"]
		current.RequirementAtomQAPath = paths["requirement_atom_qa.json"]
		current.DesignObligationsProposalPath = paths["design_obligations.proposed.json"]
		current.DesignObligationsPath = paths["design_obligations.yaml"]
		current.DesignObligationQAPath = paths["design_obligation_qa.json"]
		invalidateAfterRequirementAtoms(current)
		current.LifecycleStatus = "sources_processed"
		current.LastActivity = "Requirement atoms generated and validated."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"requirement_atoms", "requirement_atom_qa", "design_obligations", "design_obligation_qa"}, nil
}

func (s *Store) DesignObligations(projectID string) (DesignObligationArtifacts, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return DesignObligationArtifacts{}, ErrNotFound
	}
	if project.DesignObligationsProposalPath == "" || project.DesignObligationsPath == "" || project.DesignObligationQAPath == "" {
		return DesignObligationArtifacts{}, ErrNotFound
	}
	var out DesignObligationArtifacts
	if err := readJSON(s.absoluteWorkspacePath(project.DesignObligationsProposalPath), &out.Proposal); err != nil {
		return DesignObligationArtifacts{}, err
	}
	if err := readYAML(s.absoluteWorkspacePath(project.DesignObligationsPath), &out.Accepted); err != nil {
		return DesignObligationArtifacts{}, err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.DesignObligationQAPath), &out.QA); err != nil {
		return DesignObligationArtifacts{}, err
	}
	return out, nil
}

func (s *Store) DesignObligationMigrationPreview(projectID string) (map[string]any, llmpipeline.DesignObligationQA, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, llmpipeline.DesignObligationQA{}, ErrNotFound
	}
	artifacts, err := s.DesignObligations(projectID)
	if err != nil {
		return nil, llmpipeline.DesignObligationQA{}, err
	}
	var atoms llmpipeline.RequirementAtomExtractionProposal
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return nil, llmpipeline.DesignObligationQA{}, err
	}
	updated, qa := llmpipeline.ReclassifyDesignObligations(artifacts.Accepted, atoms.RequirementAtoms)
	return designObligationMigrationReport(artifacts.Accepted, updated), qa, nil
}

func (s *Store) GenerateFunctionalAnalysis(ctx context.Context, client llm.Client, projectID string, opts AnalysisStageOptions) (int, []string, error) {
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "functional_analysis", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	project, units, err := s.analysisStageInputs(projectID, opts.BaseRevision)
	if err != nil {
		return 0, nil, err
	}
	if client == nil {
		return 0, nil, errors.New("LLM client is required for functional analysis")
	}
	var atoms llmpipeline.RequirementAtomExtractionProposal
	if project.RequirementAtomsProposalPath == "" {
		return 0, nil, errors.New("requirement atoms are not ready")
	}
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return 0, nil, err
	}
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "functional_analysis", opts.MaxOutputTokens, stageBudgetInput{Atoms: len(atoms.RequirementAtoms)})
	emit := stageEmitter(opts.OnProgress)
	emit("build_functional_analysis", "Grouping requirements into business capabilities.", 25, map[string]any{"requirement_atoms": len(atoms.RequirementAtoms)})
	proposal, qa, err := llmpipeline.RunFunctionalAnalysis(ctx, client, llmpipeline.FunctionalAnalysisStageOptions{
		OutDir: s.projectWorkspaceDir(projectID), SourceUnits: units, RequirementAtoms: atoms.RequirementAtoms,
		Model: opts.Model, ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
	})
	if err != nil {
		return 0, nil, err
	}
	accepted := buildFunctionalArtifact(project, proposal)
	emit("validate_functional_analysis", "Validating actor references and atom coverage.", 72, coverageMetadata(qa.Coverage))
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"functional_analysis.proposed.json": {Value: proposal, JSON: true},
		"functional_decomposition.yaml":     {Value: accepted},
		"functional_analysis_qa.json":       {Value: qa, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.FunctionalAnalysisProposalPath = paths["functional_analysis.proposed.json"]
		current.FunctionalDecompositionPath = paths["functional_decomposition.yaml"]
		current.FunctionalAnalysisQAPath = paths["functional_analysis_qa.json"]
		invalidateAfterFunctionalAnalysis(current)
		current.LifecycleStatus = "sources_processed"
		current.LastActivity = "Functional analysis generated and validated."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"functional_analysis", "functional_decomposition"}, nil
}

func (s *Store) GenerateCRUDMapping(ctx context.Context, client llm.Client, projectID string, opts AnalysisStageOptions) (int, []string, error) {
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "crud_mapping", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	project, units, err := s.analysisStageInputs(projectID, opts.BaseRevision)
	if err != nil {
		return 0, nil, err
	}
	if client == nil {
		return 0, nil, errors.New("LLM client is required for CRUD mapping")
	}
	var atoms llmpipeline.RequirementAtomExtractionProposal
	var functional llmpipeline.FunctionalAnalysisProposal
	if project.RequirementAtomsProposalPath == "" || project.FunctionalAnalysisProposalPath == "" {
		return 0, nil, errors.New("requirements and functional analysis are not ready")
	}
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return 0, nil, err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.FunctionalAnalysisProposalPath), &functional); err != nil {
		return 0, nil, err
	}
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "crud_mapping", opts.MaxOutputTokens, stageBudgetInput{Atoms: len(atoms.RequirementAtoms), Areas: len(functional.FunctionalAreas)})
	emit := stageEmitter(opts.OnProgress)
	emit("build_crud_mapping", "Mapping business operations and persistence effects.", 25, map[string]any{"functional_areas": len(functional.FunctionalAreas)})
	proposal, qa, err := llmpipeline.RunCRUDMapping(ctx, client, llmpipeline.CRUDMappingStageOptions{
		OutDir: s.projectWorkspaceDir(projectID), SourceUnits: units, RequirementAtoms: atoms.RequirementAtoms,
		FunctionalAreas: functional.FunctionalAreas, Actors: functional.Actors, Model: opts.Model,
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
	})
	if err != nil {
		return 0, nil, err
	}
	accepted := buildCRUDArtifact(project, functional.Actors, proposal)
	emit("validate_crud_mapping", "Validating operation references and C/R/U/D effects.", 72, coverageMetadata(qa.Coverage))
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"crud_mapping.proposed.json": {Value: proposal, JSON: true},
		"crud_matrix.yaml":           {Value: accepted},
		"crud_mapping_qa.json":       {Value: qa, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.CRUDMappingProposalPath = paths["crud_mapping.proposed.json"]
		current.CRUDMatrixPath = paths["crud_matrix.yaml"]
		current.CRUDMappingQAPath = paths["crud_mapping_qa.json"]
		invalidateModelAndReview(current)
		current.AnalysisReady = true
		current.LifecycleStatus = "analysis_review"
		current.LastActivity = "CRUD mapping generated; review proposal is the next step."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"crud_mapping", "crud_matrix"}, nil
}

func (s *Store) analysisStageInputs(projectID string, baseRevision int) (*ProjectState, []dsl.SourceUnit, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, nil, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return nil, nil, ErrRevisionConflict
	}
	artifacts, err := s.SourceUnitArtifacts(projectID)
	if err != nil {
		return nil, nil, errors.New("accepted source units are not ready")
	}
	if !artifacts.QA.OK || len(artifacts.QA.NeedsAttention) > 0 {
		return nil, nil, errors.New("source-unit QA needs attention")
	}
	return project, artifacts.Accepted.SourceUnits, nil
}

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

func buildRequirementAtomsArtifact(project *ProjectState, proposal llmpipeline.RequirementAtomExtractionProposal) dsl.V05RequirementAtomsFile {
	atoms := make([]dsl.RequirementAtom, 0, len(proposal.RequirementAtoms))
	for _, item := range proposal.RequirementAtoms {
		atoms = append(atoms, dsl.RequirementAtom{
			ID: item.ID, Statement: item.Statement, AtomType: item.AtomType, ModelingRelevance: item.ModelingRelevance,
			Subject: item.Subject, Predicate: item.Predicate, Object: item.Object, Quantifier: item.Quantifier,
			Condition: item.Condition, TemporalSemantics: item.TemporalSemantics, Ownership: item.Ownership,
			SourceUnits: item.SourceUnits, FunctionalArea: item.FunctionalArea, FunctionalPattern: item.FunctionalPattern,
			SupportLevel: item.SupportLevel, Confidence: item.Confidence, RequiresReview: item.RequiresReview,
			ReviewDecisions: append([]string(nil), item.ReviewDecisions...), ModelingOutcome: dsl.RequirementOutcome{Status: item.ModelingOutcome},
		})
	}
	return dsl.V05RequirementAtomsFile{
		Document:         map[string]any{"id": project.ID + "_requirement_atoms", "title": project.Name + " requirement atoms", "pipeline_version": "0.7", "source_units_file": filepath.Base(project.SourceUnitsPath), "generation_strategy": "llm_granular_stage_with_design_obligations"},
		RequirementAtoms: atoms,
		CoverageChecks:   []map[string]any{{"id": "source_references_valid", "status": "passed"}},
	}
}

func buildFunctionalArtifact(project *ProjectState, proposal llmpipeline.FunctionalAnalysisProposal) dsl.V05FunctionalDecompositionFile {
	areas := make([]dsl.FunctionalArea, 0, len(proposal.FunctionalAreas))
	for _, item := range proposal.FunctionalAreas {
		areas = append(areas, dsl.FunctionalArea{ID: item.ID, Label: item.Label, Purpose: item.Purpose, MainActors: item.MainActors, Atoms: item.Atoms, ModelingFocus: item.ModelingFocus})
	}
	return dsl.V05FunctionalDecompositionFile{
		Document:        map[string]any{"id": project.ID + "_functional_decomposition", "title": project.Name + " functional decomposition", "pipeline_version": "0.7", "generation_strategy": "llm_granular_stage"},
		FunctionalAreas: areas, CoverageSummary: map[string]any{"total_areas": len(areas), "status": "passed"},
	}
}

func buildCRUDArtifact(project *ProjectState, actors []llmpipeline.ActorProposal, proposal llmpipeline.CRUDMappingProposal) CRUDMatrixArtifact {
	acceptedActors := make([]dsl.CRUDActor, 0, len(actors))
	for _, actor := range actors {
		acceptedActors = append(acceptedActors, dsl.CRUDActor{ID: actor.ID, Label: actor.Label, Description: actor.Description})
	}
	operations := make([]AcceptedCRUDOperation, 0, len(proposal.Operations))
	for _, item := range proposal.Operations {
		operations = append(operations, AcceptedCRUDOperation{
			ID: item.ID, Label: item.Label, FunctionalArea: item.FunctionalAreaID, FunctionalPattern: "business_operation", Actor: item.ActorID,
			SourceAtoms: item.RequirementAtoms, SourceUnits: item.SourceUnits, Creates: item.Creates, Reads: item.Reads,
			Updates: item.Updates, Deletes: item.Deletes, PersistentData: item.PersistentData, Outcome: item.Outcome,
			RequiresReview: item.RequiresReview, Warnings: item.Warnings,
		})
	}
	return CRUDMatrixArtifact{
		Document: map[string]any{"id": project.ID + "_crud_matrix", "title": project.Name + " CRUD mapping", "pipeline_version": "0.7"},
		Notation: map[string]string{"C": "create", "R": "read", "U": "update", "D": "delete"}, Actors: acceptedActors, Operations: operations,
		CoverageChecks: []map[string]any{{"id": "operation_references_valid", "status": "passed"}},
	}
}

func invalidateAfterRequirementAtoms(project *ProjectState) {
	project.FunctionalAnalysisProposalPath, project.FunctionalDecompositionPath, project.FunctionalAnalysisQAPath = "", "", ""
	project.CRUDMappingProposalPath, project.CRUDMatrixPath, project.CRUDMappingQAPath = "", "", ""
	invalidateModelAndReview(project)
}

func invalidateAfterFunctionalAnalysis(project *ProjectState) {
	project.CRUDMappingProposalPath, project.CRUDMatrixPath, project.CRUDMappingQAPath = "", "", ""
	invalidateModelAndReview(project)
}

func invalidateModelAndReview(project *ProjectState) {
	project.AnalysisReady = false
	project.ModelGenerated = false
	project.DBMLReady = false
	project.Completed = false
	project.ModelPath = ""
	project.TaskPath = ""
	project.BundlePath = ""
	project.OpenReviewIDs = map[string]bool{}
	project.AnsweredReviews = map[string]string{}
	project.ReviewCandidatesProposalPath = ""
	project.ReviewCandidatesPath = ""
	project.ReviewCandidateQAPath = ""
	project.ReviewDecisionsPath = ""
	project.LastAppliedPatchPath = ""
	invalidateModelArtifacts(project)
}

func invalidateModelArtifacts(project *ProjectState) {
	project.ConceptualModelProposalPath = ""
	project.ConceptualModelAcceptedPath = ""
	project.ConceptualModelQAPath = ""
	project.ConceptualModelDiffPath = ""
	project.LogicalPatchProposalPath = ""
	project.ObligationRealizationsPath = ""
	project.SemanticVerificationPath = ""
	project.InvariantReportPath = ""
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

func (s *Store) projectRequirementAtoms(projectID string) ([]RequirementAtom, map[string]int, bool, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, nil, false, ErrNotFound
	}
	if project.RequirementAtomsProposalPath == "" {
		return nil, nil, false, nil
	}
	var proposal llmpipeline.RequirementAtomExtractionProposal
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &proposal); err != nil {
		return nil, nil, false, err
	}
	items := make([]RequirementAtom, 0, len(proposal.RequirementAtoms))
	covered := map[string]bool{}
	for _, item := range proposal.RequirementAtoms {
		for _, id := range item.SourceUnits {
			covered[id] = true
		}
		status := "reviewed"
		if item.RequiresReview {
			status = "needs_review"
		}
		items = append(items, RequirementAtom{ID: item.ID, Statement: item.Statement, AtomType: item.AtomType, ModelingRelevance: item.ModelingRelevance,
			Subject: item.Subject, Predicate: item.Predicate, Object: item.Object, Quantifier: item.Quantifier,
			Condition: item.Condition, TemporalSemantics: item.TemporalSemantics, Ownership: item.Ownership,
			SourceUnits: item.SourceUnits, FunctionalArea: item.FunctionalArea, FunctionalPattern: item.FunctionalPattern,
			SupportLevel: item.SupportLevel, Confidence: item.Confidence, ReviewStatus: status, ModelingOutcome: item.ModelingOutcome,
			ModelImpactPreview: []string{}, OpenReviewCandidates: []string{}})
	}
	coverage := map[string]int{"source_units_covered": len(covered), "requirements_needing_review": countRequirementReview(items), "direct_db_requirements": countDirectDB(items), "non_model_requirements": len(items) - countDirectDB(items)}
	return items, coverage, true, nil
}

func countRequirementReview(items []RequirementAtom) int {
	count := 0
	for _, item := range items {
		if item.ReviewStatus != "reviewed" {
			count++
		}
	}
	return count
}
func countDirectDB(items []RequirementAtom) int {
	count := 0
	for _, item := range items {
		if item.ModelingRelevance == "direct_db" {
			count++
		}
	}
	return count
}

func (s *Store) projectFunctionalAreas(projectID string) ([]FunctionalArea, []ActorSummary, bool, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, nil, false, ErrNotFound
	}
	if project.FunctionalAnalysisProposalPath == "" {
		return nil, nil, false, nil
	}
	var proposal llmpipeline.FunctionalAnalysisProposal
	if err := readJSON(s.absoluteWorkspacePath(project.FunctionalAnalysisProposalPath), &proposal); err != nil {
		return nil, nil, false, err
	}
	areas := make([]FunctionalArea, 0, len(proposal.FunctionalAreas))
	for _, item := range proposal.FunctionalAreas {
		areas = append(areas, FunctionalArea{ID: item.ID, Label: item.Label, Purpose: item.Purpose, MainActors: item.MainActors, RequirementAtoms: item.Atoms, ModelingFocus: item.ModelingFocus, OpenReviewCandidates: []string{}})
	}
	actors := make([]ActorSummary, 0, len(proposal.Actors))
	for _, actor := range proposal.Actors {
		var areaIDs []string
		for _, area := range proposal.FunctionalAreas {
			for _, id := range area.MainActors {
				if id == actor.ID {
					areaIDs = append(areaIDs, area.ID)
					break
				}
			}
		}
		actors = append(actors, ActorSummary{ID: actor.ID, Label: actor.Label, Kind: actor.Kind, FunctionalAreas: areaIDs, MapsToUserRole: actor.Kind == "human", OpenReviewCandidates: []string{}})
	}
	return areas, actors, true, nil
}

func (s *Store) projectCRUDOperations(projectID string) ([]CrudOperation, bool, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, false, ErrNotFound
	}
	if project.CRUDMappingProposalPath == "" {
		return nil, false, nil
	}
	var proposal llmpipeline.CRUDMappingProposal
	if err := readJSON(s.absoluteWorkspacePath(project.CRUDMappingProposalPath), &proposal); err != nil {
		return nil, false, err
	}
	items := make([]CrudOperation, 0, len(proposal.Operations))
	for _, item := range proposal.Operations {
		status := "reviewed"
		if item.RequiresReview {
			status = "needs_review"
		}
		items = append(items, CrudOperation{ID: item.ID, Label: item.Label, ActorID: item.ActorID, FunctionalAreaID: item.FunctionalAreaID,
			Creates: item.Creates, Reads: item.Reads, Updates: item.Updates, Deletes: item.Deletes, PersistentData: item.PersistentData,
			Outcome: item.Outcome, RequirementAtoms: item.RequirementAtoms, SourceUnits: item.SourceUnits, ReviewStatus: status, OpenReviewCandidates: []string{}})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, true, nil
}
