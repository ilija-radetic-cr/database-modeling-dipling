package workspace

import (
	"archive/zip"
	"bytes"
	"context"
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
	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
	"dbdsl/internal/quality"
	"dbdsl/internal/scaffold"
	modeltrace "dbdsl/internal/trace"

	"gopkg.in/yaml.v3"
)

const CanonicalProjectID = "project_phf"

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
	AnalysisReady                  bool
	ModelGenerated                 bool
	DBMLReady                      bool
	Completed                      bool
	ModelPath                      string
	TaskPath                       string
	BundlePath                     string
	SourceManifestPath             string
	CombinedDocumentPath           string
	CombinedDocumentLineagePath    string
	SourceSegmentsPath             string
	SourceFidelityReportPath       string
	SourceSegmentationProposalPath string
	SourceSegmentationQAPath       string
	CombinedDocumentReady          bool
	SourceUnitsProposalPath        string
	SourceUnitsPath                string
	SourceUnitQAPath               string
	RequirementAtomsProposalPath   string
	RequirementAtomsPath           string
	RequirementAtomQAPath          string
	DesignObligationsProposalPath  string
	DesignObligationsPath          string
	DesignObligationQAPath         string
	FunctionalAnalysisProposalPath string
	FunctionalDecompositionPath    string
	FunctionalAnalysisQAPath       string
	CRUDMappingProposalPath        string
	CRUDMatrixPath                 string
	CRUDMappingQAPath              string
	ReviewCandidatesProposalPath   string
	ReviewCandidatesPath           string
	ReviewCandidateQAPath          string
	ReviewDecisionsPath            string
	LastAppliedPatchPath           string
	ConceptualModelProposalPath    string
	ConceptualModelAcceptedPath    string
	ConceptualModelQAPath          string
	ConceptualModelDiffPath        string
	LogicalPatchProposalPath       string
	ObligationRealizationsPath     string
	SemanticVerificationPath       string
	InvariantReportPath            string
	ValidationReportPath           string
	LintReportPath                 string
	QualityReportPath              string
	DBMLPath                       string
	TraceReportPath                string
	FinalModelAccepted             bool
	Imported                       bool
	OpenReviewIDs                  map[string]bool
	AnsweredReviews                map[string]string
	AcceptedQuality                map[string]bool
	Resources                      []InputResource
	CompletedSnapshot              *CompletedSnapshot
	Artifacts                      map[string]ArtifactRecord
	LLMExecutionProfile            *LLMExecutionProfile
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
	Resources           int `json:"resources"`
	CombinedSentences   int `json:"combined_sentences,omitempty"`
	SourceUnits         int `json:"source_units"`
	Examples            int `json:"examples"`
	Requirements        int `json:"requirements"`
	FunctionalAreas     int `json:"functional_areas"`
	Operations          int `json:"operations"`
	OpenReviewQuestions int `json:"open_review_questions"`
	ReviewDecisions     int `json:"review_decisions"`
	Entities            int `json:"entities,omitempty"`
	Relationships       int `json:"relationships,omitempty"`
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
	Requirements     int    `json:"requirements"`
	FunctionalAreas  int    `json:"functional_areas"`
	Operations       int    `json:"operations"`
	Entities         int    `json:"entities"`
	Relationships    int    `json:"relationships"`
	ValidationErrors int    `json:"validation_errors"`
	LintWarnings     int    `json:"lint_warnings"`
	DBMLStatus       string `json:"dbml_status"`
}

type LLMPlanFromTextOptions struct {
	Name              string
	Content           string
	Model             string
	ReasoningEffort   string
	MaxOutputTokens   int
	MaxRepairAttempts int
}

type ArtifactHealth struct {
	AnalysisStatus             string `json:"analysis_status"`
	SourceManifestStatus       string `json:"source_manifest_status"`
	CombinedDocumentStatus     string `json:"combined_document_status"`
	SourceFidelityStatus       string `json:"source_fidelity_status"`
	SourceSegmentationStatus   string `json:"source_segmentation_status"`
	SourceUnitsStatus          string `json:"source_units_status"`
	RequirementAtomsStatus     string `json:"requirement_atoms_status"`
	DesignObligationsStatus    string `json:"design_obligations_status"`
	FunctionalAnalysisStatus   string `json:"functional_analysis_status"`
	CRUDMappingStatus          string `json:"crud_mapping_status"`
	ReviewCandidatesStatus     string `json:"review_candidates_status"`
	ConceptualModelStatus      string `json:"conceptual_model_status"`
	ModelStatus                string `json:"model_status"`
	SemanticVerificationStatus string `json:"semantic_verification_status"`
	SemanticBlockingIssues     int    `json:"semantic_blocking_issues"`
	DBMLStatus                 string `json:"dbml_status"`
	OpenReviewQuestions        int    `json:"open_review_questions"`
	CanGenerateModel           bool   `json:"can_generate_model"`
	CanContinueToDBML          bool   `json:"can_continue_to_dbml"`
	CanCompleteProject         bool   `json:"can_complete_project"`
	CanGenerateSourceUnits     bool   `json:"can_generate_source_units"`
	CanExtractRequirements     bool   `json:"can_extract_requirements"`
	CanBuildFunctionalAnalysis bool   `json:"can_build_functional_analysis"`
	CanBuildCRUDMapping        bool   `json:"can_build_crud_mapping"`
	CanProposeReviewCandidates bool   `json:"can_propose_review_candidates"`
	CanProjectLogicalModel     bool   `json:"can_project_logical_model"`
	CanGenerateOutputs         bool   `json:"can_generate_outputs"`
	FinalModelAccepted         bool   `json:"final_model_accepted"`
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
	ID                   string                      `json:"id"`
	Kind                 string                      `json:"kind"`
	Section              string                      `json:"section,omitempty"`
	NormalizedText       string                      `json:"normalized_text"`
	Normalization        dsl.SourceTextNormalization `json:"normalization"`
	ExactText            string                      `json:"exact_text,omitempty"`
	Relevance            string                      `json:"relevance"`
	Confidence           string                      `json:"confidence"`
	ReviewStatus         string                      `json:"review_status"`
	OriginSpans          []OriginSpan                `json:"origin_spans"`
	LinkedExamples       []string                    `json:"linked_examples"`
	LinkedRequirements   []string                    `json:"linked_requirements"`
	OpenReviewCandidates []string                    `json:"open_review_candidates"`
	ODSentenceIDs        []string                    `json:"od_sentence_ids,omitempty"`
	Warnings             []string                    `json:"warnings,omitempty"`
}

type StructuredExample struct {
	ID                   string        `json:"id"`
	Type                 string        `json:"type"`
	Title                string        `json:"title"`
	OriginSpans          []OriginSpan  `json:"origin_spans"`
	SourceUnits          []string      `json:"source_units"`
	Authority            string        `json:"authority"`
	MappingStatus        string        `json:"mapping_status"`
	RawContent           string        `json:"raw_content"`
	ParsedFields         []ParsedField `json:"parsed_fields"`
	OpenReviewCandidates []string      `json:"open_review_candidates"`
}

type ParsedField struct {
	Path         string   `json:"path"`
	ObservedType string   `json:"observed_type"`
	SampleValues []string `json:"sample_values"`
	MappedTo     string   `json:"mapped_to,omitempty"`
}

type RequirementAtom struct {
	ID                   string   `json:"id"`
	Statement            string   `json:"statement"`
	Subject              string   `json:"subject,omitempty"`
	Predicate            string   `json:"predicate,omitempty"`
	Object               string   `json:"object,omitempty"`
	Quantifier           string   `json:"quantifier,omitempty"`
	Condition            string   `json:"condition,omitempty"`
	TemporalSemantics    string   `json:"temporal_semantics,omitempty"`
	Ownership            string   `json:"ownership,omitempty"`
	AtomType             string   `json:"atom_type"`
	ModelingRelevance    string   `json:"modeling_relevance"`
	SourceUnits          []string `json:"source_units"`
	FunctionalArea       string   `json:"functional_area,omitempty"`
	FunctionalPattern    string   `json:"functional_pattern,omitempty"`
	SupportLevel         string   `json:"support_level"`
	Confidence           string   `json:"confidence"`
	ReviewStatus         string   `json:"review_status"`
	ModelingOutcome      string   `json:"modeling_outcome"`
	ModelImpactPreview   []string `json:"model_impact_preview"`
	OpenReviewCandidates []string `json:"open_review_candidates"`
}

type FunctionalArea struct {
	ID                   string   `json:"id"`
	Label                string   `json:"label"`
	Purpose              string   `json:"purpose"`
	MainActors           []string `json:"main_actors"`
	RequirementAtoms     []string `json:"requirement_atoms"`
	ModelingFocus        []string `json:"modeling_focus"`
	OpenReviewCandidates []string `json:"open_review_candidates"`
}

type ActorSummary struct {
	ID                   string   `json:"id"`
	Label                string   `json:"label"`
	Kind                 string   `json:"kind"`
	OperationCount       int      `json:"operation_count"`
	FunctionalAreas      []string `json:"functional_areas"`
	MapsToUserRole       bool     `json:"maps_to_user_role"`
	OpenReviewCandidates []string `json:"open_review_candidates"`
}

type CrudOperation struct {
	ID                   string   `json:"id"`
	Label                string   `json:"label"`
	ActorID              string   `json:"actor_id"`
	FunctionalAreaID     string   `json:"functional_area_id"`
	Creates              []string `json:"creates"`
	Reads                []string `json:"reads"`
	Updates              []string `json:"updates"`
	Deletes              []string `json:"deletes"`
	PersistentData       []string `json:"persistent_data"`
	Outcome              string   `json:"outcome"`
	RequirementAtoms     []string `json:"requirement_atoms"`
	SourceUnits          []string `json:"source_units"`
	ReviewStatus         string   `json:"review_status"`
	OpenReviewCandidates []string `json:"open_review_candidates"`
}

type ReviewCandidate struct {
	ID                       string         `json:"id"`
	DecisionKey              string         `json:"decision_key,omitempty"`
	Question                 string         `json:"question"`
	Description              string         `json:"description"`
	Status                   string         `json:"status"`
	AffectedAtoms            []string       `json:"affected_atoms"`
	DependsOn                []string       `json:"depends_on"`
	MayAffect                []string       `json:"may_affect"`
	Options                  []ReviewOption `json:"options"`
	SelectedOption           string         `json:"selected_option,omitempty"`
	RecommendedID            string         `json:"recommended_option_id,omitempty"`
	Category                 string         `json:"category,omitempty"`
	Phase                    string         `json:"phase,omitempty"`
	Severity                 string         `json:"severity,omitempty"`
	Blocking                 bool           `json:"blocking"`
	AffectedSourceUnits      []string       `json:"affected_source_units,omitempty"`
	AffectedFunctionalAreas  []string       `json:"affected_functional_areas,omitempty"`
	AffectedOperations       []string       `json:"affected_operations,omitempty"`
	AffectedModelCandidates  []string       `json:"affected_model_candidates,omitempty"`
	RecommendationConfidence string         `json:"recommendation_confidence,omitempty"`
	Warnings                 []string       `json:"warnings,omitempty"`
}

type ReviewOption struct {
	ID                    string                           `json:"id"`
	Label                 string                           `json:"label"`
	Recommended           bool                             `json:"recommended"`
	Rationale             string                           `json:"rationale"`
	EffectSummary         string                           `json:"effect_summary,omitempty"`
	Benefits              []string                         `json:"benefits,omitempty"`
	Risks                 []string                         `json:"risks,omitempty"`
	AffectedArtifactKinds []string                         `json:"affected_artifact_kinds,omitempty"`
	Effects               *llmpipeline.ReviewOptionEffects `json:"effects,omitempty"`
}

type ReviewDecision struct {
	ID                string    `json:"id"`
	Question          string    `json:"question"`
	AffectedAtoms     []string  `json:"affected_atoms"`
	SelectedOption    string    `json:"selected_option"`
	Status            string    `json:"status"`
	Rationale         string    `json:"rationale,omitempty"`
	ReviewedBy        string    `json:"reviewed_by,omitempty"`
	ReviewedAt        time.Time `json:"reviewed_at,omitempty"`
	ProjectRevision   int       `json:"project_revision,omitempty"`
	AffectedArtifacts []string  `json:"affected_artifacts,omitempty"`
	AppliedPatchID    string    `json:"applied_patch_id,omitempty"`
	ApplyStatus       string    `json:"apply_status,omitempty"`
	NewCandidateIDs   []string  `json:"new_candidate_ids,omitempty"`
	DecisionMode      string    `json:"decision_mode,omitempty"`
	PolicyVersion     string    `json:"policy_version,omitempty"`
	ActiveReviewMS    int64     `json:"active_review_ms,omitempty"`
}

func NewStore(root string) (*Store, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	root = absRoot
	modelPath := filepath.Join(root, "poc", "printing_house_full", "v0.5_granularity_sentance", "db_model.dsl.yaml")
	taskPath := filepath.Join(root, "poc", "printing_house_full", "v0.5_granularity_sentance", "TASK_FULL.md")
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("canonical model fixture not found: %w", err)
	}
	now := time.Now()
	store := &Store{
		root:              root,
		statePath:         filepath.Join(root, ".dbdsl_workbench", "state.json"),
		projects:          map[string]*ProjectState{},
		deletedProjectIDs: map[string]bool{},
	}
	store.projects[CanonicalProjectID] = canonicalProjectState(modelPath, taskPath, now)
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
		OpenReviewIDs:      map[string]bool{},
		AnsweredReviews:    map[string]string{},
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
		bundle, err := dsl.LoadV05Bundle(path)
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
	bundle, err := dsl.LoadV05Bundle(modelPath)
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
		LastActivity:       "Imported v0.5 bundle from " + s.relativePath(filepath.Dir(modelPath)) + ".",
		AnalysisReady:      true,
		ModelGenerated:     true,
		DBMLReady:          true,
		ModelPath:          modelPath,
		TaskPath:           taskPath,
		BundlePath:         filepath.Dir(modelPath),
		SourceManifestPath: s.sourceManifestRel(id),
		Imported:           true,
		OpenReviewIDs:      map[string]bool{},
		AnsweredReviews:    map[string]string{},
		AcceptedQuality:    map[string]bool{},
		Resources: []InputResource{{
			ID:                   fmt.Sprintf("R-%03d", s.nextResource),
			Kind:                 "uploaded_file",
			FileType:             fileType,
			Title:                "Imported v0.5 bundle",
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
	bundle, err := dsl.LoadV05Bundle(result.ModelPath)
	if err != nil {
		return BundleCandidate{}, err
	}
	return s.bundleCandidate(result.ModelPath, bundle), nil
}

func (s *Store) LLMPlanBundleFromText(ctx context.Context, client llm.Client, opts LLMPlanFromTextOptions) (BundleCandidate, error) {
	if client == nil {
		return BundleCandidate{}, errors.New("LLM client is required")
	}
	content := strings.TrimSpace(opts.Content)
	if content == "" {
		return BundleCandidate{}, errors.New("task content is required")
	}
	title := nonEmpty(opts.Name, "LLM generated PIA task")
	base := outputSlug(title)
	bundleBase := base
	if !strings.HasSuffix(bundleBase, "_llm") {
		bundleBase += "_llm"
	}
	outDir := s.nextGeneratedBundleDir(bundleBase)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return BundleCandidate{}, fmt.Errorf("create LLM output directory: %w", err)
	}
	taskPath := filepath.Join(outDir, "TASK.md")
	if err := os.WriteFile(taskPath, []byte(content+"\n"), 0o644); err != nil {
		return BundleCandidate{}, fmt.Errorf("write LLM task input: %w", err)
	}
	result, err := llmpipeline.RunPlan(ctx, client, llmpipeline.PlanOptions{
		TaskPath:          taskPath,
		OutDir:            outDir,
		ModelID:           base + "_v05",
		Name:              title,
		Model:             opts.Model,
		ReasoningEffort:   opts.ReasoningEffort,
		MaxOutputTokens:   opts.MaxOutputTokens,
		MaxRepairAttempts: opts.MaxRepairAttempts,
	})
	if err != nil {
		return BundleCandidate{}, err
	}
	bundle, err := dsl.LoadV05Bundle(result.ModelPath)
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

func (s *Store) ApplyJobResult(projectID, jobType string) (int, []string, error) {
	var updated []string
	err := s.withProject(projectID, 0, func(project *ProjectState) error {
		switch jobType {
		case "process_sources":
			return errors.New("process_sources requires an explicit production runner")
		case "apply_review_decision":
			project.LastActivity = "Review decision applied and analysis refreshed."
			if len(project.OpenReviewIDs) == 0 {
				project.LifecycleStatus = "ready_for_model_generation"
			}
			updated = []string{"review_candidates", "review_decisions", "requirements", "functional_crud"}
		case "generate_model":
			if len(project.OpenReviewIDs) > 0 {
				return errors.New("project has open review questions")
			}
			project.ModelGenerated = true
			project.DBMLReady = true
			project.LifecycleStatus = "ready_for_dbml"
			project.LastActivity = "Database model generated."
			updated = []string{"model", "model_graph", "trace_index", "quality", "dbml"}
		case "run_quality":
			project.LastActivity = "Quality checks refreshed."
			updated = []string{"quality"}
		case "regenerate_dbml":
			if !project.ModelGenerated {
				return errors.New("model is not generated")
			}
			project.DBMLReady = true
			project.LifecycleStatus = "ready_for_dbml"
			project.LastActivity = "DBML regenerated."
			updated = []string{"dbml", "exports"}
		default:
			updated = []string{jobType}
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	project, _ := s.Project(projectID)
	return project.CurrentRevision, updated, nil
}

func (s *Store) AnswerReview(projectID, reviewID, selectedOption string, baseRevision int) (int, error) {
	err := s.withProject(projectID, baseRevision, func(project *ProjectState) error {
		if !project.OpenReviewIDs[reviewID] {
			return ErrNotFound
		}
		delete(project.OpenReviewIDs, reviewID)
		project.AnsweredReviews[reviewID] = selectedOption
		project.LastActivity = "Review question answered."
		if len(project.OpenReviewIDs) == 0 {
			project.LifecycleStatus = "ready_for_model_generation"
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	project, _ := s.Project(projectID)
	return project.CurrentRevision, nil
}

func (s *Store) ApplyRecommended(projectID string, baseRevision int) (int, error) {
	err := s.withProject(projectID, baseRevision, func(project *ProjectState) error {
		for _, candidate := range demoReviewCandidates() {
			if project.OpenReviewIDs[candidate.ID] {
				project.AnsweredReviews[candidate.ID] = candidate.RecommendedID
				delete(project.OpenReviewIDs, candidate.ID)
			}
		}
		project.LifecycleStatus = "ready_for_model_generation"
		project.LastActivity = "Recommended review answers applied."
		return nil
	})
	if err != nil {
		return 0, err
	}
	project, _ := s.Project(projectID)
	return project.CurrentRevision, nil
}

func (s *Store) CompleteProject(projectID string, baseRevision int) (ProjectSummary, CompletedSnapshot, error) {
	var snapshot CompletedSnapshot
	err := s.withProject(projectID, baseRevision, func(project *ProjectState) error {
		if len(project.OpenReviewIDs) > 0 {
			return errors.New("project has open review questions")
		}
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
	reopened.AnalysisReady = source.AnalysisReady
	reopened.ModelGenerated = source.ModelGenerated
	reopened.DBMLReady = false
	reopened.Completed = false
	reopened.FinalModelAccepted = false
	reopened.ModelPath = source.ModelPath
	reopened.TaskPath = source.TaskPath
	reopened.BundlePath = source.BundlePath
	reopened.CombinedDocumentPath = source.CombinedDocumentPath
	reopened.CombinedDocumentLineagePath = source.CombinedDocumentLineagePath
	reopened.SourceSegmentsPath = source.SourceSegmentsPath
	reopened.SourceFidelityReportPath = source.SourceFidelityReportPath
	reopened.SourceSegmentationProposalPath = source.SourceSegmentationProposalPath
	reopened.SourceSegmentationQAPath = source.SourceSegmentationQAPath
	reopened.CombinedDocumentReady = source.CombinedDocumentReady
	reopened.SourceUnitsProposalPath = source.SourceUnitsProposalPath
	reopened.SourceUnitsPath = source.SourceUnitsPath
	reopened.SourceUnitQAPath = source.SourceUnitQAPath
	reopened.RequirementAtomsProposalPath = source.RequirementAtomsProposalPath
	reopened.RequirementAtomsPath = source.RequirementAtomsPath
	reopened.RequirementAtomQAPath = source.RequirementAtomQAPath
	reopened.FunctionalAnalysisProposalPath = source.FunctionalAnalysisProposalPath
	reopened.FunctionalDecompositionPath = source.FunctionalDecompositionPath
	reopened.FunctionalAnalysisQAPath = source.FunctionalAnalysisQAPath
	reopened.CRUDMappingProposalPath = source.CRUDMappingProposalPath
	reopened.CRUDMatrixPath = source.CRUDMatrixPath
	reopened.CRUDMappingQAPath = source.CRUDMappingQAPath
	reopened.ReviewCandidatesProposalPath = source.ReviewCandidatesProposalPath
	reopened.ReviewCandidatesPath = source.ReviewCandidatesPath
	reopened.ReviewCandidateQAPath = source.ReviewCandidateQAPath
	reopened.ReviewDecisionsPath = source.ReviewDecisionsPath
	reopened.LastAppliedPatchPath = source.LastAppliedPatchPath
	reopened.ConceptualModelProposalPath = source.ConceptualModelProposalPath
	reopened.ConceptualModelAcceptedPath = source.ConceptualModelAcceptedPath
	reopened.ConceptualModelQAPath = source.ConceptualModelQAPath
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
	reopened.OpenReviewIDs = map[string]bool{}
	reopened.AnsweredReviews = map[string]string{}
	for id, selected := range source.AnsweredReviews {
		reopened.AnsweredReviews[id] = selected
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
	var bundle *dsl.V05Bundle
	if project.ModelPath != "" {
		loaded, err := s.bundleForProject(project)
		if err != nil {
			return ProjectSummary{}, err
		}
		bundle = loaded
	}
	report := qualityForProject(project)
	decisionIDs := map[string]bool{}
	for candidateID := range project.AnsweredReviews {
		decisionID := strings.Replace(candidateID, "RC-", "RD-", 1)
		if decisionID == candidateID {
			decisionID = "RD-" + candidateID
		}
		decisionIDs[decisionID] = true
	}
	counts := ProjectCounts{
		Resources:           len(project.Resources),
		CombinedSentences:   s.combinedSentenceCount(project),
		OpenReviewQuestions: len(project.OpenReviewIDs),
	}
	if bundle != nil {
		counts.SourceUnits = boolCount(project.AnalysisReady, len(bundle.SourceUnits.SourceUnits))
		counts.Examples = boolCount(project.AnalysisReady, len(s.Examples(project.ID)))
		counts.Requirements = boolCount(project.AnalysisReady, len(bundle.RequirementAtoms.RequirementAtoms))
		counts.FunctionalAreas = boolCount(project.AnalysisReady, len(bundle.FunctionalDecomposition.FunctionalAreas))
		counts.Operations = boolCount(project.AnalysisReady, len(bundle.CRUDMatrix.Operations))
		for _, decision := range bundle.ReviewDecisions.ReviewDecisions {
			decisionIDs[decision.ID] = true
		}
		counts.Entities = boolCount(project.ModelGenerated, len(bundle.Document.Entities))
		counts.Relationships = boolCount(project.ModelGenerated, len(bundle.Document.Relationships))
	} else {
		if artifacts, err := s.SourceUnitArtifacts(project.ID); err == nil {
			counts.SourceUnits = len(artifacts.Accepted.SourceUnits)
		}
		if items, _, found, err := s.projectRequirementAtoms(project.ID); err == nil && found {
			counts.Requirements = len(items)
		}
		if areas, _, found, err := s.projectFunctionalAreas(project.ID); err == nil && found {
			counts.FunctionalAreas = len(areas)
		}
		if operations, found, err := s.projectCRUDOperations(project.ID); err == nil && found {
			counts.Operations = len(operations)
		}
	}
	counts.ReviewDecisions = len(decisionIDs)
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
	analysisStatus := "not_started"
	if project.AnalysisReady {
		analysisStatus = "ready"
	}
	if !project.AnalysisReady && project.CombinedDocumentReady {
		analysisStatus = "sources_ready"
	}
	if len(project.OpenReviewIDs) > 0 {
		analysisStatus = "needs_attention"
	}
	sourceUnitsStatus := s.artifactStatus(project.SourceUnitsPath)
	canExtractRequirements := false
	if sourceUnitsStatus == "ready" {
		if artifacts, err := s.SourceUnitArtifacts(project.ID); err == nil {
			canExtractRequirements = artifacts.QA.OK && len(artifacts.QA.NeedsAttention) == 0
			if len(artifacts.QA.NeedsAttention) > 0 {
				sourceUnitsStatus = "needs_attention"
			}
		}
	}
	modelStatus := "not_generated"
	if project.ModelGenerated {
		if len(project.OpenReviewIDs) > 0 {
			modelStatus = "outdated"
		} else if s.artifactStatus(project.ModelPath) != "ready" || report.Summary.ValidationErrors > 0 {
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
	semanticStatus := "not_generated"
	semanticBlocking := 0
	if project.SemanticVerificationPath != "" {
		if semantic, err := s.SemanticVerification(project.ID); err == nil {
			semanticBlocking = semantic.BlockingIssues
			if semantic.OK {
				semanticStatus = "passed"
			} else {
				semanticStatus = "blocked"
			}
		} else {
			semanticStatus = "outdated"
		}
	}
	if semanticStatus == "not_generated" && project.ModelGenerated && project.DesignObligationsPath == "" {
		if bundle, err := dsl.LoadV05Bundle(project.ModelPath); err == nil && bundle.Document.Source.PipelineVersion != "0.7" {
			semanticStatus = "legacy_not_applicable"
		}
	}
	fidelityStatus := s.artifactStatus(project.SourceFidelityReportPath)
	if fidelityStatus == "ready" {
		if fidelity, err := s.SourceFidelity(project.ID); err != nil || !fidelity.OK {
			fidelityStatus = "needs_attention"
		}
	}
	segmentationStatus := "not_generated"
	if project.SourceSegmentationProposalPath != "" && project.SourceSegmentationQAPath != "" {
		segmentationStatus = "ready"
		if proposal, qa, err := s.SourceSegmentation(project.ID); err != nil || !qa.OK {
			segmentationStatus = "not_generated"
		} else if proposal.FallbackUsed {
			segmentationStatus = "fallback"
		}
	} else if s.artifactStatus(project.CombinedDocumentPath) == "ready" {
		segmentationStatus = "ready"
	}
	semanticSatisfied := semanticStatus == "passed"
	legacyCompleted := semanticStatus == "legacy_not_applicable" && project.FinalModelAccepted && project.DBMLReady
	return ArtifactHealth{
		AnalysisStatus:             analysisStatus,
		SourceManifestStatus:       s.artifactStatus(project.SourceManifestPath),
		CombinedDocumentStatus:     s.artifactStatus(project.CombinedDocumentPath),
		SourceFidelityStatus:       fidelityStatus,
		SourceSegmentationStatus:   segmentationStatus,
		SourceUnitsStatus:          sourceUnitsStatus,
		RequirementAtomsStatus:     s.artifactStatus(project.RequirementAtomsPath),
		DesignObligationsStatus:    s.artifactStatus(project.DesignObligationsPath),
		FunctionalAnalysisStatus:   s.artifactStatus(project.FunctionalDecompositionPath),
		CRUDMappingStatus:          s.artifactStatus(project.CRUDMatrixPath),
		ReviewCandidatesStatus:     s.artifactStatus(project.ReviewCandidatesPath),
		ConceptualModelStatus:      conceptualStatus,
		ModelStatus:                modelStatus,
		SemanticVerificationStatus: semanticStatus,
		SemanticBlockingIssues:     semanticBlocking,
		DBMLStatus:                 dbmlStatus,
		OpenReviewQuestions:        len(project.OpenReviewIDs),
		CanGenerateModel:           project.AnalysisReady && len(project.OpenReviewIDs) == 0,
		CanContinueToDBML:          modelValid && (semanticSatisfied || legacyCompleted) && project.FinalModelAccepted,
		CanCompleteProject:         project.DBMLReady && modelValid && (semanticSatisfied || legacyCompleted) && project.FinalModelAccepted && len(project.OpenReviewIDs) == 0 && dbmlStatus == "ready",
		CanGenerateSourceUnits:     s.artifactStatus(project.CombinedDocumentPath) == "ready" && fidelityStatus == "ready" && (segmentationStatus == "ready" || segmentationStatus == "fallback"),
		CanExtractRequirements:     canExtractRequirements,
		CanBuildFunctionalAnalysis: s.artifactStatus(project.RequirementAtomsPath) == "ready" && s.artifactStatus(project.DesignObligationsPath) == "ready",
		CanBuildCRUDMapping:        s.artifactStatus(project.FunctionalDecompositionPath) == "ready",
		CanProposeReviewCandidates: s.artifactStatus(project.CRUDMatrixPath) == "ready",
		CanProjectLogicalModel:     s.artifactStatus(project.ConceptualModelAcceptedPath) == "ready" && len(project.OpenReviewIDs) == 0,
		CanGenerateOutputs:         project.FinalModelAccepted && modelValid && semanticStatus == "passed",
		FinalModelAccepted:         project.FinalModelAccepted,
	}
}

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
	if health.OpenReviewQuestions > 0 {
		return "analysis_review"
	}
	if health.CRUDMappingStatus == "ready" {
		return "ready_for_model_generation"
	}
	if health.SourceUnitsStatus == "needs_attention" {
		return "source_review"
	}
	if health.SourceUnitsStatus == "ready" {
		return "sources_processed"
	}
	if health.CombinedDocumentStatus == "ready" {
		return "sources_processed"
	}
	return "intake"
}

func (s *Store) Bundle(projectID string) (*dsl.V05Bundle, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	return s.bundleForProject(project)
}

func (s *Store) TaskText(projectID string) string {
	project, ok := s.Project(projectID)
	if !ok || project.TaskPath == "" {
		return ""
	}
	data, err := os.ReadFile(project.TaskPath)
	if err != nil {
		return ""
	}
	return string(data)
}

func (s *Store) SourceUnits(projectID string) ([]SourceUnit, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	if units, found, err := s.projectSourceUnits(projectID); found || err != nil {
		return units, err
	}
	if !project.AnalysisReady {
		return nil, nil
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return nil, err
	}
	reqBySource := map[string][]string{}
	for _, atom := range bundle.RequirementAtoms.RequirementAtoms {
		for _, sourceID := range atom.SourceUnits {
			reqBySource[sourceID] = append(reqBySource[sourceID], atom.ID)
		}
	}
	candidateBySource := s.openCandidateBySource(project, bundle)
	units := make([]SourceUnit, 0, len(bundle.SourceUnits.SourceUnits))
	for _, source := range bundle.SourceUnits.SourceUnits {
		normalized := source.Text.Normalized
		normalization := source.Text.Normalization
		if normalization.Version == "" {
			normalized, normalization = llmpipeline.NormalizeSourceText(source.Text.Exact)
		}
		status := "reviewed"
		if len(candidateBySource[source.ID]) > 0 {
			status = "open_review"
		} else if source.Relevance == "model_supporting" {
			status = "needs_attention"
		}
		units = append(units, SourceUnit{
			ID:                   source.ID,
			Kind:                 source.Kind,
			Section:              source.Section,
			NormalizedText:       normalized,
			Normalization:        normalization,
			ExactText:            source.Text.Exact,
			Relevance:            source.Relevance,
			Confidence:           confidenceFromRelevance(source.Relevance),
			ReviewStatus:         status,
			OriginSpans:          []OriginSpan{originFromLocation(source.Location)},
			LinkedExamples:       stringSlice(linkedExamplesForSource(source.ID)),
			LinkedRequirements:   stringSlice(reqBySource[source.ID]),
			OpenReviewCandidates: stringSlice(candidateBySource[source.ID]),
		})
	}
	return units, nil
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

func (s *Store) Examples(projectID string) []StructuredExample {
	project, ok := s.Project(projectID)
	if !ok || !project.AnalysisReady || !s.isCanonicalBundle(project.ModelPath) {
		return []StructuredExample{}
	}
	return []StructuredExample{
		{
			ID:                   "EX-001",
			Type:                 "json",
			Title:                "Primer stamparije iz import fajla",
			OriginSpans:          []OriginSpan{{ResourceID: "R-001", Label: "TASK_FULL.md · import examples"}},
			SourceUnits:          []string{"PHF-GSU-112", "PHF-GSU-113"},
			Authority:            "normative",
			MappingStatus:        "mapped",
			RawContent:           `{"stamparijaId":"BGD01","nazivStamparije":"Print Studio","pib":"123456789"}`,
			OpenReviewCandidates: []string{},
			ParsedFields: []ParsedField{
				{Path: "$.stamparijaId", ObservedType: "string", SampleValues: []string{"BGD01"}, MappedTo: "PrintShop.external_code"},
				{Path: "$.nazivStamparije", ObservedType: "string", SampleValues: []string{"Print Studio"}, MappedTo: "Institution.name"},
				{Path: "$.pib", ObservedType: "string", SampleValues: []string{"123456789"}, MappedTo: "Institution.tax_id"},
			},
		},
		{
			ID:                   "EX-002",
			Type:                 "csv",
			Title:                "Cenovnik usluga stampe",
			OriginSpans:          []OriginSpan{{ResourceID: "R-001", Label: "TASK_FULL.md · catalog section"}},
			SourceUnits:          []string{"PHF-GSU-080", "PHF-GSU-081"},
			Authority:            "illustrative",
			MappingStatus:        "partially_mapped",
			RawContent:           "product,service,price\nposter,color_print,1200",
			OpenReviewCandidates: []string{},
			ParsedFields: []ParsedField{
				{Path: "product", ObservedType: "string", SampleValues: []string{"poster"}, MappedTo: "Product.name"},
				{Path: "service", ObservedType: "string", SampleValues: []string{"color_print"}, MappedTo: "PrintService.name"},
				{Path: "price", ObservedType: "number", SampleValues: []string{"1200"}, MappedTo: "ProductPrintService.price"},
			},
		},
	}
}

func (s *Store) Requirements(projectID string) ([]RequirementAtom, map[string]int, error) {
	if items, coverage, found, err := s.projectRequirementAtoms(projectID); found || err != nil {
		return items, coverage, err
	}
	project, ok := s.Project(projectID)
	if !ok {
		return nil, nil, ErrNotFound
	}
	if !project.AnalysisReady {
		return nil, map[string]int{}, nil
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return nil, nil, err
	}
	candidateByAtom := s.openCandidateByAtom(project)
	requirements := make([]RequirementAtom, 0, len(bundle.RequirementAtoms.RequirementAtoms))
	coverage := map[string]int{
		"source_units_total":          len(bundle.SourceUnits.SourceUnits),
		"source_units_covered":        0,
		"requirements_without_source": 0,
		"requirements_needing_review": 0,
		"non_model_requirements":      0,
		"direct_db_requirements":      0,
	}
	covered := map[string]bool{}
	for _, atom := range bundle.RequirementAtoms.RequirementAtoms {
		for _, sourceID := range atom.SourceUnits {
			covered[sourceID] = true
		}
		if len(atom.SourceUnits) == 0 {
			coverage["requirements_without_source"]++
		}
		if atom.ModelingRelevance == "non_model" {
			coverage["non_model_requirements"]++
		}
		if atom.ModelingRelevance == "direct_db" {
			coverage["direct_db_requirements"]++
		}
		reviewStatus := "reviewed"
		if len(candidateByAtom[atom.ID]) > 0 {
			reviewStatus = "open_review"
			coverage["requirements_needing_review"]++
		} else if atom.RequiresReview {
			reviewStatus = "needs_review"
		}
		requirements = append(requirements, RequirementAtom{
			ID:                   atom.ID,
			Statement:            atom.Statement,
			Subject:              atom.Subject,
			Predicate:            atom.Predicate,
			Object:               atom.Object,
			Quantifier:           atom.Quantifier,
			Condition:            atom.Condition,
			TemporalSemantics:    atom.TemporalSemantics,
			Ownership:            atom.Ownership,
			AtomType:             atom.AtomType,
			ModelingRelevance:    atom.ModelingRelevance,
			SourceUnits:          stringSlice(atom.SourceUnits),
			FunctionalArea:       atom.FunctionalArea,
			FunctionalPattern:    atom.FunctionalPattern,
			SupportLevel:         atom.SupportLevel,
			Confidence:           atom.Confidence,
			ReviewStatus:         reviewStatus,
			ModelingOutcome:      atom.ModelingOutcome.Status,
			ModelImpactPreview:   stringSlice(modelImpacts(atom.ModelImpacts)),
			OpenReviewCandidates: stringSlice(candidateByAtom[atom.ID]),
		})
	}
	coverage["source_units_covered"] = len(covered)
	return requirements, coverage, nil
}

func (s *Store) FunctionalAreas(projectID string) ([]FunctionalArea, error) {
	if items, _, found, err := s.projectFunctionalAreas(projectID); found || err != nil {
		return items, err
	}
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	if !project.AnalysisReady {
		return nil, nil
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return nil, err
	}
	candidateByAtom := s.openCandidateByAtom(project)
	var out []FunctionalArea
	for _, area := range bundle.FunctionalDecomposition.FunctionalAreas {
		open := collectCandidates(area.Atoms, candidateByAtom)
		out = append(out, FunctionalArea{
			ID:                   area.ID,
			Label:                area.Label,
			Purpose:              area.Purpose,
			MainActors:           stringSlice(area.MainActors),
			RequirementAtoms:     stringSlice(area.Atoms),
			ModelingFocus:        stringSlice(area.ModelingFocus),
			OpenReviewCandidates: stringSlice(open),
		})
	}
	return out, nil
}

func (s *Store) Actors(projectID string) ([]ActorSummary, error) {
	if _, items, found, err := s.projectFunctionalAreas(projectID); found || err != nil {
		return items, err
	}
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	if !project.AnalysisReady {
		return nil, nil
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return nil, err
	}
	opCount := map[string]int{}
	areas := map[string]map[string]bool{}
	for _, op := range bundle.CRUDMatrix.Operations {
		opCount[op.Actor]++
		if areas[op.Actor] == nil {
			areas[op.Actor] = map[string]bool{}
		}
		areas[op.Actor][op.FunctionalArea] = true
	}
	var out []ActorSummary
	for _, actor := range bundle.CRUDMatrix.Actors {
		out = append(out, ActorSummary{
			ID:                   actor.ID,
			Label:                actor.Label,
			Kind:                 actorKind(actor.ID),
			OperationCount:       opCount[actor.ID],
			FunctionalAreas:      sortedKeys(areas[actor.ID]),
			MapsToUserRole:       actor.ID != "system" && actor.ID != "external_payment_provider",
			OpenReviewCandidates: []string{},
		})
	}
	return out, nil
}

func (s *Store) CrudOperations(projectID string) ([]CrudOperation, error) {
	if items, found, err := s.projectCRUDOperations(projectID); found || err != nil {
		return items, err
	}
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	if !project.AnalysisReady {
		return nil, nil
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return nil, err
	}
	actions := map[string]map[string][]string{}
	for _, row := range bundle.CRUDMatrix.Matrix {
		for opID, opActions := range row.Operations {
			if actions[opID] == nil {
				actions[opID] = map[string][]string{"C": {}, "R": {}, "U": {}, "D": {}}
			}
			for _, action := range opActions {
				actions[opID][action] = append(actions[opID][action], row.Entity)
			}
		}
	}
	candidateByAtom := s.openCandidateByAtom(project)
	var out []CrudOperation
	for _, op := range bundle.CRUDMatrix.Operations {
		open := collectCandidates(op.SourceAtoms, candidateByAtom)
		reviewStatus := "reviewed"
		if len(open) > 0 {
			reviewStatus = "open_review"
		}
		persistent := union(actions[op.ID]["C"], actions[op.ID]["R"], actions[op.ID]["U"], actions[op.ID]["D"])
		out = append(out, CrudOperation{
			ID:                   op.ID,
			Label:                op.Label,
			ActorID:              op.Actor,
			FunctionalAreaID:     op.FunctionalArea,
			Creates:              stringSlice(actions[op.ID]["C"]),
			Reads:                stringSlice(actions[op.ID]["R"]),
			Updates:              stringSlice(actions[op.ID]["U"]),
			Deletes:              stringSlice(actions[op.ID]["D"]),
			PersistentData:       stringSlice(persistent),
			Outcome:              crudOutcome(persistent, actions[op.ID]),
			RequirementAtoms:     stringSlice(op.SourceAtoms),
			SourceUnits:          stringSlice(op.SourceUnits),
			ReviewStatus:         reviewStatus,
			OpenReviewCandidates: stringSlice(open),
		})
	}
	return out, nil
}

func (s *Store) ReviewCandidates(projectID string) ([]ReviewCandidate, error) {
	if items, found, err := s.projectReviewCandidates(projectID); found || err != nil {
		return items, err
	}
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	if !s.isCanonicalBundle(project.ModelPath) {
		return []ReviewCandidate{}, nil
	}
	var out []ReviewCandidate
	for _, candidate := range demoReviewCandidates() {
		if selected, ok := project.AnsweredReviews[candidate.ID]; ok {
			candidate.Status = "answered"
			candidate.SelectedOption = selected
		} else if project.OpenReviewIDs[candidate.ID] {
			candidate.Status = "open"
		} else {
			candidate.Status = "resolved"
		}
		out = append(out, candidate)
	}
	return out, nil
}

func (s *Store) ReviewDecisions(projectID string) ([]ReviewDecision, error) {
	if items, found, err := s.projectReviewDecisions(projectID); found || err != nil {
		return items, err
	}
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	bundle, err := s.bundleForProject(project)
	if err != nil {
		return nil, err
	}
	var out []ReviewDecision
	for _, decision := range bundle.ReviewDecisions.ReviewDecisions {
		status, _ := decision.Decision["status"].(string)
		selected, _ := decision.Decision["selected_option"].(string)
		rationale, _ := decision.Decision["rationale"].(string)
		out = append(out, ReviewDecision{
			ID:             decision.ID,
			Question:       decision.Question,
			AffectedAtoms:  decision.AffectedAtoms,
			SelectedOption: selected,
			Status:         status,
			Rationale:      rationale,
		})
	}
	for id, selected := range project.AnsweredReviews {
		candidate := candidateByID(id)
		out = append(out, ReviewDecision{
			ID:             strings.Replace(id, "RC", "RD", 1),
			Question:       candidate.Question,
			AffectedAtoms:  candidate.AffectedAtoms,
			SelectedOption: selected,
			Status:         "accepted",
			Rationale:      "User selected an answer in the web workbench.",
			ReviewedBy:     "web_user",
			ReviewedAt:     project.UpdatedAt,
		})
	}
	return out, nil
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
		"TASK.md":                               s.readArtifact(project.TaskPath),
		"source_manifest.yaml":                  sourceManifest,
		"source_segments.json":                  s.readArtifact(project.SourceSegmentsPath),
		"source_fidelity_report.json":           s.readArtifact(project.SourceFidelityReportPath),
		"source_segmentation.proposed.json":     s.readArtifact(project.SourceSegmentationProposalPath),
		"source_segmentation_qa.json":           s.readArtifact(project.SourceSegmentationQAPath),
		"combined_document.md":                  s.readArtifact(project.CombinedDocumentPath),
		"combined_document_lineage.json":        s.readArtifact(project.CombinedDocumentLineagePath),
		"source_units.proposed.json":            s.readArtifact(project.SourceUnitsProposalPath),
		"model.dbml":                            dbml,
		"traceability_report.md":                report,
		"db_model.dsl.yaml":                     readString(project.ModelPath),
		"source_units.yaml":                     nonEmpty(readString(filepath.Join(bundleDir, "source_units.yaml")), s.readArtifact(project.SourceUnitsPath)),
		"source_unit_qa.json":                   s.readArtifact(project.SourceUnitQAPath),
		"requirement_atoms.proposed.json":       s.readArtifact(project.RequirementAtomsProposalPath),
		"requirement_atoms.yaml":                nonEmpty(readString(filepath.Join(bundleDir, "requirement_atoms.yaml")), s.readArtifact(project.RequirementAtomsPath)),
		"requirement_atom_qa.json":              s.readArtifact(project.RequirementAtomQAPath),
		"design_obligations.proposed.json":      s.readArtifact(project.DesignObligationsProposalPath),
		"design_obligations.yaml":               s.readArtifact(project.DesignObligationsPath),
		"design_obligation_qa.json":             s.readArtifact(project.DesignObligationQAPath),
		"functional_analysis.proposed.json":     s.readArtifact(project.FunctionalAnalysisProposalPath),
		"functional_decomposition.yaml":         nonEmpty(readString(filepath.Join(bundleDir, "functional_decomposition.yaml")), s.readArtifact(project.FunctionalDecompositionPath)),
		"functional_analysis_qa.json":           s.readArtifact(project.FunctionalAnalysisQAPath),
		"crud_mapping.proposed.json":            s.readArtifact(project.CRUDMappingProposalPath),
		"crud_matrix.yaml":                      nonEmpty(readString(filepath.Join(bundleDir, "crud_matrix.yaml")), s.readArtifact(project.CRUDMatrixPath)),
		"crud_mapping_qa.json":                  s.readArtifact(project.CRUDMappingQAPath),
		"review_candidates.proposed.json":       s.readArtifact(project.ReviewCandidatesProposalPath),
		"review_candidates.yaml":                s.readArtifact(project.ReviewCandidatesPath),
		"review_candidate_qa.json":              s.readArtifact(project.ReviewCandidateQAPath),
		"review_decisions.yaml":                 nonEmpty(readString(filepath.Join(bundleDir, "review_decisions.yaml")), s.readArtifact(project.ReviewDecisionsPath)),
		"review_resolution_patch.proposed.json": s.readArtifact(project.LastAppliedPatchPath),
		"conceptual_model.proposed.json":        s.readArtifact(project.ConceptualModelProposalPath),
		"conceptual_model.accepted.json":        s.readArtifact(project.ConceptualModelAcceptedPath),
		"conceptual_model_qa.json":              s.readArtifact(project.ConceptualModelQAPath),
		"conceptual_model_diff.json":            s.readArtifact(project.ConceptualModelDiffPath),
		"dbdsl_patch.proposed.json":             s.readArtifact(project.LogicalPatchProposalPath),
		"obligation_realizations.json":          s.readArtifact(project.ObligationRealizationsPath),
		"semantic_verification_report.json":     s.readArtifact(project.SemanticVerificationPath),
		"invariant_report.md":                   s.readArtifact(project.InvariantReportPath),
		"validation_report.json":                s.readArtifact(project.ValidationReportPath),
		"lint_report.json":                      s.readArtifact(project.LintReportPath),
		"quality_report.json":                   s.readArtifact(project.QualityReportPath),
		"llm_optimization_report.json":          string(optimizationJSON),
		"llm_optimization_report.md":            optimizationReport.Markdown(),
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
		if content != "" {
			manifestArtifacts[name] = map[string]any{"sha256": sha256Hash([]byte(content)), "bytes": len(content)}
		}
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
	if _, ok := s.projects[CanonicalProjectID]; !ok && !s.deletedProjectIDs[CanonicalProjectID] {
		s.projects[CanonicalProjectID] = canonicalProjectState(s.canonicalModelPath(), s.canonicalTaskPath(), time.Now())
	}

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
		cp.SourceSegmentsPath = s.relativePath(cp.SourceSegmentsPath)
		cp.SourceFidelityReportPath = s.relativePath(cp.SourceFidelityReportPath)
		cp.SourceSegmentationProposalPath = s.relativePath(cp.SourceSegmentationProposalPath)
		cp.SourceSegmentationQAPath = s.relativePath(cp.SourceSegmentationQAPath)
		cp.SourceUnitsProposalPath = s.relativePath(cp.SourceUnitsProposalPath)
		cp.SourceUnitsPath = s.relativePath(cp.SourceUnitsPath)
		cp.SourceUnitQAPath = s.relativePath(cp.SourceUnitQAPath)
		cp.RequirementAtomsProposalPath = s.relativePath(cp.RequirementAtomsProposalPath)
		cp.RequirementAtomsPath = s.relativePath(cp.RequirementAtomsPath)
		cp.RequirementAtomQAPath = s.relativePath(cp.RequirementAtomQAPath)
		cp.FunctionalAnalysisProposalPath = s.relativePath(cp.FunctionalAnalysisProposalPath)
		cp.FunctionalDecompositionPath = s.relativePath(cp.FunctionalDecompositionPath)
		cp.FunctionalAnalysisQAPath = s.relativePath(cp.FunctionalAnalysisQAPath)
		cp.CRUDMappingProposalPath = s.relativePath(cp.CRUDMappingProposalPath)
		cp.CRUDMatrixPath = s.relativePath(cp.CRUDMatrixPath)
		cp.CRUDMappingQAPath = s.relativePath(cp.CRUDMappingQAPath)
		cp.ReviewCandidatesProposalPath = s.relativePath(cp.ReviewCandidatesProposalPath)
		cp.ReviewCandidatesPath = s.relativePath(cp.ReviewCandidatesPath)
		cp.ReviewCandidateQAPath = s.relativePath(cp.ReviewCandidateQAPath)
		cp.ReviewDecisionsPath = s.relativePath(cp.ReviewDecisionsPath)
		cp.LastAppliedPatchPath = s.relativePath(cp.LastAppliedPatchPath)
		cp.ConceptualModelProposalPath = s.relativePath(cp.ConceptualModelProposalPath)
		cp.ConceptualModelAcceptedPath = s.relativePath(cp.ConceptualModelAcceptedPath)
		cp.ConceptualModelQAPath = s.relativePath(cp.ConceptualModelQAPath)
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
	project.SourceSegmentsPath = s.absoluteWorkspacePath(project.SourceSegmentsPath)
	project.SourceFidelityReportPath = s.absoluteWorkspacePath(project.SourceFidelityReportPath)
	project.SourceSegmentationProposalPath = s.absoluteWorkspacePath(project.SourceSegmentationProposalPath)
	project.SourceSegmentationQAPath = s.absoluteWorkspacePath(project.SourceSegmentationQAPath)
	project.SourceUnitsProposalPath = s.absoluteWorkspacePath(project.SourceUnitsProposalPath)
	project.SourceUnitsPath = s.absoluteWorkspacePath(project.SourceUnitsPath)
	project.SourceUnitQAPath = s.absoluteWorkspacePath(project.SourceUnitQAPath)
	project.RequirementAtomsProposalPath = s.absoluteWorkspacePath(project.RequirementAtomsProposalPath)
	project.RequirementAtomsPath = s.absoluteWorkspacePath(project.RequirementAtomsPath)
	project.RequirementAtomQAPath = s.absoluteWorkspacePath(project.RequirementAtomQAPath)
	project.DesignObligationsProposalPath = s.absoluteWorkspacePath(project.DesignObligationsProposalPath)
	project.DesignObligationsPath = s.absoluteWorkspacePath(project.DesignObligationsPath)
	project.DesignObligationQAPath = s.absoluteWorkspacePath(project.DesignObligationQAPath)
	project.FunctionalAnalysisProposalPath = s.absoluteWorkspacePath(project.FunctionalAnalysisProposalPath)
	project.FunctionalDecompositionPath = s.absoluteWorkspacePath(project.FunctionalDecompositionPath)
	project.FunctionalAnalysisQAPath = s.absoluteWorkspacePath(project.FunctionalAnalysisQAPath)
	project.CRUDMappingProposalPath = s.absoluteWorkspacePath(project.CRUDMappingProposalPath)
	project.CRUDMatrixPath = s.absoluteWorkspacePath(project.CRUDMatrixPath)
	project.CRUDMappingQAPath = s.absoluteWorkspacePath(project.CRUDMappingQAPath)
	project.ReviewCandidatesProposalPath = s.absoluteWorkspacePath(project.ReviewCandidatesProposalPath)
	project.ReviewCandidatesPath = s.absoluteWorkspacePath(project.ReviewCandidatesPath)
	project.ReviewCandidateQAPath = s.absoluteWorkspacePath(project.ReviewCandidateQAPath)
	project.ReviewDecisionsPath = s.absoluteWorkspacePath(project.ReviewDecisionsPath)
	project.LastAppliedPatchPath = s.absoluteWorkspacePath(project.LastAppliedPatchPath)
	project.ConceptualModelProposalPath = s.absoluteWorkspacePath(project.ConceptualModelProposalPath)
	project.ConceptualModelAcceptedPath = s.absoluteWorkspacePath(project.ConceptualModelAcceptedPath)
	project.ConceptualModelQAPath = s.absoluteWorkspacePath(project.ConceptualModelQAPath)
	project.ConceptualModelDiffPath = s.absoluteWorkspacePath(project.ConceptualModelDiffPath)
	project.LogicalPatchProposalPath = s.absoluteWorkspacePath(project.LogicalPatchProposalPath)
	project.ObligationRealizationsPath = s.absoluteWorkspacePath(project.ObligationRealizationsPath)
	project.SemanticVerificationPath = s.absoluteWorkspacePath(project.SemanticVerificationPath)
	project.InvariantReportPath = s.absoluteWorkspacePath(project.InvariantReportPath)
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
	if project.OpenReviewIDs == nil {
		project.OpenReviewIDs = map[string]bool{}
	}
	if project.AnsweredReviews == nil {
		project.AnsweredReviews = map[string]string{}
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
	if project.DBMLReady && project.DBMLPath != "" && s.artifactStatus(project.DBMLPath) != "ready" {
		project.DBMLReady = false
	}
	s.refreshArtifactRegistry(project)
}

func (s *Store) refreshArtifactRegistry(project *ProjectState) {
	project.Artifacts = map[string]ArtifactRecord{}
	paths := map[string]string{
		"source_manifest": project.SourceManifestPath, "source_segments": project.SourceSegmentsPath,
		"source_fidelity": project.SourceFidelityReportPath, "combined_document": project.CombinedDocumentPath,
		"source_segmentation_proposed": project.SourceSegmentationProposalPath, "source_segmentation_qa": project.SourceSegmentationQAPath,
		"combined_document_lineage": project.CombinedDocumentLineagePath, "source_units_proposed": project.SourceUnitsProposalPath,
		"source_units_accepted": project.SourceUnitsPath, "source_unit_qa": project.SourceUnitQAPath,
		"requirement_atoms_proposed": project.RequirementAtomsProposalPath, "requirement_atoms_accepted": project.RequirementAtomsPath,
		"requirement_atom_qa": project.RequirementAtomQAPath, "functional_analysis_proposed": project.FunctionalAnalysisProposalPath,
		"design_obligations_proposed": project.DesignObligationsProposalPath, "design_obligations_accepted": project.DesignObligationsPath,
		"design_obligation_qa":              project.DesignObligationQAPath,
		"functional_decomposition_accepted": project.FunctionalDecompositionPath, "functional_analysis_qa": project.FunctionalAnalysisQAPath,
		"crud_mapping_proposed": project.CRUDMappingProposalPath, "crud_matrix_accepted": project.CRUDMatrixPath,
		"crud_mapping_qa": project.CRUDMappingQAPath, "review_candidates_proposed": project.ReviewCandidatesProposalPath,
		"review_candidates_accepted": project.ReviewCandidatesPath, "review_candidate_qa": project.ReviewCandidateQAPath,
		"review_decisions": project.ReviewDecisionsPath, "last_review_patch": project.LastAppliedPatchPath,
		"conceptual_model_proposed": project.ConceptualModelProposalPath, "conceptual_model_accepted": project.ConceptualModelAcceptedPath,
		"conceptual_model_qa": project.ConceptualModelQAPath, "conceptual_model_diff": project.ConceptualModelDiffPath,
		"logical_patch_proposed": project.LogicalPatchProposalPath, "obligation_realizations": project.ObligationRealizationsPath,
		"semantic_verification": project.SemanticVerificationPath, "invariant_report": project.InvariantReportPath,
		"logical_model_accepted": project.ModelPath, "validation_report": project.ValidationReportPath,
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
		candidate := filepath.Join(parent, name, "v0.5")
		if _, err := os.Stat(filepath.Join(candidate, "db_model.dsl.yaml")); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (s *Store) bundleForProject(project *ProjectState) (*dsl.V05Bundle, error) {
	if project.ModelPath == "" {
		return nil, ErrModelNotGenerated
	}
	return dsl.LoadV05Bundle(project.ModelPath)
}

func (s *Store) bundleCandidate(modelPath string, bundle *dsl.V05Bundle) BundleCandidate {
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
		Requirements:     len(bundle.RequirementAtoms.RequirementAtoms),
		FunctionalAreas:  len(bundle.FunctionalDecomposition.FunctionalAreas),
		Operations:       len(bundle.CRUDMatrix.Operations),
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

func (s *Store) canonicalModelPath() string {
	return filepath.Join(s.root, "poc", "printing_house_full", "v0.5_granularity_sentance", "db_model.dsl.yaml")
}

func (s *Store) canonicalTaskPath() string {
	return filepath.Join(s.root, "poc", "printing_house_full", "v0.5_granularity_sentance", "TASK_FULL.md")
}

func (s *Store) isCanonicalBundle(modelPath string) bool {
	if modelPath == "" {
		return false
	}
	absModel, err := filepath.Abs(modelPath)
	if err != nil {
		return false
	}
	absCanonical, err := filepath.Abs(s.canonicalModelPath())
	if err != nil {
		return false
	}
	return filepath.Clean(absModel) == filepath.Clean(absCanonical)
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

func taskTextPath(bundle *dsl.V05Bundle) string {
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
	cp.OpenReviewIDs = map[string]bool{}
	for key, value := range project.OpenReviewIDs {
		cp.OpenReviewIDs[key] = value
	}
	cp.AnsweredReviews = map[string]string{}
	for key, value := range project.AnsweredReviews {
		cp.AnsweredReviews[key] = value
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

func demoReviewCandidates() []ReviewCandidate {
	return []ReviewCandidate{
		{
			ID:            "PHF-RC-DEMO-001",
			Question:      "Da li korisnicke uloge modelovati kao jednu tabelu naloga ili kao odvojene tabele po tipu korisnika?",
			Description:   "Odluka utice na UserAccount, Institution i tok registracije.",
			Status:        "open",
			AffectedAtoms: []string{"PHF-RA-001", "PHF-RA-006"},
			DependsOn:     []string{},
			MayAffect:     []string{"Requirements", "Functional / CRUD", "Review Queue"},
			RecommendedID: "single_user_account",
			Options: []ReviewOption{
				{ID: "single_user_account", Label: "Jedna tabela naloga sa rolom", Recommended: true, Rationale: "Podrzava zajednicku autentifikaciju i jednostavniji trace."},
				{ID: "separate_role_tables", Label: "Odvojene tabele po ulozi", Rationale: "Jasnije razdvaja profile, ali duplira login podatke."},
			},
		},
		{
			ID:            "PHF-RC-DEMO-002",
			Question:      "Da li statistike administratora treba da budu materijalizovane tabele ili izvedeni pogledi?",
			Description:   "Odluka utice na derived views i DBML finalizaciju.",
			Status:        "open",
			AffectedAtoms: []string{"PHF-RA-007", "PHF-RA-031"},
			DependsOn:     []string{"PHF-RC-DEMO-001"},
			MayAffect:     []string{"Requirements", "Functional / CRUD", "Quality"},
			RecommendedID: "derived_views",
			Options: []ReviewOption{
				{ID: "derived_views", Label: "Izvedeni pogledi", Recommended: true, Rationale: "Grafikoni su agregati nad fakturama i feedback-om."},
				{ID: "materialized_tables", Label: "Materijalizovane tabele", Rationale: "Korisno samo ako se zahteva istorija snapshot-a."},
			},
		},
	}
}

func candidateByID(id string) ReviewCandidate {
	for _, candidate := range demoReviewCandidates() {
		if candidate.ID == id {
			return candidate
		}
	}
	return ReviewCandidate{ID: id}
}

func (s *Store) openCandidateByAtom(project *ProjectState) map[string][]string {
	out := map[string][]string{}
	for _, candidate := range demoReviewCandidates() {
		if !project.OpenReviewIDs[candidate.ID] {
			continue
		}
		for _, atom := range candidate.AffectedAtoms {
			out[atom] = append(out[atom], candidate.ID)
		}
	}
	return out
}

func (s *Store) openCandidateBySource(project *ProjectState, bundle *dsl.V05Bundle) map[string][]string {
	out := map[string][]string{}
	atomSources := map[string][]string{}
	for _, atom := range bundle.RequirementAtoms.RequirementAtoms {
		atomSources[atom.ID] = atom.SourceUnits
	}
	for _, candidate := range demoReviewCandidates() {
		if !project.OpenReviewIDs[candidate.ID] {
			continue
		}
		for _, atom := range candidate.AffectedAtoms {
			for _, sourceID := range atomSources[atom] {
				out[sourceID] = append(out[sourceID], candidate.ID)
			}
		}
	}
	return out
}

func originFromLocation(location string) OriginSpan {
	origin := OriginSpan{ResourceID: "R-001", Label: location}
	if idx := strings.LastIndex(location, "#line-"); idx >= 0 {
		line, err := strconv.Atoi(location[idx+6:])
		if err == nil {
			origin.LineStart = line
			origin.LineEnd = line
		}
	}
	return origin
}

func confidenceFromRelevance(relevance string) string {
	if relevance == "model_relevant" {
		return "high"
	}
	if relevance == "model_supporting" {
		return "medium"
	}
	return "low"
}

func linkedExamplesForSource(sourceID string) []string {
	switch sourceID {
	case "PHF-GSU-112", "PHF-GSU-113":
		return []string{"EX-001"}
	case "PHF-GSU-080", "PHF-GSU-081":
		return []string{"EX-002"}
	default:
		return nil
	}
}

func stringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func modelImpacts(impacts dsl.RequirementModelImpacts) []string {
	var out []string
	out = append(out, prefixAll("table:", impacts.Entities)...)
	out = append(out, prefixAll("field:", impacts.Attributes)...)
	out = append(out, prefixAll("relationship:", impacts.Relationships)...)
	out = append(out, prefixAll("constraint:", impacts.Constraints)...)
	out = append(out, prefixAll("import_spec:", impacts.ImportSpecs)...)
	out = append(out, prefixAll("state_machine:", impacts.StateMachines)...)
	out = append(out, prefixAll("derived_view:", impacts.DerivedViews)...)
	out = append(out, prefixAll("file_spec:", impacts.FileSpecs)...)
	return out
}

func prefixAll(prefix string, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, prefix+value)
	}
	return out
}

func collectCandidates(atomIDs []string, candidateByAtom map[string][]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, atomID := range atomIDs {
		for _, candidateID := range candidateByAtom[atomID] {
			if !seen[candidateID] {
				seen[candidateID] = true
				out = append(out, candidateID)
			}
		}
	}
	sort.Strings(out)
	return out
}

func actorKind(id string) string {
	switch id {
	case "system":
		return "system_process"
	case "external_payment_provider":
		return "external_system"
	case "client", "client_individual", "client_legal", "printer":
		return "domain_party"
	default:
		return "local_user_role"
	}
}

func crudOutcome(persistent []string, actions map[string][]string) string {
	if len(persistent) == 0 {
		return "no_db_impact"
	}
	if len(actions["C"]) > 0 || len(actions["U"]) > 0 || len(actions["D"]) > 0 {
		return "persistent_entity"
	}
	return "report"
}

func union(groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		for _, value := range group {
			if !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	sort.Strings(out)
	return out
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

func canonicalProjectState(modelPath, taskPath string, now time.Time) *ProjectState {
	return &ProjectState{
		ID:              CanonicalProjectID,
		Name:            "Printing House Full",
		Description:     "PIA task specification, v0.5 sentence-granularity evidence bundle.",
		Language:        "sr-Cyrl",
		Domain:          "information_system",
		LifecycleStatus: "analysis_review",
		CurrentRevision: 17,
		CreatedAt:       now.Add(-6 * time.Hour),
		UpdatedAt:       now,
		LastActivity:    "Analysis bundle loaded from v0.5 artifacts.",
		AnalysisReady:   true,
		ModelGenerated:  false,
		DBMLReady:       false,
		ModelPath:       modelPath,
		TaskPath:        taskPath,
		BundlePath:      filepath.Dir(modelPath),
		OpenReviewIDs: map[string]bool{
			"PHF-RC-DEMO-001": true,
			"PHF-RC-DEMO-002": true,
		},
		AnsweredReviews: map[string]string{},
		AcceptedQuality: map[string]bool{},
		Resources: []InputResource{{
			ID:                   "R-001",
			Kind:                 "uploaded_file",
			FileType:             "markdown",
			Title:                "Printing House task text",
			FileName:             "TASK_FULL.md",
			ExtractionStatus:     "ready",
			ExtractionConfidence: "high",
			CreatedAt:            now.Add(-6 * time.Hour),
			UpdatedAt:            now.Add(-5 * time.Hour),
		}},
	}
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
