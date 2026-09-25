package llmpipeline

import "dbdsl/internal/dsl"

const PipelineVersion = "0.9.0"

type CombinedDocumentResource struct {
	ID            string                 `json:"id"`
	Title         string                 `json:"title"`
	FileType      string                 `json:"file_type"`
	Language      string                 `json:"language"`
	Authority     string                 `json:"authority"`
	ContentHash   string                 `json:"content_hash"`
	ExtractedHash string                 `json:"extracted_text_hash"`
	LineCount     int                    `json:"line_count"`
	Lines         []CombinedDocumentLine `json:"lines"`
	Metadata      map[string]any         `json:"metadata,omitempty"`
}

type CombinedDocumentLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

type CombinedDocumentProposal struct {
	Sentences         []CombinedDocumentSentence `json:"sentences"`
	Warnings          []string                   `json:"warnings"`
	ConfidenceSummary map[string]string          `json:"confidence_summary"`
}

type CombinedDocumentSentence struct {
	ID                   string                   `json:"id"`
	Kind                 string                   `json:"kind,omitempty"`
	Role                 string                   `json:"role,omitempty"`
	Section              string                   `json:"section,omitempty"`
	Relevance            string                   `json:"relevance,omitempty"`
	Tags                 []string                 `json:"tags,omitempty"`
	Text                 string                   `json:"text"`
	NormalizedText       string                   `json:"normalized_text,omitempty"`
	DerivedFrom          []CombinedDocumentOrigin `json:"derived_from,omitempty"`
	Transformation       string                   `json:"transformation,omitempty"`
	SegmentationStrategy string                   `json:"segmentation_strategy,omitempty"`
	Confidence           string                   `json:"confidence,omitempty"`
	RequiresReview       bool                     `json:"requires_review,omitempty"`
	Warnings             []string                 `json:"warnings,omitempty"`
}

const (
	CombinedDocumentUnitSentence   = "sentence"
	CombinedDocumentUnitStructural = "structural"
)

// CombinedDocumentUnitKind keeps artifacts produced before the kind field was
// introduced compatible with the sentence-based downstream pipeline.
func CombinedDocumentUnitKind(unit CombinedDocumentSentence) string {
	if unit.Kind == CombinedDocumentUnitStructural {
		return CombinedDocumentUnitStructural
	}
	return CombinedDocumentUnitSentence
}

type CombinedDocumentOrigin struct {
	ResourceID      string `json:"resource_id"`
	SourceSegmentID string `json:"source_segment_id,omitempty"`
	LineStart       int    `json:"line_start"`
	LineEnd         int    `json:"line_end"`
	StartByte       int    `json:"start_byte,omitempty"`
	EndByte         int    `json:"end_byte,omitempty"`
	ExactText       string `json:"exact_text"`
}

// SourceSegmentationSegment is one segment as the LLM returned it, with the
// SU ID the backend assigned. The backend adds nothing else.
type SourceSegmentationSegment struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
}

type SourceSegmentationProposal struct {
	Segments []SourceSegmentationSegment `json:"segments"`
}

type SourceUnitProposal struct {
	ID             string                      `json:"id"`
	Kind           string                      `json:"kind"`
	Section        string                      `json:"section"`
	Relevance      string                      `json:"relevance"`
	Tags           []string                    `json:"tags"`
	ExactText      string                      `json:"exact_text"`
	NormalizedText string                      `json:"normalized_text"`
	Normalization  dsl.SourceTextNormalization `json:"normalization"`
	SegmentIDs     []string                    `json:"segment_ids,omitempty"`
	ODSentenceIDs  []string                    `json:"od_sentence_ids,omitempty"` // legacy proposals only
	Confidence     string                      `json:"confidence"`
	RequiresReview bool                        `json:"requires_review"`
	Warnings       []string                    `json:"warnings"`
	// RequirementNotes carry requirement-level ambiguity to the atom stage.
	RequirementNotes []string `json:"requirement_notes,omitempty"`
}

type SourceUnitExtractionProposal struct {
	SourceUnits       []SourceUnitProposal `json:"source_units"`
	Warnings          []string             `json:"warnings"`
	ConfidenceSummary map[string]string    `json:"confidence_summary"`
}

type SourceUnitReviewDecision struct {
	SourceUnitID           string                      `json:"source_unit_id"`
	Decision               string                      `json:"decision"`
	PreviousNormalizedText string                      `json:"previous_normalized_text"`
	NormalizedText         string                      `json:"normalized_text"`
	Normalization          dsl.SourceTextNormalization `json:"normalization"`
	PreviousRelevance      string                      `json:"previous_relevance"`
	Relevance              string                      `json:"relevance"`
	Note                   string                      `json:"note,omitempty"`
	ReviewedBy             string                      `json:"reviewed_by"`
	ReviewedAt             string                      `json:"reviewed_at"`
	ProjectRevision        int                         `json:"project_revision"`
}

type SourceUnitQA struct {
	OK                      bool                       `json:"ok"`
	DerivationStrategy      string                     `json:"derivation_strategy"`
	SegmentsTotal           int                        `json:"segments_total,omitempty"`
	SegmentsReferenced      int                        `json:"segments_referenced,omitempty"`
	UnreferencedSegments    []string                   `json:"unreferenced_segments,omitempty"`
	ODSentencesTotal        int                        `json:"od_sentences_total,omitempty"`        // legacy QA only
	ODSentencesReferenced   int                        `json:"od_sentences_referenced,omitempty"`   // legacy QA only
	UnreferencedODSentences []string                   `json:"unreferenced_od_sentences,omitempty"` // legacy QA only
	NeedsAttention          []string                   `json:"needs_attention"`
	OriginChains            map[string][]string        `json:"origin_chains"`
	Errors                  []string                   `json:"errors"`
	Warnings                []string                   `json:"warnings"`
	ReviewDecisions         []SourceUnitReviewDecision `json:"review_decisions"`
}

type EvidenceProposal struct {
	SourceUnits     []string `json:"source_units"`
	ReviewDecisions []string `json:"review_decisions"`
	SupportLevel    string   `json:"support_level"`
	Confidence      string   `json:"confidence"`
	Notes           []string `json:"notes"`
}

type RequirementAtomProposal struct {
	ID                string   `json:"id"`
	Statement         string   `json:"statement"`
	Subject           string   `json:"subject,omitempty"`
	Predicate         string   `json:"predicate,omitempty"`
	Object            string   `json:"object,omitempty"`
	Quantifier        string   `json:"quantifier,omitempty"`
	Condition         string   `json:"condition,omitempty"`
	TemporalSemantics string   `json:"temporal_semantics,omitempty"`
	Ownership         string   `json:"ownership,omitempty"`
	AtomType          string   `json:"atom_type"`
	ModelingRelevance string   `json:"modeling_relevance"`
	SourceUnits       []string `json:"source_units"`
	FunctionalArea    string   `json:"functional_area"`
	FunctionalPattern string   `json:"functional_pattern"`
	SupportLevel      string   `json:"support_level"`
	Confidence        string   `json:"confidence"`
	RequiresReview    bool     `json:"requires_review"`
	ReviewClass       string   `json:"review_class,omitempty" yaml:"review_class,omitempty"`
	ReviewTopic       string   `json:"review_topic,omitempty" yaml:"review_topic,omitempty"`
	ReviewGroup       string   `json:"review_group,omitempty" yaml:"review_group,omitempty"`
	ModelingOutcome   string   `json:"modeling_outcome"`
	PersistenceEffect string   `json:"persistence_effect,omitempty" yaml:"persistence_effect,omitempty"`
	ExampleRole       string   `json:"example_role,omitempty" yaml:"example_role,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	ReviewDecisions   []string `json:"review_decisions,omitempty"`
}

type RequirementAtomExtractionProposal struct {
	RequirementAtoms  []RequirementAtomProposal `json:"requirement_atoms"`
	Warnings          []string                  `json:"warnings"`
	ConfidenceSummary map[string]string         `json:"confidence_summary"`
}

type FunctionalAreaProposal struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Purpose       string   `json:"purpose"`
	MainActors    []string `json:"main_actors"`
	Atoms         []string `json:"atoms"`
	ModelingFocus []string `json:"modeling_focus"`
	Confidence    string   `json:"confidence,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

type ActorProposal struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Kind        string `json:"kind,omitempty"`
}

type FunctionalAnalysisProposal struct {
	FunctionalAreas   []FunctionalAreaProposal `json:"functional_areas"`
	Actors            []ActorProposal          `json:"actors"`
	Warnings          []string                 `json:"warnings"`
	ConfidenceSummary map[string]string        `json:"confidence_summary"`
}

type CRUDOperationProposal struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	ActorID          string   `json:"actor_id"`
	FunctionalAreaID string   `json:"functional_area_id"`
	Creates          []string `json:"creates"`
	Reads            []string `json:"reads"`
	Updates          []string `json:"updates"`
	Deletes          []string `json:"deletes"`
	PersistentData   []string `json:"persistent_data"`
	Outcome          string   `json:"outcome"`
	SourceUnits      []string `json:"source_units"`
	RequiresReview   bool     `json:"requires_review"`
	Warnings         []string `json:"warnings"`
}

type CRUDMappingProposal struct {
	Operations        []CRUDOperationProposal `json:"operations"`
	Warnings          []string                `json:"warnings"`
	ConfidenceSummary map[string]string       `json:"confidence_summary"`
}

type StageQA struct {
	OK       bool           `json:"ok"`
	Errors   []string       `json:"errors"`
	Warnings []string       `json:"warnings"`
	Coverage map[string]int `json:"coverage"`
}

type ConceptualAttributeProposal struct {
	ID          string `json:"id" yaml:"id"`
	Label       string `json:"label" yaml:"label"`
	Description string `json:"description" yaml:"description"`
	// Name, ValueType, Unique and EnumValues are decided at the conceptual gate so
	// that the logical projection can be a deterministic mapping.
	Name       string           `json:"name,omitempty" yaml:"name,omitempty"`
	ValueType  string           `json:"value_type,omitempty" yaml:"value_type,omitempty"`
	Required   bool             `json:"required" yaml:"required"`
	Unique     bool             `json:"unique,omitempty" yaml:"unique,omitempty"`
	EnumValues []string         `json:"enum_values,omitempty" yaml:"enum_values,omitempty"`
	Evidence   EvidenceProposal `json:"evidence" yaml:"evidence"`
}

type ConceptualEntityProposal struct {
	ID          string                        `json:"id" yaml:"id"`
	Label       string                        `json:"label" yaml:"label"`
	Description string                        `json:"description" yaml:"description"`
	Kind        string                        `json:"kind" yaml:"kind"`
	Attributes  []ConceptualAttributeProposal `json:"attributes" yaml:"attributes"`
	Evidence    EvidenceProposal              `json:"evidence" yaml:"evidence"`
}

type ConceptualRelationshipProposal struct {
	ID          string           `json:"id" yaml:"id"`
	Label       string           `json:"label" yaml:"label"`
	Description string           `json:"description" yaml:"description"`
	From        string           `json:"from" yaml:"from"`
	To          string           `json:"to" yaml:"to"`
	Cardinality string           `json:"cardinality" yaml:"cardinality"`
	Required    *bool            `json:"required,omitempty" yaml:"required,omitempty"`
	Evidence    EvidenceProposal `json:"evidence" yaml:"evidence"`
}

type ConceptualConstraintProposal struct {
	ID          string           `json:"id" yaml:"id"`
	Label       string           `json:"label" yaml:"label"`
	Description string           `json:"description" yaml:"description"`
	Kind        string           `json:"kind" yaml:"kind"`
	Targets     []string         `json:"targets" yaml:"targets"`
	Expression  string           `json:"expression,omitempty" yaml:"expression,omitempty"`
	Evidence    EvidenceProposal `json:"evidence" yaml:"evidence"`
}

// ConceptualIndexProposal is an access path: an attribute that queries search,
// filter or sort by. It is not part of the data model, only of physical design.
type ConceptualIndexProposal struct {
	ID          string           `json:"id" yaml:"id"`
	Label       string           `json:"label" yaml:"label"`
	Description string           `json:"description" yaml:"description"`
	Owner       string           `json:"owner" yaml:"owner"`
	Targets     []string         `json:"targets" yaml:"targets"`
	Evidence    EvidenceProposal `json:"evidence" yaml:"evidence"`
}

type ConceptualModelProposal struct {
	EntityConcepts      []ConceptualEntityProposal       `json:"entity_concepts" yaml:"entity_concepts"`
	Relationships       []ConceptualRelationshipProposal `json:"relationships" yaml:"relationships"`
	ConstraintConcepts  []ConceptualConstraintProposal   `json:"constraint_concepts,omitempty" yaml:"constraint_concepts,omitempty"`
	LifecycleConcepts   []PlanElementProposal            `json:"lifecycle_concepts" yaml:"lifecycle_concepts"`
	DerivedConcepts     []PlanElementProposal            `json:"derived_concepts" yaml:"derived_concepts"`
	FileConcepts        []PlanElementProposal            `json:"file_concepts" yaml:"file_concepts"`
	ImportConcepts      []PlanElementProposal            `json:"import_concepts" yaml:"import_concepts"`
	IndexConcepts       []ConceptualIndexProposal        `json:"index_concepts,omitempty" yaml:"index_concepts,omitempty"`
	UnresolvedReviewIDs []string                         `json:"unresolved_review_ids" yaml:"unresolved_review_ids"`
	Warnings            []string                         `json:"warnings" yaml:"warnings"`
	ConfidenceSummary   map[string]string                `json:"confidence_summary" yaml:"confidence_summary"`
}

type OperationProposal struct {
	ID                string   `json:"id"`
	Label             string   `json:"label"`
	FunctionalArea    string   `json:"functional_area"`
	FunctionalPattern string   `json:"functional_pattern"`
	Actor             string   `json:"actor"`
	SourceAtoms       []string `json:"source_atoms"`
	SourceUnits       []string `json:"source_units"`
	Description       string   `json:"description"`
}

type PlanElementProposal struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	TableName   string   `json:"table_name"`
	Kind        string   `json:"kind"`
	SourceUnits []string `json:"source_units"`
	// Lifecycle concepts: the owning entity, its status attribute and the states.
	Owner       string                 `json:"owner,omitempty"`
	Field       string                 `json:"field,omitempty"`
	States      []string               `json:"states,omitempty"`
	Initial     string                 `json:"initial,omitempty"`
	Terminal    []string               `json:"terminal,omitempty"`
	Transitions []ConceptualTransition `json:"transitions,omitempty"`
	// Derived concepts: the entities they are computed from and what they show.
	Sources []string `json:"sources,omitempty"`
	Metrics []string `json:"metrics,omitempty"`
}

type ConceptualTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type PatchProposal struct {
	Operations          []PatchOperation  `json:"operations"`
	Warnings            []string          `json:"warnings"`
	UnresolvedQuestions []string          `json:"unresolved_questions"`
	ConfidenceSummary   map[string]string `json:"confidence_summary"`
}

type PatchOperation struct {
	Operation       string                `json:"operation"`
	TargetOperation string                `json:"target_operation"`
	TargetID        string                `json:"target_id"`
	Entity          *EntityProposal       `json:"entity"`
	Relationship    *RelationshipProposal `json:"relationship"`
	Constraint      *ConstraintProposal   `json:"constraint"`
	StateMachine    *StateMachineProposal `json:"state_machine"`
	DerivedView     *DerivedViewProposal  `json:"derived_view"`
	FileSpec        *FileSpecProposal     `json:"file_spec"`
	ImportSpec      *ImportSpecProposal   `json:"import_spec"`
	Index           *IndexProposal        `json:"index,omitempty"`
}

// IndexProposal is a non-unique index the deterministic mapper derives from
// the search and sort criteria of the conceptual description.
type IndexProposal struct {
	ID          string           `json:"id"`
	Owner       string           `json:"owner"`
	Fields      []string         `json:"fields"`
	Description string           `json:"description"`
	Evidence    EvidenceProposal `json:"evidence"`
}

type EntityProposal struct {
	ID          string              `json:"id"`
	Label       string              `json:"label"`
	Description string              `json:"description"`
	TableName   string              `json:"table_name"`
	Kind        string              `json:"kind"`
	Evidence    EvidenceProposal    `json:"evidence"`
	Attributes  []AttributeProposal `json:"attributes"`
}

type AttributeProposal struct {
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Description string           `json:"description"`
	Type        string           `json:"type"`
	Required    bool             `json:"required"`
	Precision   *int             `json:"precision"`
	Scale       *int             `json:"scale"`
	Default     any              `json:"default"`
	SourceField string           `json:"source_field"`
	EnumValues  []string         `json:"enum_values"`
	Notes       []string         `json:"notes"`
	Evidence    EvidenceProposal `json:"evidence"`
}

type RelationshipProposal struct {
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Description string           `json:"description"`
	From        string           `json:"from"`
	To          string           `json:"to"`
	Cardinality string           `json:"cardinality"`
	Required    bool             `json:"required"`
	FKRequired  bool             `json:"fk_required"`
	OnDelete    string           `json:"on_delete"`
	Identifying bool             `json:"identifying"`
	Through     string           `json:"through"`
	Notes       []string         `json:"notes"`
	Evidence    EvidenceProposal `json:"evidence"`
}

type ConstraintProposal struct {
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	Owner       string           `json:"owner"`
	Field       string           `json:"field"`
	Fields      []string         `json:"fields"`
	Value       any              `json:"value"`
	Min         any              `json:"min"`
	Max         any              `json:"max"`
	Pattern     string           `json:"pattern"`
	Expression  string           `json:"expression"`
	Description string           `json:"description"`
	Evidence    EvidenceProposal `json:"evidence"`
}

type StateMachineProposal struct {
	ID          string                `json:"id"`
	Owner       string                `json:"owner"`
	Field       string                `json:"field"`
	States      []string              `json:"states"`
	Initial     string                `json:"initial"`
	Terminal    []string              `json:"terminal"`
	Transitions []dsl.StateTransition `json:"transitions"`
	Notes       []string              `json:"notes"`
	Evidence    EvidenceProposal      `json:"evidence"`
}

type DerivedViewProposal struct {
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Description string           `json:"description"`
	Kind        string           `json:"kind"`
	Sources     []string         `json:"sources"`
	Persistence string           `json:"persistence"`
	Metrics     []string         `json:"metrics"`
	Filters     []string         `json:"filters"`
	Notes       []string         `json:"notes"`
	Evidence    EvidenceProposal `json:"evidence"`
}

type FileSpecProposal struct {
	ID                string           `json:"id"`
	Owner             string           `json:"owner"`
	Field             string           `json:"field"`
	AllowedExtensions []string         `json:"allowed_extensions"`
	MaxSizeMB         *int             `json:"max_size_mb"`
	MIMETypes         []string         `json:"mime_types"`
	Storage           string           `json:"storage"`
	Notes             []string         `json:"notes"`
	Evidence          EvidenceProposal `json:"evidence"`
}

type ImportSpecProposal struct {
	ID          string                  `json:"id"`
	Label       string                  `json:"label"`
	Description string                  `json:"description"`
	Format      string                  `json:"format"`
	Source      ImportSourceProposal    `json:"source"`
	Root        string                  `json:"root"`
	Mappings    []ImportMappingProposal `json:"mappings"`
	Evidence    EvidenceProposal        `json:"evidence"`
}

type ImportSourceProposal struct {
	Fragment    string   `json:"fragment"`
	SourceID    string   `json:"source_id"`
	File        string   `json:"file"`
	SourceUnits []string `json:"source_units"`
}

type ImportMappingProposal struct {
	SourcePath string   `json:"source_path"`
	Target     string   `json:"target"`
	Notes      []string `json:"notes"`
}
