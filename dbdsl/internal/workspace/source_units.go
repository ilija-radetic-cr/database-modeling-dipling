package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llmpipeline"

	"gopkg.in/yaml.v3"
)

type SourceUnitArtifacts struct {
	Proposal llmpipeline.SourceUnitExtractionProposal `json:"proposal"`
	Accepted dsl.SourceUnitsFile                      `json:"accepted"`
	QA       llmpipeline.SourceUnitQA                 `json:"qa"`
}

type ReviewSourceUnitOptions struct {
	BaseRevision   int
	Decision       string
	NormalizedText string
	Note           string
	ReviewedBy     string
}

type sourceUnitArtifactPaths struct {
	Proposal string
	Accepted string
	QA       string
}

// deriveSourceUnitArtifacts projects the canonical source segments into the
// DB-DSL v0.5 source-unit compatibility contract. It is intentionally
// deterministic and is used by source processing itself; it is not a separate
// analysis stage.
func deriveSourceUnitArtifacts(project *ProjectState, document llmpipeline.CombinedDocumentProposal) (SourceUnitArtifacts, error) {
	proposal, qa := llmpipeline.BuildSourceUnitsFromSegments(document)
	if !qa.OK {
		return SourceUnitArtifacts{}, fmt.Errorf("source-unit QA failed: %s", strings.Join(qa.Errors, "; "))
	}
	qa.DerivationStrategy = llmpipeline.SegmentUnitStrategy
	return SourceUnitArtifacts{
		Proposal: proposal,
		Accepted: buildAcceptedSourceUnits(project, proposal),
		QA:       qa,
	}, nil
}

// writeSourceUnitArtifacts writes the compatibility files into the same
// revision as the source-segmentation result that produced them.
func (s *Store) writeSourceUnitArtifacts(projectID string, revision int, artifacts SourceUnitArtifacts) (sourceUnitArtifactPaths, error) {
	revisionRel := s.projectRevisionRel(projectID, revision)
	revisionDir := s.absoluteWorkspacePath(revisionRel)
	if err := os.MkdirAll(revisionDir, 0o755); err != nil {
		return sourceUnitArtifactPaths{}, fmt.Errorf("create source-unit revision directory: %w", err)
	}
	paths := sourceUnitArtifactPaths{
		Proposal: filepath.ToSlash(filepath.Join(revisionRel, "source_units.proposed.json")),
		Accepted: filepath.ToSlash(filepath.Join(revisionRel, "source_units.yaml")),
		QA:       filepath.ToSlash(filepath.Join(revisionRel, "source_unit_qa.json")),
	}
	proposalBytes, err := json.MarshalIndent(artifacts.Proposal, "", "  ")
	if err != nil {
		return sourceUnitArtifactPaths{}, err
	}
	acceptedBytes, err := yaml.Marshal(artifacts.Accepted)
	if err != nil {
		return sourceUnitArtifactPaths{}, err
	}
	qaBytes, err := json.MarshalIndent(artifacts.QA, "", "  ")
	if err != nil {
		return sourceUnitArtifactPaths{}, err
	}
	for path, data := range map[string][]byte{
		paths.Proposal: proposalBytes,
		paths.Accepted: acceptedBytes,
		paths.QA:       qaBytes,
	} {
		if err := writeAtomic(s.absoluteWorkspacePath(path), data); err != nil {
			return sourceUnitArtifactPaths{}, err
		}
	}
	return paths, nil
}

func buildAcceptedSourceUnits(project *ProjectState, proposal llmpipeline.SourceUnitExtractionProposal) dsl.SourceUnitsFile {
	units := make([]dsl.SourceUnit, 0, len(proposal.SourceUnits))
	for _, unit := range proposal.SourceUnits {
		units = append(units, dsl.SourceUnit{
			ID:        unit.ID,
			Kind:      unit.Kind,
			Section:   unit.Section,
			Location:  "combined_document.md#" + strings.Join(proposalSegmentIDs(unit), ","),
			Relevance: unit.Relevance,
			Tags:      append([]string(nil), unit.Tags...),
			Text: dsl.SourceUnitText{
				Exact:         unit.ExactText,
				Normalized:    unit.NormalizedText,
				Normalization: unit.Normalization,
			},
		})
	}
	return dsl.SourceUnitsFile{
		Document: dsl.SourceUnitsDocument{
			ID:              project.ID + "_source_units",
			Title:           project.Name + " source units",
			PipelineVersion: llmpipeline.PipelineVersion,
			SourceFile:      "combined_document.md",
			SourceLanguage:  project.Language,
			Granularity:     "semantic_unit_with_od_lineage",
		},
		SourceUnits: units,
	}
}

func (s *Store) SourceUnitArtifacts(projectID string) (SourceUnitArtifacts, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return SourceUnitArtifacts{}, ErrNotFound
	}
	if project.SourceUnitsPath == "" || project.SourceUnitsProposalPath == "" || project.SourceUnitQAPath == "" {
		return SourceUnitArtifacts{}, ErrNotFound
	}
	var out SourceUnitArtifacts
	if err := readJSON(s.absoluteWorkspacePath(project.SourceUnitsProposalPath), &out.Proposal); err != nil {
		return SourceUnitArtifacts{}, err
	}
	if err := readYAML(s.absoluteWorkspacePath(project.SourceUnitsPath), &out.Accepted); err != nil {
		return SourceUnitArtifacts{}, err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.SourceUnitQAPath), &out.QA); err != nil {
		return SourceUnitArtifacts{}, err
	}
	// Segment-based units keep the segment text verbatim and are returned as
	// stored. Old artifacts may contain LLM-authored normalization; for them the
	// exact text is authoritative and every read goes through the backend normalizer.
	if out.QA.DerivationStrategy == llmpipeline.SegmentUnitStrategy {
		return out, nil
	}
	for i := range out.Accepted.SourceUnits {
		normalized, audit := llmpipeline.NormalizeSourceText(out.Accepted.SourceUnits[i].Text.Exact)
		out.Accepted.SourceUnits[i].Text.Normalized = normalized
		out.Accepted.SourceUnits[i].Text.Normalization = audit
	}
	for i := range out.Proposal.SourceUnits {
		normalized, audit := llmpipeline.NormalizeSourceText(out.Proposal.SourceUnits[i].ExactText)
		out.Proposal.SourceUnits[i].NormalizedText = normalized
		out.Proposal.SourceUnits[i].Normalization = audit
	}
	return out, nil
}

// ReviewSourceUnit resolves one manual source-unit QA gate and persists the
// accepted source units, extraction proposal, QA state, and audit record in a
// new project revision.
func (s *Store) ReviewSourceUnit(projectID, sourceUnitID string, opts ReviewSourceUnitOptions) (int, int, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, 0, ErrNotFound
	}
	if opts.BaseRevision > 0 && opts.BaseRevision != project.CurrentRevision {
		return 0, 0, ErrRevisionConflict
	}
	decision := strings.ToLower(strings.TrimSpace(opts.Decision))
	if decision != "accept" && decision != "revise" && decision != "exclude" {
		return 0, 0, errors.New("decision must be accept, revise, or exclude")
	}
	artifacts, err := s.SourceUnitArtifacts(projectID)
	if err != nil {
		return 0, 0, err
	}
	if !containsString(artifacts.QA.NeedsAttention, sourceUnitID) {
		if _, found := findAcceptedSourceUnit(artifacts.Accepted.SourceUnits, sourceUnitID); !found {
			return 0, 0, ErrNotFound
		}
		return 0, len(artifacts.QA.NeedsAttention), errors.New("source unit does not need attention")
	}
	acceptedIndex, found := findAcceptedSourceUnit(artifacts.Accepted.SourceUnits, sourceUnitID)
	if !found {
		return 0, 0, ErrNotFound
	}
	proposalIndex, found := findProposedSourceUnit(artifacts.Proposal.SourceUnits, sourceUnitID)
	if !found {
		return 0, 0, errors.New("source-unit proposal is missing the selected unit")
	}

	accepted := &artifacts.Accepted.SourceUnits[acceptedIndex]
	proposal := &artifacts.Proposal.SourceUnits[proposalIndex]
	previousText := accepted.Text.Normalized
	previousRelevance := accepted.Relevance
	deterministicText, normalization := llmpipeline.NormalizeSourceText(accepted.Text.Exact)
	switch decision {
	case "revise":
		normalized := strings.TrimSpace(opts.NormalizedText)
		// The backend owns normalized text. Older clients may still echo the
		// expected value; validate that echo, but never require or adopt it.
		if normalized != "" {
			if _, err := llmpipeline.ValidateSourceTextNormalization(accepted.Text.Exact, normalized); err != nil {
				return 0, 0, err
			}
		}
	case "exclude":
		accepted.Relevance = "non_model"
		proposal.Relevance = "non_model"
	}
	accepted.Text.Normalized = deterministicText
	accepted.Text.Normalization = normalization
	proposal.NormalizedText = deterministicText
	proposal.Normalization = normalization
	proposal.RequiresReview = false
	artifacts.QA.NeedsAttention = withoutString(artifacts.QA.NeedsAttention, sourceUnitID)
	nextRevision := project.CurrentRevision + 1
	artifacts.QA.ReviewDecisions = append(artifacts.QA.ReviewDecisions, llmpipeline.SourceUnitReviewDecision{
		SourceUnitID: sourceUnitID, Decision: decision,
		PreviousNormalizedText: previousText, NormalizedText: accepted.Text.Normalized,
		Normalization:     normalization,
		PreviousRelevance: previousRelevance, Relevance: accepted.Relevance,
		Note: strings.TrimSpace(opts.Note), ReviewedBy: nonEmpty(strings.TrimSpace(opts.ReviewedBy), "local_user"),
		ReviewedAt: time.Now().UTC().Format(time.RFC3339Nano), ProjectRevision: nextRevision,
	})
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"source_units.proposed.json": {Value: artifacts.Proposal, JSON: true},
		"source_units.yaml":          {Value: artifacts.Accepted},
		"source_unit_qa.json":        {Value: artifacts.QA, JSON: true},
	})
	if err != nil {
		return 0, 0, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		s.invalidateDerivedFromSourceUnits(current)
		current.SourceUnitsProposalPath = paths["source_units.proposed.json"]
		current.SourceUnitsPath = paths["source_units.yaml"]
		current.SourceUnitQAPath = paths["source_unit_qa.json"]
		current.ModelGenerated = false
		current.FinalModelAccepted = false
		current.DBMLReady = false
		current.Completed = false
		remaining := len(artifacts.QA.NeedsAttention)
		if remaining > 0 {
			current.LifecycleStatus = "source_review"
			current.LastActivity = fmt.Sprintf("Source unit %s reviewed; %d still need attention.", sourceUnitID, remaining)
		} else {
			current.LifecycleStatus = "sources_processed"
			current.LastActivity = "All source units reviewed; requirement extraction is ready."
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, len(artifacts.QA.NeedsAttention), nil
}

func findAcceptedSourceUnit(units []dsl.SourceUnit, id string) (int, bool) {
	for i := range units {
		if units[i].ID == id {
			return i, true
		}
	}
	return -1, false
}

func findProposedSourceUnit(units []llmpipeline.SourceUnitProposal, id string) (int, bool) {
	for i := range units {
		if units[i].ID == id {
			return i, true
		}
	}
	return -1, false
}

func withoutString(items []string, value string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}

func (s *Store) projectSourceUnits(projectID string) ([]SourceUnit, bool, error) {
	artifacts, err := s.SourceUnitArtifacts(projectID)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	combined, err := s.CombinedDocument(projectID)
	if err != nil {
		return nil, false, err
	}
	sentenceByID := map[string]llmpipeline.CombinedDocumentSentence{}
	for _, sentence := range combined.Lineage.Sentences {
		sentenceByID[sentence.ID] = sentence
	}
	proposalByID := map[string]llmpipeline.SourceUnitProposal{}
	for _, proposal := range artifacts.Proposal.SourceUnits {
		proposalByID[proposal.ID] = proposal
	}
	needsAttention := map[string]bool{}
	for _, id := range artifacts.QA.NeedsAttention {
		needsAttention[id] = true
	}
	units := make([]SourceUnit, 0, len(artifacts.Accepted.SourceUnits))
	for _, source := range artifacts.Accepted.SourceUnits {
		proposal := proposalByID[source.ID]
		originSet := map[string]OriginSpan{}
		segmentIDs := proposalSegmentIDs(proposal)
		for _, sentenceID := range segmentIDs {
			for _, origin := range sentenceByID[sentenceID].DerivedFrom {
				key := fmt.Sprintf("%s:%s:%d:%d:%d:%d", origin.ResourceID, origin.SourceSegmentID, origin.LineStart, origin.LineEnd, origin.StartByte, origin.EndByte)
				label := fmt.Sprintf("%s lines %d-%d", origin.ResourceID, origin.LineStart, origin.LineEnd)
				if origin.SourceSegmentID != "" {
					label = fmt.Sprintf("%s · %s · bytes %d-%d", label, origin.SourceSegmentID, origin.StartByte, origin.EndByte)
				}
				originSet[key] = OriginSpan{
					ResourceID:  origin.ResourceID,
					Label:       label,
					LineStart:   origin.LineStart,
					LineEnd:     origin.LineEnd,
					StartOffset: origin.StartByte,
					EndOffset:   origin.EndByte,
				}
			}
		}
		origins := make([]OriginSpan, 0, len(originSet))
		for _, origin := range originSet {
			origins = append(origins, origin)
		}
		sort.Slice(origins, func(i, j int) bool { return origins[i].Label < origins[j].Label })
		status := "reviewed"
		if needsAttention[source.ID] {
			status = "needs_attention"
		}
		units = append(units, SourceUnit{
			ID:               source.ID,
			Kind:             source.Kind,
			Section:          source.Section,
			NormalizedText:   source.Text.Normalized,
			Normalization:    source.Text.Normalization,
			ExactText:        source.Text.Exact,
			Relevance:        source.Relevance,
			Confidence:       proposal.Confidence,
			ReviewStatus:     status,
			OriginSpans:      origins,
			SegmentIDs:       append([]string(nil), segmentIDs...),
			Warnings:         append([]string(nil), proposal.Warnings...),
			RequirementNotes: append([]string(nil), proposal.RequirementNotes...),
		})
	}
	return units, true, nil
}

func proposalSegmentIDs(proposal llmpipeline.SourceUnitProposal) []string {
	if len(proposal.SegmentIDs) > 0 {
		return proposal.SegmentIDs
	}
	return proposal.ODSentenceIDs
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func readYAML(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, target)
}
