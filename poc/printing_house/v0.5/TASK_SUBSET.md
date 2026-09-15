# Printing House - katalogski podskup zadatka

## 1. Opis podskupa

Realizovati deo web sistema "Printing House" koji sluzi za vodjenje kataloga
proizvoda jedne ili vise stamparija.

Ovaj podskup obuhvata samo podatke potrebne za katalog stamparije, proizvode,
kategorije, potkategorije, dostupne boje, slike proizvoda, usluge stampe i JSON
unos lager liste.

Korisnicki nalozi, registracija, autentifikacija, korpa, narudzbine, fakture,
placanje, javne nabavke, licitacije, komentari, lajkovi, statistike i grafici
nisu deo ovog podskupa.

## 2. Stamparija i katalog

Stamparija je vlasnik kataloga proizvoda.

Za stampariju se cuvaju poslovna sifra stamparije i naziv stamparije.

Jedna stamparija moze imati vise proizvoda u svom katalogu, a svaki proizvod
pripada tacno jednoj stampariji.

## 3. Kategorije i potkategorije

Proizvodi se dodaju u vec predefinisane kategorije i potkategorije.

Kategorije su: "Stampa malih formata", "Stampa velikih formata" i
"Kreativne stampe".

Primeri potkategorija su: olovke, vizit karte, flajeri, posteri, rollups,
fototapete, solje, stampa na majicama, stampa na duksevima i stampa na cegerima.

Svaka potkategorija pripada tacno jednoj kategoriji.

Svaki proizvod pripada tacno jednoj potkategoriji.

## 4. Proizvodi

Za svaki proizvod potrebno je cuvati sifru proizvoda, naziv proizvoda, opis
proizvoda, jedinicnu cenu, kolicinu na lageru, kategoriju i potkategoriju.

Sifra proizvoda je poslovni identifikator proizvoda u okviru kataloga
stamparije.

Naziv proizvoda je obavezan za prikaz proizvoda u katalogu.

Opis proizvoda moze biti prazan.

Jedinicna cena proizvoda mora biti pozitivna vrednost.

Kolicina proizvoda na lageru mora biti ceo broj koji nije negativan.

U ovom podskupu cuva se samo trenutno stanje lagera, a ne cuva se istorija
promena kolicina.

## 5. Slike i boje proizvoda

Proizvod moze imati jednu glavnu sliku i nula ili vise dodatnih slika.

Slike proizvoda u ovom podskupu cuvaju se kao URL ili tekstualna referenca do
slike.

Za proizvod se cuva lista dostupnih boja.

Jedan proizvod moze imati vise dostupnih boja, a ista boja moze biti dostupna
za vise proizvoda.

## 6. Usluge stampe

Usluge stampe su specijalizovani tipovi stampe ili preslikaca koji povecavaju
cenu po proizvodu.

Proizvod moze imati vise ponudjenih usluga stampe.

Ista usluga stampe moze biti ponudjena za vise proizvoda.

Za uslugu stampe cuvaju se sifra usluge i naziv, odnosno tip stampe.

Za vezu proizvoda i usluge stampe cuvaju se dodatna cena po komadu, maksimalna
sirina stampe u milimetrima i maksimalna visina stampe u milimetrima.

Dodatna cena usluge stampe po komadu ne sme biti negativna.

## 7. JSON unos lager liste

Stampar ima mogucnost unosenja lager liste putem dodavanja JSON fajla, cime se
postavljaju definisani proizvodi i usluge za njegovu stampariju.

JSON fajl ima korenska polja "stampaorijaId", "nazivStamparije" i "proizvodi".

Polje "stampaorijaId" predstavlja izvornu sifru stamparije iz JSON fajla.

Polje "nazivStamparije" predstavlja naziv stamparije.

Svaki element liste "proizvodi" sadrzi polja "sifra", "naziv", "opis",
"kategorija", "potkategorija", "jedinicnaCena", "kolicinaNaLageru",
"dostupneBoje", "slikaUrl", "dodatneSlike" i "uslugeStampe".

Polje "dostupneBoje" je lista naziva boja.

Polje "slikaUrl" predstavlja glavnu sliku proizvoda, a polje "dodatneSlike"
predstavlja listu dodatnih slika proizvoda.

Svaki element liste "uslugeStampe" sadrzi polja "idUsluge", "tipStampe",
"dodatnaCenaPoKomadu", "maxSirinaMm" i "maxVisinaMm".

## 8. Primer JSON fajla

Primer JSON fajla nalazi se u:

```text
pia/raw/2025_2026/2025_2026_003_projektni_zadatak_za_avgustovski_i_septembarski_rok_za_skolsku_2025_2026_godine_mozete_preuzeti_ovde_minimalni_zahtevi_z.json
```

U primeru se pojavljuje stamparija "Copy Studio Kumanovska" sa sifrom
"stampa_001" i proizvodima "Pamucna Polo Majica", "Keramicka solja 330ml" i
"Promotivni Roll-up Baner 85x200cm".
