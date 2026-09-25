package dsl

type Document struct {
	TopLevelKeys  map[string]bool `yaml:"-"`
	DSL           DSLMeta         `yaml:"dsl"`
	Model         ModelInfo       `yaml:"model"`
	Source        SourceInfo      `yaml:"source"`
	Entities      []Entity        `yaml:"entities"`
	Relationships []Relationship  `yaml:"relationships"`
	Constraints   []Constraint    `yaml:"constraints"`
	ImportSpecs   []ImportSpec    `yaml:"import_specs"`
	StateMachines []StateMachine  `yaml:"state_machines"`
	DerivedViews  []DerivedView   `yaml:"derived_views"`
	FileSpecs     []FileSpec      `yaml:"file_specs"`
}

type DSLMeta struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

type ModelInfo struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	DomainSlice string `yaml:"domain_slice"`
	Status      string `yaml:"status"`
	Description string `yaml:"description"`
}

type SourceInfo struct {
	ReviewedFragmentsFile       string                   `yaml:"reviewed_fragments_file"`
	ReviewState                 string                   `yaml:"review_state"`
	AcceptedReviewDecisions     []AcceptedReviewDecision `yaml:"accepted_review_decisions"`
	PipelineVersion             string                   `yaml:"pipeline_version"`
	TaskTextFile                string                   `yaml:"task_text_file"`
	SourceUnitsFile             string                   `yaml:"source_units_file"`
	RequirementAtomsFile        string                   `yaml:"requirement_atoms_file"`
	FunctionalDecompositionFile string                   `yaml:"functional_decomposition_file"`
	CRUDMatrixFile              string                   `yaml:"crud_matrix_file"`
	ReviewDecisionsFile         string                   `yaml:"review_decisions_file"`
	DerivationStrategy          string                   `yaml:"derivation_strategy"`
}

type AcceptedReviewDecision struct {
	ReviewID       string `yaml:"review_id"`
	SelectedOption string `yaml:"selected_option"`
}

type Entity struct {
	ID          string      `yaml:"id"`
	Label       string      `yaml:"label"`
	Description string      `yaml:"description"`
	TableName   string      `yaml:"table_name"`
	Kind        string      `yaml:"kind"`
	Evidence    Evidence    `yaml:"evidence"`
	Attributes  []Attribute `yaml:"attributes"`
}

type Attribute struct {
	ID          string   `yaml:"id"`
	Label       string   `yaml:"label"`
	Description string   `yaml:"description"`
	Type        string   `yaml:"type"`
	Required    *bool    `yaml:"required"`
	Precision   *int     `yaml:"precision"`
	Scale       *int     `yaml:"scale"`
	Default     any      `yaml:"default"`
	SourceField string   `yaml:"source_field"`
	EnumValues  []string `yaml:"enum_values"`
	Notes       []string `yaml:"notes"`
	Evidence    Evidence `yaml:"evidence"`
}

type Relationship struct {
	ID          string   `yaml:"id"`
	Label       string   `yaml:"label"`
	Description string   `yaml:"description"`
	From        string   `yaml:"from"`
	To          string   `yaml:"to"`
	Cardinality string   `yaml:"cardinality"`
	Required    *bool    `yaml:"required"`
	FKRequired  *bool    `yaml:"fk_required"`
	OnDelete    string   `yaml:"on_delete"`
	Identifying *bool    `yaml:"identifying"`
	Through     string   `yaml:"through"`
	Notes       []string `yaml:"notes"`
	Evidence    Evidence `yaml:"evidence"`
}

type Constraint struct {
	ID          string        `yaml:"id"`
	Type        string        `yaml:"type"`
	Owner       string        `yaml:"owner"`
	Field       string        `yaml:"field"`
	Fields      []string      `yaml:"fields"`
	Value       any           `yaml:"value"`
	Min         any           `yaml:"min"`
	Max         any           `yaml:"max"`
	Pattern     string        `yaml:"pattern"`
	Expression  string        `yaml:"expression"`
	Condition   *Condition    `yaml:"condition"`
	Requires    []Requirement `yaml:"requires"`
	Description string        `yaml:"description"`
	Evidence    Evidence      `yaml:"evidence"`
}

type Condition struct {
	Field    string `yaml:"field"`
	Operator string `yaml:"operator"`
	Value    any    `yaml:"value"`
}

type Requirement struct {
	Kind   string `yaml:"kind"`
	Field  string `yaml:"field"`
	Entity string `yaml:"entity"`
}

type ImportSpec struct {
	ID          string          `yaml:"id"`
	Label       string          `yaml:"label"`
	Description string          `yaml:"description"`
	Format      string          `yaml:"format"`
	Source      ImportSource    `yaml:"source"`
	Root        string          `yaml:"root"`
	Mappings    []ImportMapping `yaml:"mappings"`
	Evidence    Evidence        `yaml:"evidence"`
}

type ImportSource struct {
	Fragment    string   `yaml:"fragment"`
	SourceID    string   `yaml:"source_id"`
	File        string   `yaml:"file"`
	SourceUnits []string `yaml:"source_units"`
}

type ImportMapping struct {
	SourcePath string   `yaml:"source_path"`
	Target     string   `yaml:"target"`
	Notes      []string `yaml:"notes"`
}

type StateMachine struct {
	ID          string            `yaml:"id"`
	Owner       string            `yaml:"owner"`
	Field       string            `yaml:"field"`
	States      []string          `yaml:"states"`
	Initial     string            `yaml:"initial"`
	Terminal    []string          `yaml:"terminal"`
	Transitions []StateTransition `yaml:"transitions"`
	Notes       []string          `yaml:"notes"`
	Evidence    Evidence          `yaml:"evidence"`
}

type StateTransition struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type DerivedView struct {
	ID          string   `yaml:"id"`
	Label       string   `yaml:"label"`
	Description string   `yaml:"description"`
	Kind        string   `yaml:"kind"`
	Sources     []string `yaml:"sources"`
	Persistence string   `yaml:"persistence"`
	Metrics     []string `yaml:"metrics"`
	Filters     []string `yaml:"filters"`
	Notes       []string `yaml:"notes"`
	Evidence    Evidence `yaml:"evidence"`
}

type FileSpec struct {
	ID                string   `yaml:"id"`
	Owner             string   `yaml:"owner"`
	Field             string   `yaml:"field"`
	AllowedExtensions []string `yaml:"allowed_extensions"`
	MaxSizeMB         *int     `yaml:"max_size_mb"`
	MIMETypes         []string `yaml:"mime_types"`
	Storage           string   `yaml:"storage"`
	Notes             []string `yaml:"notes"`
	Evidence          Evidence `yaml:"evidence"`
}

type Evidence struct {
	Fragments        []string `yaml:"fragments"`
	SourceUnits      []string `yaml:"source_units"`
	RequirementAtoms []string `yaml:"requirement_atoms"`
	ReviewDecisions  []string `yaml:"review_decisions"`
	SupportLevel     string   `yaml:"support_level"`
	Confidence       string   `yaml:"confidence"`
	Notes            []string `yaml:"notes"`
}

type V05Bundle struct {
	ModelPath                   string
	SourceUnitsPath             string
	RequirementAtomsPath        string
	FunctionalDecompositionPath string
	CRUDMatrixPath              string
	ReviewDecisionsPath         string
	Document                    *Document
	SourceUnits                 *V05SourceUnitsFile
	RequirementAtoms            *V05RequirementAtomsFile
	FunctionalDecomposition     *V05FunctionalDecompositionFile
	CRUDMatrix                  *V05CRUDMatrixFile
	ReviewDecisions             *V05ReviewDecisionsFile
}

type V05SourceUnitsFile struct {
	Document    V05SourceUnitsDocument `yaml:"document"`
	SourceUnits []SourceUnit           `yaml:"source_units"`
}

type V05SourceUnitsDocument struct {
	ID              string `yaml:"id"`
	Title           string `yaml:"title"`
	PipelineVersion string `yaml:"pipeline_version"`
	SourceFile      string `yaml:"source_file"`
	SourceLanguage  string `yaml:"source_language"`
	Granularity     string `yaml:"granularity"`
}

type SourceUnit struct {
	ID        string         `yaml:"id"`
	Kind      string         `yaml:"kind"`
	Section   string         `yaml:"section"`
	Location  string         `yaml:"location"`
	Relevance string         `yaml:"relevance"`
	Tags      []string       `yaml:"tags"`
	Text      SourceUnitText `yaml:"text"`
}

type SourceUnitText struct {
	Exact         string                  `json:"exact" yaml:"exact"`
	Normalized    string                  `json:"normalized" yaml:"normalized"`
	Normalization SourceTextNormalization `json:"normalization" yaml:"normalization"`
}

type SourceTextNormalization struct {
	Version        string                         `json:"version" yaml:"version"`
	Strategy       string                         `json:"strategy" yaml:"strategy"`
	ExactHash      string                         `json:"exact_hash" yaml:"exact_hash"`
	NormalizedHash string                         `json:"normalized_hash" yaml:"normalized_hash"`
	Changed        bool                           `json:"changed" yaml:"changed"`
	Operations     []SourceNormalizationOperation `json:"operations" yaml:"operations"`
}

type SourceNormalizationOperation struct {
	Kind   string `json:"kind" yaml:"kind"`
	Before string `json:"before" yaml:"before"`
	After  string `json:"after" yaml:"after"`
}

type V05RequirementAtomsFile struct {
	Document         map[string]any    `yaml:"document"`
	RequirementAtoms []RequirementAtom `yaml:"requirement_atoms"`
	CoverageChecks   []map[string]any  `yaml:"coverage_checks"`
}

type RequirementAtom struct {
	ID                string                  `yaml:"id"`
	Statement         string                  `yaml:"statement"`
	Subject           string                  `yaml:"subject,omitempty"`
	Predicate         string                  `yaml:"predicate,omitempty"`
	Object            string                  `yaml:"object,omitempty"`
	Quantifier        string                  `yaml:"quantifier,omitempty"`
	Condition         string                  `yaml:"condition,omitempty"`
	TemporalSemantics string                  `yaml:"temporal_semantics,omitempty"`
	Ownership         string                  `yaml:"ownership,omitempty"`
	AtomType          string                  `yaml:"atom_type"`
	ModelingRelevance string                  `yaml:"modeling_relevance"`
	SourceUnits       []string                `yaml:"source_units"`
	FunctionalArea    string                  `yaml:"functional_area"`
	FunctionalPattern string                  `yaml:"functional_pattern"`
	SupportLevel      string                  `yaml:"support_level"`
	Confidence        string                  `yaml:"confidence"`
	RequiresReview    bool                    `yaml:"requires_review"`
	ReviewClass       string                  `yaml:"review_class,omitempty"`
	ReviewTopic       string                  `yaml:"review_topic,omitempty"`
	ReviewGroup       string                  `yaml:"review_group,omitempty"`
	ReviewDecisions   []string                `yaml:"review_decisions"`
	ModelImpacts      RequirementModelImpacts `yaml:"model_impacts"`
	ModelingOutcome   RequirementOutcome      `yaml:"modeling_outcome"`
}

type RequirementModelImpacts struct {
	Entities      []string `yaml:"entities"`
	Attributes    []string `yaml:"attributes"`
	Relationships []string `yaml:"relationships"`
	Constraints   []string `yaml:"constraints"`
	ImportSpecs   []string `yaml:"import_specs"`
	StateMachines []string `yaml:"state_machines"`
	DerivedViews  []string `yaml:"derived_views"`
	FileSpecs     []string `yaml:"file_specs"`
}

type RequirementOutcome struct {
	Status string `yaml:"status"`
}

type V05FunctionalDecompositionFile struct {
	Document        map[string]any   `yaml:"document"`
	FunctionalAreas []FunctionalArea `yaml:"functional_areas"`
	CoverageSummary map[string]any   `yaml:"coverage_summary"`
}

type FunctionalArea struct {
	ID            string   `yaml:"id"`
	Label         string   `yaml:"label"`
	Purpose       string   `yaml:"purpose"`
	MainActors    []string `yaml:"main_actors"`
	Atoms         []string `yaml:"atoms"`
	ModelingFocus []string `yaml:"modeling_focus"`
}

type V05CRUDMatrixFile struct {
	Document       map[string]any    `yaml:"document"`
	Notation       map[string]string `yaml:"notation"`
	Actors         []CRUDActor       `yaml:"actors"`
	Operations     []CRUDOperation   `yaml:"operations"`
	Matrix         []CRUDRow         `yaml:"matrix"`
	CoverageChecks []map[string]any  `yaml:"coverage_checks"`
}

type CRUDActor struct {
	ID          string `yaml:"id"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
}

type CRUDOperation struct {
	ID                string   `yaml:"id"`
	Label             string   `yaml:"label"`
	FunctionalArea    string   `yaml:"functional_area"`
	FunctionalPattern string   `yaml:"functional_pattern"`
	Actor             string   `yaml:"actor"`
	SourceAtoms       []string `yaml:"source_atoms"`
	SourceUnits       []string `yaml:"source_units"`
	Description       string   `yaml:"description"`
}

type CRUDRow struct {
	Entity     string              `yaml:"entity"`
	Table      string              `yaml:"table"`
	Operations map[string][]string `yaml:"operations"`
	Rationale  string              `yaml:"rationale"`
}

type V05ReviewDecisionsFile struct {
	Document        map[string]any      `yaml:"document"`
	ReviewState     map[string]any      `yaml:"review_state"`
	ReviewDecisions []V05ReviewDecision `yaml:"review_decisions"`
	CoverageChecks  []map[string]any    `yaml:"coverage_checks"`
}

type V05ReviewDecision struct {
	ID            string         `yaml:"id"`
	Question      string         `yaml:"question"`
	AffectedAtoms []string       `yaml:"affected_atoms"`
	Decision      map[string]any `yaml:"decision"`
}

type ReviewedSource struct {
	Document    ReviewedDocument `yaml:"document"`
	ReviewState ReviewState      `yaml:"review_state"`
	Scope       ReviewedScope    `yaml:"scope"`
	Fragments   []SourceFragment `yaml:"fragments"`
	ReviewItems []ReviewItem     `yaml:"review_items"`
}

type ReviewedDocument struct {
	ID             string              `yaml:"id"`
	Title          string              `yaml:"title"`
	Phase          string              `yaml:"phase"`
	Description    string              `yaml:"description"`
	Objective      string              `yaml:"objective"`
	SourceLanguage string              `yaml:"source_language"`
	Sources        []ReviewedSourceRef `yaml:"sources"`
	DerivedFrom    string              `yaml:"derived_from"`
}

type ReviewedSourceRef struct {
	ID          string `yaml:"id"`
	Type        string `yaml:"type"`
	Path        string `yaml:"path"`
	Description string `yaml:"description"`
}

type ReviewedScope struct {
	Description string   `yaml:"description"`
	Include     []string `yaml:"include"`
	Defer       []string `yaml:"defer"`
}

type ReviewState struct {
	Status                        string `yaml:"status"`
	AllRequiredReviewsResolved    *bool  `yaml:"all_required_reviews_resolved"`
	UnresolvedRequiresReviewFlags *int   `yaml:"unresolved_requires_review_flags"`
}

type SourceFragment struct {
	ID                string           `yaml:"id"`
	Description       string           `yaml:"description"`
	Types             []string         `yaml:"types"`
	Decision          string           `yaml:"decision"`
	Phase2Eligibility string           `yaml:"phase_2_eligibility"`
	ReviewRefs        []string         `yaml:"review_refs"`
	Source            FragmentSource   `yaml:"source"`
	Text              FragmentText     `yaml:"text"`
	Evidence          FragmentEvidence `yaml:"evidence"`
	DerivedCandidates map[string]any   `yaml:"derived_candidates"`
	Interpretation    []string         `yaml:"interpretation"`
	Ambiguities       []string         `yaml:"ambiguities"`
}

type FragmentSource struct {
	ID      string `yaml:"id"`
	Locator string `yaml:"locator"`
	Quality string `yaml:"quality"`
}

type FragmentText struct {
	Normalized string `yaml:"normalized"`
}

type FragmentEvidence struct {
	SupportLevel   string `yaml:"support_level"`
	Confidence     string `yaml:"confidence"`
	RequiresReview *bool  `yaml:"requires_review"`
}

type ReviewItem struct {
	ID                string         `yaml:"id"`
	Description       string         `yaml:"description"`
	Question          string         `yaml:"question"`
	AffectedFragments []string       `yaml:"affected_fragments"`
	ResolvesReviewFor []string       `yaml:"resolves_review_for"`
	DependsOn         []string       `yaml:"depends_on"`
	Options           []ReviewOption `yaml:"options"`
	Decision          ReviewDecision `yaml:"decision"`
}

type ReviewOption struct {
	ID          string `yaml:"id"`
	Label       string `yaml:"label"`
	Recommended *bool  `yaml:"recommended"`
	Rationale   string `yaml:"rationale"`
}

type ReviewDecision struct {
	Status         string `yaml:"status"`
	SelectedOption string `yaml:"selected_option"`
	CustomText     string `yaml:"custom_text"`
	ReviewedBy     string `yaml:"reviewed_by"`
	ReviewedAt     string `yaml:"reviewed_at"`
	Rationale      string `yaml:"rationale"`
	ResolutionNote string `yaml:"resolution_note"`
}
