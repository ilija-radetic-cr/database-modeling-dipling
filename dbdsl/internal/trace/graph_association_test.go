package trace

import (
	"testing"

	"dbdsl/internal/dsl"
)

// A many-to-many relationship is drawn as the two references of its
// association table, both traced to the relationship, so the table is not a
// disconnected box next to a line that skips it.
func TestModelGraphDrawsManyToManyThroughAssociationTable(t *testing.T) {
	doc := &dsl.Document{
		Entities:      []dsl.Entity{{ID: "ENT-KNJIGA"}, {ID: "ENT-AUTOR"}, {ID: "ENT-KNJIGA-AUTOR", Kind: "association"}},
		Relationships: []dsl.Relationship{{ID: "REL-KNJIGA-AUTOR", From: "ENT-KNJIGA", To: "ENT-AUTOR", Cardinality: "many_to_many", Through: "ENT-KNJIGA-AUTOR"}},
	}
	graph := BuildModelGraph(doc)
	if len(graph.Edges) != 2 {
		t.Fatalf("expected two association references, got %+v", graph.Edges)
	}
	targets := map[string]bool{}
	for _, edge := range graph.Edges {
		if edge.From != "table:ENT-KNJIGA-AUTOR" || edge.Cardinality != "many_to_one" || edge.ElementID != "relationship:REL-KNJIGA-AUTOR" {
			t.Fatalf("unexpected association edge: %+v", edge)
		}
		targets[edge.To] = true
	}
	if !targets["table:ENT-KNJIGA"] || !targets["table:ENT-AUTOR"] || graph.Edges[0].ID == graph.Edges[1].ID {
		t.Fatalf("association references do not reach both tables with distinct IDs: %+v", graph.Edges)
	}
}
