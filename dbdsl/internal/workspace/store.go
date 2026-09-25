package workspace

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dbdsl/internal/dsl"
	"dbdsl/internal/generate"
	"dbdsl/internal/quality"
	"dbdsl/internal/scaffold"
	modeltrace "dbdsl/internal/trace"

	"gopkg.in/yaml.v3"
)

type Store struct {
	mu                sync.Mutex
	root              string
	statePath         string
	projects          map[string]*ProjectState
	deletedProjectIDs map[string]bool
	nextProject       int
	nextResource      int
}

type persistedStore struct {
	Version           int             `json:"version"`
	NextProject       int             `json:"next_project"`
	NextResource      int             `json:"next_resource"`
	Projects          []*ProjectState `json:"projects"`
	DeletedProjectIDs []string        `json:"deleted_project_ids,omitempty"`
}

type ProjectState struct {
	ID                             string
	Name                           string
	Description                    string
	Language                       string
	Domain                         string
	LifecycleStatus                string
	CurrentRevision                int
	CreatedAt                      time.Time
	UpdatedAt                      time.Time
	LastActivity                   string
	ModelGenerated                 bool
	DBMLReady                      bool
	Completed                      bool
	ModelPath                      string
	TaskPath                       string
	BundlePath                     string
	SourceManifestPath             string
	CombinedDocumentPath           string
	CombinedDocumentLineagePath    string
	SourceSegmentationProposalPath string
	CombinedDocumentReady          bool
	SourceUnitsProposalPath        string
	SourceUnitsPath                string
	SourceUnitQAPath               string
	ConceptualModelProposalPath    string
	ConceptualModelAcceptedPath    string
	ConceptualModelQAPath          string
	ConceptualModelDiffPath        string
	// ConceptualDescriptionPath is set by the segment-based flow: the rich
	// description the conceptual model was derived from.
	ConceptualDescriptionPath string
	LogicalPatchProposalPath  string
	ValidationReportPath      string
	LintReportPath            string
	QualityReportPath         string
	DBMLPath                  string
	TraceReportPath           string
	FinalModelAccepted        bool
	Imported                  bool
	AcceptedQuality           map[string]bool
	Resources                 []InputResource
	CompletedSnapshot         *CompletedSnapshot
	Artifacts                 map[string]ArtifactRecord
	LLMExecutionProfile       *LLMExecutionProfile
}

type ArtifactRecord struct {
	Kind        string    `json:"kind"`
	Path        string    `json:"path"`
	Status      string    `json:"status"`
	Revision    int       `json:"revision"`
	ContentHash string    `json:"content_hash,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CompletedSnapshot struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type ProjectSummary struct {
	ID                  string                `json:"id"`
	Name                string                `json:"name"`
	Description         string                `json:"description,omitempty"`
	Language            string                `json:"language,omitempty"`
	Domain              string                `json:"domain,omitempty"`
	LifecycleStatus     string                `json:"lifecycle_status"`
	CurrentRevision     int                   `json:"current_revision"`
	Counts              ProjectCounts         `json:"counts"`
	Quality             ProjectQualitySummary `json:"quality"`
	LastActivity        string                `json:"last_activity,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
	LLMExecutionProfile *LLMExecutionProfile  `json:"llm_execution_profile,omitempty"`
}

type ProjectCounts struct {
	Resources         int `json:"resources"`
	CombinedSentences int `json:"combined_sentences,omitempty"`
	SourceUnits       int `json:"source_units"`
	Entities          int `json:"entities,omitempty"`
	Relationships     int `json:"relationships,omitempty"`
}

type ProjectQualitySummary struct {
	ValidationErrors   int    `json:"validation_errors"`
	LintWarnings       int    `json:"lint_warnings"`
	TraceabilityStatus string `json:"traceability_status"`
	DBMLStatus         string `json:"dbml_status"`
}

type BundleCandidate struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	ModelPath        string `json:"model_path"`
	BundlePath       string `json:"bundle_path"`
	DSLVersion       string `json:"dsl_version"`
	PipelineVersion  string `json:"pipeline_version,omitempty"`
	SourceUnits      int    `json:"source_units"`
	Entities         int    `json:"entities"`
	Relationships    int    `json:"relationships"`
	ValidationErrors int    `json:"validation_errors"`
	LintWarnings     int    `json:"lint_warnings"`
	DBMLStatus       string `json:"dbml_status"`
}

type ArtifactHealth struct {
	SourceManifestStatus     string `json:"source_manifest_status"`
	CombinedDocumentStatus   string `json:"combined_document_status"`
	SourceSegmentationStatus string `json:"source_segmentation_status"`
	SourceUnitsStatus        string `json:"source_units_status"`
	ConceptualModelStatus    string `json:"conceptual_model_status"`
	ModelStatus              string `json:"model_status"`
	DBMLStatus               string `json:"dbml_status"`
	CanContinueToDBML        bool   `json:"can_continue_to_dbml"`
	CanCompleteProject       bool   `json:"can_complete_project"`
	CanProjectLogicalModel   bool   `json:"can_project_logical_model"`
	CanGenerateOutputs       bool   `json:"can_generate_outputs"`
	FinalModelAccepted       bool   `json:"final_model_accepted"`
}

type InputResource struct {
	ID                   string    `json:"id" yaml:"id"`
	Kind                 string    `json:"kind" yaml:"kind"`
	FileType             string    `json:"file_type" yaml:"file_type"`
	Title                string    `json:"title" yaml:"title"`
	FileName             string    `json:"file_name,omitempty" yaml:"file_name,omitempty"`
	SizeBytes            int64     `json:"size_bytes,omitempty" yaml:"size_bytes,omitempty"`
	ContentPath          string    `json:"content_path,omitempty" yaml:"content_path,omitempty"`
	ExtractedTextPath    string    `json:"extracted_text_path,omitempty" yaml:"extracted_text_path,omitempty"`
	ContentHash          string    `json:"content_hash,omitempty" yaml:"content_hash,omitempty"`
	ExtractedTextHash    string    `json:"extracted_text_hash,omitempty" yaml:"extracted_text_hash,omitempty"`
	Language             string    `json:"language,omitempty" yaml:"language,omitempty"`
	Authority            string    `json:"authority,omitempty" yaml:"authority,omitempty"`
	LineCount            int       `json:"line_count,omitempty" yaml:"line_count,omitempty"`
	Warnings             []string  `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	ExtractionStatus     string    `json:"extraction_status" yaml:"extraction_status"`
	ExtractionConfidence string    `json:"extraction_confidence,omitempty" yaml:"extraction_confidence,omitempty"`
	CreatedAt            time.Time `json:"created_at" yaml:"created_at"`
	UpdatedAt            time.Time `json:"updated_at" yaml:"updated_at"`
}

type OriginSpan struct {
	ResourceID  string `json:"resource_id"`
	Label       string `json:"label"`
	Page        int    `json:"page,omitempty"`
	LineStart   int    `json:"line_start,omitempty"`
	LineEnd     int    `json:"line_end,omitempty"`
	StartOffset int    `json:"start_offset,omitempty"`
	EndOffset   int    `json:"end_offset,omitempty"`
}

type SourceUnit struct {
	ID               string                      `json:"id"`
	Kind             string                      `json:"kind"`
	Section          string                      `json:"section,omitempty"`
	NormalizedText   string                      `json:"normalized_text"`
	Normalization    dsl.SourceTextNormalization `json:"normalization"`
	ExactText        string                      `json:"exact_text,omitempty"`
	Relevance        string                      `json:"relevance"`
	Confidence       string                      `json:"confidence"`
	ReviewStatus     string                      `json:"review_status"`
	OriginSpans      []OriginSpan                `json:"origin_spans"`
	SegmentIDs       []string                    `json:"segment_ids,omitempty"`
	ODSentenceIDs    []string                    `json:"od_sentence_ids,omitempty"` // legacy projects only
	Warnings         []string                    `json:"warnings,omitempty"`
	RequirementNotes []string                    `json:"requirement_notes,omitempty"`
}

func NewStore(root string) (*Store, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	root = absRoot
	store := &Store{
		root:              root,
		statePath:         filepath.Join(root, ".dbdsl_workbench", "state.json"),
		projects:          map[string]*ProjectState{},
		deletedProjectIDs: map[string]bool{},
	}
	if err := store.loadState(); err != nil {
		return nil, err
	}
	store.refreshCounters()
	return store, nil
}

func (s *Store) Project(id string) (*ProjectState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[id]
	if !ok {
		return nil, false
	}
	return cloneProject(project), true
}

func (s *Store) ListProjects(status, search string) ([]ProjectSummary, error) {
	s.mu.Lock()
	projects := make([]*ProjectState, 0, len(s.projects))
	for _, project := range s.projects {
		projects = append(projects, cloneProject(project))
	}
	s.mu.Unlock()
	sort.Slice(projects, func(i, j int) bool {
		iRank := projectListRank(projects[i])
		jRank := projectListRank(projects[j])
		if iRank != jRank {
			return iRank < jRank
		}
		return projects[i].UpdatedAt.After(projects[j].UpdatedAt)
	})

	var out []ProjectSummary
	for _, project := range projects {
		if status == "active" && project.Completed {
			continue
		}
		if status == "completed" && !project.Completed {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(project.Name+" "+project.Description), strings.ToLower(search)) {
			continue
		}
		summary, err := s.ProjectSummary(project.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
}

func projectListRank(project *ProjectState) int {
	name := strings.ToLower(strings.TrimSpace(project.Name))
	text := strings.ToLower(project.Name + " " + project.Description + " " + project.LastActivity)
	if strings.HasPrefix(name, "archived") || strings.Contains(text, "placeholder scaffold") {
		return 2
	}
	if strings.Contains(name, "scaffold") || strings.Contains(text, "scaffold_requires_review") {
		return 1
	}
	return 0
}

func (s *Store) CreateProject(name, description, language, domain string) (ProjectSummary, error) {
	if strings.TrimSpace(name) == "" {
		return ProjectSummary{}, errors.New("project name is required")
	}
	now := time.Now()
	s.mu.Lock()
	s.nextProject++
	id := fmt.Sprintf("project_%03d", s.nextProject)
	delete(s.deletedProjectIDs, id)
	s.projects[id] = &ProjectState{
		ID:                 id,
		Name:               strings.TrimSpace(name),
		Description:        description,
		Language:           nonEmpty(language, "sr-Cyrl"),
		Domain:             nonEmpty(domain, "information_system"),
		LifecycleStatus:    "intake",
		CurrentRevision:    1,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastActivity:       "Project created.",
		SourceManifestPath: s.sourceManifestRel(id),
		AcceptedQuality:    map[string]bool{},
	}
	err := s.writeSourceManifestLocked(s.projects[id])
	if err == nil {
		err = s.saveLocked()
	}
	s.mu.Unlock()
	if err != nil {
		return ProjectSummary{}, err
	}
	return s.ProjectSummary(id)
}

// DeleteProject permanently removes the project from persisted workspace state
// and deletes only artifacts owned by its .dbdsl_workbench project directory.
// Imported or generated bundles outside that directory are intentionally kept.
func (s *Store) DeleteProject(id string) error {
	workspaceDir, err := s.safeProjectWorkspaceDir(id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	project, ok := s.projects[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	wasDeleted := s.deletedProjectIDs[id]
	delete(s.projects, id)
	s.deletedProjectIDs[id] = true
	if err := s.saveLocked(); err != nil {
		s.projects[id] = project
		if !wasDeleted {
			delete(s.deletedProjectIDs, id)
		}
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	if err := os.RemoveAll(workspaceDir); err != nil {
		return fmt.Errorf("project deleted, but workspace cleanup failed: %w", err)
	}
	return nil
}

func (s *Store) safeProjectWorkspaceDir(id string) (string, error) {
	if id == "" || id == "." || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return "", errors.New("invalid project id")
	}
	projectsRoot := filepath.Join(s.root, ".dbdsl_workbench", "projects")
	target := filepath.Join(projectsRoot, id)
	rel, err := filepath.Rel(projectsRoot, target)
	if err != nil || rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid project workspace path")
	}
	return target, nil
}

func (s *Store) DiscoverBundles() ([]BundleCandidate, error) {
	searchRoot := filepath.Join(s.root, "poc")
	if _, err := os.Stat(searchRoot); err != nil {
		if os.IsNotExist(err) {
			return []BundleCandidate{}, nil
		}
		return nil, err
	}

	var out []BundleCandidate
	err := filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "db_model.dsl.yaml" {
			return nil
		}
		bundle, err := dsl.LoadV06Bundle(path)
		if err != nil {
			return nil
		}
		out = append(out, s.bundleCandidate(path, bundle))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].BundlePath < out[j].BundlePath
	})
	return out, nil
}

func (s *Store) ImportBundle(inputPath string) (ProjectSummary, error) {
	modelPath, err := s.resolveBundleModelPath(inputPath)
	if err != nil {
		return ProjectSummary{}, err
	}
	bundle, err := dsl.LoadV06Bundle(modelPath)
	if err != nil {
		return ProjectSummary{}, err
	}

	now := time.Now()
	taskPath := taskTextPath(bundle)
	fileName := s.relativePath(modelPath)
	fileType := "unknown"
	size := int64(0)
	contentHash := ""
	lineCount := 0
	if taskPath != "" {
		fileName = s.relativePath(taskPath)
		fileType = detectFileType(taskPath)
		if data, err := os.ReadFile(taskPath); err == nil {
			contentHash = sha256Hash(data)
			lineCount = len(splitResourceLines(normalizeExtractedText(data)))
		}
		if info, err := os.Stat(taskPath); err == nil {
			size = info.Size()
		}
	}

	s.mu.Lock()
	s.nextProject++
	s.nextResource++
	id := fmt.Sprintf("project_%03d", s.nextProject)
	delete(s.deletedProjectIDs, id)
	s.projects[id] = &ProjectState{
		ID:                 id,
		Name:               nonEmpty(bundle.Document.Model.Name, filepath.Base(filepath.Dir(modelPath))),
		Description:        bundle.Document.Model.Description,
		Language:           "sr-Cyrl",
		Domain:             nonEmpty(bundle.Document.Model.DomainSlice, "information_system"),
		LifecycleStatus:    "ready_for_dbml",
		CurrentRevision:    1,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastActivity:       "Imported v0.6 bundle from " + s.relativePath(filepath.Dir(modelPath)) + ".",
		ModelGenerated:     true,
		DBMLReady:          true,
		ModelPath:          modelPath,
		TaskPath:           taskPath,
		BundlePath:         filepath.Dir(modelPath),
		SourceManifestPath: s.sourceManifestRel(id),
		Imported:           true,
		AcceptedQuality:    map[string]bool{},
		Resources: []InputResource{{
			ID:                   fmt.Sprintf("R-%03d", s.nextResource),
			Kind:                 "uploaded_file",
			FileType:             fileType,
			Title:                "Imported v0.6 bundle",
			FileName:             fileName,
			SizeBytes:            size,
			ContentPath:          s.relativePath(taskPath),
			ExtractedTextPath:    s.relativePath(taskPath),
			ContentHash:          contentHash,
			ExtractedTextHash:    contentHash,
			Language:             "sr-Cyrl",
			Authority:            "normative",
			LineCount:            lineCount,
			ExtractionStatus:     "ready",
			ExtractionConfidence: "high",
			CreatedAt:            now,
			UpdatedAt:            now,
		}},
	}
	err = s.writeSourceManifestLocked(s.projects[id])
	if err == nil {
		err = s.saveLocked()
	}
	s.mu.Unlock()
	if err != nil {
		return ProjectSummary{}, err
	}
	return s.ProjectSummary(id)
}

func (s *Store) ScaffoldBundleFromText(name, content string) (BundleCandidate, error) {
	if strings.TrimSpace(content) == "" {
		return BundleCandidate{}, errors.New("task content is required")
	}
	title := nonEmpty(name, "Generated PIA Task")
	base := outputSlug(title)
	outDir := s.nextGeneratedBundleDir(base)
	result, err := scaffold.BundleFromText(content, outDir, scaffold.Options{
		ModelID: base + "_v05",
		Name:    title,
	})
	if err != nil {
		return BundleCandidate{}, err
	}
	bundle, err := dsl.LoadV06Bundle(result.ModelPath)
	if err != nil {
		return BundleCandidate{}, err
	}
	return s.bundleCandidate(result.ModelPath, bundle), nil
}

func (s *Store) UpdateProject(id string, baseRevision int, name, description string) error {
	return s.withProject(id, baseRevision, func(project *ProjectState) error {
		if strings.TrimSpace(name) != "" {
			project.Name = strings.TrimSpace(name)
		}
		project.Description = description
		project.LastActivity = "Project metadata updated."
		return nil
	})
}

func (s *Store) AddPastedTextResource(projectID string, baseRevision int, title, content string) (InputResource, int, error) {
	if strings.TrimSpace(content) == "" {
		return InputResource{}, 0, errors.New("content is required")
	}
	now := time.Now()
	var resource InputResource
	err := s.withProject(projectID, baseRevision, func(project *ProjectState) error {
		resourceID := fmt.Sprintf("R-%03d", s.nextResource+1)
		stored, err := buildResourceFromBytes(project.ID, resourceID, "pasted_text", nonEmpty(title, "Pasted task text"), "", "text", project.Language, "normative", []byte(ensureTrailingNewline(normalizeExtractedText([]byte(content)))), s, now)
		if err != nil {
			return err
		}
		s.nextResource++
		resource = stored
		project.Resources = append(project.Resources, resource)
		s.invalidateDerivedFromResources(project)
		project.LastActivity = "Pasted text resource added."
		return s.writeSourceManifestLocked(project)
	})
	if err != nil {
		return InputResource{}, 0, err
	}
	project, _ := s.Project(projectID)
	return resource, project.CurrentRevision, nil
}

func (s *Store) AddUploadedResource(projectID string, baseRevision int, title, fileName, fileType string, content io.Reader) (InputResource, int, error) {
	if content == nil {
		return InputResource{}, 0, errors.New("file content is required")
	}
	data, err := readLimitedResource(content)
	if err != nil {
		return InputResource{}, 0, err
	}
	now := time.Now()
	var resource InputResource
	err = s.withProject(projectID, baseRevision, func(project *ProjectState) error {
		safeName := sanitizeFileName(fileName)
		resourceID := fmt.Sprintf("R-%03d", s.nextResource+1)
		stored, err := buildResourceFromBytes(project.ID, resourceID, "uploaded_file", nonEmpty(title, safeName), safeName, fileType, project.Language, "normative", data, s, now)
		if err != nil {
			return err
		}
		s.nextResource++
		resource = stored
		project.Resources = append(project.Resources, resource)
		s.invalidateDerivedFromResources(project)
		project.LastActivity = "File resource added."
		return s.writeSourceManifestLocked(project)
	})
	if err != nil {
		return InputResource{}, 0, err
	}
	project, _ := s.Project(projectID)
	return resource, project.CurrentRevision, nil
}

func (s *Store) DeleteResource(projectID string, baseRevision int, resourceID string) (int, error) {
	err := s.withProject(projectID, baseRevision, func(project *ProjectState) error {
		next := project.Resources[:0]
		found := false
		var removed InputResource
		for _, resource := range project.Resources {
			if resource.ID == resourceID {
				found = true
				removed = resource
				continue
			}
			next = append(next, resource)
		}
		if !found {
			return ErrNotFound
		}
		if err := removeIfPresent(s.absoluteWorkspacePath(removed.ContentPath)); err != nil {
			return err
		}
		if err := removeIfPresent(s.absoluteWorkspacePath(removed.ExtractedTextPath)); err != nil {
			return err
		}
		project.Resources = next
		s.invalidateDerivedFromResources(project)
		project.LastActivity = "Resource removed."
		return s.writeSourceManifestLocked(project)
	})
	if err != nil {
		return 0, err
	}
	project, _ := s.Project(projectID)
	return project.CurrentRevision, nil
}

func (s *Store) Resources(projectID string) ([]InputResource, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	return project.Resources, nil
}

func (s *Store) CompleteProject(projectID string, baseRevision int) (ProjectSummary, CompletedSnapshot, error) {
	var snapshot CompletedSnapshot
	err := s.withProject(projectID, baseRevision, func(project *ProjectState) error {
		if !project.FinalModelAccepted {
			return errors.New("final model review has not been accepted")
		}
		if !project.DBMLReady || !project.ModelGenerated || project.DBMLPath == "" || project.TraceReportPath == "" {
			return errors.New("DBML is not ready")
		}
		if s.artifactStatus(project.DBMLPath) != "ready" || s.artifactStatus(project.TraceReportPath) != "ready" {
			return errors.New("final outputs are missing or outdated")
		}
		if report := qualityForProject(project); report.Summary.ValidationErrors > 0 || report.Summary.BlockingIssues > 0 {
			return errors.New("blocking validation or quality issues remain")
		}
		snapshot = CompletedSnapshot{
			ID:        fmt.Sprintf("snapshot_%s_%d", project.ID, project.CurrentRevision+1),
			CreatedAt: time.Now(),
		}
		project.Completed = true
		project.LifecycleStatus = "completed"
		project.CompletedSnapshot = &snapshot
		project.LastActivity = "Project completed."
		return nil
	})
	if err != nil {
		return ProjectSummary{}, CompletedSnapshot{}, err
	}
	summary, err := s.ProjectSummary(projectID)
	return summary, snapshot, err
}

func (s *Store) ReopenProject(projectID, note string) (ProjectSummary, string, error) {
	source, ok := s.Project(projectID)
	if !ok {
		return ProjectSummary{}, "", ErrNotFound
	}
	if !source.Completed || source.CompletedSnapshot == nil {
		return ProjectSummary{}, "", errors.New("only a completed project can be reopened")
	}
	summary, err := s.CreateProject(source.Name+" - Revision 2", note, source.Language, source.Domain)
	if err != nil {
		return ProjectSummary{}, "", err
	}
	if err := s.copyProjectLLMRuns(projectID, summary.ID); err != nil {
		_ = s.DeleteProject(summary.ID)
		return ProjectSummary{}, "", fmt.Errorf("preserve LLM run history while reopening: %w", err)
	}
	reopenedResources, err := s.copyProjectResources(source.Resources, summary.ID)
	if err != nil {
		_ = s.DeleteProject(summary.ID)
		return ProjectSummary{}, "", fmt.Errorf("preserve input resources while reopening: %w", err)
	}
	s.mu.Lock()
	reopened := s.projects[summary.ID]
	reopened.ModelGenerated = source.ModelGenerated
	reopened.DBMLReady = false
	reopened.Completed = false
	reopened.FinalModelAccepted = false
	reopened.ModelPath = source.ModelPath
	reopened.TaskPath = source.TaskPath
	reopened.BundlePath = source.BundlePath
	reopened.CombinedDocumentPath = source.CombinedDocumentPath
	reopened.CombinedDocumentLineagePath = source.CombinedDocumentLineagePath
	reopened.SourceSegmentationProposalPath = source.SourceSegmentationProposalPath
	reopened.CombinedDocumentReady = source.CombinedDocumentReady
	reopened.SourceUnitsProposalPath = source.SourceUnitsProposalPath
	reopened.SourceUnitsPath = source.SourceUnitsPath
	reopened.SourceUnitQAPath = source.SourceUnitQAPath
	reopened.ConceptualModelProposalPath = source.ConceptualModelProposalPath
	reopened.ConceptualModelAcceptedPath = source.ConceptualModelAcceptedPath
	reopened.ConceptualModelQAPath = source.ConceptualModelQAPath
	reopened.ConceptualDescriptionPath = source.ConceptualDescriptionPath
	reopened.LogicalPatchProposalPath = source.LogicalPatchProposalPath
	reopened.ValidationReportPath = source.ValidationReportPath
	reopened.LintReportPath = source.LintReportPath
	reopened.QualityReportPath = source.QualityReportPath
	reopened.DBMLPath = ""
	reopened.TraceReportPath = ""
	reopened.Imported = source.Imported
	reopened.LifecycleStatus = "ready_for_model_generation"
	if reopened.ModelGenerated {
		reopened.LifecycleStatus = "model_generated"
	}
	reopened.AcceptedQuality = map[string]bool{}
	for id, accepted := range source.AcceptedQuality {
		reopened.AcceptedQuality[id] = accepted
	}
	reopened.Resources = reopenedResources
	reopened.LastActivity = "Reopened from completed snapshot " + source.CompletedSnapshot.ID + "; final model acceptance and outputs must be regenerated."
	sourceSnapshotID := source.CompletedSnapshot.ID
	err = s.writeSourceManifestLocked(reopened)
	if err == nil {
		err = s.saveLocked()
	}
	s.mu.Unlock()
	if err != nil {
		return ProjectSummary{}, "", err
	}
	reopenedSummary, err := s.ProjectSummary(summary.ID)
	return reopenedSummary, sourceSnapshotID, err
}

func (s *Store) copyProjectResources(resources []InputResource, targetProjectID string) ([]InputResource, error) {
	copied := make([]InputResource, 0, len(resources))
	for _, resource := range resources {
		content, err := os.ReadFile(s.absoluteWorkspacePath(resource.ContentPath))
		if err != nil {
			return nil, err
		}
		extracted, err := os.ReadFile(s.absoluteWorkspacePath(resource.ExtractedTextPath))
		if err != nil {
			return nil, err
		}
		contentRel := s.resourceOriginalRel(targetProjectID, resource.ID, resource.FileType, resource.FileName)
		extractedRel := s.resourceExtractedRel(targetProjectID, resource.ID)
		if err := writeAtomic(s.absoluteWorkspacePath(contentRel), content); err != nil {
			return nil, err
		}
		if err := writeAtomic(s.absoluteWorkspacePath(extractedRel), extracted); err != nil {
			return nil, err
		}
		clone := resource
		clone.ContentPath = contentRel
		clone.ExtractedTextPath = extractedRel
		copied = append(copied, clone)
	}
	return copied, nil
}

func (s *Store) copyProjectLLMRuns(sourceProjectID, targetProjectID string) error {
	sourceRoot := filepath.Join(s.projectWorkspaceDir(sourceProjectID), "llm_runs")
	targetRoot := filepath.Join(s.projectWorkspaceDir(targetProjectID), "llm_runs")
	return filepath.WalkDir(sourceRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, current)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("invalid LLM run path while reopening project")
		}
		target := filepath.Join(targetRoot, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		return writeAtomic(target, data)
	})
}

func (s *Store) ProjectSummary(projectID string) (ProjectSummary, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return ProjectSummary{}, ErrNotFound
	}
	var bundle *dsl.Bundle
	if project.ModelPath != "" {
		// A bundle that cannot be read must not hide the whole project list;
		// the summary then reports the project without model counts.
		if loaded, err := s.bundleForProject(project); err == nil {
			bundle = loaded
		}
	}
	report := qualityForProject(project)
	counts := ProjectCounts{Resources: len(project.Resources), CombinedSentences: s.combinedSentenceCount(project)}
	if bundle != nil {
		counts.SourceUnits = len(bundle.SourceUnits.SourceUnits)
		counts.Entities = boolCount(project.ModelGenerated, len(bundle.Document.Entities))
		counts.Relationships = boolCount(project.ModelGenerated, len(bundle.Document.Relationships))
	} else if artifacts, err := s.SourceUnitArtifacts(project.ID); err == nil {
		counts.SourceUnits = len(artifacts.Accepted.SourceUnits)
	}
	health := s.artifactHealthForProject(project, report)
	return ProjectSummary{
		ID:              project.ID,
		Name:            project.Name,
		Description:     project.Description,
		Language:        project.Language,
		Domain:          project.Domain,
		LifecycleStatus: deriveLifecycle(project, health),
		CurrentRevision: project.CurrentRevision,
		Counts:          counts,
		Quality: ProjectQualitySummary{
			ValidationErrors:   report.Summary.ValidationErrors,
			LintWarnings:       report.Summary.LintWarnings,
			TraceabilityStatus: report.Summary.TraceabilityStatus,
			DBMLStatus:         report.Summary.DBMLStatus,
		},
		LastActivity:        project.LastActivity,
		CreatedAt:           project.CreatedAt,
		UpdatedAt:           project.UpdatedAt,
		LLMExecutionProfile: project.LLMExecutionProfile,
	}, nil
}

func (s *Store) ArtifactHealth(projectID string) (ArtifactHealth, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return ArtifactHealth{}, ErrNotFound
	}
	report := qualityForProject(project)
	return s.artifactHealthForProject(project, report), nil
}

func (s *Store) artifactHealthForProject(project *ProjectState, report quality.Report) ArtifactHealth {
	sourceUnitsStatus := s.artifactStatus(project.SourceUnitsPath)
	if sourceUnitsStatus == "ready" {
		if artifacts, err := s.SourceUnitArtifacts(project.ID); err == nil && len(artifacts.QA.NeedsAttention) > 0 {
			sourceUnitsStatus = "needs_attention"
		}
	}
	modelStatus := "not_generated"
	if project.ModelGenerated {
		if s.artifactStatus(project.ModelPath) != "ready" || report.Summary.ValidationErrors > 0 {
			modelStatus = "failed"
		} else {
			modelStatus = "ready"
		}
	}
	dbmlStatus := report.Summary.DBMLStatus
	if project.DBMLReady && project.DBMLPath != "" && s.artifactStatus(project.DBMLPath) != "ready" {
		dbmlStatus = "outdated"
	}
	modelValid := modelStatus == "ready" && report.Summary.ValidationErrors == 0
	conceptualStatus := s.artifactStatus(project.ConceptualModelAcceptedPath)
	if conceptualStatus == "not_generated" && s.artifactStatus(project.ConceptualModelProposalPath) == "ready" {
		conceptualStatus = "proposed"
	}
	segmentationStatus := "not_generated"
	if project.SourceSegmentationProposalPath != "" {
		segmentationStatus = "ready"
		if _, err := s.SourceSegmentation(project.ID); err != nil {
			segmentationStatus = "not_generated"
		}
	} else if s.artifactStatus(project.CombinedDocumentPath) == "ready" {
		segmentationStatus = "ready"
	}
	return ArtifactHealth{
		SourceManifestStatus:     s.artifactStatus(project.SourceManifestPath),
		CombinedDocumentStatus:   s.artifactStatus(project.CombinedDocumentPath),
		SourceSegmentationStatus: segmentationStatus,
		SourceUnitsStatus:        sourceUnitsStatus,
		ConceptualModelStatus:    conceptualStatus,
		ModelStatus:              modelStatus,
		DBMLStatus:               dbmlStatus,
		CanContinueToDBML:        modelValid && project.FinalModelAccepted,
		CanCompleteProject:       project.DBMLReady && modelValid && project.FinalModelAccepted && dbmlStatus == "ready",
		CanProjectLogicalModel:   conceptualStatus == "ready",
		CanGenerateOutputs:       project.FinalModelAccepted && modelValid,
		FinalModelAccepted:       project.FinalModelAccepted,
	}
}

// deriveLifecycle is the single lifecycle the API exposes; it follows the
// artifacts instead of the stored status so the two never disagree.
func deriveLifecycle(project *ProjectState, health ArtifactHealth) string {
	if project.Completed && health.CanCompleteProject {
		return "completed"
	}
	if health.DBMLStatus == "ready" && health.FinalModelAccepted {
		return "ready_for_dbml"
	}
	if health.ModelStatus == "ready" {
		return "model_generated"
	}
	if health.ConceptualModelStatus == "ready" {
		return "ready_for_model_generation"
	}
	if health.ConceptualModelStatus == "proposed" {
		return "conceptual_review"
	}
	if health.SourceUnitsStatus == "needs_attention" {
		return "source_review"
	}
	if health.SourceUnitsStatus == "ready" || health.CombinedDocumentStatus == "ready" {
		return "sources_processed"
	}
	return "intake"
}

func (s *Store) SourceUnits(projectID string) ([]SourceUnit, error) {
	if _, ok := s.Project(projectID); !ok {
		return nil, ErrNotFound
	}
	if units, found, err := s.projectSourceUnits(projectID); found || err != nil {
		return units, err
	}
	return nil, nil
}

func (s *Store) SourceUnit(projectID, sourceUnitID string) (SourceUnit, bool, error) {
	units, err := s.SourceUnits(projectID)
	if err != nil {
		return SourceUnit{}, false, err
	}
	for _, unit := range units {
		if unit.ID == sourceUnitID {
			return unit, true, nil
		}
	}
	return SourceUnit{}, false, nil
}

func (s *Store) ModelGraph(projectID string) (modeltrace.ModelGraph, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return modeltrace.ModelGraph{}, ErrNotFound
	}
	if !project.ModelGenerated {
		return modeltrace.ModelGraph{}, ErrModelNotGenerated
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return modeltrace.ModelGraph{}, err
	}
	return modeltrace.BuildModelGraph(bundle.Document), nil
}

func (s *Store) TraceIndex(projectID string) (modeltrace.TraceIndex, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return modeltrace.TraceIndex{}, ErrNotFound
	}
	if !project.ModelGenerated {
		return modeltrace.TraceIndex{}, ErrModelNotGenerated
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return modeltrace.TraceIndex{}, err
	}
	return modeltrace.BuildTraceIndex(bundle.Document), nil
}

func (s *Store) ElementDetails(projectID, elementID string) (modeltrace.ElementDetails, bool, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return modeltrace.ElementDetails{}, false, ErrNotFound
	}
	if !project.ModelGenerated {
		return modeltrace.ElementDetails{}, false, ErrModelNotGenerated
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return modeltrace.ElementDetails{}, false, err
	}
	details, found := modeltrace.BuildElementDetails(bundle.Document, elementID)
	if !found {
		return modeltrace.ElementDetails{}, false, nil
	}
	sourceByID := map[string]string{}
	for _, unit := range bundle.SourceUnits.SourceUnits {
		sourceByID[unit.ID] = unit.Text.Normalized
	}
	for _, sourceID := range details.Evidence.SourceUnits {
		if text := sourceByID[sourceID]; text != "" {
			details.SourceUnitSummaries = append(details.SourceUnitSummaries, sourceID+": "+text)
		}
	}
	return details, true, nil
}

func (s *Store) Quality(projectID string) (quality.Report, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return quality.Report{}, ErrNotFound
	}
	report := qualityForProject(project)
	for i := range report.Issues {
		if project.AcceptedQuality[report.Issues[i].ID] {
			report.Issues[i].Accepted = true
			report.Issues[i].Blocking = false
		}
	}
	return report, nil
}

func (s *Store) AcceptQualityIssue(projectID, issueID string) (int, error) {
	err := s.withProject(projectID, 0, func(project *ProjectState) error {
		project.AcceptedQuality[issueID] = true
		project.LastActivity = "Quality warning accepted."
		return nil
	})
	if err != nil {
		return 0, err
	}
	project, _ := s.Project(projectID)
	return project.CurrentRevision, nil
}

func (s *Store) DBML(projectID string) (string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return "", ErrNotFound
	}
	if !project.DBMLReady {
		return "", ErrDBMLNotReady
	}
	if project.DBMLPath != "" {
		data, err := os.ReadFile(s.absoluteWorkspacePath(project.DBMLPath))
		if err != nil {
			return "", ErrDBMLNotReady
		}
		return string(data), nil
	}
	return generate.DBMLFile(project.ModelPath)
}

// PostgreSQL renders DDL from the same accepted DB-DSL model the DBML came from.
// It is generated on demand, so it can never drift from the model.
func (s *Store) PostgreSQL(projectID string) (string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return "", ErrNotFound
	}
	if !project.DBMLReady || project.ModelPath == "" {
		return "", ErrDBMLNotReady
	}
	return generate.PostgreSQLFile(project.ModelPath)
}

func (s *Store) TraceReport(projectID string) (string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return "", ErrNotFound
	}
	if !project.ModelGenerated {
		return "", ErrModelNotGenerated
	}
	if project.TraceReportPath != "" {
		data, err := os.ReadFile(s.absoluteWorkspacePath(project.TraceReportPath))
		if err != nil {
			return "", ErrModelNotGenerated
		}
		return string(data), nil
	}
	return generate.TraceFile(project.ModelPath)
}

func (s *Store) ExportBundle(projectID string) ([]byte, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	if project.ModelPath == "" {
		return nil, ErrModelNotGenerated
	}
	dbml, err := s.DBML(projectID)
	if err != nil {
		return nil, err
	}
	report, err := s.TraceReport(projectID)
	if err != nil {
		return nil, err
	}
	postgreSQL, err := s.PostgreSQL(projectID)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	bundleDir := filepath.Dir(project.ModelPath)
	exportResources := buildExportResources(project.Resources)
	sourceManifest, err := s.exportSourceManifest(project, exportResources)
	if err != nil {
		return nil, err
	}
	optimizationReport, err := s.LLMOptimizationReport(projectID)
	if err != nil {
		return nil, err
	}
	optimizationJSON, err := json.MarshalIndent(optimizationReport, "", "  ")
	if err != nil {
		return nil, err
	}
	files := map[string]string{
		"TASK.md":                           s.readArtifact(project.TaskPath),
		"source_manifest.yaml":              sourceManifest,
		"source_segmentation.proposed.json": s.readArtifact(project.SourceSegmentationProposalPath),
		"combined_document.md":              s.readArtifact(project.CombinedDocumentPath),
		"combined_document_lineage.json":    s.readArtifact(project.CombinedDocumentLineagePath),
		"source_units.proposed.json":        s.readArtifact(project.SourceUnitsProposalPath),
		"model.dbml":                        dbml,
		"schema.postgresql.sql":             postgreSQL,
		"traceability_report.md":            report,
		"db_model.dsl.yaml":                 readString(project.ModelPath),
		"source_units.yaml":                 nonEmpty(readString(filepath.Join(bundleDir, "source_units.yaml")), s.readArtifact(project.SourceUnitsPath)),
		"review_decisions.yaml":             readString(filepath.Join(bundleDir, "review_decisions.yaml")),
		"source_unit_qa.json":               s.readArtifact(project.SourceUnitQAPath),
		"conceptual_model.proposed.json":    s.readArtifact(project.ConceptualModelProposalPath),
		"conceptual_model.accepted.json":    s.readArtifact(project.ConceptualModelAcceptedPath),
		"conceptual_model_qa.json":          s.readArtifact(project.ConceptualModelQAPath),
		"conceptual_model_diff.json":        s.readArtifact(project.ConceptualModelDiffPath),
		"conceptual_description.json":       s.readArtifact(project.ConceptualDescriptionPath),
		"dbdsl_patch.proposed.json":         s.readArtifact(project.LogicalPatchProposalPath),
		"validation_report.json":            s.readArtifact(project.ValidationReportPath),
		"lint_report.json":                  s.readArtifact(project.LintReportPath),
		"quality_report.json":               s.readArtifact(project.QualityReportPath),
		"llm_optimization_report.json":      string(optimizationJSON),
		"llm_optimization_report.md":        optimizationReport.Markdown(),
	}
	for _, resource := range exportResources {
		base := filepath.ToSlash(filepath.Join("resources", resource.Metadata.ID))
		files[base+resource.Extension] = s.readArtifact(resource.ContentPath)
		files[base+".extracted.txt"] = s.readArtifact(resource.ExtractedPath)
		metadata, marshalErr := json.MarshalIndent(resource.Metadata, "", "  ")
		if marshalErr != nil {
			return nil, marshalErr
		}
		files[base+".metadata.json"] = string(metadata)
	}
	llmRunsRoot := filepath.Join(s.projectWorkspaceDir(projectID), "llm_runs")
	walkErr := filepath.WalkDir(llmRunsRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(s.projectWorkspaceDir(projectID), filePath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return nil, walkErr
	}
	manifestArtifacts := map[string]any{}
	for name, content := range files {
		if content == "" {
			// Artifacts a flow did not produce (e.g. design obligations in the
			// segment-based flow) are left out of the archive and the manifest alike.
			delete(files, name)
			continue
		}
		manifestArtifacts[name] = map[string]any{"sha256": sha256Hash([]byte(content)), "bytes": len(content)}
	}
	modelHash := sha256Hash([]byte(files["db_model.dsl.yaml"]))
	manifestBytes, _ := json.MarshalIndent(map[string]any{
		"schema_version": 1, "project_id": project.ID, "project_revision": project.CurrentRevision,
		"model_sha256": modelHash, "artifacts": manifestArtifacts,
		"derivations": map[string]any{
			"model.dbml":             map[string]string{"source": "db_model.dsl.yaml", "source_sha256": modelHash},
			"traceability_report.md": map[string]string{"source": "db_model.dsl.yaml", "source_sha256": modelHash},
		},
	}, "", "  ")
	files["export_manifest.json"] = string(manifestBytes)
	names := sortedKeysString(files)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type exportResource struct {
	Metadata      InputResource
	ContentPath   string
	ExtractedPath string
	Extension     string
}

func buildExportResources(resources []InputResource) []exportResource {
	sorted := append([]InputResource(nil), resources...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	out := make([]exportResource, 0, len(sorted))
	for index, resource := range sorted {
		resourceID := resource.ID
		if resourceID == "" || filepath.Base(resourceID) != resourceID || strings.Contains(resourceID, "..") {
			resourceID = fmt.Sprintf("resource_%03d", index+1)
		}
		extension := exportResourceExtension(resource)
		metadata := resource
		metadata.ID = resourceID
		metadata.ContentPath = filepath.ToSlash(filepath.Join("resources", resourceID+extension))
		metadata.ExtractedTextPath = filepath.ToSlash(filepath.Join("resources", resourceID+".extracted.txt"))
		out = append(out, exportResource{Metadata: metadata, ContentPath: resource.ContentPath, ExtractedPath: resource.ExtractedTextPath, Extension: extension})
	}
	return out
}

func (s *Store) exportSourceManifest(project *ProjectState, resources []exportResource) (string, error) {
	manifest := s.buildSourceManifest(project)
	manifest.Document.WorkspacePath = "."
	manifest.Document.GeneratedAt = project.UpdatedAt
	manifest.Resources = make([]InputResource, 0, len(resources))
	for _, resource := range resources {
		manifest.Resources = append(manifest.Resources, resource.Metadata)
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func exportResourceExtension(resource InputResource) string {
	extension := strings.ToLower(filepath.Ext(filepath.Base(resource.FileName)))
	switch extension {
	case ".txt", ".md", ".json", ".csv", ".xml", ".pdf", ".docx":
		return extension
	}
	switch strings.ToLower(resource.FileType) {
	case "markdown":
		return ".md"
	case "json":
		return ".json"
	case "csv":
		return ".csv"
	case "xml":
		return ".xml"
	case "pdf":
		return ".pdf"
	case "docx":
		return ".docx"
	default:
		return ".txt"
	}
}

func (s *Store) readArtifact(path string) string {
	return readString(s.absoluteWorkspacePath(path))
}

func (s *Store) loadState() error {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read workspace state: %w", err)
	}

	var state persistedStore
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse workspace state: %w", err)
	}
	if state.Version > 2 {
		return fmt.Errorf("workspace state version %d is newer than supported version 2", state.Version)
	}

	s.deletedProjectIDs = map[string]bool{}
	for _, id := range state.DeletedProjectIDs {
		if strings.TrimSpace(id) != "" {
			s.deletedProjectIDs[id] = true
		}
	}

	loaded := map[string]*ProjectState{}
	for _, project := range state.Projects {
		if project == nil || strings.TrimSpace(project.ID) == "" {
			continue
		}
		if s.deletedProjectIDs[project.ID] {
			continue
		}
		cp := cloneProject(project)
		s.normalizeLoadedProject(cp)
		loaded[cp.ID] = cp
	}
	s.projects = loaded

	s.nextProject = maxInt(s.nextProject, state.NextProject)
	s.nextResource = maxInt(s.nextResource, state.NextResource)
	s.refreshCounters()
	return nil
}

func (s *Store) refreshCounters() {
	for _, project := range s.projects {
		s.nextProject = maxInt(s.nextProject, projectIndex(project.ID))
		for _, resource := range project.Resources {
			s.nextResource = maxInt(s.nextResource, resourceIndex(resource.ID))
		}
	}
}

func (s *Store) saveLocked() error {
	state := persistedStore{
		Version:           2,
		NextProject:       s.nextProject,
		NextResource:      s.nextResource,
		Projects:          make([]*ProjectState, 0, len(s.projects)),
		DeletedProjectIDs: sortedKeys(s.deletedProjectIDs),
	}
	for _, project := range s.projects {
		s.refreshArtifactRegistry(project)
		cp := cloneProject(project)
		cp.ModelPath = s.relativePath(cp.ModelPath)
		cp.TaskPath = s.relativePath(cp.TaskPath)
		cp.BundlePath = s.relativePath(cp.BundlePath)
		cp.SourceManifestPath = s.relativePath(cp.SourceManifestPath)
		cp.CombinedDocumentPath = s.relativePath(cp.CombinedDocumentPath)
		cp.CombinedDocumentLineagePath = s.relativePath(cp.CombinedDocumentLineagePath)
		cp.SourceSegmentationProposalPath = s.relativePath(cp.SourceSegmentationProposalPath)
		cp.SourceUnitsProposalPath = s.relativePath(cp.SourceUnitsProposalPath)
		cp.SourceUnitsPath = s.relativePath(cp.SourceUnitsPath)
		cp.SourceUnitQAPath = s.relativePath(cp.SourceUnitQAPath)
		cp.ConceptualModelProposalPath = s.relativePath(cp.ConceptualModelProposalPath)
		cp.ConceptualModelAcceptedPath = s.relativePath(cp.ConceptualModelAcceptedPath)
		cp.ConceptualModelQAPath = s.relativePath(cp.ConceptualModelQAPath)
		cp.ConceptualDescriptionPath = s.relativePath(cp.ConceptualDescriptionPath)
		cp.LogicalPatchProposalPath = s.relativePath(cp.LogicalPatchProposalPath)
		cp.ValidationReportPath = s.relativePath(cp.ValidationReportPath)
		cp.LintReportPath = s.relativePath(cp.LintReportPath)
		cp.QualityReportPath = s.relativePath(cp.QualityReportPath)
		cp.DBMLPath = s.relativePath(cp.DBMLPath)
		cp.TraceReportPath = s.relativePath(cp.TraceReportPath)
		for key, artifact := range cp.Artifacts {
			artifact.Path = s.relativePath(artifact.Path)
			cp.Artifacts[key] = artifact
		}
		state.Projects = append(state.Projects, cp)
	}
	sort.Slice(state.Projects, func(i, j int) bool {
		return state.Projects[i].ID < state.Projects[j].ID
	})

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o755); err != nil {
		return fmt.Errorf("create workspace state directory: %w", err)
	}
	tmpPath := s.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write workspace state: %w", err)
	}
	if err := os.Rename(tmpPath, s.statePath); err != nil {
		return fmt.Errorf("replace workspace state: %w", err)
	}
	return nil
}

func (s *Store) normalizeLoadedProject(project *ProjectState) {
	project.ModelPath = s.absoluteWorkspacePath(project.ModelPath)
	project.TaskPath = s.absoluteWorkspacePath(project.TaskPath)
	project.BundlePath = s.absoluteWorkspacePath(project.BundlePath)
	project.SourceManifestPath = s.absoluteWorkspacePath(project.SourceManifestPath)
	project.CombinedDocumentPath = s.absoluteWorkspacePath(project.CombinedDocumentPath)
	project.CombinedDocumentLineagePath = s.absoluteWorkspacePath(project.CombinedDocumentLineagePath)
	project.SourceSegmentationProposalPath = s.absoluteWorkspacePath(project.SourceSegmentationProposalPath)
	project.SourceUnitsProposalPath = s.absoluteWorkspacePath(project.SourceUnitsProposalPath)
	project.SourceUnitsPath = s.absoluteWorkspacePath(project.SourceUnitsPath)
	project.SourceUnitQAPath = s.absoluteWorkspacePath(project.SourceUnitQAPath)
	project.ConceptualModelProposalPath = s.absoluteWorkspacePath(project.ConceptualModelProposalPath)
	project.ConceptualModelAcceptedPath = s.absoluteWorkspacePath(project.ConceptualModelAcceptedPath)
	project.ConceptualModelQAPath = s.absoluteWorkspacePath(project.ConceptualModelQAPath)
	project.ConceptualModelDiffPath = s.absoluteWorkspacePath(project.ConceptualModelDiffPath)
	project.ConceptualDescriptionPath = s.absoluteWorkspacePath(project.ConceptualDescriptionPath)
	project.LogicalPatchProposalPath = s.absoluteWorkspacePath(project.LogicalPatchProposalPath)
	project.ValidationReportPath = s.absoluteWorkspacePath(project.ValidationReportPath)
	project.LintReportPath = s.absoluteWorkspacePath(project.LintReportPath)
	project.QualityReportPath = s.absoluteWorkspacePath(project.QualityReportPath)
	project.DBMLPath = s.absoluteWorkspacePath(project.DBMLPath)
	project.TraceReportPath = s.absoluteWorkspacePath(project.TraceReportPath)
	if project.Artifacts == nil {
		project.Artifacts = map[string]ArtifactRecord{}
	}
	for key, artifact := range project.Artifacts {
		artifact.Path = s.absoluteWorkspacePath(artifact.Path)
		project.Artifacts[key] = artifact
	}
	if project.BundlePath == "" && project.ModelPath != "" {
		project.BundlePath = filepath.Dir(project.ModelPath)
	}
	if project.SourceManifestPath == "" {
		project.SourceManifestPath = s.absoluteWorkspacePath(s.sourceManifestRel(project.ID))
	}
	if project.AcceptedQuality == nil {
		project.AcceptedQuality = map[string]bool{}
	}
	if project.CurrentRevision == 0 {
		project.CurrentRevision = 1
	}
	for i := range project.Resources {
		if project.Resources[i].Authority == "" {
			project.Resources[i].Authority = "normative"
		}
		if project.Resources[i].Language == "" {
			project.Resources[i].Language = nonEmpty(project.Language, "unknown")
		}
		if project.Resources[i].Warnings == nil {
			project.Resources[i].Warnings = []string{}
		}
	}
	now := time.Now()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	if project.UpdatedAt.IsZero() {
		project.UpdatedAt = project.CreatedAt
	}
	if project.CombinedDocumentReady && s.artifactStatus(project.CombinedDocumentPath) != "ready" {
		project.CombinedDocumentReady = false
	}
	if project.ModelGenerated && s.artifactStatus(project.ModelPath) != "ready" {
		project.ModelGenerated = false
		project.FinalModelAccepted = false
		project.DBMLReady = false
	}
	// A model written by an older DSL contract cannot be read any more. The
	// project keeps its sources, segmentation and accepted conceptual model and
	// continues from the logical stage, which regenerates the bundle.
	if project.ModelPath != "" {
		if _, err := dsl.LoadV06Bundle(project.ModelPath); err != nil {
			project.ModelPath = ""
			project.BundlePath = ""
			project.TaskPath = ""
			project.DBMLPath = ""
			project.TraceReportPath = ""
			project.LogicalPatchProposalPath = ""
			project.ValidationReportPath = ""
			project.LintReportPath = ""
			project.QualityReportPath = ""
			project.ModelGenerated = false
			project.FinalModelAccepted = false
			project.DBMLReady = false
			project.Completed = false
			project.CompletedSnapshot = nil
			project.LifecycleStatus = "ready_for_model_generation"
			project.LastActivity = "The logical model was written by an older DB-DSL version; rerun the logical stage to regenerate it."
		}
	}
	if project.DBMLReady && project.DBMLPath != "" && s.artifactStatus(project.DBMLPath) != "ready" {
		project.DBMLReady = false
	}
	s.refreshArtifactRegistry(project)
}

func (s *Store) refreshArtifactRegistry(project *ProjectState) {
	project.Artifacts = map[string]ArtifactRecord{}
	paths := map[string]string{
		"source_manifest": project.SourceManifestPath, "combined_document": project.CombinedDocumentPath,
		"source_segmentation_proposed": project.SourceSegmentationProposalPath, "combined_document_lineage": project.CombinedDocumentLineagePath, "source_units_proposed": project.SourceUnitsProposalPath,
		"source_units_accepted": project.SourceUnitsPath, "source_unit_qa": project.SourceUnitQAPath,
		"conceptual_model_proposed": project.ConceptualModelProposalPath, "conceptual_model_accepted": project.ConceptualModelAcceptedPath,
		"conceptual_model_qa": project.ConceptualModelQAPath, "conceptual_model_diff": project.ConceptualModelDiffPath,
		"conceptual_description": project.ConceptualDescriptionPath,
		"logical_patch_proposed": project.LogicalPatchProposalPath, "logical_model_accepted": project.ModelPath, "validation_report": project.ValidationReportPath,
		"lint_report": project.LintReportPath, "quality_report": project.QualityReportPath,
		"model_dbml": project.DBMLPath, "traceability_report": project.TraceReportPath,
	}
	for kind, artifactPath := range paths {
		if artifactPath == "" {
			continue
		}
		record := ArtifactRecord{Kind: kind, Path: artifactPath, Status: s.artifactStatus(artifactPath), Revision: project.CurrentRevision, UpdatedAt: project.UpdatedAt}
		absolutePath := s.absoluteWorkspacePath(artifactPath)
		if data, err := os.ReadFile(absolutePath); err == nil {
			record.ContentHash = sha256Hash(data)
			if info, statErr := os.Stat(absolutePath); statErr == nil {
				record.UpdatedAt = info.ModTime()
			}
		}
		project.Artifacts[kind] = record
	}
}

func (s *Store) absoluteWorkspacePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(cleaned) {
		return cleaned
	}
	return filepath.Join(s.root, cleaned)
}

func (s *Store) nextGeneratedBundleDir(base string) string {
	parent := filepath.Join(s.root, "poc", "generated")
	for i := 0; ; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s_%d", base, i+1)
		}
		candidate := filepath.Join(parent, name, "v0.6")
		if _, err := os.Stat(filepath.Join(candidate, "db_model.dsl.yaml")); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (s *Store) bundleForProject(project *ProjectState) (*dsl.Bundle, error) {
	if project.ModelPath == "" {
		return nil, ErrModelNotGenerated
	}
	return dsl.LoadV06Bundle(project.ModelPath)
}

func (s *Store) bundleCandidate(modelPath string, bundle *dsl.Bundle) BundleCandidate {
	bundlePath := filepath.Dir(modelPath)
	relBundle := s.relativePath(bundlePath)
	id := strings.ToLower(relBundle)
	id = strings.NewReplacer("/", "__", "\\", "__", " ", "_", ".", "_").Replace(id)
	report := quality.BuildReport(modelPath, true)
	return BundleCandidate{
		ID:               id,
		Name:             nonEmpty(bundle.Document.Model.Name, filepath.Base(bundlePath)),
		Description:      bundle.Document.Model.Description,
		ModelPath:        s.relativePath(modelPath),
		BundlePath:       relBundle,
		DSLVersion:       bundle.Document.DSL.Version,
		PipelineVersion:  bundle.Document.Source.PipelineVersion,
		SourceUnits:      len(bundle.SourceUnits.SourceUnits),
		Entities:         len(bundle.Document.Entities),
		Relationships:    len(bundle.Document.Relationships),
		ValidationErrors: report.Summary.ValidationErrors,
		LintWarnings:     report.Summary.LintWarnings,
		DBMLStatus:       report.Summary.DBMLStatus,
	}
}

func (s *Store) resolveBundleModelPath(inputPath string) (string, error) {
	resolved, err := s.resolveWorkspacePath(inputPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("bundle path not found: %w", err)
	}
	if info.IsDir() {
		resolved = filepath.Join(resolved, "db_model.dsl.yaml")
		if _, err := os.Stat(resolved); err != nil {
			return "", fmt.Errorf("db_model.dsl.yaml not found in bundle folder: %w", err)
		}
	}
	return resolved, nil
}

func (s *Store) resolveWorkspacePath(inputPath string) (string, error) {
	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" {
		return "", errors.New("bundle path is required")
	}
	candidate := filepath.Clean(trimmed)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(s.root, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(s.root, absCandidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("bundle path must be inside the workspace")
	}
	return absCandidate, nil
}

func (s *Store) relativePath(path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func taskTextPath(bundle *dsl.Bundle) string {
	if bundle.Document.Source.TaskTextFile != "" {
		candidate := dsl.ResolveModelResourcePath(bundle.ModelPath, bundle.Document.Source.TaskTextFile)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	for _, name := range []string{"TASK_FULL.md", "TASK_SUBSET.md", "TASK.md"} {
		candidate := filepath.Join(filepath.Dir(bundle.ModelPath), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func qualityForProject(project *ProjectState) quality.Report {
	if project.ModelPath == "" {
		return quality.Report{
			Summary: quality.Summary{
				TraceabilityStatus: "not_generated",
				DBMLStatus:         "not_generated",
			},
		}
	}
	return quality.BuildReport(project.ModelPath, project.DBMLReady)
}

var (
	ErrNotFound          = errors.New("not found")
	ErrRevisionConflict  = errors.New("revision conflict")
	ErrModelNotGenerated = errors.New("model is not generated")
	ErrDBMLNotReady      = errors.New("DBML is not ready")
)

func (s *Store) withProject(id string, baseRevision int, fn func(project *ProjectState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[id]
	if !ok {
		return ErrNotFound
	}
	if baseRevision > 0 && project.CurrentRevision != baseRevision {
		return ErrRevisionConflict
	}
	if err := fn(project); err != nil {
		return err
	}
	project.CurrentRevision++
	project.UpdatedAt = time.Now()
	return s.saveLocked()
}

func cloneProject(project *ProjectState) *ProjectState {
	cp := *project
	if project.LLMExecutionProfile != nil {
		profile := *project.LLMExecutionProfile
		cp.LLMExecutionProfile = &profile
	}
	cp.AcceptedQuality = map[string]bool{}
	for key, value := range project.AcceptedQuality {
		cp.AcceptedQuality[key] = value
	}
	cp.Artifacts = map[string]ArtifactRecord{}
	for key, value := range project.Artifacts {
		cp.Artifacts[key] = value
	}
	cp.Resources = append([]InputResource(nil), project.Resources...)
	if project.CompletedSnapshot != nil {
		snapshot := *project.CompletedSnapshot
		cp.CompletedSnapshot = &snapshot
	}
	return &cp
}

func stringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func sortedKeys(values map[string]bool) []string {
	var out []string
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sortedKeysString(values map[string]string) []string {
	var out []string
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func boolCount(enabled bool, count int) int {
	if !enabled {
		return 0
	}
	return count
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func projectIndex(id string) int {
	if !strings.HasPrefix(id, "project_") {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimPrefix(id, "project_"))
	if err != nil {
		return 0
	}
	return value
}

func resourceIndex(id string) int {
	if !strings.HasPrefix(id, "R-") {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimPrefix(id, "R-"))
	if err != nil {
		return 0
	}
	return value
}

func outputSlug(value string) string {
	var out []rune
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out = append(out, r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			out = append(out, '_')
			lastUnderscore = true
		}
	}
	slug := strings.Trim(string(out), "_")
	if slug == "" {
		return "generated_pia_task"
	}
	if slug[0] >= '0' && slug[0] <= '9' {
		return "task_" + slug
	}
	return slug
}

func detectFileType(fileName string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), ".")
	switch ext {
	case "pdf", "json", "csv", "xml":
		return ext
	case "md", "markdown":
		return "markdown"
	case "txt":
		return "text"
	case "docx":
		return "docx"
	default:
		return "unknown"
	}
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
