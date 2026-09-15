package llmpipeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

const (
	defaultConceptualMaxOutputTokens = 24000
	defaultLogicalMaxOutputTokens    = 32000
)

type ConceptualModelOptions struct {
	OutDir                string
	SourceUnits           []dsl.SourceUnit
	RequirementAtoms      []RequirementAtomProposal
	FunctionalAreas       []FunctionalAreaProposal
	Actors                []ActorProposal
	Operations            []CRUDOperationProposal
	ReviewDecisions       []string
	ReviewDecisionContext []ConceptualReviewDecisionInput
	DesignObligations     []DesignObligation
	Model                 string
	ReasoningEffort       string
	MaxOutputTokens       int
}

type ConceptualReviewDecisionInput struct {
	ID               string   `json:"id"`
	Question         string   `json:"question"`
	SelectedOptionID string   `json:"selected_option_id"`
	Rationale        string   `json:"rationale"`
	AffectedAtoms    []string `json:"affected_atoms"`
}

type modelSourceUnitInput struct {
	ID         string `json:"id"`
	Normalized string `json:"normalized"`
}

type modelRequirementAtomInput struct {
	ID                string   `json:"id"`
	Statement         string   `json:"statement"`
	Subject           string   `json:"subject"`
	Predicate         string   `json:"predicate"`
	Object            string   `json:"object"`
	Quantifier        string   `json:"quantifier"`
	Condition         string   `json:"condition"`
	TemporalSemantics string   `json:"temporal_semantics"`
	Ownership         string   `json:"ownership"`
	ModelingOutcome   string   `json:"modeling_outcome"`
	SourceUnits       []string `json:"source_units"`
	FunctionalArea    string   `json:"functional_area"`
	ReviewDecisions   []string `json:"review_decisions,omitempty"`
}

type conceptualFunctionalAreaInput struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Purpose       string   `json:"purpose"`
	MainActors    []string `json:"main_actors"`
	Atoms         []string `json:"atoms"`
	ModelingFocus []string `json:"modeling_focus"`
}

type conceptualCRUDOperationInput struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	ActorID          string   `json:"actor_id"`
	FunctionalAreaID string   `json:"functional_area_id"`
	Creates          []string `json:"creates"`
	Reads            []string `json:"reads"`
	Updates          []string `json:"updates"`
	Deletes          []string `json:"deletes"`
	PersistentData   []string `json:"persistent_data"`
	Outcome          string   `json:"outcome"`
	RequirementAtoms []string `json:"requirement_atoms"`
	SourceUnits      []string `json:"source_units"`
}

type LogicalProjectionOptions struct {
	OutDir                string
	ConceptualModel       ConceptualModelProposal
	SourceUnits           []dsl.SourceUnit
	RequirementAtoms      []RequirementAtomProposal
	ReviewDecisions       []string
	ReviewDecisionContext []ConceptualReviewDecisionInput
	DesignObligations     []DesignObligation
	PreviousProposal      *PatchProposal
	ValidationErrors      []string
	Model                 string
	ReasoningEffort       string
	MaxOutputTokens       int
}

type LogicalArtifacts struct {
	RequirementAtoms        dsl.V05RequirementAtomsFile
	FunctionalDecomposition dsl.V05FunctionalDecompositionFile
	CRUDMatrix              dsl.V05CRUDMatrixFile
	ReviewDecisions         dsl.V05ReviewDecisionsFile
	Model                   dsl.Document
}

func RunConceptualModel(ctx context.Context, client llm.Client, opts ConceptualModelOptions) (ConceptualModelProposal, StageQA, error) {
	if client == nil {
		return ConceptualModelProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || len(opts.RequirementAtoms) == 0 || len(opts.Operations) == 0 {
		return ConceptualModelProposal{}, StageQA{}, errors.New("output directory and accepted analysis are required")
	}
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = defaultConceptualMaxOutputTokens
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	input := conceptualModelInput(opts)
	var proposal ConceptualModelProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 8, llm.Request{
		Stage: "conceptual_model", Model: opts.Model, Instructions: conceptualModelInstructions,
		Input: input, SchemaName: "DBDSLConceptualModel", Schema: conceptualModelSchema(),
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{"template_version": promptTemplateVersion},
	}, &proposal, func() []string {
		qa = ValidateConceptualModelWithObligations(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.ReviewDecisions, opts.DesignObligations)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	qa = ValidateConceptualModelWithObligations(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.ReviewDecisions, opts.DesignObligations)
	return proposal, qa, nil
}

func conceptualModelInput(opts ConceptualModelOptions) string {
	sourceUnits := modelSourceUnitInputs(opts.SourceUnits)
	atoms := modelRequirementAtomInputs(opts.RequirementAtoms)

	areas := make([]conceptualFunctionalAreaInput, 0, len(opts.FunctionalAreas))
	for _, area := range opts.FunctionalAreas {
		areas = append(areas, conceptualFunctionalAreaInput{
			ID: area.ID, Label: area.Label, Purpose: area.Purpose, MainActors: area.MainActors,
			Atoms: area.Atoms, ModelingFocus: area.ModelingFocus,
		})
	}

	operations := make([]conceptualCRUDOperationInput, 0, len(opts.Operations))
	for _, operation := range opts.Operations {
		operations = append(operations, conceptualCRUDOperationInput{
			ID: operation.ID, Label: operation.Label, ActorID: operation.ActorID,
			FunctionalAreaID: operation.FunctionalAreaID, Creates: operation.Creates,
			Reads: operation.Reads, Updates: operation.Updates, Deletes: operation.Deletes,
			PersistentData: operation.PersistentData, Outcome: operation.Outcome,
			RequirementAtoms: operation.RequirementAtoms, SourceUnits: operation.SourceUnits,
		})
	}

	return mustCompactJSON(map[string]any{
		"source_units": sourceUnits, "requirement_atoms": atoms,
		"functional_areas": areas, "actors": opts.Actors, "crud_operations": operations,
		"review_decisions": opts.ReviewDecisions, "resolved_review_decisions": opts.ReviewDecisionContext,
		"design_obligations": opts.DesignObligations,
		"output_contract":    "conceptual_model",
		"pipeline_version":   "0.7", "template_version": promptTemplateVersion,
	})
}

func modelSourceUnitInputs(units []dsl.SourceUnit) []modelSourceUnitInput {
	sourceUnits := make([]modelSourceUnitInput, 0, len(units))
	for _, unit := range units {
		normalized := strings.TrimSpace(unit.Text.Normalized)
		if normalized == "" {
			normalized = strings.TrimSpace(unit.Text.Exact)
		}
		sourceUnits = append(sourceUnits, modelSourceUnitInput{ID: unit.ID, Normalized: normalized})
	}
	return sourceUnits
}

func modelRequirementAtomInputs(requirementAtoms []RequirementAtomProposal) []modelRequirementAtomInput {
	atoms := make([]modelRequirementAtomInput, 0, len(requirementAtoms))
	for _, atom := range requirementAtoms {
		atoms = append(atoms, modelRequirementAtomInput{
			ID: atom.ID, Statement: atom.Statement, ModelingOutcome: atom.ModelingOutcome,
			Subject: atom.Subject, Predicate: atom.Predicate, Object: atom.Object, Quantifier: atom.Quantifier,
			Condition: atom.Condition, TemporalSemantics: atom.TemporalSemantics, Ownership: atom.Ownership,
			SourceUnits: atom.SourceUnits, FunctionalArea: atom.FunctionalArea,
			ReviewDecisions: atom.ReviewDecisions,
		})
	}
	return atoms
}

func RunLogicalProjection(ctx context.Context, client llm.Client, opts LogicalProjectionOptions) (PatchProposal, StageQA, error) {
	if client == nil {
		return PatchProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || len(opts.ConceptualModel.EntityConcepts) == 0 {
		return PatchProposal{}, StageQA{}, errors.New("output directory and accepted conceptual model are required")
	}
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = defaultLogicalMaxOutputTokens
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	input := logicalProjectionInput(opts)
	sourceIDs := sourceUnitIDSet(opts.SourceUnits)
	atomIDs := atomIDSet(opts.RequirementAtoms)
	var proposal PatchProposal
	qa := newStageQA(nil)
	err := runStructuredStage(ctx, client, opts.OutDir, 9, llm.Request{
		Stage: "logical_projection", Model: opts.Model, Instructions: logicalProjectionInstructions,
		Input: input, SchemaName: "DBDSLLogicalProjection", Schema: patchSchema(), ReasoningEffort: opts.ReasoningEffort,
		MaxOutputTokens: opts.MaxOutputTokens, Metadata: map[string]string{"template_version": promptTemplateVersion},
	}, &proposal, func() []string {
		qa.Errors = validatePatchProposal(proposal, sourceIDs, atomIDs)
		qa.OK = len(qa.Errors) == 0
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	qa.Errors = validatePatchProposal(proposal, sourceIDs, atomIDs)
	qa.OK = len(qa.Errors) == 0
	qa.Coverage["patch_operations"] = len(proposal.Operations)
	return proposal, qa, nil
}

func logicalProjectionInput(opts LogicalProjectionOptions) string {
	input := map[string]any{
		"source_units": modelSourceUnitInputs(opts.SourceUnits), "requirement_atoms": modelRequirementAtomInputs(opts.RequirementAtoms),
		"review_decisions": opts.ReviewDecisions, "resolved_review_decisions": opts.ReviewDecisionContext,
		"design_obligations": opts.DesignObligations,
		"output_contract":    "logical_projection", "pipeline_version": "0.7", "template_version": promptTemplateVersion,
	}
	if opts.PreviousProposal != nil && len(opts.ValidationErrors) > 0 {
		input["repair_mode"] = true
		input["previous_proposal"] = opts.PreviousProposal
		input["validation_errors"] = opts.ValidationErrors
	} else {
		input["conceptual_model"] = opts.ConceptualModel
	}
	return mustCompactJSON(input)
}

func ValidateConceptualModel(proposal ConceptualModelProposal, units []dsl.SourceUnit, atoms []RequirementAtomProposal, reviewDecisions []string) StageQA {
	return ValidateConceptualModelWithObligations(proposal, units, atoms, reviewDecisions, nil)
}

func ValidateConceptualModelWithObligations(proposal ConceptualModelProposal, units []dsl.SourceUnit, atoms []RequirementAtomProposal, reviewDecisions []string, obligations []DesignObligation) StageQA {
	qa := newStageQA(proposal.Warnings)
	sourceIDs, atomIDs, decisionIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, item := range units {
		sourceIDs[item.ID] = true
	}
	for _, item := range atoms {
		atomIDs[item.ID] = true
	}
	for _, id := range reviewDecisions {
		decisionIDs[id] = true
	}
	concepts := map[string]bool{}
	fileConcepts := map[string]bool{}
	if len(proposal.EntityConcepts) == 0 {
		qa.Errors = append(qa.Errors, "conceptual model produced no entity concepts")
	}
	validateEvidence := func(owner string, evidence EvidenceProposal) {
		if len(evidence.SourceUnits) == 0 || len(evidence.RequirementAtoms) == 0 {
			qa.Errors = append(qa.Errors, owner+" has incomplete evidence")
		}
		for _, id := range evidence.SourceUnits {
			if !sourceIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown source unit %s", owner, id))
			}
		}
		for _, id := range evidence.RequirementAtoms {
			if !atomIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown atom %s", owner, id))
			}
		}
		for _, id := range evidence.ReviewDecisions {
			if !decisionIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown decision %s", owner, id))
			}
		}
		if evidence.SupportLevel == "assumption" && len(evidence.ReviewDecisions) == 0 {
			qa.Errors = append(qa.Errors, owner+" assumption has no review decision")
		}
	}
	for _, concept := range proposal.EntityConcepts {
		if concept.ID == "" || concepts[concept.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate concept id %q", concept.ID))
		}
		concepts[concept.ID] = true
		validateEvidence(concept.ID, concept.Evidence)
		attributeIDs := map[string]bool{}
		for _, attribute := range concept.Attributes {
			if attribute.ID == "" || attributeIDs[attribute.ID] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s has empty or duplicate attribute %q", concept.ID, attribute.ID))
			}
			attributeIDs[attribute.ID] = true
			validateEvidence(concept.ID+"."+attribute.ID, attribute.Evidence)
		}
	}
	for _, concept := range proposal.FileConcepts {
		if concept.ID != "" {
			fileConcepts[concept.ID] = true
		}
	}
	for _, relationship := range proposal.Relationships {
		entityToEntity := concepts[relationship.From] && concepts[relationship.To]
		entityToFile := (concepts[relationship.From] && fileConcepts[relationship.To]) ||
			(fileConcepts[relationship.From] && concepts[relationship.To])
		if !entityToEntity && !entityToFile {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown relationship endpoint", relationship.ID))
		}
		if relationship.Cardinality == "unknown" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has unresolved cardinality", relationship.ID))
		}
		validateEvidence(relationship.ID, relationship.Evidence)
	}
	representedAtoms := map[string]bool{}
	collectEvidence := func(evidence EvidenceProposal) {
		for _, atomID := range evidence.RequirementAtoms {
			representedAtoms[atomID] = true
		}
	}
	for _, concept := range proposal.EntityConcepts {
		collectEvidence(concept.Evidence)
		for _, attribute := range concept.Attributes {
			collectEvidence(attribute.Evidence)
		}
	}
	for _, relationship := range proposal.Relationships {
		collectEvidence(relationship.Evidence)
	}
	for _, group := range [][]PlanElementProposal{proposal.LifecycleConcepts, proposal.DerivedConcepts, proposal.FileConcepts, proposal.ImportConcepts} {
		for _, concept := range group {
			for _, atomID := range concept.RequirementAtoms {
				representedAtoms[atomID] = true
			}
		}
	}
	coveredObligations := 0
	for _, obligation := range obligations {
		if obligation.Status == "not_required" || obligation.Persistence == "not_required" {
			continue
		}
		covered := false
		for _, atomID := range obligation.RequirementAtoms {
			covered = covered || representedAtoms[atomID]
		}
		if !covered {
			qa.Errors = append(qa.Errors, fmt.Sprintf("design obligation %s is not represented in the conceptual model", obligation.ID))
		} else {
			coveredObligations++
		}
	}
	if len(proposal.UnresolvedReviewIDs) > 0 {
		qa.Errors = append(qa.Errors, "conceptual model retains unresolved review IDs: "+strings.Join(proposal.UnresolvedReviewIDs, ", "))
	}
	qa.Coverage["entity_concepts"] = len(proposal.EntityConcepts)
	qa.Coverage["relationships"] = len(proposal.Relationships)
	qa.Coverage["design_obligations"] = len(obligations)
	qa.Coverage["covered_design_obligations"] = coveredObligations
	qa.OK = len(qa.Errors) == 0
	return qa
}

func BuildLogicalArtifacts(name string, sourceUnits []dsl.SourceUnit, atoms RequirementAtomExtractionProposal, functional FunctionalAnalysisProposal, crud CRUDMappingProposal, patch PatchProposal, decisions []dsl.V05ReviewDecision) (LogicalArtifacts, error) {
	// RA extraction intentionally does not assign final functional areas. The
	// later FA stage owns that classification, so copy it back onto the logical
	// projection input before the legacy v0.5 artifact converter runs. Without
	// this alignment every empty RA area becomes a synthetic core_model area and
	// can introduce an actor that is not present in the accepted actor catalog.
	atoms = alignRequirementAtomAreas(atoms, functional.FunctionalAreas)
	legacyOperations := make([]OperationProposal, 0, len(crud.Operations))
	for _, operation := range crud.Operations {
		legacyOperations = append(legacyOperations, OperationProposal{ID: operation.ID, Label: operation.Label, FunctionalArea: operation.FunctionalAreaID,
			FunctionalPattern: "business_operation", Actor: operation.ActorID, SourceAtoms: operation.RequirementAtoms, SourceUnits: operation.SourceUnits, Description: operation.Outcome})
	}
	extraction := RequirementExtractionProposal{RequirementAtoms: atoms.RequirementAtoms, FunctionalAreas: functional.FunctionalAreas, Actors: functional.Actors, Operations: legacyOperations}
	built, err := buildArtifacts("", name, sourceUnits, extraction, patch, &conversionDiagnostics{})
	if err != nil {
		return LogicalArtifacts{}, err
	}
	built.Model.Model.Status = "logical_draft_requires_final_review"
	built.Model.Source.PipelineVersion = "0.7"
	built.Model.Source.DerivationStrategy = "lossless_sources_design_obligations_v07"
	built.Model.Source.ReviewState = "resolved"
	built.Model.Source.AcceptedReviewDecisions = []dsl.AcceptedReviewDecision{}
	for _, decision := range decisions {
		selected, _ := decision.Decision["selected_option"].(string)
		built.Model.Source.AcceptedReviewDecisions = append(built.Model.Source.AcceptedReviewDecisions, dsl.AcceptedReviewDecision{ReviewID: decision.ID, SelectedOption: selected})
	}
	built.ReviewDecisions.ReviewDecisions = decisions
	built.ReviewDecisions.ReviewState = map[string]any{"status": "resolved", "all_required_reviews_resolved": true, "unresolved_requires_review_flags": 0}
	return LogicalArtifacts{RequirementAtoms: built.RequirementAtoms, FunctionalDecomposition: built.FunctionalDecomposition, CRUDMatrix: built.CRUDMatrix, ReviewDecisions: built.ReviewDecisions, Model: built.Model}, nil
}

func alignRequirementAtomAreas(atoms RequirementAtomExtractionProposal, areas []FunctionalAreaProposal) RequirementAtomExtractionProposal {
	atoms.RequirementAtoms = append([]RequirementAtomProposal(nil), atoms.RequirementAtoms...)
	areaByAtom := map[string]string{}
	orderedAreas := append([]FunctionalAreaProposal(nil), areas...)
	sort.SliceStable(orderedAreas, func(i, j int) bool { return orderedAreas[i].ID < orderedAreas[j].ID })
	for _, area := range orderedAreas {
		for _, atomID := range area.Atoms {
			if areaByAtom[atomID] == "" {
				areaByAtom[atomID] = area.ID
			}
		}
	}
	for i := range atoms.RequirementAtoms {
		if areaID := areaByAtom[atoms.RequirementAtoms[i].ID]; areaID != "" {
			atoms.RequirementAtoms[i].FunctionalArea = areaID
		}
	}
	return atoms
}
