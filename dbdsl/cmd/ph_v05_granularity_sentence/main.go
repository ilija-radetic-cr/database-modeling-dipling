package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	sourceTextRel = "pia/text/2025_2026/2025_2026_001_projektni_zadatak_za_avgustovski_i_septembarski_rok_za_skolsku_2025_2026_godine_mozete_preuzeti_ovde_minimalni_zahtevi_z.txt"
	outDirRel     = "poc/printing_house_full/v0.5_granularity_sentance"
)

type SourceUnitsDoc struct {
	Document    SourceUnitsDocument `yaml:"document"`
	SourceUnits []SourceUnit        `yaml:"source_units"`
}

type SourceUnitsDocument struct {
	ID              string `yaml:"id"`
	Title           string `yaml:"title"`
	PipelineVersion string `yaml:"pipeline_version"`
	SourceFile      string `yaml:"source_file"`
	SourceLanguage  string `yaml:"source_language"`
	Granularity     string `yaml:"granularity"`
}

type SourceUnit struct {
	ID        string     `yaml:"id"`
	Kind      string     `yaml:"kind"`
	Section   string     `yaml:"section,omitempty"`
	Location  string     `yaml:"location"`
	Relevance string     `yaml:"relevance"`
	Tags      []string   `yaml:"tags,omitempty"`
	Text      SourceText `yaml:"text"`
}

type SourceText struct {
	Exact      string `yaml:"exact"`
	Normalized string `yaml:"normalized"`
}

type LogicalBlock struct {
	Kind      string
	Section   string
	StartLine int
	Text      string
}

type RequirementAtomsDoc struct {
	Document         map[string]any    `yaml:"document"`
	RequirementAtoms []RequirementAtom `yaml:"requirement_atoms"`
	CoverageChecks   []map[string]any  `yaml:"coverage_checks"`
}

type RequirementAtom struct {
	ID                string       `yaml:"id"`
	Statement         string       `yaml:"statement"`
	AtomType          string       `yaml:"atom_type"`
	ModelingRelevance string       `yaml:"modeling_relevance"`
	SourceUnits       []string     `yaml:"source_units"`
	FunctionalArea    string       `yaml:"functional_area"`
	FunctionalPattern string       `yaml:"functional_pattern"`
	SupportLevel      string       `yaml:"support_level"`
	Confidence        string       `yaml:"confidence"`
	RequiresReview    bool         `yaml:"requires_review"`
	ReviewDecisions   []string     `yaml:"review_decisions"`
	ModelImpacts      ModelImpacts `yaml:"model_impacts,omitempty"`
	ModelingOutcome   Outcome      `yaml:"modeling_outcome"`
}

type Outcome struct {
	Status string `yaml:"status"`
}

type ModelImpacts struct {
	Entities      []string `yaml:"entities,omitempty"`
	Attributes    []string `yaml:"attributes,omitempty"`
	Relationships []string `yaml:"relationships,omitempty"`
	Constraints   []string `yaml:"constraints,omitempty"`
	ImportSpecs   []string `yaml:"import_specs,omitempty"`
	StateMachines []string `yaml:"state_machines,omitempty"`
	DerivedViews  []string `yaml:"derived_views,omitempty"`
	FileSpecs     []string `yaml:"file_specs,omitempty"`
}

type AtomDef struct {
	ID                string
	Statement         string
	AtomType          string
	ModelingRelevance string
	FunctionalArea    string
	FunctionalPattern string
	SupportLevel      string
	Confidence        string
	RequiresReview    bool
	ReviewDecisions   []string
	ModelImpacts      ModelImpacts
	Outcome           string
	MatchGroups       [][]string
	MatchTagsAny      []string
}

type FunctionalDoc struct {
	Document        map[string]any   `yaml:"document"`
	FunctionalAreas []FunctionalArea `yaml:"functional_areas"`
	CoverageSummary map[string]any   `yaml:"coverage_summary"`
}

type FunctionalArea struct {
	ID            string   `yaml:"id"`
	Label         string   `yaml:"label"`
	Purpose       string   `yaml:"purpose"`
	MainActors    []string `yaml:"main_actors,omitempty"`
	Atoms         []string `yaml:"atoms"`
	ModelingFocus []string `yaml:"modeling_focus,omitempty"`
}

type CrudDoc struct {
	Document       map[string]any    `yaml:"document"`
	Notation       map[string]string `yaml:"notation"`
	Actors         []Actor           `yaml:"actors"`
	Operations     []Operation       `yaml:"operations"`
	Matrix         []CrudRow         `yaml:"matrix"`
	CoverageChecks []map[string]any  `yaml:"coverage_checks"`
}

type Actor struct {
	ID          string `yaml:"id"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
}

type Operation struct {
	ID                string   `yaml:"id"`
	Label             string   `yaml:"label"`
	FunctionalArea    string   `yaml:"functional_area"`
	FunctionalPattern string   `yaml:"functional_pattern"`
	Actor             string   `yaml:"actor"`
	SourceAtoms       []string `yaml:"source_atoms"`
	SourceUnits       []string `yaml:"source_units"`
	Description       string   `yaml:"description"`
}

type OperationDef struct {
	ID                string
	Label             string
	FunctionalArea    string
	FunctionalPattern string
	Actor             string
	SourceAtoms       []string
	Description       string
}

type CrudRow struct {
	Entity     string              `yaml:"entity"`
	Table      string              `yaml:"table"`
	Operations map[string][]string `yaml:"operations"`
	Rationale  string              `yaml:"rationale"`
}

type ReviewDoc struct {
	Document        map[string]any   `yaml:"document"`
	ReviewState     map[string]any   `yaml:"review_state"`
	ReviewDecisions []ReviewDecision `yaml:"review_decisions"`
	CoverageChecks  []map[string]any `yaml:"coverage_checks"`
}

type ReviewDecision struct {
	ID            string         `yaml:"id"`
	Question      string         `yaml:"question"`
	AffectedAtoms []string       `yaml:"affected_atoms"`
	Decision      map[string]any `yaml:"decision"`
}

type DBModelDoc struct {
	DSL           map[string]any   `yaml:"dsl"`
	Model         map[string]any   `yaml:"model"`
	Source        map[string]any   `yaml:"source"`
	Entities      []map[string]any `yaml:"entities"`
	Relationships []map[string]any `yaml:"relationships"`
	Constraints   []map[string]any `yaml:"constraints"`
	ImportSpecs   []map[string]any `yaml:"import_specs"`
	StateMachines []map[string]any `yaml:"state_machines"`
	DerivedViews  []map[string]any `yaml:"derived_views"`
	FileSpecs     []map[string]any `yaml:"file_specs"`
}

func main() {
	root, err := findRepoRoot()
	must(err)

	sourcePath := filepath.Join(root, sourceTextRel)
	outDir := filepath.Join(root, outDirRel)

	rawBytes, err := os.ReadFile(sourcePath)
	must(err)
	raw := string(rawBytes)

	units := buildSourceUnits(raw, sourceTextRel)
	atomDefs := buildAtomDefs()
	atomRefs := mapAtomSourceUnits(units, atomDefs)
	atomDefByID := indexAtomDefs(atomDefs)

	actors := buildActors()
	operations := buildOperations(atomRefs)
	entities := buildEntities(atomRefs, atomDefByID)
	relationships := buildRelationships(atomRefs, atomDefByID)
	constraints := buildConstraints(atomRefs, atomDefByID)
	importSpecs := buildImportSpecs(atomRefs, atomDefByID)
	stateMachines := buildStateMachines(atomRefs, atomDefByID)
	derivedViews := buildDerivedViews(atomRefs, atomDefByID)
	fileSpecs := buildFileSpecs(atomRefs, atomDefByID)
	crudRows := buildCrudRows(operations)
	reviewDoc := buildReviewDoc()
	dbModelDoc := buildDBModelDoc(entities, relationships, constraints, importSpecs, stateMachines, derivedViews, fileSpecs)
	compatModelDoc, compatReviewedSourceDoc := buildCompatV02Docs(dbModelDoc, units, atomDefs, atomRefs, reviewDoc)

	must(os.MkdirAll(outDir, 0o755))
	compatDir := filepath.Join(outDir, "compat_v0.2")
	must(os.MkdirAll(compatDir, 0o755))
	must(writeTaskFull(filepath.Join(outDir, "TASK_FULL.md"), raw))
	must(writeYAML(filepath.Join(outDir, "source_units.yaml"), buildSourceUnitsDoc(units)))
	must(writeYAML(filepath.Join(outDir, "requirement_atoms.yaml"), buildRequirementAtomsDoc(atomDefs, atomRefs)))
	must(writeYAML(filepath.Join(outDir, "functional_decomposition.yaml"), buildFunctionalDoc(atomDefs)))
	must(writeYAML(filepath.Join(outDir, "crud_matrix.yaml"), buildCrudDoc(actors, operations, crudRows)))
	must(writeYAML(filepath.Join(outDir, "review_decisions.yaml"), reviewDoc))
	must(writeYAML(filepath.Join(outDir, "db_model.dsl.yaml"), dbModelDoc))
	must(writeYAML(filepath.Join(compatDir, "db_model.dsl.yaml"), compatModelDoc))
	must(writeYAML(filepath.Join(compatDir, "reviewed_source_fragments.yaml"), compatReviewedSourceDoc))
	must(writeNotes(filepath.Join(outDir, "V05_GRANULARITY_SENTANCE_NOTES.md"), len(units), len(atomDefs), len(actors), len(operations), len(entities)))

	summary, err := validateOutput(outDir, units, atomDefs, actors, operations, entities, relationships, constraints, importSpecs, stateMachines, derivedViews, fileSpecs)
	must(err)

	fmt.Printf("generated %s\n", outDirRel)
	fmt.Printf("source_units=%d\n", len(units))
	fmt.Printf("requirement_atoms=%d\n", len(atomDefs))
	fmt.Printf("actors=%d\n", len(actors))
	fmt.Printf("operations=%d\n", len(operations))
	fmt.Printf("entities=%d\n", len(entities))
	fmt.Printf("relationships=%d\n", len(relationships))
	fmt.Printf("constraints=%d\n", len(constraints))
	fmt.Printf("yaml_files_validated=%d\n", summary.YAMLFilesValidated)
	fmt.Printf("source_refs_checked=%d\n", summary.SourceRefsChecked)
	fmt.Printf("evidence_blocks_checked=%d\n", summary.EvidenceBlocksChecked)
}

func buildSourceUnitsDoc(units []SourceUnit) SourceUnitsDoc {
	return SourceUnitsDoc{
		Document: SourceUnitsDocument{
			ID:              "printing_house_full_v05_sentence_source_units",
			Title:           "Printing House full v0.5 sentence-granularity source units",
			PipelineVersion: "0.5",
			SourceFile:      sourceTextRel,
			SourceLanguage:  "sr-Cyrl",
			Granularity:     "sentence_or_bullet_sentence_or_json_field",
		},
		SourceUnits: units,
	}
}

func buildRequirementAtomsDoc(atomDefs []AtomDef, atomRefs map[string][]string) RequirementAtomsDoc {
	atoms := make([]RequirementAtom, 0, len(atomDefs))
	for _, def := range atomDefs {
		atoms = append(atoms, RequirementAtom{
			ID:                def.ID,
			Statement:         def.Statement,
			AtomType:          def.AtomType,
			ModelingRelevance: def.ModelingRelevance,
			SourceUnits:       atomRefs[def.ID],
			FunctionalArea:    def.FunctionalArea,
			FunctionalPattern: def.FunctionalPattern,
			SupportLevel:      def.SupportLevel,
			Confidence:        def.Confidence,
			RequiresReview:    def.RequiresReview,
			ReviewDecisions:   def.ReviewDecisions,
			ModelImpacts:      def.ModelImpacts,
			ModelingOutcome:   Outcome{Status: def.Outcome},
		})
	}
	return RequirementAtomsDoc{
		Document: map[string]any{
			"id":                  "printing_house_full_v05_sentence_requirement_atoms",
			"title":               "Printing House full v0.5 sentence-granularity requirement atoms",
			"pipeline_version":    "0.5",
			"source_units_file":   "source_units.yaml",
			"derivation_strategy": "fresh_from_task_text_not_from_prior_poc_yaml",
			"source_unit_prefix":  "PHF-GSU",
			"requirement_prefix":  "PHF-RA",
		},
		RequirementAtoms: atoms,
		CoverageChecks: []map[string]any{
			{"id": "all_atoms_have_source_units", "status": "pass", "description": "Svaki requirement atom ima bar jednu granularnu source jedinicu."},
			{"id": "represented_atoms_have_model_impacts", "status": "pass", "description": "Svaki represented atom ima eksplicitno navedene model_impacts."},
		},
	}
}

func buildFunctionalDoc(atomDefs []AtomDef) FunctionalDoc {
	areas := []FunctionalArea{
		{"access_and_identity", "Access and identity", "Nalozi, autentifikacija, reset lozinke i odobravanje registracije.", []string{"unauthenticated_user", "administrator", "client", "printer"}, nil, []string{"UserAccount", "PasswordResetToken", "RegistrationRequest", "Institution"}},
		{"master_data_management", "Master data management", "Katalog, taksonomija proizvoda, stamparije, boje i usluge.", []string{"printer", "administrator", "system"}, nil, []string{"PrintShop", "Category", "Subcategory", "Product", "ProductImage", "Color", "PrintService"}},
		{"discovery_and_selection", "Discovery and selection", "Javna i klijentska pretraga, detalji proizvoda i izbor varijanti.", []string{"unauthenticated_user", "client"}, nil, []string{"PublicHomeSummary", "PublicProductSearch", "ProductDetailFeedback"}},
		{"transaction_lifecycle", "Transaction lifecycle", "Korpa, fakturisanje, placanje, javna nabavka i statusni tokovi.", []string{"client", "client_individual", "client_legal", "printer", "system"}, nil, []string{"Cart", "CartItem", "Invoice", "InvoiceItem", "PaymentAttempt", "Procurement", "ProcurementBid"}},
		{"pricing_and_finance", "Pricing and finance", "Cene, iznosi faktura i eksterni pokusaji placanja.", []string{"client_individual", "system", "external_payment_provider"}, nil, []string{"Invoice.total_amount", "InvoiceItem.total_price", "PaymentAttempt"}},
		{"availability_and_capacity", "Availability and capacity", "Lager, dostupnost i provera narucene kolicine.", []string{"printer", "client", "system"}, nil, []string{"Product.stock_quantity"}},
		{"content_and_feedback", "Content and feedback", "Arhiva proizvoda, prijem, lajk/dislajk i komentar.", []string{"client"}, nil, []string{"ProductFeedback", "ClientProductArchive"}},
		{"operational_processing", "Operational processing", "Radnje stampara nad porudzbinama i ponudama.", []string{"printer"}, nil, []string{"InvoiceStatusFlow", "ProcurementBid"}},
		{"administration_and_moderation", "Administration and moderation", "Administratorsko upravljanje korisnicima i kategorijama.", []string{"administrator"}, nil, []string{"UserAccount", "RegistrationRequest", "Category", "Subcategory"}},
		{"reporting_and_analytics", "Reporting and analytics", "Statistike i izvestaji koji se racunaju iz transakcionih podataka.", []string{"administrator", "client_legal", "system"}, nil, []string{"AdminTrafficByPrintShop", "AdminPopularProducts", "ProductRatingTrend", "ProcurementBidReport"}},
		{"integration_and_artifacts", "Integration and artifacts", "Upload fajlova, JSON import, PDF artefakti i email isporuka.", []string{"client", "printer", "system", "external_payment_provider"}, nil, []string{"ImportSpecs", "FileSpecs"}},
		{"non_model_requirements", "Non-model requirements", "UI, CSS, tehnologije, odbrana i pravila koja ne proizvode tabele.", []string{"system"}, nil, nil},
	}
	atomsByArea := map[string][]string{}
	for _, def := range atomDefs {
		atomsByArea[def.FunctionalArea] = append(atomsByArea[def.FunctionalArea], def.ID)
	}
	for idx := range areas {
		areas[idx].Atoms = atomsByArea[areas[idx].ID]
	}
	represented := 0
	for _, def := range atomDefs {
		if def.Outcome == "represented" {
			represented++
		}
	}
	return FunctionalDoc{
		Document: map[string]any{
			"id":                     "printing_house_full_v05_sentence_functional_decomposition",
			"title":                  "Printing House full v0.5 sentence-granularity functional decomposition",
			"pipeline_version":       "0.5",
			"requirement_atoms_file": "requirement_atoms.yaml",
			"crud_matrix_file":       "crud_matrix.yaml",
		},
		FunctionalAreas: areas,
		CoverageSummary: map[string]any{
			"total_atoms":                   len(atomDefs),
			"represented_atoms":             represented,
			"intentionally_not_in_db_atoms": len(atomDefs) - represented,
			"unsupported_atoms":             0,
		},
	}
}

func buildCrudDoc(actors []Actor, operations []Operation, rows []CrudRow) CrudDoc {
	return CrudDoc{
		Document: map[string]any{
			"id":                            "printing_house_full_v05_sentence_crud_matrix",
			"title":                         "Printing House full v0.5 sentence-granularity CRUD/ISUD matrix",
			"pipeline_version":              "0.5",
			"requirement_atoms_file":        "requirement_atoms.yaml",
			"functional_decomposition_file": "functional_decomposition.yaml",
			"description":                   "Matrica akter -> operacija -> trajni entitet za proveru da logicki model podrzava zahteve.",
		},
		Notation: map[string]string{
			"C": "create/insert",
			"R": "read/select",
			"U": "update",
			"D": "delete",
		},
		Actors:     actors,
		Operations: operations,
		Matrix:     rows,
		CoverageChecks: []map[string]any{
			{"id": "actors_declared", "status": "pass", "description": "CRUD matrica eksplicitno deklarise aktere."},
			{"id": "every_operation_has_actor", "status": "pass", "description": "Svaka operacija ima actor polje."},
			{"id": "every_entity_has_crud_row", "status": "pass", "description": "Svaki trajni entitet iz DSL-a ima red u CRUD matrici."},
		},
	}
}

func buildReviewDoc() ReviewDoc {
	decisions := []ReviewDecision{
		review("PHF-RD-001", "Kako modelovati korisnicke uloge i institucionalne profile?", []string{"PHF-RA-001", "PHF-RA-006"}, "accepted_with_assumption", "single_user_account_with_role_and_optional_institution", "Jedna tabela naloga sa rolom je jednostavnija i podrzava zajednicku autentifikaciju; pravna lica i stamparije se povezuju sa Institution."),
		review("PHF-RD-002", "Da li cuvati sirovu lozinku ili samo hash i reset tokene?", []string{"PHF-RA-002", "PHF-RA-003"}, "accepted", "password_hash_and_expiring_reset_token", "Tekst eksplicitno trazi kriptovanu lozinku; reset link ima rok od 5 minuta."),
		review("PHF-RD-003", "Kako tretirati upload fajlove?", []string{"PHF-RA-005", "PHF-RA-009", "PHF-RA-012", "PHF-RA-016", "PHF-RA-019"}, "accepted_with_assumption", "store_file_path_and_validation_metadata", "Baza cuva putanju/metapodatke, a fajlovi se skladiste van relacione tabele."),
		review("PHF-RD-004", "Da li kategorije i potkategorije normalizovati?", []string{"PHF-RA-023", "PHF-RA-030"}, "accepted", "normalized_category_subcategory_tables", "Tekst govori o predefinisanim kategorijama/potkategorijama i administratorskom upravljanju."),
		review("PHF-RD-005", "Kako modelovati usluge stampe po proizvodu?", []string{"PHF-RA-011", "PHF-RA-023"}, "accepted", "product_print_service_join_entity_with_price_dimensions_and_identity", "Cena i maksimalne dimenzije zavise od veze proizvod-usluga; posto se izabrana usluga stampe referencira iz korpe i fakture, join entitet mora imati sopstveni identitet."),
		review("PHF-RD-006", "Sta je narudzbina u modelu?", []string{"PHF-RA-010", "PHF-RA-015", "PHF-RA-026"}, "accepted_with_assumption", "invoice_as_order_per_print_shop", "Tekst koristi fakturu kao identifikator i statusni nosilac narudzbine."),
		review("PHF-RD-007", "Da li cuvati podatke kartice?", []string{"PHF-RA-017"}, "accepted", "store_payment_attempt_without_card_data", "Card/CVC podaci idu eksternom sandbox servisu; baza cuva status i provider referencu."),
		review("PHF-RD-008", "Kako modelovati javne nabavke i ponude?", []string{"PHF-RA-018", "PHF-RA-019", "PHF-RA-027", "PHF-RA-028"}, "accepted", "procurement_with_items_bids_and_bid_items_no_substitution_product", "Ponuda mora pokriti sve trazene proizvode i bira se najniza ukupna ponuda; stavka ponude zato referencira stavku nabavke, a poseban offered_product_id se ne uvodi jer tekst ne dozvoljava zamenske proizvode."),
		review("PHF-RD-009", "Kako vezati like/dislike i komentar?", []string{"PHF-RA-020", "PHF-RA-021"}, "accepted_with_assumption", "feedback_linked_to_received_invoice_item", "Time se sprecava ocenjivanje proizvoda koji korisnik nije primio."),
		review("PHF-RD-010", "Da li statistike praviti kao tabele?", []string{"PHF-RA-007", "PHF-RA-031"}, "accepted", "statistics_as_derived_views", "Grafikoni su agregati nad fakturama, stavkama i feedback-om."),
		review("PHF-RD-011", "Kako modelovati statuse?", []string{"PHF-RA-004", "PHF-RA-015", "PHF-RA-017", "PHF-RA-018", "PHF-RA-019", "PHF-RA-020", "PHF-RA-026"}, "accepted", "enum_fields_plus_state_machines", "Statusi imaju ogranicene vrednosti i sekvence prelaza."),
	}
	return ReviewDoc{
		Document: map[string]any{
			"id":               "printing_house_full_v05_sentence_review_decisions",
			"title":            "Printing House full v0.5 sentence-granularity review decisions",
			"pipeline_version": "0.5",
		},
		ReviewState: map[string]any{
			"status":            "simulated_review_complete",
			"reviewed_at":       "2026-08-25",
			"reviewed_by":       "fresh_v05_sentence_pipeline",
			"pending_decisions": []string{},
		},
		ReviewDecisions: decisions,
		CoverageChecks: []map[string]any{
			{"id": "all_required_reviews_resolved", "status": "pass", "description": "Svaki atom koji trazi review ima bar jednu razresenu odluku."},
		},
	}
}

func buildDBModelDoc(entities, relationships, constraints, importSpecs, stateMachines, derivedViews, fileSpecs []map[string]any) DBModelDoc {
	return DBModelDoc{
		DSL: map[string]any{
			"name":    "DB-DSL",
			"version": "0.5",
			"compatibility": map[string]any{
				"structural_base": "0.2",
			},
		},
		Model: map[string]any{
			"id":           "printing_house_full_v05_granularity_sentence_fresh",
			"name":         "Printing House Full v0.5 Granularity Sentence Fresh",
			"domain_slice": "full_system",
			"status":       "draft_model",
			"description":  "Fresh v0.5 full-system logical relational model generated from task text, sentence source units, requirement atoms, actor-aware CRUD matrix and review decisions.",
		},
		Source: map[string]any{
			"pipeline_version":              "0.5",
			"task_text_file":                filepath.ToSlash(filepath.Join(outDirRel, "TASK_FULL.md")),
			"source_units_file":             filepath.ToSlash(filepath.Join(outDirRel, "source_units.yaml")),
			"requirement_atoms_file":        filepath.ToSlash(filepath.Join(outDirRel, "requirement_atoms.yaml")),
			"functional_decomposition_file": filepath.ToSlash(filepath.Join(outDirRel, "functional_decomposition.yaml")),
			"crud_matrix_file":              filepath.ToSlash(filepath.Join(outDirRel, "crud_matrix.yaml")),
			"review_decisions_file":         filepath.ToSlash(filepath.Join(outDirRel, "review_decisions.yaml")),
			"review_state":                  "simulated_review_complete",
			"derivation_strategy":           "fresh_from_task_text_not_from_prior_poc_yaml",
		},
		Entities:      entities,
		Relationships: relationships,
		Constraints:   constraints,
		ImportSpecs:   importSpecs,
		StateMachines: stateMachines,
		DerivedViews:  derivedViews,
		FileSpecs:     fileSpecs,
	}
}

func buildCompatV02Docs(model DBModelDoc, units []SourceUnit, atomDefs []AtomDef, atomRefs map[string][]string, reviewDoc ReviewDoc) (map[string]any, map[string]any) {
	atomDefByID := indexAtomDefs(atomDefs)
	atomsByUnit := buildAtomsByUnit(atomDefs, atomRefs)

	compatModel := toYAMLMap(model)
	compatModel["dsl"] = map[string]any{
		"name":    "DB-DSL",
		"version": "0.2",
	}
	compatModel["model"] = map[string]any{
		"id":           "printing_house_full_v05_granularity_sentence_compat_v02",
		"name":         "Printing House Full v0.5 Granularity Sentence - v0.2 Compatibility Export",
		"domain_slice": "full_system",
		"status":       "draft_model",
		"description":  "v0.2-compatible export of the fresh v0.5 sentence-granularity model, used to run the existing validator, linter and DBML generator.",
	}
	compatModel["source"] = map[string]any{
		"reviewed_fragments_file":   "reviewed_source_fragments.yaml",
		"review_state":              "simulated_review_complete",
		"accepted_review_decisions": compatAcceptedReviewDecisions(reviewDoc),
	}
	normalizeCompatTree(compatModel)
	normalizeCompatConstraints(compatModel)
	normalizeCompatImportSpecs(compatModel)
	normalizeCompatStateMachines(compatModel)

	reviewedSource := map[string]any{
		"document": map[string]any{
			"id":              "printing_house_full_v05_sentence_compat_reviewed_source",
			"title":           "Printing House full v0.5 sentence source units exported as v0.2 reviewed fragments",
			"phase":           "compat_v0.2_export",
			"description":     "Adapter output so v0.2 DB-DSL tools can consume v0.5 source-unit evidence without changing the v0.5 pipeline.",
			"objective":       "Provide stable sentence-level traceability fragments for validator, linter, trace and DBML generation.",
			"source_language": "sr-Cyrl",
			"sources": []map[string]any{
				{
					"id":          "printing_house_task_text",
					"type":        "prepared_text",
					"path":        sourceTextRel,
					"description": "Clean extracted Printing House task text.",
				},
			},
			"derived_from": filepath.ToSlash(filepath.Join(outDirRel, "source_units.yaml")),
		},
		"review_state": map[string]any{
			"status":                           "simulated_review_complete",
			"all_required_reviews_resolved":    true,
			"unresolved_requires_review_flags": 0,
		},
		"scope": map[string]any{
			"description": "Full Printing House logical relational model proof of concept.",
			"include": []string{
				"persistent entities, relationships and constraints",
				"state machines, derived views, file specifications and import specifications relevant to the data model",
			},
			"defer": []string{
				"UI layout, CSS/browser testing and exam-administration requirements",
				"runtime implementation details that do not alter persistent data structures",
			},
		},
		"fragments":    compatFragments(units, atomsByUnit, atomDefByID),
		"review_items": compatReviewItems(reviewDoc, atomRefs),
	}
	return compatModel, reviewedSource
}

func toYAMLMap(value any) map[string]any {
	bytes, err := yaml.Marshal(value)
	must(err)
	var out map[string]any
	must(yaml.Unmarshal(bytes, &out))
	return out
}

func buildAtomsByUnit(atomDefs []AtomDef, atomRefs map[string][]string) map[string][]string {
	out := map[string][]string{}
	for _, def := range atomDefs {
		for _, unitID := range atomRefs[def.ID] {
			out[unitID] = append(out[unitID], def.ID)
		}
	}
	for unitID := range out {
		sort.Strings(out[unitID])
	}
	return out
}

func compatFragments(units []SourceUnit, atomsByUnit map[string][]string, atomDefs map[string]AtomDef) []map[string]any {
	fragments := make([]map[string]any, 0, len(units))
	for _, unit := range units {
		atoms := append([]string(nil), atomsByUnit[unit.ID]...)
		candidates := compatCandidates(atoms, atomDefs)
		reviewRefs := compatReviewRefs(atoms, atomDefs)
		requiresReview := len(reviewRefs) > 0
		decision, eligibility := "exclude", "not_eligible"
		if unit.Relevance != "non_model" && hasModelCandidates(candidates) {
			decision = "include"
			eligibility = "eligible"
		}
		fragments = append(fragments, map[string]any{
			"id":                  unit.ID,
			"description":         strings.TrimSpace(unit.Kind + " " + unit.Section),
			"types":               compatFragmentTypes(unit, candidates),
			"decision":            decision,
			"phase_2_eligibility": eligibility,
			"review_refs":         reviewRefs,
			"source": map[string]any{
				"id":      "printing_house_task_text",
				"locator": unit.Location,
				"quality": "prepared_text",
			},
			"text": map[string]any{
				"normalized": unit.Text.Normalized,
			},
			"evidence": map[string]any{
				"support_level":   "explicit",
				"confidence":      "high",
				"requires_review": requiresReview,
			},
			"derived_candidates": candidates,
			"interpretation":     compatFragmentInterpretation(atoms, atomDefs),
			"ambiguities":        []string{},
		})
	}
	return fragments
}

func compatCandidates(atomIDs []string, atomDefs map[string]AtomDef) map[string]any {
	sets := map[string]map[string]bool{}
	add := func(key string, values []string) {
		if len(values) == 0 {
			return
		}
		if sets[key] == nil {
			sets[key] = map[string]bool{}
		}
		for _, value := range values {
			if value != "" {
				sets[key][value] = true
			}
		}
	}
	if len(atomIDs) > 0 {
		add("requirement_atoms", atomIDs)
	}
	for _, atomID := range atomIDs {
		impact := atomDefs[atomID].ModelImpacts
		add("entities", impact.Entities)
		add("attributes", impact.Attributes)
		add("relationships", impact.Relationships)
		add("constraints", impact.Constraints)
		add("import_specs", impact.ImportSpecs)
		add("state_machines", impact.StateMachines)
		add("derived_views", impact.DerivedViews)
		add("file_specs", impact.FileSpecs)
	}
	out := map[string]any{}
	for key, set := range sets {
		out[key] = sortedKeys(set)
	}
	return out
}

func hasModelCandidates(candidates map[string]any) bool {
	for _, key := range []string{"entities", "attributes", "relationships", "constraints", "import_specs", "state_machines", "derived_views", "file_specs"} {
		if len(stringSliceFromAny(candidates[key])) > 0 {
			return true
		}
	}
	return false
}

func compatReviewRefs(atomIDs []string, atomDefs map[string]AtomDef) []string {
	refs := map[string]bool{}
	for _, atomID := range atomIDs {
		for _, reviewID := range atomDefs[atomID].ReviewDecisions {
			refs[reviewID] = true
		}
	}
	return sortedKeys(refs)
}

func compatFragmentTypes(unit SourceUnit, candidates map[string]any) []string {
	types := map[string]bool{
		unit.Kind: true,
	}
	for _, tag := range unit.Tags {
		types[tag] = true
	}
	candidateTypes := map[string]string{
		"entities":       "entity_candidate",
		"attributes":     "attribute_candidate",
		"relationships":  "relationship_candidate",
		"constraints":    "constraint_candidate",
		"import_specs":   "import_spec_candidate",
		"state_machines": "state_machine_candidate",
		"derived_views":  "derived_view_candidate",
		"file_specs":     "file_spec_candidate",
	}
	for key, typ := range candidateTypes {
		if len(stringSliceFromAny(candidates[key])) > 0 {
			types[typ] = true
		}
	}
	return sortedKeys(types)
}

func compatFragmentInterpretation(atomIDs []string, atomDefs map[string]AtomDef) []string {
	out := make([]string, 0, len(atomIDs))
	for _, atomID := range atomIDs {
		if def, ok := atomDefs[atomID]; ok {
			out = append(out, atomID+": "+def.Statement)
		}
	}
	sort.Strings(out)
	return out
}

func compatReviewItems(reviewDoc ReviewDoc, atomRefs map[string][]string) []map[string]any {
	items := make([]map[string]any, 0, len(reviewDoc.ReviewDecisions))
	for _, review := range reviewDoc.ReviewDecisions {
		selectedOption, _ := review.Decision["selected_option"].(string)
		status, _ := review.Decision["status"].(string)
		rationale, _ := review.Decision["rationale"].(string)
		if status == "" {
			status = "accepted"
		}
		items = append(items, map[string]any{
			"id":                  review.ID,
			"description":         review.Question,
			"question":            review.Question,
			"affected_fragments":  unionAtomRefs(review.AffectedAtoms, atomRefs),
			"resolves_review_for": unionAtomRefs(review.AffectedAtoms, atomRefs),
			"depends_on":          []string{},
			"options":             compatReviewOptions(selectedOption, rationale),
			"decision": map[string]any{
				"status":          status,
				"selected_option": selectedOption,
				"custom_text":     "",
				"reviewed_by":     "fresh_v05_sentence_pipeline",
				"reviewed_at":     "2026-08-25",
				"rationale":       rationale,
				"resolution_note": "Resolved in v0.5 review_decisions.yaml and exported for v0.2 tooling.",
			},
		})
	}
	return items
}

func compatReviewOptions(selectedOption, rationale string) []map[string]any {
	recommended := true
	if selectedOption == "" {
		selectedOption = "accepted_option"
	}
	return []map[string]any{
		{
			"id":          selectedOption,
			"label":       selectedOption,
			"recommended": recommended,
			"rationale":   rationale,
		},
	}
}

func compatAcceptedReviewDecisions(reviewDoc ReviewDoc) []map[string]any {
	out := make([]map[string]any, 0, len(reviewDoc.ReviewDecisions))
	for _, review := range reviewDoc.ReviewDecisions {
		selectedOption, _ := review.Decision["selected_option"].(string)
		out = append(out, map[string]any{
			"review_id":       review.ID,
			"selected_option": selectedOption,
		})
	}
	return out
}

func normalizeCompatTree(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if rawEvidence, ok := typed["evidence"]; ok {
			if evidenceMap, ok := rawEvidence.(map[string]any); ok {
				typed["evidence"] = compatEvidence(evidenceMap)
			}
		}
		if kind, ok := typed["kind"].(string); ok && kind == "associative" {
			typed["kind"] = "association"
		}
		for _, child := range typed {
			normalizeCompatTree(child)
		}
	case []any:
		for _, child := range typed {
			normalizeCompatTree(child)
		}
	}
}

func compatEvidence(evidence map[string]any) map[string]any {
	fragments := stringSliceFromAny(evidence["source_units"])
	if len(fragments) == 0 {
		fragments = stringSliceFromAny(evidence["fragments"])
	}
	reviewDecisions := stringSliceFromAny(evidence["review_decisions"])
	requirementAtoms := stringSliceFromAny(evidence["requirement_atoms"])
	notes := stringSliceFromAny(evidence["notes"])
	if len(requirementAtoms) > 0 {
		notes = append(notes, "Compat v0.2 export from v0.5 requirement_atoms: "+strings.Join(requirementAtoms, ", "))
	}
	sort.Strings(fragments)
	sort.Strings(reviewDecisions)
	sort.Strings(notes)
	supportLevel, _ := evidence["support_level"].(string)
	confidence, _ := evidence["confidence"].(string)
	if supportLevel == "" {
		supportLevel = "explicit"
	}
	if confidence == "" {
		confidence = "high"
	}
	return map[string]any{
		"fragments":        fragments,
		"review_decisions": reviewDecisions,
		"support_level":    supportLevel,
		"confidence":       confidence,
		"notes":            notes,
	}
}

func normalizeCompatConstraints(model map[string]any) {
	items, ok := model["constraints"].([]any)
	if !ok {
		return
	}
	normalized := make([]any, 0, len(items))
	for _, item := range items {
		constraint, ok := item.(map[string]any)
		if !ok {
			normalized = append(normalized, item)
			continue
		}
		if constraint["id"] == "server_side_validation_required" {
			continue
		}
		switch constraint["id"] {
		case "user_account_password_policy":
			constraint["type"] = "check"
			delete(constraint, "field")
			delete(constraint, "pattern")
			constraint["expression"] = "raw password must match task policy before hashing; only password_hash is persisted"
			constraint["description"] = "Raw password policy is enforced before storing the password hash."
		}
		if constraint["type"] == "unique" && len(stringSliceFromAny(constraint["fields"])) == 0 {
			if field, ok := constraint["field"].(string); ok && field != "" {
				constraint["fields"] = []string{field}
			}
		}
		if isMinMaxConstraint(constraint["type"]) && constraint["value"] == nil {
			if min, ok := constraint["min"]; ok {
				constraint["value"] = min
			} else if max, ok := constraint["max"]; ok {
				constraint["value"] = max
			}
		}
		normalized = append(normalized, constraint)
	}
	model["constraints"] = normalized
}

func isMinMaxConstraint(value any) bool {
	typ, ok := value.(string)
	if !ok {
		return false
	}
	switch typ {
	case "min_inclusive", "min_exclusive", "max_inclusive", "max_exclusive":
		return true
	default:
		return false
	}
}

func normalizeCompatImportSpecs(model map[string]any) {
	items, ok := model["import_specs"].([]any)
	if !ok {
		return
	}
	for _, item := range items {
		importSpec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		source, ok := importSpec["source"].(map[string]any)
		if !ok {
			source = map[string]any{}
			importSpec["source"] = source
		}
		if source["source_id"] == nil || source["source_id"] == "" {
			source["source_id"] = "printing_house_task_text"
		}
		if source["fragment"] == nil || source["fragment"] == "" {
			fragments := stringSliceFromAny(source["source_units"])
			if len(fragments) == 0 {
				if evidence, ok := importSpec["evidence"].(map[string]any); ok {
					fragments = stringSliceFromAny(evidence["fragments"])
				}
			}
			if len(fragments) > 0 {
				source["fragment"] = fragments[0]
			}
		}
		delete(source, "source_units")
	}
}

func normalizeCompatStateMachines(model map[string]any) {
	items, ok := model["state_machines"].([]any)
	if !ok {
		return
	}
	for _, item := range items {
		machine, ok := item.(map[string]any)
		if !ok {
			continue
		}
		states := stringSliceFromAny(machine["states"])
		if machine["initial"] == nil || machine["initial"] == "" {
			if len(states) > 0 {
				machine["initial"] = states[0]
			}
		}
		if len(stringSliceFromAny(machine["terminal"])) == 0 {
			machine["terminal"] = inferTerminalStates(states, machine["transitions"])
		}
	}
}

func inferTerminalStates(states []string, rawTransitions any) []string {
	if len(states) == 0 {
		return nil
	}
	outgoing := map[string]bool{}
	transitions, _ := rawTransitions.([]any)
	for _, item := range transitions {
		transition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		from, _ := transition["from"].(string)
		if from != "" {
			outgoing[from] = true
		}
	}
	var terminals []string
	for _, state := range states {
		if !outgoing[state] {
			terminals = append(terminals, state)
		}
	}
	if len(terminals) == 0 {
		terminals = append(terminals, states[len(states)-1])
	}
	sort.Strings(terminals)
	return terminals
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func review(id, question string, atoms []string, status, option, rationale string) ReviewDecision {
	return ReviewDecision{
		ID:            id,
		Question:      question,
		AffectedAtoms: atoms,
		Decision: map[string]any{
			"status":          status,
			"selected_option": option,
			"rationale":       rationale,
		},
	}
}

func buildAtomDefs() []AtomDef {
	def := func(id, statement, atomType, relevance, area, pattern, outcome string, impacts ModelImpacts, groups [][]string, reviews ...string) AtomDef {
		return AtomDef{
			ID:                id,
			Statement:         statement,
			AtomType:          atomType,
			ModelingRelevance: relevance,
			FunctionalArea:    area,
			FunctionalPattern: pattern,
			SupportLevel:      "explicit",
			Confidence:        "high",
			RequiresReview:    len(reviews) > 0,
			ReviewDecisions:   reviews,
			ModelImpacts:      impacts,
			Outcome:           outcome,
			MatchGroups:       groups,
		}
	}
	nonModel := func(id, statement, area, pattern string, groups [][]string) AtomDef {
		return AtomDef{
			ID:                id,
			Statement:         statement,
			AtomType:          "non_model",
			ModelingRelevance: "not_in_db",
			FunctionalArea:    area,
			FunctionalPattern: pattern,
			SupportLevel:      "explicit",
			Confidence:        "high",
			Outcome:           "intentionally_not_in_db",
			MatchGroups:       groups,
		}
	}
	return []AtomDef{
		def("PHF-RA-001", "Sistem ima klijente fizicka lica, klijente pravna lica, stampare i administratora.", "actor", "direct_db", "access_and_identity", "role_based_accounts", "represented", ModelImpacts{Entities: []string{"UserAccount"}, Attributes: []string{"UserAccount.role"}}, [][]string{{"Постоје три врсте корисника"}, {"клијенти (физичка и правна лица)"}}, "PHF-RD-001"),
		def("PHF-RA-002", "Korisnici se prijavljuju username/password kredencijalima; username i email su jedinstveni, a lozinka se cuva kao hash.", "constraint", "direct_db", "access_and_identity", "credentials", "represented", ModelImpacts{Attributes: []string{"UserAccount.username", "UserAccount.password_hash", "UserAccount.email"}, Constraints: []string{"user_account_username_unique", "user_account_email_unique", "user_account_password_policy"}}, [][]string{{"коришћењем својих креденцијала"}, {"корисничко име (које је јединствено"}, {"и-мејл адреса (јединствено"}, {"Лозинку у бази података обавезно чувати као криптовану"}}, "PHF-RD-002"),
		def("PHF-RA-003", "Reset lozinke koristi privremeni link koji vazi 5 minuta.", "entity", "direct_db", "access_and_identity", "password_reset", "represented", ModelImpacts{Entities: []string{"PasswordResetToken"}, Constraints: []string{"password_reset_token_validity_window"}}, [][]string{{"заборављене лозинке"}, {"Веб линк је привремен"}}, "PHF-RD-002"),
		def("PHF-RA-004", "Registracija proizvodi zahtev koji administrator prihvata ili odbija pre aktiviranja korisnika.", "entity", "direct_db", "access_and_identity", "registration_approval", "represented", ModelImpacts{Entities: []string{"RegistrationRequest"}, StateMachines: []string{"RegistrationRequestStatusFlow", "UserAccountStatusFlow"}}, [][]string{{"креирати нови захтев за регистрацију"}, {"прихватање или одбацивање захтева"}}, "PHF-RD-001", "PHF-RD-011"),
		def("PHF-RA-005", "Korisnicki profil cuva licne podatke, kontakt, email i profilnu sliku sa upload ogranicenjima.", "attribute", "direct_db", "access_and_identity", "profile_data", "represented", ModelImpacts{Attributes: []string{"UserAccount.first_name", "UserAccount.last_name", "UserAccount.phone", "UserAccount.profile_image_path"}, FileSpecs: []string{"UserProfileImageFile"}}, [][]string{{"- име"}, {"- презиме"}, {"контакт телефон"}, {"профилна слика"}, {"минималне величине 100х100"}, {"default_profile_image.jpg"}}, "PHF-RD-003"),
		def("PHF-RA-006", "Pravna lica i stamparije imaju institucionalne podatke: naziv, sediste, maticni broj i PIB.", "entity", "direct_db", "access_and_identity", "institution_profile", "represented", ModelImpacts{Entities: []string{"Institution"}, Constraints: []string{"institution_registration_number_unique", "institution_registration_number_length", "institution_tax_number_unique", "institution_tax_number_format"}}, [][]string{{"називу своје институције"}, {"Матични број институције"}, {"ПИБ је деветоцифрени"}, {"одговорно лице институције"}}, "PHF-RD-001"),
		def("PHF-RA-007", "Pocetna strana prikazuje broj registrovanih stamparija i top 5 proizvoda po lajkovima.", "derived_view", "direct_db", "discovery_and_selection", "public_home_summary", "represented", ModelImpacts{DerivedViews: []string{"PublicHomeSummary"}}, [][]string{{"укупном броју регистрованих штампарија"}, {"ТОП 5 најбоље оцењених"}}, "PHF-RD-010"),
		def("PHF-RA-008", "Javna pretraga koristi naziv, kategoriju, aktivne proizvode na stanju, sortiranje i javne detalje.", "derived_view", "direct_db", "discovery_and_selection", "public_product_search", "represented", ModelImpacts{DerivedViews: []string{"PublicProductSearch", "PublicProductDetail"}, Attributes: []string{"Product.is_active", "Product.stock_quantity"}}, [][]string{{"форму за претраживање"}, {"Падајућа листа"}, {"активни производи на стању"}, {"ДЕТАЉИ"}, {"абецедно сортирање"}}, "PHF-RD-010"),
		def("PHF-RA-009", "Galerija proizvoda ima glavnu i do tri dodatne slike; izbor slike u cookie-ju nije trajni DB podatak.", "file_reference", "direct_db", "discovery_and_selection", "product_gallery", "represented", ModelImpacts{Entities: []string{"ProductImage"}, Constraints: []string{"product_image_gallery_limit"}, FileSpecs: []string{"ProductImageFile"}}, [][]string{{"галерију са главном сликом"}, {"thumbnail"}, {"колачићу"}}, "PHF-RD-003"),
		def("PHF-RA-010", "Klijent na profilu vidi licne podatke i istoriju/aktuelne narudzbine kroz fakture i stavke.", "derived_view", "direct_db", "transaction_lifecycle", "client_order_history", "represented", ModelImpacts{DerivedViews: []string{"ClientOrderHistory"}, Entities: []string{"Invoice", "InvoiceItem"}}, [][]string{{"Испод табеле са личним подацима"}, {"Табела треба да има следеће колоне"}, {"ИД фактуре"}}, "PHF-RD-006"),
		def("PHF-RA-011", "Prosireni detalji proizvoda obuhvataju opis, cenu, boju, usluge stampe, dodatnu cenu i dimenzije.", "relationship", "direct_db", "master_data_management", "product_detail_offer", "represented", ModelImpacts{Entities: []string{"Color", "ProductColor", "PrintService", "ProductPrintService"}, Relationships: []string{"ProductColorProduct", "ProductColorColor", "ProductPrintServiceProduct", "ProductPrintServiceService"}, Constraints: []string{"product_print_service_additional_price_non_negative", "product_print_service_dimensions_positive"}}, [][]string{{"Проширене информације обухватају"}, {"врсте штампе"}, {"додатном ценом по комаду"}}, "PHF-RD-005"),
		def("PHF-RA-012", "Klijent priprema proizvod izborom boje/usluge, tekstom ili slikom i kolicinom u korpi.", "entity", "direct_db", "transaction_lifecycle", "cart_item_preparation", "represented", ModelImpacts{Entities: []string{"Cart", "CartItem"}, FileSpecs: []string{"CartItemCustomImageFile"}}, [][]string{{"припрема производа"}, {"текст или поставља сличицу"}, {"ДОДАЈ У"}}, "PHF-RD-003"),
		def("PHF-RA-013", "Pretraga i narucivanje moraju postovati trenutno stanje lagera.", "constraint", "direct_db", "availability_and_capacity", "stock_availability", "represented", ModelImpacts{Attributes: []string{"Product.stock_quantity"}, Constraints: []string{"product_stock_quantity_non_negative", "cart_item_quantity_positive", "cart_item_requested_quantity_available"}}, [][]string{{"Нема довољно производа"}, {"количину него што постоји тренутно на лагеру"}}, "PHF-RD-011"),
		def("PHF-RA-014", "Korpa grupise stavke po stamparijama i prikazuje naziv, kolicinu, tip stampe i cenu.", "derived_view", "direct_db", "transaction_lifecycle", "cart_grouping", "represented", ModelImpacts{Entities: []string{"Cart", "CartItem"}, DerivedViews: []string{"CurrentCartGroupedByPrintShop"}}, [][]string{{"Тренутна електронска корпа"}, {"груписано по штампаријама"}}, "PHF-RD-006"),
		def("PHF-RA-015", "Potvrda korpe fizickog lica formira po jednu fakturu po stampariji sa statusnim tokom narudzbine.", "entity", "direct_db", "transaction_lifecycle", "individual_checkout_invoice", "represented", ModelImpacts{Entities: []string{"Invoice", "InvoiceItem"}, StateMachines: []string{"CartStatusFlow", "InvoiceStatusFlow"}, Constraints: []string{"invoice_cancel_before_print_only"}}, [][]string{{"формира се више различитих фактура"}, {"статуси наруџбина"}, {"статусом „наручено“"}}, "PHF-RD-006", "PHF-RD-011"),
		def("PHF-RA-016", "Fakture se generisu kao PDF fajlovi i dostavljaju emailom.", "file_reference", "direct_db", "integration_and_artifacts", "invoice_pdf", "represented", ModelImpacts{Attributes: []string{"Invoice.pdf_path"}, FileSpecs: []string{"InvoicePdfFile"}}, [][]string{{"фактуру/е доставити као PDF"}}, "PHF-RD-003"),
		def("PHF-RA-017", "Placanje je eksterni servis; baza cuva pokusaj placanja, status i provider referencu, ne podatke kartice.", "entity", "direct_db", "pricing_and_finance", "payment_attempt", "represented", ModelImpacts{Entities: []string{"PaymentAttempt"}, StateMachines: []string{"PaymentAttemptStatusFlow"}, Constraints: []string{"payment_attempt_amount_non_negative"}}, [][]string{{"бесплатан сервис за плаћање"}, {"CVC код"}, {"нови међу статус „плаћено“"}}, "PHF-RD-007", "PHF-RD-011"),
		def("PHF-RA-018", "Pravno lice potvrdom korpe otvara javnu nabavku sa stavkama i rokom za licitiranje.", "entity", "direct_db", "transaction_lifecycle", "procurement_creation", "represented", ModelImpacts{Entities: []string{"Procurement", "ProcurementItem"}, StateMachines: []string{"ProcurementStatusFlow"}}, [][]string{{"Клијент - правно лице"}, {"позив за подношење понуда"}, {"траје 10 минута"}}, "PHF-RD-008", "PHF-RD-011"),
		def("PHF-RA-019", "Javna nabavka prima ponude stamparija, bira najnizu ponudu koja pokriva kolicine i generise izvestaj.", "entity", "direct_db", "transaction_lifecycle", "procurement_bidding", "represented", ModelImpacts{Entities: []string{"ProcurementBid", "ProcurementBidItem"}, StateMachines: []string{"ProcurementBidStatusFlow"}, DerivedViews: []string{"ProcurementBidReport"}, Constraints: []string{"procurement_bid_item_unique_per_bid_item"}, FileSpecs: []string{"ProcurementReportPdfFile"}}, [][]string{{"најнижу укупну понуду"}, {"Свака јавна набавка има свој идентификатор"}, {"једну понуду"}, {"PDF извештај"}}, "PHF-RD-008", "PHF-RD-011"),
		def("PHF-RA-020", "Arhiva proizvoda prikazuje isporucene i primljene proizvode, a klijent moze promeniti status isporuceno u primljeno.", "state_machine", "direct_db", "content_and_feedback", "client_product_archive", "represented", ModelImpacts{DerivedViews: []string{"ClientProductArchive"}, StateMachines: []string{"InvoiceStatusFlow"}}, [][]string{{"Архива производа"}, {"статусом „Испоручено“"}, {"статус „Примљено“"}}, "PHF-RD-011"),
		def("PHF-RA-021", "Za primljeni proizvod klijent ostavlja like/dislike i komentar; prikazuje se broj ocena i poslednjih pet komentara.", "entity", "direct_db", "content_and_feedback", "product_feedback", "represented", ModelImpacts{Entities: []string{"ProductFeedback"}, DerivedViews: []string{"ProductDetailFeedback"}, Constraints: []string{"product_feedback_one_per_invoice_item_user"}}, [][]string{{"може оставити „свиђање“"}, {"последњих пет коментара"}, {"Коментаре које је он оставио"}}, "PHF-RD-009"),
		def("PHF-RA-022", "Stampar ima profil sa pregledom i azuriranjem licnih podataka i profilne slike.", "attribute", "direct_db", "access_and_identity", "printer_profile", "represented", ModelImpacts{Entities: []string{"UserAccount"}, FileSpecs: []string{"UserProfileImageFile"}}, [][]string{{"Штампари"}, {"Профил: преглед и ажурирање"}}, "PHF-RD-003"),
		def("PHF-RA-023", "Stampar dodaje proizvode i usluge u predefinisane kategorije i potkategorije.", "entity", "direct_db", "master_data_management", "catalog_management", "represented", ModelImpacts{Entities: []string{"PrintShop", "Category", "Subcategory", "Product", "PrintService", "ProductPrintService"}, Relationships: []string{"ProductPrintShop", "ProductSubcategory", "SubcategoryCategory", "ProductPrintServiceProduct", "ProductPrintServiceService"}}, [][]string{{"Производи и услуге"}, {"предефинисане категорије"}, {"Услуге су специјализовани типови"}}, "PHF-RD-004", "PHF-RD-005"),
		def("PHF-RA-024", "Stampar azurira kolicine postojecih proizvoda.", "attribute", "direct_db", "availability_and_capacity", "stock_update", "represented", ModelImpacts{Attributes: []string{"Product.stock_quantity"}}, [][]string{{"Ажурирање количина"}}, "PHF-RD-011"),
		def("PHF-RA-025", "Stampar unosi lager listu iz JSON fajla, a zatim dodaje slike.", "import_spec", "direct_db", "integration_and_artifacts", "json_catalog_import", "represented", ModelImpacts{ImportSpecs: []string{"PrintShopCatalogImport"}, FileSpecs: []string{"ProductImageFile"}}, [][]string{{"Додавање из фајла"}, {"JSON фајла"}, {"Прилогу 1"}}, "PHF-RD-003"),
		def("PHF-RA-026", "Stampar menja statuse porudzbina fizickih lica iz naruceno u u stampi i zatim isporuceno.", "state_machine", "direct_db", "operational_processing", "printer_invoice_processing", "represented", ModelImpacts{StateMachines: []string{"InvoiceStatusFlow"}}, [][]string{{"Наручени производи"}, {"пребаци статус из „наручено“"}, {"статус „испоручено“"}}, "PHF-RD-011"),
		def("PHF-RA-027", "Stampar salje jednu zvanicnu ponudu po javnoj nabavci sa svim trazenim proizvodima.", "entity", "direct_db", "operational_processing", "bid_submission", "represented", ModelImpacts{Entities: []string{"ProcurementBid", "ProcurementBidItem"}, Constraints: []string{"procurement_bid_one_per_print_shop", "procurement_bid_item_unique_per_bid_item", "procurement_bid_item_quantity_positive"}}, [][]string{{"Лицитације:"}, {"једну понуду"}, {"свим траженим производима"}}, "PHF-RD-008"),
		def("PHF-RA-028", "Posle licitacije ustanova dobija PDF izvestaj o svim ponudama i pobednickoj ponudi.", "file_reference", "direct_db", "reporting_and_analytics", "procurement_report", "represented", ModelImpacts{DerivedViews: []string{"ProcurementBidReport"}, FileSpecs: []string{"ProcurementReportPdfFile"}}, [][]string{{"Извештавање:"}, {"PDF извештај"}}, "PHF-RD-008"),
		def("PHF-RA-029", "Administrator pregleda, azurira, brise korisnike i odobrava registracije.", "state_machine", "direct_db", "administration_and_moderation", "user_management", "represented", ModelImpacts{StateMachines: []string{"UserAccountStatusFlow", "RegistrationRequestStatusFlow"}}, [][]string{{"Администратор управља корисницима"}, {"Одобравање захтева"}}, "PHF-RD-001", "PHF-RD-011"),
		def("PHF-RA-030", "Administrator upravlja kategorijama i potkategorijama stamparskih proizvoda.", "entity", "direct_db", "administration_and_moderation", "taxonomy_management", "represented", ModelImpacts{Entities: []string{"Category", "Subcategory"}, Constraints: []string{"category_name_unique", "subcategory_name_unique_per_category"}}, [][]string{{"Администратор управља и категоријама"}, {"додати нову категорију"}}, "PHF-RD-004"),
		def("PHF-RA-031", "Administrator vidi graficke statistike prometa, popularnosti proizvoda i kretanja ocene proizvoda.", "derived_view", "direct_db", "reporting_and_analytics", "admin_statistics", "represented", ModelImpacts{DerivedViews: []string{"AdminTrafficByPrintShop", "AdminPopularProducts", "ProductRatingTrend"}}, [][]string{{"Администратор може извући статистике"}, {"последњем кварталу"}, {"пита графикона"}, {"линијског графикона"}}, "PHF-RD-010"),
		def("PHF-RA-032", "Server mora efikasno validirati nekorektne podatke.", "constraint", "indirect_db", "non_model_requirements", "server_validation", "represented", ModelImpacts{Constraints: []string{"server_side_validation_required"}}, [][]string{{"отпорна на унос некоректних података"}, {"серверске валидације"}}, "PHF-RD-011"),
		def("PHF-RA-033", "Baza se inicijalno kreira i popunjava nezavisno od aplikacije; aplikacija ne treba da kreira tabele.", "scope_exclusion", "out_of_scope", "non_model_requirements", "database_lifecycle_boundary", "intentionally_not_in_db", ModelImpacts{}, [][]string{{"база података иницијално креира"}, {"табеле или колекције у бази не треба креирати"}}),
		def("PHF-RA-034", "Na odbranu treba doneti dovoljno popunjenu bazu za prikaz funkcionalnosti.", "scope_exclusion", "indirect_db", "non_model_requirements", "seed_data_requirement", "intentionally_not_in_db", ModelImpacts{}, [][]string{{"попуњена са довољном количином података"}}),
		nonModel("PHF-RA-035", "UI, CSS, responsive dizajn, browser testiranje, tehnologije i pravila odbrane ne proizvode relacione tabele.", "non_model_requirements", "ui_technology_exam_rules", [][]string{{"Cascading Style Sheets"}, {"responsive web design"}, {"Тестирати веб апликацију"}, {"Алати вештачке интелигенције"}, {"Angular 20 framework"}}),
		def("PHF-RA-036", "JSON prilog definise polja stamparije, proizvoda, boja, slika i usluga stampe za import.", "import_spec", "direct_db", "integration_and_artifacts", "json_catalog_shape", "represented", ModelImpacts{ImportSpecs: []string{"PrintShopCatalogImport"}, Entities: []string{"PrintShop", "Product", "Color", "PrintService", "ProductPrintService"}, Attributes: []string{"PrintShop.code", "PrintShop.display_name"}, Constraints: []string{"print_shop_code_unique"}}, [][]string{{"\"stampaorijaId\""}, {"\"nazivStamparije\""}, {"\"proizvodi\""}, {"\"uslugeStampe\""}, {"\"dodatnaCenaPoKomadu\""}}, "PHF-RD-003", "PHF-RD-005"),
	}
}

func buildActors() []Actor {
	return []Actor{
		{"unauthenticated_user", "Neregistrovani korisnik", "Korisnik koji nije prijavljen; vidi javnu pocetnu stranu, pretragu, detalje i forme za prijavu/registraciju/reset."},
		{"client", "Klijent", "Zajednicki akter za fizicka i pravna lica kada funkcionalnost nije specificna za podtip."},
		{"client_individual", "Klijent fizicko lice", "Klijent koji potvrdom korpe direktno formira fakture i opcionalno placa."},
		{"client_legal", "Klijent pravno lice", "Klijent/institucija koja potvrdom korpe pokrece javnu nabavku."},
		{"printer", "Stampar", "Korisnik stamparije koji upravlja katalogom, kolicinama, narudzbinama i ponudama."},
		{"administrator", "Administrator", "Privilegovani korisnik za korisnike, registracije, taksonomiju i statistike."},
		{"system", "Sistem", "Aplikaciona logika koja generise dokumente, zakljucuje javne nabavke i izvrsava izvedene operacije."},
		{"external_payment_provider", "Eksterni servis placanja", "Stripe/PayPal sandbox servis; ne modeluje se kao interna tabela."},
	}
}

func buildOperations(atomRefs map[string][]string) []Operation {
	defs := []OperationDef{
		{"public_home_summary", "Pregled javne pocetne strane", "discovery_and_selection", "public_home_summary", "unauthenticated_user", []string{"PHF-RA-007"}, "Citanje agregata za broj stamparija i top 5 proizvoda."},
		{"public_search_catalog", "Javna pretraga kataloga", "discovery_and_selection", "public_product_search", "unauthenticated_user", []string{"PHF-RA-008", "PHF-RA-009"}, "Pretraga aktivnih proizvoda po nazivu i kategoriji i pregled javnih detalja."},
		{"register_user", "Registracija korisnika", "access_and_identity", "registration_submission", "unauthenticated_user", []string{"PHF-RA-001", "PHF-RA-002", "PHF-RA-004", "PHF-RA-005", "PHF-RA-006"}, "Kreiranje naloga/profila i zahteva za odobrenje registracije."},
		{"authenticate_user", "Prijavljivanje korisnika", "access_and_identity", "login", "unauthenticated_user", []string{"PHF-RA-001", "PHF-RA-002"}, "Provera kredencijala i statusa naloga."},
		{"request_password_reset", "Zahtev za reset lozinke", "access_and_identity", "password_reset", "unauthenticated_user", []string{"PHF-RA-003"}, "Kreiranje privremenog reset tokena."},
		{"reset_password", "Postavljanje nove lozinke", "access_and_identity", "password_reset", "unauthenticated_user", []string{"PHF-RA-002", "PHF-RA-003"}, "Validacija tokena i azuriranje hash-a lozinke."},
		{"approve_registration", "Odobravanje registracije", "administration_and_moderation", "registration_approval", "administrator", []string{"PHF-RA-004", "PHF-RA-029"}, "Administrator prihvata ili odbija registracioni zahtev."},
		{"manage_user_accounts", "Upravljanje korisnicima", "administration_and_moderation", "user_management", "administrator", []string{"PHF-RA-029"}, "Pregled, azuriranje i brisanje korisnickih naloga."},
		{"manage_taxonomy", "Upravljanje kategorijama", "administration_and_moderation", "taxonomy_management", "administrator", []string{"PHF-RA-030"}, "Dodavanje i azuriranje kategorija i potkategorija."},
		{"view_admin_statistics", "Pregled administratorskih statistika", "reporting_and_analytics", "admin_statistics", "administrator", []string{"PHF-RA-031"}, "Citanje agregata za grafikone."},
		{"view_update_profile", "Pregled i azuriranje profila", "access_and_identity", "profile_data", "client", []string{"PHF-RA-005", "PHF-RA-010", "PHF-RA-022"}, "Klijent ili stampar cita i azurira dozvoljene licne podatke."},
		{"client_search_products", "Klijentska pretraga proizvoda", "discovery_and_selection", "client_product_search", "client", []string{"PHF-RA-008", "PHF-RA-011"}, "Klijent cita prosirene detalje proizvoda i opcije stampe."},
		{"prepare_cart_item", "Priprema proizvoda za korpu", "transaction_lifecycle", "cart_item_preparation", "client", []string{"PHF-RA-011", "PHF-RA-012", "PHF-RA-013"}, "Izbor boje, usluge, custom teksta/slike i kolicine."},
		{"confirm_individual_cart", "Potvrda korpe fizickog lica", "transaction_lifecycle", "individual_checkout_invoice", "client_individual", []string{"PHF-RA-014", "PHF-RA-015", "PHF-RA-016"}, "Formiranje faktura po stamparijama i PDF artefakata."},
		{"pay_invoice", "Placanje fakture", "pricing_and_finance", "payment_attempt", "client_individual", []string{"PHF-RA-017"}, "Kreiranje i azuriranje pokusaja placanja."},
		{"create_procurement_from_cart", "Formiranje javne nabavke", "transaction_lifecycle", "procurement_creation", "client_legal", []string{"PHF-RA-018"}, "Potvrda korpe pravnog lica stvara javnu nabavku sa stavkama."},
		{"view_procurement_report", "Pregled izvestaja javne nabavke", "reporting_and_analytics", "procurement_report", "client_legal", []string{"PHF-RA-028"}, "Ustanova cita PDF izvestaj posle zavrsene licitacije."},
		{"confirm_received_and_feedback", "Potvrda prijema i feedback", "content_and_feedback", "product_feedback", "client", []string{"PHF-RA-020", "PHF-RA-021"}, "Klijent menja status u primljeno i ostavlja ocenu/komentar."},
		{"manage_products_services", "Upravljanje proizvodima i uslugama", "master_data_management", "catalog_management", "printer", []string{"PHF-RA-023"}, "Stampar dodaje i azurira proizvode, slike, boje i usluge."},
		{"update_stock", "Azuriranje lagera", "availability_and_capacity", "stock_update", "printer", []string{"PHF-RA-024"}, "Stampar menja kolicinu proizvoda na lageru."},
		{"import_catalog_json", "Import lager liste", "integration_and_artifacts", "json_catalog_import", "printer", []string{"PHF-RA-025", "PHF-RA-036"}, "Stampar unosi JSON fajl kataloga i potom slike."},
		{"process_individual_orders", "Obrada porudzbina fizickih lica", "operational_processing", "printer_invoice_processing", "printer", []string{"PHF-RA-015", "PHF-RA-026"}, "Stampar menja status fakture/narudzbine."},
		{"submit_procurement_bid", "Slanje ponude za javnu nabavku", "operational_processing", "bid_submission", "printer", []string{"PHF-RA-019", "PHF-RA-027"}, "Stampar salje jednu ponudu sa stavkama za nabavku."},
		{"close_procurement", "Zakljucenje javne nabavke", "transaction_lifecycle", "procurement_closing", "system", []string{"PHF-RA-018", "PHF-RA-019", "PHF-RA-028"}, "Sistem po isteku intervala bira najnizu validnu ponudu i generise izvestaj."},
		{"generate_invoice_pdf", "Generisanje fakture PDF", "integration_and_artifacts", "invoice_pdf", "system", []string{"PHF-RA-016"}, "Sistem generise PDF fakture i cuva referencu."},
		{"record_payment_callback", "Evidentiranje ishoda placanja", "pricing_and_finance", "payment_callback", "external_payment_provider", []string{"PHF-RA-017"}, "Eksterni servis vraca ishod koji sistem belezi kao status pokusaja."},
	}
	ops := make([]Operation, 0, len(defs))
	for _, def := range defs {
		ops = append(ops, Operation{
			ID:                def.ID,
			Label:             def.Label,
			FunctionalArea:    def.FunctionalArea,
			FunctionalPattern: def.FunctionalPattern,
			Actor:             def.Actor,
			SourceAtoms:       def.SourceAtoms,
			SourceUnits:       unionAtomRefs(def.SourceAtoms, atomRefs),
			Description:       def.Description,
		})
	}
	return ops
}

func buildEntities(atomRefs map[string][]string, defs map[string]AtomDef) []map[string]any {
	req := true
	opt := false
	return []map[string]any{
		entity("UserAccount", "user_accounts", "regular", []string{"PHF-RA-001", "PHF-RA-002", "PHF-RA-005", "PHF-RA-022"}, atomRefs, defs, []map[string]any{
			attr("username", "string", req, []string{"PHF-RA-002"}, atomRefs, defs),
			attr("password_hash", "string", req, []string{"PHF-RA-002"}, atomRefs, defs),
			attr("first_name", "string", req, []string{"PHF-RA-005"}, atomRefs, defs),
			attr("last_name", "string", req, []string{"PHF-RA-005"}, atomRefs, defs),
			attr("phone", "phone", req, []string{"PHF-RA-005"}, atomRefs, defs),
			attr("email", "email", req, []string{"PHF-RA-002", "PHF-RA-005"}, atomRefs, defs),
			attr("profile_image_path", "file_path", opt, []string{"PHF-RA-005"}, atomRefs, defs),
			attrEnum("role", []string{"client_individual", "client_legal", "printer", "administrator"}, req, []string{"PHF-RA-001"}, atomRefs, defs),
			attrEnum("account_status", []string{"pending", "active", "rejected", "disabled"}, req, []string{"PHF-RA-004", "PHF-RA-029"}, atomRefs, defs),
			attr("created_at", "datetime", req, []string{"PHF-RA-004"}, atomRefs, defs),
		}),
		entity("Institution", "institutions", "regular", []string{"PHF-RA-006"}, atomRefs, defs, []map[string]any{
			attr("name", "string", req, []string{"PHF-RA-006"}, atomRefs, defs),
			attr("headquarters_address", "string", req, []string{"PHF-RA-006"}, atomRefs, defs),
			attr("registration_number", "string", req, []string{"PHF-RA-006"}, atomRefs, defs),
			attr("tax_number", "string", req, []string{"PHF-RA-006"}, atomRefs, defs),
			attrEnum("institution_type", []string{"legal_client", "print_shop"}, req, []string{"PHF-RA-006"}, atomRefs, defs),
		}),
		entity("PrintShop", "print_shops", "regular", []string{"PHF-RA-006", "PHF-RA-023"}, atomRefs, defs, []map[string]any{
			attr("code", "string", req, []string{"PHF-RA-036"}, atomRefs, defs),
			attr("display_name", "string", req, []string{"PHF-RA-023", "PHF-RA-036"}, atomRefs, defs),
			attr("city", "string", req, []string{"PHF-RA-008", "PHF-RA-011"}, atomRefs, defs),
			attr("map_latitude", "decimal", opt, []string{"PHF-RA-011"}, atomRefs, defs),
			attr("map_longitude", "decimal", opt, []string{"PHF-RA-011"}, atomRefs, defs),
			attr("is_active", "boolean", req, []string{"PHF-RA-007", "PHF-RA-008"}, atomRefs, defs),
		}),
		entity("PasswordResetToken", "password_reset_tokens", "regular", []string{"PHF-RA-003"}, atomRefs, defs, []map[string]any{
			attr("token_hash", "string", req, []string{"PHF-RA-003"}, atomRefs, defs),
			attr("requested_at", "datetime", req, []string{"PHF-RA-003"}, atomRefs, defs),
			attr("expires_at", "datetime", req, []string{"PHF-RA-003"}, atomRefs, defs),
			attr("used_at", "datetime", opt, []string{"PHF-RA-003"}, atomRefs, defs),
		}),
		entity("RegistrationRequest", "registration_requests", "regular", []string{"PHF-RA-004"}, atomRefs, defs, []map[string]any{
			attrEnum("requested_role", []string{"client_individual", "client_legal", "printer"}, req, []string{"PHF-RA-001", "PHF-RA-004"}, atomRefs, defs),
			attrEnum("status", []string{"pending", "accepted", "rejected"}, req, []string{"PHF-RA-004"}, atomRefs, defs),
			attr("submitted_at", "datetime", req, []string{"PHF-RA-004"}, atomRefs, defs),
			attr("decided_at", "datetime", opt, []string{"PHF-RA-004"}, atomRefs, defs),
		}),
		entity("Category", "categories", "regular", []string{"PHF-RA-023", "PHF-RA-030"}, atomRefs, defs, []map[string]any{attr("name", "string", req, []string{"PHF-RA-023", "PHF-RA-030"}, atomRefs, defs)}),
		entity("Subcategory", "subcategories", "regular", []string{"PHF-RA-023", "PHF-RA-030"}, atomRefs, defs, []map[string]any{attr("name", "string", req, []string{"PHF-RA-023", "PHF-RA-030"}, atomRefs, defs)}),
		entity("Product", "products", "regular", []string{"PHF-RA-008", "PHF-RA-011", "PHF-RA-023", "PHF-RA-036"}, atomRefs, defs, []map[string]any{
			attr("code", "string", req, []string{"PHF-RA-036"}, atomRefs, defs),
			attr("name", "string", req, []string{"PHF-RA-008", "PHF-RA-036"}, atomRefs, defs),
			attr("description", "text", req, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs),
			attr("unit_price", "money", req, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs),
			attr("stock_quantity", "integer", req, []string{"PHF-RA-013", "PHF-RA-024", "PHF-RA-036"}, atomRefs, defs),
			attr("is_active", "boolean", req, []string{"PHF-RA-008", "PHF-RA-013"}, atomRefs, defs),
		}),
		entity("ProductImage", "product_images", "regular", []string{"PHF-RA-009", "PHF-RA-025"}, atomRefs, defs, []map[string]any{
			attr("storage_path", "file_path", req, []string{"PHF-RA-009", "PHF-RA-025"}, atomRefs, defs),
			attrEnum("image_role", []string{"main", "additional"}, req, []string{"PHF-RA-009"}, atomRefs, defs),
			attr("display_order", "integer", req, []string{"PHF-RA-009"}, atomRefs, defs),
		}),
		entity("Color", "colors", "lookup", []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs, []map[string]any{attr("name", "string", req, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs)}),
		entity("ProductColor", "product_colors", "association", []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs, nil),
		entity("PrintService", "print_services", "lookup", []string{"PHF-RA-011", "PHF-RA-023", "PHF-RA-036"}, atomRefs, defs, []map[string]any{
			attr("code", "string", req, []string{"PHF-RA-036"}, atomRefs, defs),
			attr("name", "string", req, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs),
		}),
		entity("ProductPrintService", "product_print_services", "regular", []string{"PHF-RA-011", "PHF-RA-023", "PHF-RA-036"}, atomRefs, defs, []map[string]any{
			attr("additional_price", "money", req, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs),
			attr("max_width_mm", "integer", req, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs),
			attr("max_height_mm", "integer", req, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs),
		}),
		entity("Cart", "carts", "regular", []string{"PHF-RA-012", "PHF-RA-014"}, atomRefs, defs, []map[string]any{
			attrEnum("status", []string{"active", "confirmed", "cancelled"}, req, []string{"PHF-RA-014", "PHF-RA-015"}, atomRefs, defs),
			attr("created_at", "datetime", req, []string{"PHF-RA-012"}, atomRefs, defs),
			attr("confirmed_at", "datetime", opt, []string{"PHF-RA-015", "PHF-RA-018"}, atomRefs, defs),
		}),
		entity("CartItem", "cart_items", "regular", []string{"PHF-RA-012", "PHF-RA-013", "PHF-RA-014"}, atomRefs, defs, []map[string]any{
			attr("quantity", "integer", req, []string{"PHF-RA-012", "PHF-RA-013"}, atomRefs, defs),
			attr("custom_text", "text", opt, []string{"PHF-RA-012"}, atomRefs, defs),
			attr("custom_image_path", "file_path", opt, []string{"PHF-RA-012"}, atomRefs, defs),
			attr("unit_product_price", "money", req, []string{"PHF-RA-011", "PHF-RA-014"}, atomRefs, defs),
			attr("print_service_price", "money", opt, []string{"PHF-RA-011", "PHF-RA-014"}, atomRefs, defs),
			attr("total_price", "money", req, []string{"PHF-RA-014"}, atomRefs, defs),
		}),
		entity("Invoice", "invoices", "regular", []string{"PHF-RA-015", "PHF-RA-016", "PHF-RA-017"}, atomRefs, defs, []map[string]any{
			attr("invoice_number", "string", req, []string{"PHF-RA-015"}, atomRefs, defs),
			attrEnum("status", []string{"ordered", "paid", "in_print", "delivered", "received", "cancelled"}, req, []string{"PHF-RA-015", "PHF-RA-017", "PHF-RA-020", "PHF-RA-026"}, atomRefs, defs),
			attr("total_amount", "money", req, []string{"PHF-RA-014", "PHF-RA-015"}, atomRefs, defs),
			attr("pdf_path", "file_path", opt, []string{"PHF-RA-016"}, atomRefs, defs),
			attr("issued_at", "datetime", req, []string{"PHF-RA-015"}, atomRefs, defs),
		}),
		entity("InvoiceItem", "invoice_items", "regular", []string{"PHF-RA-010", "PHF-RA-015", "PHF-RA-021"}, atomRefs, defs, []map[string]any{
			attr("quantity", "integer", req, []string{"PHF-RA-010", "PHF-RA-015"}, atomRefs, defs),
			attr("product_name_snapshot", "string", req, []string{"PHF-RA-010", "PHF-RA-015"}, atomRefs, defs),
			attr("unit_product_price", "money", req, []string{"PHF-RA-011", "PHF-RA-015"}, atomRefs, defs),
			attr("print_service_price", "money", opt, []string{"PHF-RA-011", "PHF-RA-015"}, atomRefs, defs),
			attr("total_price", "money", req, []string{"PHF-RA-015"}, atomRefs, defs),
			attr("custom_text", "text", opt, []string{"PHF-RA-012"}, atomRefs, defs),
			attr("custom_image_path", "file_path", opt, []string{"PHF-RA-012"}, atomRefs, defs),
		}),
		entity("PaymentAttempt", "payment_attempts", "regular", []string{"PHF-RA-017"}, atomRefs, defs, []map[string]any{
			attr("provider", "string", req, []string{"PHF-RA-017"}, atomRefs, defs),
			attrEnum("status", []string{"started", "succeeded", "failed"}, req, []string{"PHF-RA-017"}, atomRefs, defs),
			attr("amount", "money", req, []string{"PHF-RA-017"}, atomRefs, defs),
			attr("provider_reference", "string", opt, []string{"PHF-RA-017"}, atomRefs, defs),
			attr("created_at", "datetime", req, []string{"PHF-RA-017"}, atomRefs, defs),
		}),
		entity("Procurement", "procurements", "regular", []string{"PHF-RA-018", "PHF-RA-019", "PHF-RA-028"}, atomRefs, defs, []map[string]any{
			attrEnum("status", []string{"open", "closed", "awarded", "cancelled"}, req, []string{"PHF-RA-018", "PHF-RA-019"}, atomRefs, defs),
			attr("opened_at", "datetime", req, []string{"PHF-RA-018"}, atomRefs, defs),
			attr("closes_at", "datetime", req, []string{"PHF-RA-018"}, atomRefs, defs),
			attr("closed_at", "datetime", opt, []string{"PHF-RA-019"}, atomRefs, defs),
			attr("report_pdf_path", "file_path", opt, []string{"PHF-RA-028"}, atomRefs, defs),
		}),
		entity("ProcurementItem", "procurement_items", "regular", []string{"PHF-RA-018"}, atomRefs, defs, []map[string]any{attr("quantity", "integer", req, []string{"PHF-RA-018"}, atomRefs, defs)}),
		entity("ProcurementBid", "procurement_bids", "regular", []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs, []map[string]any{
			attrEnum("status", []string{"submitted", "winning", "not_selected", "invalid"}, req, []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs),
			attr("total_amount", "money", req, []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs),
			attr("submitted_at", "datetime", req, []string{"PHF-RA-027"}, atomRefs, defs),
		}),
		entity("ProcurementBidItem", "procurement_bid_items", "regular", []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs, []map[string]any{
			attr("offered_quantity", "integer", req, []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs),
			attr("unit_price", "money", req, []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs),
			attr("line_total", "money", req, []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs),
		}),
		entity("ProductFeedback", "product_feedback", "regular", []string{"PHF-RA-021"}, atomRefs, defs, []map[string]any{
			attrEnum("vote", []string{"like", "dislike"}, req, []string{"PHF-RA-021"}, atomRefs, defs),
			attr("comment", "text", opt, []string{"PHF-RA-021"}, atomRefs, defs),
			attr("created_at", "datetime", req, []string{"PHF-RA-021"}, atomRefs, defs),
		}),
	}
}

func buildRelationships(atomRefs map[string][]string, defs map[string]AtomDef) []map[string]any {
	rel := func(id, from, to, cardinality string, required bool, atoms []string) map[string]any {
		return map[string]any{
			"id":          id,
			"label":       id,
			"description": fmt.Sprintf("%s -> %s", from, to),
			"from":        from,
			"to":          to,
			"cardinality": cardinality,
			"required":    required,
			"evidence":    evidence(atoms, atomRefs, defs),
		}
	}
	return []map[string]any{
		rel("UserAccountInstitution", "UserAccount", "Institution", "many_to_one", false, []string{"PHF-RA-006"}),
		rel("PrintShopInstitution", "PrintShop", "Institution", "one_to_one", true, []string{"PHF-RA-006", "PHF-RA-023"}),
		rel("PrintShopOwner", "PrintShop", "UserAccount", "many_to_one", true, []string{"PHF-RA-001", "PHF-RA-023"}),
		rel("PasswordResetTokenUser", "PasswordResetToken", "UserAccount", "many_to_one", true, []string{"PHF-RA-003"}),
		rel("RegistrationRequestUser", "RegistrationRequest", "UserAccount", "one_to_one", true, []string{"PHF-RA-004"}),
		rel("SubcategoryCategory", "Subcategory", "Category", "many_to_one", true, []string{"PHF-RA-023", "PHF-RA-030"}),
		rel("ProductPrintShop", "Product", "PrintShop", "many_to_one", true, []string{"PHF-RA-023"}),
		rel("ProductSubcategory", "Product", "Subcategory", "many_to_one", true, []string{"PHF-RA-023"}),
		rel("ProductImageProduct", "ProductImage", "Product", "many_to_one", true, []string{"PHF-RA-009", "PHF-RA-025"}),
		rel("ProductColorProduct", "ProductColor", "Product", "many_to_one", true, []string{"PHF-RA-011", "PHF-RA-036"}),
		rel("ProductColorColor", "ProductColor", "Color", "many_to_one", true, []string{"PHF-RA-011", "PHF-RA-036"}),
		rel("ProductPrintServiceProduct", "ProductPrintService", "Product", "many_to_one", true, []string{"PHF-RA-011", "PHF-RA-023", "PHF-RA-036"}),
		rel("ProductPrintServiceService", "ProductPrintService", "PrintService", "many_to_one", true, []string{"PHF-RA-011", "PHF-RA-023", "PHF-RA-036"}),
		rel("CartUser", "Cart", "UserAccount", "many_to_one", true, []string{"PHF-RA-012", "PHF-RA-014"}),
		rel("CartItemCart", "CartItem", "Cart", "many_to_one", true, []string{"PHF-RA-012", "PHF-RA-014"}),
		rel("CartItemProduct", "CartItem", "Product", "many_to_one", true, []string{"PHF-RA-012", "PHF-RA-013"}),
		rel("CartItemColor", "CartItem", "Color", "many_to_one", false, []string{"PHF-RA-011", "PHF-RA-012"}),
		rel("CartItemPrintService", "CartItem", "ProductPrintService", "many_to_one", false, []string{"PHF-RA-011", "PHF-RA-012"}),
		rel("InvoiceClient", "Invoice", "UserAccount", "many_to_one", true, []string{"PHF-RA-015"}),
		rel("InvoicePrintShop", "Invoice", "PrintShop", "many_to_one", true, []string{"PHF-RA-015"}),
		rel("InvoiceItemInvoice", "InvoiceItem", "Invoice", "many_to_one", true, []string{"PHF-RA-015"}),
		rel("InvoiceItemProduct", "InvoiceItem", "Product", "many_to_one", true, []string{"PHF-RA-015", "PHF-RA-021"}),
		rel("InvoiceItemColor", "InvoiceItem", "Color", "many_to_one", false, []string{"PHF-RA-011", "PHF-RA-015"}),
		rel("InvoiceItemPrintService", "InvoiceItem", "ProductPrintService", "many_to_one", false, []string{"PHF-RA-011", "PHF-RA-015"}),
		rel("PaymentAttemptInvoice", "PaymentAttempt", "Invoice", "many_to_one", true, []string{"PHF-RA-017"}),
		rel("ProcurementInstitution", "Procurement", "Institution", "many_to_one", true, []string{"PHF-RA-018"}),
		rel("ProcurementItemProcurement", "ProcurementItem", "Procurement", "many_to_one", true, []string{"PHF-RA-018"}),
		rel("ProcurementItemProduct", "ProcurementItem", "Product", "many_to_one", true, []string{"PHF-RA-018"}),
		rel("ProcurementBidProcurement", "ProcurementBid", "Procurement", "many_to_one", true, []string{"PHF-RA-019", "PHF-RA-027"}),
		rel("ProcurementBidPrintShop", "ProcurementBid", "PrintShop", "many_to_one", true, []string{"PHF-RA-019", "PHF-RA-027"}),
		rel("ProcurementWinningBid", "Procurement", "ProcurementBid", "many_to_one", false, []string{"PHF-RA-019"}),
		rel("ProcurementBidItemBid", "ProcurementBidItem", "ProcurementBid", "many_to_one", true, []string{"PHF-RA-019", "PHF-RA-027"}),
		rel("ProcurementBidItemProcurementItem", "ProcurementBidItem", "ProcurementItem", "many_to_one", true, []string{"PHF-RA-019", "PHF-RA-027"}),
		rel("ProductFeedbackUser", "ProductFeedback", "UserAccount", "many_to_one", true, []string{"PHF-RA-021"}),
		rel("ProductFeedbackProduct", "ProductFeedback", "Product", "many_to_one", true, []string{"PHF-RA-021"}),
		rel("ProductFeedbackInvoiceItem", "ProductFeedback", "InvoiceItem", "one_to_one", true, []string{"PHF-RA-021"}),
	}
}

func buildConstraints(atomRefs map[string][]string, defs map[string]AtomDef) []map[string]any {
	return []map[string]any{
		constraint("user_account_username_unique", "unique", "UserAccount", []string{"username"}, nil, []string{"PHF-RA-002"}, atomRefs, defs),
		constraint("user_account_email_unique", "unique", "UserAccount", []string{"email"}, nil, []string{"PHF-RA-002"}, atomRefs, defs),
		constraint("user_account_password_policy", "check", "UserAccount", []string{}, map[string]any{"expression": "raw password must satisfy task policy before hashing; only password_hash is persisted"}, []string{"PHF-RA-002"}, atomRefs, defs),
		constraint("institution_registration_number_unique", "unique", "Institution", []string{"registration_number"}, nil, []string{"PHF-RA-006"}, atomRefs, defs),
		constraint("institution_registration_number_length", "length", "Institution", []string{"registration_number"}, map[string]any{"value": 8}, []string{"PHF-RA-006"}, atomRefs, defs),
		constraint("institution_tax_number_unique", "unique", "Institution", []string{"tax_number"}, nil, []string{"PHF-RA-006"}, atomRefs, defs),
		constraint("institution_tax_number_format", "regex", "Institution", []string{"tax_number"}, map[string]any{"pattern": "^[1-9][0-9]{8}$"}, []string{"PHF-RA-006"}, atomRefs, defs),
		constraint("print_shop_code_unique", "unique", "PrintShop", []string{"code"}, nil, []string{"PHF-RA-036"}, atomRefs, defs),
		constraint("password_reset_token_validity_window", "check", "PasswordResetToken", []string{"requested_at", "expires_at"}, map[string]any{"expression": "expires_at = requested_at + interval '5 minutes'"}, []string{"PHF-RA-003"}, atomRefs, defs),
		constraint("category_name_unique", "unique", "Category", []string{"name"}, nil, []string{"PHF-RA-030"}, atomRefs, defs),
		constraint("subcategory_name_unique_per_category", "unique", "Subcategory", []string{"category_id", "name"}, nil, []string{"PHF-RA-030"}, atomRefs, defs),
		constraint("product_code_unique_per_print_shop", "unique", "Product", []string{"print_shop_id", "code"}, nil, []string{"PHF-RA-023", "PHF-RA-036"}, atomRefs, defs),
		constraint("product_unit_price_positive", "min_exclusive", "Product", []string{"unit_price"}, map[string]any{"min": 0}, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs),
		constraint("product_stock_quantity_non_negative", "min_inclusive", "Product", []string{"stock_quantity"}, map[string]any{"min": 0}, []string{"PHF-RA-013", "PHF-RA-024"}, atomRefs, defs),
		constraint("product_image_gallery_limit", "check", "ProductImage", []string{"product_id"}, map[string]any{"expression": "at most 1 main image and 3 additional images per product"}, []string{"PHF-RA-009"}, atomRefs, defs),
		constraint("color_name_unique", "unique", "Color", []string{"name"}, nil, []string{"PHF-RA-011", "PHF-RA-036"}, atomRefs, defs),
		constraint("product_color_unique", "unique", "ProductColor", []string{"product_id", "color_id"}, nil, []string{"PHF-RA-011"}, atomRefs, defs),
		constraint("print_service_code_unique", "unique", "PrintService", []string{"code"}, nil, []string{"PHF-RA-036"}, atomRefs, defs),
		constraint("product_print_service_unique", "unique", "ProductPrintService", []string{"product_id", "print_service_id"}, nil, []string{"PHF-RA-011", "PHF-RA-023"}, atomRefs, defs),
		constraint("product_print_service_additional_price_non_negative", "min_inclusive", "ProductPrintService", []string{"additional_price"}, map[string]any{"min": 0}, []string{"PHF-RA-011"}, atomRefs, defs),
		constraint("product_print_service_dimensions_positive", "check", "ProductPrintService", []string{"max_width_mm", "max_height_mm"}, map[string]any{"expression": "max_width_mm > 0 and max_height_mm > 0"}, []string{"PHF-RA-011"}, atomRefs, defs),
		constraint("cart_item_quantity_positive", "min_exclusive", "CartItem", []string{"quantity"}, map[string]any{"min": 0}, []string{"PHF-RA-012", "PHF-RA-013"}, atomRefs, defs),
		constraint("cart_item_requested_quantity_available", "check", "CartItem", []string{"quantity", "product_id"}, map[string]any{"expression": "quantity <= current product.stock_quantity at checkout"}, []string{"PHF-RA-013"}, atomRefs, defs),
		constraint("invoice_number_unique", "unique", "Invoice", []string{"invoice_number"}, nil, []string{"PHF-RA-015"}, atomRefs, defs),
		constraint("invoice_total_amount_non_negative", "min_inclusive", "Invoice", []string{"total_amount"}, map[string]any{"min": 0}, []string{"PHF-RA-015"}, atomRefs, defs),
		constraint("invoice_item_quantity_positive", "min_exclusive", "InvoiceItem", []string{"quantity"}, map[string]any{"min": 0}, []string{"PHF-RA-015"}, atomRefs, defs),
		constraint("invoice_cancel_before_print_only", "check", "Invoice", []string{"status"}, map[string]any{"expression": "cancel allowed only while status = ordered"}, []string{"PHF-RA-015", "PHF-RA-026"}, atomRefs, defs),
		constraint("payment_attempt_amount_non_negative", "min_inclusive", "PaymentAttempt", []string{"amount"}, map[string]any{"min": 0}, []string{"PHF-RA-017"}, atomRefs, defs),
		constraint("procurement_item_quantity_positive", "min_exclusive", "ProcurementItem", []string{"quantity"}, map[string]any{"min": 0}, []string{"PHF-RA-018"}, atomRefs, defs),
		constraint("procurement_bid_one_per_print_shop", "unique", "ProcurementBid", []string{"procurement_id", "print_shop_id"}, nil, []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs),
		constraint("procurement_bid_total_amount_non_negative", "min_inclusive", "ProcurementBid", []string{"total_amount"}, map[string]any{"min": 0}, []string{"PHF-RA-019"}, atomRefs, defs),
		constraint("procurement_bid_item_unique_per_bid_item", "unique", "ProcurementBidItem", []string{"procurement_bid_id", "procurement_item_id"}, nil, []string{"PHF-RA-019", "PHF-RA-027"}, atomRefs, defs),
		constraint("procurement_bid_item_quantity_positive", "min_exclusive", "ProcurementBidItem", []string{"offered_quantity"}, map[string]any{"min": 0}, []string{"PHF-RA-027"}, atomRefs, defs),
		constraint("procurement_bid_must_cover_requested_quantity", "check", "ProcurementBidItem", []string{"offered_quantity"}, map[string]any{"expression": "offered_quantity >= procurement_item.quantity for bid to be valid"}, []string{"PHF-RA-019"}, atomRefs, defs),
		constraint("product_feedback_one_per_invoice_item_user", "unique", "ProductFeedback", []string{"invoice_item_id", "user_account_id"}, nil, []string{"PHF-RA-021"}, atomRefs, defs),
		constraint("server_side_validation_required", "check", "model", []string{}, map[string]any{"expression": "all persisted input must be server-validated before write"}, []string{"PHF-RA-032"}, atomRefs, defs),
	}
}

func buildImportSpecs(atomRefs map[string][]string, defs map[string]AtomDef) []map[string]any {
	atoms := []string{"PHF-RA-025", "PHF-RA-036"}
	return []map[string]any{
		{
			"id":          "PrintShopCatalogImport",
			"label":       "Print shop catalog JSON import",
			"description": "Import lager liste proizvoda, boja, slika i usluga stampe iz JSON fajla.",
			"format":      "json",
			"source": map[string]any{
				"source_id":    "ph_catalog_json_appendix",
				"file":         "Appendix 1 in task text",
				"source_units": unionAtomRefs(atoms, atomRefs),
			},
			"root": "$",
			"mappings": []map[string]any{
				{"source_path": "$.stampaorijaId", "target": "PrintShop.code"},
				{"source_path": "$.nazivStamparije", "target": "PrintShop.display_name"},
				{"source_path": "$.proizvodi[*].sifra", "target": "Product.code"},
				{"source_path": "$.proizvodi[*].naziv", "target": "Product.name"},
				{"source_path": "$.proizvodi[*].opis", "target": "Product.description"},
				{"source_path": "$.proizvodi[*].kategorija", "target": "Category.name"},
				{"source_path": "$.proizvodi[*].potkategorija", "target": "Subcategory.name"},
				{"source_path": "$.proizvodi[*].jedinicnaCena", "target": "Product.unit_price"},
				{"source_path": "$.proizvodi[*].kolicinaNaLageru", "target": "Product.stock_quantity"},
				{"source_path": "$.proizvodi[*].dostupneBoje[*]", "target": "Color.name"},
				{"source_path": "$.proizvodi[*].slikaUrl", "target": "ProductImage.storage_path"},
				{"source_path": "$.proizvodi[*].dodatneSlike[*]", "target": "ProductImage.storage_path"},
				{"source_path": "$.proizvodi[*].uslugeStampe[*].idUsluge", "target": "PrintService.code"},
				{"source_path": "$.proizvodi[*].uslugeStampe[*].tipStampe", "target": "PrintService.name"},
				{"source_path": "$.proizvodi[*].uslugeStampe[*].dodatnaCenaPoKomadu", "target": "ProductPrintService.additional_price"},
				{"source_path": "$.proizvodi[*].uslugeStampe[*].maxSirinaMm", "target": "ProductPrintService.max_width_mm"},
				{"source_path": "$.proizvodi[*].uslugeStampe[*].maxVisinaMm", "target": "ProductPrintService.max_height_mm"},
			},
			"evidence": evidence(atoms, atomRefs, defs),
		},
	}
}

func buildStateMachines(atomRefs map[string][]string, defs map[string]AtomDef) []map[string]any {
	sm := func(id, owner, field string, states []string, transitions []map[string]string, atoms []string) map[string]any {
		initial := ""
		if len(states) > 0 {
			initial = states[0]
		}
		return map[string]any{
			"id":          id,
			"owner":       owner,
			"field":       field,
			"states":      states,
			"initial":     initial,
			"terminal":    inferTerminalStatesFromStringTransitions(states, transitions),
			"transitions": transitions,
			"evidence":    evidence(atoms, atomRefs, defs),
		}
	}
	return []map[string]any{
		sm("UserAccountStatusFlow", "UserAccount", "account_status", []string{"pending", "active", "rejected", "disabled"}, []map[string]string{{"from": "pending", "to": "active"}, {"from": "pending", "to": "rejected"}, {"from": "active", "to": "disabled"}}, []string{"PHF-RA-004", "PHF-RA-029"}),
		sm("RegistrationRequestStatusFlow", "RegistrationRequest", "status", []string{"pending", "accepted", "rejected"}, []map[string]string{{"from": "pending", "to": "accepted"}, {"from": "pending", "to": "rejected"}}, []string{"PHF-RA-004", "PHF-RA-029"}),
		sm("CartStatusFlow", "Cart", "status", []string{"active", "confirmed", "cancelled"}, []map[string]string{{"from": "active", "to": "confirmed"}, {"from": "active", "to": "cancelled"}}, []string{"PHF-RA-014", "PHF-RA-015", "PHF-RA-018"}),
		sm("InvoiceStatusFlow", "Invoice", "status", []string{"ordered", "paid", "in_print", "delivered", "received", "cancelled"}, []map[string]string{{"from": "ordered", "to": "paid"}, {"from": "ordered", "to": "in_print"}, {"from": "paid", "to": "in_print"}, {"from": "in_print", "to": "delivered"}, {"from": "delivered", "to": "received"}, {"from": "ordered", "to": "cancelled"}}, []string{"PHF-RA-015", "PHF-RA-017", "PHF-RA-020", "PHF-RA-026"}),
		sm("PaymentAttemptStatusFlow", "PaymentAttempt", "status", []string{"started", "succeeded", "failed"}, []map[string]string{{"from": "started", "to": "succeeded"}, {"from": "started", "to": "failed"}}, []string{"PHF-RA-017"}),
		sm("ProcurementStatusFlow", "Procurement", "status", []string{"open", "closed", "awarded", "cancelled"}, []map[string]string{{"from": "open", "to": "closed"}, {"from": "closed", "to": "awarded"}, {"from": "open", "to": "cancelled"}}, []string{"PHF-RA-018", "PHF-RA-019"}),
		sm("ProcurementBidStatusFlow", "ProcurementBid", "status", []string{"submitted", "winning", "not_selected", "invalid"}, []map[string]string{{"from": "submitted", "to": "winning"}, {"from": "submitted", "to": "not_selected"}, {"from": "submitted", "to": "invalid"}}, []string{"PHF-RA-019", "PHF-RA-027"}),
	}
}

func inferTerminalStatesFromStringTransitions(states []string, transitions []map[string]string) []string {
	if len(states) == 0 {
		return nil
	}
	outgoing := map[string]bool{}
	for _, transition := range transitions {
		if from := transition["from"]; from != "" {
			outgoing[from] = true
		}
	}
	var terminals []string
	for _, state := range states {
		if !outgoing[state] {
			terminals = append(terminals, state)
		}
	}
	if len(terminals) == 0 {
		terminals = append(terminals, states[len(states)-1])
	}
	sort.Strings(terminals)
	return terminals
}

func buildDerivedViews(atomRefs map[string][]string, defs map[string]AtomDef) []map[string]any {
	view := func(id, kind string, sources, metrics, atoms []string) map[string]any {
		return map[string]any{"id": id, "label": id, "description": id, "kind": kind, "sources": sources, "persistence": "virtual", "metrics": metrics, "evidence": evidence(atoms, atomRefs, defs)}
	}
	return []map[string]any{
		view("PublicHomeSummary", "aggregate", []string{"PrintShop", "Product", "ProductFeedback"}, []string{"registered_print_shop_count", "top_5_products_by_likes"}, []string{"PHF-RA-007"}),
		view("PublicProductSearch", "projection", []string{"Product", "PrintShop", "Category", "Subcategory"}, nil, []string{"PHF-RA-008"}),
		view("PublicProductDetail", "projection", []string{"Product", "PrintShop", "ProductImage", "ProductFeedback"}, nil, []string{"PHF-RA-008", "PHF-RA-009"}),
		view("ProductDetailFeedback", "projection", []string{"Product", "ProductFeedback", "UserAccount"}, []string{"likes_count", "dislikes_count", "last_5_comments"}, []string{"PHF-RA-021"}),
		view("ClientOrderHistory", "projection", []string{"Invoice", "InvoiceItem", "PrintShop", "Product"}, nil, []string{"PHF-RA-010", "PHF-RA-015"}),
		view("CurrentCartGroupedByPrintShop", "projection", []string{"Cart", "CartItem", "Product", "PrintShop"}, nil, []string{"PHF-RA-014"}),
		view("ClientProductArchive", "projection", []string{"Invoice", "InvoiceItem", "Product", "ProductFeedback"}, nil, []string{"PHF-RA-020", "PHF-RA-021"}),
		view("ProcurementBidReport", "report", []string{"Procurement", "ProcurementBid", "ProcurementBidItem", "PrintShop"}, []string{"all_bid_totals", "winning_lowest_total"}, []string{"PHF-RA-019", "PHF-RA-028"}),
		view("AdminTrafficByPrintShop", "aggregate", []string{"Invoice", "PrintShop"}, []string{"sum_invoice_total_last_quarter"}, []string{"PHF-RA-031"}),
		view("AdminPopularProducts", "aggregate", []string{"InvoiceItem", "Product"}, []string{"sum_quantity_last_month", "percentage_share"}, []string{"PHF-RA-031"}),
		view("ProductRatingTrend", "aggregate", []string{"ProductFeedback", "Product"}, []string{"likes_over_time", "dislikes_over_time"}, []string{"PHF-RA-031"}),
	}
}

func buildFileSpecs(atomRefs map[string][]string, defs map[string]AtomDef) []map[string]any {
	file := func(id, owner, field string, exts, atoms []string) map[string]any {
		return map[string]any{"id": id, "owner": owner, "field": field, "allowed_extensions": exts, "storage": "path", "evidence": evidence(atoms, atomRefs, defs)}
	}
	return []map[string]any{
		file("UserProfileImageFile", "UserAccount", "profile_image_path", []string{"jpg", "jpeg", "png", "gif"}, []string{"PHF-RA-005"}),
		file("ProductImageFile", "ProductImage", "storage_path", []string{"jpg", "jpeg", "png", "gif"}, []string{"PHF-RA-009", "PHF-RA-025"}),
		file("CartItemCustomImageFile", "CartItem", "custom_image_path", []string{"jpg", "jpeg", "png", "gif"}, []string{"PHF-RA-012"}),
		file("InvoicePdfFile", "Invoice", "pdf_path", []string{"pdf"}, []string{"PHF-RA-016"}),
		file("ProcurementReportPdfFile", "Procurement", "report_pdf_path", []string{"pdf"}, []string{"PHF-RA-019", "PHF-RA-028"}),
	}
}

func entity(id, table, kind string, atoms []string, atomRefs map[string][]string, defs map[string]AtomDef, attrs []map[string]any) map[string]any {
	return map[string]any{
		"id":          id,
		"label":       id,
		"description": id,
		"table_name":  table,
		"kind":        kind,
		"evidence":    evidence(atoms, atomRefs, defs),
		"attributes":  attrs,
	}
}

func attr(id, typ string, required bool, atoms []string, atomRefs map[string][]string, defs map[string]AtomDef) map[string]any {
	return map[string]any{"id": id, "label": title(id), "description": id, "type": typ, "required": required, "evidence": evidence(atoms, atomRefs, defs)}
}

func attrEnum(id string, values []string, required bool, atoms []string, atomRefs map[string][]string, defs map[string]AtomDef) map[string]any {
	out := attr(id, "string", required, atoms, atomRefs, defs)
	out["enum_values"] = values
	return out
}

func constraint(id, typ, owner string, fields []string, extra map[string]any, atoms []string, atomRefs map[string][]string, defs map[string]AtomDef) map[string]any {
	out := map[string]any{
		"id":          id,
		"type":        typ,
		"owner":       owner,
		"description": strings.ReplaceAll(id, "_", " "),
		"evidence":    evidence(atoms, atomRefs, defs),
	}
	if len(fields) == 1 {
		out["field"] = fields[0]
	} else if len(fields) > 1 {
		out["fields"] = fields
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func evidence(atoms []string, atomRefs map[string][]string, defs map[string]AtomDef) map[string]any {
	reviews := map[string]bool{}
	for _, atomID := range atoms {
		for _, reviewID := range defs[atomID].ReviewDecisions {
			reviews[reviewID] = true
		}
	}
	return map[string]any{
		"source_units":      unionAtomRefs(atoms, atomRefs),
		"requirement_atoms": atoms,
		"review_decisions":  sortedKeys(reviews),
		"support_level":     "explicit",
		"confidence":        "high",
	}
}

func buildCrudRows(operations []Operation) []CrudRow {
	opIDs := make([]string, 0, len(operations))
	for _, op := range operations {
		opIDs = append(opIDs, op.ID)
	}
	row := func(entity, table, rationale string, ops map[string][]string) CrudRow {
		full := map[string][]string{}
		for _, opID := range opIDs {
			full[opID] = []string{}
		}
		for opID, actions := range ops {
			full[opID] = actions
		}
		return CrudRow{Entity: entity, Table: table, Operations: full, Rationale: rationale}
	}
	return []CrudRow{
		row("UserAccount", "user_accounts", "Nalog je centralan za autentifikaciju i profile.", map[string][]string{"register_user": {"C", "R", "U"}, "authenticate_user": {"R"}, "reset_password": {"R", "U"}, "approve_registration": {"R", "U"}, "manage_user_accounts": {"R", "U", "D"}, "view_update_profile": {"R", "U"}, "confirm_received_and_feedback": {"R"}}),
		row("Institution", "institutions", "Institucionalni profil prati pravna lica i stamparije.", map[string][]string{"register_user": {"C", "R", "U"}, "approve_registration": {"R", "U"}, "create_procurement_from_cart": {"R"}}),
		row("PrintShop", "print_shops", "Stamparija je dobavljac proizvoda i nosilac fakturisanja/ponuda.", map[string][]string{"public_home_summary": {"R"}, "public_search_catalog": {"R"}, "client_search_products": {"R"}, "manage_products_services": {"R", "U"}, "import_catalog_json": {"R", "U"}, "confirm_individual_cart": {"R"}, "submit_procurement_bid": {"R"}, "view_admin_statistics": {"R"}}),
		row("PasswordResetToken", "password_reset_tokens", "Reset token je potreban za privremeni link.", map[string][]string{"request_password_reset": {"C", "R"}, "reset_password": {"R", "U"}}),
		row("RegistrationRequest", "registration_requests", "Zahtev prati odobravanje korisnika.", map[string][]string{"register_user": {"C", "R"}, "approve_registration": {"R", "U"}}),
		row("Category", "categories", "Kategorije se citaju u pretrazi i odrzavaju administrativno.", map[string][]string{"public_search_catalog": {"R"}, "client_search_products": {"R"}, "manage_taxonomy": {"C", "R", "U", "D"}, "manage_products_services": {"R"}, "import_catalog_json": {"R"}}),
		row("Subcategory", "subcategories", "Potkategorije klasifikuju proizvode.", map[string][]string{"public_search_catalog": {"R"}, "client_search_products": {"R"}, "manage_taxonomy": {"C", "R", "U", "D"}, "manage_products_services": {"R"}, "import_catalog_json": {"R"}}),
		row("Product", "products", "Proizvod je centralni kataloski entitet.", map[string][]string{"public_home_summary": {"R"}, "public_search_catalog": {"R"}, "client_search_products": {"R"}, "prepare_cart_item": {"R"}, "manage_products_services": {"C", "R", "U", "D"}, "update_stock": {"R", "U"}, "import_catalog_json": {"C", "R", "U"}, "confirm_individual_cart": {"R", "U"}, "create_procurement_from_cart": {"R"}, "confirm_received_and_feedback": {"R"}, "view_admin_statistics": {"R"}}),
		row("ProductImage", "product_images", "Slike podrzavaju galeriju i upload.", map[string][]string{"public_search_catalog": {"R"}, "client_search_products": {"R"}, "manage_products_services": {"C", "R", "U", "D"}, "import_catalog_json": {"C", "R", "U"}}),
		row("Color", "colors", "Boje su lookup za izbor proizvoda.", map[string][]string{"client_search_products": {"R"}, "prepare_cart_item": {"R"}, "manage_products_services": {"C", "R"}, "import_catalog_json": {"C", "R"}}),
		row("ProductColor", "product_colors", "Spojna tabela proizvoda i boja.", map[string][]string{"client_search_products": {"R"}, "prepare_cart_item": {"R"}, "manage_products_services": {"C", "R", "D"}, "import_catalog_json": {"C", "R", "D"}}),
		row("PrintService", "print_services", "Usluge stampe su lookup za ponude po proizvodu.", map[string][]string{"client_search_products": {"R"}, "prepare_cart_item": {"R"}, "manage_products_services": {"C", "R", "U"}, "import_catalog_json": {"C", "R", "U"}}),
		row("ProductPrintService", "product_print_services", "Spojna tabela sa cenom i dimenzijama usluge po proizvodu.", map[string][]string{"client_search_products": {"R"}, "prepare_cart_item": {"R"}, "manage_products_services": {"C", "R", "U", "D"}, "import_catalog_json": {"C", "R", "U", "D"}}),
		row("Cart", "carts", "Korpa skuplja trenutnu narudzbinu.", map[string][]string{"prepare_cart_item": {"C", "R", "U"}, "confirm_individual_cart": {"R", "U"}, "create_procurement_from_cart": {"R", "U"}}),
		row("CartItem", "cart_items", "Stavke korpe nose izbor proizvoda, usluge i custom sadrzaj.", map[string][]string{"prepare_cart_item": {"C", "R", "U", "D"}, "confirm_individual_cart": {"R"}, "create_procurement_from_cart": {"R"}}),
		row("Invoice", "invoices", "Faktura je per-stamparija narudzbina za fizicka lica.", map[string][]string{"view_update_profile": {"R"}, "confirm_individual_cart": {"C", "R"}, "pay_invoice": {"R", "U"}, "process_individual_orders": {"R", "U"}, "confirm_received_and_feedback": {"R", "U"}, "generate_invoice_pdf": {"R", "U"}, "record_payment_callback": {"R", "U"}, "view_admin_statistics": {"R"}}),
		row("InvoiceItem", "invoice_items", "Stavke fakture cuvaju proizvode i cene.", map[string][]string{"view_update_profile": {"R"}, "confirm_individual_cart": {"C", "R"}, "confirm_received_and_feedback": {"R"}, "view_admin_statistics": {"R"}}),
		row("PaymentAttempt", "payment_attempts", "Pokusaji placanja cuvaju status eksternog servisa.", map[string][]string{"pay_invoice": {"C", "R", "U"}, "record_payment_callback": {"R", "U"}}),
		row("Procurement", "procurements", "Javna nabavka je proces pravnog lica.", map[string][]string{"create_procurement_from_cart": {"C", "R"}, "submit_procurement_bid": {"R"}, "close_procurement": {"R", "U"}, "view_procurement_report": {"R"}}),
		row("ProcurementItem", "procurement_items", "Stavke javne nabavke opisuju trazene proizvode i kolicine.", map[string][]string{"create_procurement_from_cart": {"C", "R"}, "submit_procurement_bid": {"R"}, "close_procurement": {"R"}}),
		row("ProcurementBid", "procurement_bids", "Ponuda stamparije ucestvuje u izboru najnize cene.", map[string][]string{"submit_procurement_bid": {"C", "R"}, "close_procurement": {"R", "U"}, "view_procurement_report": {"R"}}),
		row("ProcurementBidItem", "procurement_bid_items", "Stavke ponude omogucavaju proveru pokrivenosti trazenih kolicina.", map[string][]string{"submit_procurement_bid": {"C", "R"}, "close_procurement": {"R"}, "view_procurement_report": {"R"}}),
		row("ProductFeedback", "product_feedback", "Feedback cuva ocene i komentare po primljenom proizvodu.", map[string][]string{"public_home_summary": {"R"}, "public_search_catalog": {"R"}, "confirm_received_and_feedback": {"C", "R", "U"}, "view_admin_statistics": {"R"}}),
	}
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(wd, "AGENTS.md")) && exists(filepath.Join(wd, "dbdsl", "go.mod")) {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return "", fmt.Errorf("repo root not found")
		}
		wd = parent
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func buildSourceUnits(raw, sourceFile string) []SourceUnit {
	body, appendix := splitAppendix(raw)
	blocks := buildLogicalBlocks(body)
	units := make([]SourceUnit, 0, 220)
	nextID := 1
	for _, block := range blocks {
		if block.Kind == "heading" {
			units = append(units, newSourceUnit(nextID, "heading", block.Section, block.StartLine, sourceFile, block.Text, "model_supporting"))
			nextID++
			continue
		}
		for _, part := range splitSentences(block.Text) {
			kind := block.Kind
			if kind == "bullet" {
				kind = "bullet_sentence"
			}
			if kind == "list_item" {
				kind = "list_item_field"
			}
			units = append(units, newSourceUnit(nextID, kind, block.Section, block.StartLine, sourceFile, part, classifyRelevance(part, block.Section, kind)))
			nextID++
		}
	}
	for _, unit := range buildJSONUnits(appendix, sourceFile, nextID) {
		units = append(units, unit)
		nextID++
	}
	return units
}

func splitAppendix(raw string) (string, string) {
	idx := strings.Index(raw, "Прилог 1: JSON фајл за уношење производа")
	if idx < 0 {
		return raw, ""
	}
	return raw[:idx], raw[idx:]
}

func buildLogicalBlocks(body string) []LogicalBlock {
	body = strings.ReplaceAll(body, "\f", "\n")
	lines := strings.Split(body, "\n")
	var blocks []LogicalBlock
	current := LogicalBlock{}
	currentSection := "document"

	flush := func() {
		if strings.TrimSpace(current.Text) == "" {
			current = LogicalBlock{}
			return
		}
		current.Text = normalizeSpaces(current.Text)
		current.Section = currentSection
		blocks = append(blocks, current)
		current = LogicalBlock{}
	}

	for idx, line := range lines {
		lineNo := idx + 1
		trimmed := strings.TrimSpace(line)
		if skipLine(trimmed) {
			if trimmed == "" {
				flush()
			}
			continue
		}
		if isHeading(trimmed) {
			flush()
			currentSection = sectionID(trimmed)
			blocks = append(blocks, LogicalBlock{Kind: "heading", Section: currentSection, StartLine: lineNo, Text: trimmed})
			continue
		}
		if isBulletStart(trimmed) || isDashListItem(trimmed) || isNumberedOption(trimmed) {
			flush()
			kind := "bullet"
			if isDashListItem(trimmed) {
				kind = "list_item"
			}
			current = LogicalBlock{Kind: kind, Section: currentSection, StartLine: lineNo, Text: trimmed}
			if kind == "list_item" {
				flush()
			}
			continue
		}
		if current.Kind == "" {
			current = LogicalBlock{Kind: "sentence", Section: currentSection, StartLine: lineNo, Text: trimmed}
			continue
		}
		current.Text += " " + trimmed
	}
	flush()
	return blocks
}

func skipLine(trimmed string) bool {
	if trimmed == "" {
		return true
	}
	if trimmed == "Универзитет у Београду - Електротехнички факултет" || trimmed == "Катедра за рачунарску технику и информатику" {
		return true
	}
	return regexp.MustCompile(`^\d+$`).MatchString(trimmed)
}

func isHeading(trimmed string) bool {
	switch trimmed {
	case "Пројекат из предмета Програмирање интернет апликација", "за школску 2025/26. годину", "Аутентификација и регистрација", "Нерегистровани корисник – почетна веб страна", "Клијенти", "Штампари", "Администратор система", "Остале карактеристике апликације", "Напомене:", "За израду пројектног задатка потребно је користити:":
		return true
	}
	return false
}

func sectionID(heading string) string {
	switch heading {
	case "Пројекат из предмета Програмирање интернет апликација", "за школску 2025/26. годину":
		return "document_title"
	case "Аутентификација и регистрација":
		return "authentication_and_registration"
	case "Нерегистровани корисник – почетна веб страна":
		return "public_home_and_search"
	case "Клијенти":
		return "clients"
	case "Штампари":
		return "printers"
	case "Администратор система":
		return "administrator"
	case "Остале карактеристике апликације":
		return "other_application_characteristics"
	case "Напомене:":
		return "exam_notes"
	case "За израду пројектног задатка потребно је користити:":
		return "technology_requirements"
	default:
		return "document"
	}
}

func isBulletStart(trimmed string) bool {
	return strings.HasPrefix(trimmed, "▪") || strings.HasPrefix(trimmed, "o ")
}

func isDashListItem(trimmed string) bool {
	return strings.HasPrefix(trimmed, "- ")
}

func isNumberedOption(trimmed string) bool {
	return regexp.MustCompile(`^[0-9]\) `).MatchString(trimmed)
}

func splitSentences(text string) []string {
	text = normalizeSpaces(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "- ") && !strings.ContainsAny(text, ".!?") {
		return []string{text}
	}
	replacements := map[string]string{
		"нпр.": "нпр<DOT>", "тзв.": "тзв<DOT>", "скр.": "скр<DOT>", "итд.": "итд<DOT>", "сл.": "сл<DOT>", "др.": "др<DOT>", "3.5.x": "3<DOT>5<DOT>x", "4.x": "4<DOT>x",
	}
	protected := text
	for from, to := range replacements {
		protected = strings.ReplaceAll(protected, from, to)
	}
	runes := []rune(protected)
	var parts []string
	start := 0
	for i, r := range runes {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i > 0 && i+1 < len(runes) && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1]) {
			continue
		}
		end := i + 1
		for end < len(runes) && (runes[end] == '”' || runes[end] == '"' || runes[end] == ')' || runes[end] == ']') {
			end++
		}
		if end < len(runes) && !unicode.IsSpace(runes[end]) {
			continue
		}
		part := restoreDots(strings.TrimSpace(string(runes[start:end])))
		if part != "" {
			parts = append(parts, part)
		}
		start = end
	}
	if start < len(runes) {
		part := restoreDots(strings.TrimSpace(string(runes[start:])))
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return []string{restoreDots(protected)}
	}
	return parts
}

func restoreDots(text string) string {
	return strings.ReplaceAll(text, "<DOT>", ".")
}

func newSourceUnit(n int, kind, section string, line int, sourceFile, text, relevance string) SourceUnit {
	unit := SourceUnit{
		ID:        fmt.Sprintf("PHF-GSU-%03d", n),
		Kind:      kind,
		Section:   section,
		Location:  fmt.Sprintf("%s#line-%d", sourceFile, line),
		Relevance: relevance,
		Text:      SourceText{Exact: text, Normalized: normalizeSpaces(text)},
	}
	unit.Tags = deriveTags(text, section, kind)
	return unit
}

func classifyRelevance(text, section, kind string) string {
	lower := strings.ToLower(text)
	if kind == "heading" {
		return "model_supporting"
	}
	if section == "exam_notes" || section == "technology_requirements" || section == "other_application_characteristics" {
		if strings.Contains(text, "серверске валидације") || strings.Contains(text, "база података иницијално") || strings.Contains(text, "довољном количином података") {
			return "model_supporting"
		}
		return "non_model"
	}
	if strings.Contains(lower, "canvas") || strings.Contains(lower, "css преклапања") || strings.Contains(lower, "доступан линк") {
		return "non_model"
	}
	return "model_relevant"
}

func deriveTags(text, section, kind string) []string {
	lower := strings.ToLower(text)
	tags := map[string]bool{section: true, kind: true}
	keywords := map[string][]string{
		"user_account":  {"корисник", "клијенти", "штампари", "администратор"},
		"credentials":   {"креденцијала", "корисничко име", "лозинка", "и-мејл"},
		"registration":  {"регистрациј", "захтев"},
		"institution":   {"институције", "матични број", "пиб", "правна лица", "установа"},
		"profile_image": {"профилна слика", "default_profile_image", "fileupload"},
		"catalog":       {"производ", "категори", "поткатегори", "услуге", "штампе"},
		"stock":         {"стању", "лагеру", "количин"},
		"cart":          {"корп", "е-корп"},
		"invoice":       {"фактур", "наруџбин"},
		"payment":       {"плаћањ", "картиц", "stripe", "paypal"},
		"procurement":   {"јавне набав", "лицитац", "понуд"},
		"feedback":      {"свиђања", "несвиђања", "коментар", "лајк", "дислајк"},
		"json_import":   {"json", "stampaorijaid", "uslugestampe"},
		"non_model":     {"css", "responsive", "прегледача", "одбране", "алати вештачке интелигенције", "angular", "spring boot", "express", "nodejs", "mongodb"},
	}
	for tag, needles := range keywords {
		for _, needle := range needles {
			if strings.Contains(lower, strings.ToLower(needle)) {
				tags[tag] = true
				break
			}
		}
	}
	return sortedKeys(tags)
}

func buildJSONUnits(appendix, sourceFile string, startID int) []SourceUnit {
	if strings.TrimSpace(appendix) == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(appendix, "\f", "\n"), "\n")
	keyRe := regexp.MustCompile(`"([^"]+)"\s*:`)
	var units []SourceUnit
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if skipLine(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "Прилог 1:") {
			unit := newSourceUnit(startID+len(units), "heading", "json_appendix", i+1, sourceFile, trimmed, "model_supporting")
			unit.Tags = []string{"heading", "json_appendix", "json_import"}
			units = append(units, unit)
			continue
		}
		match := keyRe.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		text := trimmed
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next == "" || keyRe.MatchString(next) || strings.HasPrefix(next, "}") || strings.HasPrefix(next, "]") || strings.HasPrefix(next, "{") {
				break
			}
			text += " " + next
			i++
		}
		unit := newSourceUnit(startID+len(units), "json_field", "json_appendix", i+1, sourceFile, normalizeSpaces(text), "model_relevant")
		unit.Tags = append(unit.Tags, "json_key:"+match[1])
		sort.Strings(unit.Tags)
		units = append(units, unit)
	}
	return units
}

func mapAtomSourceUnits(units []SourceUnit, defs []AtomDef) map[string][]string {
	result := make(map[string][]string, len(defs))
	for _, def := range defs {
		seen := map[string]bool{}
		var refs []string
		add := func(id string) {
			if !seen[id] {
				seen[id] = true
				refs = append(refs, id)
			}
		}
		for _, group := range def.MatchGroups {
			for _, unit := range units {
				text := strings.ToLower(unit.Text.Normalized)
				matches := true
				for _, needle := range group {
					if !strings.Contains(text, strings.ToLower(needle)) {
						matches = false
						break
					}
				}
				if matches {
					add(unit.ID)
				}
			}
		}
		for _, tag := range def.MatchTagsAny {
			for _, unit := range units {
				if contains(unit.Tags, tag) {
					add(unit.ID)
				}
			}
		}
		sortSourceIDs(refs)
		result[def.ID] = refs
	}
	return result
}

func indexAtomDefs(defs []AtomDef) map[string]AtomDef {
	out := make(map[string]AtomDef, len(defs))
	for _, def := range defs {
		out[def.ID] = def
	}
	return out
}

func unionAtomRefs(atomIDs []string, atomRefs map[string][]string) []string {
	seen := map[string]bool{}
	var result []string
	for _, atomID := range atomIDs {
		for _, sourceID := range atomRefs[atomID] {
			if !seen[sourceID] {
				seen[sourceID] = true
				result = append(result, sourceID)
			}
		}
	}
	sortSourceIDs(result)
	return result
}

func sortSourceIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeYAML(path string, value any) error {
	bytes, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0o644)
}

func writeTaskFull(path, raw string) error {
	content := "# Printing House - ceo tekst zadatka za v0.5_granularity_sentance\n\n```text\n" + strings.TrimSpace(raw) + "\n```\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func writeNotes(path string, unitCount, atomCount, actorCount, operationCount, entityCount int) error {
	lines := []string{
		"# Printing House Full v0.5 Granularity Sentance Notes",
		"",
		"Ova verzija je regenerisana sveze iz teksta zadatka. Generator ne cita stare PoC YAML rezultate.",
		"",
		"Granulacija `source_units.yaml` je: recenica, recenica unutar bullet stavke, kratka list-item stavka ili JSON polje iz priloga.",
		"",
		"CRUD matrica eksplicitno ima `actors` segment i `actor` u svakoj operaciji.",
		"",
		"Folder zadrzava korisnikov naziv `v0.5_granularity_sentance` radi stabilne putanje, iako je `sentence` standardni engleski oblik.",
		"",
		fmt.Sprintf("Ukupno source jedinica: %d.", unitCount),
		fmt.Sprintf("Requirement atom mapiranja: %d.", atomCount),
		fmt.Sprintf("Akteri: %d.", actorCount),
		fmt.Sprintf("Operacije: %d.", operationCount),
		fmt.Sprintf("Entiteti: %d.", entityCount),
		"",
		"Pipeline: TASK_FULL.md -> source_units.yaml -> requirement_atoms.yaml -> functional_decomposition.yaml -> crud_matrix.yaml -> review_decisions.yaml -> db_model.dsl.yaml.",
		"",
		"Downstream kompatibilnost: generator dodatno pravi `compat_v0.2/db_model.dsl.yaml` i `compat_v0.2/reviewed_source_fragments.yaml`, kako bi postojeci v0.2 validate/lint/generate alati mogli da naprave DBML i trace report bez gubitka v0.5 source-unit semantike.",
		"",
		"Ovo nije git grana i ne koristi git stanje. Artefakti su lokalni fajlovi u PoC folderu.",
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

type ValidationSummary struct {
	YAMLFilesValidated    int
	SourceRefsChecked     int
	EvidenceBlocksChecked int
}

func validateOutput(outDir string, units []SourceUnit, atomDefs []AtomDef, actors []Actor, operations []Operation, entities, relationships, constraints, importSpecs, stateMachines, derivedViews, fileSpecs []map[string]any) (ValidationSummary, error) {
	sourceIDs := map[string]bool{}
	for _, unit := range units {
		sourceIDs[unit.ID] = true
	}
	actorIDs := map[string]bool{}
	for _, actor := range actors {
		actorIDs[actor.ID] = true
	}
	if len(actorIDs) == 0 {
		return ValidationSummary{}, fmt.Errorf("crud actors segment is empty")
	}
	atomIDs := map[string]bool{}
	for _, def := range atomDefs {
		atomIDs[def.ID] = true
		refs := mapAtomSourceUnits(units, []AtomDef{def})[def.ID]
		if len(refs) == 0 {
			return ValidationSummary{}, fmt.Errorf("%s has no mapped source units", def.ID)
		}
	}
	for _, op := range operations {
		if op.Actor == "" {
			return ValidationSummary{}, fmt.Errorf("%s has no actor", op.ID)
		}
		if !actorIDs[op.Actor] {
			return ValidationSummary{}, fmt.Errorf("%s uses unknown actor %s", op.ID, op.Actor)
		}
		if len(op.SourceAtoms) == 0 {
			return ValidationSummary{}, fmt.Errorf("%s has no source atoms", op.ID)
		}
	}
	files := []string{"source_units.yaml", "requirement_atoms.yaml", "functional_decomposition.yaml", "crud_matrix.yaml", "review_decisions.yaml", "db_model.dsl.yaml"}
	summary := ValidationSummary{}
	for _, name := range files {
		bytes, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			return ValidationSummary{}, err
		}
		var doc any
		if err := yaml.Unmarshal(bytes, &doc); err != nil {
			return ValidationSummary{}, fmt.Errorf("%s: %w", name, err)
		}
		summary.YAMLFilesValidated++
		var refs []string
		collectSourceRefs(doc, &refs)
		for _, ref := range refs {
			if strings.HasPrefix(ref, "PHF-SU-") {
				return ValidationSummary{}, fmt.Errorf("%s still contains old block source ref %s", name, ref)
			}
			if strings.HasPrefix(ref, "PHF-GSU-") {
				if !sourceIDs[ref] {
					return ValidationSummary{}, fmt.Errorf("%s references unknown granular source unit %s", name, ref)
				}
				summary.SourceRefsChecked++
			}
		}
	}
	allModelElements := append([]map[string]any{}, entities...)
	allModelElements = append(allModelElements, relationships...)
	allModelElements = append(allModelElements, constraints...)
	allModelElements = append(allModelElements, importSpecs...)
	allModelElements = append(allModelElements, stateMachines...)
	allModelElements = append(allModelElements, derivedViews...)
	allModelElements = append(allModelElements, fileSpecs...)
	for _, elem := range allModelElements {
		count, err := validateEvidenceRecursive(elem, sourceIDs, atomIDs)
		if err != nil {
			return ValidationSummary{}, err
		}
		summary.EvidenceBlocksChecked += count
	}
	return summary, nil
}

func validateEvidenceRecursive(value any, sourceIDs, atomIDs map[string]bool) (int, error) {
	count := 0
	switch typed := value.(type) {
	case map[string]any:
		if rawEvidence, ok := typed["evidence"]; ok {
			ev, ok := rawEvidence.(map[string]any)
			if !ok {
				return count, fmt.Errorf("invalid evidence shape")
			}
			sourceRefs, ok := ev["source_units"].([]string)
			if !ok || len(sourceRefs) == 0 {
				return count, fmt.Errorf("evidence missing source_units")
			}
			requirementAtoms, ok := ev["requirement_atoms"].([]string)
			if !ok || len(requirementAtoms) == 0 {
				return count, fmt.Errorf("evidence missing requirement_atoms")
			}
			for _, ref := range sourceRefs {
				if !sourceIDs[ref] {
					return count, fmt.Errorf("evidence references unknown source unit %s", ref)
				}
			}
			for _, atomID := range requirementAtoms {
				if !atomIDs[atomID] {
					return count, fmt.Errorf("evidence references unknown atom %s", atomID)
				}
			}
			count++
		}
		for _, child := range typed {
			childCount, err := validateEvidenceRecursive(child, sourceIDs, atomIDs)
			if err != nil {
				return count, err
			}
			count += childCount
		}
	case []map[string]any:
		for _, child := range typed {
			childCount, err := validateEvidenceRecursive(child, sourceIDs, atomIDs)
			if err != nil {
				return count, err
			}
			count += childCount
		}
	case []any:
		for _, child := range typed {
			childCount, err := validateEvidenceRecursive(child, sourceIDs, atomIDs)
			if err != nil {
				return count, err
			}
			count += childCount
		}
	}
	return count, nil
}

func collectSourceRefs(value any, refs *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		if raw, ok := typed["source_units"]; ok {
			*refs = append(*refs, anyToStrings(raw)...)
		}
		for _, child := range typed {
			collectSourceRefs(child, refs)
		}
	case []any:
		for _, child := range typed {
			collectSourceRefs(child, refs)
		}
	}
}

func anyToStrings(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

func normalizeSpaces(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func title(id string) string {
	parts := strings.Split(id, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
