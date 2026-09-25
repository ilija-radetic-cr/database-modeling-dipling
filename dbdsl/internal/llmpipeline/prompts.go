package llmpipeline

const PromptTemplateVersion = "llm_protocol_v0_11_1_2026_09_25"

const promptTemplateVersion = PromptTemplateVersion

const sourceSegmentationInstructions = `Zadatak je da dokument podeliš na segmente i svakom segmentu odrediš tip.
Dokument je specifikacija zadatka izvučena iz PDF-a ili tekstualne datoteke.
Rečenice su u njemu prelomljene u više redova, a strane sadrže zaglavlja,
podnožja, brojeve strana i fusnote. Dokument se nalazi između oznaka <document>.
## Tipovi segmenata
- heading: naslov dokumenta ili odeljka
- sentence: jedna rečenica tekućeg teksta
- list: rečenica koja uvodi listu, zajedno sa svim stavkama liste
- example: rečenica koja najavljuje primer, zajedno sa samim primerom (redovi sa
  oznakama, kao „Primer: … Rešenje: …“, ili JSON, XML, CSV ili tabela)
- footnote: fusnota, zajedno sa svojim brojem
- page_header: tekst koji se ponavlja na vrhu strana
- page_footer: tekst koji se ponavlja na dnu strana
- page_number: samostalan broj strane
- other: sve što ne odgovara nijednom od prethodnih tipova
## Pravila
1. Tekst prepiši tačno onako kako je napisan: ista slova u istom pismu, iste
   cifre, interpunkcija, navodnici i simboli, uključujući greške i oštećene
   strukturirane podatke.
2. Svaki prelom reda unutar segmenta zameni jednim razmakom.
3. Reč prelomljenu crticom na kraju reda spoji i izostavi tu crticu.
4. Kada zaglavlje, podnožje, broj strane ili fusnota prekida rečenicu, listu ili
   primer, prekinuti segment vrati ceo, a tekst koji ga prekida vrati kao
   zasebne segmente odmah posle njega.
5. Svaki deo dokumenta pripada tačno jednom segmentu. Segmente vrati poređane
   prema mestu na kome počinju u dokumentu.
## Izlaz
Vrati isključivo JSON:
{"segments": [{"type": "...", "text": "..."}]}`

// conceptualDescriptionInstructions is the second LLM interaction of the flow:
// numbered segments in, a rich denormalized description of what the system
// must remember out. Tables and keys are derived from it deterministically.
const conceptualDescriptionInstructions = `Zadatak je da iz specifikacije softverskog sistema izdvojiš potpun opis onoga
što sistem mora da pamti: aktere, stvari, njihova svojstva i veze, zapise o
događajima, stanja i pravila. Opis služi kao osnova za projektovanje baze
podataka.

## Ulaz
Specifikacija je data kao segmenti između oznaka <segments>, u redosledu
dokumenta. Svaki red počinje ID-jem segmenta i tipom u uglastim zagradama:
- heading: naslov; određuje odeljak kojem pripadaju segmenti ispod njega.
- sentence: rečenica.
- list: rečenica koja uvodi listu, zajedno sa svim stavkama liste. Uvodna
  rečenica važi za svaku stavku.
- footnote: fusnota sa svojim brojem; važi za mesto u tekstu koje nosi taj broj
  kao oznaku uz reč.
- example: primer sa rečenicom koja ga najavljuje. Primer pokazuje oblik
  podataka ili format uvoza; vrednosti u njemu nisu skup dozvoljenih vrednosti
  niti početni podaci, osim ako tekst to izričito kaže.
- other: ostalo.

## Kakav opis se traži
- Potpun: sve što tekst kaže ili nužno podrazumeva o podacima.
- Denormalizovan: svojstva i veze navedi kod stvari o kojoj tekst govori, onako
  kako ih tekst opisuje. Ne projektuj tabele, ključeve ni strane ključeve.
- Oslonjen na tekst: svaka tvrdnja navodi segmente iz kojih potiče i da li
  tekst to kaže direktno ili nužno podrazumeva. Ono
  što bi zahtevalo nagađanje ide u otvorena pitanja; ne dopunjavaj zdravim
  razumom.

## Postupak
1. Pročitaj ceo tekst pre opisivanja. Akteri, pravila i dopune često se pojave
   tek u kasnijim odeljcima, a isti pojam se na različitim mestima može zvati
   različito.
2. Izdvoj tekst koji se ne odnosi na sistem, nego na izradu i ocenjivanje
   zadatka: tehnologije, izgled stranica i navigaciju, poene, rokove, odbranu,
   podatke za demonstraciju, način kreiranja baze. On ne ulazi u opis, osim dela
   koji ipak kaže nešto o sistemu (npr. prikaz bojom koji razlikuje dve vrste
   zapisa: pamti se razlika, ne boja).
3. Opiši aktere i stvari (tačke 1–6 ispod).
4. Za svaku radnju koju tekst opisuje proveri šta posle nje mora ostati
   zapamćeno: učesnici, sadržaj, vreme, stanje i ishod.
5. Za svaki prikaz, pretragu, izveštaj i izračunavanje proveri da li su podaci
   koje zahteva opisani; ako nisu, dodaj ih kao nužno podrazumevane.
6. Proveri pokrivenost (odeljak Pokrivenost).

## Na šta obratiti pažnju
1. Akteri i identitet. Vrste korisnika, po čemu se razlikuju, kome pripadaju i
   koje podatke imaju; nalog, osoba i organizacija nisu isto, a spisak vrsta
   korisnika na početku teksta ne mora biti potpun. Kada tekst razlikuje opštu
   definiciju od konkretnog pojavljivanja ili primerka (npr. ponudu i pojedinačni
   vaučer iz nje), opiši oba nivoa i vezu među njima. Za svaku stvar navedi kome
   pripada (npr. preduzeću ili korisniku) kao vezu ka toj stvari. U identified_by
   navedi svojstva koja stvar jedinstveno određuju, a kada jedinstvenost važi samo
   u okviru nečega, i ID te stvari (npr. ["sifra", "preduzece"] za šifru
   jedinstvenu u okviru preduzeća); ako tekst to ne kaže, ostavi []. Svaka druga
   jedinstvena vrednost ili kombinacija je posebno pravilo vrste uniqueness, čiji
   applies_to navodi tačno tu kombinaciju (npr. ["korisnicki_nalog.email"]).
   Navedi i kako identifikator nastaje.
2. Zapisi o događajima. Zahtevi, prijave, rezervacije, odluke, uplate i slične
   interakcije su stvari za sebe, sa učesnicima, vremenom, sadržajem, stanjem i
   ishodom. Kada se kasnije prikazuje ili proverava ono što je bilo, zapis ostaje
   u istoriji. Vrednost koja se može promeniti, a mora ostati onakva kakva je
   bila u trenutku događaja (cena, porez, važeći parametar ili iz njih
   izračunat iznos), navedi kod zapisa tog događaja, sa izvorom iz kog je
   preuzeta. Veza ka akteru koji je radnju izvršio postoji samo kada tekst kaže
   ili nužno podrazumeva da se pamti ko je to uradio; to što akter sme da doda,
   izmeni ili odobri stvar samo po sebi nije veza.
3. Stanja i prelazi. Stanja, šta ili ko menja stanje, i sve posledice prelaza
   (dodela, promena količine, promena prava ili limita). Navedi kada zapis
   nastaje ako ne nastaje odmah, i koje korake akter pokreće ručno.
4. Pravila. Ko sme šta da vidi, menja ili odobri; uslovi i preduslovi radnji;
   prvenstvo; vidljivost zavisna od stanja; šta se ne sme menjati posle nastanka;
   da li se briše, arhivira ili nikad ne briše.
5. Vreme i količina. Rokovi, intervali važenja, periodična pravila i
   resetovanja, pravila nad uzastopnim događajima, najmanje i najveće vrednosti,
   kapaciteti, limiti koji se menjaju, i parametri koje neki akter podešava i
   koji važe za ceo sistem.
6. Struktura vrednosti. Vrsta vrednosti svakog svojstva (value_type; izbor
   da/ne je boolean), složene vrednosti sa delovima, višestruke vrednosti,
   opcione i uslovne vrednosti, svojstva samo jedne varijante, vrednosti iz
   zatvorenog skupa, hijerarhije klasifikacija, pravila i granice koje zavise od
   drugog podatka. Vrednost koja se ponavlja po nečemu drugom (po danu, po
   stavci) nije višestruko svojstvo, nego posebna stvar čiji identitet
   uključuje to po čemu se ponavlja.
7. Granice. Spoljni proces se ne opisuje, ali ishod koji sistem o njemu pamti se
   opisuje. Odluku koju tekst prepušta izvođaču ne pretvaraj u zahtev.
8. Nejasnoće. Kontradikcije, nedefinisana pravila, različiti nazivi za možda
   istu stvar i tekst koji očigledno pripada nekom drugom zadatku vrati kao
   otvoreno pitanje sa mogućim tumačenjima.

## Pokrivenost
Svaki segment, osim naslova, je dokaz bar jedne tvrdnje ili je naveden u
excluded.

## Nazivi i jezik
Nazivi i opisi su na jeziku dokumenta, kratki. Ista stvar ima isti naziv na
svim mestima. ID-jevi su kratki ASCII nazivi izvedeni iz naziva (npr.
rezervacija, stavka_racuna). Veze (links.to), represented_by i instance_of
navode ID stvari; applies_to, needs i fills navode ID stvari ili "id.svojstvo".
Polja bez vrednosti su "" ili [].

## Izlaz
Vrati isključivo JSON:
- actors: [{id, name, description, represented_by (ID stvari ili ""),
  differs_by, evidence}]
- things: [{id, name, kind (object | record | classification | settings),
  description, identified_by, instance_of, variants, created_when,
  properties: [{name, meaning, value_type (text | long_text | integer | decimal |
    money | boolean | date | time | datetime | file | email | phone | url),
    shape (single | composite | multiple), parts,
    presence (required | optional | conditional), condition, variant,
    allowed_values, origin (entered | generated | derived | copied), source,
    evidence}],
  links: [{to, meaning, per_this, per_other, evidence}],
  states, transitions: [{from, to, trigger, by, effects, evidence}],
  evidence}]
- rules: [{id, kind (uniqueness | access | visibility | eligibility |
  precondition | approval | priority | quantity | time | mutability | retention |
  parameter | context | other), statement, applies_to, parameters, evidence}]
- queries: [{id, description, needs, evidence}]
- imports: [{id, description, fills, evidence}]
- boundaries: [{kind (external_process | delegated_decision), description,
  kept_outcome, evidence}]
- excluded: [{segment, reason (technology | presentation | assessment |
  demo_data | database_setup | other)}]
- open_questions: [{id, question, readings, affects, evidence}]
gde je evidence: {segments, mode (direct | implied)}; source je pravilo
izračunavanja za derived, odnosno "stvar.svojstvo" i događaj za copied;
per_this i per_other su brojnosti kao "1", "0..1", "0..N", "1..N" ili "najviše 3".`

const sourceUnitExtractionInstructions = `You are classifying an LLM-segmented
document for a relational-database modeling pipeline.

Return JSON only. Return exactly one classification for every supplied OD unit ID.
Do not copy source text and do not invent IDs; the backend owns segment text and
stable OD and SU IDs. Do not translate,
paraphrase, normalize, or return rewritten source text. Respect the supplied
segment type. Classify UI-only text,
examples, headings and noise explicitly instead of treating every noun as persistent
data.

Set requires_review=true (with a warning) only for problems of the source itself:
damaged or garbled text (OCR, broken structured examples), a sentence that is
cut or merged wrongly, or genuinely uncertain kind or relevance. Confidence
describes how certain the classification is. When the sentence is read and
classified correctly but what it requires is vague, incomplete, refers to
content elsewhere or conflicts with another rule, keep requires_review=false and
describe that in requirement_notes; those notes become modeling questions later.

Return one classification for every supplied segment ID.`

const requirementAtomExtractionInstructions = `Extract atomic requirements from
validated source units for logical relational database design. Return JSON only.
Every atom must cite existing source-unit IDs. Separate persistent-data requirements
from UI-only behavior, application logic, external behavior and examples. A source
unit may carry requirement_notes describing vague, incomplete or conflicting
requirements. Treat those notes as context, not as automatic human-review requests.
Use review_class=non_blocking_gap when a useful detail is simply absent but the stated
database fact is still clear. Use blocking_model_choice only when two or more plausible
interpretations would change persistence, identity, cardinality, uniqueness, lifecycle,
enforcement or schema shape. Use blocking_source_defect only when damaged source text
prevents a reliable database interpretation. Use external_dependency for a referenced
external rule that does not require a local modeling choice, otherwise none. Set a
review_topic and a short stable review_group for blocking atoms that share one business
decision. Explain every non-none classification in warnings. Write each statement as one
self-contained sentence naming the actor, the action, the object and any timing. Fill quantifier and
condition only when the source states them; otherwise use an empty string and
never invent a generic filler.
Set atom_type to the single best category: entity_identity, persistent_data,
data_attribute, relationship (an association between two concepts), cardinality_constraint
(how many), ownership_rule, uniqueness_constraint, validation_rule, business_rule,
state_transition, lifecycle_event, history_requirement, generated_value, derived_value,
report_query, authorization_rule, actor_role, operation, file_import, notification,
ui_behavior, navigation, technology_constraint, example or other. A sentence that
links two concepts ("each line has several stops") needs its own relationship or
cardinality_constraint atom. Preserve exact
numbers, boundaries and timing rules. Do not propose tables, SQL, DBML, functional
areas or CRUD here. Set persistence_effect to the concrete durable-data consequence.
For examples, distinguish schema shape, normative seed data, constraint boundaries,
and illustrative instances. Schema-shape, seed-data and constraint-boundary examples
are normative; illustrative instances are not represented in the model. Never create
one normative atom for every literal value inside an illustrative JSON object.

A structured example is supplied as a shape-only sketch: quoted scalar values have
already been replaced with the short placeholder <v>. Use observed key names, nesting,
arrays, repeated keys and record boundaries only as evidence for candidate concepts,
attributes, relationships, ordering or cardinality. Literal example values never
define enums, uniqueness, validation boundaries, seed data or required rows unless
separate normative prose explicitly says that they do. A damaged JSON example may
still provide schema-shape evidence; record the uncertain part in warnings instead
of discarding every observable key and containment relation. Every cited
structured_example source unit must be covered by at least one atom whose
example_role explicitly states schema_shape, seed_data, constraint_boundary or
illustrative_instance. Keep example_role=none on the separate file_import or
operation atom; express the observed shape in one or more distinct represented
schema_shape atoms.`

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
Return JSON only. Cite existing IDs only in the affected_* and depends_on fields;
write the question, description, option labels and rationales as plain business
language for a database designer, never mentioning internal IDs such as RA-, SU-,
OP- or FA-. Provide two or three materially different options, explain effects and risks, and form an acyclic depends_on graph.
Use depends_on only for hard ordering and may_affect for informative impact. Mark a
question blocking only when the downstream conceptual/logical model would otherwise
encode an unsupported choice. Group all atoms controlled by the same business choice
under one stable decision_key. Input atoms already carry review_group: emit exactly one
candidate per distinct review_group, cover every supplied atom exactly once, and never
combine different groups in one candidate. Every option must declare machine-readable atom_updates,
impact_dimensions and any already-known followup_candidate_ids; use no_change when the
choice only records evidence. Do not defer structured patch creation to a later LLM
call and do not create cosmetic or generic questions. Source units with
structured_shape contain <v> placeholders for omitted illustrative values. Questions
may address uncertain field shape, nesting or cardinality, but must never treat those
omitted values as candidate enums, seed rows or validation rules.`

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
smallest directly supporting evidence set for each element. Labels are short
business names of at most six words (for example "First name"); never put
reasoning, questions, uncertainty or review remarks into a label or description. Relationship endpoints must reference entity concept IDs,
except that an attachment/ownership relationship may connect exactly one entity
concept and one file concept. Never use lifecycle, derived or import concept IDs as
relationship endpoints. Do not hide an unresolved blocking ambiguity. Prefer
stable business concepts over UI components. Every required design obligation must
be represented explicitly (relationship, cardinality and ownership obligations by a
relationship citing the obligation's atom; key obligations by a constraint concept): generated values that must be reproducible need a
snapshot concept, repeatable actions need event/history concepts, lifecycle rules
need lifecycle concepts, and derived outputs need a derived concept rather than an
untyped generic fact container. Represent uniqueness, checks, security, temporal,
ownership, cross-row and application-enforced invariants as constraint_concepts with
direct evidence; do not attach citations to an unrelated attribute merely to satisfy
coverage.

Source units with structured_shape are shape-only evidence. Use their observed keys,
nesting, arrays and repeated keys to propose concepts, attributes and relationships.
The <v> placeholders are deliberately omitted illustrative values: never turn them
into enum members, defaults, seed rows, uniqueness rules or validation boundaries.

The accepted conceptual model is mapped to database tables by deterministic rules,
so every decision a table needs must be made here:
- attribute name: lower snake_case column name ("first_name"); never model a
  surrogate primary key or a reference to another entity as an attribute, because
  keys and foreign keys are generated from entities and relationships.
- attribute value_type: string, text, integer, decimal, boolean, date, time,
  datetime, uuid, email, phone, url, file_path or money. unique=true only when the
  source requires the value to be unique. enum_values only for a string with a
  closed value set stated by the source.
- relationship cardinality is read from "from" to "to" (one_to_many: one "from"
  has many "to"). required=true when the referencing side must always have the
  reference. Use many_to_many only for a plain link; a link with its own data is an
  association entity with two relationships. Avoid two relationships between the
  same pair of entities in the same direction.
- a check constraint concept carries a boolean expression over attribute names of
  one entity (for example "price >= 0"); leave expression empty when the rule is
  application-enforced. targets reference attribute IDs.
- a lifecycle concept names its owner entity ID, the status attribute name in field,
  the states, initial, terminal states and allowed transitions.
- a derived concept lists the entity IDs it is computed from in sources and what it
  shows in metrics.`

const conceptualChunkInstructions = conceptualModelInstructions + `
This request is one bounded functional-area/design-obligation chunk. Return a
self-contained conceptual-model fragment for only the supplied chunk_scope. Reuse
stable business-oriented IDs so fragments from other chunks can be merged by ID.
Include both endpoints when they are required to explain an in-scope relationship,
but do not model unrelated areas. Cover every supplied design obligation explicitly.`

const conceptualRepairInstructions = `Labels are short business names of at most six
words and never contain reasoning or remarks. Repair a conceptual model using only the
reported validation issues, compact evidence context, affected existing fragment
and lightweight concept registry. Return JSON only using the conceptual-model
schema. Return only new or corrected concepts; when correcting an existing entity,
return that complete entity with the same ID. Do not recreate registry entries or
unaffected concepts. Every returned element must have direct evidence, and every
listed missing design obligation must become represented. Constraint and security
rules belong in constraint_concepts. The backend deterministically merges this
delta into the retained model, preserves existing evidence coverage, and revalidates
the complete model.`

// Design obligations are supplied as an explicit bridge between requirement
// meaning and model realization. The prompt deliberately keeps them visible:
// application behavior may still require persisted events, snapshots or
// constraints even when it is not itself a table.

const logicalProjectionInstructions = `Project an accepted conceptual model and its
design obligations into a DB-DSL v0.5 structured patch. Return JSON only,
never YAML, DBML or SQL.
Every element must preserve source-unit, requirement-atom and review-decision
evidence. Use regular, lookup or association entities, lower snake_case attributes,
and DB-DSL scalar types. Do not emit explicit surrogate id attributes for regular or
lookup entities because the deterministic DBML generator supplies them. Cite only
the smallest directly supporting evidence set. Keep descriptions and notes concise.
Source units with structured_shape contain <v> placeholders for omitted illustrative
values. Preserve the modeled field and relationship shape, but never materialize
those placeholders or infer enums, defaults, seed rows or constraints from them.
Project an entity-to-file conceptual relationship as a scalar file-reference
attribute on the owning entity plus a file_spec; do not emit the file concept as an
entity or as a database relationship endpoint.
Do not emit a separate required constraint when an attribute's required flag already
expresses the same rule. Relationship requiredness belongs only in relationship.required
or relationship.fk_required; never emit a required constraint for a relationship FK.
Constraint field and fields references may use an owner-local scalar attribute, a
generated FK name, or the relationship_id listed for that owner in
constraint_reference_catalog. Prefer relationship IDs in composite unique constraints;
the backend resolves them deterministically to physical FK columns. Import mapping
targets must use Entity.attribute syntax and reference an existing scalar attribute.
Every file spec owner.field must reference
an existing entity attribute. Every represented requirement atom must be cited by
at least one model element. Every required design obligation must have an explicit
realization suitable for deterministic verification: cite the obligation's atom on a
relationship for relationship, cardinality and ownership obligations; on a unique
constraint or key field for key and identity; on a state_machine or status field for
lifecycle; on a derived_view for derived_view; on a constraint, field or state_machine
for invariant; and on a field or entity for attribute. Do not use fact_kind/fact_value
or another generic key/value container in place of named domain facts. Preserve
generated input snapshots and score-relevant event history when replayability or
auditability is required. An association entity cannot be an endpoint that needs
a scalar foreign key; use a regular entity when it has its own lifecycle or incoming
relationships. When repair_mode is true, address every supplied validation error and
return only corrected, new, or remove_operation entries for the supplied failing
fragment. Use remove_operation with target_operation and target_id only to delete an
invalid prior operation; all object payload fields remain null. Keep the same element
IDs when correcting an operation. The backend applies removals, replacements and
additions in that order and validates the complete DB-DSL bundle. Outside repair_mode,
use add operations only.`

const logicalProjectionChunkInstructions = logicalProjectionInstructions + `
This request is one bounded logical-projection chunk. Emit entity operations only
for chunk_scope.primary_concept_ids. Emit the supplied relationships, constraints
and auxiliary concepts exactly once when they belong to this chunk. Context-only
relationship endpoints must not be emitted as duplicate entity operations. The
backend deterministically merges this fragment with the other chunks and validates
the complete patch.`

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
- For add operations, set target_operation and target_id to empty strings. Those
  fields are reserved for repair-only remove_operation entries.

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
