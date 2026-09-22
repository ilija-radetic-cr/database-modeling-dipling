package llmpipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

func TestConceptualModelInputIsCompactAndStageSpecific(t *testing.T) {
	input := conceptualModelInput(ConceptualModelOptions{
		SourceUnits: []dsl.SourceUnit{{
			ID: "SU-001", Kind: "requirement_sentence", Section: "registration", Location: "line 1",
			Relevance: "model_relevant", Tags: []string{"account"},
			Text: dsl.SourceUnitText{Exact: "Exact source text.", Normalized: "Normalized source text."},
		}},
		RequirementAtoms: []RequirementAtomProposal{{
			ID: "RA-001", Statement: "An account is stored.", AtomType: "data_requirement",
			ModelingRelevance: "direct_db", SourceUnits: []string{"SU-001"}, FunctionalArea: "FA-IDENTITY",
			FunctionalPattern: "domain_management", SupportLevel: "explicit", Confidence: "high",
			RequiresReview: false, ModelingOutcome: "represented", Warnings: []string{"resolved warning"},
			ReviewDecisions: []string{"RD-001"},
		}},
		FunctionalAreas: []FunctionalAreaProposal{{
			ID: "FA-IDENTITY", Label: "Identity", Purpose: "Manage accounts.", MainActors: []string{"ACT-USER"},
			Atoms: []string{"RA-001"}, ModelingFocus: []string{"account"}, Confidence: "high", Warnings: []string{"resolved warning"},
		}},
		Actors: []ActorProposal{{ID: "ACT-USER", Label: "User", Description: "Registered user.", Kind: "human"}},
		Operations: []CRUDOperationProposal{{
			ID: "OP-001", Label: "Register", ActorID: "ACT-USER", FunctionalAreaID: "FA-IDENTITY",
			Creates: []string{"account"}, Reads: []string{}, Updates: []string{}, Deletes: []string{},
			PersistentData: []string{"account"}, Outcome: "Account created.",
			RequirementAtoms: []string{"RA-001"}, SourceUnits: []string{"SU-001"},
			RequiresReview: false, Warnings: []string{"resolved warning"},
		}},
		ReviewDecisions: []string{"RD-001"},
		ReviewDecisionContext: []ConceptualReviewDecisionInput{{
			ID: "RD-001", Question: "Is the account global?", SelectedOptionID: "RC-001-O1",
			Rationale: "A global account matches the login requirement.", AffectedAtoms: []string{"RA-001"},
		}},
	})

	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(input)); err != nil {
		t.Fatalf("conceptual input is not valid JSON: %v", err)
	}
	if input != compact.String() {
		t.Fatal("conceptual input must use compact JSON")
	}

	var parsed struct {
		SourceUnits      []map[string]any `json:"source_units"`
		RequirementAtoms []map[string]any `json:"requirement_atoms"`
		FunctionalAreas  []map[string]any `json:"functional_areas"`
		CRUDOperations   []map[string]any `json:"crud_operations"`
		ReviewDecisions  []map[string]any `json:"resolved_review_decisions"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("decode conceptual input: %v", err)
	}
	assertFields(t, parsed.SourceUnits[0], "id", "normalized")
	assertFields(t, parsed.RequirementAtoms[0], "condition", "functional_area", "id", "modeling_outcome", "object", "ownership", "predicate", "quantifier", "review_decisions", "source_units", "statement", "subject", "temporal_semantics")
	assertFields(t, parsed.FunctionalAreas[0], "atoms", "id", "label", "main_actors", "modeling_focus", "purpose")
	assertFields(t, parsed.CRUDOperations[0], "actor_id", "creates", "deletes", "functional_area_id", "id", "label", "outcome", "persistent_data", "reads", "requirement_atoms", "updates")
	assertFields(t, parsed.ReviewDecisions[0], "affected_atoms", "id", "question", "rationale", "selected_option_id")
	if parsed.SourceUnits[0]["normalized"] != "Normalized source text." {
		t.Fatalf("normalized source text was not preserved: %#v", parsed.SourceUnits[0])
	}
	if parsed.CRUDOperations[0]["persistent_data"] == nil {
		t.Fatal("CRUD persistent_data must be preserved for conceptual modeling")
	}
}

func TestConceptualModelInputFallsBackToExactSourceText(t *testing.T) {
	input := conceptualModelInput(ConceptualModelOptions{SourceUnits: []dsl.SourceUnit{{
		ID: "SU-001", Text: dsl.SourceUnitText{Exact: "Fallback source text."},
	}}})
	var parsed struct {
		SourceUnits []modelSourceUnitInput `json:"source_units"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("decode conceptual input: %v", err)
	}
	if got := parsed.SourceUnits[0].Normalized; got != "Fallback source text." {
		t.Fatalf("normalized fallback = %q, want exact source text", got)
	}
}

func TestValidateConceptualModelAllowsEntityToFileConceptRelationship(t *testing.T) {
	evidence := EvidenceProposal{
		SourceUnits: []string{"SU-001"}, RequirementAtoms: []string{"RA-001"},
		SupportLevel: "explicit", Confidence: "high",
	}
	proposal := ConceptualModelProposal{
		EntityConcepts: []ConceptualEntityProposal{{
			ID: "E-REGISTRATION", Label: "Registration", Kind: "regular", Evidence: evidence,
		}},
		FileConcepts: []PlanElementProposal{{
			ID: "FC-001", Label: "Profile image", Kind: "profile_image",
			SourceUnits: []string{"SU-001"}, RequirementAtoms: []string{"RA-001"},
		}},
		Relationships: []ConceptualRelationshipProposal{{
			ID: "R-REG-IMAGE", From: "E-REGISTRATION", To: "FC-001",
			Cardinality: "one_to_one", Evidence: evidence,
		}},
	}
	units := []dsl.SourceUnit{{ID: "SU-001"}}
	atoms := []RequirementAtomProposal{{ID: "RA-001"}}

	qa := ValidateConceptualModel(proposal, units, atoms, nil)
	if !qa.OK || len(qa.Errors) != 0 {
		t.Fatalf("entity-to-file relationship must be valid: %+v", qa)
	}

	proposal.Relationships[0].From = "E-UNKNOWN"
	qa = ValidateConceptualModel(proposal, units, atoms, nil)
	if qa.OK || len(qa.Errors) != 1 || qa.Errors[0] != "R-REG-IMAGE references unknown relationship endpoint" {
		t.Fatalf("unknown endpoint must remain invalid: %+v", qa)
	}
}

func TestConceptualSecurityObligationRequiresConstraintConcept(t *testing.T) {
	evidence := EvidenceProposal{SourceUnits: []string{"SU-001"}, RequirementAtoms: []string{"RA-001"}, SupportLevel: "explicit", Confidence: "high"}
	proposal := ConceptualModelProposal{EntityConcepts: []ConceptualEntityProposal{{ID: "E-ACCOUNT", Label: "Account", Kind: "regular", Evidence: evidence}}}
	units := []dsl.SourceUnit{{ID: "SU-001"}}
	atoms := []RequirementAtomProposal{{ID: "RA-001"}}
	obligations := []DesignObligation{{ID: "DO-0001", Kind: "security", Persistence: "required", Status: "accepted", RequirementAtoms: []string{"RA-001"}}}
	if qa := ValidateConceptualModelWithObligations(proposal, units, atoms, nil, obligations); qa.OK {
		t.Fatal("security citation on an entity alone must not satisfy a security obligation")
	}
	proposal.ConstraintConcepts = []ConceptualConstraintProposal{{ID: "CC-PASSWORD", Label: "Password policy", Kind: "security", Targets: []string{"E-ACCOUNT"}, Evidence: evidence}}
	if qa := ValidateConceptualModelWithObligations(proposal, units, atoms, nil, obligations); !qa.OK {
		t.Fatalf("explicit security constraint must satisfy the obligation: %+v", qa)
	}
}

func TestConceptualInputExcludesNotRequiredEvidence(t *testing.T) {
	opts := ConceptualModelOptions{
		SourceUnits:       []dsl.SourceUnit{{ID: "SU-001", Text: dsl.SourceUnitText{Exact: "Store account."}}, {ID: "SU-002", Text: dsl.SourceUnitText{Exact: "Red is an example."}}},
		RequirementAtoms:  []RequirementAtomProposal{{ID: "RA-001", Statement: "Store account.", SourceUnits: []string{"SU-001"}}, {ID: "RA-002", Statement: "Red is an example.", SourceUnits: []string{"SU-002"}}},
		FunctionalAreas:   []FunctionalAreaProposal{{ID: "FA-1", Atoms: []string{"RA-001", "RA-002"}}},
		Operations:        []CRUDOperationProposal{{ID: "OP-1", RequirementAtoms: []string{"RA-001"}}, {ID: "OP-2", RequirementAtoms: []string{"RA-002"}}},
		DesignObligations: []DesignObligation{{ID: "DO-1", Persistence: "required", RequirementAtoms: []string{"RA-001"}}, {ID: "DO-2", Persistence: "not_required", Status: "not_required", RequirementAtoms: []string{"RA-002"}}},
	}
	var parsed struct {
		SourceUnits []modelSourceUnitInput         `json:"source_units"`
		Atoms       []modelRequirementAtomInput    `json:"requirement_atoms"`
		Operations  []conceptualCRUDOperationInput `json:"crud_operations"`
		Obligations []modelDesignObligationInput   `json:"design_obligations"`
	}
	if err := json.Unmarshal([]byte(conceptualModelInput(opts)), &parsed); err != nil {
		t.Fatalf("decode compact conceptual input: %v", err)
	}
	if len(parsed.SourceUnits) != 1 || parsed.SourceUnits[0].ID != "SU-001" || len(parsed.Atoms) != 1 || len(parsed.Operations) != 1 || len(parsed.Obligations) != 1 {
		t.Fatalf("not-required evidence leaked into conceptual input: %+v", parsed)
	}
}

func TestStructuredStageCacheReusesValidatedConceptualResponse(t *testing.T) {
	units := []dsl.SourceUnit{{ID: "SU-001", Text: dsl.SourceUnitText{Exact: "Products have names."}}}
	atoms := []RequirementAtomProposal{{ID: "RA-001", Statement: "Products have names.", SourceUnits: []string{"SU-001"}, SupportLevel: "explicit", Confidence: "high", ModelingOutcome: "represented"}}
	opts := ConceptualModelOptions{OutDir: t.TempDir(), SourceUnits: units, RequirementAtoms: atoms, FunctionalAreas: []FunctionalAreaProposal{{ID: "core", Atoms: []string{"RA-001"}}}, Actors: []ActorProposal{{ID: "system"}}, Operations: []CRUDOperationProposal{{ID: "OP-001", RequirementAtoms: []string{"RA-001"}}}, Model: "mock-model"}
	mock := llm.NewDefaultMockClient()
	if _, _, err := RunConceptualModel(context.Background(), mock, opts); err != nil {
		t.Fatalf("first conceptual call: %v", err)
	}
	var first RunSummary
	readJSONTestFile(t, opts.OutDir+"/llm_runs/008_conceptual_model/run.json", &first)
	if _, err := os.Stat(opts.OutDir + "/llm_cache/" + strings.TrimPrefix(first.CacheKey, "sha256:") + ".json"); err != nil {
		t.Fatalf("validated response was not cached: %v", err)
	}
	if _, _, err := RunConceptualModel(context.Background(), mock, opts); err != nil {
		t.Fatalf("cached conceptual call: %v", err)
	}
	var summary RunSummary
	readJSONTestFile(t, opts.OutDir+"/llm_runs/008_conceptual_model_attempt_002/run.json", &summary)
	if !summary.Cached || summary.CacheKey == "" || summary.ContextBytes == 0 || summary.CacheKey != first.CacheKey {
		t.Fatalf("validated cache hit was not recorded: first=%+v second=%+v", first, summary)
	}
}

func TestConceptualModelUsesBoundedCachedChunksAndPreservesCoverage(t *testing.T) {
	outDir := t.TempDir()
	opts := ConceptualModelOptions{OutDir: outDir, Model: "mock-model", MaxOutputTokens: 32000}
	areaAtoms := map[string][]string{"FA-001": {}, "FA-002": {}}
	for index := 1; index <= 30; index++ {
		atomID := fmt.Sprintf("RA-%03d", index)
		sourceID := fmt.Sprintf("SU-%03d", index)
		areaID := "FA-001"
		if index > 15 {
			areaID = "FA-002"
		}
		areaAtoms[areaID] = append(areaAtoms[areaID], atomID)
		opts.SourceUnits = append(opts.SourceUnits, dsl.SourceUnit{ID: sourceID, Text: dsl.SourceUnitText{Exact: "Store domain fact."}})
		opts.RequirementAtoms = append(opts.RequirementAtoms, RequirementAtomProposal{
			ID: atomID, Statement: "Store domain fact.", SourceUnits: []string{sourceID},
			SupportLevel: "explicit", Confidence: "high", ModelingOutcome: "represented",
		})
		opts.DesignObligations = append(opts.DesignObligations, DesignObligation{
			ID: fmt.Sprintf("DO-%04d", index), Kind: "attribute", Persistence: "required",
			Status: "accepted", RequirementAtoms: []string{atomID},
		})
	}
	for _, areaID := range []string{"FA-001", "FA-002"} {
		opts.FunctionalAreas = append(opts.FunctionalAreas, FunctionalAreaProposal{ID: areaID, Atoms: areaAtoms[areaID]})
		opts.Operations = append(opts.Operations, CRUDOperationProposal{ID: "OP-" + areaID, FunctionalAreaID: areaID, RequirementAtoms: areaAtoms[areaID]})
	}
	metrics := ConceptualInputMetrics(opts)
	if metrics["chunk_count"] != 2 || metrics["max_chunk_context_bytes"] >= metrics["monolithic_context_bytes"] {
		t.Fatalf("conceptual optimization preview does not describe bounded chunks: %+v", metrics)
	}

	proposal, qa, err := RunConceptualModel(context.Background(), llm.NewDefaultMockClient(), opts)
	if err != nil || !qa.OK {
		t.Fatalf("chunked conceptual model: qa=%+v err=%v", qa, err)
	}
	if len(proposal.EntityConcepts) != 1 || len(proposal.EntityConcepts[0].Evidence.RequirementAtoms) != 30 {
		t.Fatalf("chunk merge lost evidence coverage: %+v", proposal.EntityConcepts)
	}
	for index := 1; index <= 2; index++ {
		runPath := filepath.Join(outDir, "llm_runs", fmt.Sprintf("008_conceptual_model_chunk_%03d", index), "run.json")
		var summary RunSummary
		readJSONTestFile(t, runPath, &summary)
		if !summary.ValidationOK || summary.ContextBytes >= summary.FullContextBytes || summary.MaxOutputTokens > conceptualChunkMaxOutputTokens {
			t.Fatalf("chunk %d was not bounded and valid: %+v", index, summary)
		}
	}

	if _, _, err := RunConceptualModel(context.Background(), llm.NewDefaultMockClient(), opts); err != nil {
		t.Fatalf("cached chunked conceptual model: %v", err)
	}
	for index := 1; index <= 2; index++ {
		runPath := filepath.Join(outDir, "llm_runs", fmt.Sprintf("008_conceptual_model_chunk_%03d_attempt_002", index), "run.json")
		var summary RunSummary
		readJSONTestFile(t, runPath, &summary)
		if !summary.Cached {
			t.Fatalf("chunk %d was not reused from cache: %+v", index, summary)
		}
	}
}

func TestConceptualMergePreservesExistingEvidenceAndAttributes(t *testing.T) {
	base := ConceptualModelProposal{EntityConcepts: []ConceptualEntityProposal{{
		ID: "E-ACCOUNT", Label: "Account", Kind: "regular",
		Evidence: EvidenceProposal{SourceUnits: []string{"SU-001"}, RequirementAtoms: []string{"RA-001"}},
		Attributes: []ConceptualAttributeProposal{{
			ID: "name", Label: "Name", Required: true,
			Evidence: EvidenceProposal{SourceUnits: []string{"SU-001"}, RequirementAtoms: []string{"RA-001"}},
		}},
	}}}
	fragment := ConceptualModelProposal{EntityConcepts: []ConceptualEntityProposal{{
		ID: "E-ACCOUNT", Label: "Customer account", Kind: "regular",
		Evidence: EvidenceProposal{SourceUnits: []string{"SU-002"}, RequirementAtoms: []string{"RA-002"}},
		Attributes: []ConceptualAttributeProposal{{
			ID: "email", Label: "Email", Required: true,
			Evidence: EvidenceProposal{SourceUnits: []string{"SU-002"}, RequirementAtoms: []string{"RA-002"}},
		}},
	}}}
	merged := mergeConceptualModel(base, fragment)
	entity := merged.EntityConcepts[0]
	if len(entity.Attributes) != 2 || strings.Join(entity.Evidence.RequirementAtoms, ",") != "RA-001,RA-002" {
		t.Fatalf("merge replaced existing coverage: %+v", entity)
	}
}

func TestConceptualRepairInputIsDeltaScoped(t *testing.T) {
	opts := ConceptualModelOptions{
		SourceUnits:       []dsl.SourceUnit{{ID: "SU-001", Text: dsl.SourceUnitText{Exact: "Store account."}}, {ID: "SU-002", Text: dsl.SourceUnitText{Exact: "Store product."}}},
		RequirementAtoms:  []RequirementAtomProposal{{ID: "RA-001", SourceUnits: []string{"SU-001"}}, {ID: "RA-002", SourceUnits: []string{"SU-002"}}},
		FunctionalAreas:   []FunctionalAreaProposal{{ID: "FA-1", Atoms: []string{"RA-001"}}, {ID: "FA-2", Atoms: []string{"RA-002"}}},
		DesignObligations: []DesignObligation{{ID: "DO-0001", Persistence: "required", RequirementAtoms: []string{"RA-001"}}},
	}
	proposal := ConceptualModelProposal{EntityConcepts: []ConceptualEntityProposal{
		{ID: "E-ACCOUNT", Evidence: EvidenceProposal{SourceUnits: []string{"SU-001"}, RequirementAtoms: []string{"RA-001"}}},
		{ID: "E-PRODUCT", Evidence: EvidenceProposal{SourceUnits: []string{"SU-002"}, RequirementAtoms: []string{"RA-002"}}},
	}}
	scope := conceptualRepairScope{Index: 1, Count: 1, Areas: []string{"FA-1"}, Obligations: opts.DesignObligations, Errors: []string{"design obligation DO-0001 is not represented in the conceptual model"}}
	input, compact := conceptualRepairScopeInput(opts, proposal, scope)
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("decode repair input: %v", err)
	}
	if _, exists := parsed["previous_proposal"]; exists {
		t.Fatal("delta repair must not resend the complete previous proposal")
	}
	var focused ConceptualModelProposal
	if err := json.Unmarshal(parsed["affected_existing_fragment"], &focused); err != nil {
		t.Fatalf("decode focused repair fragment: %v", err)
	}
	if len(focused.EntityConcepts) != 1 || focused.EntityConcepts[0].ID != "E-ACCOUNT" || len(compact.RequirementAtoms) != 1 || compact.RequirementAtoms[0].ID != "RA-001" {
		t.Fatalf("repair input was not issue-scoped: focused=%+v compact=%+v", focused, compact.RequirementAtoms)
	}
}

func TestConceptualChunksFilterUnrelatedActorsAndReviewDecisions(t *testing.T) {
	opts := ConceptualModelOptions{
		SourceUnits: []dsl.SourceUnit{{ID: "SU-001"}, {ID: "SU-002"}},
		RequirementAtoms: []RequirementAtomProposal{
			{ID: "RA-001", SourceUnits: []string{"SU-001"}, ReviewDecisions: []string{"RD-001"}},
			{ID: "RA-002", SourceUnits: []string{"SU-002"}, ReviewDecisions: []string{"RD-002"}},
		},
		FunctionalAreas:       []FunctionalAreaProposal{{ID: "FA-1", Atoms: []string{"RA-001"}, MainActors: []string{"ACT-1"}}, {ID: "FA-2", Atoms: []string{"RA-002"}, MainActors: []string{"ACT-2"}}},
		Actors:                []ActorProposal{{ID: "ACT-1"}, {ID: "ACT-2"}},
		Operations:            []CRUDOperationProposal{{ID: "OP-1", ActorID: "ACT-1", RequirementAtoms: []string{"RA-001"}}, {ID: "OP-2", ActorID: "ACT-2", RequirementAtoms: []string{"RA-002"}}},
		ReviewDecisions:       []string{"RD-001", "RD-002"},
		ReviewDecisionContext: []ConceptualReviewDecisionInput{{ID: "RD-001", AffectedAtoms: []string{"RA-001"}}, {ID: "RD-002", AffectedAtoms: []string{"RA-002"}}},
	}
	compact := compactConceptualOptionsForAtoms(opts, nil, map[string]bool{"RA-001": true})
	if len(compact.Actors) != 1 || compact.Actors[0].ID != "ACT-1" || len(compact.ReviewDecisionContext) != 1 || compact.ReviewDecisionContext[0].ID != "RD-001" {
		t.Fatalf("conceptual chunk retained unrelated context: actors=%+v decisions=%+v", compact.Actors, compact.ReviewDecisionContext)
	}
}

func TestLogicalProjectionInputIncludesConceptualModelOnlyForInitialProjection(t *testing.T) {
	conceptual := ConceptualModelProposal{EntityConcepts: []ConceptualEntityProposal{{ID: "account"}}}
	initial := logicalProjectionInput(LogicalProjectionOptions{ConceptualModel: conceptual})
	var initialInput map[string]json.RawMessage
	if err := json.Unmarshal([]byte(initial), &initialInput); err != nil {
		t.Fatalf("decode initial logical input: %v", err)
	}
	if _, ok := initialInput["conceptual_model"]; !ok {
		t.Fatal("initial logical projection input is missing the conceptual model")
	}

	repair := logicalProjectionInput(LogicalProjectionOptions{
		ConceptualModel:  conceptual,
		PreviousProposal: &PatchProposal{Operations: []PatchOperation{{Operation: "add_entity"}}},
		ValidationErrors: []string{"invalid entity"},
	})
	var repairInput map[string]json.RawMessage
	if err := json.Unmarshal([]byte(repair), &repairInput); err != nil {
		t.Fatalf("decode repair logical input: %v", err)
	}
	if _, ok := repairInput["conceptual_model"]; ok {
		t.Fatal("repair logical projection input duplicates the already-projected conceptual model")
	}
	if _, ok := repairInput["previous_proposal"]; !ok {
		t.Fatal("repair logical projection input is missing the focused previous fragment")
	}
	if _, ok := repairInput["existing_operation_registry"]; !ok {
		t.Fatal("repair logical projection input is missing the lightweight operation registry")
	}
}

func TestLogicalRepairDoesNotFallbackToCompletePatch(t *testing.T) {
	base := PatchProposal{Operations: []PatchOperation{
		{Operation: "add_entity", Entity: &EntityProposal{ID: "Account", TableName: "accounts"}},
		{Operation: "add_entity", Entity: &EntityProposal{ID: "Product", TableName: "products"}},
	}}
	focused := focusedLogicalRepairPatch(base, []string{"generated output violates an unnamed cross-row rule"})
	if len(focused.Operations) != 0 {
		t.Fatalf("unmatched repair error resent the complete patch: %+v", focused)
	}
	registry := logicalOperationRegistry(base)
	if len(registry) != 2 {
		t.Fatalf("lightweight registry lost operation identity: %+v", registry)
	}
}

func TestLogicalRepairFocusAndMergePreserveUnaffectedOperations(t *testing.T) {
	base := PatchProposal{Operations: []PatchOperation{
		{Operation: "add_entity", Entity: &EntityProposal{ID: "Account", TableName: "accounts"}},
		{Operation: "add_entity", Entity: &EntityProposal{ID: "Product", TableName: "products"}},
	}}
	focused := focusedLogicalRepairPatch(base, []string{"entity Product has an invalid attribute"})
	if len(focused.Operations) != 1 || focused.Operations[0].Entity.ID != "Product" {
		t.Fatalf("logical repair was not issue-scoped: %+v", focused)
	}
	fragment := PatchProposal{Operations: []PatchOperation{{Operation: "add_entity", Entity: &EntityProposal{ID: "Product", TableName: "catalog_products"}}}}
	merged := mergeLogicalPatch(base, fragment)
	if len(merged.Operations) != 2 || merged.Operations[0].Entity.TableName != "accounts" || merged.Operations[1].Entity.TableName != "catalog_products" {
		t.Fatalf("logical repair merge lost or failed to replace operations: %+v", merged)
	}
}

func TestLogicalProjectionUsesBoundedEntityChunks(t *testing.T) {
	opts := LogicalProjectionOptions{OutDir: t.TempDir(), Model: "mock-model", MaxOutputTokens: 40000, MaxParallelism: 2}
	for index := 1; index <= LogicalEntityChunkSize+1; index++ {
		atomID := fmt.Sprintf("RA-%03d", index)
		sourceID := fmt.Sprintf("SU-%03d", index)
		entityID := fmt.Sprintf("E-%03d", index)
		evidence := EvidenceProposal{SourceUnits: []string{sourceID}, RequirementAtoms: []string{atomID}}
		opts.SourceUnits = append(opts.SourceUnits, dsl.SourceUnit{ID: sourceID})
		opts.RequirementAtoms = append(opts.RequirementAtoms, RequirementAtomProposal{ID: atomID, SourceUnits: []string{sourceID}, ModelingOutcome: "represented"})
		opts.ConceptualModel.EntityConcepts = append(opts.ConceptualModel.EntityConcepts, ConceptualEntityProposal{ID: entityID, Evidence: evidence})
		opts.DesignObligations = append(opts.DesignObligations, DesignObligation{ID: fmt.Sprintf("DO-%04d", index), Persistence: "required", Status: "accepted", RequirementAtoms: []string{atomID}})
	}
	chunks := logicalProjectionChunks(opts)
	if len(chunks) != 2 || len(chunks[0].PrimaryConceptIDs) != LogicalEntityChunkSize || len(chunks[1].PrimaryConceptIDs) != 1 {
		t.Fatalf("logical chunk partition is not bounded: %+v", chunks)
	}
	patch, qa, err := RunLogicalProjection(context.Background(), llm.NewDefaultMockClient(), opts)
	if err != nil || !qa.OK || len(patch.Operations) == 0 {
		t.Fatalf("chunked logical projection failed: patch=%+v qa=%+v err=%v", patch, qa, err)
	}
	for index := 1; index <= 2; index++ {
		var summary RunSummary
		readJSONTestFile(t, filepath.Join(opts.OutDir, "llm_runs", fmt.Sprintf("009_logical_projection_chunk_%03d", index), "run.json"), &summary)
		if summary.ContextBytes >= summary.FullContextBytes || summary.MaxOutputTokens > 16000 {
			t.Fatalf("logical chunk %d was not bounded: %+v", index, summary)
		}
	}
}

func assertFields(t *testing.T, item map[string]any, expected ...string) {
	t.Helper()
	if len(item) != len(expected) {
		t.Fatalf("fields = %#v, want exactly %v", item, expected)
	}
	for _, field := range expected {
		if _, ok := item[field]; !ok {
			t.Fatalf("field %q is missing from %#v", field, item)
		}
	}
}

func TestConceptualAndLogicalStagesWithMock(t *testing.T) {
	units := []dsl.SourceUnit{{ID: "SU-001", Text: dsl.SourceUnitText{Exact: "Products have names.", Normalized: "Products have names."}}}
	atoms := []RequirementAtomProposal{{ID: "RA-001", Statement: "Products have names.", SourceUnits: []string{"SU-001"}, SupportLevel: "explicit", Confidence: "high", ModelingOutcome: "represented", ReviewDecisions: []string{"RD-001"}}}
	mock := llm.NewDefaultMockClient()
	outDir := t.TempDir()
	conceptual, qa, err := RunConceptualModel(context.Background(), mock, ConceptualModelOptions{OutDir: outDir, SourceUnits: units, RequirementAtoms: atoms,
		FunctionalAreas: []FunctionalAreaProposal{{ID: "core", Atoms: []string{"RA-001"}}}, Actors: []ActorProposal{{ID: "system"}},
		Operations: []CRUDOperationProposal{{ID: "OP-001"}}, ReviewDecisions: []string{"RD-001"}, Model: "mock-model"})
	if err != nil || !qa.OK {
		t.Fatalf("conceptual stage: proposal=%+v qa=%+v err=%v", conceptual, qa, err)
	}
	var conceptualRun RunSummary
	readJSONTestFile(t, outDir+"/llm_runs/008_conceptual_model/run.json", &conceptualRun)
	if conceptualRun.MaxOutputTokens != conceptualChunkBudget(defaultConceptualMaxOutputTokens, 0, len(atoms)) {
		t.Fatalf("conceptual max output tokens = %d, want adaptive chunk budget", conceptualRun.MaxOutputTokens)
	}
	decisionContext := []ConceptualReviewDecisionInput{{
		ID: "RD-001", Question: "Is the name required?", SelectedOptionID: "RC-001-O1",
		Rationale: "The source requires a name.", AffectedAtoms: []string{"RA-001"},
	}}
	previousProposal := PatchProposal{Operations: []PatchOperation{{Operation: "add_entity", Entity: &EntityProposal{ID: "DomainRecord", TableName: "domain_records"}}}}
	patch, patchQA, err := RunLogicalProjection(context.Background(), mock, LogicalProjectionOptions{
		OutDir: outDir, ConceptualModel: conceptual, SourceUnits: units, RequirementAtoms: atoms,
		ReviewDecisions: []string{"RD-001"}, ReviewDecisionContext: decisionContext, Model: "mock-model",
		PreviousProposal: &previousProposal, ValidationErrors: []string{"entity DomainRecord requires repair"},
	})
	if err != nil || !patchQA.OK || len(patch.Operations) == 0 {
		t.Fatalf("logical stage: patch=%+v qa=%+v err=%v", patch, patchQA, err)
	}
	var logicalRun RunSummary
	readJSONTestFile(t, outDir+"/llm_runs/009_logical_projection/run.json", &logicalRun)
	if logicalRun.MaxOutputTokens != defaultLogicalMaxOutputTokens {
		t.Fatalf("logical max output tokens = %d, want %d", logicalRun.MaxOutputTokens, defaultLogicalMaxOutputTokens)
	}
	var logicalRequest struct {
		Input string `json:"input"`
	}
	readJSONTestFile(t, outDir+"/llm_runs/009_logical_projection/request.json", &logicalRequest)
	var compactLogical bytes.Buffer
	if err := json.Compact(&compactLogical, []byte(logicalRequest.Input)); err != nil {
		t.Fatalf("logical input is not valid JSON: %v", err)
	}
	if logicalRequest.Input != compactLogical.String() {
		t.Fatal("logical input must use compact JSON")
	}
	var logicalInput struct {
		ConceptualModel  *ConceptualModelProposal `json:"conceptual_model"`
		SourceUnits      []map[string]any         `json:"source_units"`
		RequirementAtoms []map[string]any         `json:"requirement_atoms"`
		ReviewDecisions  []map[string]any         `json:"resolved_review_decisions"`
		RepairMode       bool                     `json:"repair_mode"`
		ValidationErrors []string                 `json:"validation_errors"`
		PreviousProposal PatchProposal            `json:"previous_proposal"`
	}
	if err := json.Unmarshal([]byte(logicalRequest.Input), &logicalInput); err != nil {
		t.Fatalf("decode logical input: %v", err)
	}
	assertFields(t, logicalInput.SourceUnits[0], "id", "normalized")
	assertFields(t, logicalInput.RequirementAtoms[0], "condition", "functional_area", "id", "modeling_outcome", "object", "ownership", "predicate", "quantifier", "review_decisions", "source_units", "statement", "subject", "temporal_semantics")
	assertFields(t, logicalInput.ReviewDecisions[0], "affected_atoms", "id", "question", "rationale", "selected_option_id")
	if !logicalInput.RepairMode || len(logicalInput.ValidationErrors) != 1 || len(logicalInput.PreviousProposal.Operations) != 1 {
		t.Fatalf("logical repair context was not preserved: %+v", logicalInput)
	}
	if logicalInput.ConceptualModel != nil {
		t.Fatal("repair input must not duplicate the conceptual model already projected by the previous complete patch")
	}
	artifacts, err := BuildLogicalArtifacts("Products", units, RequirementAtomExtractionProposal{RequirementAtoms: atoms}, FunctionalAnalysisProposal{
		FunctionalAreas: []FunctionalAreaProposal{{ID: "core", MainActors: []string{"system"}, Atoms: []string{"RA-001"}}},
		Actors:          []ActorProposal{{ID: "system"}},
	}, CRUDMappingProposal{Operations: []CRUDOperationProposal{{ID: "OP-001", ActorID: "system", FunctionalAreaID: "core", RequirementAtoms: []string{"RA-001"}, SourceUnits: []string{"SU-001"}}}}, patch, nil)
	if err != nil {
		t.Fatalf("build logical artifacts: %v", err)
	}
	if got := artifacts.RequirementAtoms.RequirementAtoms[0].FunctionalArea; got != "core" {
		t.Fatalf("functional-analysis assignment was not projected onto atom: %q", got)
	}
	for _, area := range artifacts.FunctionalDecomposition.FunctionalAreas {
		if area.ID == "core_model" {
			t.Fatal("covered atom produced an unexpected synthetic core_model area")
		}
	}
}

func readJSONTestFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestLogicalArtifactBuilderAddsActorForUncoveredFallbackArea(t *testing.T) {
	actors := ensureReferencedActors(nil, []dsl.FunctionalArea{{ID: "core_model", MainActors: []string{"system"}}}, nil)
	if len(actors) != 1 || actors[0].ID != "system" {
		t.Fatalf("fallback area actor is missing: %+v", actors)
	}
}
