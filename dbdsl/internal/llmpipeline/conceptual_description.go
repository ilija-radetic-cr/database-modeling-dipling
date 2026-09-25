package llmpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

// ConceptualDescription is the rich, denormalized description of what a system
// must remember, produced by the second LLM interaction directly from the
// backend-numbered segments. Structure (tables, keys, normalization) is derived
// from it deterministically by ConceptualDescriptionToModel.
type ConceptualDescription struct {
	Actors        []DescriptionActor    `json:"actors"`
	Things        []DescriptionThing    `json:"things"`
	Rules         []DescriptionRule     `json:"rules"`
	Queries       []DescriptionQuery    `json:"queries"`
	Imports       []DescriptionImport   `json:"imports"`
	Boundaries    []DescriptionBoundary `json:"boundaries"`
	Excluded      []DescriptionExcluded `json:"excluded"`
	OpenQuestions []DescriptionQuestion `json:"open_questions"`
}

type DescriptionEvidence struct {
	Segments []string `json:"segments"`
	Mode     string   `json:"mode"`
}

type DescriptionActor struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	RepresentedBy string              `json:"represented_by"`
	DiffersBy     string              `json:"differs_by"`
	Evidence      DescriptionEvidence `json:"evidence"`
}

// DescriptionRefs is a list of references ("svojstvo" or a thing ID). It also
// reads the older free-text form of identified_by as a one-element list, so
// descriptions generated before the field became structured still load.
type DescriptionRefs []string

func (r *DescriptionRefs) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*r = list
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	*r = DescriptionRefs{}
	if strings.TrimSpace(text) != "" {
		*r = DescriptionRefs{text}
	}
	return nil
}

type DescriptionThing struct {
	ID           string                  `json:"id"`
	Name         string                  `json:"name"`
	Kind         string                  `json:"kind"`
	Description  string                  `json:"description"`
	IdentifiedBy DescriptionRefs         `json:"identified_by"`
	InstanceOf   string                  `json:"instance_of"`
	Variants     []string                `json:"variants"`
	CreatedWhen  string                  `json:"created_when"`
	Properties   []DescriptionProperty   `json:"properties"`
	Links        []DescriptionLink       `json:"links"`
	States       []string                `json:"states"`
	Transitions  []DescriptionTransition `json:"transitions"`
	Evidence     DescriptionEvidence     `json:"evidence"`
}

type DescriptionProperty struct {
	Name          string              `json:"name"`
	Meaning       string              `json:"meaning"`
	ValueType     string              `json:"value_type"`
	Shape         string              `json:"shape"`
	Parts         []string            `json:"parts"`
	Presence      string              `json:"presence"`
	Condition     string              `json:"condition"`
	Variant       string              `json:"variant"`
	AllowedValues []string            `json:"allowed_values"`
	Origin        string              `json:"origin"`
	Source        string              `json:"source"`
	Evidence      DescriptionEvidence `json:"evidence"`
}

type DescriptionLink struct {
	To       string              `json:"to"`
	Meaning  string              `json:"meaning"`
	PerThis  string              `json:"per_this"`
	PerOther string              `json:"per_other"`
	Evidence DescriptionEvidence `json:"evidence"`
}

type DescriptionTransition struct {
	From     string              `json:"from"`
	To       string              `json:"to"`
	Trigger  string              `json:"trigger"`
	By       string              `json:"by"`
	Effects  string              `json:"effects"`
	Evidence DescriptionEvidence `json:"evidence"`
}

type DescriptionRule struct {
	ID         string              `json:"id"`
	Kind       string              `json:"kind"`
	Statement  string              `json:"statement"`
	AppliesTo  []string            `json:"applies_to"`
	Parameters []string            `json:"parameters"`
	Evidence   DescriptionEvidence `json:"evidence"`
}

type DescriptionQuery struct {
	ID          string              `json:"id"`
	Description string              `json:"description"`
	Needs       []string            `json:"needs"`
	Criteria    []string            `json:"criteria"`
	Evidence    DescriptionEvidence `json:"evidence"`
}

type DescriptionImport struct {
	ID          string              `json:"id"`
	Description string              `json:"description"`
	Fills       []string            `json:"fills"`
	Evidence    DescriptionEvidence `json:"evidence"`
}

type DescriptionBoundary struct {
	Kind        string              `json:"kind"`
	Description string              `json:"description"`
	KeptOutcome string              `json:"kept_outcome"`
	Evidence    DescriptionEvidence `json:"evidence"`
}

type DescriptionExcluded struct {
	Segment string `json:"segment"`
	Reason  string `json:"reason"`
}

type DescriptionQuestion struct {
	ID       string              `json:"id"`
	Question string              `json:"question"`
	Readings []string            `json:"readings"`
	Affects  []string            `json:"affects"`
	Evidence DescriptionEvidence `json:"evidence"`
}

// DescriptionValueTypes are the value kinds a property may declare; they map
// one-to-one onto conceptual value types (see descriptionValueType).
var DescriptionValueTypes = []string{"text", "long_text", "integer", "decimal", "money", "boolean", "date", "time", "datetime", "file", "email", "phone", "url"}

func conceptualDescriptionSchema() map[string]any {
	evidence := object(map[string]any{"segments": array(str()), "mode": enum("direct", "implied")})
	return object(map[string]any{
		"actors": array(object(map[string]any{
			"id": str(), "name": str(), "description": str(), "represented_by": str(), "differs_by": str(), "evidence": evidence,
		})),
		"things": array(object(map[string]any{
			"id": str(), "name": str(), "kind": enum("object", "record", "classification", "settings"),
			"description": str(), "identified_by": array(str()), "instance_of": str(), "variants": array(str()), "created_when": str(),
			"properties": array(object(map[string]any{
				"name": str(), "meaning": str(), "value_type": enum(DescriptionValueTypes...),
				"shape": enum("single", "composite", "multiple"), "parts": array(str()),
				"presence": enum("required", "optional", "conditional"), "condition": str(), "variant": str(),
				"allowed_values": array(str()), "origin": enum("entered", "generated", "derived", "copied"), "source": str(),
				"evidence": evidence,
			})),
			"links": array(object(map[string]any{
				"to": str(), "meaning": str(), "per_this": str(), "per_other": str(), "evidence": evidence,
			})),
			"states": array(str()),
			"transitions": array(object(map[string]any{
				"from": str(), "to": str(), "trigger": str(), "by": str(), "effects": str(), "evidence": evidence,
			})),
			"evidence": evidence,
		})),
		"rules": array(object(map[string]any{
			"id": str(), "kind": enum("uniqueness", "access", "visibility", "eligibility", "precondition", "approval", "priority", "quantity", "time", "mutability", "retention", "parameter", "context", "other"),
			"statement": str(), "applies_to": array(str()), "parameters": array(str()), "evidence": evidence,
		})),
		"queries":    array(object(map[string]any{"id": str(), "description": str(), "needs": array(str()), "criteria": array(str()), "evidence": evidence})),
		"imports":    array(object(map[string]any{"id": str(), "description": str(), "fills": array(str()), "evidence": evidence})),
		"boundaries": array(object(map[string]any{"kind": enum("external_process", "delegated_decision"), "description": str(), "kept_outcome": str(), "evidence": evidence})),
		"excluded": array(object(map[string]any{
			"segment": str(), "reason": enum("technology", "presentation", "assessment", "demo_data", "database_setup", "other"),
		})),
		"open_questions": array(object(map[string]any{
			"id": str(), "question": str(), "readings": array(str()), "affects": array(str()), "evidence": evidence,
		})),
	})
}

type ConceptualDescriptionOptions struct {
	OutDir          string
	SourceUnits     []dsl.SourceUnit
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	PromptVersion   string
}

// RunConceptualDescription is the second LLM interaction of the flow: it reads
// the numbered segments and returns the rich description. Structural problems
// that would break the deterministic transformation get one feedback retry.
func RunConceptualDescription(ctx context.Context, client llm.Client, opts ConceptualDescriptionOptions) (ConceptualDescription, StageQA, error) {
	if client == nil {
		return ConceptualDescription{}, StageQA{}, errors.New("LLM client is required for conceptual modeling")
	}
	if opts.OutDir == "" || len(opts.SourceUnits) == 0 {
		return ConceptualDescription{}, StageQA{}, errors.New("output directory and source units are required")
	}
	opts.Model = nonEmpty(opts.Model, llm.DefaultModel)
	opts.ReasoningEffort = nonEmpty(opts.ReasoningEffort, llm.DefaultReasoningEffort)
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = llm.DefaultMaxOutputTokens
	}
	input := RenderSegmentsForConceptualModel(opts.SourceUnits)
	var description ConceptualDescription
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 2, llm.Request{
		Stage: "conceptual_description", Model: opts.Model, Instructions: conceptualDescriptionInstructions,
		Input: input, SchemaName: "DBDSLConceptualDescription", Schema: conceptualDescriptionSchema(),
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": resolvedPromptVersion(opts.PromptVersion), "run_key": "segments",
			"full_context_bytes": fmt.Sprint(len(input)), "context_policy": "all_segments_v1",
			"canonicalizer_version": "backend_source_unit_ids_v1", "call_gate_policy": "always_v1",
			"budget_policy": "adaptive_v1", "stage_max_output_tokens": fmt.Sprint(opts.MaxOutputTokens),
		},
	}, &description, func() []string {
		qa = ValidateConceptualDescription(&description, opts.SourceUnits)
		return qa.Errors
	})
	if err != nil {
		return description, qa, err
	}
	if !qa.OK {
		return description, qa, fmt.Errorf("conceptual description failed validation: %s", strings.Join(qa.Errors, "; "))
	}
	return description, qa, nil
}

// RenderSegmentsForConceptualModel writes the numbered segments in the format
// the conceptual prompt describes: "SU-xxx [type] text". Page headers, footers
// and numbers are left out.
func RenderSegmentsForConceptualModel(units []dsl.SourceUnit) string {
	var out strings.Builder
	out.WriteString("<segments>\n")
	for _, unit := range units {
		if conceptualInputUnit(unit) {
			fmt.Fprintf(&out, "%s [%s] %s\n", unit.ID, conceptualSegmentType(unit.Kind), unitText(unit))
		}
	}
	out.WriteString("</segments>")
	return out.String()
}

func unitText(unit dsl.SourceUnit) string {
	return strings.TrimSpace(nonEmpty(unit.Text.Exact, unit.Text.Normalized))
}

func conceptualInputUnit(unit dsl.SourceUnit) bool {
	return !pageNoiseSegmentTypes[unit.Kind] && unit.Kind != "noise" && unitText(unit) != ""
}

// conceptualSegmentType keeps segment types and maps kinds of source units that
// an older LLM classification produced onto them.
func conceptualSegmentType(kind string) string {
	switch kind {
	case "heading", "sentence", "list", "example", "footnote", "other", "list_item":
		return kind
	case "structured_example", "data_example":
		return "example"
	}
	return "sentence"
}

// conceptualCoverageTargets lists the units every description must use as
// evidence or exclude: all input units except headings.
func conceptualCoverageTargets(units []dsl.SourceUnit) []string {
	targets := []string{}
	for _, unit := range units {
		if conceptualInputUnit(unit) && unit.Kind != "heading" {
			targets = append(targets, unit.ID)
		}
	}
	return targets
}

// ValidateConceptualDescription repairs what can be repaired without guessing
// (unknown segment citations, actor IDs used as link targets) and reports as
// errors only problems that break the deterministic transformation. Missing
// coverage is a warning the conceptual review shows to the person.
func ValidateConceptualDescription(description *ConceptualDescription, units []dsl.SourceUnit) StageQA {
	qa := newStageQA(nil)
	known := map[string]bool{}
	for _, unit := range units {
		known[unit.ID] = true
	}
	dropped := map[string]bool{}
	clean := func(evidence *DescriptionEvidence) {
		kept := []string{}
		for _, id := range evidence.Segments {
			id = strings.TrimSpace(id)
			if known[id] {
				kept = appendUnique(kept, id)
			} else if id != "" {
				dropped[id] = true
			}
		}
		evidence.Segments = kept
	}
	things := map[string]bool{}
	for i := range description.Things {
		thing := &description.Things[i]
		thing.ID = strings.TrimSpace(thing.ID)
		if thing.ID == "" || things[thing.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("thing %q has an empty or duplicate id", thing.ID))
		}
		things[thing.ID] = true
		clean(&thing.Evidence)
		for p := range thing.Properties {
			clean(&thing.Properties[p].Evidence)
		}
		for l := range thing.Links {
			clean(&thing.Links[l].Evidence)
		}
		for t := range thing.Transitions {
			clean(&thing.Transitions[t].Evidence)
		}
	}
	if len(description.Things) == 0 {
		qa.Errors = append(qa.Errors, "the description contains no things")
	}
	actorThing := map[string]string{}
	for i := range description.Actors {
		actor := &description.Actors[i]
		clean(&actor.Evidence)
		if actor.RepresentedBy != "" && !things[actor.RepresentedBy] {
			qa.Warnings = append(qa.Warnings, fmt.Sprintf("actor %s represented_by %q is not a thing; the reference was cleared", actor.ID, actor.RepresentedBy))
			actor.RepresentedBy = ""
		}
		if actor.RepresentedBy != "" {
			actorThing[actor.ID] = actor.RepresentedBy
		}
	}
	for i := range description.Things {
		thing := &description.Things[i]
		if thing.InstanceOf != "" && !things[thing.InstanceOf] {
			qa.Warnings = append(qa.Warnings, fmt.Sprintf("thing %s instance_of %q is not a thing; the reference was cleared", thing.ID, thing.InstanceOf))
			thing.InstanceOf = ""
		}
		for l := range thing.Links {
			link := &thing.Links[l]
			if target, ok := actorThing[link.To]; ok && !things[link.To] {
				link.To = target
			}
			if !things[link.To] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("thing %s links to %q, which is not a thing id", thing.ID, link.To))
			}
		}
	}
	for _, group := range [][]*DescriptionEvidence{ruleEvidence(description), queryEvidence(description), importEvidence(description), boundaryEvidence(description), questionEvidence(description)} {
		for _, evidence := range group {
			clean(evidence)
		}
	}
	excluded := map[string]bool{}
	for i := range description.Excluded {
		id := strings.TrimSpace(description.Excluded[i].Segment)
		description.Excluded[i].Segment = id
		if known[id] {
			excluded[id] = true
		} else if id != "" {
			dropped[id] = true
		}
	}
	if len(dropped) > 0 {
		qa.Warnings = append(qa.Warnings, "unknown segment IDs were removed from evidence: "+strings.Join(sortedKeys(dropped), ", "))
	}
	cited := citedSegments(*description)
	targets := conceptualCoverageTargets(units)
	uncovered := []string{}
	for _, id := range targets {
		if !cited[id] && !excluded[id] {
			uncovered = append(uncovered, id)
		}
	}
	if len(uncovered) > 0 {
		qa.Warnings = append(qa.Warnings, "segments neither used as evidence nor excluded: "+strings.Join(uncovered, ", "))
	}
	qa.Coverage["segments"] = len(targets)
	qa.Coverage["covered_segments"] = len(targets) - len(uncovered)
	qa.Coverage["uncovered_segments"] = len(uncovered)
	qa.Coverage["things"] = len(description.Things)
	qa.Coverage["rules"] = len(description.Rules)
	qa.Coverage["open_questions"] = len(description.OpenQuestions)
	qa.Coverage["excluded_segments"] = len(excluded)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func ruleEvidence(d *ConceptualDescription) []*DescriptionEvidence {
	out := make([]*DescriptionEvidence, 0, len(d.Rules))
	for i := range d.Rules {
		out = append(out, &d.Rules[i].Evidence)
	}
	return out
}

func queryEvidence(d *ConceptualDescription) []*DescriptionEvidence {
	out := make([]*DescriptionEvidence, 0, len(d.Queries))
	for i := range d.Queries {
		out = append(out, &d.Queries[i].Evidence)
	}
	return out
}

func importEvidence(d *ConceptualDescription) []*DescriptionEvidence {
	out := make([]*DescriptionEvidence, 0, len(d.Imports))
	for i := range d.Imports {
		out = append(out, &d.Imports[i].Evidence)
	}
	return out
}

func boundaryEvidence(d *ConceptualDescription) []*DescriptionEvidence {
	out := make([]*DescriptionEvidence, 0, len(d.Boundaries))
	for i := range d.Boundaries {
		out = append(out, &d.Boundaries[i].Evidence)
	}
	return out
}

func questionEvidence(d *ConceptualDescription) []*DescriptionEvidence {
	out := make([]*DescriptionEvidence, 0, len(d.OpenQuestions))
	for i := range d.OpenQuestions {
		out = append(out, &d.OpenQuestions[i].Evidence)
	}
	return out
}

func citedSegments(d ConceptualDescription) map[string]bool {
	cited := map[string]bool{}
	mark := func(evidence DescriptionEvidence) {
		for _, id := range evidence.Segments {
			cited[id] = true
		}
	}
	for _, actor := range d.Actors {
		mark(actor.Evidence)
	}
	for _, thing := range d.Things {
		mark(thing.Evidence)
		for _, property := range thing.Properties {
			mark(property.Evidence)
		}
		for _, link := range thing.Links {
			mark(link.Evidence)
		}
		for _, transition := range thing.Transitions {
			mark(transition.Evidence)
		}
	}
	for _, group := range [][]*DescriptionEvidence{ruleEvidence(&d), queryEvidence(&d), importEvidence(&d), boundaryEvidence(&d), questionEvidence(&d)} {
		for _, evidence := range group {
			mark(*evidence)
		}
	}
	return cited
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
