package llmpipeline

const PromptTemplateVersion = "llm_protocol_v0_12_1_2026_09_25"

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
3. Reč prelomljenu crticom na kraju reda spoji. Crticu izostavi ako samo deli
   reč, a zadrži je ako bi postojala i da reč nije prelomljena.
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
   zapamćeno: učesnici čije se učešće pamti, sadržaj, vreme, stanje i ishod.
5. Za svaki prikaz, pretragu, izveštaj, izračunavanje i pravilo proveri da li su
   podaci koje zahteva opisani i da li se do njih stiže vezama od stvari na koju
   se odnosi; ako nisu, dodaj ih kao nužno podrazumevane.
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
   jedinstvenu u okviru preduzeća). Vrednost kojom se korisnik prijavljuje ili po
   kojoj se jedna stvar bira između drugih nužno je jedinstvena. Ako tekst
   jedinstvenost ne kaže niti je nužno podrazumeva, ostavi [] i postavi otvoreno
   pitanje. identified_by navodi svojstva i ID-jeve stvari, ne aktera. Svaka druga
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
   izmeni ili odobri stvar samo po sebi nije veza. Ako to nije sigurno, pitanje
   ide u otvorena pitanja, a ne u veze.
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
   opisuje. Odluku koju tekst prepušta izvođaču ne pretvaraj u zahtev. Vrednost
   za koju tekst kaže da se drži u datoteci ili podešavanjima van baze je
   granica, a ne stvar; zapis koji je koristi čuva je kao copied.
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
navode ID stvari; applies_to, needs, criteria i fills navode ID stvari ili
"id.svojstvo".
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
- queries: [{id, description, needs, criteria, evidence}]
- imports: [{id, description, fills, evidence}]
- boundaries: [{kind (external_process | delegated_decision), description,
  kept_outcome, evidence}]
- excluded: [{segment, reason (technology | presentation | assessment |
  demo_data | database_setup | other)}]
- open_questions: [{id, question, readings, affects, evidence}]
gde je evidence: {segments, mode (direct | implied)}; source je pravilo
izračunavanja za derived, odnosno "stvar.svojstvo" i događaj za copied;
per_this i per_other su brojnosti kao "1", "0..1", "0..N", "1..N" ili "najviše 3";
needs su svi podaci koje upit koristi, a criteria oni po kojima upit bira ili
ređa zapise.`

// Design obligations are supplied as an explicit bridge between requirement
// meaning and model realization. The prompt deliberately keeps them visible:
// application behavior may still require persisted events, snapshots or
// constraints even when it is not itself a table.

const baselineDBMLInstructions = `Generate DBML directly from the task text.
This is an evaluation baseline only. Do not include explanations outside DBML.`

const baselineSQLInstructions = `Generate SQL DDL directly from the task text.
This is an evaluation baseline only. Do not include explanations outside SQL.`
