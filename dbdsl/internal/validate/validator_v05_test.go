package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateV05ValidModel(t *testing.T) {
	modelPath := writeV05Fixture(t, baseV05Model(), baseV05SourceUnits(), baseV05RequirementAtoms(), baseV05Functional(), baseV05CRUD(), baseV05Reviews())

	result := ValidateFile(modelPath)
	if !result.OK() {
		t.Fatalf("expected validation ok, got:\n%s", strings.Join(result.Errors, "\n"))
	}
}

func TestValidateV05RejectsReservedGeneratedIDAttribute(t *testing.T) {
	model := strings.Replace(baseV05Model(), `      - id: code
        label: Code`, `      - id: id
        label: ID
        description: Technical identifier.
        type: id
        required: true
        evidence: *ev
      - id: code
        label: Code`, 1)
	modelPath := writeV05Fixture(t, model, baseV05SourceUnits(), baseV05RequirementAtoms(), baseV05Functional(), baseV05CRUD(), baseV05Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "attribute Product.id uses reserved generated primary key id")
}

func TestValidateV05RejectsNonLowerSnakeAttributeID(t *testing.T) {
	model := strings.Replace(baseV05Model(), `      - id: code
        label: Code`, `      - id: Product-Name
        label: Product name
        description: Product name.
        type: string
        required: true
        evidence: *ev
      - id: code
        label: Code`, 1)
	modelPath := writeV05Fixture(t, model, baseV05SourceUnits(), baseV05RequirementAtoms(), baseV05Functional(), baseV05CRUD(), baseV05Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "attribute Product.Product-Name.id must be a lower snake_case column name")
}

func TestValidateV05RejectsRequiredConstraintWhenAttributeIsNullable(t *testing.T) {
	model := strings.Replace(baseV05Model(), `      - id: code
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
	modelPath := writeV05Fixture(t, model, baseV05SourceUnits(), baseV05RequirementAtoms(), baseV05Functional(), baseV05CRUD(), baseV05Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "constraint product_code_required requires Product.code but attribute.required is false")
}

func TestValidateV05UnknownEvidenceSourceUnit(t *testing.T) {
	model := strings.Replace(baseV05Model(), "source_units: [SU-001]", "source_units: [SU-999]", 1)
	modelPath := writeV05Fixture(t, model, baseV05SourceUnits(), baseV05RequirementAtoms(), baseV05Functional(), baseV05CRUD(), baseV05Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "unknown source unit SU-999")
}

func TestValidateV05RepresentedAtomMustBeReferencedByModel(t *testing.T) {
	atoms := strings.Replace(baseV05RequirementAtoms(), "coverage_checks: []", `
  - id: RA-002
    statement: Unreferenced represented atom.
    atom_type: entity
    modeling_relevance: direct_db
    source_units: [SU-001]
    functional_area: catalog
    functional_pattern: catalog_management
    support_level: explicit
    confidence: high
    requires_review: false
    review_decisions: []
    model_impacts:
      entities: [Product]
    modeling_outcome:
      status: represented
coverage_checks: []`, 1)
	modelPath := writeV05Fixture(t, baseV05Model(), baseV05SourceUnits(), atoms, baseV05Functional(), baseV05CRUD(), baseV05Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "requirement atom RA-002 is represented but no model element evidence references it")
}

func TestValidateV05CRUDOperationRequiresKnownActor(t *testing.T) {
	crud := strings.Replace(baseV05CRUD(), "actor: system", "actor: missing_actor", 1)
	modelPath := writeV05Fixture(t, baseV05Model(), baseV05SourceUnits(), baseV05RequirementAtoms(), baseV05Functional(), crud, baseV05Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "CRUD operation manage_catalog references unknown actor missing_actor")
}

func TestValidateV05RejectsScalarFKToAssociationTarget(t *testing.T) {
	model := strings.Replace(baseV05Model(), "relationships:\n", `
  - id: ProductColor
    label: Product color
    description: Product color association.
    table_name: product_colors
    kind: association
    evidence: &ev
      source_units: [SU-001]
      requirement_atoms: [RA-001]
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
	crud := strings.Replace(baseV05CRUD(), "matrix:\n", "matrix:\n  - entity: ProductColor\n    table: product_colors\n    operations:\n      manage_catalog: [R]\n", 1)
	modelPath := writeV05Fixture(t, model, baseV05SourceUnits(), baseV05RequirementAtoms(), baseV05Functional(), crud, baseV05Reviews())

	result := ValidateFile(modelPath)
	assertHasError(t, result, "targets association entity ProductColor without scalar id")
}

func writeV05Fixture(t *testing.T, model, sourceUnits, atoms, functional, crud, reviews string) string {
	t.Helper()
	dir := t.TempDir()
	writeV05File(t, dir, "source_units.yaml", sourceUnits)
	writeV05File(t, dir, "requirement_atoms.yaml", atoms)
	writeV05File(t, dir, "functional_decomposition.yaml", functional)
	writeV05File(t, dir, "crud_matrix.yaml", crud)
	writeV05File(t, dir, "review_decisions.yaml", reviews)
	return writeV05File(t, dir, "db_model.dsl.yaml", model)
}

func writeV05File(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func baseV05Model() string {
	return `
dsl:
  name: DB-DSL
  version: "0.5"
model:
  id: test_v05
  name: Test v0.5
  status: draft_model
  description: Test v0.5 model.
source:
  source_units_file: source_units.yaml
  requirement_atoms_file: requirement_atoms.yaml
  functional_decomposition_file: functional_decomposition.yaml
  crud_matrix_file: crud_matrix.yaml
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
      requirement_atoms: [RA-001]
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

func baseV05SourceUnits() string {
	return `
document:
  id: test_source_units
  pipeline_version: "0.5"
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

func baseV05RequirementAtoms() string {
	return `
document:
  id: test_requirement_atoms
  pipeline_version: "0.5"
requirement_atoms:
  - id: RA-001
    statement: Product belongs to print shop.
    atom_type: entity
    modeling_relevance: direct_db
    source_units: [SU-001]
    functional_area: catalog
    functional_pattern: catalog_management
    support_level: explicit
    confidence: high
    requires_review: true
    review_decisions: [RD-001]
    model_impacts:
      entities: [PrintShop, Product]
      attributes: [PrintShop.name, Product.code]
      relationships: [ProductPrintShop]
      constraints: [product_code_unique_per_print_shop]
    modeling_outcome:
      status: represented
coverage_checks: []
`
}

func baseV05Functional() string {
	return `
document:
  id: test_functional
functional_areas:
  - id: catalog
    label: Catalog
    purpose: Catalog management.
    main_actors: [system]
    atoms: [RA-001]
    modeling_focus: [Product]
coverage_summary: {}
`
}

func baseV05CRUD() string {
	return `
document:
  id: test_crud
notation:
  C: create
  R: read
  U: update
  D: delete
actors:
  - id: system
    label: System
    description: System actor.
operations:
  - id: manage_catalog
    label: Manage catalog
    functional_area: catalog
    functional_pattern: catalog_management
    actor: system
    source_atoms: [RA-001]
    source_units: [SU-001]
    description: Manage catalog data.
matrix:
  - entity: PrintShop
    table: print_shops
    operations:
      manage_catalog: [C, R, U]
  - entity: Product
    table: products
    operations:
      manage_catalog: [C, R, U]
coverage_checks: []
`
}

func baseV05Reviews() string {
	return `
document:
  id: test_reviews
review_state:
  status: simulated_review_complete
review_decisions:
  - id: RD-001
    question: How to model catalog?
    affected_atoms: [RA-001]
    decision:
      status: accepted
      selected_option: catalog_tables
      rationale: Test rationale.
coverage_checks: []
`
}
