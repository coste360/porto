---
title: Licenza e marchio
description: Business Source License 1.1 con uso gratuito per i porti, conversione in AGPL-3.0 dopo tre anni, marchio riservato.
sidebar:
  order: 7
---

Porto è **source available** (codice sorgente disponibile) con licenza
**[Business Source License 1.1](https://github.com/coste360/porto/blob/main/LICENSE)** (SPDX `BUSL-1.1`). Il testo
vincolante è quello del file `LICENSE`; questa sintesi non lo sostituisce.

## In pratica

- **Gratuito per chi gestisce il proprio porto.** Puoi usare Porto in produzione, senza pagare nulla, per i porti
  turistici, le marine, gli approdi e i punti d'ormeggio (compresi i campi boe), i cantieri e i rimessaggi che
  possiedi o gestisci direttamente, anche tramite società controllate, con i servizi accessori dello stesso sito, e
  pubblicare la pagina di prenotazione e il widget per i tuoi clienti.
- **Libero per provarlo e migliorarlo.** Puoi leggere il codice, copiarlo, modificarlo, testarlo e contribuire.
- **Non rivendibile.** Offrire Porto o una sua versione modificata a terzi a pagamento o con altro vantaggio
  commerciale (hosting, servizio gestito o SaaS, installazione, configurazione, personalizzazione, manutenzione,
  assistenza, rivendita), o includerlo in un prodotto concorrente, richiede una **licenza commerciale** di Coste360
  SRL: scrivi a info@coste360.it.
- **Diventa AGPL-3.0 dopo 3 anni.** Ogni versione passa alla licenza
  [GNU AGPL-3.0 o successiva](https://www.gnu.org/licenses/agpl-3.0.html) tre anni dopo il suo primo rilascio
  pubblico.

## Casi pratici

- **Gestione diretta.** Gestisci direttamente una struttura se tu, o una società del tuo gruppo, ne hai la
  concessione (per esempio la concessione demaniale marittima), la licenza o il titolo, oppure la gestisci per conto
  tuo e non come fornitore di un altro. Sono compresi i servizi accessori dello stesso sito, come ormeggi,
  carburante, noleggio, lavori di cantiere, bar e ristorante.
- **Agenzie, tecnici e software house.** Installare, configurare, personalizzare, ospitare, mantenere o assistere
  Porto per altri porti a pagamento richiede una licenza commerciale. Il gestore può invece installarlo da sé, con i
  propri dipendenti o su un server che noleggia.
- **Consorzi e associazioni.** Ogni porto socio usa Porto gratis per sé; un consorzio o un'associazione che non
  controlla i porti soci non li gestisce direttamente e, per offrire Porto ai soci, serve una licenza commerciale.
- **Siti web.** Inserire la pagina di prenotazione o il widget in un sito o in un'app non richiede licenza, purché
  chi lo fa non installi, ospiti, configuri, personalizzi, mantenga o assista Porto.
- **Aiuto gratuito.** Aiutare un gestore gratis è permesso, se l'aiuto non serve a vendere o promuovere un'offerta a
  pagamento legata a Porto o alla gestione, alle prenotazioni o al sito del porto.
- **Modelli di installazione.** Un modello generico o "in un clic", con cui il gestore avvia Porto da sé sul server
  che noleggia, è permesso se il fornitore non gestisce Porto per lui e non vende un'offerta dedicata a Porto.

La decisione e le sue ragioni sono nell'[ADR 0018](https://github.com/coste360/porto/blob/main/docs/adr/0018-licenza.md);
altre domande nelle [domande frequenti](/installazione/domande-frequenti/).

## Marchio

Il nome **Porto**, il logo e i file in `assets/brand/porto/` sono di Coste360 SRL, con tutti i diritti riservati, ed
**esclusi dalla licenza del codice**. Un fork deve avere un proprio nome e un proprio logo. Le regole d'uso sono
nell'[ADR 0007](https://github.com/coste360/porto/blob/main/docs/adr/0007-marchio.md) e nella
[politica sul marchio](https://github.com/coste360/porto/blob/main/TRADEMARK.md).

## Dipendenze

Le dipendenze di Porto hanno licenze compatibili (MIT, BSD, Apache-2.0, ISC, MPL-2.0; OFL-1.1 per il font Titillium
Web), verificate da `make licenses`.
