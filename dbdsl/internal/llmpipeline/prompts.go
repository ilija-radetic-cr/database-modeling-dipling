package llmpipeline

const promptTemplateVersion = "llm_protocol_v0_7_2026_09_14_r1"

const combinedDocumentInstructions = `You are preparing source material for a
database-modeling LLM pipeline.

Create a consolidated document from the supplied resources. Preserve facts, remove
duplicates, and keep normative task requirements separate from illustrative
examples where possible.

Rules:
- Return JSON only.
- Every sentence must cite one or more resource spans in derived_from.
- Do not add a normative statement unless it is supported by cited source text.
- Use stable ASCII IDs in the form OD-S-001, OD-S-002, ...
- Keep each sentence focused on one requirement or example observation.
- Use transformation copied for unchanged text, cleaned for light cleanup, merged
  for statements assembled from multiple source spans, and summarized only when
  the original source is verbose but the meaning is unchanged.
- Put uncertainty, duplicate/conflict notes, OCR concerns, and weak evidence in
  warnings.

The deterministic backend will validate resource IDs, line spans, and lineage
before accepting the combined document.`

const sourceUnitExtractionInstructions = `You are segmenting a validated combined
document for a traceable relational-database modeling pipeline.

Return JSON only. Split or merge combined-document sentences only when that creates
one focused semantic unit. Every unit must cite existing OD sentence IDs and its
exact_text must be supported by those sentences. normalized_text may clean wording
but must not introduce facts. Classify UI-only text, examples, headings and noise
explicitly instead of treating every noun as persistent data. Use stable IDs
SU-001, SU-002, ... and mark low confidence, conflicts, OCR ambiguity, or uncertain
relevance with requires_review and a warning.

The backend derives original resource spans from OD lineage and rejects invented
text, unknown IDs and uncovered combined-document sentences.`

const requirementAtomExtractionInstructions = `Extract atomic requirements from
validated source units for logical relational database design. Return JSON only.
Every atom must cite existing source-unit IDs. Separate persistent-data requirements
from UI-only behavior, application logic, external behavior and examples. An atom
with assumption support or low confidence must set requires_review and explain the
risk in warnings. Decompose each atom into subject, predicate and object, and record
quantifier, condition, temporal_semantics and ownership explicitly; use an empty
string only when the source truly does not state that dimension. Preserve exact
numbers, boundaries and timing rules. Do not propose tables, SQL, DBML, functional
areas or CRUD here.`

const functionalAnalysisInstructions = `Group validated requirement atoms into
coherent business capabilities and identify their actors. Return JSON only. Every
functional area must cite existing atom IDs and every main actor must exist in the
actor catalog. Functional areas are a coverage and review aid; do not turn them into
tables and do not generate CRUD, SQL, DBML or DB-DSL.`

const crudMappingInstructions = `Map validated requirement atoms and functional
areas to business operations and explicit create/read/update/delete effects over
persistent-data candidates. Return JSON only. Cite existing actors, areas, atoms and
source units. A mutating operation without C/U/D effects must carry a warning and
requires_review. Reports and derived values are not persistent data unless evidence
or a review decision says so. Do not generate tables, SQL, DBML or DB-DSL.`

const projectReviewInstructions = `Identify only genuine modeling ambiguities from
validated source units, requirement atoms, functional areas and CRUD operations.
Return JSON only. Questions must cite existing IDs, provide two or three materially
different options, explain effects and risks, and form an acyclic depends_on graph.
Use depends_on only for hard ordering and may_affect for informative impact. Mark a
question blocking only when the downstream conceptual/logical model would otherwise
encode an unsupported choice. Do not create cosmetic or generic questions.`

const reviewResolutionPatchInstructions = `Translate one explicit human review
decision into the smallest analysis patch that records or applies that decision.
Return JSON only. Modify only cited requirement atoms using the allowed operations.
Always link the decision to affected atoms. For every link_review_decision operation,
set field to review_decisions and copy the input decision_id exactly into value; do
not append the candidate or selected-option ID. Do not regenerate the project.
reserved_review_candidate_ids contains IDs that are already in use. If the selected
option reveals another independent ambiguity, return it as a new review candidate
with a unique RC-NNN ID not present in that reserved list, and prefix its option IDs
with the same new candidate ID. Every affected_source_units, affected_atoms,
affected_functional_areas, affected_operations and depends_on value must be copied
exactly from the corresponding valid_reference_ids list. The backend applies the
patch to a copy and validates references before committing a new revision.`

const conceptualModelInstructions = `Propose a conceptual relational-data model
from validated requirements, design obligations, functional analysis, CRUD operations and the supplied
resolved-review-decision summaries. Return JSON only. Model domain concepts, conceptual attributes,
relationships and cardinality hypotheses without SQL, target-DB types, indexes or
physical design. Every concept and attribute must cite existing source units and
requirement atoms; assumptions must cite a resolved review decision. Cite only the
smallest directly supporting evidence set for each element. Keep labels and
descriptions concise. Relationship endpoints must reference entity concept IDs,
except that an attachment/ownership relationship may connect exactly one entity
concept and one file concept. Never use lifecycle, derived or import concept IDs as
relationship endpoints. Do not hide an unresolved blocking ambiguity. Prefer
stable business concepts over UI components. Every required design obligation must
be represented explicitly: generated values that must be reproducible need a
snapshot concept, repeatable actions need event/history concepts, lifecycle rules
need lifecycle concepts, and derived outputs need a derived concept rather than an
untyped generic fact container.`

// Design obligations are supplied as an explicit bridge between requirement
// meaning and model realization. The prompt deliberately keeps them visible:
// application behavior may still require persisted events, snapshots or
// constraints even when it is not itself a table.

const logicalProjectionInstructions = `Project an accepted conceptual model and its
design obligations into a DB-DSL v0.5 add-only structured patch. Return JSON only,
never YAML, DBML or SQL.
Every element must preserve source-unit, requirement-atom and review-decision
evidence. Use regular, lookup or association entities, lower snake_case attributes,
and DB-DSL scalar types. Do not emit explicit surrogate id attributes for regular or
lookup entities because the deterministic DBML generator supplies them. Cite only
the smallest directly supporting evidence set. Keep descriptions and notes concise.
Project an entity-to-file conceptual relationship as a scalar file-reference
attribute on the owning entity plus a file_spec; do not emit the file concept as an
entity or as a database relationship endpoint.
Do not emit a separate required constraint when an attribute's required flag already
expresses the same rule. Import mapping targets must use Entity.attribute syntax and
reference an existing scalar attribute. Every file spec owner.field must reference
an existing entity attribute. Every represented requirement atom must be cited by
at least one model element. Every required design obligation must have an explicit
realization suitable for deterministic verification. Do not use fact_kind/fact_value
or another generic key/value container in place of named domain facts. Preserve
generated input snapshots and score-relevant event history when replayability or
auditability is required. An association entity cannot be an endpoint that needs
a scalar foreign key; use a regular entity when it has its own lifecycle or incoming
relationships. When repair_mode is true, address every supplied validation error
and return a complete corrected replacement patch, preserving valid operations.`

const requirementExtractionInstructions = `You are assisting logical relational database design for a diploma project.

Convert source units from a PIA-style textual specification into requirement atoms,
functional areas, actors, CRUD operations, review candidates, warnings, and a
confidence summary.

Rules:
- Return JSON only.
- Use only source unit IDs present in the input.
- Do not invent requirements not supported by source text.
- Classify UI-only behavior and application logic separately from persistent data.
- Mark uncertainty as review candidates or warnings.
- Do not generate SQL, DBML, YAML, or final database tables in this stage.
- Use stable ASCII IDs.

The next deterministic stage will verify references before writing DB-DSL v0.5
artifacts.`

const modelPlanInstructions = `You are assisting the conceptual and logical design
phase of a relational database.

Propose a model plan from requirement atoms, functional areas, actors, and CRUD
operations. This is not final DB-DSL and not SQL.

Rules:
- Return JSON only.
- Cite existing requirement atom IDs and source unit IDs.
- Prefer persistent entities with identity over turning every noun into a table.
- Identify association entities for many-to-many relationships with attributes.
- Treat reports as derived views unless the text explicitly asks for stored snapshots.
- Put unclear cardinality, state history, physical storage, and app-logic questions
  into review candidates or unresolved questions.

The output will be consumed by a later add-only DB-DSL patch prompt.`

const patchInstructions = `You are assisting a controlled DB-DSL v0.5 pipeline.

Convert accepted model plan candidates into add-only DB-DSL patch operations.

Rules:
- Return JSON only.
- Allowed operations are add-only: add_entity, add_relationship, add_constraint,
  add_state_machine, add_derived_view, add_file_spec, add_import_spec.
- Every element must cite source unit IDs and requirement atom IDs.
- Do not use support_level assumption unless a resolved review decision is cited.
- Do not delete, rename, or overwrite elements.
- Do not generate SQL or DBML.
- Use snake_case table names and lower snake_case attribute names.
- Use only DB-DSL v0.5 scalar types.
- For each patch operation, include null for every object field that does not apply
  to that operation.

The deterministic system will validate references, lint quality risks, and generate
DBML only after this patch is converted into a v0.5 bundle.`

const repairInstructions = `You are repairing one deterministic DB-DSL validation
or lint issue.

Rules:
- Return JSON only.
- Do not regenerate the whole model.
- Do not silently drop evidence.
- Do not invent source unit or requirement atom IDs.
- Set requires_human_review to true when the fix changes meaning, cardinality,
  lifecycle, persistence of derived data, or classification of a requirement.

The v0 CLI logs repair proposals but does not automatically apply them to a bundle.`

const baselineDBMLInstructions = `Generate DBML directly from the task text.
This is an evaluation baseline only. Do not include explanations outside DBML.`

const baselineSQLInstructions = `Generate SQL DDL directly from the task text.
This is an evaluation baseline only. Do not include explanations outside SQL.`
