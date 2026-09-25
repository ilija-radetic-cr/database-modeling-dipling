package llmpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/generate"
	"dbdsl/internal/llm"
	"dbdsl/internal/validate"
)

func segmentDocument() CombinedDocumentProposal {
	segments := []struct{ role, text string }{
		{"page_header", "Електротехнички факултет Универзитета у Београду"},
		{"heading", "Регистрација"},
		{"list", "Регистрација треба да омогући да нови корисник унесе следеће податке:\n- име,\n- корисничко име1 ,\n- тајанствено питање."},
		{"sentence", "Нови корисник уноси и е-пошту."},
		{"sentence", "Корисник уноси корисничко име."},
		{"sentence", "Корисник бира тајанствено питање и одговор."},
		{"sentence", "Администратор разматра захтев, а исход може бити прихватање или одбацивање."},
		{"footnote", "1 Корисничко име мора бити јединствено у овом систему."},
		{"sentence", "Такмичар може одиграти највише једну игру дана."},
		{"other", "СРЕЋАН РАД!"},
	}
	proposal := SourceSegmentationProposal{}
	for i, segment := range segments {
		proposal.Segments = append(proposal.Segments, SourceSegmentationSegment{ID: fmt.Sprintf("SU-%03d", i+1), Type: segment.role, Text: segment.text})
	}
	return combinedDocumentFromSegments(proposal)
}

func acceptedUnits(t *testing.T) []dsl.SourceUnit {
	t.Helper()
	proposal, qa := BuildSourceUnitsFromSegments(segmentDocument())
	if !qa.OK {
		t.Fatalf("segment units failed QA: %v", qa.Errors)
	}
	units := make([]dsl.SourceUnit, 0, len(proposal.SourceUnits))
	for _, unit := range proposal.SourceUnits {
		units = append(units, dsl.SourceUnit{ID: unit.ID, Kind: unit.Kind, Tags: unit.Tags,
			Text: dsl.SourceUnitText{Exact: unit.ExactText, Normalized: unit.NormalizedText, Normalization: unit.Normalization}})
	}
	return units
}

func TestBuildSourceUnitsFromSegmentsKeepsTypeAndText(t *testing.T) {
	units := acceptedUnits(t)
	list := units[2]
	if list.ID != "SU-003" || list.Kind != "list" || list.Text.Exact != segmentDocument().Sentences[2].Text || list.Text.Normalized != list.Text.Exact {
		t.Fatalf("segment was not kept unchanged: %+v", list)
	}
	if list.Text.Normalization.Strategy != SegmentTextNormalization {
		t.Fatalf("segment text must not be normalized: %+v", list.Text.Normalization)
	}
}

func TestRenderSegmentsForConceptualModel(t *testing.T) {
	rendered := RenderSegmentsForConceptualModel(acceptedUnits(t))
	for _, want := range []string{
		"SU-003 [list] Регистрација треба да омогући да нови корисник унесе следеће податке:\n- име,",
		"SU-008 [footnote] 1 Корисничко",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered segments miss %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Електротехнички") {
		t.Fatalf("page headers must not be sent to the model:\n%s", rendered)
	}
}

func testDescription() ConceptualDescription {
	ev := func(ids ...string) DescriptionEvidence { return DescriptionEvidence{Segments: ids, Mode: "direct"} }
	return ConceptualDescription{
		Actors: []DescriptionActor{{ID: "korisnik", Name: "Корисник", RepresentedBy: "korisnik", Evidence: ev("SU-003")}},
		Things: []DescriptionThing{
			{ID: "korisnik", Name: "Корисник", Kind: "object", IdentifiedBy: DescriptionRefs{"korisnicko_ime"}, Evidence: ev("SU-003"), Properties: []DescriptionProperty{
				{Name: "ime", ValueType: "text", Shape: "single", Presence: "required", Origin: "entered", Evidence: ev("SU-004")},
				{Name: "korisnicko_ime", ValueType: "text", Shape: "single", Presence: "required", Origin: "entered", Evidence: ev("SU-005", "SU-008")},
				{Name: "email", ValueType: "email", Shape: "single", Presence: "optional", Origin: "entered", Evidence: ev("SU-003")},
				{Name: "тајанствено питање", Shape: "composite", Parts: []string{"питање", "одговор"}, Presence: "required", Origin: "entered", Evidence: ev("SU-006")},
				{Name: "aktivan", Shape: "single", Presence: "required", Origin: "entered", AllowedValues: []string{"да", "не"}, Evidence: ev("SU-007")},
				{Name: "број одиграних игара", Shape: "single", Origin: "derived", Source: "број игара", Evidence: ev("SU-009")},
			}},
			{ID: "zahtev", Name: "Захтев за регистрацију", Kind: "record", Evidence: ev("SU-007"),
				States:      []string{"на чекању", "прихваћен", "одбијен"},
				Transitions: []DescriptionTransition{{From: "на чекању", To: "прихваћен", Evidence: ev("SU-007")}, {From: "на чекању", To: "одбијен", Evidence: ev("SU-007")}},
				Links:       []DescriptionLink{{To: "korisnik", Meaning: "подноси", PerThis: "1", PerOther: "0..N", Evidence: ev("SU-007")}},
				Properties:  []DescriptionProperty{{Name: "vreme_podnosenja", ValueType: "datetime", Shape: "single", Presence: "required", Origin: "generated", Evidence: ev("SU-007")}}},
			{ID: "igra", Name: "Игра дана", Kind: "record", IdentifiedBy: DescriptionRefs{"datum", "korisnik"}, Evidence: ev("SU-009"),
				Properties: []DescriptionProperty{{Name: "datum", ValueType: "date", Shape: "single", Presence: "required", Origin: "entered", Evidence: ev("SU-009")}},
				Links:      []DescriptionLink{{To: "korisnik", Meaning: "игра", PerThis: "1", PerOther: "0..N", Evidence: ev("SU-009")}}},
		},
		Rules: []DescriptionRule{
			{ID: "jedna_igra", Kind: "quantity", Statement: "Највише једна игра дневно.", AppliesTo: []string{"igra"}, Evidence: ev("SU-009")},
			{ID: "jedinstven_email", Kind: "uniqueness", Statement: "Е-пошта је јединствена.", AppliesTo: []string{"korisnik.email"}, Evidence: ev("SU-003")},
		},
		Excluded: []DescriptionExcluded{{Segment: "SU-010", Reason: "other"}},
	}
}

func TestConceptualDescriptionValidationAndCoverage(t *testing.T) {
	description := testDescription()
	description.Things[1].Evidence.Segments = append(description.Things[1].Evidence.Segments, "SU-999")
	description.Things[2].Links = append(description.Things[2].Links, DescriptionLink{To: "korisnik", Meaning: "преко актера"})
	qa := ValidateConceptualDescription(&description, acceptedUnits(t))
	if !qa.OK {
		t.Fatalf("valid description rejected: %v", qa.Errors)
	}
	if qa.Coverage["uncovered_segments"] != 0 {
		t.Fatalf("coverage should be complete: %v %v", qa.Coverage, qa.Warnings)
	}
	if !strings.Contains(strings.Join(qa.Warnings, " "), "SU-999") {
		t.Fatalf("unknown segment should be reported and dropped: %v", qa.Warnings)
	}
	broken := testDescription()
	broken.Things[1].Links[0].To = "nepostoji"
	if qa := ValidateConceptualDescription(&broken, acceptedUnits(t)); qa.OK {
		t.Fatalf("a link to an unknown thing must be an error")
	}
	uncovered := testDescription()
	uncovered.Excluded = nil
	if qa := ValidateConceptualDescription(&uncovered, acceptedUnits(t)); qa.Coverage["uncovered_segments"] != 1 {
		t.Fatalf("SU-010 should be reported uncovered: %v", qa.Coverage)
	}
}

func TestConceptualDescriptionTransformsIntoValidDBDSL(t *testing.T) {
	units := acceptedUnits(t)
	description := testDescription()
	if qa := ValidateConceptualDescription(&description, units); !qa.OK {
		t.Fatalf("description rejected: %v", qa.Errors)
	}
	model := ConceptualDescriptionToModel(description, units)
	if qa := ValidateConceptualModel(model, units, nil); !qa.OK {
		t.Fatalf("transformed conceptual model fails the conceptual validator: %v", qa.Errors)
	}
	user := model.EntityConcepts[0]
	names := map[string]ConceptualAttributeProposal{}
	for _, attribute := range user.Attributes {
		names[attribute.Name] = attribute
	}
	if _, ok := names["tajanstveno_pitanje_pitanje"]; !ok {
		t.Fatalf("composite property should become one attribute per part: %+v", user.Attributes)
	}
	uniqueKeys := map[string]bool{}
	for _, constraint := range model.ConstraintConcepts {
		if constraint.Kind == "uniqueness" {
			uniqueKeys[strings.Join(constraint.Targets, "+")] = true
		}
	}
	for _, key := range []string{"ATTR-KORISNIK-KORISNICKO-IME", "ATTR-KORISNIK-EMAIL", "ATTR-IGRA-DATUM+REL-IGRA-KORISNIK"} {
		if !uniqueKeys[key] {
			t.Fatalf("unique key %s missing; keys=%v", key, uniqueKeys)
		}
	}
	if names["aktivan"].ValueType != "boolean" || len(names["aktivan"].EnumValues) != 0 || names["email"].ValueType != "email" {
		t.Fatalf("value types not derived: %+v %+v", names["aktivan"], names["email"])
	}
	if _, ok := names["broj_odigranih_igara"]; ok || len(model.DerivedConcepts) != 1 {
		t.Fatalf("derived property must become a derived concept, not a column")
	}
	if len(model.LifecycleConcepts) != 1 || len(model.LifecycleConcepts[0].Terminal) != 2 {
		t.Fatalf("states should become a lifecycle with two terminal states: %+v", model.LifecycleConcepts)
	}
	patch, _, err := MapConceptualToLogical(model, LogicalMappingOptions{Language: LanguageSerbianCyrillic})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := BuildLogicalArtifacts("test", units, patch, []dsl.ReviewDecision{})
	if err != nil {
		t.Fatal(err)
	}
	uniqueFields := map[string]bool{}
	for _, constraint := range artifacts.Model.Constraints {
		if constraint.Type == "unique" {
			uniqueFields[constraint.Owner+":"+constraint.Field+strings.Join(constraint.Fields, ",")] = true
		}
	}
	if len(uniqueFields) != 3 {
		t.Fatalf("expected three unique constraints in DB-DSL, got %v", uniqueFields)
	}
	sourceFile := dsl.SourceUnitsFile{SourceUnits: units}
	bundle := &dsl.Bundle{Document: &artifacts.Model, SourceUnits: &sourceFile, ReviewDecisions: &artifacts.ReviewDecisions}
	if result := validate.ValidateV06Bundle(bundle); len(result.Errors) > 0 {
		t.Fatalf("DB-DSL bundle is invalid:\n%s", strings.Join(result.Errors, "\n"))
	}
}

func TestConceptualTransformIndexesCriteriaNumericValuesAndLinkKeys(t *testing.T) {
	units := acceptedUnits(t)
	description := testDescription()
	ev := DescriptionEvidence{Segments: []string{"SU-009"}, Mode: "direct"}
	description.Things = append(description.Things, DescriptionThing{
		ID: "ocena", Name: "Оцена", Kind: "record", IdentifiedBy: DescriptionRefs{"igra", "korisnik"}, Evidence: ev,
		Properties: []DescriptionProperty{
			{Name: "vrednost", ValueType: "integer", Shape: "single", Presence: "required", Origin: "entered", AllowedValues: []string{"1", "2", "3", "4", "5"}, Evidence: ev},
			{Name: "tezina", ValueType: "decimal", Shape: "single", Presence: "required", Origin: "entered", AllowedValues: []string{"0,5", "1", "2"}, Evidence: ev},
		},
		Links: []DescriptionLink{
			{To: "igra", Meaning: "оцењује", PerThis: "1", PerOther: "0..N", Evidence: ev},
			{To: "korisnik", Meaning: "даје", PerThis: "1", PerOther: "0..N", Evidence: ev},
		},
	})
	description.Queries = append(description.Queries, DescriptionQuery{ID: "igre_po_danu", Description: "Игре изабраног дана.",
		Needs: []string{"igra.datum", "ocena.vrednost"}, Criteria: []string{"igra.datum", "ocena.vrednost"}, Evidence: ev})
	if qa := ValidateConceptualDescription(&description, units); !qa.OK {
		t.Fatalf("description rejected: %v", qa.Errors)
	}
	model := ConceptualDescriptionToModel(description, units)

	for _, derived := range model.DerivedConcepts {
		if derived.ID == "DER-IGRE-PO-DANU" {
			t.Fatalf("a query must not become a derived concept: %+v", derived)
		}
	}
	indexes := map[string]ConceptualIndexProposal{}
	for _, index := range model.IndexConcepts {
		indexes[index.Targets[0]] = index
	}
	if indexes["ATTR-IGRA-DATUM"].Owner != "ENT-IGRA" || indexes["ATTR-OCENA-VREDNOST"].Owner != "ENT-OCENA" || len(indexes) != 2 {
		t.Fatalf("each criterion must become an index on its attribute: %+v", model.IndexConcepts)
	}
	checks := map[string]ConceptualConstraintProposal{}
	for _, constraint := range model.ConstraintConcepts {
		if constraint.Kind == "check" && constraint.Expression != "" {
			checks[constraint.Targets[0]] = constraint
		}
	}
	for _, entity := range model.EntityConcepts {
		for _, attribute := range entity.Attributes {
			if attribute.ID == "ATTR-OCENA-VREDNOST" && (attribute.ValueType != "integer" || len(attribute.EnumValues) != 0) {
				t.Fatalf("numeric values must keep the numeric type: %+v", attribute)
			}
		}
	}
	if got := checks["ATTR-OCENA-VREDNOST"].Expression; got != "vrednost BETWEEN 1 AND 5" {
		t.Fatalf("consecutive integers must become a range check, got %q", got)
	}
	if got := checks["ATTR-OCENA-TEZINA"].Expression; got != "tezina IN (0.5, 1, 2)" {
		t.Fatalf("other numeric values must become a list check, got %q", got)
	}

	patch, _, err := MapConceptualToLogical(model, LogicalMappingOptions{Language: LanguageSerbianCyrillic})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := BuildLogicalArtifacts("test", units, patch, []dsl.ReviewDecision{})
	if err != nil {
		t.Fatal(err)
	}
	var searchIndexes []string
	for _, index := range artifacts.Model.Indexes {
		searchIndexes = append(searchIndexes, index.Owner+":"+strings.Join(index.Fields, ","))
	}
	// igra.datum leads the unique key (datum, korisnik) and needs no second index.
	if strings.Join(searchIndexes, ";") != "ENT-OCENA:vrednost" {
		t.Fatalf("unexpected search indexes: %v", searchIndexes)
	}
	if dbml := generate.DBML(&artifacts.Model); !strings.Contains(dbml, "vrednost [name: 'IDX-OCENA-VREDNOST']") {
		t.Fatalf("DBML misses the search index:\n%s", dbml)
	}
	if sql := generate.PostgreSQL(&artifacts.Model); !strings.Contains(sql, "CREATE INDEX idx_ocena_vrednost ON ocena (vrednost); -- IDX-OCENA-VREDNOST") {
		t.Fatalf("SQL misses the search index:\n%s", sql)
	}
	sourceFile := dsl.SourceUnitsFile{SourceUnits: units}
	bundle := &dsl.Bundle{Document: &artifacts.Model, SourceUnits: &sourceFile, ReviewDecisions: &artifacts.ReviewDecisions}
	if result := validate.ValidateV06Bundle(bundle); len(result.Errors) > 0 {
		t.Fatalf("DB-DSL bundle with indexes is invalid:\n%s", strings.Join(result.Errors, "\n"))
	}
	var linkKey, rangeCheck bool
	for _, constraint := range artifacts.Model.Constraints {
		if constraint.Owner != "ENT-OCENA" {
			continue
		}
		if constraint.Type == "unique" && len(constraint.Fields) == 2 {
			linkKey = true
		}
		if constraint.Type == "check" && constraint.Expression == "vrednost BETWEEN 1 AND 5" {
			rangeCheck = true
		}
	}
	if !linkKey || !rangeCheck {
		t.Fatalf("link-only key or value check missing from DB-DSL: %+v", artifacts.Model.Constraints)
	}
	for _, entity := range artifacts.Model.Entities {
		if strings.HasSuffix(entity.TableName, "s") {
			t.Fatalf("Serbian table names must not get an English plural: %s", entity.TableName)
		}
	}
}

func TestRunConceptualDescriptionWithMockClient(t *testing.T) {
	units := acceptedUnits(t)
	description, qa, err := RunConceptualDescription(context.Background(), llm.NewDefaultMockClient(), ConceptualDescriptionOptions{OutDir: t.TempDir(), SourceUnits: units})
	if err != nil {
		t.Fatal(err)
	}
	if !qa.OK || len(description.Things) != 2 || qa.Coverage["uncovered_segments"] != 0 {
		data, _ := json.MarshalIndent(description, "", "  ")
		t.Fatalf("mock description unexpected: %v %v\n%s", qa.Errors, qa.Coverage, data)
	}
	if !strings.Contains(conceptualDescriptionInstructions, "Denormalizovan") {
		t.Fatalf("the agreed Serbian conceptual prompt must be used")
	}
}

func TestDescriptionRefsReadLegacyText(t *testing.T) {
	var thing DescriptionThing
	if err := json.Unmarshal([]byte(`{"identified_by": "Јединствено корисничко име"}`), &thing); err != nil || len(thing.IdentifiedBy) != 1 {
		t.Fatalf("legacy identified_by not read: %v %v", thing.IdentifiedBy, err)
	}
	if err := json.Unmarshal([]byte(`{"identified_by": ["sifra", "preduzece"]}`), &thing); err != nil || len(thing.IdentifiedBy) != 2 {
		t.Fatalf("list identified_by not read: %v %v", thing.IdentifiedBy, err)
	}
}

func TestConceptualTransformResolvesLinksAndLifecycles(t *testing.T) {
	units := acceptedUnits(t)
	ev := DescriptionEvidence{Segments: []string{"SU-004"}, Mode: "direct"}
	text := func(name string) DescriptionProperty {
		return DescriptionProperty{Name: name, ValueType: "text", Shape: "single", Presence: "required", Origin: "entered", Evidence: ev}
	}
	description := ConceptualDescription{
		Actors: []DescriptionActor{{ID: "nastavnik", Name: "Наставник", RepresentedBy: "nalog", Evidence: ev}},
		Things: []DescriptionThing{
			{ID: "nalog", Name: "Налог", Kind: "object", Evidence: ev, Properties: []DescriptionProperty{text("ime")}},
			// The subject states the teacher link from its own side first, with counts
			// that contradict the assignment record.
			{ID: "predmet", Name: "Предмет", Kind: "object", Evidence: ev, Properties: []DescriptionProperty{text("naziv")},
				Links:  []DescriptionLink{{To: "angazovanje", Meaning: "наставници", PerThis: "1..N", PerOther: "0..N", Evidence: ev}},
				States: []string{"активан"}},
			{ID: "angazovanje", Name: "Ангажовање", Kind: "record", IdentifiedBy: DescriptionRefs{"nastavnik", "predmet"}, Evidence: ev,
				Links: []DescriptionLink{
					{To: "nalog", Meaning: "наставник", PerThis: "1", PerOther: "0..N", Evidence: ev},
					{To: "predmet", Meaning: "предмет", PerThis: "1", PerOther: "1..N", Evidence: ev},
				}},
			{ID: "stavka", Name: "Ставка", Kind: "record", Evidence: ev, Properties: []DescriptionProperty{text("opis")},
				Links: []DescriptionLink{{To: "rad", Meaning: "обухваћени рад", PerThis: "1..N", PerOther: "0..1", Evidence: ev}}},
			{ID: "rad", Name: "Рад", Kind: "record", Evidence: ev, Properties: []DescriptionProperty{text("opis")}},
		},
	}
	model := ConceptualDescriptionToModel(description, units)

	byPair := map[string]ConceptualRelationshipProposal{}
	for _, rel := range model.Relationships {
		byPair[rel.From+">"+rel.To] = rel
	}
	if _, ok := byPair["ENT-PREDMET>ENT-ANGAZOVANJE"]; ok {
		t.Fatalf("the contradicting many-to-many statement must be dropped: %+v", model.Relationships)
	}
	kept := byPair["ENT-ANGAZOVANJE>ENT-PREDMET"]
	if kept.ID != "REL-ANGAZOVANJE-PREDMET" || kept.Cardinality != "many_to_one" || kept.Required == nil || !*kept.Required {
		t.Fatalf("the link holding the foreign key must be kept: %+v", kept)
	}
	keyed := false
	for _, constraint := range model.ConstraintConcepts {
		if constraint.ID == "CON-ID-ANGAZOVANJE" && strings.Join(sortedCopy(constraint.Targets), ",") == "REL-ANGAZOVANJE-NALOG,REL-ANGAZOVANJE-PREDMET" {
			keyed = true
		}
	}
	if !keyed {
		t.Fatalf("the assignment identity must become a key over both links: %+v", model.ConstraintConcepts)
	}
	covers := byPair["ENT-STAVKA>ENT-RAD"]
	if covers.Cardinality != "one_to_many" || covers.Required == nil || *covers.Required {
		t.Fatalf("work recorded before any settlement item needs an optional foreign key: %+v", covers)
	}
	for _, lifecycle := range model.LifecycleConcepts {
		if lifecycle.Owner == "ENT-PREDMET" {
			t.Fatalf("a single state is not a lifecycle: %+v", lifecycle)
		}
	}
}
