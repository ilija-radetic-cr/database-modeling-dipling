package generate

import (
	"strings"
	"testing"

	"dbdsl/internal/dsl"
)

func TestDBMLIncludesDerivedFKsAndIndexes(t *testing.T) {
	doc := generatorFixtureDocument()

	got := DBML(doc)

	assertContains(t, got, "Table products {")
	assertContains(t, got, "print_shop_id int [not null]")
	assertContains(t, got, "(print_shop_id, code) [unique, name: 'product_code_unique_per_print_shop']")
	assertContains(t, got, "Table product_colors {")
	assertContains(t, got, "(product_id, color_id) [pk]")
	assertContains(t, got, "Ref: product_colors.product_id > products.id")
	assertContains(t, got, "Ref: product_colors.color_id > colors.id")
}

func TestFKNameNormalizesNamespacedLogicalEntityID(t *testing.T) {
	if got := fkName("ENT-CATEGORY"); got != "category_id" {
		t.Fatalf("unexpected FK name: %q", got)
	}
	if got := fkName("ProductPrintService"); got != "product_print_service_id" {
		t.Fatalf("camel-case FK name changed: %q", got)
	}
}

func TestDBMLResolvesRelationshipIDsInCompositeUniqueConstraint(t *testing.T) {
	doc := &dsl.Document{
		DSL: dsl.DSLMeta{Version: "0.5"}, Model: dsl.ModelInfo{ID: "inventory"},
		Entities: []dsl.Entity{
			{ID: "ENT-ITEM-INVENTORY", TableName: "item_inventory", Kind: "association"},
			{ID: "ENT-ITEM", TableName: "items", Kind: "regular"},
			{ID: "ENT-WAREHOUSE-FACILITY", TableName: "warehouse_facilities", Kind: "regular"},
		},
		Relationships: []dsl.Relationship{
			{ID: "REL-INVENTORY-ITEM", From: "ENT-ITEM-INVENTORY", To: "ENT-ITEM", Cardinality: "many_to_one"},
			{ID: "REL-INVENTORY-LOCATION", From: "ENT-ITEM-INVENTORY", To: "ENT-WAREHOUSE-FACILITY", Cardinality: "many_to_one"},
		},
		Constraints: []dsl.Constraint{{
			ID: "CON-INVENTORY-ITEM-LOCATION-UNIQUE", Type: "unique", Owner: "ENT-ITEM-INVENTORY",
			Fields: []string{"REL-INVENTORY-ITEM", "REL-INVENTORY-LOCATION"},
		}},
	}

	got := DBML(doc)
	assertContains(t, got, "(item_id, warehouse_facility_id) [unique, name: 'CON-INVENTORY-ITEM-LOCATION-UNIQUE']")
}

func TestTraceReportIncludesEvidenceTrail(t *testing.T) {
	doc := generatorFixtureDocument()
	source := &dsl.ReviewedSource{
		ReviewState: dsl.ReviewState{
			Status:                        "simulated_review_complete",
			AllRequiredReviewsResolved:    boolPtr(true),
			UnresolvedRequiresReviewFlags: intPtr(0),
		},
		Fragments: []dsl.SourceFragment{
			{
				ID:                "PH-CAT-001",
				Description:       "Fragment uvodi stampariju i katalog.",
				Types:             []string{"entity_candidate"},
				Decision:          "include",
				Phase2Eligibility: "eligible",
				ReviewRefs:        []string{"PH-REV-001"},
				Source:            dsl.FragmentSource{ID: "test_source", Locator: "$.printShop"},
				Text:              dsl.FragmentText{Normalized: "Stamparija ima katalog proizvoda."},
				Evidence:          dsl.FragmentEvidence{SupportLevel: "explicit", Confidence: "high"},
			},
		},
		ReviewItems: []dsl.ReviewItem{
			{
				ID:       "PH-REV-001",
				Question: "Da li modelovati stampariju?",
				Decision: dsl.ReviewItemDecision{
					Status:         "accepted",
					SelectedOption: "print_shop_only",
					Rationale:      "PoC modeluje katalog.",
				},
			},
		},
	}

	got := TraceReport(doc, source, "reviewed_source_fragments.yaml")

	assertContains(t, got, "Traceability Report")
	assertContains(t, got, "PrintShop.code")
	assertContains(t, got, "PH-CAT-001")
	assertContains(t, got, "Stamparija ima katalog proizvoda.")
	assertContains(t, got, "PH-REV-001")
}

func TestDBMLV02UsesNullableFKsAndNewTypes(t *testing.T) {
	doc := generatorFixtureDocumentV02()

	got := DBML(doc)

	assertContains(t, got, "print_shop_id int\n")
	assertContains(t, got, "unit_price decimal(12,2) [not null]")
	assertContains(t, got, "created_at datetime [not null]")
	assertContains(t, got, "image_path varchar(255)")
	assertContains(t, got, "Constraint product_code_format: code matches ^[A-Z0-9]+$")
}

func TestTraceReportV02IncludesNewSections(t *testing.T) {
	doc := generatorFixtureDocumentV02()
	source := &dsl.ReviewedSource{
		ReviewState: dsl.ReviewState{
			Status:                        "simulated_review_complete",
			AllRequiredReviewsResolved:    boolPtr(true),
			UnresolvedRequiresReviewFlags: intPtr(0),
		},
		Fragments: []dsl.SourceFragment{
			{
				ID:                "PH-CAT-001",
				Description:       "Fragment uvodi status i izvedene prikaze.",
				Types:             []string{"entity_candidate"},
				Decision:          "include",
				Phase2Eligibility: "eligible",
				Source:            dsl.FragmentSource{ID: "test_source"},
				Text:              dsl.FragmentText{Normalized: "Proizvod ima status, sliku i statistiku."},
				Evidence:          dsl.FragmentEvidence{SupportLevel: "explicit", Confidence: "high"},
			},
		},
	}

	got := TraceReport(doc, source, "reviewed_source_fragments.yaml")

	assertContains(t, got, "state_machine")
	assertContains(t, got, "ProductStatusFlow")
	assertContains(t, got, "derived_view")
	assertContains(t, got, "ProductStats")
	assertContains(t, got, "file_spec")
	assertContains(t, got, "ProductImageFile")
}

func TestDBMLV06RegularJoinEntityKeepsScalarID(t *testing.T) {
	doc := generatorFixtureDocumentV06()

	got := DBML(doc)

	assertContains(t, got, "Table product_print_services {\n  id int [pk, increment]")
	assertContains(t, got, "Ref: cart_items.product_print_service_id > product_print_services.id")
}

func TestTraceReportV06IncludesNativePipelineSections(t *testing.T) {
	bundle := &dsl.Bundle{
		Document: generatorFixtureDocumentV06(),
		SourceUnits: &dsl.SourceUnitsFile{SourceUnits: []dsl.SourceUnit{
			{ID: "SU-001", Section: "catalog", Kind: "sentence", Relevance: "model_relevant", Text: dsl.SourceUnitText{Normalized: "Product has print service."}},
		}},
		ReviewDecisions: &dsl.ReviewDecisionsFile{ReviewDecisions: []dsl.ReviewDecision{
			{ID: "RD-001", AffectedElements: []string{"ProductPrintService"}, Decision: map[string]any{"status": "accepted", "selected_option": "join_entity"}},
		}},
	}

	got := TraceReportV06(bundle)

	assertContains(t, got, "Traceability Report v0.6")
	assertContains(t, got, "Source units referenced by model")
	assertContains(t, got, "Source Unit Appendix")
	assertContains(t, got, "RD-001")
	assertContains(t, got, "SU-001")
	if strings.Contains(got, "Requirement atoms") || strings.Contains(got, "CRUD") {
		t.Fatalf("v0.6 trace must not mention atoms or CRUD:\n%s", got)
	}
}

func generatorFixtureDocument() *dsl.Document {
	return &dsl.Document{
		DSL: dsl.DSLMeta{Name: "DB-DSL", Version: "0.1"},
		Model: dsl.ModelInfo{
			ID:          "test_model",
			Name:        "Test Model",
			DomainSlice: "catalog",
			Status:      "reviewed_poc_model",
			Description: "Test model.",
		},
		Source: dsl.SourceInfo{
			ReviewedFragmentsFile: "reviewed_source_fragments.yaml",
			ReviewState:           "simulated_review_complete",
			AcceptedReviewDecisions: []dsl.AcceptedReviewDecision{
				{ReviewID: "PH-REV-001", SelectedOption: "print_shop_only"},
			},
		},
		Entities: []dsl.Entity{
			{
				ID:        "PrintShop",
				Label:     "Print shop",
				TableName: "print_shops",
				Kind:      "regular",
				Evidence:  evidence("PH-CAT-001", "PH-REV-001"),
				Attributes: []dsl.Attribute{
					{
						ID:          "code",
						Label:       "Code",
						Type:        "string",
						Required:    boolPtr(true),
						SourceField: "stampaorijaId",
						Evidence:    evidence("PH-CAT-001", "PH-REV-001"),
					},
				},
			},
			{
				ID:        "Product",
				Label:     "Product",
				TableName: "products",
				Kind:      "regular",
				Evidence:  evidence("PH-CAT-001", "PH-REV-001"),
				Attributes: []dsl.Attribute{
					{
						ID:          "code",
						Label:       "Code",
						Type:        "string",
						Required:    boolPtr(true),
						SourceField: "sifra",
						Evidence:    evidence("PH-CAT-001", "PH-REV-001"),
					},
				},
			},
			{
				ID:        "Color",
				Label:     "Color",
				TableName: "colors",
				Kind:      "lookup",
				Evidence:  evidence("PH-CAT-001", ""),
				Attributes: []dsl.Attribute{
					{
						ID:       "name",
						Label:    "Name",
						Type:     "string",
						Required: boolPtr(true),
						Evidence: evidence("PH-CAT-001", ""),
					},
				},
			},
			{
				ID:         "ProductColor",
				Label:      "Product color",
				TableName:  "product_colors",
				Kind:       "association",
				Evidence:   evidence("PH-CAT-001", ""),
				Attributes: []dsl.Attribute{},
			},
		},
		Relationships: []dsl.Relationship{
			{
				ID:          "ProductPrintShop",
				Label:       "Product print shop",
				From:        "Product",
				To:          "PrintShop",
				Cardinality: "many_to_one",
				Required:    boolPtr(true),
				Evidence:    evidence("PH-CAT-001", "PH-REV-001"),
			},
			{
				ID:          "ProductAvailableColors",
				Label:       "Product colors",
				From:        "Product",
				To:          "Color",
				Cardinality: "many_to_many",
				Through:     "ProductColor",
				Required:    boolPtr(false),
				Evidence:    evidence("PH-CAT-001", ""),
			},
		},
		Constraints: []dsl.Constraint{
			{
				ID:          "product_code_unique_per_print_shop",
				Type:        "unique",
				Owner:       "Product",
				Fields:      []string{"print_shop_id", "code"},
				Description: "Unique product code per shop.",
				Evidence:    evidence("PH-CAT-001", "PH-REV-001"),
			},
		},
		ImportSpecs: []dsl.ImportSpec{},
	}
}

func generatorFixtureDocumentV06() *dsl.Document {
	return &dsl.Document{
		DSL: dsl.DSLMeta{Name: "DB-DSL", Version: "0.6"},
		Model: dsl.ModelInfo{
			ID:          "test_model_v06",
			Name:        "Test Model v0.6",
			DomainSlice: "catalog",
			Status:      "draft_model",
			Description: "Test model.",
		},
		Entities: []dsl.Entity{
			{
				ID:        "Product",
				Label:     "Product",
				TableName: "products",
				Kind:      "regular",
				Evidence:  evidenceV06("SU-001", "RD-001"),
				Attributes: []dsl.Attribute{
					{ID: "code", Label: "Code", Type: "string", Required: boolPtr(true), Evidence: evidenceV06("SU-001", "RD-001")},
				},
			},
			{
				ID:        "PrintService",
				Label:     "Print service",
				TableName: "print_services",
				Kind:      "lookup",
				Evidence:  evidenceV06("SU-001", "RD-001"),
				Attributes: []dsl.Attribute{
					{ID: "name", Label: "Name", Type: "string", Required: boolPtr(true), Evidence: evidenceV06("SU-001", "RD-001")},
				},
			},
			{
				ID:        "ProductPrintService",
				Label:     "Product print service",
				TableName: "product_print_services",
				Kind:      "regular",
				Evidence:  evidenceV06("SU-001", "RD-001"),
				Attributes: []dsl.Attribute{
					{ID: "additional_price", Label: "Additional price", Type: "money", Required: boolPtr(true), Evidence: evidenceV06("SU-001", "RD-001")},
				},
			},
			{
				ID:        "CartItem",
				Label:     "Cart item",
				TableName: "cart_items",
				Kind:      "regular",
				Evidence:  evidenceV06("SU-001", "RD-001"),
			},
		},
		Relationships: []dsl.Relationship{
			{ID: "ProductPrintServiceProduct", Label: "Product print service product", From: "ProductPrintService", To: "Product", Cardinality: "many_to_one", Required: boolPtr(true), Evidence: evidenceV06("SU-001", "RD-001")},
			{ID: "ProductPrintServiceService", Label: "Product print service service", From: "ProductPrintService", To: "PrintService", Cardinality: "many_to_one", Required: boolPtr(true), Evidence: evidenceV06("SU-001", "RD-001")},
			{ID: "CartItemPrintService", Label: "Cart item print service", From: "CartItem", To: "ProductPrintService", Cardinality: "many_to_one", Required: boolPtr(false), Evidence: evidenceV06("SU-001", "RD-001")},
		},
	}
}

func generatorFixtureDocumentV02() *dsl.Document {
	return &dsl.Document{
		DSL: dsl.DSLMeta{Name: "DB-DSL", Version: "0.2"},
		Model: dsl.ModelInfo{
			ID:          "test_model_v02",
			Name:        "Test Model v0.2",
			DomainSlice: "catalog",
			Status:      "draft_model",
			Description: "Test model.",
		},
		Entities: []dsl.Entity{
			{
				ID:        "PrintShop",
				Label:     "Print shop",
				TableName: "print_shops",
				Kind:      "regular",
				Evidence:  evidence("PH-CAT-001", ""),
				Attributes: []dsl.Attribute{
					{
						ID:       "email",
						Label:    "Email",
						Type:     "email",
						Required: boolPtr(true),
						Evidence: evidence("PH-CAT-001", ""),
					},
				},
			},
			{
				ID:        "Product",
				Label:     "Product",
				TableName: "products",
				Kind:      "regular",
				Evidence:  evidence("PH-CAT-001", ""),
				Attributes: []dsl.Attribute{
					{
						ID:       "code",
						Label:    "Code",
						Type:     "string",
						Required: boolPtr(true),
						Evidence: evidence("PH-CAT-001", ""),
					},
					{
						ID:        "unit_price",
						Label:     "Unit price",
						Type:      "money",
						Required:  boolPtr(true),
						Precision: intPtr(12),
						Scale:     intPtr(2),
						Evidence:  evidence("PH-CAT-001", ""),
					},
					{
						ID:       "created_at",
						Label:    "Created at",
						Type:     "datetime",
						Required: boolPtr(true),
						Evidence: evidence("PH-CAT-001", ""),
					},
					{
						ID:       "image_path",
						Label:    "Image path",
						Type:     "file_path",
						Required: boolPtr(false),
						Evidence: evidence("PH-CAT-001", ""),
					},
					{
						ID:         "status",
						Label:      "Status",
						Type:       "string",
						Required:   boolPtr(true),
						EnumValues: []string{"draft", "active", "inactive"},
						Evidence:   evidence("PH-CAT-001", ""),
					},
				},
			},
		},
		Relationships: []dsl.Relationship{
			{
				ID:          "ProductPrintShop",
				Label:       "Product print shop",
				From:        "Product",
				To:          "PrintShop",
				Cardinality: "many_to_one",
				Required:    boolPtr(false),
				FKRequired:  boolPtr(false),
				OnDelete:    "set_null",
				Evidence:    evidence("PH-CAT-001", ""),
			},
		},
		Constraints: []dsl.Constraint{
			{
				ID:          "product_code_format",
				Type:        "regex",
				Owner:       "Product",
				Field:       "code",
				Pattern:     "^[A-Z0-9]+$",
				Description: "Code format.",
				Evidence:    evidence("PH-CAT-001", ""),
			},
		},
		StateMachines: []dsl.StateMachine{
			{
				ID:      "ProductStatusFlow",
				Owner:   "Product",
				Field:   "status",
				States:  []string{"draft", "active", "inactive"},
				Initial: "draft",
				Transitions: []dsl.StateTransition{
					{From: "draft", To: "active"},
					{From: "active", To: "inactive"},
				},
				Evidence: evidence("PH-CAT-001", ""),
			},
		},
		DerivedViews: []dsl.DerivedView{
			{
				ID:          "ProductStats",
				Label:       "Product stats",
				Description: "Product statistics.",
				Kind:        "aggregate",
				Sources:     []string{"Product"},
				Persistence: "virtual",
				Evidence:    evidence("PH-CAT-001", ""),
			},
		},
		FileSpecs: []dsl.FileSpec{
			{
				ID:                "ProductImageFile",
				Owner:             "Product",
				Field:             "image_path",
				AllowedExtensions: []string{"jpg", "png"},
				MaxSizeMB:         intPtr(3),
				Storage:           "path",
				Evidence:          evidence("PH-CAT-001", ""),
			},
		},
	}
}

func evidence(fragmentID, reviewID string) dsl.Evidence {
	evidence := dsl.Evidence{
		Fragments:    []string{fragmentID},
		SupportLevel: "explicit",
		Confidence:   "high",
	}
	if reviewID != "" {
		evidence.ReviewDecisions = []string{reviewID}
	}
	return evidence
}

func evidenceV06(sourceUnitID, reviewID string) dsl.Evidence {
	return dsl.Evidence{
		SourceUnits:     []string{sourceUnitID},
		ReviewDecisions: []string{reviewID},
		SupportLevel:    "explicit",
		Confidence:      "high",
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q, got:\n%s", want, got)
	}
}
