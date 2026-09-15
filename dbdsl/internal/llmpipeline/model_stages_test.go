package llmpipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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
	assertFields(t, parsed.CRUDOperations[0], "actor_id", "creates", "deletes", "functional_area_id", "id", "label", "outcome", "persistent_data", "reads", "requirement_atoms", "source_units", "updates")
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
		t.Fatal("repair logical projection input is missing the previous complete patch")
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
	if conceptualRun.MaxOutputTokens != defaultConceptualMaxOutputTokens {
		t.Fatalf("conceptual max output tokens = %d, want %d", conceptualRun.MaxOutputTokens, defaultConceptualMaxOutputTokens)
	}
	decisionContext := []ConceptualReviewDecisionInput{{
		ID: "RD-001", Question: "Is the name required?", SelectedOptionID: "RC-001-O1",
		Rationale: "The source requires a name.", AffectedAtoms: []string{"RA-001"},
	}}
	previousProposal := PatchProposal{Operations: []PatchOperation{{Operation: "add_entity"}}}
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
