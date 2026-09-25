package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"dbdsl/internal/jobs"
	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"

	"gopkg.in/yaml.v3"
)

const maxResourceBytes int64 = 32 << 20

type SourceManifest struct {
	Document  SourceManifestDocument `json:"document" yaml:"document"`
	Resources []InputResource        `json:"resources" yaml:"resources"`
	Summary   SourceManifestSummary  `json:"summary" yaml:"summary"`
}

type SourceManifestDocument struct {
	ID              string    `json:"id" yaml:"id"`
	ProjectID       string    `json:"project_id" yaml:"project_id"`
	ProjectName     string    `json:"project_name" yaml:"project_name"`
	PipelineVersion string    `json:"pipeline_version" yaml:"pipeline_version"`
	WorkspacePath   string    `json:"workspace_path" yaml:"workspace_path"`
	GeneratedAt     time.Time `json:"generated_at" yaml:"generated_at"`
}

type SourceManifestSummary struct {
	Total          int `json:"total" yaml:"total"`
	Ready          int `json:"ready" yaml:"ready"`
	NeedsAttention int `json:"needs_attention" yaml:"needs_attention"`
	Failed         int `json:"failed" yaml:"failed"`
	LineCount      int `json:"line_count" yaml:"line_count"`
}

type CombinedDocumentLineage struct {
	Document          CombinedDocumentLineageDocument        `json:"document"`
	Sentences         []llmpipeline.CombinedDocumentSentence `json:"sentences"`
	Warnings          []string                               `json:"warnings"`
	ConfidenceSummary map[string]string                      `json:"confidence_summary"`
}

type CombinedDocumentLineageDocument struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	ProjectName        string    `json:"project_name"`
	SourceManifestFile string    `json:"source_manifest_file"`
	PipelineVersion    string    `json:"pipeline_version"`
	CreatedAt          time.Time `json:"created_at"`
}

type CombinedDocumentSummary struct {
	Status               string `json:"status"`
	UnitCount            int    `json:"unit_count"`
	SentenceCount        int    `json:"sentence_count"`
	StructuralUnitCount  int    `json:"structural_unit_count"`
	ResourceCount        int    `json:"resource_count"`
	WarningCount         int    `json:"warning_count"`
	SegmentationStrategy string `json:"segmentation_strategy"`
	LLMAssisted          bool   `json:"llm_assisted"`
	FallbackUsed         bool   `json:"fallback_used"`
	NeedsAttentionCount  int    `json:"needs_attention_count"`
	LayoutSegmentCount   int    `json:"layout_segment_count"`
}

type CombinedDocumentResponse struct {
	Markdown string                  `json:"markdown"`
	Lineage  CombinedDocumentLineage `json:"lineage"`
	Summary  CombinedDocumentSummary `json:"summary"`
}

type ProcessSourcesOptions struct {
	BaseRevision    int
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	OnProgress      jobs.StepEmitter
}

type extractedResource struct {
	Resource InputResource
	Text     string
	Lines    []string
}

func (s *Store) ensureProjectWorkspace(projectID string) error {
	for _, dir := range []string{
		s.projectWorkspaceDir(projectID),
		filepath.Join(s.projectWorkspaceDir(projectID), "resources", "originals"),
		filepath.Join(s.projectWorkspaceDir(projectID), "resources", "extracted"),
		filepath.Join(s.projectWorkspaceDir(projectID), "llm_runs"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create project workspace: %w", err)
		}
	}
	return nil
}

func (s *Store) projectWorkspaceDir(projectID string) string {
	return filepath.Join(s.root, ".dbdsl_workbench", "projects", projectID)
}

func (s *Store) projectWorkspaceRel(projectID string) string {
	return filepath.ToSlash(filepath.Join(".dbdsl_workbench", "projects", projectID))
}

func (s *Store) sourceManifestRel(projectID string) string {
	return filepath.ToSlash(filepath.Join(s.projectWorkspaceRel(projectID), "source_manifest.yaml"))
}

func (s *Store) combinedDocumentRel(projectID string) string {
	return filepath.ToSlash(filepath.Join(s.projectWorkspaceRel(projectID), "combined_document.md"))
}

func (s *Store) combinedDocumentLineageRel(projectID string) string {
	return filepath.ToSlash(filepath.Join(s.projectWorkspaceRel(projectID), "combined_document_lineage.json"))
}

func (s *Store) sourceSegmentationProposalRel(projectID string) string {
	return filepath.ToSlash(filepath.Join(s.projectWorkspaceRel(projectID), "source_segmentation.proposed.json"))
}

func (s *Store) projectRevisionRel(projectID string, revision int) string {
	return filepath.ToSlash(filepath.Join(s.projectWorkspaceRel(projectID), "revisions", fmt.Sprintf("rev_%06d", revision)))
}

func (s *Store) resourceOriginalRel(projectID, resourceID, fileType, fileName string) string {
	ext := resourceExtension(fileType, fileName)
	return filepath.ToSlash(filepath.Join(s.projectWorkspaceRel(projectID), "resources", "originals", resourceID+ext))
}

func (s *Store) resourceExtractedRel(projectID, resourceID string) string {
	return filepath.ToSlash(filepath.Join(s.projectWorkspaceRel(projectID), "resources", "extracted", resourceID+".txt"))
}

func (s *Store) SourceManifest(projectID string) (SourceManifest, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return SourceManifest{}, ErrNotFound
	}
	return s.buildSourceManifest(project), nil
}

func (s *Store) SourceManifestYAML(projectID string) (string, error) {
	manifest, err := s.SourceManifest(projectID)
	if err != nil {
		return "", err
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Store) Resource(projectID, resourceID string) (InputResource, bool, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return InputResource{}, false, ErrNotFound
	}
	for _, resource := range project.Resources {
		if resource.ID == resourceID {
			return resource, true, nil
		}
	}
	return InputResource{}, false, nil
}

func (s *Store) ResourceText(projectID, resourceID string) (string, InputResource, error) {
	resource, found, err := s.Resource(projectID, resourceID)
	if err != nil {
		return "", InputResource{}, err
	}
	if !found {
		return "", InputResource{}, ErrNotFound
	}
	if resource.ExtractedTextPath == "" {
		return "", resource, errors.New("resource has no extracted text")
	}
	data, err := os.ReadFile(s.absoluteWorkspacePath(resource.ExtractedTextPath))
	if err != nil {
		return "", resource, err
	}
	return string(data), resource, nil
}

func (s *Store) CombinedDocument(projectID string) (CombinedDocumentResponse, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return CombinedDocumentResponse{}, ErrNotFound
	}
	if project.CombinedDocumentPath == "" || project.CombinedDocumentLineagePath == "" {
		return CombinedDocumentResponse{}, ErrNotFound
	}
	markdown, err := os.ReadFile(s.absoluteWorkspacePath(project.CombinedDocumentPath))
	if err != nil {
		return CombinedDocumentResponse{}, err
	}
	lineageBytes, err := os.ReadFile(s.absoluteWorkspacePath(project.CombinedDocumentLineagePath))
	if err != nil {
		return CombinedDocumentResponse{}, err
	}
	var lineage CombinedDocumentLineage
	if err := json.Unmarshal(lineageBytes, &lineage); err != nil {
		return CombinedDocumentResponse{}, err
	}
	warnings := len(lineage.Warnings)
	sentenceCount := 0
	structuralCount := 0
	for _, sentence := range lineage.Sentences {
		warnings += len(sentence.Warnings)
		if llmpipeline.CombinedDocumentUnitKind(sentence) == llmpipeline.CombinedDocumentUnitStructural {
			structuralCount++
		} else {
			sentenceCount++
		}
	}
	segmentation, _ := s.SourceSegmentation(projectID)
	layoutCount := 0
	for _, segment := range segmentation.Segments {
		if segment.Type == "page_header" || segment.Type == "page_footer" || segment.Type == "page_number" {
			layoutCount++
		}
	}
	return CombinedDocumentResponse{
		Markdown: string(markdown),
		Lineage:  lineage,
		Summary: CombinedDocumentSummary{
			Status:               "ready",
			UnitCount:            len(lineage.Sentences),
			SentenceCount:        sentenceCount,
			StructuralUnitCount:  structuralCount,
			ResourceCount:        len(project.Resources),
			WarningCount:         warnings,
			SegmentationStrategy: "llm_complete_resource",
			LLMAssisted:          len(segmentation.Segments) > 0,
			FallbackUsed:         false,
			NeedsAttentionCount:  0,
			LayoutSegmentCount:   layoutCount,
		},
	}, nil
}

func (s *Store) combinedSentenceCount(project *ProjectState) int {
	if project.CombinedDocumentLineagePath == "" {
		return 0
	}
	data, err := os.ReadFile(s.absoluteWorkspacePath(project.CombinedDocumentLineagePath))
	if err != nil {
		return 0
	}
	var lineage CombinedDocumentLineage
	if err := json.Unmarshal(data, &lineage); err != nil {
		return 0
	}
	count := 0
	for _, unit := range lineage.Sentences {
		if llmpipeline.CombinedDocumentUnitKind(unit) == llmpipeline.CombinedDocumentUnitSentence {
			count++
		}
	}
	return count
}

func (s *Store) artifactStatus(resourcePath string) string {
	if strings.TrimSpace(resourcePath) == "" {
		return "not_generated"
	}
	if _, err := os.Stat(s.absoluteWorkspacePath(resourcePath)); err == nil {
		return "ready"
	}
	return "not_generated"
}

// ProcessSources is the offline/test entry point. API callers use
// ProcessSourcesWithLLM with the configured provider client.
func (s *Store) ProcessSources(projectID string, opts ProcessSourcesOptions) (int, []string, error) {
	return s.ProcessSourcesWithLLM(context.Background(), llm.NewDefaultMockClient(), projectID, opts)
}

func (s *Store) ProcessSourcesWithLLM(ctx context.Context, client llm.Client, projectID string, opts ProcessSourcesOptions) (int, []string, error) {
	// Sources may have changed since the last run, so detect the language afresh
	// instead of reusing the frozen profile.
	sourceLanguage := s.detectSourceLanguage(projectID)
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if opts.BaseRevision > 0 && project.CurrentRevision != opts.BaseRevision {
		return 0, nil, ErrRevisionConflict
	}
	emit := opts.OnProgress
	if emit == nil {
		emit = func(string, string, int, map[string]any) {}
	}

	emit("load_extracted_resources", "Loading extracted resource text.", 12, nil)
	resources, err := s.readyResourcesForCombined(project)
	if err != nil {
		return 0, nil, err
	}
	if len(resources) == 0 {
		return 0, nil, errors.New("at least one resource with extracted text is required")
	}

	emit("write_source_manifest", "Preparing refreshed source manifest.", 25, map[string]any{"resource_count": len(resources)})

	if client == nil {
		return 0, nil, errors.New("LLM client is required for source segmentation")
	}
	emit("prepare_source_segmentation", "Preparing complete source resources for segmentation.", 36, map[string]any{"resource_count": len(resources)})
	llmResources := make([]llmpipeline.CombinedDocumentResource, 0, len(resources))
	for _, resource := range resources {
		lines := make([]llmpipeline.CombinedDocumentLine, 0, len(resource.Lines))
		for i, line := range resource.Lines {
			lines = append(lines, llmpipeline.CombinedDocumentLine{Number: i + 1, Text: line})
		}
		llmResources = append(llmResources, llmpipeline.CombinedDocumentResource{
			ID:            resource.Resource.ID,
			Title:         resource.Resource.Title,
			FileType:      resource.Resource.FileType,
			Language:      nonEmpty(resource.Resource.Language, project.Language),
			Authority:     nonEmpty(resource.Resource.Authority, "normative"),
			ContentHash:   resource.Resource.ContentHash,
			ExtractedHash: resource.Resource.ExtractedTextHash,
			LineCount:     len(resource.Lines),
			Lines:         lines,
		})
	}
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "source_segmentation", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	controls := s.resolveLLMExecutionControls(projectID)
	lineCount := 0
	for _, resource := range llmResources {
		lineCount += len(resource.Lines)
	}
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "source_segmentation", opts.MaxOutputTokens, stageBudgetInput{Sentences: lineCount})
	emit("propose_source_segmentation", "Segmenting complete source resources sequentially.", 55, map[string]any{"resource_count": len(llmResources)})
	segmentation, proposal, stageErr := llmpipeline.RunSourceSegmentation(ctx, client, llmpipeline.SourceSegmentationOptions{
		OutDir: s.projectWorkspaceDir(projectID), Resources: llmResources,
		Model: opts.Model, ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		PromptVersion: controls.PromptVersion,
	})
	if stageErr != nil {
		return 0, nil, stageErr
	}
	lineage := CombinedDocumentLineage{
		Document: CombinedDocumentLineageDocument{
			ID:                 project.ID + "_combined_document_lineage",
			ProjectID:          project.ID,
			ProjectName:        project.Name,
			SourceManifestFile: "source_manifest.yaml",
			PipelineVersion:    llmpipeline.PipelineVersion,
			CreatedAt:          time.Now(),
		},
		Sentences:         proposal.Sentences,
		Warnings:          stringSlice(proposal.Warnings),
		ConfidenceSummary: proposal.ConfidenceSummary,
	}
	document := llmpipeline.CombinedDocumentProposal{
		Sentences:         proposal.Sentences,
		Warnings:          proposal.Warnings,
		ConfidenceSummary: proposal.ConfidenceSummary,
	}
	emit("assign_source_unit_ids", "Canonical SU evidence IDs assigned to the LLM evidence units.", 78, map[string]any{"segment_count": len(proposal.Sentences)})
	sourceArtifacts, err := deriveSourceUnitArtifacts(project, document)
	if err != nil {
		return 0, nil, err
	}
	emit("validate_source_units", "Validating source evidence coverage and origin chains.", 84, map[string]any{
		"source_unit_count": len(sourceArtifacts.Accepted.SourceUnits),
		"needs_attention":   len(sourceArtifacts.QA.NeedsAttention),
		"strategy":          llmpipeline.SegmentUnitStrategy,
	})
	emit("write_combined_document", "Writing segmented source and evidence artifacts in one revision.", 90, map[string]any{
		"unit_count":        len(proposal.Sentences),
		"source_unit_count": len(sourceArtifacts.Accepted.SourceUnits),
	})

	var sourcePaths sourceUnitArtifactPaths
	err = s.withProject(projectID, opts.BaseRevision, func(project *ProjectState) error {
		// Recheck the revision under the store lock before replacing canonical
		// artifacts, so a stale background job cannot overwrite newer intake.
		var err error
		sourcePaths, err = s.writeSourceUnitArtifacts(projectID, project.CurrentRevision+1, sourceArtifacts)
		if err != nil {
			return err
		}
		if err := s.writeSourceManifestLocked(project); err != nil {
			return err
		}
		if err := s.writeCombinedDocumentArtifacts(project, lineage); err != nil {
			return err
		}
		if err := writeJSONArtifact(s.absoluteWorkspacePath(s.sourceSegmentationProposalRel(project.ID)), segmentation); err != nil {
			return err
		}
		profile := defaultLLMExecutionProfile(opts.Model)
		if opts.ReasoningEffort != "" {
			profile.ReasoningEffort = opts.ReasoningEffort
		}
		if opts.MaxOutputTokens > 0 {
			profile.MaxOutputTokens = opts.MaxOutputTokens
		}
		profile.OutputLanguage = sourceLanguage
		project.LLMExecutionProfile = &profile
		s.invalidateDerivedFromCombinedDocument(project)
		project.SourceManifestPath = s.sourceManifestRel(project.ID)
		project.CombinedDocumentPath = s.combinedDocumentRel(project.ID)
		project.CombinedDocumentLineagePath = s.combinedDocumentLineageRel(project.ID)
		project.SourceSegmentationProposalPath = s.sourceSegmentationProposalRel(project.ID)
		project.SourceUnitsProposalPath = sourcePaths.Proposal
		project.SourceUnitsPath = sourcePaths.Accepted
		project.SourceUnitQAPath = sourcePaths.QA
		project.CombinedDocumentReady = true
		project.ModelGenerated = false
		project.DBMLReady = false
		project.Completed = false
		project.ModelPath = ""
		project.TaskPath = ""
		project.BundlePath = ""
		if len(sourceArtifacts.QA.NeedsAttention) > 0 {
			project.LifecycleStatus = "source_review"
		} else {
			project.LifecycleStatus = "sources_processed"
		}
		project.LastActivity = "Source resources were segmented and assigned canonical SU evidence IDs in one revision."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	updated := []string{"resources", "source_manifest", "source_segmentation", "combined_document", "source_units", "source_unit_qa"}
	project, _ = s.Project(projectID)
	return project.CurrentRevision, updated, nil
}

func (s *Store) buildSourceManifest(project *ProjectState) SourceManifest {
	resources := append([]InputResource(nil), project.Resources...)
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].ID < resources[j].ID
	})
	summary := SourceManifestSummary{Total: len(resources)}
	for _, resource := range resources {
		summary.LineCount += resource.LineCount
		switch resource.ExtractionStatus {
		case "ready":
			summary.Ready++
		case "needs_attention":
			summary.NeedsAttention++
		case "failed":
			summary.Failed++
		}
	}
	return SourceManifest{
		Document: SourceManifestDocument{
			ID:              project.ID + "_source_manifest",
			ProjectID:       project.ID,
			ProjectName:     project.Name,
			PipelineVersion: llmpipeline.PipelineVersion,
			WorkspacePath:   s.projectWorkspaceRel(project.ID),
			GeneratedAt:     time.Now(),
		},
		Resources: resources,
		Summary:   summary,
	}
}

func (s *Store) writeSourceManifestLocked(project *ProjectState) error {
	if err := s.ensureProjectWorkspace(project.ID); err != nil {
		return err
	}
	project.SourceManifestPath = s.sourceManifestRel(project.ID)
	manifest := s.buildSourceManifest(project)
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}
	return writeAtomic(s.absoluteWorkspacePath(project.SourceManifestPath), data)
}

func (s *Store) invalidateDerivedFromResources(project *ProjectState) {
	_ = removeIfPresent(s.absoluteWorkspacePath(project.SourceSegmentationProposalPath))
	_ = removeIfPresent(s.absoluteWorkspacePath(project.CombinedDocumentPath))
	_ = removeIfPresent(s.absoluteWorkspacePath(project.CombinedDocumentLineagePath))
	project.CombinedDocumentPath = ""
	project.CombinedDocumentLineagePath = ""
	project.SourceSegmentationProposalPath = ""
	project.CombinedDocumentReady = false
	project.ModelGenerated = false
	project.DBMLReady = false
	project.Completed = false
	project.ModelPath = ""
	project.TaskPath = ""
	project.BundlePath = ""
	project.LifecycleStatus = "intake"
	s.invalidateDerivedFromCombinedDocument(project)
}

func writeJSONArtifact(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data)
}

func (s *Store) SourceSegmentation(projectID string) (llmpipeline.SourceSegmentationProposal, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return llmpipeline.SourceSegmentationProposal{}, ErrNotFound
	}
	if project.SourceSegmentationProposalPath == "" {
		return llmpipeline.SourceSegmentationProposal{}, ErrNotFound
	}
	var proposal llmpipeline.SourceSegmentationProposal
	if err := readJSON(s.absoluteWorkspacePath(project.SourceSegmentationProposalPath), &proposal); err != nil {
		return proposal, err
	}
	return proposal, nil
}

func (s *Store) invalidateDerivedFromCombinedDocument(project *ProjectState) {
	project.SourceUnitsProposalPath = ""
	project.SourceUnitsPath = ""
	project.SourceUnitQAPath = ""
	s.invalidateDerivedFromSourceUnits(project)
}

func (s *Store) invalidateDerivedFromSourceUnits(project *ProjectState) {
	invalidateModelAndReview(project)
}

func (s *Store) readyResourcesForCombined(project *ProjectState) ([]extractedResource, error) {
	out := make([]extractedResource, 0, len(project.Resources))
	for _, resource := range project.Resources {
		if resource.ExtractedTextPath == "" {
			continue
		}
		if resource.ExtractionStatus != "ready" && resource.ExtractionStatus != "needs_attention" {
			continue
		}
		data, err := os.ReadFile(s.absoluteWorkspacePath(resource.ExtractedTextPath))
		if err != nil {
			return nil, fmt.Errorf("read extracted text for %s: %w", resource.ID, err)
		}
		text := normalizeExtractedText(data)
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, extractedResource{
			Resource: resource,
			Text:     text,
			Lines:    splitResourceLines(text),
		})
	}
	return out, nil
}

func (s *Store) writeCombinedDocumentArtifacts(project *ProjectState, lineage CombinedDocumentLineage) error {
	markdown := renderCombinedDocumentMarkdown(project.Name, lineage.Sentences)
	if err := writeAtomic(s.absoluteWorkspacePath(s.combinedDocumentRel(project.ID)), []byte(markdown)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lineage, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(s.absoluteWorkspacePath(s.combinedDocumentLineageRel(project.ID)), data)
}

func renderCombinedDocumentMarkdown(projectName string, sentences []llmpipeline.CombinedDocumentSentence) string {
	var b strings.Builder
	b.WriteString("# Combined Document\n\n")
	if strings.TrimSpace(projectName) != "" {
		b.WriteString("Project: ")
		b.WriteString(projectName)
		b.WriteString("\n\n")
	}
	for _, sentence := range sentences {
		b.WriteString("[")
		b.WriteString(sentence.ID)
		b.WriteString("] ")
		b.WriteString(strings.TrimSpace(sentence.Text))
		b.WriteString("\n")
	}
	return b.String()
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func buildResourceFromBytes(projectID, resourceID, kind, title, fileName, fileType, language, authority string, data []byte, s *Store, now time.Time) (InputResource, error) {
	fileType = nonEmpty(fileType, detectFileType(fileName))
	if fileType == "unknown" && kind == "pasted_text" {
		fileType = "text"
	}
	if err := s.ensureProjectWorkspace(projectID); err != nil {
		return InputResource{}, err
	}
	contentRel := s.resourceOriginalRel(projectID, resourceID, fileType, fileName)
	contentPath := s.absoluteWorkspacePath(contentRel)
	if err := writeAtomic(contentPath, data); err != nil {
		return InputResource{}, err
	}
	extractedText, status, confidence, warnings := extractText(contentPath, fileType, data)
	extractedText = ensureTrailingNewline(extractedText)
	extractedRel := s.resourceExtractedRel(projectID, resourceID)
	if err := writeAtomic(s.absoluteWorkspacePath(extractedRel), []byte(extractedText)); err != nil {
		return InputResource{}, err
	}
	if strings.TrimSpace(extractedText) == "" && status == "ready" {
		status = "needs_attention"
		confidence = "low"
		warnings = append(warnings, "extracted text is empty")
	}
	return InputResource{
		ID:                   resourceID,
		Kind:                 kind,
		FileType:             fileType,
		Title:                nonEmpty(title, nonEmpty(fileName, "Input resource")),
		FileName:             fileName,
		SizeBytes:            int64(len(data)),
		ContentPath:          contentRel,
		ExtractedTextPath:    extractedRel,
		ContentHash:          sha256Hash(data),
		ExtractedTextHash:    sha256Hash([]byte(extractedText)),
		Language:             nonEmpty(language, "unknown"),
		Authority:            nonEmpty(authority, "normative"),
		LineCount:            len(splitResourceLines(extractedText)),
		Warnings:             warnings,
		ExtractionStatus:     status,
		ExtractionConfidence: confidence,
		CreatedAt:            now,
		UpdatedAt:            now,
	}, nil
}

func readLimitedResource(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	limited := io.LimitReader(r, maxResourceBytes+1)
	if _, err := io.Copy(&buf, limited); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > maxResourceBytes {
		return nil, fmt.Errorf("resource is larger than %d bytes", maxResourceBytes)
	}
	return buf.Bytes(), nil
}

func extractText(path, fileType string, data []byte) (string, string, string, []string) {
	switch fileType {
	case "text", "markdown", "json", "csv", "xml":
		text := normalizeExtractedText(data)
		return text, "ready", "high", nil
	case "pdf":
		text, warnings, err := extractPDFText(path)
		if err != nil {
			return "", "failed", "low", []string{err.Error()}
		}
		if strings.TrimSpace(text) == "" {
			return text, "needs_attention", "low", append(warnings, "pdf extraction returned empty text")
		}
		return text, "ready", "medium", warnings
	case "docx":
		text, warnings, err := extractDOCXText(path)
		if err != nil {
			return "", "failed", "low", []string{err.Error()}
		}
		if strings.TrimSpace(text) == "" {
			return text, "needs_attention", "low", append(warnings, "docx extraction returned empty text")
		}
		return text, "ready", "medium", warnings
	default:
		if utf8.Valid(data) {
			return normalizeExtractedText(data), "needs_attention", "medium", []string{"unknown file type treated as UTF-8 text"}
		}
		return "", "failed", "low", []string{"unknown binary file type cannot be extracted"}
	}
}

func extractPDFText(path string) (string, []string, error) {
	binary, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", nil, errors.New("pdftotext is required for PDF extraction")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-layout", path, "-")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", nil, fmt.Errorf("pdf extraction timed out: %w", ctx.Err())
	}
	if err != nil {
		return "", nil, fmt.Errorf("pdf extraction failed: %s", strings.TrimSpace(string(output)))
	}
	return normalizeExtractedText(output), nil, nil
}

func extractDOCXText(path string) (string, []string, error) {
	var warnings []string
	if binary, err := exec.LookPath("textutil"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "-convert", "txt", "-stdout", path)
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return "", warnings, fmt.Errorf("docx extraction timed out: %w", ctx.Err())
		}
		if err == nil && strings.TrimSpace(string(output)) != "" {
			return normalizeExtractedText(output), warnings, nil
		}
		warnings = append(warnings, "textutil did not extract DOCX text; used XML fallback")
	}
	text, err := extractDOCXTextFromZip(path)
	return text, warnings, err
}

func extractDOCXTextFromZip(path string) (string, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open docx zip: %w", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return "", err
		}
		return extractWordDocumentXMLText(data)
	}
	return "", errors.New("docx file does not contain word/document.xml")
}

func extractWordDocumentXMLText(data []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var b strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
			if t.Name.Local == "p" {
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.WriteString(html.UnescapeString(string(t)))
			}
		}
	}
	return normalizeExtractedText([]byte(b.String())), nil
}

func sha256Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeExtractedText(data []byte) string {
	text := strings.ToValidUTF8(string(data), "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

func splitResourceLines(text string) []string {
	text = normalizeExtractedText([]byte(text))
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func ensureTrailingNewline(text string) string {
	if text == "" || strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}

func resourceExtension(fileType, fileName string) string {
	if ext := strings.ToLower(filepath.Ext(fileName)); ext != "" {
		return ext
	}
	switch fileType {
	case "pdf":
		return ".pdf"
	case "markdown":
		return ".md"
	case "json":
		return ".json"
	case "csv":
		return ".csv"
	case "xml":
		return ".xml"
	case "docx":
		return ".docx"
	default:
		return ".txt"
	}
}

func sanitizeFileName(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	if value == "" || value == "." || value == string(filepath.Separator) {
		return "resource"
	}
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "resource"
	}
	return out
}

func removeIfPresent(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}
