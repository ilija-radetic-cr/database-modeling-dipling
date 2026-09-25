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
	MaxParallelism        int
	MaxRepairAttempts     int
	PromptVersion         string
	OnChunkProgress       func(phase string, completed, total int)
}

type ConceptualReviewDecisionInput struct {
	ID               string   `json:"id"`
	Question         string   `json:"question"`
	SelectedOptionID string   `json:"selected_option_id"`
	Rationale        string   `json:"rationale"`
	AffectedAtoms    []string `json:"affected_atoms"`
}

type modelSourceUnitInput struct {
	ID              string                       `json:"id"`
	Normalized      string                       `json:"normalized"`
	StructuredShape *structuredExampleShapeInput `json:"structured_shape,omitempty"`
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
	PersistenceEffect string   `json:"persistence_effect,omitempty"`
	ExampleRole       string   `json:"example_role,omitempty"`
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
}

type modelDesignObligationInput struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Persistence        string   `json:"persistence"`
	RequirementAtoms   []string `json:"requirement_atoms"`
	VerificationTarget string   `json:"verification_target"`
	Risk               string   `json:"risk"`
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
	RepairMaxOutputTokens int
	MaxParallelism        int
	MaxRepairAttempts     int
	PromptVersion         string
	ChunkIndex            int
	ChunkCount            int
	PrimaryConceptIDs     []string
	FullContextBytes      int
	OnChunkProgress       func(completed, total int)
	OnRepairProgress      func(round, maximum int, errors []string, stalled bool)
	ValidateProposal      func(PatchProposal) []string
}

type LogicalArtifacts struct {
	RequirementAtoms        dsl.V05RequirementAtomsFile
	FunctionalDecomposition dsl.V05FunctionalDecompositionFile
	CRUDMatrix              dsl.V05CRUDMatrixFile
	ReviewDecisions         dsl.V05ReviewDecisionsFile
	Model                   dsl.Document
}

func ConceptualInputMetrics(opts ConceptualModelOptions) map[string]int {
	compact := compactConceptualOptions(opts, opts.DesignObligations)
	chunks := conceptualObligationChunks(opts)
	maxChunkBytes, totalChunkBytes := 0, 0
	for _, chunk := range chunks {
		chunkBytes := len(conceptualChunkInput(compactConceptualOptions(opts, chunk.Obligations), chunk))
		totalChunkBytes += chunkBytes
		if chunkBytes > maxChunkBytes {
			maxChunkBytes = chunkBytes
		}
	}
	return map[string]int{
		"context_bytes":             maxChunkBytes,
		"max_chunk_context_bytes":   maxChunkBytes,
		"total_chunk_context_bytes": totalChunkBytes,
		"monolithic_context_bytes":  len(conceptualModelInput(opts)),
		"full_context_bytes":        conceptualFullContextBytes(opts),
		"chunk_count":               len(chunks),
		"source_units":              len(compact.SourceUnits),
		"requirement_atoms":         len(compact.RequirementAtoms),
		"functional_areas":          len(compact.FunctionalAreas),
		"crud_operations":           len(compact.Operations),
		"design_obligations":        len(compact.DesignObligations),
		"review_decisions":          len(compact.ReviewDecisionContext),
	}
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
	return runChunkedConceptualModel(ctx, client, opts)
}

func conceptualModelInput(opts ConceptualModelOptions) string {
	opts = compactConceptualOptions(opts, opts.DesignObligations)
	promptVersion := resolvedPromptVersion(opts.PromptVersion)
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
			RequirementAtoms: operation.RequirementAtoms,
		})
	}

	return mustCompactJSON(map[string]any{
		"source_units": sourceUnits, "requirement_atoms": atoms,
		"functional_areas": areas, "actors": opts.Actors, "crud_operations": operations,
		"review_decisions": opts.ReviewDecisions, "resolved_review_decisions": opts.ReviewDecisionContext,
		"design_obligations": modelDesignObligationInputs(opts.DesignObligations),
		"output_contract":    "conceptual_model",
		"pipeline_version":   "0.7.2", "template_version": promptVersion,
	})
}

func modelDesignObligationInputs(items []DesignObligation) []modelDesignObligationInput {
	out := make([]modelDesignObligationInput, 0, len(items))
	for _, item := range items {
		out = append(out, modelDesignObligationInput{
			ID: item.ID, Kind: item.Kind, Persistence: item.Persistence,
			RequirementAtoms: item.RequirementAtoms, VerificationTarget: item.VerificationTarget, Risk: item.Risk,
		})
	}
	return out
}

func compactConceptualOptions(opts ConceptualModelOptions, obligations []DesignObligation) ConceptualModelOptions {
	if len(opts.RequirementAtoms) == 0 {
		return opts
	}
	activeAtomIDs := map[string]bool{}
	activeObligations := make([]DesignObligation, 0, len(obligations))
	for _, obligation := range obligations {
		if obligation.Status == "not_required" || obligation.Persistence == "not_required" {
			continue
		}
		activeObligations = append(activeObligations, obligation)
		for _, id := range obligation.RequirementAtoms {
			activeAtomIDs[id] = true
		}
	}
	if len(obligations) == 0 {
		for _, atom := range opts.RequirementAtoms {
			if atom.ModelingOutcome != "intentionally_not_in_db" && atom.ModelingOutcome != "unsupported" {
				activeAtomIDs[atom.ID] = true
			}
		}
	}
	return compactConceptualOptionsForAtoms(opts, activeObligations, activeAtomIDs)
}

func compactConceptualOptionsForAtoms(opts ConceptualModelOptions, obligations []DesignObligation, activeAtomIDs map[string]bool) ConceptualModelOptions {
	var atoms []RequirementAtomProposal
	activeSourceIDs := map[string]bool{}
	for _, atom := range opts.RequirementAtoms {
		if !activeAtomIDs[atom.ID] {
			continue
		}
		atoms = append(atoms, atom)
		for _, id := range atom.SourceUnits {
			activeSourceIDs[id] = true
		}
	}
	var units []dsl.SourceUnit
	for _, unit := range opts.SourceUnits {
		if activeSourceIDs[unit.ID] {
			units = append(units, unit)
		}
	}
	var areas []FunctionalAreaProposal
	for _, area := range opts.FunctionalAreas {
		if intersects(area.Atoms, activeAtomIDs) {
			areas = append(areas, area)
		}
	}
	var operations []CRUDOperationProposal
	activeActorIDs := map[string]bool{}
	for _, operation := range opts.Operations {
		if intersects(operation.RequirementAtoms, activeAtomIDs) {
			operations = append(operations, operation)
			activeActorIDs[operation.ActorID] = true
		}
	}
	for _, area := range areas {
		for _, actorID := range area.MainActors {
			activeActorIDs[actorID] = true
		}
	}
	var actors []ActorProposal
	for _, actor := range opts.Actors {
		if activeActorIDs[actor.ID] {
			actors = append(actors, actor)
		}
	}
	activeDecisionIDs := map[string]bool{}
	for _, atom := range atoms {
		for _, decisionID := range atom.ReviewDecisions {
			activeDecisionIDs[decisionID] = true
		}
	}
	var decisionContext []ConceptualReviewDecisionInput
	for _, decision := range opts.ReviewDecisionContext {
		if intersects(decision.AffectedAtoms, activeAtomIDs) || activeDecisionIDs[decision.ID] {
			decisionContext = append(decisionContext, decision)
			activeDecisionIDs[decision.ID] = true
		}
	}
	var decisions []string
	for _, decisionID := range opts.ReviewDecisions {
		if activeDecisionIDs[decisionID] {
			decisions = append(decisions, decisionID)
		}
	}
	opts.SourceUnits = units
	opts.RequirementAtoms = atoms
	opts.FunctionalAreas = areas
	opts.Operations = operations
	opts.Actors = actors
	opts.ReviewDecisions = decisions
	opts.ReviewDecisionContext = decisionContext
	opts.DesignObligations = obligations
	return opts
}

func intersects(ids []string, selected map[string]bool) bool {
	for _, id := range ids {
		if selected[id] {
			return true
		}
	}
	return false
}

func mergeConceptualModel(base, fragment ConceptualModelProposal) ConceptualModelProposal {
	out := base
	out.EntityConcepts = mergeByID(base.EntityConcepts, fragment.EntityConcepts, func(v ConceptualEntityProposal) string { return v.ID }, mergeConceptualEntity)
	out.Relationships = mergeByID(base.Relationships, fragment.Relationships, func(v ConceptualRelationshipProposal) string { return v.ID }, mergeConceptualRelationship)
	out.ConstraintConcepts = mergeByID(base.ConstraintConcepts, fragment.ConstraintConcepts, func(v ConceptualConstraintProposal) string { return v.ID }, mergeConceptualConstraint)
	out.LifecycleConcepts = mergeByID(base.LifecycleConcepts, fragment.LifecycleConcepts, func(v PlanElementProposal) string { return v.ID }, mergePlanElement)
	out.DerivedConcepts = mergeByID(base.DerivedConcepts, fragment.DerivedConcepts, func(v PlanElementProposal) string { return v.ID }, mergePlanElement)
	out.FileConcepts = mergeByID(base.FileConcepts, fragment.FileConcepts, func(v PlanElementProposal) string { return v.ID }, mergePlanElement)
	out.ImportConcepts = mergeByID(base.ImportConcepts, fragment.ImportConcepts, func(v PlanElementProposal) string { return v.ID }, mergePlanElement)
	out.Warnings = sortedUniqueStrings(append(append([]string(nil), base.Warnings...), fragment.Warnings...))
	out.UnresolvedReviewIDs = sortedUniqueStrings(append(append([]string(nil), base.UnresolvedReviewIDs...), fragment.UnresolvedReviewIDs...))
	out.ConfidenceSummary = map[string]string{}
	for key, value := range base.ConfidenceSummary {
		out.ConfidenceSummary[key] = value
	}
	for key, value := range fragment.ConfidenceSummary {
		out.ConfidenceSummary[key] = value
	}
	return out
}

func mergeConceptualRepair(base, fragment ConceptualModelProposal) ConceptualModelProposal {
	out := mergeConceptualModel(base, fragment)
	if fragment.UnresolvedReviewIDs != nil {
		out.UnresolvedReviewIDs = sortedUniqueStrings(fragment.UnresolvedReviewIDs)
	}
	return out
}

func mergeByID[T any](base, updates []T, id func(T) string, merge func(T, T) T) []T {
	// Keep empty merged collections JSON-stable as [] rather than null. The
	// conceptual-model API is consumed directly by collection-oriented UI code.
	out := append([]T{}, base...)
	index := map[string]int{}
	for i, item := range out {
		index[id(item)] = i
	}
	for _, item := range updates {
		if i, ok := index[id(item)]; ok {
			out[i] = merge(out[i], item)
		} else {
			index[id(item)] = len(out)
			out = append(out, item)
		}
	}
	return out
}

func mergeConceptualEntity(base, update ConceptualEntityProposal) ConceptualEntityProposal {
	out := update
	if out.ID == "" {
		out.ID = base.ID
	}
	if out.Label == "" {
		out.Label = base.Label
	}
	if out.Description == "" {
		out.Description = base.Description
	}
	if out.Kind == "" {
		out.Kind = base.Kind
	}
	out.Evidence = mergeEvidence(base.Evidence, update.Evidence)
	out.Attributes = mergeByID(base.Attributes, update.Attributes, func(v ConceptualAttributeProposal) string { return v.ID }, mergeConceptualAttribute)
	return out
}

func mergeConceptualAttribute(base, update ConceptualAttributeProposal) ConceptualAttributeProposal {
	out := update
	if out.ID == "" {
		out.ID = base.ID
	}
	if out.Label == "" {
		out.Label = base.Label
	}
	if out.Description == "" {
		out.Description = base.Description
	}
	if out.Name == "" {
		out.Name = base.Name
	}
	if out.ValueType == "" {
		out.ValueType = base.ValueType
	}
	if len(out.EnumValues) == 0 {
		out.EnumValues = base.EnumValues
	}
	out.Unique = out.Unique || base.Unique
	out.Evidence = mergeEvidence(base.Evidence, update.Evidence)
	return out
}

func mergeConceptualRelationship(base, update ConceptualRelationshipProposal) ConceptualRelationshipProposal {
	out := update
	if out.ID == "" {
		out.ID = base.ID
	}
	if out.Label == "" {
		out.Label = base.Label
	}
	if out.Description == "" {
		out.Description = base.Description
	}
	if out.From == "" {
		out.From = base.From
	}
	if out.To == "" {
		out.To = base.To
	}
	if out.Cardinality == "" {
		out.Cardinality = base.Cardinality
	}
	if out.Required == nil {
		out.Required = base.Required
	}
	out.Evidence = mergeEvidence(base.Evidence, update.Evidence)
	return out
}

func mergeConceptualConstraint(base, update ConceptualConstraintProposal) ConceptualConstraintProposal {
	out := update
	if out.ID == "" {
		out.ID = base.ID
	}
	if out.Label == "" {
		out.Label = base.Label
	}
	if out.Description == "" {
		out.Description = base.Description
	}
	if out.Kind == "" {
		out.Kind = base.Kind
	}
	out.Targets = sortedUniqueStrings(append(append([]string(nil), base.Targets...), update.Targets...))
	if out.Expression == "" {
		out.Expression = base.Expression
	}
	out.Evidence = mergeEvidence(base.Evidence, update.Evidence)
	return out
}

func mergePlanElement(base, update PlanElementProposal) PlanElementProposal {
	out := update
	if out.ID == "" {
		out.ID = base.ID
	}
	if out.Label == "" {
		out.Label = base.Label
	}
	if out.Description == "" {
		out.Description = base.Description
	}
	if out.TableName == "" {
		out.TableName = base.TableName
	}
	if out.Kind == "" {
		out.Kind = base.Kind
	}
	if out.Owner == "" {
		out.Owner, out.Field, out.Initial = base.Owner, base.Field, base.Initial
	}
	if len(out.States) == 0 {
		out.States, out.Terminal, out.Transitions = base.States, base.Terminal, base.Transitions
	}
	if len(out.Sources) == 0 {
		out.Sources = base.Sources
	}
	if len(out.Metrics) == 0 {
		out.Metrics = base.Metrics
	}
	out.SourceUnits = sortedUniqueStrings(append(append([]string(nil), base.SourceUnits...), update.SourceUnits...))
	out.RequirementAtoms = sortedUniqueStrings(append(append([]string(nil), base.RequirementAtoms...), update.RequirementAtoms...))
	return out
}

func mergeEvidence(base, update EvidenceProposal) EvidenceProposal {
	out := update
	out.SourceUnits = sortedUniqueStrings(append(append([]string(nil), base.SourceUnits...), update.SourceUnits...))
	out.RequirementAtoms = sortedUniqueStrings(append(append([]string(nil), base.RequirementAtoms...), update.RequirementAtoms...))
	out.ReviewDecisions = sortedUniqueStrings(append(append([]string(nil), base.ReviewDecisions...), update.ReviewDecisions...))
	out.Notes = sortedUniqueStrings(append(append([]string(nil), base.Notes...), update.Notes...))
	if out.SupportLevel == "" {
		out.SupportLevel = base.SupportLevel
	}
	if out.Confidence == "" {
		out.Confidence = base.Confidence
	}
	return out
}

func modelSourceUnitInputs(units []dsl.SourceUnit) []modelSourceUnitInput {
	sourceUnits := make([]modelSourceUnitInput, 0, len(units))
	for _, unit := range units {
		normalized := strings.TrimSpace(unit.Text.Normalized)
		if normalized == "" {
			normalized = strings.TrimSpace(unit.Text.Exact)
		}
		item := modelSourceUnitInput{ID: unit.ID, Normalized: normalized}
		if unit.Kind == "structured_example" {
			raw := unit.Text.Exact
			if strings.TrimSpace(raw) == "" {
				raw = normalized
			}
			sketch, observed, repeated := structuredExampleShape(raw)
			item.Normalized = sketch
			item.StructuredShape = &structuredExampleShapeInput{
				ObservedKeys: observed, RepeatedKeys: repeated, LiteralValuesOmitted: true,
				Interpretation: "schema_shape_evidence_only",
			}
		}
		sourceUnits = append(sourceUnits, item)
	}
	return sourceUnits
}

func modelRequirementAtomInputs(requirementAtoms []RequirementAtomProposal) []modelRequirementAtomInput {
	atoms := make([]modelRequirementAtomInput, 0, len(requirementAtoms))
	for _, atom := range requirementAtoms {
		atoms = append(atoms, modelRequirementAtomInput{
			ID: atom.ID, Statement: atom.Statement, ModelingOutcome: atom.ModelingOutcome,
			PersistenceEffect: atom.PersistenceEffect, ExampleRole: atom.ExampleRole,
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
	var proposal PatchProposal
	var qa StageQA
	var err error
	if opts.PreviousProposal == nil && len(opts.ConceptualModel.EntityConcepts) > LogicalEntityChunkSize {
		proposal, qa, err = runChunkedLogicalProjection(ctx, client, opts)
		if err == nil && opts.ValidateProposal != nil {
			qa.Errors = opts.ValidateProposal(proposal)
			qa.OK = len(qa.Errors) == 0
			if !qa.OK {
				err = fmt.Errorf("logical projection failed full DB-DSL validation: %s", strings.Join(qa.Errors, "; "))
			}
		}
	} else {
		proposal, qa, err = runLogicalProjectionCall(ctx, client, opts)
	}
	if err == nil && qa.OK {
		return proposal, qa, nil
	}
	if len(qa.Errors) == 0 {
		return proposal, qa, err
	}

	previousErrors := normalizedLogicalErrors(qa.Errors)
	for round := 1; round <= opts.MaxRepairAttempts; round++ {
		if opts.OnRepairProgress != nil {
			opts.OnRepairProgress(round, opts.MaxRepairAttempts, qa.Errors, false)
		}
		repair := opts
		if opts.RepairMaxOutputTokens > 0 {
			repair.MaxOutputTokens = opts.RepairMaxOutputTokens
		}
		repair.PreviousProposal = &proposal
		repair.ValidationErrors = append([]string(nil), qa.Errors...)
		repair.ChunkIndex, repair.ChunkCount = 0, 0
		repair.PrimaryConceptIDs = nil
		proposal, qa, err = runLogicalProjectionCall(ctx, client, repair)
		if err == nil && qa.OK {
			return proposal, qa, nil
		}
		if len(qa.Errors) == 0 {
			return proposal, qa, err
		}
		currentErrors := normalizedLogicalErrors(qa.Errors)
		if currentErrors == previousErrors {
			if opts.OnRepairProgress != nil {
				opts.OnRepairProgress(round, opts.MaxRepairAttempts, qa.Errors, true)
			}
			return proposal, qa, fmt.Errorf("logical projection repair_stalled after round %d: %s", round, strings.Join(qa.Errors, "; "))
		}
		previousErrors = currentErrors
	}
	return proposal, qa, fmt.Errorf("logical projection exhausted %d repair attempts: %s", opts.MaxRepairAttempts, strings.Join(qa.Errors, "; "))
}

func normalizedLogicalErrors(errors []string) string {
	items := append([]string(nil), errors...)
	for index := range items {
		items[index] = strings.Join(strings.Fields(strings.ToLower(items[index])), " ")
	}
	sort.Strings(items)
	return strings.Join(items, "|")
}

func runLogicalProjectionCall(ctx context.Context, client llm.Client, opts LogicalProjectionOptions) (PatchProposal, StageQA, error) {
	input := logicalProjectionInput(opts)
	fullInput := mustJSON(map[string]any{"conceptual_model": opts.ConceptualModel, "source_units": opts.SourceUnits, "requirement_atoms": opts.RequirementAtoms, "review_decisions": opts.ReviewDecisionContext, "design_obligations": opts.DesignObligations, "previous_proposal": opts.PreviousProposal, "validation_errors": opts.ValidationErrors})
	fullContextBytes := len(fullInput)
	if opts.FullContextBytes > 0 {
		fullContextBytes = opts.FullContextBytes
	}
	validationScope := "patch_preflight"
	if opts.ValidateProposal != nil && opts.ChunkCount <= 1 {
		validationScope = "full_dbdsl_v05"
	}
	metadata := map[string]string{"template_version": resolvedPromptVersion(opts.PromptVersion), "full_context_bytes": fmt.Sprint(fullContextBytes), "context_policy": "minimal_context_v1", "canonicalizer_version": "domain_slugs_v1", "call_reason": logicalCallReason(opts), "issue_id": logicalIssueID(opts), "validation_scope": validationScope, "cache_policy": "full_validation_only"}
	instructions := logicalProjectionInstructions
	if opts.ChunkCount > 1 {
		metadata["run_key"] = fmt.Sprintf("chunk_%03d", opts.ChunkIndex)
		metadata["chunk_index"] = fmt.Sprint(opts.ChunkIndex)
		metadata["chunk_count"] = fmt.Sprint(opts.ChunkCount)
		metadata["context_policy"] = "logical_entity_chunks_v1"
		instructions = logicalProjectionChunkInstructions
	}
	sourceIDs := sourceUnitIDSet(opts.SourceUnits)
	atomIDs := atomIDSet(opts.RequirementAtoms)
	var proposal PatchProposal
	var merged PatchProposal
	qa := newStageQA(nil)
	err := runStructuredStage(ctx, client, opts.OutDir, 9, llm.Request{
		Stage: "logical_projection", Model: opts.Model, Instructions: instructions,
		Input: input, SchemaName: "DBDSLLogicalProjection", Schema: patchSchema(), ReasoningEffort: opts.ReasoningEffort,
		MaxOutputTokens: opts.MaxOutputTokens, Metadata: metadata,
	}, &proposal, func() []string {
		candidate := proposal
		if opts.PreviousProposal != nil && len(opts.ValidationErrors) > 0 {
			candidate = mergeLogicalPatch(*opts.PreviousProposal, proposal)
			merged = candidate
		}
		qa.Errors = validatePatchProposal(candidate, sourceIDs, atomIDs)
		if len(qa.Errors) == 0 && opts.ValidateProposal != nil && opts.ChunkCount <= 1 {
			qa.Errors = opts.ValidateProposal(candidate)
		}
		qa.OK = len(qa.Errors) == 0
		return qa.Errors
	})
	if err != nil {
		if opts.PreviousProposal != nil && len(opts.ValidationErrors) > 0 && len(merged.Operations) > 0 {
			proposal = merged
		}
		return proposal, qa, err
	}
	if opts.PreviousProposal != nil && len(opts.ValidationErrors) > 0 {
		proposal = merged
	}
	qa.Errors = validatePatchProposal(proposal, sourceIDs, atomIDs)
	if len(qa.Errors) == 0 && opts.ValidateProposal != nil && opts.ChunkCount <= 1 {
		qa.Errors = opts.ValidateProposal(proposal)
	}
	qa.OK = len(qa.Errors) == 0
	qa.Coverage["patch_operations"] = len(proposal.Operations)
	return proposal, qa, nil
}

func logicalCallReason(opts LogicalProjectionOptions) string {
	if opts.PreviousProposal != nil && len(opts.ValidationErrors) > 0 {
		return "semantic_repair"
	}
	return "logical_projection"
}

func logicalIssueID(opts LogicalProjectionOptions) string {
	if len(opts.ValidationErrors) == 0 {
		return ""
	}
	return textHash(strings.Join(opts.ValidationErrors, "|"))
}

func logicalProjectionInput(opts LogicalProjectionOptions) string {
	opts = compactLogicalOptions(opts)
	input := map[string]any{
		"source_units": modelSourceUnitInputs(opts.SourceUnits), "requirement_atoms": modelRequirementAtomInputs(opts.RequirementAtoms),
		"review_decisions": opts.ReviewDecisions, "resolved_review_decisions": opts.ReviewDecisionContext,
		"design_obligations": modelDesignObligationInputs(opts.DesignObligations),
		"output_contract":    "logical_projection", "pipeline_version": "0.7.2", "template_version": resolvedPromptVersion(opts.PromptVersion),
		"constraint_reference_catalog": logicalConstraintReferenceCatalog(opts),
	}
	if opts.ChunkCount > 1 {
		input["output_contract"] = "logical_projection_fragment"
		input["chunk_scope"] = map[string]any{"index": opts.ChunkIndex, "count": opts.ChunkCount, "primary_concept_ids": opts.PrimaryConceptIDs}
	}
	if opts.PreviousProposal != nil && len(opts.ValidationErrors) > 0 {
		input["repair_mode"] = true
		input["previous_proposal"] = focusedLogicalRepairPatch(*opts.PreviousProposal, opts.ValidationErrors)
		input["existing_operation_registry"] = logicalOperationRegistry(*opts.PreviousProposal)
		input["validation_errors"] = opts.ValidationErrors
		input["output_contract"] = "logical_projection_repair_fragment"
	} else {
		input["conceptual_model"] = opts.ConceptualModel
	}
	return mustCompactJSON(input)
}

func logicalConstraintReferenceCatalog(opts LogicalProjectionOptions) []map[string]any {
	entities := map[string][]string{}
	relationships := []RelationshipProposal{}
	if opts.PreviousProposal != nil {
		for _, operation := range opts.PreviousProposal.Operations {
			if operation.Entity != nil {
				for _, attribute := range operation.Entity.Attributes {
					entities[operation.Entity.ID] = append(entities[operation.Entity.ID], attribute.ID)
				}
			}
			if operation.Relationship != nil {
				relationships = append(relationships, *operation.Relationship)
			}
		}
	} else {
		for _, entity := range opts.ConceptualModel.EntityConcepts {
			for _, attribute := range entity.Attributes {
				entities[entity.ID] = append(entities[entity.ID], attribute.ID)
			}
		}
		for _, relationship := range opts.ConceptualModel.Relationships {
			relationships = append(relationships, RelationshipProposal{
				ID: relationship.ID, From: relationship.From, To: relationship.To, Cardinality: relationship.Cardinality,
			})
		}
	}

	entries := make([]map[string]any, 0, len(entities)+len(relationships))
	entityIDs := make([]string, 0, len(entities))
	for entityID := range entities {
		entityIDs = append(entityIDs, entityID)
	}
	sort.Strings(entityIDs)
	for _, entityID := range entityIDs {
		attributes := append([]string(nil), entities[entityID]...)
		sort.Strings(attributes)
		entries = append(entries, map[string]any{"kind": "entity", "entity_id": entityID, "scalar_attributes": attributes})
	}
	for _, relationship := range relationships {
		foreignKeys := dsl.RelationshipForeignKeys(dsl.Relationship{
			ID: relationship.ID, From: relationship.From, To: relationship.To, Cardinality: relationship.Cardinality, Through: relationship.Through,
		})
		for _, fk := range foreignKeys {
			entries = append(entries, map[string]any{
				"kind": "relationship_fk", "relationship_id": relationship.ID, "owner": fk.OwnerEntityID,
				"generated_field": fk.Field, "cardinality": relationship.Cardinality,
				"required": relationship.Required, "fk_required": relationship.FKRequired,
			})
		}
	}
	return entries
}

func focusedLogicalRepairPatch(previous PatchProposal, validationErrors []string) PatchProposal {
	joined := strings.ToLower(strings.Join(validationErrors, "\n"))
	focused := PatchProposal{Warnings: []string{}, UnresolvedQuestions: []string{}, ConfidenceSummary: previous.ConfidenceSummary}
	for _, operation := range previous.Operations {
		for _, term := range patchOperationSearchTerms(operation) {
			if term != "" && strings.Contains(joined, strings.ToLower(term)) {
				focused.Operations = append(focused.Operations, operation)
				break
			}
		}
	}
	return focused
}

func logicalOperationRegistry(previous PatchProposal) []map[string]string {
	registry := make([]map[string]string, 0, len(previous.Operations))
	for _, operation := range previous.Operations {
		terms := patchOperationSearchTerms(operation)
		if len(terms) == 0 || terms[0] == "" {
			continue
		}
		item := map[string]string{"operation": operation.Operation, "id": terms[0]}
		if len(terms) > 1 && terms[1] != "" {
			item["owner_or_endpoint"] = terms[1]
		}
		registry = append(registry, item)
	}
	sort.SliceStable(registry, func(i, j int) bool {
		if registry[i]["operation"] == registry[j]["operation"] {
			return registry[i]["id"] < registry[j]["id"]
		}
		return registry[i]["operation"] < registry[j]["operation"]
	})
	return registry
}

func patchOperationSearchTerms(operation PatchOperation) []string {
	switch {
	case operation.Entity != nil:
		return []string{operation.Entity.ID, operation.Entity.TableName}
	case operation.Relationship != nil:
		return []string{operation.Relationship.ID, operation.Relationship.From, operation.Relationship.To}
	case operation.Constraint != nil:
		return []string{operation.Constraint.ID, operation.Constraint.Owner, operation.Constraint.Field}
	case operation.StateMachine != nil:
		return []string{operation.StateMachine.ID, operation.StateMachine.Owner}
	case operation.DerivedView != nil:
		return []string{operation.DerivedView.ID}
	case operation.FileSpec != nil:
		return []string{operation.FileSpec.ID, operation.FileSpec.Owner, operation.FileSpec.Field}
	case operation.ImportSpec != nil:
		return []string{operation.ImportSpec.ID}
	case operation.Operation == "remove_operation":
		return []string{operation.TargetID, operation.TargetOperation}
	default:
		return nil
	}
}

func patchOperationKey(operation PatchOperation) string {
	if operation.Operation == "remove_operation" {
		if operation.TargetOperation == "" || operation.TargetID == "" {
			return ""
		}
		return operation.TargetOperation + ":" + operation.TargetID
	}
	terms := patchOperationSearchTerms(operation)
	if len(terms) == 0 || strings.TrimSpace(terms[0]) == "" {
		return ""
	}
	return operation.Operation + ":" + terms[0]
}

func mergeLogicalPatch(base, fragment PatchProposal) PatchProposal {
	out := PatchProposal{Warnings: append([]string(nil), base.Warnings...), UnresolvedQuestions: append([]string(nil), base.UnresolvedQuestions...), ConfidenceSummary: base.ConfidenceSummary}
	index := map[string]int{}
	for _, operation := range base.Operations {
		if operation.Operation == "remove_operation" {
			continue
		}
		key := patchOperationKey(operation)
		if key == "" {
			continue
		}
		index[key] = len(out.Operations)
		out.Operations = append(out.Operations, operation)
	}
	for _, operation := range fragment.Operations {
		if operation.Operation == "remove_operation" {
			key := patchOperationKey(operation)
			if i, ok := index[key]; ok {
				out.Operations = append(out.Operations[:i], out.Operations[i+1:]...)
				index = map[string]int{}
				for nextIndex, existing := range out.Operations {
					index[patchOperationKey(existing)] = nextIndex
				}
			}
		}
	}
	for _, operation := range fragment.Operations {
		if operation.Operation == "remove_operation" {
			continue
		}
		key := patchOperationKey(operation)
		if key == "" {
			continue
		}
		if i, ok := index[key]; ok {
			out.Operations[i] = operation
		} else {
			index[key] = len(out.Operations)
			out.Operations = append(out.Operations, operation)
		}
	}
	out.Warnings = append(out.Warnings, fragment.Warnings...)
	if fragment.UnresolvedQuestions != nil {
		out.UnresolvedQuestions = fragment.UnresolvedQuestions
	}
	if fragment.ConfidenceSummary != nil {
		out.ConfidenceSummary = fragment.ConfidenceSummary
	}
	return out
}

func compactLogicalOptions(opts LogicalProjectionOptions) LogicalProjectionOptions {
	activeAtomIDs := map[string]bool{}
	collectEvidence := func(evidence EvidenceProposal) {
		for _, atomID := range evidence.RequirementAtoms {
			activeAtomIDs[atomID] = true
		}
	}
	for _, entity := range opts.ConceptualModel.EntityConcepts {
		collectEvidence(entity.Evidence)
		for _, attribute := range entity.Attributes {
			collectEvidence(attribute.Evidence)
		}
	}
	for _, relationship := range opts.ConceptualModel.Relationships {
		collectEvidence(relationship.Evidence)
	}
	for _, constraint := range opts.ConceptualModel.ConstraintConcepts {
		collectEvidence(constraint.Evidence)
	}
	for _, group := range [][]PlanElementProposal{opts.ConceptualModel.LifecycleConcepts, opts.ConceptualModel.DerivedConcepts, opts.ConceptualModel.FileConcepts, opts.ConceptualModel.ImportConcepts} {
		for _, item := range group {
			for _, atomID := range item.RequirementAtoms {
				activeAtomIDs[atomID] = true
			}
		}
	}
	var obligations []DesignObligation
	for _, obligation := range opts.DesignObligations {
		if obligation.Status == "not_required" || obligation.Persistence == "not_required" {
			continue
		}
		obligations = append(obligations, obligation)
		for _, id := range obligation.RequirementAtoms {
			activeAtomIDs[id] = true
		}
	}
	if len(opts.DesignObligations) == 0 {
		for _, atom := range opts.RequirementAtoms {
			if atom.ModelingOutcome != "intentionally_not_in_db" && atom.ModelingOutcome != "unsupported" {
				activeAtomIDs[atom.ID] = true
			}
		}
	}
	activeSourceIDs := map[string]bool{}
	var atoms []RequirementAtomProposal
	for _, atom := range opts.RequirementAtoms {
		if !activeAtomIDs[atom.ID] {
			continue
		}
		atoms = append(atoms, atom)
		for _, id := range atom.SourceUnits {
			activeSourceIDs[id] = true
		}
	}
	var units []dsl.SourceUnit
	for _, unit := range opts.SourceUnits {
		if activeSourceIDs[unit.ID] {
			units = append(units, unit)
		}
	}
	opts.RequirementAtoms = atoms
	opts.SourceUnits = units
	opts.DesignObligations = obligations
	activeDecisionIDs := map[string]bool{}
	for _, atom := range atoms {
		for _, decisionID := range atom.ReviewDecisions {
			activeDecisionIDs[decisionID] = true
		}
	}
	var decisionContext []ConceptualReviewDecisionInput
	for _, decision := range opts.ReviewDecisionContext {
		if intersects(decision.AffectedAtoms, activeAtomIDs) || activeDecisionIDs[decision.ID] {
			decisionContext = append(decisionContext, decision)
			activeDecisionIDs[decision.ID] = true
		}
	}
	var decisions []string
	for _, decisionID := range opts.ReviewDecisions {
		if activeDecisionIDs[decisionID] {
			decisions = append(decisions, decisionID)
		}
	}
	opts.ReviewDecisions = decisions
	opts.ReviewDecisionContext = decisionContext
	return opts
}

func ValidateConceptualModel(proposal ConceptualModelProposal, units []dsl.SourceUnit, atoms []RequirementAtomProposal, reviewDecisions []string) StageQA {
	return ValidateConceptualModelWithObligations(proposal, units, atoms, reviewDecisions, nil)
}

// validateConceptLabel rejects labels that carry model commentary instead of a
// short business name; such labels leak into every downstream view and export.
func validateConceptLabel(qa *StageQA, owner, label string) {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		qa.Errors = append(qa.Errors, owner+" has an empty label")
		return
	}
	if strings.ContainsAny(trimmed, "?!") || len(strings.Fields(trimmed)) > 6 {
		qa.Errors = append(qa.Errors, fmt.Sprintf("%s label %q must be a short business name (at most 6 words, no commentary)", owner, trimmed))
	}
}

func ValidateConceptualModelWithObligations(proposal ConceptualModelProposal, units []dsl.SourceUnit, atoms []RequirementAtomProposal, reviewDecisions []string, obligations []DesignObligation) StageQA {
	atoms = NormalizeRequirementReviewSemantics(atoms)
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
		validateConceptLabel(&qa, concept.ID, concept.Label)
		attributeIDs := map[string]bool{}
		columnNames := map[string]string{}
		for _, attribute := range concept.Attributes {
			if attribute.ValueType != "" && !containsString(ConceptualValueTypes, attribute.ValueType) {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s.%s has unsupported value_type %q", concept.ID, attribute.ID, attribute.ValueType))
			}
			if name := attribute.Name; name != "" {
				if !lowerSnakeIdentifierPattern.MatchString(name) {
					qa.Errors = append(qa.Errors, fmt.Sprintf("%s.%s name %q must be a lower snake_case column name", concept.ID, attribute.ID, name))
				} else if other := columnNames[name]; other != "" {
					// The deterministic mapper merges same-named columns; flag it without forcing an LLM repair.
					qa.Warnings = append(qa.Warnings, fmt.Sprintf("%s attributes %s and %s both use column name %q and will be merged", concept.ID, other, attribute.ID, name))
				}
				columnNames[name] = attribute.ID
			}
			if attribute.ID == "" || attributeIDs[attribute.ID] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s has empty or duplicate attribute %q", concept.ID, attribute.ID))
			}
			attributeIDs[attribute.ID] = true
			validateEvidence(concept.ID+"."+attribute.ID, attribute.Evidence)
			validateConceptLabel(&qa, concept.ID+"."+attribute.ID, attribute.Label)
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
	for _, lifecycle := range proposal.LifecycleConcepts {
		if lifecycle.Owner != "" && !concepts[lifecycle.Owner] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("lifecycle %s owner %s is not an entity concept", lifecycle.ID, lifecycle.Owner))
		}
		if len(lifecycle.States) > 0 && lifecycle.Initial != "" && !containsString(lifecycle.States, lifecycle.Initial) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("lifecycle %s initial state %q is not one of its states", lifecycle.ID, lifecycle.Initial))
		}
	}
	constraintAtoms := map[string]bool{}
	constraintIDs := map[string]bool{}
	for _, constraint := range proposal.ConstraintConcepts {
		if constraint.ID == "" || constraintIDs[constraint.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate constraint concept id %q", constraint.ID))
		}
		constraintIDs[constraint.ID] = true
		if len(constraint.Targets) == 0 {
			qa.Errors = append(qa.Errors, constraint.ID+" has no target")
		}
		validateEvidence(constraint.ID, constraint.Evidence)
		for _, atomID := range constraint.Evidence.RequirementAtoms {
			constraintAtoms[atomID] = true
		}
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
	for _, constraint := range proposal.ConstraintConcepts {
		collectEvidence(constraint.Evidence)
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
			if obligation.Kind == "invariant" || obligation.Kind == "security" {
				covered = covered || constraintAtoms[atomID]
			} else {
				covered = covered || representedAtoms[atomID]
			}
		}
		if !covered {
			qa.Errors = append(qa.Errors, fmt.Sprintf("design obligation %s is not represented in the conceptual model", obligation.ID))
		} else {
			coveredObligations++
		}
	}
	for _, atom := range atoms {
		if atom.ModelingOutcome == "represented" && !representedAtoms[atom.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("represented requirement atom %s has no conceptual model evidence", atom.ID))
		}
	}
	if len(proposal.UnresolvedReviewIDs) > 0 {
		qa.Errors = append(qa.Errors, "conceptual model retains unresolved review IDs: "+strings.Join(proposal.UnresolvedReviewIDs, ", "))
	}
	qa.Coverage["entity_concepts"] = len(proposal.EntityConcepts)
	qa.Coverage["relationships"] = len(proposal.Relationships)
	qa.Coverage["constraint_concepts"] = len(proposal.ConstraintConcepts)
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
