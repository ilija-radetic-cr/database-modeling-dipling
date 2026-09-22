package llmpipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"dbdsl/internal/llm"
)

const LogicalEntityChunkSize = 12

type logicalChunkResult struct {
	patch PatchProposal
	qa    StageQA
	err   error
}

func runChunkedLogicalProjection(ctx context.Context, client llm.Client, opts LogicalProjectionOptions) (PatchProposal, StageQA, error) {
	chunks := logicalProjectionChunks(opts)
	fullContextBytes := len(logicalProjectionInput(opts))
	results := make([]logicalChunkResult, len(chunks))
	semaphore := make(chan struct{}, normalizedParallelism(opts.MaxParallelism, 3))
	var wait sync.WaitGroup
	var progressMu sync.Mutex
	completed := 0
	for index, chunk := range chunks {
		wait.Add(1)
		go func(index int, chunk LogicalProjectionOptions) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			chunk.FullContextBytes = fullContextBytes
			chunk.MaxOutputTokens = logicalChunkBudget(opts.MaxOutputTokens, len(chunk.ConceptualModel.EntityConcepts), len(chunk.ConceptualModel.Relationships), len(chunk.ConceptualModel.ConstraintConcepts))
			patch, qa, err := runLogicalProjectionCall(ctx, client, chunk)
			results[index] = logicalChunkResult{patch: patch, qa: qa, err: err}
			progressMu.Lock()
			completed++
			if opts.OnChunkProgress != nil {
				opts.OnChunkProgress(completed, len(chunks))
			}
			progressMu.Unlock()
		}(index, chunk)
	}
	wait.Wait()

	merged := PatchProposal{Warnings: []string{}, UnresolvedQuestions: []string{}, ConfidenceSummary: map[string]string{}}
	for index, result := range results {
		if result.err != nil {
			return merged, result.qa, fmt.Errorf("logical-projection chunk %d/%d: %w", index+1, len(chunks), result.err)
		}
		merged = mergeLogicalPatch(merged, result.patch)
	}
	qa := newStageQA(merged.Warnings)
	qa.Errors = validatePatchProposal(merged, sourceUnitIDSet(opts.SourceUnits), atomIDSet(opts.RequirementAtoms))
	qa.OK = len(qa.Errors) == 0
	qa.Coverage["patch_operations"] = len(merged.Operations)
	_ = writeJSONFile(filepath.Join(opts.OutDir, "llm_runs", "logical_projection_consolidation.json"), map[string]any{
		"strategy": "conceptual_entity_chunks_with_deterministic_merge", "chunk_size": LogicalEntityChunkSize,
		"chunk_count": len(chunks), "max_parallelism": normalizedParallelism(opts.MaxParallelism, 3), "validation": qa,
	})
	if !qa.OK {
		return merged, qa, fmt.Errorf("merged logical projection failed validation: %v", qa.Errors)
	}
	return merged, qa, nil
}

func logicalProjectionChunks(opts LogicalProjectionOptions) []LogicalProjectionOptions {
	entities := append([]ConceptualEntityProposal(nil), opts.ConceptualModel.EntityConcepts...)
	sort.SliceStable(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
	count := (len(entities) + LogicalEntityChunkSize - 1) / LogicalEntityChunkSize
	chunks := make([]LogicalProjectionOptions, count)
	entityChunk := map[string]int{}
	for index := range chunks {
		start := index * LogicalEntityChunkSize
		end := start + LogicalEntityChunkSize
		if end > len(entities) {
			end = len(entities)
		}
		chunk := opts
		chunk.ChunkIndex, chunk.ChunkCount = index+1, count
		chunk.PreviousProposal, chunk.ValidationErrors = nil, nil
		chunk.ConceptualModel = ConceptualModelProposal{ConfidenceSummary: map[string]string{}}
		chunk.ConceptualModel.EntityConcepts = append([]ConceptualEntityProposal(nil), entities[start:end]...)
		for _, entity := range chunk.ConceptualModel.EntityConcepts {
			chunk.PrimaryConceptIDs = append(chunk.PrimaryConceptIDs, entity.ID)
			entityChunk[entity.ID] = index
		}
		chunks[index] = chunk
	}

	for _, relationship := range opts.ConceptualModel.Relationships {
		index, ok := entityChunk[relationship.From]
		if !ok {
			index, ok = entityChunk[relationship.To]
		}
		if !ok {
			index = 0
		}
		chunks[index].ConceptualModel.Relationships = append(chunks[index].ConceptualModel.Relationships, relationship)
		addLogicalContextEntity(&chunks[index].ConceptualModel, entities, relationship.From)
		addLogicalContextEntity(&chunks[index].ConceptualModel, entities, relationship.To)
	}
	for _, constraint := range opts.ConceptualModel.ConstraintConcepts {
		index := firstEntityChunk(constraint.Targets, entityChunk)
		chunks[index].ConceptualModel.ConstraintConcepts = append(chunks[index].ConceptualModel.ConstraintConcepts, constraint)
	}
	assignLogicalPlanElements(chunks, opts.ConceptualModel.LifecycleConcepts, func(model *ConceptualModelProposal, items []PlanElementProposal) {
		model.LifecycleConcepts = append(model.LifecycleConcepts, items...)
	})
	assignLogicalPlanElements(chunks, opts.ConceptualModel.DerivedConcepts, func(model *ConceptualModelProposal, items []PlanElementProposal) {
		model.DerivedConcepts = append(model.DerivedConcepts, items...)
	})
	assignLogicalPlanElements(chunks, opts.ConceptualModel.FileConcepts, func(model *ConceptualModelProposal, items []PlanElementProposal) {
		model.FileConcepts = append(model.FileConcepts, items...)
	})
	assignLogicalPlanElements(chunks, opts.ConceptualModel.ImportConcepts, func(model *ConceptualModelProposal, items []PlanElementProposal) {
		model.ImportConcepts = append(model.ImportConcepts, items...)
	})

	chunkAtoms := make([]map[string]bool, len(chunks))
	for index := range chunks {
		chunkAtoms[index] = conceptualModelAtomIDs(chunks[index].ConceptualModel)
		chunks[index].DesignObligations = nil
	}
	for _, obligation := range opts.DesignObligations {
		index := firstIntersectingChunk(obligation.RequirementAtoms, chunkAtoms)
		chunks[index].DesignObligations = append(chunks[index].DesignObligations, obligation)
	}
	for index := range chunks {
		chunks[index] = compactLogicalOptions(chunks[index])
	}
	return chunks
}

func addLogicalContextEntity(model *ConceptualModelProposal, all []ConceptualEntityProposal, id string) {
	for _, existing := range model.EntityConcepts {
		if existing.ID == id {
			return
		}
	}
	for _, entity := range all {
		if entity.ID == id {
			model.EntityConcepts = append(model.EntityConcepts, entity)
			return
		}
	}
}

func firstEntityChunk(ids []string, byEntity map[string]int) int {
	for _, id := range ids {
		if index, ok := byEntity[id]; ok {
			return index
		}
	}
	return 0
}

func assignLogicalPlanElements(chunks []LogicalProjectionOptions, items []PlanElementProposal, assign func(*ConceptualModelProposal, []PlanElementProposal)) {
	for _, item := range items {
		atomSets := make([]map[string]bool, len(chunks))
		for index := range chunks {
			atomSets[index] = conceptualModelAtomIDs(chunks[index].ConceptualModel)
		}
		index := firstIntersectingChunk(item.RequirementAtoms, atomSets)
		assign(&chunks[index].ConceptualModel, []PlanElementProposal{item})
	}
}

func firstIntersectingChunk(ids []string, sets []map[string]bool) int {
	for index, set := range sets {
		if intersects(ids, set) {
			return index
		}
	}
	return 0
}

func conceptualModelAtomIDs(model ConceptualModelProposal) map[string]bool {
	ids := map[string]bool{}
	collect := func(evidence EvidenceProposal) {
		for _, id := range evidence.RequirementAtoms {
			ids[id] = true
		}
	}
	for _, entity := range model.EntityConcepts {
		collect(entity.Evidence)
		for _, attribute := range entity.Attributes {
			collect(attribute.Evidence)
		}
	}
	for _, relationship := range model.Relationships {
		collect(relationship.Evidence)
	}
	for _, constraint := range model.ConstraintConcepts {
		collect(constraint.Evidence)
	}
	for _, group := range [][]PlanElementProposal{model.LifecycleConcepts, model.DerivedConcepts, model.FileConcepts, model.ImportConcepts} {
		for _, item := range group {
			for _, id := range item.RequirementAtoms {
				ids[id] = true
			}
		}
	}
	return ids
}

func logicalChunkBudget(ceiling, entities, relationships, constraints int) int {
	if ceiling <= 0 {
		ceiling = defaultLogicalMaxOutputTokens
	}
	if ceiling > 16000 {
		ceiling = 16000
	}
	budget := 6000 + 450*entities + 220*relationships + 160*constraints
	if budget < 8000 {
		budget = 8000
	}
	if budget > ceiling {
		budget = ceiling
	}
	return budget
}
