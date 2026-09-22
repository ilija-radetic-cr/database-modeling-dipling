package llmpipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

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
