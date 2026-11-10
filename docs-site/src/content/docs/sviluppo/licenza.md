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

Inserire la pagina di prenotazione o il widget in un sito non richiede licenza, purché chi lo fa non installi,
ospiti, configuri o assista Porto. La sintesi completa, con i casi di agenzie, consorzi e modelli di installazione,
è nel [README](https://github.com/coste360/porto#licenza-e-marchio).

## Marchio

Il nome **Porto**, il logo e i file in `assets/brand/porto/` sono di Coste360 SRL, con tutti i diritti riservati, ed
**esclusi dalla licenza del codice**. Un fork deve avere un proprio nome e un proprio logo. Le regole d'uso sono
nell'[ADR 0007](https://github.com/coste360/porto/blob/main/docs/adr/0007-marchio.md).

## Dipendenze

Le dipendenze di Porto hanno licenze compatibili (MIT, BSD, Apache-2.0, ISC, MPL-2.0; OFL-1.1 per il font Titillium
Web), verificate da `make licenses`.
