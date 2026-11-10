# Politica sul marchio

Il codice di Porto è disponibile con la [Business Source License 1.1](LICENSE) (BSL 1.1), che converte ogni versione
in AGPL-3.0 tre anni dopo il suo primo rilascio pubblico (ADR 0018): puoi usarlo, modificarlo e ridistribuirlo alle
condizioni della licenza. **Né la BSL 1.1 né la AGPL-3.0 concedono diritti sul nome e sul logo del prodotto: nome e
logo restano riservati a Coste360 SRL.** Questa politica spiega come usare il marchio in modo corretto; le regole con
cui il software ufficiale lo mostra sono nell'[ADR 0007](docs/adr/0007-marchio.md).

> Il nome **Porto** è confermato (D1, ADR 0007). Il sito ufficiale è [portomarina.app](https://portomarina.app); questa
> politica si applica al nome e al logo in uso.

## Il marchio

Sono marchi di **Coste360 SRL** ("i Marchi"):

- il nome **Porto** usato come nome di un software per la gestione di porti turistici, marine, approdi, cantieri
  nautici e rimessaggi;
- il logo di Porto: il simbolo (la "P" che diventa banchina, con il pontile in negativo e lo specchio acqueo in
  azzurro), il logotipo "porto" in minuscolo, le icone dell'app, la firma **"Porto — Coste360"** delle email, la
  dicitura **"powered by Porto"** della pagina pubblica e gli elementi grafici distintivi del marchio (colori e
  composizione);
- il nome e il logo **Coste360** / **Coste360.it**.

"Porto" è anche una parola comune: questa politica non limita in alcun modo l'uso della parola nel suo significato
ordinario ("il porto di Gaeta", "porto turistico"), ma solo il suo uso come nome di un software o di un servizio
confondibile con questo prodotto.

I file ufficiali dei marchi sono in [`assets/brand/`](assets/brand/) (`porto/` per Porto, `coste360/` per Coste360):
sono di Coste360 SRL, con tutti i diritti riservati, e **non** sono coperti dalla licenza del codice (vedi
[`assets/brand/porto/NOTICE.md`](assets/brand/porto/NOTICE.md) e
[`assets/brand/coste360/NOTICE.md`](assets/brand/coste360/NOTICE.md)). Il repository li contiene perché il software
ufficiale li mostra (pannello, pagina di prenotazione e widget, scheda pubblica del porto, email, app desktop,
documentazione); usali solo come descritto qui.

Lo scopo è semplice: chi vede il nome Porto deve poter contare sul fatto che si tratti del prodotto ufficiale di
Coste360, o di una sua copia non modificata.

## Usi consentiti senza autorizzazione

- **Parlare del prodotto** in articoli, recensioni, presentazioni, tesi, corsi e materiale formativo ("il gestionale
  Porto", "compatibile con Porto", "basato su Porto").
- **Ridistribuire copie non modificate** del software ufficiale (binari, immagini Docker, pacchetti dell'app desktop,
  sorgenti) mantenendone il nome.
- **Offrire formazione** su Porto e, se la licenza del codice lo consente (licenza commerciale di Coste360 SRL, oppure
  versioni già convertite in AGPL-3.0), servizi di installazione, hosting o assistenza, purché non si lasci credere di
  essere il fornitore ufficiale o di avere l'approvazione di Coste360 SRL (per esempio "Installazione e assistenza per
  Porto", non "Porto Cloud ufficiale"). Questa politica riguarda solo il marchio: i limiti sull'uso del codice sono
  quelli del file [LICENSE](LICENSE).
- **Fare piccole modifiche** di configurazione o di pacchettizzazione (per esempio per una distribuzione Linux o un
  modello di installazione), dichiarandolo in modo chiaro.
- **Usare il nome nelle estensioni** costruite sulle interfacce di `pkg/ext`, in forma descrittiva ("Driver di
  pagamento Satispay per Porto", "Esportatore verso il software XYZ per Porto").
- **Indicare che un porto usa Porto**, per esempio nella pagina di prenotazione o sul sito del marina, lasciando la
  dicitura "powered by Porto" del software ufficiale.

## Usi che richiedono un nome diverso

**I fork devono cambiare nome e logo.** Vale anche per i fork delle versioni già convertite in AGPL-3.0: la
conversione della licenza riguarda il codice, non il marchio. Se distribuisci o offri come servizio una versione
modificata del software (oltre alle piccole modifiche indicate sopra), devi:

1. usare un nome che non contenga "Porto" come nome del software e non sia confondibile con esso;
2. togliere o sostituire il logo, le icone e gli elementi grafici distintivi;
3. sostituire i riferimenti al marchio nell'interfaccia (logo del pannello e firma "un prodotto di Coste360"), nella
   pagina di prenotazione e nella scheda pubblica (dicitura "powered by Porto"), nelle email (firma
   "Porto — Coste360"), nelle icone dell'app desktop e nella documentazione per gli utenti finali, togliendo i file di
   `assets/brand/` dalla tua distribuzione;
4. puoi (e ti incoraggiamo a) indicare l'origine in forma descrittiva: "basato su Porto, di Coste360 SRL".

Restano obbligatori gli avvisi di copyright e di licenza: la BSL 1.1 richiede di riportare la licenza su ogni copia,
originale o modificata; per le versioni convertite la AGPL-3.0 richiede anche di rendere disponibile il codice
sorgente.

## Usi non consentiti

- Usare i Marchi per far pensare a una sponsorizzazione, un'approvazione o una partnership con Coste360 SRL che non
  esiste.
- Usare i Marchi nel nome di un'azienda, di un prodotto o servizio commerciale diverso o di un dominio (per esempio
  `porto-gestionale-cloud.it`) senza autorizzazione scritta.
- Modificare il logo (proporzioni, colori, carattere del logotipo, dicitura), ridisegnarlo, aggiungervi ombre o
  gradienti, o combinarlo con altri marchi.
- Usare il logo o le icone di Porto per prodotti, siti o app diversi dal software ufficiale.
- Usare i Marchi in modo ingannevole o denigratorio, o per software che contiene codice malevolo.

## Richieste di autorizzazione

Per gli usi non coperti da questa politica, o in caso di dubbio, scrivi a **info@coste360.it** oppure usa la pagina
[www.coste360.it/contatti](https://www.coste360.it/contatti). Valutiamo con favore le richieste della comunità.

Coste360 SRL può aggiornare questa politica; vale la versione pubblicata nel repository.
