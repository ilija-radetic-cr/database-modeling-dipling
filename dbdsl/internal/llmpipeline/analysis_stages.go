package llmpipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

type RequirementAtomStageOptions struct {
	OutDir          string
	SourceUnits     []dsl.SourceUnit
	Model           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
}

type FunctionalAnalysisStageOptions struct {
	OutDir           string
	SourceUnits      []dsl.SourceUnit
	RequirementAtoms []RequirementAtomProposal
	Model            string
	ReasoningEffort  string
	Temperature      float64
	MaxOutputTokens  int
}

type CRUDMappingStageOptions struct {
	OutDir           string
	SourceUnits      []dsl.SourceUnit
	RequirementAtoms []RequirementAtomProposal
	FunctionalAreas  []FunctionalAreaProposal
	Actors           []ActorProposal
	Model            string
	ReasoningEffort  string
	Temperature      float64
	MaxOutputTokens  int
}

func RunRequirementAtomExtraction(ctx context.Context, client llm.Client, opts RequirementAtomStageOptions) (RequirementAtomExtractionProposal, StageQA, error) {
	if client == nil {
		return RequirementAtomExtractionProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || len(opts.SourceUnits) == 0 {
		return RequirementAtomExtractionProposal{}, StageQA{}, errors.New("output directory and source units are required")
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	chunks := chunkSourceUnits(opts.SourceUnits, 45, 2)
	if len(chunks) == 1 {
		return runRequirementAtomChunk(ctx, client, opts, chunks[0], 1, 1)
	}
	merged := RequirementAtomExtractionProposal{RequirementAtoms: []RequirementAtomProposal{}, Warnings: []string{}, ConfidenceSummary: map[string]string{"strategy": "chunked_with_deterministic_merge"}}
	seen := map[string]bool{}
	for index, chunk := range chunks {
		proposal, _, err := runRequirementAtomChunk(ctx, client, opts, chunk, index+1, len(chunks))
		if err != nil {
			return merged, ValidateRequirementAtomProposal(merged, opts.SourceUnits), fmt.Errorf("requirement atom chunk %d/%d: %w", index+1, len(chunks), err)
		}
		merged.Warnings = append(merged.Warnings, proposal.Warnings...)
		for _, atom := range proposal.RequirementAtoms {
			key := requirementAtomMergeKey(atom)
			if seen[key] {
				continue
			}
			seen[key] = true
			atom.ID = fmt.Sprintf("RA-%04d", len(merged.RequirementAtoms)+1)
			merged.RequirementAtoms = append(merged.RequirementAtoms, atom)
		}
	}
	qa := ValidateRequirementAtomProposal(merged, opts.SourceUnits)
	_ = writeJSONFile(filepath.Join(opts.OutDir, "llm_runs", "requirement_atom_consolidation.json"), map[string]any{
		"strategy": "section_ordered_chunks_with_overlap", "chunk_count": len(chunks), "overlap_source_units": 2,
		"merged_atoms": len(merged.RequirementAtoms), "validation": qa,
	})
	if !qa.OK {
		return merged, qa, fmt.Errorf("merged requirement atoms failed validation: %s", strings.Join(qa.Errors, "; "))
	}
	return merged, qa, nil
}

func runRequirementAtomChunk(ctx context.Context, client llm.Client, opts RequirementAtomStageOptions, units []dsl.SourceUnit, chunkIndex, chunkCount int) (RequirementAtomExtractionProposal, StageQA, error) {
	input := mustJSON(map[string]any{
		"source_units": sourceUnitInputs(units), "pipeline_version": "0.7", "chunk_index": chunkIndex, "chunk_count": chunkCount,
		"output_contract": "requirement_atom_extraction", "template_version": promptTemplateVersion,
	})
	var proposal RequirementAtomExtractionProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 3, llm.Request{
		Stage: "requirement_atom_extraction", Model: opts.Model, Instructions: requirementAtomExtractionInstructions,
		Input: input, SchemaName: "DBDSLRequirementAtomExtraction", Schema: requirementAtomExtractionSchema(),
		ReasoningEffort: opts.ReasoningEffort, Temperature: opts.Temperature, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{"template_version": promptTemplateVersion, "chunk_index": fmt.Sprint(chunkIndex), "chunk_count": fmt.Sprint(chunkCount)},
	}, &proposal, func() []string {
		qa = ValidateRequirementAtomProposal(proposal, units)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	qa = ValidateRequirementAtomProposal(proposal, units)
	return proposal, qa, nil
}

func chunkSourceUnits(units []dsl.SourceUnit, maxSize, overlap int) [][]dsl.SourceUnit {
	if len(units) <= maxSize {
		return [][]dsl.SourceUnit{units}
	}
	var chunks [][]dsl.SourceUnit
	for start := 0; start < len(units); {
		end := start + maxSize
		if end > len(units) {
			end = len(units)
		}
		chunks = append(chunks, append([]dsl.SourceUnit(nil), units[start:end]...))
		if end == len(units) {
			break
		}
		start = end - overlap
	}
	return chunks
}

func requirementAtomMergeKey(atom RequirementAtomProposal) string {
	sources := append([]string(nil), atom.SourceUnits...)
	sort.Strings(sources)
	return strings.ToLower(strings.TrimSpace(atom.Statement)) + "|" + strings.Join(sources, ",")
}

func RunFunctionalAnalysis(ctx context.Context, client llm.Client, opts FunctionalAnalysisStageOptions) (FunctionalAnalysisProposal, StageQA, error) {
	if client == nil {
		return FunctionalAnalysisProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || len(opts.RequirementAtoms) == 0 {
		return FunctionalAnalysisProposal{}, StageQA{}, errors.New("output directory and requirement atoms are required")
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	input := mustJSON(map[string]any{
		"source_units": sourceUnitInputs(opts.SourceUnits), "requirement_atoms": opts.RequirementAtoms,
		"pipeline_version": "0.7", "output_contract": "functional_analysis", "template_version": promptTemplateVersion,
	})
	var proposal FunctionalAnalysisProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 4, llm.Request{
		Stage: "functional_analysis", Model: opts.Model, Instructions: functionalAnalysisInstructions,
		Input: input, SchemaName: "DBDSLFunctionalAnalysis", Schema: functionalAnalysisSchema(),
		ReasoningEffort: opts.ReasoningEffort, Temperature: opts.Temperature, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{"template_version": promptTemplateVersion},
	}, &proposal, func() []string {
		qa = ValidateFunctionalAnalysisProposal(proposal, opts.RequirementAtoms)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	qa = ValidateFunctionalAnalysisProposal(proposal, opts.RequirementAtoms)
	return proposal, qa, nil
}

func RunCRUDMapping(ctx context.Context, client llm.Client, opts CRUDMappingStageOptions) (CRUDMappingProposal, StageQA, error) {
	if client == nil {
		return CRUDMappingProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || len(opts.FunctionalAreas) == 0 || len(opts.Actors) == 0 {
		return CRUDMappingProposal{}, StageQA{}, errors.New("output directory, functional areas and actors are required")
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	input := mustJSON(map[string]any{
		"source_units": sourceUnitInputs(opts.SourceUnits), "requirement_atoms": opts.RequirementAtoms,
		"functional_areas": opts.FunctionalAreas, "actors": opts.Actors, "pipeline_version": "0.7",
		"output_contract": "crud_mapping", "template_version": promptTemplateVersion,
	})
	var proposal CRUDMappingProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 5, llm.Request{
		Stage: "crud_mapping", Model: opts.Model, Instructions: crudMappingInstructions,
		Input: input, SchemaName: "DBDSLCRUDMapping", Schema: crudMappingSchema(),
		ReasoningEffort: opts.ReasoningEffort, Temperature: opts.Temperature, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{"template_version": promptTemplateVersion},
	}, &proposal, func() []string {
		qa = ValidateCRUDMappingProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Actors)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	qa = ValidateCRUDMappingProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Actors)
	return proposal, qa, nil
}

func ValidateRequirementAtomProposal(proposal RequirementAtomExtractionProposal, units []dsl.SourceUnit) StageQA {
	qa := newStageQA(proposal.Warnings)
	sources := map[string]bool{}
	for _, unit := range units {
		sources[unit.ID] = true
	}
	seen := map[string]bool{}
	covered := map[string]bool{}
	if len(proposal.RequirementAtoms) == 0 {
		qa.Errors = append(qa.Errors, "requirement extraction produced no atoms")
	}
	for _, atom := range proposal.RequirementAtoms {
		if strings.TrimSpace(atom.ID) == "" || seen[atom.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate requirement atom id %q", atom.ID))
		}
		seen[atom.ID] = true
		if strings.TrimSpace(atom.Statement) == "" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has an empty statement", atom.ID))
		}
		if strings.TrimSpace(atom.Subject) == "" || strings.TrimSpace(atom.Predicate) == "" {
			qa.Warnings = append(qa.Warnings, fmt.Sprintf("%s lacks complete subject/predicate atomic-fact structure", atom.ID))
		}
		if len(atom.SourceUnits) == 0 && atom.ModelingOutcome == "represented" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s represented atom has no source evidence", atom.ID))
		}
		for _, id := range atom.SourceUnits {
			if !sources[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown source unit %s", atom.ID, id))
			} else {
				covered[id] = true
			}
		}
		if (atom.SupportLevel == "assumption" || atom.Confidence == "low") && (!atom.RequiresReview || len(atom.Warnings) == 0) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s assumption/low-confidence atom requires review and warning", atom.ID))
		}
	}
	qa.Coverage["source_units_total"] = len(units)
	qa.Coverage["source_units_covered"] = len(covered)
	qa.Coverage["requirement_atoms"] = len(proposal.RequirementAtoms)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func ValidateFunctionalAnalysisProposal(proposal FunctionalAnalysisProposal, atoms []RequirementAtomProposal) StageQA {
	qa := newStageQA(proposal.Warnings)
	atomIDs := map[string]bool{}
	for _, atom := range atoms {
		atomIDs[atom.ID] = true
	}
	actorIDs := map[string]bool{}
	for _, actor := range proposal.Actors {
		if actor.ID == "" || actorIDs[actor.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate actor id %q", actor.ID))
		}
		actorIDs[actor.ID] = true
	}
	areaIDs := map[string]bool{}
	covered := map[string]bool{}
	if len(proposal.FunctionalAreas) == 0 {
		qa.Errors = append(qa.Errors, "functional analysis produced no areas")
	}
	for _, area := range proposal.FunctionalAreas {
		if area.ID == "" || areaIDs[area.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate functional area id %q", area.ID))
		}
		areaIDs[area.ID] = true
		for _, actor := range area.MainActors {
			if !actorIDs[actor] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown actor %s", area.ID, actor))
			}
		}
		for _, atom := range area.Atoms {
			if !atomIDs[atom] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown atom %s", area.ID, atom))
			} else {
				covered[atom] = true
			}
		}
	}
	var uncovered []string
	for id := range atomIDs {
		if !covered[id] {
			uncovered = append(uncovered, id)
		}
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		qa.Errors = append(qa.Errors, "functional analysis does not cover atoms: "+strings.Join(uncovered, ", "))
	}
	qa.Coverage["requirement_atoms_total"] = len(atoms)
	qa.Coverage["requirement_atoms_covered"] = len(covered)
	qa.Coverage["functional_areas"] = len(proposal.FunctionalAreas)
	qa.Coverage["actors"] = len(proposal.Actors)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func ValidateCRUDMappingProposal(proposal CRUDMappingProposal, units []dsl.SourceUnit, atoms []RequirementAtomProposal, areas []FunctionalAreaProposal, actors []ActorProposal) StageQA {
	qa := newStageQA(proposal.Warnings)
	sourceIDs, atomIDs, areaIDs, actorIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, item := range units {
		sourceIDs[item.ID] = true
	}
	for _, item := range atoms {
		atomIDs[item.ID] = true
	}
	for _, item := range areas {
		areaIDs[item.ID] = true
	}
	for _, item := range actors {
		actorIDs[item.ID] = true
	}
	seen := map[string]bool{}
	coveredAtoms := map[string]bool{}
	if len(proposal.Operations) == 0 {
		qa.Errors = append(qa.Errors, "CRUD mapping produced no operations")
	}
	for _, operation := range proposal.Operations {
		if operation.ID == "" || seen[operation.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate operation id %q", operation.ID))
		}
		seen[operation.ID] = true
		if !actorIDs[operation.ActorID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown actor %s", operation.ID, operation.ActorID))
		}
		if !areaIDs[operation.FunctionalAreaID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown functional area %s", operation.ID, operation.FunctionalAreaID))
		}
		for _, id := range operation.RequirementAtoms {
			if !atomIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown atom %s", operation.ID, id))
			} else {
				coveredAtoms[id] = true
			}
		}
		for _, id := range operation.SourceUnits {
			if !sourceIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown source unit %s", operation.ID, id))
			}
		}
		mutating := strings.Contains(strings.ToLower(operation.Outcome), "mutat") || len(operation.Creates)+len(operation.Updates)+len(operation.Deletes) > 0
		if mutating && len(operation.Creates)+len(operation.Updates)+len(operation.Deletes) == 0 && (!operation.RequiresReview || len(operation.Warnings) == 0) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s mutating operation has no C/U/D effect or review warning", operation.ID))
		}
	}
	qa.Coverage["operations"] = len(proposal.Operations)
	qa.Coverage["requirement_atoms_total"] = len(atoms)
	qa.Coverage["requirement_atoms_covered"] = len(coveredAtoms)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func newStageQA(warnings []string) StageQA {
	return StageQA{Errors: []string{}, Warnings: append([]string(nil), warnings...), Coverage: map[string]int{}}
}

func normalizeAnalysisOptions(model, effort *string, tokens *int) {
	*model = nonEmpty(*model, llm.DefaultModel)
	*effort = nonEmpty(*effort, llm.DefaultReasoningEffort)
	if *tokens <= 0 {
		*tokens = llm.DefaultMaxOutputTokens
	}
}
