package llmpipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"dbdsl/internal/llm"
)

const (
	SourceSegmentationChunkSize = 300
	sourceSegmentationOverlap   = 24
)

type SourceSegmentationOptions struct {
	OutDir          string
	Resources       []CombinedDocumentResource
	Segments        []SourceSegment
	Fallback        CombinedDocumentProposal
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	MaxParallelism  int
	PromptVersion   string
}

type sourceSegmentationClassification struct {
	CandidateID       string   `json:"candidate_id"`
	Role              string   `json:"role"`
	Boundary          string   `json:"boundary"`
	JoinToCandidateID string   `json:"join_to_candidate_id"`
	Confidence        string   `json:"confidence"`
	RequiresReview    bool     `json:"requires_review"`
	Warnings          []string `json:"warnings"`
}

type sourceSegmentationClassificationProposal struct {
	Classifications   []sourceSegmentationClassification `json:"classifications"`
	Warnings          []string                           `json:"warnings"`
	ConfidenceSummary map[string]string                  `json:"confidence_summary"`
}

type segmentationChunk struct {
	CoreStart, CoreEnd       int
	ContextStart, ContextEnd int
}

// BuildSourceSegmentationCandidates deterministically turns physical source
// segments into exact, addressable spans. The LLM only receives these IDs and
// may not return replacement text.
func BuildSourceSegmentationCandidates(resources []CombinedDocumentResource, segments []SourceSegment) []SourceSegmentationCandidate {
	structured := map[string]bool{}
	for _, resource := range resources {
		structured[resource.ID] = resource.FileType == "json" || resource.FileType == "csv" || resource.FileType == "xml"
	}
	candidates := make([]SourceSegmentationCandidate, 0, len(segments))
	for _, segment := range segments {
		units := splitCombinedText(segment.Text, structured[segment.ResourceID])
		if len(units) == 0 {
			units = []combinedTextUnit{{text: segment.Text, start: 0, end: len(segment.Text), structural: true}}
		}
		for index, unit := range units {
			role := "sentence"
			if unit.structural || structured[segment.ResourceID] {
				role = "other_structural"
			}
			candidates = append(candidates, SourceSegmentationCandidate{
				ID: fmt.Sprintf("%s-F-%02d", segment.ID, index+1), SegmentID: segment.ID,
				ResourceID: segment.ResourceID, LineStart: segment.LineStart, LineEnd: segment.LineEnd,
				StartByte: unit.start, EndByte: unit.end, ExactText: unit.text, SuggestedRole: role,
			})
		}
	}
	return candidates
}

// RunSourceSegmentation uses the LLM when available and fails over to the
// existing deterministic projection without weakening physical fidelity.
func RunSourceSegmentation(ctx context.Context, client llm.Client, opts SourceSegmentationOptions) (SourceSegmentationProposal, CombinedDocumentProposal, SourceSegmentationQA) {
	candidates := BuildSourceSegmentationCandidates(opts.Resources, opts.Segments)
	if client == nil {
		return sourceSegmentationFallback(candidates, opts.Fallback, "LLM client is unavailable")
	}
	if opts.OutDir == "" || len(candidates) == 0 {
		return sourceSegmentationFallback(candidates, opts.Fallback, "segmentation input is empty")
	}
	opts.Model = nonEmpty(opts.Model, llm.DefaultModel)
	opts.ReasoningEffort = nonEmpty(opts.ReasoningEffort, llm.DefaultReasoningEffort)
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = llm.DefaultMaxOutputTokens
	}

	chunks := buildSegmentationChunks(candidates)
	results := make([]sourceSegmentationClassificationProposal, len(chunks))
	errs := make([]error, len(chunks))
	parallelism := normalizedParallelism(opts.MaxParallelism, 3)
	semaphore := make(chan struct{}, parallelism)
	var wait sync.WaitGroup
	for index, chunk := range chunks {
		wait.Add(1)
		go func(index int, chunk segmentationChunk) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[index], errs[index] = runSourceSegmentationChunk(ctx, client, opts, candidates, chunk, index+1, len(chunks))
		}(index, chunk)
	}
	wait.Wait()

	merged := sourceSegmentationClassificationProposal{
		Classifications: []sourceSegmentationClassification{}, Warnings: []string{},
		ConfidenceSummary: map[string]string{"strategy": "bounded_candidate_chunks_with_deterministic_merge"},
	}
	for index := range results {
		if errs[index] != nil {
			return sourceSegmentationFallback(candidates, opts.Fallback, fmt.Sprintf("LLM segmentation chunk %d/%d failed: %v", index+1, len(chunks), errs[index]))
		}
		merged.Classifications = append(merged.Classifications, results[index].Classifications...)
		merged.Warnings = append(merged.Warnings, results[index].Warnings...)
	}
	proposal, document, qa := buildLLMSegmentation(candidates, merged)
	if !qa.OK {
		return sourceSegmentationFallback(candidates, opts.Fallback, "LLM segmentation failed backend validation: "+strings.Join(qa.Errors, "; "))
	}
	_ = writeJSONFile(filepath.Join(opts.OutDir, "llm_runs", "source_segmentation_consolidation.json"), map[string]any{
		"strategy": proposal.Strategy, "chunk_count": len(chunks), "chunk_size": SourceSegmentationChunkSize,
		"overlap": sourceSegmentationOverlap, "candidates": len(candidates), "groups": len(proposal.Groups), "validation": qa,
	})
	return proposal, document, qa
}

func buildSegmentationChunks(candidates []SourceSegmentationCandidate) []segmentationChunk {
	var chunks []segmentationChunk
	for start := 0; start < len(candidates); start += SourceSegmentationChunkSize {
		end := start + SourceSegmentationChunkSize
		if end > len(candidates) {
			end = len(candidates)
		}
		contextStart := start - sourceSegmentationOverlap
		if contextStart < 0 {
			contextStart = 0
		}
		contextEnd := end + sourceSegmentationOverlap
		if contextEnd > len(candidates) {
			contextEnd = len(candidates)
		}
		chunks = append(chunks, segmentationChunk{CoreStart: start, CoreEnd: end, ContextStart: contextStart, ContextEnd: contextEnd})
	}
	return chunks
}

func runSourceSegmentationChunk(ctx context.Context, client llm.Client, opts SourceSegmentationOptions, candidates []SourceSegmentationCandidate, chunk segmentationChunk, chunkIndex, chunkCount int) (sourceSegmentationClassificationProposal, error) {
	items := make([]map[string]any, 0, chunk.ContextEnd-chunk.ContextStart)
	coreIDs := make([]string, 0, chunk.CoreEnd-chunk.CoreStart)
	languageByResource := map[string]string{}
	for _, resource := range opts.Resources {
		languageByResource[resource.ID] = resource.Language
	}
	for index := chunk.ContextStart; index < chunk.ContextEnd; index++ {
		candidate := candidates[index]
		scope := "context"
		if index >= chunk.CoreStart && index < chunk.CoreEnd {
			scope = "core"
			coreIDs = append(coreIDs, candidate.ID)
		}
		items = append(items, map[string]any{
			"id": candidate.ID, "resource_id": candidate.ResourceID, "line": candidate.LineStart,
			"language": languageByResource[candidate.ResourceID], "text": candidate.ExactText,
			"suggested_role": candidate.SuggestedRole, "scope": scope,
		})
	}
	input := mustJSON(map[string]any{
		"candidates": items, "core_candidate_ids": coreIDs, "chunk_index": chunkIndex, "chunk_count": chunkCount,
		"pipeline_version": PipelineVersion, "output_contract": "source_segmentation_classification",
	})
	var proposal sourceSegmentationClassificationProposal
	err := runStructuredStage(ctx, client, opts.OutDir, 1, llm.Request{
		Stage: "source_segmentation", Model: opts.Model, Instructions: sourceSegmentationInstructions,
		Input: input, SchemaName: "DBDSLSourceSegmentation", Schema: sourceSegmentationSchema(),
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": resolvedPromptVersion(opts.PromptVersion), "run_key": fmt.Sprintf("chunk_%03d", chunkIndex),
			"chunk_index": fmt.Sprint(chunkIndex), "chunk_count": fmt.Sprint(chunkCount),
			"full_context_bytes": fmt.Sprint(len(input)), "context_policy": "overlapping_source_candidates_v1",
			"canonicalizer_version": "pipeline_ids_v2", "call_gate_policy": "semantic_need_v1",
			"budget_policy": "adaptive_v1", "stage_max_output_tokens": fmt.Sprint(opts.MaxOutputTokens),
		},
	}, &proposal, func() []string {
		return validateSegmentationClassifications(candidates, chunk, proposal)
	})
	return proposal, err
}

func validateSegmentationClassifications(candidates []SourceSegmentationCandidate, chunk segmentationChunk, proposal sourceSegmentationClassificationProposal) []string {
	known := map[string]int{}
	core := map[string]bool{}
	for index := chunk.ContextStart; index < chunk.ContextEnd; index++ {
		known[candidates[index].ID] = index
		if index >= chunk.CoreStart && index < chunk.CoreEnd {
			core[candidates[index].ID] = true
		}
	}
	seen := map[string]bool{}
	var issues []string
	for _, item := range proposal.Classifications {
		index, ok := known[item.CandidateID]
		if !ok || !core[item.CandidateID] {
			issues = append(issues, "classification must reference one core candidate: "+item.CandidateID)
		}
		if seen[item.CandidateID] {
			issues = append(issues, "duplicate candidate classification: "+item.CandidateID)
		}
		seen[item.CandidateID] = true
		if item.Boundary != "start" && item.Boundary != "continue" && item.Boundary != "resume" {
			issues = append(issues, "invalid boundary for "+item.CandidateID)
		}
		if item.Boundary == "resume" {
			target, targetOK := known[item.JoinToCandidateID]
			if !targetOK || target >= index {
				issues = append(issues, "resume target must be an earlier context candidate for "+item.CandidateID)
			}
		}
	}
	for id := range core {
		if !seen[id] {
			issues = append(issues, "missing candidate classification: "+id)
		}
	}
	sort.Strings(issues)
	return issues
}

func buildLLMSegmentation(candidates []SourceSegmentationCandidate, classified sourceSegmentationClassificationProposal) (SourceSegmentationProposal, CombinedDocumentProposal, SourceSegmentationQA) {
	byID := map[string]sourceSegmentationClassification{}
	for _, item := range classified.Classifications {
		byID[item.CandidateID] = item
	}
	groups := []SourceSegmentationGroup{}
	groupByCandidate := map[string]int{}
	lastGroupByResource := map[string]int{}
	for _, candidate := range candidates {
		item := byID[candidate.ID]
		groupIndex := -1
		switch item.Boundary {
		case "continue":
			if previous, ok := lastGroupByResource[candidate.ResourceID]; ok {
				groupIndex = previous
			}
		case "resume":
			if index, ok := groupByCandidate[item.JoinToCandidateID]; ok {
				groupIndex = index
			}
		}
		if groupIndex < 0 || groupIndex >= len(groups) {
			groupIndex = len(groups)
			groups = append(groups, SourceSegmentationGroup{
				ID: fmt.Sprintf("SG-%04d", groupIndex+1), Role: item.Role, Confidence: nonEmpty(item.Confidence, "medium"),
				RequiresReview: item.RequiresReview, Warnings: append([]string(nil), item.Warnings...),
			})
		}
		if groups[groupIndex].Role != item.Role {
			groups[groupIndex].RequiresReview = true
			groups[groupIndex].Warnings = append(groups[groupIndex].Warnings, "Joined candidates have inconsistent roles.")
		}
		groups[groupIndex].CandidateIDs = append(groups[groupIndex].CandidateIDs, candidate.ID)
		groups[groupIndex].Confidence = lowerConfidence(groups[groupIndex].Confidence, item.Confidence)
		groups[groupIndex].RequiresReview = groups[groupIndex].RequiresReview || item.RequiresReview
		groups[groupIndex].Warnings = append(groups[groupIndex].Warnings, item.Warnings...)
		groupByCandidate[candidate.ID] = groupIndex
		if item.Role != "layout_noise" {
			lastGroupByResource[candidate.ResourceID] = groupIndex
		}
	}
	proposal := SourceSegmentationProposal{
		Candidates: candidates, Groups: groups, Strategy: "llm_assisted_backend_reconstruction", LLMAssisted: true,
		Warnings: append([]string(nil), classified.Warnings...), ConfidenceSummary: classified.ConfidenceSummary,
	}
	document := combinedDocumentFromSegmentation(&proposal)
	qa := ValidateSourceSegmentationProposal(proposal)
	return proposal, document, qa
}

func combinedDocumentFromSegmentation(proposal *SourceSegmentationProposal) CombinedDocumentProposal {
	candidateByID := map[string]SourceSegmentationCandidate{}
	for _, candidate := range proposal.Candidates {
		candidateByID[candidate.ID] = candidate
	}
	sentences := []CombinedDocumentSentence{}
	for index := range proposal.Groups {
		group := &proposal.Groups[index]
		if group.Role == "layout_noise" {
			continue
		}
		parts := make([]string, 0, len(group.CandidateIDs))
		origins := make([]CombinedDocumentOrigin, 0, len(group.CandidateIDs))
		for _, id := range group.CandidateIDs {
			candidate := candidateByID[id]
			parts = append(parts, candidate.ExactText)
			origins = append(origins, CombinedDocumentOrigin{
				ResourceID: candidate.ResourceID, SourceSegmentID: candidate.SegmentID,
				LineStart: candidate.LineStart, LineEnd: candidate.LineEnd, StartByte: candidate.StartByte,
				EndByte: candidate.EndByte, ExactText: candidate.ExactText,
			})
		}
		kind := CombinedDocumentUnitSentence
		if group.Role == "heading" || group.Role == "structured_example" || group.Role == "external_reference" || group.Role == "other_structural" {
			kind = CombinedDocumentUnitStructural
		}
		transformation := "llm_grouped"
		if len(group.CandidateIDs) == 1 {
			transformation = "copied"
		} else if proposal.FallbackUsed {
			transformation = "merged"
		}
		odID := fmt.Sprintf("OD-S-%04d", len(sentences)+1)
		group.ODSentenceID = odID
		sentences = append(sentences, CombinedDocumentSentence{
			ID: odID, Kind: kind, Role: group.Role, Text: strings.Join(parts, " "), DerivedFrom: origins,
			Transformation: transformation, SegmentationStrategy: proposal.Strategy,
			Confidence: nonEmpty(group.Confidence, "medium"), Warnings: uniqueStrings(group.Warnings),
		})
	}
	return CombinedDocumentProposal{
		Sentences: sentences, Warnings: uniqueStrings(proposal.Warnings),
		ConfidenceSummary: map[string]string{"overall": "LLM-assisted grouping with backend-owned exact spans", "strategy": proposal.Strategy},
	}
}

func ValidateSourceSegmentationProposal(proposal SourceSegmentationProposal) SourceSegmentationQA {
	qa := SourceSegmentationQA{
		Strategy: proposal.Strategy, CandidatesTotal: len(proposal.Candidates), NeedsAttention: []string{},
		Errors: []string{}, Warnings: append([]string(nil), proposal.Warnings...), RoleCounts: map[string]int{},
		FallbackUsed: proposal.FallbackUsed, FallbackReason: proposal.FallbackReason,
	}
	known := map[string]SourceSegmentationCandidate{}
	for _, candidate := range proposal.Candidates {
		known[candidate.ID] = candidate
	}
	seen := map[string]bool{}
	for _, group := range proposal.Groups {
		if len(group.CandidateIDs) == 0 {
			qa.Errors = append(qa.Errors, group.ID+" has no candidates")
			continue
		}
		qa.RoleCounts[group.Role]++
		switch group.Role {
		case "layout_noise":
			qa.LayoutGroups++
		case "heading", "structured_example", "external_reference", "other_structural":
			qa.StructuralGroups++
		case "sentence", "list_item", "footnote":
			qa.SemanticGroups++
		default:
			qa.Errors = append(qa.Errors, group.ID+" has invalid role "+group.Role)
		}
		resourceID := ""
		for _, id := range group.CandidateIDs {
			candidate, ok := known[id]
			if !ok {
				qa.Errors = append(qa.Errors, group.ID+" references unknown candidate "+id)
				continue
			}
			if seen[id] {
				qa.Errors = append(qa.Errors, "candidate assigned more than once: "+id)
			}
			seen[id] = true
			if resourceID == "" {
				resourceID = candidate.ResourceID
			} else if resourceID != candidate.ResourceID {
				qa.Errors = append(qa.Errors, group.ID+" crosses resource boundaries")
			}
		}
		if group.RequiresReview || group.Confidence == "low" {
			qa.NeedsAttention = append(qa.NeedsAttention, group.ID)
		}
	}
	qa.CandidatesAssigned = len(seen)
	for id := range known {
		if !seen[id] {
			qa.Errors = append(qa.Errors, "candidate is not assigned: "+id)
		}
	}
	sort.Strings(qa.Errors)
	sort.Strings(qa.NeedsAttention)
	qa.OK = len(qa.Errors) == 0 && qa.CandidatesAssigned == qa.CandidatesTotal
	return qa
}

// BuildSourceFidelityReportFromSegmentation accounts for semantic, structural
// and layout dispositions without treating layout noise as lost source text.
func BuildSourceFidelityReportFromSegmentation(segments []SourceSegment, proposal SourceSegmentationProposal) SourceFidelityReport {
	candidateByID := map[string]SourceSegmentationCandidate{}
	for _, candidate := range proposal.Candidates {
		candidateByID[candidate.ID] = candidate
	}
	type segmentState struct {
		odIDs    []string
		roles    map[string]bool
		assigned int
	}
	states := map[string]*segmentState{}
	for _, segment := range segments {
		states[segment.ID] = &segmentState{roles: map[string]bool{}}
	}
	for _, group := range proposal.Groups {
		for _, candidateID := range group.CandidateIDs {
			candidate, ok := candidateByID[candidateID]
			if !ok {
				continue
			}
			state := states[candidate.SegmentID]
			if state == nil {
				continue
			}
			state.assigned++
			state.roles[group.Role] = true
			if group.ODSentenceID != "" {
				state.odIDs = appendUniqueString(state.odIDs, group.ODSentenceID)
			}
		}
	}
	report := SourceFidelityReport{
		PipelineVersion: PipelineVersion, SegmentsTotal: len(segments), DispositionCounts: map[string]int{},
		UncoveredSegmentIDs: []string{}, NeedsAttention: []string{}, Dispositions: []SourceSegmentDisposition{},
		Errors: []string{}, Warnings: append([]string(nil), proposal.Warnings...),
	}
	for _, segment := range segments {
		if segment.Authority == "normative" {
			report.NormativeSegments++
		}
		state := states[segment.ID]
		status := "uncovered"
		reason := "no segmentation group references this source segment"
		if state != nil && state.assigned > 0 {
			if len(state.odIDs) > 0 {
				status = "semantic"
				reason = "retained in one or more combined-document units"
			} else {
				status = "layout"
				reason = "retained as layout noise outside the semantic combined document"
			}
			if proposal.FallbackUsed {
				status = "fallback"
				reason = "retained by deterministic segmentation fallback"
			}
			if segment.Authority == "normative" {
				report.NormativeCovered++
			}
		} else {
			report.UncoveredSegmentIDs = append(report.UncoveredSegmentIDs, segment.ID)
		}
		odIDs := []string{}
		if state != nil {
			odIDs = append(odIDs, state.odIDs...)
		}
		report.DispositionCounts[status]++
		report.Dispositions = append(report.Dispositions, SourceSegmentDisposition{
			SegmentID: segment.ID, Status: status, ODIDs: odIDs, Reason: reason,
		})
	}
	report.NormativeCoverage = 1
	if report.NormativeSegments > 0 {
		report.NormativeCoverage = float64(report.NormativeCovered) / float64(report.NormativeSegments)
	}
	if len(report.UncoveredSegmentIDs) > 0 {
		report.Errors = append(report.Errors, "one or more source segments are not assigned by source segmentation")
	}
	report.OK = len(segments) > 0 && len(report.Errors) == 0 && report.NormativeCovered == report.NormativeSegments
	return report
}

func sourceSegmentationFallback(candidates []SourceSegmentationCandidate, fallback CombinedDocumentProposal, reason string) (SourceSegmentationProposal, CombinedDocumentProposal, SourceSegmentationQA) {
	groups := make([]SourceSegmentationGroup, 0, len(fallback.Sentences))
	assigned := map[string]bool{}
	for _, sentence := range fallback.Sentences {
		group := SourceSegmentationGroup{
			ID: fmt.Sprintf("SG-%04d", len(groups)+1), Role: fallbackRole(sentence), Confidence: sentence.Confidence,
			Warnings: append([]string(nil), sentence.Warnings...), ODSentenceID: sentence.ID,
		}
		for _, candidate := range candidates {
			if assigned[candidate.ID] || !candidateWithinOrigins(candidate, sentence.DerivedFrom) {
				continue
			}
			if containsNormalized(sentence.Text, candidate.ExactText) {
				group.CandidateIDs = append(group.CandidateIDs, candidate.ID)
				assigned[candidate.ID] = true
			}
		}
		if len(group.CandidateIDs) > 0 {
			groups = append(groups, group)
		}
	}
	for _, candidate := range candidates {
		if assigned[candidate.ID] {
			continue
		}
		groups = append(groups, SourceSegmentationGroup{
			ID: fmt.Sprintf("SG-%04d", len(groups)+1), Role: "other_structural", CandidateIDs: []string{candidate.ID},
			Confidence: "low", RequiresReview: true, Warnings: []string{"Candidate was retained separately by deterministic fallback."},
		})
	}
	warning := "LLM-assisted source segmentation was unavailable; deterministic fallback was used."
	if strings.TrimSpace(reason) != "" {
		warning += " " + strings.TrimSpace(reason)
	}
	proposal := SourceSegmentationProposal{
		Candidates: candidates, Groups: groups, Strategy: "deterministic_fallback", LLMAssisted: false,
		FallbackUsed: true, FallbackReason: reason, Warnings: []string{warning},
		ConfidenceSummary: map[string]string{"overall": "deterministic fallback", "strategy": "deterministic_fallback"},
	}
	reconstructed := combinedDocumentFromSegmentation(&proposal)
	reconstructed.Warnings = uniqueStrings(append(append([]string(nil), fallback.Warnings...), warning))
	reconstructed.ConfidenceSummary = proposal.ConfidenceSummary
	qa := ValidateSourceSegmentationProposal(proposal)
	qa.Warnings = uniqueStrings(append(qa.Warnings, warning))
	return proposal, reconstructed, qa
}

func candidateWithinOrigins(candidate SourceSegmentationCandidate, origins []CombinedDocumentOrigin) bool {
	for _, origin := range origins {
		if origin.ResourceID == candidate.ResourceID && candidate.LineStart >= origin.LineStart && candidate.LineEnd <= origin.LineEnd {
			return true
		}
	}
	return false
}

func fallbackRole(sentence CombinedDocumentSentence) string {
	if sentence.Role != "" {
		return sentence.Role
	}
	if CombinedDocumentUnitKind(sentence) == CombinedDocumentUnitStructural {
		return "other_structural"
	}
	return "sentence"
}

func lowerConfidence(left, right string) string {
	rank := map[string]int{"high": 3, "medium": 2, "low": 1}
	if rank[right] == 0 {
		return left
	}
	if rank[left] == 0 || rank[right] < rank[left] {
		return right
	}
	return left
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
