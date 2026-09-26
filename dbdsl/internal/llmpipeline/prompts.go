package llmpipeline

const PromptTemplateVersion = "llm_protocol_v0_13_3_2026_09_26"

const promptTemplateVersion = PromptTemplateVersion

const sourceSegmentationInstructions = `Zadatak je da dokument podelis na segmente i svakom segmentu odredis tip.
Dokument je specifikacija zadatka izvucena iz PDF-a ili tekstualne datoteke.
Recenice su u njemu prelomljene u vise redova, a strane sadrze zaglavlja,
podnozja, brojeve strana i fusnote. Dokument se nalazi izmedju oznaka <document>.
## Tipovi segmenata
- heading: naslov dokumenta ili odeljka
- sentence: jedna recenica tekuceg teksta
- list: recenica koja uvodi listu, zajedno sa svim stavkama liste
- example: recenica koja najavljuje primer, zajedno sa samim primerom (redovi sa
  oznakama, kao „Primer: … Resenje: …“, ili JSON, XML, CSV ili tabela)
- footnote: fusnota, zajedno sa svojim brojem
- page_header: tekst koji se ponavlja na vrhu strana
- page_footer: tekst koji se ponavlja na dnu strana
- page_number: samostalan broj strane
- other: sve sto ne odgovara nijednom od prethodnih tipova
## Pravila
1. Tekst prepisi tacno onako kako je napisan: ista slova u istom pismu, iste
   cifre, interpunkcija, navodnici i simboli, ukljucujuci greske i ostecene
   strukturirane podatke.
2. Svaki prelom reda unutar segmenta zameni jednim razmakom.
3. Rec prelomljenu crticom na kraju reda spoji. Crticu izostavi ako samo deli
   rec, a zadrzi je ako bi postojala i da rec nije prelomljena.
4. Kada zaglavlje, podnozje, broj strane ili fusnota prekida recenicu, listu ili
   primer, prekinuti segment vrati ceo, a tekst koji ga prekida vrati kao
   zasebne segmente odmah posle njega.
5. Svaki deo dokumenta pripada tacno jednom segmentu. Segmente vrati poredjane
   prema mestu na kome pocinju u dokumentu.
## Izlaz
Vrati iskljucivo JSON:
{"segments": [{"type": "...", "text": "..."}]}`

// conceptualDescriptionInstructions is the second LLM interaction of the flow:
// numbered segments in, a rich denormalized description of what the system
// must remember out. Tables and keys are derived from it deterministically.
const conceptualDescriptionInstructions = `Zadatak je da iz specifikacije softverskog sistema izdvojis potpun opis onoga
sto sistem mora da pamti: aktere, stvari, njihova svojstva i veze, zapise o
dogadjajima, stanja i pravila. Opis sluzi kao osnova za projektovanje baze
podataka.

## Ulaz
Specifikacija je data kao segmenti izmedju oznaka <segments>, u redosledu
dokumenta. Svaki red pocinje ID-jem segmenta i tipom u uglastim zagradama:
- heading: naslov; odredjuje odeljak kojem pripadaju segmenti ispod njega.
- sentence: recenica.
- list: recenica koja uvodi listu, zajedno sa svim stavkama liste. Uvodna
  recenica vazi za svaku stavku.
- footnote: fusnota sa svojim brojem; vazi za mesto u tekstu koje nosi taj broj
  kao oznaku uz rec.
- example: primer sa recenicom koja ga najavljuje. Primer pokazuje oblik
  podataka ili format uvoza; vrednosti u njemu nisu skup dozvoljenih vrednosti
  niti pocetni podaci, osim ako tekst to izricito kaze.
- other: ostalo.

## Kakav opis se trazi
- Potpun: sve sto tekst kaze ili nuzno podrazumeva o podacima.
- Denormalizovan: svojstva i veze navedi kod stvari o kojoj tekst govori, onako
  kako ih tekst opisuje. Ne projektuj tabele, kljuceve ni strane kljuceve.
- Oslonjen na tekst: svaka tvrdnja navodi segmente iz kojih potice i da li
  tekst to kaze direktno ili nuzno podrazumeva. Ono
  sto bi zahtevalo nagadjanje ide u otvorena pitanja; ne dopunjavaj zdravim
  razumom.

## Postupak
1. Procitaj ceo tekst pre opisivanja. Akteri, pravila i dopune cesto se pojave
   tek u kasnijim odeljcima, a isti pojam se na razlicitim mestima moze zvati
   razlicito.
2. Izdvoj tekst koji se ne odnosi na sistem, nego na izradu i ocenjivanje
   zadatka: tehnologije, izgled stranica i navigaciju, poene, rokove, odbranu,
   podatke za demonstraciju, nacin kreiranja baze. On ne ulazi u opis, osim dela
   koji ipak kaze nesto o sistemu (npr. prikaz bojom koji razlikuje dve vrste
   zapisa: pamti se razlika, ne boja). Segment se iskljucuje samo ako nijedan
   njegov deo ne govori o podacima sistema; iskljucen segment nije dokaz.
3. Opisi aktere i stvari (tacke 1–6 ispod).
4. Za svaku radnju koju tekst opisuje proveri sta posle nje mora ostati
   zapamceno: ucesnici cije se ucesce pamti, sadrzaj, vreme, stanje i ishod.
5. Za svaki prikaz, pretragu, izvestaj, izracunavanje i pravilo proveri da li su
   podaci koje zahteva opisani i da li se do njih stize vezama od stvari na koju
   se odnosi; ako nisu, dodaj ih kao nuzno podrazumevane.
6. Proveri pokrivenost (odeljak Pokrivenost).

## Na sta obratiti paznju
1. Akteri i identitet. Vrste korisnika, po cemu se razlikuju, kome pripadaju i
   koje podatke imaju; nalog, osoba i organizacija nisu isto, a spisak vrsta
   korisnika na pocetku teksta ne mora biti potpun. Kada tekst razlikuje opstu
   definiciju od konkretnog pojavljivanja ili primerka (npr. ponudu i pojedinacni
   vaucer iz nje), opisi oba nivoa i vezu medju njima. Za svaku stvar navedi kome
   pripada (npr. preduzecu ili korisniku) kao vezu ka toj stvari. U identified_by
   navedi svojstva koja stvar jedinstveno odredjuju, a kada jedinstvenost vazi samo
   u okviru necega, i ID te stvari (npr. ["sifra", "preduzece"] za sifru
   jedinstvenu u okviru preduzeca). Vrednost kojom se korisnik prijavljuje ili po
   kojoj se jedna stvar bira izmedju drugih nuzno je jedinstvena. Ako tekst
   jedinstvenost ne kaze niti je nuzno podrazumeva, ostavi [] i postavi otvoreno
   pitanje. identified_by navodi svojstva i ID-jeve stvari, ne aktera. Svaka druga
   jedinstvena vrednost ili kombinacija je posebno pravilo vrste uniqueness, ciji
   applies_to navodi tacno tu kombinaciju (npr. ["korisnicki_nalog.email"]);
   za uniqueness pravilo postavi comparison na case_sensitive ili
   case_insensitive samo kada tekst izricito zahteva taj nacin poredjenja, inace
   postavi ""; ne zakljucuj comparison iz vrste podatka ili uobicajene prakse;
   ogranicenje broja zapisa sa nekom vrednoscu je pravilo vrste quantity. Navedi
   i kako identifikator nastaje. Ne dodaj tehnicke identifikatore koje tekst ne
   pominje.
2. Zapisi o dogadjajima. Zahtevi, prijave, rezervacije, odluke, uplate i slicne
   interakcije su stvari za sebe, sa ucesnicima, vremenom, sadrzajem, stanjem i
   ishodom. Kada se kasnije prikazuje ili proverava ono sto je bilo, zapis ostaje
   u istoriji. Vrednost koja se moze promeniti, a mora ostati onakva kakva je
   bila u trenutku dogadjaja (cena, porez, vazeci parametar ili iz njih
   izracunat iznos), navedi kod zapisa tog dogadjaja, sa izvorom iz kog je
   preuzeta. Veza ka akteru koji je radnju izvrsio postoji samo kada tekst kaze
   ili nuzno podrazumeva da se pamti ko je to uradio; to sto akter sme da doda,
   izmeni ili odobri stvar samo po sebi nije veza. Ako to nije sigurno, pitanje
   ide u otvorena pitanja, a ne u veze. Kada ishod zapisa stvara novu stvar, nova
   stvar ima vezu ka zapisu iz kog je nastala, a podatke zapisa koje njena
   pravila, upiti i prikazi koriste navodi kod nje kao copied.
3. Stanja i prelazi. Stanja, sta ili ko menja stanje, i sve posledice prelaza
   (dodela, promena kolicine, promena prava ili limita). Navedi kada zapis
   nastaje ako ne nastaje odmah, i koje korake akter pokrece rucno.
4. Pravila. Ko sme sta da vidi, menja ili odobri; uslovi i preduslovi radnji;
   prvenstvo; vidljivost zavisna od stanja; sta se ne sme menjati posle nastanka;
   da li se brise, arhivira ili nikad ne brise.
5. Vreme i kolicina. Rokovi, intervali vazenja, periodicna pravila i
   resetovanja, pravila nad uzastopnim dogadjajima, najmanje i najvece vrednosti,
   kapaciteti, limiti koji se menjaju, i parametri koje neki akter podesava i
   koji vaze za ceo sistem.
6. Struktura vrednosti. Vrsta vrednosti svakog svojstva (value_type; izbor
   da/ne je boolean), slozene vrednosti sa delovima (delovi jedne vrednosti, ne
   grupa nepovezanih svojstava), visestruke vrednosti,
   opcione i uslovne vrednosti, svojstva samo jedne varijante, vrednosti iz
   zatvorenog skupa, hijerarhije klasifikacija, pravila i granice koje zavise od
   drugog podatka. Vrednost koja se ponavlja po necemu drugom (po danu, po
   stavci) nije visestruko svojstvo, nego posebna stvar ciji identitet
   ukljucuje to po cemu se ponavlja.
7. Granice. Spoljni proces se ne opisuje, ali ishod koji sistem o njemu pamti se
   opisuje. Odluku koju tekst prepusta izvodjacu ne pretvaraj u zahtev. Vrednost
   za koju tekst kaze da se drzi u datoteci ili podesavanjima van baze je
   granica, a ne stvar; zapis koji je koristi cuva je kao copied.
8. Nejasnoce. Kontradikcije, nedefinisana pravila, razliciti nazivi za mozda
   istu stvar i tekst koji ocigledno pripada nekom drugom zadatku vrati kao
   otvoreno pitanje sa mogucim tumacenjima.

## Pokrivenost
Svaki segment, osim naslova, je dokaz bar jedne tvrdnje ili je naveden u
excluded. Segmenti se navode pojedinacno, tacnim ID-jem.

## Nazivi i jezik
Nazivi i opisi su na jeziku dokumenta, kratki. Srpski tekst pisi osisanom
latinicom, bez slova č, ć, š, ž i đ (c, c, s, z i dj), i kada je dokument
cirilicom. Ista stvar ima isti naziv na
svim mestima. ID-jevi su kratki ASCII nazivi izvedeni iz naziva (npr.
rezervacija, stavka_racuna). Veze (links.to), represented_by i instance_of
navode ID stvari. applies_to, needs, criteria, fills, affects i identified_by
navode ID stvari ili "id.X", gde je X svojstvo te stvari, ID stvari sa kojom je
povezana ili njeno stanje ("id.stanje"). Svaku vezu navedi jednom: kod stvari
koja upucuje na drugu, a vezu vise-prema-vise kod bilo koje od dve stvari.
Polja bez vrednosti su "" ili [].

## Izlaz
Vrati iskljucivo JSON:
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
  parameter | context | other), statement, applies_to, parameters,
  comparison ("" | case_sensitive | case_insensitive), evidence}]
- queries: [{id, description, needs, criteria, evidence}]
- imports: [{id, description, fills, evidence}]
- boundaries: [{kind (external_process | delegated_decision), description,
  kept_outcome, evidence}]
- excluded: [{segment, reason (technology | presentation | assessment |
  demo_data | database_setup | other)}]
- open_questions: [{id, question, readings, affects, evidence}]
gde je evidence: {segments, mode (direct | implied)}; origin je entered (unosi
akter), generated (postavlja sistem i pamti, npr. vreme ili sadrzaj koji sistem
proizvede), copied (preuzeto u trenutku dogadjaja) ili derived (uvek se moze
izracunati iz drugih sacuvanih podataka i ne pamti se); source je pravilo
izracunavanja za derived, odnosno "stvar.svojstvo" i dogadjaj za copied;
per_this je koliko drugih stvari ima jedna ova, a per_other koliko ovih ima
jedna druga, kao "1", "0..1", "0..N", "1..N" ili "najvise 3"; needs su svi
podaci koje upit koristi, a criteria oni po kojima upit bira ili redja zapise, ne
oni koje samo poredi sa unetom vrednoscu.`

const baselineDBMLInstructions = `Generate DBML directly from the task text.
This is an evaluation baseline only. Do not include explanations outside DBML.`

const baselineSQLInstructions = `Generate SQL DDL directly from the task text.
This is an evaluation baseline only. Do not include explanations outside SQL.`
