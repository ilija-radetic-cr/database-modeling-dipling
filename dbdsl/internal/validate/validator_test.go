package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateValidModel(t *testing.T) {
	modelPath := writeFixture(t, baseReviewedSource(), baseModel())

	result := ValidateFile(modelPath)
	if !result.OK() {
		t.Fatalf("expected validation ok, got:\n%s", strings.Join(result.Errors, "\n"))
	}
}

func TestValidateUnknownRelationshipEntity(t *testing.T) {
	model := strings.Replace(baseModel(), "to: PrintShop", "to: Shop", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "references unknown to entity Shop")
}

func TestValidateManyToManyRequiresThrough(t *testing.T) {
	model := strings.Replace(baseModel(), "    through: ProductColor\n", "", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "with many_to_many cardinality requires through")
}

func TestValidateThroughMustBeAssociation(t *testing.T) {
	model := strings.Replace(baseModel(), "kind: association", "kind: regular", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "through entity ProductColor must be kind: association")
}

func TestValidateConstraintUnknownField(t *testing.T) {
	model := strings.Replace(baseModel(), "field: code", "field: missing_code", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "references unknown field Product.missing_code")
}

func TestValidateConstraintDerivedFKPasses(t *testing.T) {
	modelPath := writeFixture(t, baseReviewedSource(), baseModel())

	result := ValidateFile(modelPath)
	if !result.OK() {
		t.Fatalf("expected derived FK constraint to pass, got:\n%s", strings.Join(result.Errors, "\n"))
	}
}

func TestValidateUnknownEvidenceFragment(t *testing.T) {
	model := strings.Replace(baseModel(), "PH-CAT-001", "PH-CAT-999", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "unknown fragment PH-CAT-999")
}

func TestValidateUnknownEvidenceReview(t *testing.T) {
	model := strings.Replace(baseModel(), "PH-REV-001", "PH-REV-999", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "unknown review PH-REV-999")
}

func TestValidatePendingReviewItem(t *testing.T) {
	source := strings.Replace(baseReviewedSource(), "status: accepted", "status: pending", 1)
	modelPath := writeFixture(t, source, baseModel())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "review item PH-REV-001 is pending")
}

func TestValidateValidV02Model(t *testing.T) {
	modelPath := writeFixture(t, baseReviewedSource(), baseModelV02())

	result := ValidateFile(modelPath)
	if !result.OK() {
		t.Fatalf("expected validation ok, got:\n%s", strings.Join(result.Errors, "\n"))
	}
}

func TestValidateV02RequiresNewTopLevelSections(t *testing.T) {
	model := strings.Replace(baseModelV02(), "state_machines:\n", "", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "missing top-level section state_machines")
}

func TestValidateV02SetNullRequiresNullableFK(t *testing.T) {
	model := strings.Replace(baseModelV02(), "fk_required: false\n    on_delete: set_null", "fk_required: true\n    on_delete: set_null", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "cannot use on_delete set_null with fk_required=true")
}

func TestValidateV02InvalidRegexPattern(t *testing.T) {
	model := strings.Replace(baseModelV02(), "pattern: \"^[A-Z0-9]+$\"", "pattern: \"[\"", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "has invalid regex pattern")
}

func TestValidateV02StateMachineUnknownState(t *testing.T) {
	model := strings.Replace(baseModelV02(), "to: active", "to: archived", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "to state archived is not in states")
}

func TestValidateV02DerivedViewUnknownSource(t *testing.T) {
	model := strings.Replace(baseModelV02(), "sources: [Product, PrintShop]", "sources: [Product, MissingShop]", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "references unknown source entity MissingShop")
}

func TestValidateV02FileSpecUnknownField(t *testing.T) {
	model := strings.Replace(baseModelV02(), "field: image_path", "field: missing_path", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "references unknown field Product.missing_path")
}

func TestValidateV02ConditionalRequiredNeedsCondition(t *testing.T) {
	model := strings.Replace(baseModelV02(), "    condition:\n      field: status\n      operator: equals\n      value: active\n", "", 1)
	modelPath := writeFixture(t, baseReviewedSource(), model)

	result := ValidateFile(modelPath)
	assertHasError(t, result, "requires condition")
}

func writeFixture(t *testing.T, sourceYAML, modelYAML string) string {
	t.Helper()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "reviewed_source_fragments.yaml")
	modelPath := filepath.Join(dir, "db_model.dsl.yaml")

	if err := os.WriteFile(sourcePath, []byte(sourceYAML), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	modelYAML = strings.ReplaceAll(modelYAML, "__SOURCE_PATH__", sourcePath)
	if err := os.WriteFile(modelPath, []byte(modelYAML), 0o600); err != nil {
		t.Fatalf("write model fixture: %v", err)
	}

	return modelPath
}

func assertHasError(t *testing.T, result Result, want string) {
	t.Helper()

	for _, got := range result.Errors {
		if strings.Contains(got, want) {
			return
		}
	}
	t.Fatalf("expected error containing %q, got:\n%s", want, strings.Join(result.Errors, "\n"))
}

func baseReviewedSource() string {
	return `
review_state:
  status: simulated_review_complete
  all_required_reviews_resolved: true
  unresolved_requires_review_flags: 0
fragments:
  - id: PH-CAT-001
    decision: include
    phase_2_eligibility: eligible
    review_refs:
      - PH-REV-001
  - id: PH-CAT-002
    decision: include
    phase_2_eligibility: eligible
    review_refs:
      - PH-REV-001
review_items:
  - id: PH-REV-001
    decision:
      status: accepted
      selected_option: accepted_option
`
}

func baseModel() string {
	return `
dsl:
  name: "DB-DSL"
  version: "0.1"
model:
  id: test_model
  name: Test Model
  status: reviewed_poc_model
  description: Test model.
source:
  reviewed_fragments_file: "__SOURCE_PATH__"
  review_state: simulated_review_complete
  accepted_review_decisions:
    - review_id: PH-REV-001
      selected_option: accepted_option
entities:
  - id: PrintShop
    label: Print shop
    description: Print shop.
    table_name: print_shops
    kind: regular
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
    attributes:
      - id: code
        label: Code
        description: Code.
        type: string
        required: true
        evidence:
          fragments: [PH-CAT-001]
          review_decisions: [PH-REV-001]
          support_level: explicit
          confidence: high
  - id: Product
    label: Product
    description: Product.
    table_name: products
    kind: regular
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
    attributes:
      - id: code
        label: Code
        description: Code.
        type: string
        required: true
        evidence:
          fragments: [PH-CAT-001]
          review_decisions: [PH-REV-001]
          support_level: explicit
          confidence: high
  - id: Color
    label: Color
    description: Color.
    table_name: colors
    kind: lookup
    evidence:
      fragments: [PH-CAT-002]
      review_decisions: []
      support_level: explicit
      confidence: high
    attributes:
      - id: name
        label: Name
        description: Name.
        type: string
        required: true
        evidence:
          fragments: [PH-CAT-002]
          review_decisions: []
          support_level: explicit
          confidence: high
  - id: ProductColor
    label: Product color
    description: Product color association.
    table_name: product_colors
    kind: association
    evidence:
      fragments: [PH-CAT-002]
      review_decisions: []
      support_level: inferred
      confidence: high
    attributes: []
relationships:
  - id: ProductPrintShop
    label: Product print shop
    description: Product belongs to print shop.
    from: Product
    to: PrintShop
    cardinality: many_to_one
    required: true
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: inferred
      confidence: high
  - id: ProductAvailableColors
    label: Product colors
    description: Product colors.
    from: Product
    to: Color
    cardinality: many_to_many
    through: ProductColor
    required: false
    evidence:
      fragments: [PH-CAT-002]
      review_decisions: []
      support_level: inferred
      confidence: high
constraints:
  - id: product_code_required
    type: required
    owner: Product
    field: code
    description: Product code is required.
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: inferred
      confidence: high
  - id: product_code_unique_per_shop
    type: unique
    owner: Product
    fields: [print_shop_id, code]
    description: Product code is unique per shop.
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: inferred
      confidence: high
import_specs:
  - id: TestImport
    label: Test import
    description: Test import.
    format: json
    source:
      fragment: PH-CAT-001
      source_id: test_source
    root: "$"
    mappings:
      - source_path: "$.code"
        target: Product.code
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
`
}

func baseModelV02() string {
	return `
dsl:
  name: "DB-DSL"
  version: "0.2"
model:
  id: test_model_v02
  name: Test Model v0.2
  status: draft_model
  description: Test model.
source:
  reviewed_fragments_file: "__SOURCE_PATH__"
  review_state: simulated_review_complete
  accepted_review_decisions:
    - review_id: PH-REV-001
      selected_option: accepted_option
entities:
  - id: PrintShop
    label: Print shop
    description: Print shop.
    table_name: print_shops
    kind: regular
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
    attributes:
      - id: email
        label: Email
        description: Email.
        type: email
        required: true
        evidence:
          fragments: [PH-CAT-001]
          review_decisions: [PH-REV-001]
          support_level: explicit
          confidence: high
  - id: Product
    label: Product
    description: Product.
    table_name: products
    kind: regular
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
    attributes:
      - id: code
        label: Code
        description: Code.
        type: string
        required: true
        evidence:
          fragments: [PH-CAT-001]
          review_decisions: [PH-REV-001]
          support_level: explicit
          confidence: high
      - id: status
        label: Status
        description: Product status.
        type: string
        required: true
        enum_values: [draft, active, inactive]
        evidence:
          fragments: [PH-CAT-001]
          review_decisions: [PH-REV-001]
          support_level: explicit
          confidence: high
      - id: unit_price
        label: Unit price
        description: Unit price.
        type: money
        required: true
        precision: 12
        scale: 2
        evidence:
          fragments: [PH-CAT-001]
          review_decisions: [PH-REV-001]
          support_level: explicit
          confidence: high
      - id: created_at
        label: Created at
        description: Creation timestamp.
        type: datetime
        required: true
        evidence:
          fragments: [PH-CAT-001]
          review_decisions: [PH-REV-001]
          support_level: explicit
          confidence: high
      - id: image_path
        label: Image path
        description: Product image path.
        type: file_path
        required: false
        evidence:
          fragments: [PH-CAT-002]
          review_decisions: []
          support_level: explicit
          confidence: high
relationships:
  - id: ProductPrintShop
    label: Product print shop
    description: Product may belong to print shop.
    from: Product
    to: PrintShop
    cardinality: many_to_one
    required: false
    fk_required: false
    on_delete: set_null
    identifying: false
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: inferred
      confidence: high
constraints:
  - id: product_code_format
    type: regex
    owner: Product
    field: code
    pattern: "^[A-Z0-9]+$"
    description: Product code format.
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
  - id: product_code_length
    type: length
    owner: Product
    field: code
    min: 3
    max: 20
    description: Product code length.
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
  - id: product_price_max
    type: max_inclusive
    owner: Product
    field: unit_price
    value: 100000
    description: Product price maximum.
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
  - id: active_product_image_required
    type: conditional_required
    owner: Product
    condition:
      field: status
      operator: equals
      value: active
    requires:
      - kind: field
        field: image_path
    description: Active product requires image.
    evidence:
      fragments: [PH-CAT-002]
      review_decisions: []
      support_level: inferred
      confidence: high
import_specs:
  - id: ProductImport
    label: Product import
    description: Product import.
    format: csv
    source:
      fragment: PH-CAT-001
      source_id: product_csv
    root: "$[*]"
    mappings:
      - source_path: "$.code"
        target: Product.code
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
state_machines:
  - id: ProductStatusFlow
    owner: Product
    field: status
    states: [draft, active, inactive]
    initial: draft
    terminal: [inactive]
    transitions:
      - from: draft
        to: active
      - from: active
        to: inactive
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: explicit
      confidence: high
derived_views:
  - id: ProductStats
    label: Product stats
    description: Product statistics.
    kind: aggregate
    sources: [Product, PrintShop]
    persistence: virtual
    metrics: [product_count]
    evidence:
      fragments: [PH-CAT-001]
      review_decisions: [PH-REV-001]
      support_level: inferred
      confidence: high
file_specs:
  - id: ProductImageFile
    owner: Product
    field: image_path
    allowed_extensions: [jpg, png]
    max_size_mb: 3
    storage: path
    evidence:
      fragments: [PH-CAT-002]
      review_decisions: []
      support_level: explicit
      confidence: high
`
}
