package llmpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

func TestRequirementSourceUnitInputsDeduplicateText(t *testing.T) {
	inputs := requirementSourceUnitInputs([]dsl.SourceUnit{
		{
			ID: "SU-001", Kind: "requirement_sentence", Section: "catalog", Location: "line 10",
			Relevance: "model_relevant", Tags: []string{"product", "name"},
			Text: dsl.SourceUnitText{Exact: "Products have names.", Normalized: "Products have names."},
		},
		{
			ID: "SU-002", Kind: "requirement_sentence", Section: "catalog", Location: "line 11",
			Relevance: "model_relevant", Tags: []string{"product"},
			Text: dsl.SourceUnitText{Exact: "Products   have prices.", Normalized: "Products have prices."},
		},
		{
			ID: "SU-003", Kind: "structured_example", Section: "example", Relevance: "example",
			Text: dsl.SourceUnitText{Exact: `{"name":"Example","tags":["red","blue"],"score":42,"active":true,"missing":null,"name":"Other"}`},
		},
	})

	data := mustCompactJSON(map[string]any{"source_units": inputs})
	var parsed struct {
		SourceUnits []map[string]any `json:"source_units"`
	}
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		t.Fatalf("decode compact source-unit input: %v", err)
	}
	assertFields(t, parsed.SourceUnits[0], "id", "kind", "relevance", "section", "text")
	assertFields(t, parsed.SourceUnits[1], "exact", "id", "kind", "relevance", "section", "text")
	assertFields(t, parsed.SourceUnits[2], "id", "kind", "relevance", "section", "structured_shape", "text")
	if got := parsed.SourceUnits[0]["text"]; got != "Products have names." {
		t.Fatalf("normalized text = %#v", got)
	}
	if got := parsed.SourceUnits[1]["exact"]; got != "Products   have prices." {
		t.Fatalf("changed exact text was not preserved: %#v", got)
	}
	if got := parsed.SourceUnits[2]["text"]; got != `{"name":"<v>","tags":["<v>","<v>"],"score":"<v>","active":"<v>","missing":"<v>","name":"<v>"}` {
		t.Fatalf("structured example sketch = %#v", got)
	}
	shape, ok := parsed.SourceUnits[2]["structured_shape"].(map[string]any)
	if !ok || shape["literal_values_omitted"] != true || shape["interpretation"] != "schema_shape_evidence_only" {
		t.Fatalf("structured example metadata = %#v", parsed.SourceUnits[2]["structured_shape"])
	}
	if got := fmt.Sprint(shape["observed_keys"]); got != "[name tags score active missing]" {
		t.Fatalf("observed keys = %s", got)
	}
	if got := fmt.Sprint(shape["repeated_keys"]); got != "[name]" {
		t.Fatalf("repeated keys = %s", got)
	}
	for index, item := range parsed.SourceUnits {
		for _, removed := range []string{"location", "tags", "normalized"} {
			if _, ok := item[removed]; ok {
				t.Fatalf("source unit %d retains removed field %q: %#v", index, removed, item)
			}
		}
	}
}

func TestStageSpecificRequirementAtomInputs(t *testing.T) {
	atoms := []RequirementAtomProposal{{
		ID: "RA-001", Statement: "A customer places an order.", Subject: "customer", Predicate: "places", Object: "order",
		Quantifier: "many", Condition: "when checkout succeeds", TemporalSemantics: "after payment", Ownership: "sales",
		AtomType: "data_requirement", ModelingRelevance: "direct_db", ModelingOutcome: "represented",
		PersistenceEffect: "required", ExampleRole: "", SourceUnits: []string{"SU-001"}, FunctionalArea: "FA-SALES",
		ReviewDecisions: []string{"RD-001"},
	}}

	var functional struct {
		Atoms []map[string]any `json:"atoms"`
	}
	if err := json.Unmarshal([]byte(mustCompactJSON(map[string]any{"atoms": functionalRequirementAtomInputs(atoms)})), &functional); err != nil {
		t.Fatalf("decode functional atom input: %v", err)
	}
	assertFields(t, functional.Atoms[0], "id", "modeling_outcome", "ownership", "persistence_effect", "review_decisions", "source_units", "statement")

	var crud struct {
		Atoms []map[string]any `json:"atoms"`
	}
	if err := json.Unmarshal([]byte(mustCompactJSON(map[string]any{"atoms": crudRequirementAtomInputs(atoms)})), &crud); err != nil {
		t.Fatalf("decode CRUD atom input: %v", err)
	}
	assertFields(t, crud.Atoms[0], "functional_area", "id", "modeling_outcome", "ownership", "persistence_effect", "review_decisions", "source_units", "statement")
	if crud.Atoms[0]["functional_area"] != "FA-SALES" {
		t.Fatalf("CRUD projection lost functional area: %#v", crud.Atoms[0])
	}
}

func TestCompactAnalysisInputsMeetSizeBudgets(t *testing.T) {
	units := make([]dsl.SourceUnit, 0, 17)
	for index := 1; index <= 17; index++ {
		text := fmt.Sprintf("Requirement %d stores a persistent catalog value with an exact business meaning.", index)
		units = append(units, dsl.SourceUnit{
			ID: fmt.Sprintf("SU-%03d", index), Kind: "requirement_sentence", Section: "catalog requirements",
			Location: fmt.Sprintf("combined_document.md:line-%d", index), Relevance: "model_relevant",
			Tags: []string{"catalog", "persistent-data", "business-requirement"},
			Text: dsl.SourceUnitText{Exact: text, Normalized: text},
		})
	}
	atoms := make([]RequirementAtomProposal, 0, 28)
	for index := 1; index <= 28; index++ {
		atoms = append(atoms, RequirementAtomProposal{
			ID: fmt.Sprintf("RA-%04d", index), Statement: fmt.Sprintf("The catalog stores persistent value %d for later business processing.", index),
			Subject: "catalog record", Predicate: "stores", Object: "persistent business value", Quantifier: "exactly one",
			Condition: "when the catalog entry is accepted", TemporalSemantics: "retained after creation", Ownership: "catalog service",
			ModelingOutcome: "represented", PersistenceEffect: "required", ExampleRole: "constraint_boundary",
			SourceUnits: []string{fmt.Sprintf("SU-%03d", (index-1)%17+1)}, FunctionalArea: "FA-CATALOG",
			ReviewDecisions: []string{"RD-CATALOG-POLICY"},
		})
	}

	requirementFull := len(mustCompactJSON(map[string]any{"source_units": sourceUnitInputs(units)}))
	requirementCompact := len(mustCompactJSON(map[string]any{"source_units": requirementSourceUnitInputs(units)}))
	functionalFull := len(mustCompactJSON(map[string]any{"requirement_atoms": modelRequirementAtomInputs(atoms)}))
	functionalCompact := len(mustCompactJSON(map[string]any{"requirement_atoms": functionalRequirementAtomInputs(atoms)}))
	crudFull := functionalFull
	crudCompact := len(mustCompactJSON(map[string]any{"requirement_atoms": crudRequirementAtomInputs(atoms)}))

	// The full baseline no longer carries location or a duplicate normalized text, so the relative gain is smaller.
	assertInputReduction(t, "requirement extraction", requirementFull, requirementCompact, 0.20)
	assertInputReduction(t, "functional analysis", functionalFull, functionalCompact, 0.40)
	assertInputReduction(t, "CRUD mapping", crudFull, crudCompact, 0.30)
}

func assertInputReduction(t *testing.T, stage string, full, compact int, minimum float64) {
	t.Helper()
	if full <= 0 || compact >= full {
		t.Fatalf("%s input was not reduced: full=%d compact=%d", stage, full, compact)
	}
	reduction := float64(full-compact) / float64(full)
	if reduction < minimum {
		t.Fatalf("%s input reduction %.1f%% is below %.1f%% (full=%d compact=%d)", stage, reduction*100, minimum*100, full, compact)
	}
}

func TestGranularAnalysisStageChainWithMock(t *testing.T) {
	units := []dsl.SourceUnit{{ID: "SU-001", Kind: "requirement_sentence", Relevance: "model_relevant", Text: dsl.SourceUnitText{Exact: "Products have names.", Normalized: "Products have names."}}}
	mock := llm.NewDefaultMockClient()
	outDir := t.TempDir()
	atoms, atomQA, err := RunRequirementAtomExtraction(context.Background(), mock, RequirementAtomStageOptions{OutDir: outDir, SourceUnits: units, Model: "mock-model"})
	if err != nil || !atomQA.OK {
		t.Fatalf("requirement stage: qa=%+v err=%v", atomQA, err)
	}
	functional, functionalQA, err := RunFunctionalAnalysis(context.Background(), mock, FunctionalAnalysisStageOptions{OutDir: outDir, SourceUnits: units, RequirementAtoms: atoms.RequirementAtoms, Model: "mock-model"})
	if err != nil || !functionalQA.OK {
		t.Fatalf("functional stage: qa=%+v err=%v", functionalQA, err)
	}
	crud, crudQA, err := RunCRUDMapping(context.Background(), mock, CRUDMappingStageOptions{OutDir: outDir, SourceUnits: units, RequirementAtoms: atoms.RequirementAtoms, FunctionalAreas: functional.FunctionalAreas, Actors: functional.Actors, Model: "mock-model"})
	if err != nil || !crudQA.OK || len(crud.Operations) != 1 {
		t.Fatalf("CRUD stage: proposal=%+v qa=%+v err=%v", crud, crudQA, err)
	}
}

func TestRequirementAtomExtractionChunksAndMergesLongInputs(t *testing.T) {
	units := make([]dsl.SourceUnit, 0, 92)
	for i := 1; i <= 92; i++ {
		text := fmt.Sprintf("Requirement %d stores a value.", i)
		units = append(units, dsl.SourceUnit{ID: fmt.Sprintf("SU-%03d", i), Kind: "requirement_sentence", Relevance: "model_relevant", Text: dsl.SourceUnitText{Exact: text, Normalized: text}})
	}
	outDir := t.TempDir()
	proposal, qa, err := RunRequirementAtomExtraction(context.Background(), llm.NewDefaultMockClient(), RequirementAtomStageOptions{OutDir: outDir, SourceUnits: units, Model: "mock-model"})
	if err != nil || !qa.OK || len(proposal.RequirementAtoms) != len(units) {
		t.Fatalf("chunked extraction: atoms=%d qa=%+v err=%v", len(proposal.RequirementAtoms), qa, err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "llm_runs", "requirement_atom_consolidation.json")); err != nil {
		t.Fatalf("missing consolidation report: %v", err)
	}
	var firstChunk RunSummary
	readJSONTestFile(t, filepath.Join(outDir, "llm_runs", "003_requirement_atom_extraction_chunk_001", "run.json"), &firstChunk)
	if firstChunk.FullContextBytes <= firstChunk.ContextBytes || firstChunk.ContextReduction <= 0 {
		t.Fatalf("requirement chunk did not record the monolithic counterfactual: %+v", firstChunk)
	}
	for i, atom := range proposal.RequirementAtoms {
		if atom.ID != fmt.Sprintf("RA-%04d", i+1) {
			t.Fatalf("unstable merged id at %d: %s", i, atom.ID)
		}
	}
}

func TestRequirementAtomAssumptionRequiresReview(t *testing.T) {
	qa := ValidateRequirementAtomProposal(RequirementAtomExtractionProposal{RequirementAtoms: []RequirementAtomProposal{{
		ID: "RA-001", Statement: "Maybe persist a report.", SourceUnits: []string{"SU-001"}, SupportLevel: "assumption", Confidence: "low", ModelingOutcome: "represented",
	}}}, []dsl.SourceUnit{{ID: "SU-001"}})
	if qa.OK {
		t.Fatalf("expected assumption without review warning to fail: %+v", qa)
	}
}

func TestRequirementAtomAssumptionAllowsLinkedReviewDecision(t *testing.T) {
	qa := ValidateRequirementAtomProposal(RequirementAtomExtractionProposal{RequirementAtoms: []RequirementAtomProposal{{
		ID: "RA-001", Statement: "Persist a configurable approval policy.", SourceUnits: []string{"SU-001"},
		SupportLevel: "assumption", Confidence: "medium", ModelingOutcome: "represented",
		Warnings: []string{"The configurable policy is an accepted modeling assumption."}, ReviewDecisions: []string{"RD-001"},
	}}}, []dsl.SourceUnit{{ID: "SU-001"}})
	if !qa.OK {
		t.Fatalf("linked review decision should resolve the assumption review gate: %+v", qa)
	}
}

func TestRequirementAtomStructuredExampleRequiresExplicitDisposition(t *testing.T) {
	units := []dsl.SourceUnit{{ID: "SU-001", Kind: "structured_example", Relevance: "example"}}
	withoutRole := RequirementAtomExtractionProposal{RequirementAtoms: []RequirementAtomProposal{{
		ID: "RA-001", Statement: "The example contains a name field.", SourceUnits: []string{"SU-001"},
		ModelingOutcome: "represented", PersistenceEffect: "required", SupportLevel: "example_based", Confidence: "medium",
	}}}
	if qa := ValidateRequirementAtomProposal(withoutRole, units); qa.OK {
		t.Fatalf("structured example without disposition passed QA: %+v", qa)
	}
	withShape := withoutRole
	withShape.RequirementAtoms = append([]RequirementAtomProposal(nil), withoutRole.RequirementAtoms...)
	withShape.RequirementAtoms[0].ExampleRole = "schema_shape"
	if qa := ValidateRequirementAtomProposal(withShape, units); !qa.OK {
		t.Fatalf("schema-shape disposition failed QA: %+v", qa)
	}
}

func TestRequirementAtomSchemaShapeCannotBeDiscarded(t *testing.T) {
	qa := ValidateRequirementAtomProposal(RequirementAtomExtractionProposal{RequirementAtoms: []RequirementAtomProposal{{
		ID: "RA-001", Statement: "The example contains a name field.", SourceUnits: []string{"SU-001"},
		ExampleRole: "schema_shape", ModelingOutcome: "intentionally_not_in_db", PersistenceEffect: "not_required",
	}}}, []dsl.SourceUnit{{ID: "SU-001", Kind: "structured_example", Relevance: "example"}})
	if qa.OK {
		t.Fatalf("discarded schema-shape example passed QA: %+v", qa)
	}
}
