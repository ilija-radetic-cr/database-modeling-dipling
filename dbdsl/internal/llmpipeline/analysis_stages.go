package llmpipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

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
	MaxParallelism  int
	PromptVersion   string
	// SourceUnitNotes maps source-unit IDs to requirement ambiguity recorded
	// during classification.
	SourceUnitNotes map[string][]string
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

type requirementSourceUnitInput struct {
	ID              string                       `json:"id"`
	Kind            string                       `json:"kind"`
	Section         string                       `json:"section"`
	Relevance       string                       `json:"relevance"`
	Text            string                       `json:"text"`
	Exact           string                       `json:"exact,omitempty"`
	StructuredShape *structuredExampleShapeInput `json:"structured_shape,omitempty"`
	// RequirementNotes flag open questions the atom stage must surface.
	RequirementNotes []string `json:"requirement_notes,omitempty"`
}

type structuredExampleShapeInput struct {
	ObservedKeys         []string `json:"observed_keys"`
	RepeatedKeys         []string `json:"repeated_keys,omitempty"`
	LiteralValuesOmitted bool     `json:"literal_values_omitted"`
	Interpretation       string   `json:"interpretation"`
}

type functionalRequirementAtomInput struct {
	ID                string   `json:"id"`
	Statement         string   `json:"statement"`
	Ownership         string   `json:"ownership"`
	ModelingOutcome   string   `json:"modeling_outcome"`
	PersistenceEffect string   `json:"persistence_effect,omitempty"`
	SourceUnits       []string `json:"source_units"`
	ReviewDecisions   []string `json:"review_decisions,omitempty"`
}

type crudRequirementAtomInput struct {
	ID                string   `json:"id"`
	Statement         string   `json:"statement"`
	Ownership         string   `json:"ownership"`
	ModelingOutcome   string   `json:"modeling_outcome"`
	PersistenceEffect string   `json:"persistence_effect,omitempty"`
	SourceUnits       []string `json:"source_units"`
	ReviewDecisions   []string `json:"review_decisions,omitempty"`
	FunctionalArea    string   `json:"functional_area"`
}

func requirementSourceUnitInputs(units []dsl.SourceUnit) []requirementSourceUnitInput {
	out := make([]requirementSourceUnitInput, 0, len(units))
	for _, unit := range units {
		text := unit.Text.Normalized
		if strings.TrimSpace(text) == "" {
			text = unit.Text.Exact
		}
		item := requirementSourceUnitInput{
			ID: unit.ID, Kind: unit.Kind, Section: unit.Section,
			Relevance: unit.Relevance, Text: text,
		}
		if unit.Kind == "structured_example" {
			raw := unit.Text.Exact
			if strings.TrimSpace(raw) == "" {
				raw = text
			}
			sketch, observed, repeated := structuredExampleShape(raw)
			item.Text = sketch
			item.StructuredShape = &structuredExampleShapeInput{
				ObservedKeys: observed, RepeatedKeys: repeated, LiteralValuesOmitted: true,
				Interpretation: "schema_shape_evidence_only",
			}
		} else if strings.TrimSpace(unit.Text.Normalized) != "" && unit.Text.Exact != "" && unit.Text.Exact != unit.Text.Normalized {
			item.Exact = unit.Text.Exact
		}
		out = append(out, item)
	}
	return out
}

// structuredExampleShape keeps JSON-like keys, delimiters and nesting visible to
// the model while replacing quoted scalar values. Structured examples are shape
// evidence; their sample values must not silently become enums, seed data or
// database constraints.
func structuredExampleShape(value string) (string, []string, []string) {
	var out strings.Builder
	counts := map[string]int{}
	order := []string{}
	for index := 0; index < len(value); {
		if value[index] != '"' {
			out.WriteByte(value[index])
			index++
			continue
		}
		end := index + 1
		escaped := false
		for end < len(value) {
			if escaped {
				escaped = false
				end++
				continue
			}
			if value[end] == '\\' {
				escaped = true
				end++
				continue
			}
			if value[end] == '"' {
				break
			}
			end++
		}
		if end >= len(value) {
			out.WriteString(value[index:])
			break
		}
		token := value[index : end+1]
		next := end + 1
		for next < len(value) && (value[next] == ' ' || value[next] == '\t' || value[next] == '\r' || value[next] == '\n') {
			next++
		}
		if next < len(value) && value[next] == ':' {
			out.WriteString(token)
			key := strings.Trim(token, "\"")
			if decoded, err := strconv.Unquote(token); err == nil {
				key = decoded
			}
			if counts[key] == 0 {
				order = append(order, key)
			}
			counts[key]++
		} else {
			out.WriteString("\"<v>\"")
		}
		index = end + 1
	}
	repeated := []string{}
	for _, key := range order {
		if counts[key] > 1 {
			repeated = append(repeated, key)
		}
	}
	return strings.TrimSpace(redactBareStructuredValues(out.String())), order, repeated
}

var bareStructuredValuePattern = regexp.MustCompile(`-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null`)

func redactBareStructuredValues(value string) string {
	matches := bareStructuredValuePattern.FindAllStringIndex(value, -1)
	if len(matches) == 0 {
		return value
	}
	var out strings.Builder
	last := 0
	for _, match := range matches {
		previous := match[0] - 1
		for previous >= 0 && (value[previous] == ' ' || value[previous] == '\t' || value[previous] == '\r' || value[previous] == '\n') {
			previous--
		}
		next := match[1]
		for next < len(value) && (value[next] == ' ' || value[next] == '\t' || value[next] == '\r' || value[next] == '\n') {
			next++
		}
		valuePosition := previous >= 0 && strings.ContainsRune(":[,]", rune(value[previous])) &&
			next < len(value) && strings.ContainsRune(",}]", rune(value[next]))
		if !valuePosition {
			continue
		}
		out.WriteString(value[last:match[0]])
		out.WriteString("\"<v>\"")
		last = match[1]
	}
	if last == 0 {
		return value
	}
	out.WriteString(value[last:])
	return out.String()
}

func withRequirementNotes(items []requirementSourceUnitInput, notes map[string][]string) []requirementSourceUnitInput {
	for i := range items {
		items[i].RequirementNotes = notes[items[i].ID]
	}
	return items
}

func functionalRequirementAtomInputs(atoms []RequirementAtomProposal) []functionalRequirementAtomInput {
	out := make([]functionalRequirementAtomInput, 0, len(atoms))
	for _, atom := range atoms {
		out = append(out, functionalRequirementAtomInput{
			ID: atom.ID, Statement: atom.Statement, Ownership: atom.Ownership,
			ModelingOutcome: atom.ModelingOutcome, PersistenceEffect: atom.PersistenceEffect,
			SourceUnits: atom.SourceUnits, ReviewDecisions: atom.ReviewDecisions,
		})
	}
	return out
}

func crudRequirementAtomInputs(atoms []RequirementAtomProposal) []crudRequirementAtomInput {
	out := make([]crudRequirementAtomInput, 0, len(atoms))
	for _, atom := range atoms {
		out = append(out, crudRequirementAtomInput{
			ID: atom.ID, Statement: atom.Statement, Ownership: atom.Ownership,
			ModelingOutcome: atom.ModelingOutcome, PersistenceEffect: atom.PersistenceEffect,
			SourceUnits: atom.SourceUnits, ReviewDecisions: atom.ReviewDecisions,
			FunctionalArea: atom.FunctionalArea,
		})
	}
	return out
}

func normalizedParallelism(value, fallback int) int {
	if value <= 0 {
		value = fallback
	}
	if value < 1 {
		return 1
	}
	return value
}

func resolvedPromptVersion(value string) string {
	if strings.TrimSpace(value) == "" {
		return promptTemplateVersion
	}
	return strings.TrimSpace(value)
}

func RunRequirementAtomExtraction(ctx context.Context, client llm.Client, opts RequirementAtomStageOptions) (RequirementAtomExtractionProposal, StageQA, error) {
	if client == nil {
		return RequirementAtomExtractionProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || len(opts.SourceUnits) == 0 {
		return RequirementAtomExtractionProposal{}, StageQA{}, errors.New("output directory and source units are required")
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	units := modelRelevantSourceUnits(opts.SourceUnits)
	if len(units) == 0 {
		return RequirementAtomExtractionProposal{}, StageQA{}, errors.New("no model-relevant source units are available")
	}
	chunks := chunkSourceUnits(units, RequirementAtomChunkSize, 0)
	if len(chunks) == 1 {
		proposal, _, err := runRequirementAtomChunk(ctx, client, opts, chunks[0], 1, 1)
		if err != nil {
			return proposal, ValidateRequirementAtomProposal(proposal, opts.SourceUnits), err
		}
		qa := ValidateRequirementAtomProposal(proposal, opts.SourceUnits)
		if !qa.OK {
			return proposal, qa, fmt.Errorf("requirement atoms failed validation: %s", strings.Join(qa.Errors, "; "))
		}
		return proposal, qa, nil
	}
	merged := RequirementAtomExtractionProposal{RequirementAtoms: []RequirementAtomProposal{}, Warnings: []string{}, ConfidenceSummary: map[string]string{"strategy": "chunked_with_deterministic_merge"}}
	type chunkResult struct {
		proposal RequirementAtomExtractionProposal
		err      error
		skipped  bool
	}
	results := make([]chunkResult, len(chunks))
	maxParallelism := normalizedParallelism(opts.MaxParallelism, 3)
	semaphore := make(chan struct{}, maxParallelism)
	var wait sync.WaitGroup
	for index, chunk := range chunks {
		wait.Add(1)
		go func(index int, chunk []dsl.SourceUnit) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			skipped := requirementChunkIsStructuralOnly(chunk)
			proposal, _, err := runRequirementAtomChunk(ctx, client, opts, chunk, index+1, len(chunks))
			results[index] = chunkResult{proposal: proposal, err: err, skipped: skipped}
		}(index, chunk)
	}
	wait.Wait()
	seen := map[string]bool{}
	skippedChunks := 0
	for index, result := range results {
		if result.err != nil {
			return merged, ValidateRequirementAtomProposal(merged, opts.SourceUnits), fmt.Errorf("requirement atom chunk %d/%d: %w", index+1, len(chunks), result.err)
		}
		proposal := result.proposal
		if result.skipped {
			skippedChunks++
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
	merged.RequirementAtoms = NormalizeRequirementReviewSemantics(merged.RequirementAtoms)
	qa := ValidateRequirementAtomProposal(merged, opts.SourceUnits)
	_ = writeJSONFile(filepath.Join(opts.OutDir, "llm_runs", "requirement_atom_consolidation.json"), map[string]any{
		"strategy": "parallel_section_ordered_chunks", "chunk_count": len(chunks), "overlap_source_units": 0, "max_parallelism": maxParallelism,
		"merged_atoms": len(merged.RequirementAtoms), "skipped_structural_chunks": skippedChunks, "validation": qa,
	})
	if !qa.OK {
		return merged, qa, fmt.Errorf("merged requirement atoms failed validation: %s", strings.Join(qa.Errors, "; "))
	}
	return merged, qa, nil
}

func runRequirementAtomChunk(ctx context.Context, client llm.Client, opts RequirementAtomStageOptions, units []dsl.SourceUnit, chunkIndex, chunkCount int) (RequirementAtomExtractionProposal, StageQA, error) {
	if requirementChunkIsStructuralOnly(units) {
		warning := fmt.Sprintf("chunk %d/%d contains only structured-example closing delimiters; skipped without an LLM call", chunkIndex, chunkCount)
		proposal := RequirementAtomExtractionProposal{RequirementAtoms: []RequirementAtomProposal{}, Warnings: []string{warning}, ConfidenceSummary: map[string]string{"strategy": "deterministic_structural_skip"}}
		qa := StageQA{OK: true, Errors: []string{}, Warnings: []string{warning}, Coverage: map[string]int{"source_units": len(units), "requirement_atoms": 0}}
		return proposal, qa, nil
	}
	fullInput := mustJSON(map[string]any{
		"source_units": requirementSourceUnitInputs(modelRelevantSourceUnits(opts.SourceUnits)), "pipeline_version": "0.7",
		"output_contract": "requirement_atom_extraction", "template_version": resolvedPromptVersion(opts.PromptVersion),
	})
	input := mustJSON(map[string]any{
		"source_units": withRequirementNotes(requirementSourceUnitInputs(units), opts.SourceUnitNotes), "pipeline_version": "0.7", "chunk_index": chunkIndex, "chunk_count": chunkCount,
		"output_contract": "requirement_atom_extraction", "template_version": resolvedPromptVersion(opts.PromptVersion),
	})
	var proposal RequirementAtomExtractionProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 3, llm.Request{
		Stage: "requirement_atom_extraction", Model: opts.Model, Instructions: requirementAtomExtractionInstructions,
		Input: input, SchemaName: "DBDSLRequirementAtomExtraction", Schema: requirementAtomExtractionSchema(),
		ReasoningEffort: opts.ReasoningEffort, Temperature: opts.Temperature, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{"template_version": resolvedPromptVersion(opts.PromptVersion), "chunk_index": fmt.Sprint(chunkIndex), "chunk_count": fmt.Sprint(chunkCount), "run_key": fmt.Sprintf("chunk_%03d", chunkIndex), "full_context_bytes": fmt.Sprint(len(fullInput)), "context_policy": "bounded_requirement_compact_v2", "canonicalizer_version": "pipeline_ids_v2"},
	}, &proposal, func() []string {
		canonicalizeRequirementAtomIDs(&proposal)
		proposal.RequirementAtoms = NormalizeRequirementReviewSemantics(proposal.RequirementAtoms)
		qa = ValidateRequirementAtomProposal(proposal, units)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	canonicalizeRequirementAtomIDs(&proposal)
	proposal.RequirementAtoms = NormalizeRequirementReviewSemantics(proposal.RequirementAtoms)
	qa = ValidateRequirementAtomProposal(proposal, units)
	return proposal, qa, nil
}

func requirementChunkIsStructuralOnly(units []dsl.SourceUnit) bool {
	if len(units) == 0 {
		return false
	}
	for _, unit := range units {
		if unit.Kind != "structured_example" || unit.Relevance != "example" || !isClosingStructuredDelimiter(unit.Text.Exact) {
			return false
		}
	}
	return true
}

func isClosingStructuredDelimiter(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		switch r {
		case '}', ']', ',':
		default:
			return false
		}
	}
	return true
}

func canonicalizeRequirementAtomIDs(proposal *RequirementAtomExtractionProposal) {
	sort.SliceStable(proposal.RequirementAtoms, func(i, j int) bool {
		left, right := requirementAtomMergeKey(proposal.RequirementAtoms[i]), requirementAtomMergeKey(proposal.RequirementAtoms[j])
		return left < right
	})
	for i := range proposal.RequirementAtoms {
		proposal.RequirementAtoms[i].ID = fmt.Sprintf("RA-%04d", i+1)
	}
}

func modelRelevantSourceUnits(units []dsl.SourceUnit) []dsl.SourceUnit {
	out := make([]dsl.SourceUnit, 0, len(units))
	for _, unit := range units {
		if unit.Kind == "heading" || unit.Kind == "noise" || unit.Relevance == "non_model" {
			continue
		}
		out = append(out, unit)
	}
	return out
}

// RequirementAtomChunkSize keeps each extraction call short enough that
// parallel chunks, not one long generation, determine the stage latency.
const RequirementAtomChunkSize = 20

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
	compactAtoms := functionalRequirementAtomInputs(opts.RequirementAtoms)
	input := mustCompactJSON(map[string]any{
		"requirement_atoms": compactAtoms,
		"pipeline_version":  "0.7", "output_contract": "functional_analysis", "template_version": promptTemplateVersion,
	})
	fullInput := mustJSON(map[string]any{"requirement_atoms": opts.RequirementAtoms})
	var proposal FunctionalAnalysisProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 4, llm.Request{
		Stage: "functional_analysis", Model: opts.Model, Instructions: functionalAnalysisInstructions,
		Input: input, SchemaName: "DBDSLFunctionalAnalysis", Schema: functionalAnalysisSchema(),
		ReasoningEffort: opts.ReasoningEffort, Temperature: opts.Temperature, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{"template_version": promptTemplateVersion, "full_context_bytes": fmt.Sprint(len(fullInput)), "context_policy": "functional_atoms_compact_v2", "canonicalizer_version": "pipeline_ids_v2"},
	}, &proposal, func() []string {
		canonicalizeFunctionalAnalysisIDs(&proposal)
		qa = ValidateFunctionalAnalysisProposal(proposal, opts.RequirementAtoms)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	canonicalizeFunctionalAnalysisIDs(&proposal)
	qa = ValidateFunctionalAnalysisProposal(proposal, opts.RequirementAtoms)
	return proposal, qa, nil
}

func canonicalizeFunctionalAnalysisIDs(proposal *FunctionalAnalysisProposal) {
	sort.SliceStable(proposal.Actors, func(i, j int) bool {
		return strings.ToLower(proposal.Actors[i].Label+"|"+proposal.Actors[i].Description) < strings.ToLower(proposal.Actors[j].Label+"|"+proposal.Actors[j].Description)
	})
	actorMap := map[string]string{}
	for i := range proposal.Actors {
		oldID := proposal.Actors[i].ID
		newID := fmt.Sprintf("ACT-%03d", i+1)
		actorMap[oldID] = newID
		proposal.Actors[i].ID = newID
	}
	sort.SliceStable(proposal.FunctionalAreas, func(i, j int) bool {
		return strings.ToLower(proposal.FunctionalAreas[i].Label+"|"+proposal.FunctionalAreas[i].Purpose) < strings.ToLower(proposal.FunctionalAreas[j].Label+"|"+proposal.FunctionalAreas[j].Purpose)
	})
	for i := range proposal.FunctionalAreas {
		proposal.FunctionalAreas[i].ID = fmt.Sprintf("FA-%03d", i+1)
		for j, actorID := range proposal.FunctionalAreas[i].MainActors {
			if canonical, ok := actorMap[actorID]; ok {
				proposal.FunctionalAreas[i].MainActors[j] = canonical
			}
		}
	}
}

func RunCRUDMapping(ctx context.Context, client llm.Client, opts CRUDMappingStageOptions) (CRUDMappingProposal, StageQA, error) {
	if client == nil {
		return CRUDMappingProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || len(opts.FunctionalAreas) == 0 || len(opts.Actors) == 0 {
		return CRUDMappingProposal{}, StageQA{}, errors.New("output directory, functional areas and actors are required")
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	compactAtoms := crudRequirementAtomInputs(opts.RequirementAtoms)
	compactAreas := make([]conceptualFunctionalAreaInput, 0, len(opts.FunctionalAreas))
	for _, area := range opts.FunctionalAreas {
		compactAreas = append(compactAreas, conceptualFunctionalAreaInput{ID: area.ID, Label: area.Label, Purpose: area.Purpose, MainActors: area.MainActors, Atoms: area.Atoms, ModelingFocus: area.ModelingFocus})
	}
	input := mustCompactJSON(map[string]any{
		"requirement_atoms": compactAtoms,
		"functional_areas":  compactAreas, "actors": opts.Actors, "pipeline_version": "0.7.2",
		"output_contract": "crud_mapping", "template_version": promptTemplateVersion,
	})
	fullInput := mustJSON(map[string]any{"requirement_atoms": opts.RequirementAtoms, "functional_areas": opts.FunctionalAreas, "actors": opts.Actors})
	var proposal CRUDMappingProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 5, llm.Request{
		Stage: "crud_mapping", Model: opts.Model, Instructions: crudMappingInstructions,
		Input: input, SchemaName: "DBDSLCRUDMapping", Schema: crudMappingSchema(),
		ReasoningEffort: opts.ReasoningEffort, Temperature: opts.Temperature, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{"template_version": promptTemplateVersion, "full_context_bytes": fmt.Sprint(len(fullInput)), "context_policy": "crud_atoms_compact_v2", "canonicalizer_version": "pipeline_ids_v2"},
	}, &proposal, func() []string {
		canonicalizeCRUDOperationIDs(&proposal)
		normalizeCRUDSourceUnits(&proposal, opts.RequirementAtoms)
		qa = ValidateCRUDMappingProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Actors)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	canonicalizeCRUDOperationIDs(&proposal)
	normalizeCRUDSourceUnits(&proposal, opts.RequirementAtoms)
	qa = ValidateCRUDMappingProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Actors)
	return proposal, qa, nil
}

func canonicalizeCRUDOperationIDs(proposal *CRUDMappingProposal) {
	sort.SliceStable(proposal.Operations, func(i, j int) bool {
		left := proposal.Operations[i].FunctionalAreaID + "|" + proposal.Operations[i].ActorID + "|" + proposal.Operations[i].Label
		right := proposal.Operations[j].FunctionalAreaID + "|" + proposal.Operations[j].ActorID + "|" + proposal.Operations[j].Label
		return strings.ToLower(left) < strings.ToLower(right)
	})
	for i := range proposal.Operations {
		proposal.Operations[i].ID = fmt.Sprintf("OP-%03d", i+1)
	}
}

func normalizeCRUDSourceUnits(proposal *CRUDMappingProposal, atoms []RequirementAtomProposal) {
	byID := map[string]RequirementAtomProposal{}
	for _, atom := range atoms {
		byID[atom.ID] = atom
	}
	for i := range proposal.Operations {
		seen := map[string]bool{}
		var sources []string
		for _, atomID := range proposal.Operations[i].RequirementAtoms {
			for _, sourceID := range byID[atomID].SourceUnits {
				if !seen[sourceID] {
					seen[sourceID] = true
					sources = append(sources, sourceID)
				}
			}
		}
		sort.Strings(sources)
		proposal.Operations[i].SourceUnits = sources
	}
}

func ValidateRequirementAtomProposal(proposal RequirementAtomExtractionProposal, units []dsl.SourceUnit) StageQA {
	proposal.RequirementAtoms = NormalizeRequirementReviewSemantics(proposal.RequirementAtoms)
	qa := newStageQA(proposal.Warnings)
	sources := map[string]bool{}
	structuredExamples := map[string]bool{}
	structuredExampleDisposition := map[string]bool{}
	for _, unit := range units {
		sources[unit.ID] = true
		if unit.Kind == "structured_example" {
			structuredExamples[unit.ID] = true
		}
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
		if len(atom.SourceUnits) == 0 && atom.ModelingOutcome == "represented" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s represented atom has no source evidence", atom.ID))
		}
		for _, id := range atom.SourceUnits {
			if !sources[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown source unit %s", atom.ID, id))
			} else {
				covered[id] = true
				if structuredExamples[id] && atom.ExampleRole != "" && atom.ExampleRole != "none" {
					structuredExampleDisposition[id] = true
				}
			}
		}
		switch atom.ExampleRole {
		case "", "none":
		case "schema_shape", "seed_data":
			if atom.ModelingOutcome != "represented" || atom.PersistenceEffect != "required" {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s %s example must be represented with persistence_effect=required", atom.ID, atom.ExampleRole))
			}
		case "constraint_boundary":
			validOutcome := atom.ModelingOutcome == "represented" || atom.ModelingOutcome == "requires_app_logic"
			validPersistence := atom.PersistenceEffect == "required" || atom.PersistenceEffect == "derived_basis"
			if !validOutcome || !validPersistence {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s constraint_boundary example has no enforceable model consequence", atom.ID))
			}
		case "illustrative_instance":
			if atom.ModelingOutcome != "intentionally_not_in_db" || atom.PersistenceEffect != "not_required" {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s illustrative example must remain outside the persistent model", atom.ID))
			}
		default:
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has unknown example_role %q", atom.ID, atom.ExampleRole))
		}
		if atom.SupportLevel == "assumption" || atom.Confidence == "low" {
			reviewResolved := len(atom.ReviewDecisions) > 0
			if (!atom.RequiresReview && !reviewResolved) || len(atom.Warnings) == 0 {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s assumption/low-confidence atom requires a warning and either an open or linked review decision", atom.ID))
			}
		}
		if !containsStringValue(RequirementReviewClasses, atom.ReviewClass) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has unknown review_class %q", atom.ID, atom.ReviewClass))
		}
		if !containsStringValue(RequirementReviewTopics, atom.ReviewTopic) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has unknown review_topic %q", atom.ID, atom.ReviewTopic))
		}
		if atom.ReviewClass != ReviewClassNone && len(atom.Warnings) == 0 {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s non-none review_class requires a warning", atom.ID))
		}
		if atom.RequiresReview != IsBlockingReviewClass(atom.ReviewClass) && len(atom.ReviewDecisions) == 0 {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s review flag disagrees with review_class", atom.ID))
		}
		if atom.RequiresReview && strings.TrimSpace(atom.ReviewGroup) == "" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s blocking review has no review_group", atom.ID))
		}
		if atom.ModelingOutcome == "represented" && atom.PersistenceEffect == "not_required" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s cannot be represented with persistence_effect=not_required", atom.ID))
		}
	}
	for id := range structuredExamples {
		if !structuredExampleDisposition[id] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("structured example %s has no explicit example_role disposition", id))
		}
	}
	qa.Coverage["source_units_total"] = len(units)
	qa.Coverage["source_units_covered"] = len(covered)
	qa.Coverage["requirement_atoms"] = len(proposal.RequirementAtoms)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func containsStringValue(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
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
