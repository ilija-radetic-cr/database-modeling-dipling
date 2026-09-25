package llmpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"dbdsl/internal/llm"
)

const (
	ConceptualObligationChunkSize   = 24
	defaultConceptualMaxParallelism = 3
	conceptualChunkMaxOutputTokens  = 12000
	conceptualRepairMaxOutputTokens = 10000
)

type conceptualObligationChunk struct {
	Index       int
	Count       int
	Areas       []string
	Obligations []DesignObligation
}

type conceptualChunkResult struct {
	Proposal ConceptualModelProposal
	QA       StageQA
	Err      error
}

func runChunkedConceptualModel(ctx context.Context, client llm.Client, opts ConceptualModelOptions) (ConceptualModelProposal, StageQA, error) {
	chunks := conceptualObligationChunks(opts)
	fullContextBytes := conceptualFullContextBytes(opts)
	results := make([]conceptualChunkResult, len(chunks))
	maxParallelism := normalizedParallelism(opts.MaxParallelism, defaultConceptualMaxParallelism)
	semaphore := make(chan struct{}, maxParallelism)
	var wait sync.WaitGroup
	var progressMu sync.Mutex
	completedChunks := 0
	for index, chunk := range chunks {
		wait.Add(1)
		go func(index int, chunk conceptualObligationChunk) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			proposal, qa, err := runConceptualChunk(ctx, client, opts, chunk, fullContextBytes)
			results[index] = conceptualChunkResult{Proposal: proposal, QA: qa, Err: err}
			progressMu.Lock()
			completedChunks++
			if opts.OnChunkProgress != nil {
				opts.OnChunkProgress("generation", completedChunks, len(chunks))
			}
			progressMu.Unlock()
		}(index, chunk)
	}
	wait.Wait()

	proposal := ConceptualModelProposal{}
	for index, result := range results {
		if result.Err != nil {
			qa := ValidateConceptualModelWithObligations(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.ReviewDecisions, opts.DesignObligations)
			writeConceptualConsolidation(opts, chunks, 0, qa, "failed")
			return proposal, qa, fmt.Errorf("conceptual-model chunk %d/%d: %w", index+1, len(chunks), result.Err)
		}
		proposal = mergeConceptualModel(proposal, result.Proposal)
	}

	qa := ValidateConceptualModelWithObligations(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.ReviewDecisions, opts.DesignObligations)
	if qa.OK {
		writeConceptualConsolidation(opts, chunks, 0, qa, "completed")
		return proposal, qa, nil
	}

	maxRepairAttempts := opts.MaxRepairAttempts
	if maxRepairAttempts < 0 {
		maxRepairAttempts = 0
	}
	totalRepairChunks := 0
	for repairRound := 1; !qa.OK && repairRound <= maxRepairAttempts; repairRound++ {
		repairScopes := conceptualRepairScopes(opts, qa.Errors)
		for index := range repairScopes {
			repairScopes[index].Round = repairRound
		}
		repairResults := make([]conceptualChunkResult, len(repairScopes))
		baseProposal := proposal
		baseErrors := append([]string(nil), qa.Errors...)
		completedRepairs := 0
		for index, scope := range repairScopes {
			wait.Add(1)
			go func(index int, scope conceptualRepairScope) {
				defer wait.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				fragment, repairQA, err := runConceptualRepairScope(ctx, client, opts, baseProposal, baseErrors, scope, fullContextBytes)
				repairResults[index] = conceptualChunkResult{Proposal: fragment, QA: repairQA, Err: err}
				progressMu.Lock()
				completedRepairs++
				if opts.OnChunkProgress != nil {
					opts.OnChunkProgress("repair", completedRepairs, len(repairScopes))
				}
				progressMu.Unlock()
			}(index, scope)
		}
		wait.Wait()
		totalRepairChunks += len(repairScopes)
		for index, result := range repairResults {
			if result.Err != nil {
				writeConceptualConsolidation(opts, chunks, totalRepairChunks, qa, "failed")
				return proposal, qa, fmt.Errorf("conceptual-model repair round %d chunk %d/%d: %w", repairRound, index+1, len(repairScopes), result.Err)
			}
			proposal = mergeConceptualRepair(proposal, result.Proposal)
		}
		qa = ValidateConceptualModelWithObligations(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.ReviewDecisions, opts.DesignObligations)
	}
	status := "completed"
	if !qa.OK {
		status = "failed"
	}
	writeConceptualConsolidation(opts, chunks, totalRepairChunks, qa, status)
	if !qa.OK {
		return proposal, qa, fmt.Errorf("conceptual_model_repair proposal failed preflight validation: %s", strings.Join(qa.Errors, "; "))
	}
	return proposal, qa, nil
}

func runConceptualChunk(ctx context.Context, client llm.Client, opts ConceptualModelOptions, chunk conceptualObligationChunk, fullContextBytes int) (ConceptualModelProposal, StageQA, error) {
	compact := compactConceptualOptions(opts, chunk.Obligations)
	input := conceptualChunkInput(compact, chunk)
	budget := conceptualChunkBudget(opts.MaxOutputTokens, len(chunk.Obligations), len(compact.RequirementAtoms))
	metadata := map[string]string{
		"template_version":        resolvedPromptVersion(opts.PromptVersion),
		"full_context_bytes":      fmt.Sprint(fullContextBytes),
		"context_policy":          "functional_obligation_chunks_v1",
		"canonicalizer_version":   "domain_slugs_v1",
		"call_reason":             "schema_design_chunk",
		"chunk_index":             fmt.Sprint(chunk.Index),
		"chunk_count":             fmt.Sprint(chunk.Count),
		"budget_policy":           "adaptive_v1",
		"verifier_version":        "conceptual_coverage_v2+review_semantics_v2",
		"stage_max_output_tokens": fmt.Sprint(opts.MaxOutputTokens),
	}
	if chunk.Count > 1 {
		metadata["run_key"] = fmt.Sprintf("chunk_%03d", chunk.Index)
	}
	var proposal ConceptualModelProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 8, llm.Request{
		Stage: "conceptual_model", Model: opts.Model, Instructions: conceptualChunkInstructions,
		Input: input, SchemaName: "DBDSLConceptualModelFragment", Schema: conceptualModelSchema(),
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: budget, Metadata: metadata,
	}, &proposal, func() []string {
		sanitizeConceptualReferences(&proposal, opts.ReviewDecisions)
		qa = validateConceptualChunk(proposal, compact, chunk.Obligations)
		return qa.Errors
	})
	if err != nil && conceptualProposalHasContent(proposal) && len(qa.Errors) > 0 {
		// A structurally parseable partial result is still useful. It is not cached
		// by runStructuredStage; the global pass repairs only its missing coverage.
		return proposal, qa, nil
	}
	return proposal, qa, err
}

func conceptualObligationChunks(opts ConceptualModelOptions) []conceptualObligationChunk {
	active := make([]DesignObligation, 0, len(opts.DesignObligations))
	for _, obligation := range opts.DesignObligations {
		if obligation.Status == "not_required" || obligation.Persistence == "not_required" {
			continue
		}
		active = append(active, obligation)
	}
	if len(active) == 0 {
		return []conceptualObligationChunk{{Index: 1, Count: 1, Areas: []string{"all"}}}
	}

	atomAreas := map[string][]string{}
	for _, area := range opts.FunctionalAreas {
		for _, atomID := range area.Atoms {
			atomAreas[atomID] = appendUnique(atomAreas[atomID], area.ID)
		}
	}
	byArea := map[string][]DesignObligation{}
	for _, obligation := range active {
		areas := []string{}
		for _, atomID := range obligation.RequirementAtoms {
			areas = append(areas, atomAreas[atomID]...)
		}
		areas = sortedUniqueStrings(areas)
		area := "unassigned"
		if len(areas) > 0 {
			area = areas[0]
		}
		byArea[area] = append(byArea[area], obligation)
	}
	areaIDs := make([]string, 0, len(byArea))
	for areaID := range byArea {
		areaIDs = append(areaIDs, areaID)
	}
	sort.Strings(areaIDs)
	for _, areaID := range areaIDs {
		sort.SliceStable(byArea[areaID], func(i, j int) bool { return byArea[areaID][i].ID < byArea[areaID][j].ID })
	}

	chunks := []conceptualObligationChunk{}
	current := conceptualObligationChunk{}
	flush := func() {
		if len(current.Obligations) == 0 {
			return
		}
		current.Areas = sortedUniqueStrings(current.Areas)
		chunks = append(chunks, current)
		current = conceptualObligationChunk{}
	}
	for _, areaID := range areaIDs {
		remaining := byArea[areaID]
		for len(remaining) > 0 {
			capacity := ConceptualObligationChunkSize - len(current.Obligations)
			if capacity == 0 {
				flush()
				capacity = ConceptualObligationChunkSize
			}
			take := len(remaining)
			if take > capacity {
				take = capacity
			}
			current.Areas = append(current.Areas, areaID)
			current.Obligations = append(current.Obligations, remaining[:take]...)
			remaining = remaining[take:]
			if len(current.Obligations) == ConceptualObligationChunkSize {
				flush()
			}
		}
	}
	flush()
	for index := range chunks {
		chunks[index].Index = index + 1
		chunks[index].Count = len(chunks)
	}
	return chunks
}

func conceptualChunkInput(opts ConceptualModelOptions, chunk conceptualObligationChunk) string {
	var root map[string]any
	if err := json.Unmarshal([]byte(conceptualModelInput(opts)), &root); err != nil {
		panic(err)
	}
	obligationIDs := make([]string, 0, len(chunk.Obligations))
	for _, obligation := range chunk.Obligations {
		obligationIDs = append(obligationIDs, obligation.ID)
	}
	root["output_contract"] = "conceptual_model_fragment"
	root["chunk_scope"] = map[string]any{
		"index": chunk.Index, "count": chunk.Count, "functional_areas": chunk.Areas,
		"design_obligation_ids": obligationIDs,
	}
	return mustCompactJSON(root)
}

func conceptualChunkBudget(ceiling, obligations, atoms int) int {
	if ceiling <= 0 {
		ceiling = defaultConceptualMaxOutputTokens
	}
	if ceiling > conceptualChunkMaxOutputTokens {
		ceiling = conceptualChunkMaxOutputTokens
	}
	budget := 4000 + 320*obligations + 35*atoms
	if budget < 6000 {
		budget = 6000
	}
	if budget > ceiling {
		budget = ceiling
	}
	return budget
}

func validateConceptualChunk(proposal ConceptualModelProposal, opts ConceptualModelOptions, obligations []DesignObligation) StageQA {
	qa := ValidateConceptualModelWithObligations(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.ReviewDecisions, obligations)
	filtered := make([]string, 0, len(qa.Errors))
	for _, issue := range qa.Errors {
		if issue == "conceptual model produced no entity concepts" || strings.HasSuffix(issue, " references unknown relationship endpoint") {
			continue
		}
		filtered = append(filtered, issue)
	}
	qa.Errors = sortedUniqueStrings(filtered)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func conceptualProposalHasContent(proposal ConceptualModelProposal) bool {
	return len(proposal.EntityConcepts)+len(proposal.Relationships)+len(proposal.ConstraintConcepts)+
		len(proposal.LifecycleConcepts)+len(proposal.DerivedConcepts)+len(proposal.FileConcepts)+len(proposal.ImportConcepts) > 0
}

type conceptualRepairScope struct {
	Round       int
	Index       int
	Count       int
	Areas       []string
	Obligations []DesignObligation
	Errors      []string
}

func conceptualRepairScopes(opts ConceptualModelOptions, validationErrors []string) []conceptualRepairScope {
	missing := missingConceptualObligations(opts.DesignObligations, validationErrors)
	chunks := conceptualObligationChunks(ConceptualModelOptions{FunctionalAreas: opts.FunctionalAreas, DesignObligations: missing})
	if len(missing) == 0 {
		chunks = []conceptualObligationChunk{{Index: 1, Count: 1, Areas: []string{"structural"}}}
	}
	scopes := make([]conceptualRepairScope, len(chunks))
	for index, chunk := range chunks {
		scopes[index] = conceptualRepairScope{Index: index + 1, Count: len(chunks), Areas: chunk.Areas, Obligations: chunk.Obligations}
	}
	for _, issue := range validationErrors {
		assigned := false
		for index := range scopes {
			for _, obligation := range scopes[index].Obligations {
				if strings.Contains(issue, "design obligation "+obligation.ID+" ") {
					scopes[index].Errors = append(scopes[index].Errors, issue)
					assigned = true
					break
				}
			}
			if assigned {
				break
			}
		}
		if !assigned {
			scopes[0].Errors = append(scopes[0].Errors, issue)
		}
	}
	return scopes
}

func missingConceptualObligations(obligations []DesignObligation, validationErrors []string) []DesignObligation {
	missing := []DesignObligation{}
	for _, obligation := range obligations {
		needle := "design obligation " + obligation.ID + " "
		for _, issue := range validationErrors {
			if strings.Contains(issue, needle) {
				missing = append(missing, obligation)
				break
			}
		}
	}
	return missing
}

func runConceptualRepairScope(ctx context.Context, client llm.Client, opts ConceptualModelOptions, base ConceptualModelProposal, baseErrors []string, scope conceptualRepairScope, fullContextBytes int) (ConceptualModelProposal, StageQA, error) {
	input, compact := conceptualRepairScopeInput(opts, base, scope)
	budget := conceptualRepairChunkBudget(opts.MaxOutputTokens, len(scope.Obligations), len(compact.RequirementAtoms))
	var fragment ConceptualModelProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 8, llm.Request{
		Stage: "conceptual_model_repair", Model: opts.Model, Instructions: conceptualRepairInstructions,
		Input: input, SchemaName: "DBDSLConceptualModelRepairFragment", Schema: conceptualModelSchema(),
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: budget,
		Metadata: map[string]string{
			"template_version": resolvedPromptVersion(opts.PromptVersion), "run_key": fmt.Sprintf("round_%03d_chunk_%03d", scope.Round, scope.Index),
			"context_policy": "obligation_delta_repair_v2", "canonicalizer_version": "domain_slugs_v1",
			"call_reason": "semantic_delta_repair", "issue_id": textHash(strings.Join(scope.Errors, "|")),
			"full_context_bytes": fmt.Sprint(fullContextBytes), "budget_policy": "adaptive_v1",
			"stage_max_output_tokens": fmt.Sprint(opts.MaxOutputTokens), "chunk_index": fmt.Sprint(scope.Index),
			"chunk_count": fmt.Sprint(scope.Count),
		},
	}, &fragment, func() []string {
		sanitizeConceptualReferences(&fragment, opts.ReviewDecisions)
		merged := mergeConceptualRepair(base, fragment)
		qa = ValidateConceptualModelWithObligations(merged, opts.SourceUnits, opts.RequirementAtoms, opts.ReviewDecisions, opts.DesignObligations)
		return conceptualRepairValidationErrors(baseErrors, scope.Errors, qa.Errors)
	})
	return fragment, qa, err
}

func conceptualRepairScopeInput(opts ConceptualModelOptions, proposal ConceptualModelProposal, scope conceptualRepairScope) (string, ConceptualModelOptions) {
	atomIDs := map[string]bool{}
	for _, obligation := range scope.Obligations {
		for _, atomID := range obligation.RequirementAtoms {
			atomIDs[atomID] = true
		}
	}
	focused := focusedConceptualProposal(proposal, atomIDs, scope.Errors)
	collectConceptualProposalAtoms(focused, atomIDs)
	compact := compactConceptualOptionsForAtoms(opts, scope.Obligations, atomIDs)
	return mustCompactJSON(map[string]any{
		"evidence_context":           json.RawMessage(conceptualModelInput(compact)),
		"existing_concept_registry":  conceptualConceptRegistry(proposal),
		"affected_existing_fragment": focused,
		"validation_errors":          scope.Errors,
		"missing_design_obligations": modelDesignObligationInputs(scope.Obligations),
		"chunk_scope":                map[string]any{"index": scope.Index, "count": scope.Count, "functional_areas": scope.Areas},
		"output_contract":            "conceptual_model_delta_repair_fragment",
		"pipeline_version":           "0.7.2", "template_version": resolvedPromptVersion(opts.PromptVersion),
	}), compact
}

func conceptualRepairChunkBudget(ceiling, obligations, atoms int) int {
	if ceiling <= 0 {
		ceiling = defaultConceptualMaxOutputTokens
	}
	if ceiling > conceptualRepairMaxOutputTokens {
		ceiling = conceptualRepairMaxOutputTokens
	}
	budget := 3500 + 400*obligations + 30*atoms
	if budget < 5000 {
		budget = 5000
	}
	if budget > ceiling {
		budget = ceiling
	}
	return budget
}

func focusedConceptualProposal(proposal ConceptualModelProposal, atomIDs map[string]bool, validationErrors []string) ConceptualModelProposal {
	mentions := func(id string) bool {
		if id == "" {
			return false
		}
		for _, issue := range validationErrors {
			if strings.Contains(issue, id) {
				return true
			}
		}
		return false
	}
	evidenceSelected := func(evidence EvidenceProposal) bool { return intersects(evidence.RequirementAtoms, atomIDs) }
	planSelected := func(item PlanElementProposal) bool {
		return mentions(item.ID) || intersects(item.RequirementAtoms, atomIDs)
	}

	out := ConceptualModelProposal{ConfidenceSummary: map[string]string{}}
	for _, entity := range proposal.EntityConcepts {
		selected := mentions(entity.ID) || evidenceSelected(entity.Evidence)
		for _, attribute := range entity.Attributes {
			selected = selected || mentions(attribute.ID) || evidenceSelected(attribute.Evidence)
		}
		if selected {
			out.EntityConcepts = append(out.EntityConcepts, entity)
		}
	}
	for _, relationship := range proposal.Relationships {
		if mentions(relationship.ID) || evidenceSelected(relationship.Evidence) {
			out.Relationships = append(out.Relationships, relationship)
		}
	}
	for _, constraint := range proposal.ConstraintConcepts {
		if mentions(constraint.ID) || evidenceSelected(constraint.Evidence) {
			out.ConstraintConcepts = append(out.ConstraintConcepts, constraint)
		}
	}
	for _, item := range proposal.LifecycleConcepts {
		if planSelected(item) {
			out.LifecycleConcepts = append(out.LifecycleConcepts, item)
		}
	}
	for _, item := range proposal.DerivedConcepts {
		if planSelected(item) {
			out.DerivedConcepts = append(out.DerivedConcepts, item)
		}
	}
	for _, item := range proposal.FileConcepts {
		if planSelected(item) {
			out.FileConcepts = append(out.FileConcepts, item)
		}
	}
	for _, item := range proposal.ImportConcepts {
		if planSelected(item) {
			out.ImportConcepts = append(out.ImportConcepts, item)
		}
	}
	for _, issue := range validationErrors {
		if strings.Contains(issue, "unresolved review IDs") {
			out.UnresolvedReviewIDs = append([]string(nil), proposal.UnresolvedReviewIDs...)
			break
		}
	}
	return out
}

func collectConceptualProposalAtoms(proposal ConceptualModelProposal, selected map[string]bool) {
	collectEvidence := func(evidence EvidenceProposal) {
		for _, atomID := range evidence.RequirementAtoms {
			selected[atomID] = true
		}
	}
	for _, entity := range proposal.EntityConcepts {
		collectEvidence(entity.Evidence)
		for _, attribute := range entity.Attributes {
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
		for _, item := range group {
			for _, atomID := range item.RequirementAtoms {
				selected[atomID] = true
			}
		}
	}
}

func conceptualConceptRegistry(proposal ConceptualModelProposal) []map[string]string {
	registry := []map[string]string{}
	for _, entity := range proposal.EntityConcepts {
		registry = append(registry, map[string]string{"id": entity.ID, "label": entity.Label, "kind": entity.Kind, "element_type": "entity"})
	}
	for _, relationship := range proposal.Relationships {
		registry = append(registry, map[string]string{"id": relationship.ID, "label": relationship.Label, "from": relationship.From, "to": relationship.To, "element_type": "relationship"})
	}
	for _, constraint := range proposal.ConstraintConcepts {
		registry = append(registry, map[string]string{"id": constraint.ID, "label": constraint.Label, "kind": constraint.Kind, "element_type": "constraint"})
	}
	groups := []struct {
		kind  string
		items []PlanElementProposal
	}{
		{"lifecycle", proposal.LifecycleConcepts}, {"derived", proposal.DerivedConcepts},
		{"file", proposal.FileConcepts}, {"import", proposal.ImportConcepts},
	}
	for _, group := range groups {
		for _, item := range group.items {
			registry = append(registry, map[string]string{"id": item.ID, "label": item.Label, "kind": item.Kind, "element_type": group.kind})
		}
	}
	sort.SliceStable(registry, func(i, j int) bool {
		if registry[i]["element_type"] == registry[j]["element_type"] {
			return registry[i]["id"] < registry[j]["id"]
		}
		return registry[i]["element_type"] < registry[j]["element_type"]
	})
	return registry
}

func conceptualRepairValidationErrors(baseline, scoped, current []string) []string {
	baselineSet, currentSet := map[string]bool{}, map[string]bool{}
	for _, issue := range baseline {
		baselineSet[issue] = true
	}
	for _, issue := range current {
		currentSet[issue] = true
	}
	issues := []string{}
	for _, issue := range scoped {
		if currentSet[issue] {
			issues = append(issues, issue)
		}
	}
	for _, issue := range current {
		if !baselineSet[issue] {
			issues = append(issues, "repair introduced: "+issue)
		}
	}
	return sortedUniqueStrings(issues)
}

func conceptualFullContextBytes(opts ConceptualModelOptions) int {
	return len(mustJSON(map[string]any{
		"source_units": opts.SourceUnits, "requirement_atoms": opts.RequirementAtoms,
		"functional_areas": opts.FunctionalAreas, "actors": opts.Actors, "crud_operations": opts.Operations,
		"review_decisions": opts.ReviewDecisionContext, "design_obligations": opts.DesignObligations,
	}))
}

func writeConceptualConsolidation(opts ConceptualModelOptions, chunks []conceptualObligationChunk, repairChunks int, qa StageQA, status string) {
	chunkSummary := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		chunkSummary = append(chunkSummary, map[string]any{
			"index": chunk.Index, "areas": chunk.Areas, "design_obligations": len(chunk.Obligations),
		})
	}
	_ = writeJSONFile(filepath.Join(opts.OutDir, "llm_runs", "conceptual_model_consolidation.json"), map[string]any{
		"strategy": "functional_obligation_chunks_with_deterministic_merge", "status": status,
		"chunk_size": ConceptualObligationChunkSize, "chunk_count": len(chunks),
		"repair_chunk_count": repairChunks, "max_parallelism": normalizedParallelism(opts.MaxParallelism, defaultConceptualMaxParallelism),
		"chunks": chunkSummary, "validation": qa,
	})
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// sanitizeConceptualReferences removes references that cannot be valid instead
// of paying for an LLM repair round: unresolved_review_ids may only name review
// candidates (obligation IDs there are a model mistake, and coverage is checked
// separately), and evidence may only cite accepted review decisions (free text
// written into an ID field is dropped). Every removal is kept as a warning.
func sanitizeConceptualReferences(proposal *ConceptualModelProposal, reviewDecisions []string) {
	known := map[string]bool{}
	for _, id := range reviewDecisions {
		known[id] = true
	}
	kept := []string{}
	for _, id := range proposal.UnresolvedReviewIDs {
		if strings.HasPrefix(id, "RC-") {
			kept = append(kept, id)
		} else {
			proposal.Warnings = append(proposal.Warnings, "Ignored non-review ID in unresolved_review_ids: "+id)
		}
	}
	proposal.UnresolvedReviewIDs = kept
	clean := func(owner string, evidence *EvidenceProposal) {
		valid := []string{}
		for _, id := range evidence.ReviewDecisions {
			if known[id] {
				valid = append(valid, id)
			} else {
				proposal.Warnings = append(proposal.Warnings, fmt.Sprintf("%s: dropped unknown review decision reference %q", owner, id))
			}
		}
		evidence.ReviewDecisions = valid
		if evidence.SupportLevel == "assumption" && len(valid) == 0 {
			evidence.SupportLevel = "inferred"
		}
	}
	for i := range proposal.EntityConcepts {
		entity := &proposal.EntityConcepts[i]
		clean(entity.ID, &entity.Evidence)
		for j := range entity.Attributes {
			clean(entity.ID+"."+entity.Attributes[j].ID, &entity.Attributes[j].Evidence)
		}
	}
	for i := range proposal.Relationships {
		clean(proposal.Relationships[i].ID, &proposal.Relationships[i].Evidence)
	}
	for i := range proposal.ConstraintConcepts {
		clean(proposal.ConstraintConcepts[i].ID, &proposal.ConstraintConcepts[i].Evidence)
	}
}
