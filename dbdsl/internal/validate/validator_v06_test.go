package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/dsl"
)

func TestValidateV06ValidModel(t *testing.T) {
	modelPath := writeV06Fixture(t, baseV06Model(), baseV06SourceUnits(), baseV06Reviews())

	result := ValidateFile(modelPath)
	if !result.OK() {
		t.Fatalf("expected validation ok, got:\n%s", strings.Join(result.Errors, "\n"))
	}
}

func TestV06AttributeReferencesSupportNamespacedEntityIDs(t *testing.T) {
	v := validatorV06{
		entityByID: map[string]dsl.Entity{
			"entity.user-account": {ID: "entity.user-account"},
		},
		attributeIDs: map[string]map[string]bool{
			"entity.user-account": {"username": true},
		},
	}
	v.validateImportTarget("import mapping", "entity.user-account.username")
	if len(v.errors) != 0 {
		t.Fatalf("namespaced import target was rejected: %v", v.errors)
	}
}

func TestSplitAttributeReferenceRejectsMalformedValues(t *testing.T) {
	for _, ref := range []string{"", "username", ".username", "entity.user-account."} {
		if _, _, ok := splitAttributeReference(ref); ok {
			t.Fatalf("malformed attribute reference %q was accepted", ref)
		}
	}
}

func TestValidateV06RejectsReservedGeneratedIDAttribute(t *testing.T) {
	model := strings.Replace(baseV06Model(), `      - id: code
        label: Code`, `      - id: id
        label: ID
        description: Technical identifier.
        type: id
        required: true
        evidence: *ev
      - id: code
        label: Code`, 1)
	modelPath := writeV06Fixture(t, model, baseV06SourceUnits(), baseV06Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "attribute Product.id uses reserved generated primary key id")
}

func TestValidateV06RejectsNonLowerSnakeAttributeID(t *testing.T) {
	model := strings.Replace(baseV06Model(), `      - id: code
        label: Code`, `      - id: Product-Name
        label: Product name
        description: Product name.
        type: string
        required: true
        evidence: *ev
      - id: code
        label: Code`, 1)
	modelPath := writeV06Fixture(t, model, baseV06SourceUnits(), baseV06Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "attribute Product.Product-Name.id must be a lower snake_case column name")
}

func TestValidateV06RejectsRequiredConstraintWhenAttributeIsNullable(t *testing.T) {
	model := strings.Replace(baseV06Model(), `      - id: code
        label: Code
        description: Code.
        type: string
        required: true`, `      - id: code
        label: Code
        description: Code.
        type: string
        required: false`, 1)
	model = strings.Replace(model, `constraints:
  - id: product_code_unique_per_print_shop`, `constraints:
  - id: product_code_required
    type: required
    owner: Product
    field: code
    description: Product code is required.
    evidence: *ev
  - id: product_code_unique_per_print_shop`, 1)
	modelPath := writeV06Fixture(t, model, baseV06SourceUnits(), baseV06Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "constraint product_code_required requires Product.code but attribute.required is false")
}

func TestValidateV06UnknownEvidenceSourceUnit(t *testing.T) {
	model := strings.Replace(baseV06Model(), "source_units: [SU-001]", "source_units: [SU-999]", 1)
	modelPath := writeV06Fixture(t, model, baseV06SourceUnits(), baseV06Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "unknown source unit SU-999")
}

func TestValidateV06RejectsScalarFKToAssociationTarget(t *testing.T) {
	model := strings.Replace(baseV06Model(), "relationships:\n", `
  - id: ProductColor
    label: Product color
    description: Product color association.
    table_name: product_colors
    kind: association
    evidence: &ev
      source_units: [SU-001]
      review_decisions: [RD-001]
      support_level: explicit
      confidence: high
    attributes: []
relationships:
  - id: ProductProductColor
    label: Product product color
    description: Invalid scalar FK to association entity.
    from: Product
    to: ProductColor
    cardinality: many_to_one
    required: false
    evidence: *ev
`, 1)
	modelPath := writeV06Fixture(t, model, baseV06SourceUnits(), baseV06Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "targets association entity ProductColor without scalar id")
}

func writeV06Fixture(t *testing.T, model, sourceUnits, reviews string) string {
	t.Helper()
	dir := t.TempDir()
	writeV06File(t, dir, "source_units.yaml", sourceUnits)
	writeV06File(t, dir, "review_decisions.yaml", reviews)
	return writeV06File(t, dir, "db_model.dsl.yaml", model)
}

func writeV06File(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func baseV06Model() string {
	return `
dsl:
  name: DB-DSL
  version: "0.6"
model:
  id: test_v06
  name: Test v0.6
  status: draft_model
  description: Test v0.6 model.
source:
  source_units_file: source_units.yaml
  review_decisions_file: review_decisions.yaml
  review_state: simulated_review_complete
entities:
  - id: PrintShop
    label: Print shop
    description: Print shop.
    table_name: print_shops
    kind: regular
    evidence: &ev
      source_units: [SU-001]
      review_decisions: [RD-001]
      support_level: explicit
      confidence: high
    attributes:
      - id: name
        label: Name
        description: Name.
        type: string
        required: true
        evidence: *ev
  - id: Product
    label: Product
    description: Product.
    table_name: products
    kind: regular
    evidence: *ev
    attributes:
      - id: code
        label: Code
        description: Code.
        type: string
        required: true
        evidence: *ev
relationships:
  - id: ProductPrintShop
    label: Product print shop
    description: Product belongs to print shop.
    from: Product
    to: PrintShop
    cardinality: many_to_one
    required: true
    evidence: *ev
constraints:
  - id: product_code_unique_per_print_shop
    type: unique
    owner: Product
    fields: [print_shop_id, code]
    description: Product code is unique per print shop.
    evidence: *ev
import_specs: []
state_machines: []
derived_views: []
file_specs: []
`
}

func baseV06SourceUnits() string {
	return `
document:
  id: test_source_units
  pipeline_version: "0.6"
source_units:
  - id: SU-001
    kind: sentence
    section: catalog
    location: test#line-1
    relevance: model_relevant
    tags: [catalog]
    text:
      exact: Product belongs to print shop.
      normalized: Product belongs to print shop.
`
}

func baseV06Reviews() string {
	return `
document:
  id: test_reviews
review_state:
  status: simulated_review_complete
review_decisions:
  - id: RD-001
    question: How to model catalog?
    affected_elements: [Product]
    decision:
      status: accepted
      selected_option: catalog_tables
      rationale: Test rationale.
coverage_checks: []
`
}
