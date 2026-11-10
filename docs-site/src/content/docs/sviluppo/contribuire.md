---
title: Contribuire
description: Segnalazioni, pull request, regole del progetto e licenza dei contributi.
sidebar:
  order: 6
---

Segnalazioni dai porti e dai cantieri, correzioni, traduzioni, driver e documentazione sono benvenuti. Le regole
complete sono in [CONTRIBUTING.md](https://github.com/coste360/porto/blob/main/CONTRIBUTING.md).

## Prima di iniziare

1. Leggi `AGENTS.md`, la [specifica](https://github.com/coste360/porto/tree/main/docs/spec) e gli
   [ADR](https://github.com/coste360/porto/tree/main/docs/adr).
2. Apri una issue prima di una modifica non banale. Le
   [decisioni aperte](https://github.com/coste360/porto/blob/main/docs/spec/33-decisioni-aperte.md) non si decidono
   nel codice.
3. **Vulnerabilità**: mai in una issue pubblica, scrivi a security@coste360.it
   ([SECURITY.md](https://github.com/coste360/porto/blob/main/SECURITY.md)).

## Regole principali

- **API first**: modifica `api/openapi.yaml`, esegui `make gen`, poi implementa. Mai modificare i file generati.
- **Migrazioni** per SQLite e PostgreSQL con lo stesso numero; una migrazione già su `main` non si modifica.
- **Invarianti** sempre vere e verificate da test ([Invarianti](/sviluppo/invarianti/)).
- **Interfaccia** solo con componenti shadcn/ui e token del tema; ogni testo è una chiave i18n (italiano sorgente,
  inglese obbligatorio).
- **Privacy**: mai email, telefoni, documenti, token o segreti nei log.
- **Dipendenze** con licenza MIT, BSD, Apache-2.0, ISC o MPL-2.0 (OFL-1.1 solo per i font): `make licenses`.
- Codice, identificatori e commenti in inglese; issue e pull request in italiano o in inglese.

## Pull request

- Piccole e su un solo argomento, con i test, messaggi [Conventional Commits](https://www.conventionalcommits.org/).
- Una riga nel `CHANGELOG.md` per le modifiche visibili agli utenti.
- `make check` sempre; `make check-full` per modifiche a occupazione, prezzi, addebiti, pagamenti, migrazioni o flusso
  pubblico.

## Documentazione

Questo sito è in `docs-site/` (Astro Starlight). Le pagine sono in `src/content/docs/` (italiano) e
`src/content/docs/en/` (inglese) con gli stessi percorsi. `make docs-dev` avvia il sito con ricarica automatica;
`make docs-build` verifica anche tutti i link interni.

## Licenza dei contributi

I contributi sono distribuiti con la stessa licenza del progetto (BSL 1.1, poi AGPL-3.0). Contribuire non dà diritto
a rivendere Porto o a offrirlo come servizio a terzi. Il nome Porto e i file del marchio non sono coperti dalla
licenza del codice e non si modificano in una pull request. Vedi [Licenza](/sviluppo/licenza/).
