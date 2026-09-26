package trace

import (
	"reflect"
	"testing"

	"dbdsl/internal/dsl"
)

func TestEveryTraceIndexElementKindHasDetailsWithMatchingEvidence(t *testing.T) {
	evidence := func(source, review string) dsl.Evidence {
		return dsl.Evidence{SourceUnits: []string{source}, ReviewDecisions: []string{review}, SupportLevel: "explicit", Confidence: "high"}
	}
	doc := &dsl.Document{
		Entities: []dsl.Entity{{
			ID: "ENT-ITEM", Label: "Item", TableName: "items", Evidence: evidence("SU-TABLE", "RD-TABLE"),
			Attributes: []dsl.Attribute{{ID: "file", Label: "File", Type: "file_path", Evidence: evidence("SU-FIELD", "RD-FIELD")}},
		}},
		Constraints: []dsl.Constraint{{
			ID: "CON-FILE", Type: "check", Owner: "ENT-ITEM", Field: "file", Expression: "file <> ''", Description: "File is required.", Evidence: evidence("SU-CON", "RD-CON"),
		}},
		ImportSpecs: []dsl.ImportSpec{{
			ID: "IMP-ITEM", Label: "Item import", Description: "Imports items.", Format: "csv", Root: "items", Evidence: evidence("SU-IMP", "RD-IMP"),
		}},
		StateMachines: []dsl.StateMachine{{
			ID: "SM-ITEM", Owner: "ENT-ITEM", Field: "file", States: []string{"new", "ready"}, Evidence: evidence("SU-SM", "RD-SM"),
		}},
		FileSpecs: []dsl.FileSpec{{
			ID: "FS-ITEM", Owner: "ENT-ITEM", Field: "file", Storage: "path", Evidence: evidence("SU-FS", "RD-FS"),
		}},
	}

	idx := BuildTraceIndex(doc)
	cases := []struct {
		id      string
		kind    string
		source  string
		review  string
		related []string
	}{
		{id: "constraint:CON-FILE", kind: "constraint", source: "SU-CON", review: "RD-CON", related: []string{"table:ENT-ITEM", "field:ENT-ITEM.file"}},
		{id: "import_spec:IMP-ITEM", kind: "import_spec", source: "SU-IMP", review: "RD-IMP"},
		{id: "state_machine:SM-ITEM", kind: "state_machine", source: "SU-SM", review: "RD-SM", related: []string{"table:ENT-ITEM", "field:ENT-ITEM.file"}},
		{id: "file_spec:FS-ITEM", kind: "file_spec", source: "SU-FS", review: "RD-FS", related: []string{"table:ENT-ITEM", "field:ENT-ITEM.file"}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			details, found := BuildElementDetails(doc, tc.id)
			if !found {
				t.Fatalf("TraceIndex publishes %s but BuildElementDetails did not find it", tc.id)
			}
			if details.Kind != tc.kind || details.ID != tc.id {
				t.Fatalf("unexpected details identity: %+v", details)
			}
			if !reflect.DeepEqual(details.Evidence.SourceUnits, []string{tc.source}) ||
				!reflect.DeepEqual(details.Evidence.ReviewDecisions, []string{tc.review}) {
				t.Fatalf("details evidence does not match model evidence: %+v", details.Evidence)
			}
			if !reflect.DeepEqual(idx.ElementToSources[tc.id], []string{tc.source}) {
				t.Fatalf("trace index evidence mismatch for %s: %+v", tc.id, idx.ElementToSources[tc.id])
			}
			if !reflect.DeepEqual(details.RelatedElements, tc.related) {
				t.Fatalf("unexpected related elements for %s: %+v", tc.id, details.RelatedElements)
			}
		})
	}
}
