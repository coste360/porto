# ADR 0018 — Licenza: Business Source License 1.1, gratuita per i porti, AGPL dopo tre anni

- **Stato:** accettata
- **Data:** 2026-11-09
- **Decisione aperta chiusa:** D2 (`docs/spec/33-decisioni-aperte.md`)
- **Sezioni della specifica:** README, 31 (distribuzione), 33

## Contesto

Porto viene pubblicato su GitHub con il primo rilascio pubblico. L'obiettivo è lo stesso dell'altro prodotto
Coste360, Arenile: **gratuito per chi gestisce la struttura** (nessun canone, nessuna versione "Pro", nessuna
commissione sulle prenotazioni), codice leggibile e modificabile, ma senza che intermediari possano rivendere Porto
come servizio agli stessi porti a cui Coste360 lo dà gratis.

Vincoli:

- il porto, l'approdo, il cantiere o il rimessaggio deve poter installare Porto da solo (o con i propri
  dipendenti), anche su un server noleggiato o con un modello di installazione pronto (Coolify, CapRover), e
  pubblicare la pagina di prenotazione e la scheda pubblica per i propri clienti;
- chi offre Porto a terzi a pagamento (hosting, SaaS, installazione, configurazione, personalizzazione,
  manutenzione, assistenza, rivendita) o lo usa in un prodotto concorrente deve avere una licenza commerciale;
- il codice deve diventare open source nel tempo;
- nome e logo non devono seguire la licenza del codice (ADR 0007).

Le alternative valutate per Arenile valgono anche qui: la AGPL-3.0 non impedisce la rivendita come servizio, la
Functional Source License non copre con chiarezza installazione e assistenza a pagamento e si converte in una
licenza permissiva, la Elastic License 2.0 non prevede conversione e non consente un uso gratuito su misura. Usare
la stessa licenza dell'altro prodotto Coste360 rende le condizioni uguali per chi conosce già Arenile e permette di
riusare lo stesso impianto, con le definizioni adattate al dominio dei porti.

## Decisione

1. **Licenza.** Il codice di Porto è distribuito con la **Business Source License 1.1** (SPDX `BUSL-1.1`), testo
   ufficiale invariato, con i parametri nel file [`LICENSE`](../../LICENSE), che è il testo vincolante:
   - **Licensor:** Coste360 SRL. **Licensed Work:** Porto, (c) 2026 Coste360 SRL.
   - **Additional Use Grant:** uso in produzione consentito per gestire una o più **Marina Facilities**
     possedute o gestite direttamente (porti turistici, approdi e punti d'ormeggio compresi i campi boe, cantieri
     navali e nautici, rimessaggi, con i servizi accessori dello stesso sito), inclusa la pagina di prenotazione e
     il widget per i propri clienti; inoltre valutazione, sviluppo, test e preparazione di contributi. Non sono
     consentiti senza licenza commerciale la fornitura a terzi, a pagamento o con altro vantaggio commerciale, di
     servizi su Porto (hosting, servizi gestiti o SaaS, installazione, configurazione, personalizzazione,
     manutenzione, assistenza, rivendita) e l'uso in un prodotto o servizio che sostituisce Porto. La licenza
     definisce "gestita direttamente" (concessione demaniale, licenza o titolo propri, o gruppo controllato oltre
     il 50%), chiarisce che dipendenti e aiuti gratuiti non sono servizi a terzi, e che noleggiare un server
     generico o offrire un modello di installazione generico non lo sono.
   - **Change Date:** tre anni dalla data del primo rilascio pubblico di ciascuna versione.
   - **Change License:** GNU Affero General Public License v3.0 o successiva.
2. **Marchio escluso.** Nome, logotipo, simbolo, icone e il resto del kit in `assets/brand/porto/` restano di
   Coste360 SRL con tutti i diritti riservati e non sono coperti dalla licenza del codice (ADR 0007,
   `assets/brand/porto/NOTICE.md`); la BSL stessa non concede diritti sui marchi. Un fork pubblico deve cambiare
   nome e logo.
3. **Comunicazione.** Porto non viene definito "open source" finché una versione non si converte: si usano
   "codice sorgente disponibile", "source available", "gratuito per i porti" e "diventa AGPL-3.0 dopo tre anni".
   Le licenze commerciali si chiedono a info@coste360.it.
4. **Dipendenze.** Restano ammesse solo licenze compatibili sia con la BSL 1.1 sia con la AGPL-3.0 (MIT,
   BSD-2/3-Clause, Apache-2.0, ISC, MPL-2.0; OFL-1.1 per i caratteri), verificate da `make licenses`. L'immagine
   container porta l'etichetta `org.opencontainers.image.licenses=BUSL-1.1`.
5. **Contributi.** Sono benvenuti alle condizioni della licenza del repository. Se chiedere un accordo di
   licenza dei contributi (CLA) o solo la firma DCO è una decisione separata, ancora aperta (D15 in
   `docs/spec/33-decisioni-aperte.md`): finché non è presa, `CONTRIBUTING.md` non impone né l'uno né l'altra.

## Conseguenze

- Positive: i porti usano Porto gratis e senza limiti di funzioni; agenzie e rivenditori non possono offrirlo a
  pagamento senza accordo con Coste360, che può concedere licenze commerciali; ogni versione diventa AGPL-3.0 dopo
  tre anni; condizioni identiche nella sostanza a quelle di Arenile.
- Negative: Porto non è open source secondo la definizione OSI finché una versione non si converte; alcuni
  cataloghi e contributori evitano software BSL; i concetti di "gestita direttamente" e di "servizio a terzi"
  possono generare casi limite, da chiarire caso per caso (domande frequenti nel README).
- Da fare: validare con un legale il testo della concessione d'uso adattato ai porti, prima di comunicazioni
  ufficiali; decidere D15 prima della prima pull request esterna.
